//go:build headless

// headless 版入口：复用同一份 App / services，但不启动 Wails GUI。
// 事件通过 WebSocket 广播给远程前端；App 的绑定方法通过反射式 HTTP-RPC 暴露。
// 用 `go build -tags headless` 编译，跑在 NAS 上当后端服务器。
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/rt"
)

func main() {
	addr := ":" + envOr("PORT", "8810")

	app := NewApp()

	// 事件出口接到 WS hub；日志走 rt 默认(stdout)；窗口/浏览器类调用在 headless 下无意义，保持默认空实现。
	hub := newWSHub()
	rt.Wire(rt.Impl{
		Emit: func(event string, data ...interface{}) { hub.broadcast(event, data) },
		On:   hub.on,
		Off:  hub.off,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 复用桌面版同样的启动流程(调度器、MCP、服务初始化)
	app.startup(ctx)
	stdlog.Printf("[headless] startup 完成，服务监听 %s", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version, "time": time.Now().Format(time.RFC3339)})
	})
	// 访问控制：设置 JCP_TOKEN 后 /rpc /reports /ws 都要带凭证(X-JCP-Token 头或 ?token=)。
	// 两种凭证：主人令牌(JCP_TOKEN,全权) / 访客会话令牌(Login 换取,受限:配置类方法拉黑、GetConfig 脱敏)。
	// /health 与 /rpc/Login 开放。公网暴露(Cloudflare 隧道)前必须设置 JCP_TOKEN——config 里有 AI key。
	token := os.Getenv("JCP_TOKEN")
	auth := func(h http.Handler) http.Handler {
		if token == "" {
			return h
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions { // CORS 预检不带自定义头
				h.ServeHTTP(w, r)
				return
			}
			if m := strings.TrimPrefix(r.URL.Path, "/rpc/"); m == "Login" || m == "Register" { // 登录/注册免鉴权
				h.ServeHTTP(w, r)
				return
			}
			got := r.Header.Get("X-JCP-Token")
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if got == token {
				h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRole{}, "admin")))
				return
			}
			if user, ok := lookupRemoteSession(got); ok {
				r = r.WithContext(context.WithValue(context.WithValue(r.Context(), ctxKeyRole{}, "guest"), ctxKeyUser{}, user))
				h.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		})
	}
	// WS 是全局事件广播(rt.Emit 发给所有客户端),只允许主人连——访客走轮询,拿不到别人的事件流。
	mux.Handle("/ws", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role, _ := r.Context().Value(ctxKeyRole{}).(string); token != "" && role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "访客无权连接事件流"})
			return
		}
		hub.serveWS(w, r)
	})))
	mux.Handle("/rpc/", auth(withCORS(newRPCHandler(app, ctx))))
	// 投研报告 Word 文件下载。必须 no-store:Cloudflare 默认按扩展名缓存 .doc,
	// 边缘缓存命中不回源→绕过鉴权(实测无 token 也能拿到缓存副本)。
	// 访客的报告在各自私有目录,按角色定位。
	mux.Handle("/reports/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		dir := researchReportsDirAt("")
		if role, _ := r.Context().Value(ctxKeyRole{}).(string); role == "guest" {
			user, _ := r.Context().Value(ctxKeyUser{}).(string)
			g, err := guestAppFor(app, user)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "用户空间初始化失败"})
				return
			}
			dir = g.reportsDir()
		}
		http.StripPrefix("/reports/", http.FileServer(http.Dir(dir))).ServeHTTP(w, r)
	})))
	// 公开下载页:别人打开链接就能下载安装包(dataDir/dist 里的文件),无需鉴权。
	mux.HandleFunc("/download", serveDownloadPage)
	mux.HandleFunc("/download/", serveDownloadFile)
	// 自更新:客户端点「软件更新」拉这里的清单和安装包(取代 GitHub,国内可达)。全部公开。
	mux.HandleFunc("/update/manifest.json", serveUpdateManifest)
	mux.HandleFunc("/update/", serveUpdateAsset)
	// 管理页:操作审计后台,仅主人令牌(/admin?token=JCP_TOKEN)。
	mux.Handle("/admin", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role, _ := r.Context().Value(ctxKeyRole{}).(string); token != "" && role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "仅管理员可访问"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write([]byte(adminPageHTML))
	})))

	srv := &http.Server{Addr: addr, Handler: withCORS(mux)}

	// 优雅退出
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		stdlog.Println("[headless] 收到退出信号，关闭中…")
		app.shutdown(ctx)
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		stdlog.Fatalf("[headless] 服务器错误: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// 角色上下文键
type ctxKeyRole struct{}
type ctxKeyUser struct{}

// guestBlockedMethods 访客禁用的方法：配置/密钥/账号类变更,以及主人共享资产(策略/专家)的写操作。
// 其余(行情/自选/台账/AI问答等)照常——这些已按用户隔离,各改各的。
var guestBlockedMethods = map[string]bool{
	"UpdateConfig":      true,
	"SetTailForwardConfig": true,
	"RunBackupNow":      true,
	"GetBackupStatus":   true,
	"AddAgentConfig":    true,
	"UpdateAgentConfig": true,
	"DeleteAgentConfig": true,
	"AddMCPServer":      true,
	"UpdateMCPServer":   true,
	"DeleteMCPServer":   true,
	"TestMCPConnection": true,
	"TestAIConnection":  true,
	"SetRemoteUser":     true,
	"DeleteRemoteUser":  true,
	"ListRemoteUsers":   true,
	"GetAuditLogs":      true,
	"GetAuditUsers":     true,
	"SetRegisterInviteCode": true,
	// 策略/专家配置是共享资产,访客只读
	"SetActiveStrategy": true,
	"AddStrategy":       true,
	"UpdateStrategy":    true,
	"DeleteStrategy":    true,
	"GenerateStrategy":  true,
	"EnhancePrompt":     true,
	// 盘中监控读主人持仓推主人通道,访客不可触发
	"RunPositionMonitorOnce": true,
	// 进程级操作:更新/重启 NAS 后端
	"DoUpdate":   true,
	"RestartApp": true,
	// 推送走主人的私人通道(手机/Telegram)
	"PushSignal": true,
	"TestPush":   true,
	// 账号分级
	"SetUserTrusted": true,
}

// guestResourceMethods 资源类防线:普通访客 403,信任账号(RemoteUser.Trusted)放行。
// 全市场历史采集/回补是重任务(约45分钟),采集器配置是全局的。
// 未来加 AI 配额时同样按 Trusted 豁免。
var guestResourceMethods = map[string]bool{
	"CollectDailyHistory":      true,
	"BackfillHistory":          true,
	"BackfillAllHistory":       true,
	"EnrichBacktestData":       true,
	"UpdateHistoryAutoCollect": true,
}

// ---------- 访客分身 App 注册表 ----------

var (
	guestAppsMu sync.Mutex
	guestApps   = map[string]*App{}
)

// guestAppFor 取(或惰性创建)某访客的分身 App。登录名大小写不敏感,分身按小写键复用。
func guestAppFor(owner *App, username string) (*App, error) {
	key := strings.ToLower(username)
	guestAppsMu.Lock()
	defer guestAppsMu.Unlock()
	if g, ok := guestApps[key]; ok {
		return g, nil
	}
	g, err := owner.ForUser(username)
	if err != nil {
		return nil, err
	}
	stdlog.Printf("[guest] 已创建用户空间: %s", username)
	guestApps[key] = g
	return g, nil
}

// ---------- 访客操作审计 ----------
//
// 访客的 RPC 调用记入 audit.db(见 app_audit.go)。两级降噪：
//   - auditSkip：纯轮询、无个体信息的方法完全不记(否则 5s 一条刷爆)。
//   - auditDedupe：能反映"在看哪只股票"的高频查询,同参数 30 分钟内只记一条。
// Login/Register 无论成败都记(只记用户名,不碰密码)。主人(admin)流量不记。

var auditSkip = map[string]bool{
	"GetMarketIndices":  true,
	"GetTelegraphList":  true,
	"GetSessionMessages": true,
	"GetBackendMode":    true,
	"GetConfig":         true,
	"GetConfigMasked":   true,
}

var auditDedupe = map[string]bool{
	"GetStockRealTimeData": true,
	"GetOrderBook":         true,
	"GetKLineData":         true,
	"GetArchiveKLine":      true,
	"GetArchiveBars":       true,
	"GetWatchlist":         true,
}

var (
	auditDedupMu   sync.Mutex
	auditDedupSeen = map[string]time.Time{}
)

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" { // Cloudflare 隧道带真实来源
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func auditRPC(role, user, name string, rawArgs []json.RawMessage, r *http.Request) {
	if name == "Login" || name == "Register" {
		uname := ""
		if len(rawArgs) > 0 {
			_ = json.Unmarshal(rawArgs[0], &uname)
		}
		recordAudit(uname, name, "", clientIP(r)) // 只记用户名,密码绝不落盘
		return
	}
	if role != "guest" || auditSkip[name] {
		return
	}
	args := ""
	if len(rawArgs) > 0 {
		if b, err := json.Marshal(rawArgs); err == nil {
			args = string(b)
		}
	}
	if auditDedupe[name] {
		key := user + "|" + name + "|" + args
		now := time.Now()
		auditDedupMu.Lock()
		if t, ok := auditDedupSeen[key]; ok && now.Sub(t) < 30*time.Minute {
			auditDedupMu.Unlock()
			return
		}
		if len(auditDedupSeen) > 5000 { // 防无限膨胀,顺手清过期
			for k, t := range auditDedupSeen {
				if now.Sub(t) > 30*time.Minute {
					delete(auditDedupSeen, k)
				}
			}
		}
		auditDedupSeen[key] = now
		auditDedupMu.Unlock()
	}
	recordAudit(user, name, args, clientIP(r))
}

// ---------- 反射式 HTTP-RPC ----------
//
// POST /rpc/<MethodName>，body 是 JSON 数组，元素依次对应方法的参数。
// 返回：成功→方法首个非 error 返回值的 JSON(无返回值则 null)；失败→500 {"error": msg}。
// 与 Wails 绑定语义对齐(前端 window.go.main.App.Method(...) 的返回/异常)。

func newRPCHandler(app *App, appCtx context.Context) http.HandlerFunc {
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/rpc/")
		if name == "" || strings.ContainsAny(name, "/") {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "bad method path"})
			return
		}
		role, _ := r.Context().Value(ctxKeyRole{}).(string)
		guestUser, _ := r.Context().Value(ctxKeyUser{}).(string)
		// 访客权限:黑名单方法 403;GetConfig 换成脱敏版;路由到该用户的分身 App(数据隔离)
		target := app
		if role == "guest" {
			if guestBlockedMethods[name] {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "访客账号无权执行: " + name})
				return
			}
			if guestResourceMethods[name] && !app.IsTrustedRemoteUser(guestUser) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "该操作仅信任账号可用: " + name})
				return
			}
			if name == "GetConfig" {
				name = "GetConfigMasked"
			}
			g, err := guestAppFor(app, guestUser)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "用户空间初始化失败: " + err.Error()})
				return
			}
			target = g
		}
		method := reflect.ValueOf(target).MethodByName(name)
		if !method.IsValid() {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown method: " + name})
			return
		}

		body, _ := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		var rawArgs []json.RawMessage
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &rawArgs); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "args must be a JSON array: " + err.Error()})
				return
			}
		}

		auditRPC(role, guestUser, name, rawArgs, r)

		mt := method.Type()
		if mt.IsVariadic() {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "variadic methods not supported via rpc: " + name})
			return
		}

		in := make([]reflect.Value, 0, mt.NumIn())
		argIdx := 0
		for i := 0; i < mt.NumIn(); i++ {
			pt := mt.In(i)
			// context.Context 参数直接注入 app 的主 ctx，不从 body 取
			if pt == ctxType {
				in = append(in, reflect.ValueOf(appCtx))
				continue
			}
			ptr := reflect.New(pt)
			if argIdx < len(rawArgs) && len(rawArgs[argIdx]) > 0 && string(rawArgs[argIdx]) != "null" {
				if err := json.Unmarshal(rawArgs[argIdx], ptr.Interface()); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "arg " + name + "#" + strconv.Itoa(argIdx) + ": " + err.Error()})
					return
				}
			}
			in = append(in, ptr.Elem())
			argIdx++
		}

		var results []reflect.Value
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "panic in " + name})
					stdlog.Printf("[rpc] panic in %s: %v", name, rec)
				}
			}()
			results = method.Call(in)
		}()
		if results == nil {
			return // panic 已处理并写了响应
		}

		// 提取 error 与首个非 error 返回值
		var payload any
		for _, rv := range results {
			if rv.Type().Implements(errType) {
				if !rv.IsNil() {
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": rv.Interface().(error).Error()})
					return
				}
				continue
			}
			if payload == nil {
				payload = rv.Interface()
			}
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func withCORS(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-JCP-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	}
}

// ---------- WebSocket 事件总线 ----------

type wsMessage struct {
	Type  string        `json:"type"`  // "event"(下行) / "emit"(上行)
	Event string        `json:"event"` //
	Data  []interface{} `json:"data"`  //
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

type wsHub struct {
	mu       sync.RWMutex
	clients  map[*wsClient]struct{}
	handlers map[string][]func(...interface{})
	upgrader websocket.Upgrader
}

func newWSHub() *wsHub {
	return &wsHub{
		clients:  make(map[*wsClient]struct{}),
		handlers: make(map[string][]func(...interface{})),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
}

// broadcast 把 rt.Emit 的事件推给所有 WS 客户端。
func (h *wsHub) broadcast(event string, data []interface{}) {
	msg, err := json.Marshal(wsMessage{Type: "event", Event: event, Data: data})
	if err != nil {
		return
	}
	h.mu.RLock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default: // 客户端积压，丢弃这一帧，避免阻塞
		}
	}
	h.mu.RUnlock()
}

// on 注册后端对前端事件的订阅(对应 rt.On / wails EventsOn)。
func (h *wsHub) on(event string, cb func(...interface{})) {
	h.mu.Lock()
	h.handlers[event] = append(h.handlers[event], cb)
	h.mu.Unlock()
}

func (h *wsHub) off(event string) {
	h.mu.Lock()
	delete(h.handlers, event)
	h.mu.Unlock()
}

func (h *wsHub) dispatch(event string, data ...interface{}) {
	h.mu.RLock()
	cbs := append([]func(...interface{}){}, h.handlers[event]...)
	h.mu.RUnlock()
	for _, cb := range cbs {
		func() {
			defer func() { _ = recover() }()
			cb(data...)
		}()
	}
}

func (h *wsHub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{conn: conn, send: make(chan []byte, 256)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	// 写协程
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer func() {
			ticker.Stop()
			conn.Close()
		}()
		for {
			select {
			case msg, ok := <-c.send:
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, nil)
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// 读协程(前端上行 emit → dispatch 给后端订阅者)
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			close(c.send)
		}()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m wsMessage
			if json.Unmarshal(data, &m) != nil || m.Event == "" {
				continue
			}
			if m.Type == "emit" {
				h.dispatch(m.Event, m.Data...)
			}
		}
	}()
}

// ---------- 公开下载页 ----------

func distDir() string {
	dir := filepath.Join(paths.GetDataDir(), "dist")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// updateDir 自更新资源目录(manifest.json + 各平台可执行文件)。
func updateDir() string {
	dir := filepath.Join(paths.GetDataDir(), "update")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// serveUpdateManifest 提供版本清单;no-store 保证客户端总拿到最新版本号。
func serveUpdateManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	http.ServeFile(w, r, filepath.Join(updateDir(), "manifest.json"))
}

// serveUpdateAsset 提供更新安装包。更新包很大(~66MB),NAS 家宽上行有限,
// 直传经隧道慢到客户端 10 分钟超时("context deadline while reading body")。
// 对支持 gzip 的客户端做传输压缩(压到~21MB,快3倍),Go 客户端会自动解压——
// 纯服务端改动,老客户端(默认 http.Client 会带 Accept-Encoding: gzip)无需升级即可受益。
func serveUpdateAsset(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/update/"))
	if name == "" || name == "." || name == "/" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(updateDir(), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	// 版本化文件名是不可变资源,允许缓存;Vary 让 CF 按 Accept-Encoding 分别缓存压缩/未压缩两版。
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, max-age=86400, no-transform")
	// 已经是压缩格式的资源(.gz/.zip)不再二次压缩。
	compressible := !strings.HasSuffix(name, ".gz") && !strings.HasSuffix(name, ".zip")
	if compressible && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_, _ = io.Copy(gz, f)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// serveDownloadPage 下载落地页:列出 dist 目录里的安装包,任何人可访问。
func serveDownloadPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := os.ReadDir(distDir())
	type fileInfo struct {
		name string
		size int64
		mod  time.Time
	}
	files := []fileInfo{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, fileInfo{e.Name(), info.Size(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	var rows strings.Builder
	if len(files) == 0 {
		rows.WriteString(`<div class="empty">安装包正在准备中,请稍后再来。</div>`)
	}
	for _, f := range files {
		n := html.EscapeString(f.name)
		rows.WriteString(fmt.Sprintf(
			`<a class="item" href="/download/%s" download><div class="fname">📦 %s</div><div class="fmeta">%s · 更新于 %s</div><div class="btn">下 载</div></a>`,
			n, n, humanSize(f.size), f.mod.Format("2006-01-02 15:04")))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, downloadPageHTML, rows.String())
}

// serveDownloadFile 提供 dist 目录的文件下载(防目录穿越:只取 base 名)。
func serveDownloadFile(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/download/"))
	if name == "" || name == "." || name == "/" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	// 公开文件允许 CDN 短缓存;max-age 小保证换包后 5 分钟内生效
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeFile(w, r, filepath.Join(distDir(), name))
}

const downloadPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>JOEY · 下载</title>
<style>
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#060d1a;color:#e2e8f0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;}
  .card{width:min(560px,92vw);padding:36px 34px 30px;border:1px solid rgba(148,163,184,.22);border-radius:18px;background:#0b1524;box-shadow:0 24px 80px rgba(0,0,0,.5);}
  h1{margin:0;font-size:26px;} .sub{margin-top:6px;color:#94a3b8;font-size:14px;line-height:1.6;}
  .item{margin-top:18px;display:flex;align-items:center;gap:14px;padding:16px 18px;border:1px solid rgba(148,163,184,.25);border-radius:12px;background:#081020;text-decoration:none;color:inherit;transition:border-color .15s;}
  .item:hover{border-color:#0ea5e9;}
  .fname{font-size:15px;font-weight:700;flex:1;} .fmeta{color:#94a3b8;font-size:12px;white-space:nowrap;}
  .btn{flex:none;padding:8px 18px;border-radius:8px;background:#0ea5e9;color:#fff;font-size:14px;font-weight:700;}
  .empty{margin-top:18px;padding:24px;text-align:center;color:#94a3b8;border:1px dashed rgba(148,163,184,.3);border-radius:12px;}
  .steps{margin-top:22px;padding:16px 18px;border-radius:12px;background:rgba(14,165,233,.08);border:1px solid rgba(14,165,233,.25);font-size:13px;line-height:2;color:#bae6fd;}
  .steps b{color:#e0f2fe;}
</style>
</head>
<body>
<div class="card">
  <h1>JOEY 智能选股</h1>
  <div class="sub">AI 圆桌诊股 · 实时行情 · 选股扫描 · 投研报告</div>
  %s
  <div class="steps">
    <b>🍎 macOS(Apple Silicon)</b><br>
    1. 下载 macOS 版并解压<br>
    2. 双击运行 <b>安装.command</b>(首次可能需在系统设置里允许)<br>
    3. 打开 JOEY,<b>注册账号</b>或使用管理员发给你的账号登录
  </div>
  <div class="steps">
    <b>🪟 Windows 10/11(64位)</b><br>
    1. 下载 Windows 版并解压到任意文件夹<br>
    2. 双击 <b>安装并启动.bat</b>(之后直接开 JOEY.exe 即可)<br>
    3. 若提示缺 WebView2:到微软官网装 "WebView2 Runtime" 后重开<br>
    4. <b>注册账号</b>或使用管理员发给你的账号登录
  </div>
</div>
</body>
</html>`

// ---------- 管理后台页(操作审计) ----------

const adminPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>JOEY · 管理后台</title>
<style>
  body{margin:0;background:#060d1a;color:#e2e8f0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;font-size:14px;}
  .wrap{max-width:1080px;margin:0 auto;padding:28px 20px 60px;}
  h1{font-size:22px;margin:0 0 4px;} .sub{color:#94a3b8;font-size:13px;margin-bottom:20px;}
  .cards{display:flex;flex-wrap:wrap;gap:12px;margin-bottom:20px;}
  .ucard{padding:12px 16px;border:1px solid rgba(148,163,184,.25);border-radius:10px;background:#0b1524;cursor:pointer;min-width:150px;}
  .ucard.active{border-color:#0ea5e9;background:rgba(14,165,233,.12);}
  .ucard .name{font-weight:700;font-size:15px;} .ucard .meta{color:#94a3b8;font-size:12px;margin-top:3px;}
  .ucard .view{margin-top:6px;display:inline-block;padding:3px 10px;border-radius:6px;font-size:12px;background:rgba(34,197,94,.15);color:#86efac;cursor:pointer;}
  .dlg{position:fixed;inset:0;z-index:1000;display:flex;align-items:center;justify-content:center;background:rgba(3,7,15,.8);}
  .dlg .box{width:min(680px,94vw);max-height:82vh;overflow:auto;padding:22px 24px;border:1px solid rgba(148,163,184,.3);border-radius:14px;background:#0b1524;}
  .dlg h2{margin:0 0 4px;font-size:17px;} .dlg .sec{margin-top:14px;font-weight:700;color:#7dd3fc;font-size:13px;}
  .dlg .close{float:right;cursor:pointer;color:#94a3b8;font-size:20px;line-height:1;}
  .bar{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:14px;align-items:center;}
  input,select,button{padding:8px 12px;border-radius:8px;border:1px solid rgba(148,163,184,.3);background:#0b1524;color:#e2e8f0;font-size:13px;outline:none;}
  button{cursor:pointer;background:#0ea5e9;border:none;color:#fff;font-weight:700;}
  button.ghost{background:#0b1524;border:1px solid rgba(148,163,184,.3);color:#e2e8f0;font-weight:400;}
  table{width:100%;border-collapse:collapse;background:#0b1524;border-radius:10px;overflow:hidden;}
  th,td{padding:9px 12px;text-align:left;border-bottom:1px solid rgba(148,163,184,.12);font-size:13px;vertical-align:top;}
  th{background:#081020;color:#94a3b8;font-weight:600;white-space:nowrap;}
  td.args{color:#94a3b8;font-family:ui-monospace,Menlo,monospace;font-size:12px;word-break:break-all;max-width:420px;}
  .tag{display:inline-block;padding:2px 8px;border-radius:6px;font-size:12px;background:rgba(14,165,233,.15);color:#7dd3fc;white-space:nowrap;}
  .tag.pick{background:rgba(34,197,94,.15);color:#86efac;}
  .tag.auth{background:rgba(250,204,21,.14);color:#fde047;}
  .more{margin:16px auto;display:block;}
  .empty{padding:30px;text-align:center;color:#64748b;}
</style>
</head>
<body>
<div class="wrap">
  <h1>JOEY 管理后台</h1>
  <div class="sub">访客操作审计 · 点用户卡片筛选 · 高频行情查询同参数 30 分钟合并记一条</div>
  <div class="cards" id="cards"></div>
  <div class="bar">
    <select id="fUser"><option value="">全部用户</option></select>
    <input id="fMethod" placeholder="操作名过滤(如 Scanner / Watchlist)" style="width:240px">
    <button class="ghost" onclick="quick('')">全部</button>
    <button class="ghost" onclick="quick('Scanner')">选股扫描</button>
    <button class="ghost" onclick="quick('Watchlist')">自选</button>
    <button class="ghost" onclick="quick('Search')">搜索</button>
    <button class="ghost" onclick="quick('Meeting')">圆桌</button>
    <button onclick="reload()">刷新</button>
  </div>
  <table>
    <thead><tr><th>时间</th><th>用户</th><th>操作</th><th>参数</th><th>IP</th></tr></thead>
    <tbody id="rows"></tbody>
  </table>
  <div class="empty" id="empty" style="display:none">暂无记录</div>
  <button class="more ghost" id="more" onclick="loadMore()">加载更多</button>
</div>
<script>
const token = new URLSearchParams(location.search).get('token') || '';
const LABELS = {
  Login:'登录', Register:'注册账号', SearchStocks:'搜索股票', AddToWatchlist:'加入自选',
  RemoveFromWatchlist:'移除自选', SetStockGroups:'设置分组', GetWatchlist:'查看自选',
  GetStockRealTimeData:'查看行情', GetOrderBook:'查看盘口', GetKLineData:'查看K线',
  GetArchiveKLine:'查看历史K线', GetArchiveBars:'查看历史数据',
  StartResearchReport:'生成投研报告', GetResearchReport:'查投研报告', GetResearchReportHTML:'看投研报告',
  SendMeetingMessage:'圆桌发言', GetOrCreateSession:'打开圆桌', GenerateBoardReport:'生成看板AI报告',
  BackfillWatchlistHistory:'回补自选历史'
};
const PICKY = /Scanner|Watchlist|Search|Meeting|Report|KLine|OrderBook|RealTime/;
function label(m){
  if (LABELS[m]) return LABELS[m];
  if (/^Run.*Scanner/.test(m)) return '选股扫描·' + m.replace(/^Run|Scanner.*$/g,'');
  return m;
}
function tagClass(m){
  if (m==='Login'||m==='Register') return 'tag auth';
  return PICKY.test(m) ? 'tag pick' : 'tag';
}
async function rpc(method, args){
  const r = await fetch('/rpc/'+method, {method:'POST',
    headers:{'Content-Type':'application/json','X-JCP-Token':token},
    body: JSON.stringify(args)});
  if (!r.ok) throw new Error(method+' '+r.status);
  return r.json();
}
let offset = 0; const LIMIT = 100;
function esc(s){ return String(s??'').replace(/[&<>"]/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }
function fmtTime(t){ return (t||'').replace('T',' ').replace(/([+-]\d\d:\d\d|Z).*$/,''); }
async function loadUsers(){
  try {
    const [sum, accounts] = await Promise.all([rpc('GetAuditUsers',[]), rpc('ListRemoteUsers',[])]);
    const byName = {}; (sum||[]).forEach(u=>byName[u.username]=u);
    (accounts||[]).forEach(n=>{ if(!byName[n]) byName[n]={username:n,count:0,lastTime:''}; });
    const sel = document.getElementById('fUser');
    sel.innerHTML = '<option value="">全部用户</option>';
    const cards = document.getElementById('cards'); cards.innerHTML='';
    Object.values(byName).forEach(u=>{
      sel.insertAdjacentHTML('beforeend', '<option>'+esc(u.username)+'</option>');
      const c = document.createElement('div'); c.className='ucard'; c.dataset.user=u.username;
      c.innerHTML = '<div class="name">👤 '+esc(u.username)+'</div><div class="meta">'+u.count+' 次操作'+
        (u.lastTime? ' · 最近 '+esc(fmtTime(u.lastTime)) : ' · 未活跃')+'</div>'+
        '<div class="view">📂 查看数据</div>';
      c.onclick = ()=>{ sel.value = (sel.value===u.username?'':u.username); reload(); };
      c.querySelector('.view').onclick = (e)=>{ e.stopPropagation(); viewUserData(u.username); };
      cards.appendChild(c);
    });
  } catch(e){ console.error(e); }
}
async function fetchLogs(append){
  const user = document.getElementById('fUser').value;
  const method = document.getElementById('fMethod').value.trim();
  document.querySelectorAll('.ucard').forEach(c=>c.classList.toggle('active', c.dataset.user===user && user!==''));
  try {
    const logs = await rpc('GetAuditLogs', [user, method, LIMIT, offset]);
    const tb = document.getElementById('rows');
    if (!append) tb.innerHTML='';
    (logs||[]).forEach(l=>{
      const tr = document.createElement('tr');
      tr.innerHTML = '<td style="white-space:nowrap">'+esc(fmtTime(l.time))+'</td><td>'+esc(l.username)+
        '</td><td><span class="'+tagClass(l.method)+'">'+esc(label(l.method))+'</span></td>'+
        '<td class="args">'+esc(l.args)+'</td><td>'+esc(l.ip)+'</td>';
      tb.appendChild(tr);
    });
    offset += (logs||[]).length;
    document.getElementById('empty').style.display = tb.children.length? 'none':'block';
    document.getElementById('more').style.display = (logs||[]).length===LIMIT? 'block':'none';
  } catch(e){
    document.getElementById('empty').style.display='block';
    document.getElementById('empty').textContent = '加载失败: '+e.message+'(检查 ?token= 是否正确)';
  }
}
// 查看某用户的数据:用主人令牌代登(Login 密码=JCP_TOKEN)拿该用户会话,再以他的身份读自选/持仓。
async function viewUserData(user){
  const dlg = document.createElement('div'); dlg.className='dlg';
  dlg.innerHTML = '<div class="box"><span class="close">✕</span><h2>👤 '+esc(user)+' 的数据</h2><div id="ud-body" style="color:#94a3b8">加载中…</div></div>';
  dlg.querySelector('.close').onclick = ()=>dlg.remove();
  dlg.onclick = (e)=>{ if(e.target===dlg) dlg.remove(); };
  document.body.appendChild(dlg);
  const body = dlg.querySelector('#ud-body');
  try {
    const login = await rpc('Login', [user, token]);
    if (!login || !login.success) throw new Error(login && login.error || '代登失败');
    const grpc = async (m,a)=>{
      const r = await fetch('/rpc/'+m,{method:'POST',headers:{'Content-Type':'application/json','X-JCP-Token':login.token},body:JSON.stringify(a)});
      return r.ok ? r.json() : null;
    };
    const [wl, pos] = await Promise.all([grpc('GetWatchlist',[]), grpc('GetHeldPositions',[])]);
    let h = '<div class="sec">⭐ 自选 ('+((wl||[]).length)+')</div>';
    h += (wl||[]).length ? '<table><tr><th>代码</th><th>名称</th><th>现价</th><th>涨跌幅</th></tr>'+
      (wl||[]).map(s=>'<tr><td>'+esc(s.symbol)+'</td><td>'+esc(s.name)+'</td><td>'+(s.price??'-')+'</td><td>'+(s.changePercent!=null?s.changePercent+'%':'-')+'</td></tr>').join('')+'</table>'
      : '<div style="color:#64748b;padding:6px 0">空</div>';
    h += '<div class="sec">💼 持仓 ('+((pos||[]).length)+')</div>';
    h += (pos||[]).length ? '<table><tr><th>代码</th><th>名称</th><th>股数</th><th>成本价</th><th>买入日</th></tr>'+
      (pos||[]).map(p=>'<tr><td>'+esc(p.stockCode)+'</td><td>'+esc(p.stockName)+'</td><td>'+(p.position?.shares??'-')+'</td><td>'+(p.position?.costPrice??'-')+'</td><td>'+esc(p.position?.buyDate||'-')+'</td></tr>').join('')+'</table>'
      : '<div style="color:#64748b;padding:6px 0">空</div>';
    h += '<div style="margin-top:14px;font-size:12px;color:#64748b">想看他的完整界面:在 JOEY 登录框输入 账号「'+esc(user)+'」+ 密码填你的管理令牌 即可代登。</div>';
    body.innerHTML = h;
  } catch(e){ body.innerHTML = '<span style="color:#f87171">加载失败: '+esc(e.message)+'</span>'; }
}
function reload(){ offset=0; fetchLogs(false); }
function loadMore(){ fetchLogs(true); }
function quick(k){ document.getElementById('fMethod').value=k; reload(); }
document.getElementById('fUser').onchange = reload;
document.getElementById('fMethod').onkeydown = e=>{ if(e.key==='Enter') reload(); };
loadUsers(); reload();
setInterval(()=>{ if(offset<=LIMIT){ loadUsers(); reload(); } }, 30000);
</script>
</body>
</html>`
