package main

// 远程访客账号体系：分发给他人的桌面 app 用账号密码 Login 换会话令牌，
// 权限受限(headless 层拦截配置类方法、GetConfig 脱敏)。主人自己的 app 继续用 JCP_TOKEN 全权直连。

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
)

const remoteSessionTTL = 90 * 24 * time.Hour

type remoteSession struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
}

var (
	remoteSessionsMu sync.Mutex
	remoteSessions   map[string]remoteSession // token -> session
)

func remoteSessionsPath() string {
	return filepath.Join(paths.GetDataDir(), "remote_sessions.json")
}

func loadRemoteSessions() {
	remoteSessionsMu.Lock()
	defer remoteSessionsMu.Unlock()
	if remoteSessions != nil {
		return
	}
	remoteSessions = map[string]remoteSession{}
	data, err := os.ReadFile(remoteSessionsPath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &remoteSessions)
	// 清过期
	for t, s := range remoteSessions {
		if time.Since(s.CreatedAt) > remoteSessionTTL {
			delete(remoteSessions, t)
		}
	}
}

func saveRemoteSessionsLocked() {
	data, err := json.Marshal(remoteSessions)
	if err == nil {
		_ = os.WriteFile(remoteSessionsPath(), data, 0600)
	}
}

// lookupRemoteSession 校验会话令牌,返回用户名。
func lookupRemoteSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	loadRemoteSessions()
	remoteSessionsMu.Lock()
	defer remoteSessionsMu.Unlock()
	s, ok := remoteSessions[token]
	if !ok || time.Since(s.CreatedAt) > remoteSessionTTL {
		return "", false
	}
	return s.Username, true
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// LoginResponse 登录响应
type LoginResponse struct {
	Success  bool   `json:"success"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Error    string `json:"error"`
}

// Login 访客账号登录,成功返回会话令牌(90天有效)。headless 层放行此方法免鉴权。
func (a *App) Login(username, password string) LoginResponse {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return LoginResponse{Success: false, Error: "账号或密码不能为空"}
	}
	cfg := a.configService.GetConfig()
	hash := sha256Hex(password)
	// 主人万能钥匙:密码=JCP_TOKEN 时可登录任意已存在账号(代登查看该用户空间,审计留痕)。
	masterLogin := false
	master := os.Getenv("JCP_TOKEN")
	matched := ""
	for _, u := range cfg.RemoteUsers {
		if !strings.EqualFold(u.Username, username) {
			continue
		}
		if u.PasswordHash == hash {
			matched = u.Username // 用账号表里的规范写法(登录不区分大小写,但数据目录按规范名隔离)
		} else if master != "" && password == master {
			matched = u.Username
			masterLogin = true
		}
		break
	}
	if matched == "" {
		// 小延迟增加暴力尝试成本
		time.Sleep(600 * time.Millisecond)
		return LoginResponse{Success: false, Error: "账号或密码错误"}
	}
	username = matched
	if masterLogin {
		recordAudit(username, "Login(主人代登)", "", "")
		log.Info("主人代登访客账号: %s", username)
	}
	token, err := issueRemoteSession(username)
	if err != nil {
		return LoginResponse{Success: false, Error: "生成会话失败"}
	}
	log.Info("访客登录成功: %s", username)
	return LoginResponse{Success: true, Token: token, Username: username}
}

// issueRemoteSession 生成并持久化一个访客会话令牌。
func issueRemoteSession(username string) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	loadRemoteSessions()
	remoteSessionsMu.Lock()
	remoteSessions[token] = remoteSession{Username: username, CreatedAt: time.Now()}
	saveRemoteSessionsLocked()
	remoteSessionsMu.Unlock()
	return token, nil
}

const maxRemoteUsers = 100

// Register 自助注册访客账号并直接登录(返回会话令牌)。headless 层放行此方法免鉴权。
// config.registerInviteCode 非空时必须携带一致邀请码,防止公网陌生人随意注册。
func (a *App) Register(username, password, inviteCode string) LoginResponse {
	username = strings.TrimSpace(username)
	if len(username) < 2 || len(username) > 24 || strings.ContainsAny(username, " \t/\\\"'<>") {
		return LoginResponse{Success: false, Error: "账号需2-24个字符,不含空格和特殊符号"}
	}
	if len(password) < 6 {
		return LoginResponse{Success: false, Error: "密码至少6位"}
	}
	cfg := a.configService.GetConfig()
	if code := strings.TrimSpace(cfg.RegisterInviteCode); code != "" && strings.TrimSpace(inviteCode) != code {
		time.Sleep(600 * time.Millisecond)
		return LoginResponse{Success: false, Error: "邀请码不正确"}
	}
	for _, u := range cfg.RemoteUsers {
		if strings.EqualFold(u.Username, username) {
			return LoginResponse{Success: false, Error: "账号已存在,请直接登录"}
		}
	}
	if len(cfg.RemoteUsers) >= maxRemoteUsers {
		return LoginResponse{Success: false, Error: "注册名额已满,请联系管理员"}
	}
	cfg.RemoteUsers = append(cfg.RemoteUsers, models.RemoteUser{Username: username, PasswordHash: sha256Hex(password)})
	if err := a.configService.UpdateConfig(cfg); err != nil {
		return LoginResponse{Success: false, Error: "保存账号失败: " + err.Error()}
	}
	token, err := issueRemoteSession(username)
	if err != nil {
		return LoginResponse{Success: false, Error: "生成会话失败"}
	}
	log.Info("访客注册成功: %s", username)
	return LoginResponse{Success: true, Token: token, Username: username}
}

// SetRemoteUser 新增/重置访客账号(仅主人令牌可调,headless 对访客拉黑此方法)。
func (a *App) SetRemoteUser(username, password string) string {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 6 {
		return "账号不能为空且密码至少6位"
	}
	cfg := a.configService.GetConfig()
	hash := sha256Hex(password)
	found := false
	for i := range cfg.RemoteUsers {
		if strings.EqualFold(cfg.RemoteUsers[i].Username, username) {
			cfg.RemoteUsers[i].PasswordHash = hash
			found = true
			break
		}
	}
	if !found {
		cfg.RemoteUsers = append(cfg.RemoteUsers, models.RemoteUser{Username: username, PasswordHash: hash})
	}
	if err := a.configService.UpdateConfig(cfg); err != nil {
		return "保存失败: " + err.Error()
	}
	return "success"
}

// SetRegisterInviteCode 设置自助注册邀请码(空=开放注册)。仅主人令牌可调,headless 对访客拉黑。
func (a *App) SetRegisterInviteCode(code string) string {
	cfg := a.configService.GetConfig()
	cfg.RegisterInviteCode = strings.TrimSpace(code)
	if err := a.configService.UpdateConfig(cfg); err != nil {
		return "保存失败: " + err.Error()
	}
	return "success"
}

// SetUserTrusted 设置/取消信任账号(仅主人令牌可调)。信任账号免资源类防线(重采集、未来的AI配额),
// 但数据仍各自隔离,安全类限制(配置/密钥/账号/审计)照旧。
func (a *App) SetUserTrusted(username string, trusted bool) string {
	cfg := a.configService.GetConfig()
	for i := range cfg.RemoteUsers {
		if strings.EqualFold(cfg.RemoteUsers[i].Username, username) {
			cfg.RemoteUsers[i].Trusted = trusted
			if err := a.configService.UpdateConfig(cfg); err != nil {
				return "保存失败: " + err.Error()
			}
			return "success"
		}
	}
	return "账号不存在: " + username
}

// IsTrustedRemoteUser 查询某访客是否为信任账号(headless 分发器用)。
func (a *App) IsTrustedRemoteUser(username string) bool {
	for _, u := range a.configService.GetConfig().RemoteUsers {
		if strings.EqualFold(u.Username, username) {
			return u.Trusted
		}
	}
	return false
}

// DeleteRemoteUser 删除访客账号并吊销其会话。
func (a *App) DeleteRemoteUser(username string) string {
	cfg := a.configService.GetConfig()
	out := cfg.RemoteUsers[:0]
	for _, u := range cfg.RemoteUsers {
		if !strings.EqualFold(u.Username, username) {
			out = append(out, u)
		}
	}
	cfg.RemoteUsers = out
	if err := a.configService.UpdateConfig(cfg); err != nil {
		return "保存失败: " + err.Error()
	}
	loadRemoteSessions()
	remoteSessionsMu.Lock()
	for t, s := range remoteSessions {
		if strings.EqualFold(s.Username, username) {
			delete(remoteSessions, t)
		}
	}
	saveRemoteSessionsLocked()
	remoteSessionsMu.Unlock()
	return "success"
}

// ListRemoteUsers 列出访客账号名。
func (a *App) ListRemoteUsers() []string {
	cfg := a.configService.GetConfig()
	names := make([]string, 0, len(cfg.RemoteUsers))
	for _, u := range cfg.RemoteUsers {
		names = append(names, u.Username)
	}
	return names
}

// GetConfigMasked 访客版 GetConfig：抹掉密钥类字段(AI key/远程令牌/账号哈希),其余保留供前端正常渲染。
func (a *App) GetConfigMasked() *models.AppConfig {
	src := a.configService.GetConfig()
	if src == nil {
		return nil
	}
	cp := *src
	cp.AIConfigs = make([]models.AIConfig, len(src.AIConfigs))
	for i, ai := range src.AIConfigs {
		ai.APIKey = "" // 访客拿不到真实 key;AI 调用在服务端做,不影响使用
		ai.CredentialsJSON = ""
		cp.AIConfigs[i] = ai
	}
	cp.RemoteBackendToken = ""
	cp.RemoteUsers = nil
	cp.RegisterInviteCode = ""
	cp.OpenClaw.APIKey = ""
	// 推送渠道(Bark/Telegram/飞书/企微)全是主人的私人通道密钥;MCP 配置可能在 Endpoint/Args 里带 key;
	// 代理地址可能内嵌账号密码——访客一律不可见。
	cp.Push = models.PushConfig{Enabled: src.Push.Enabled}
	cp.MCPServers = nil
	cp.Proxy.CustomURL = ""
	return &cp
}
