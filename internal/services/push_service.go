package services

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"

	_ "github.com/glebarez/go-sqlite"
)

var pushLog = logger.New("push")

// PushService 信号推送服务：支持 Bark / Telegram / 飞书 / 企业微信，带 24h 防重。
type PushService struct {
	db            *sql.DB
	configService *ConfigService
}

// NewPushService 创建推送服务，推送日志存于 dataDir/push.db。
func NewPushService(dataDir string, configService *ConfigService) (*PushService, error) {
	if configService == nil {
		return nil, errors.New("config service is nil")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "push.db"))
	if err != nil {
		return nil, err
	}
	svc := &PushService{db: db, configService: configService}
	if err := svc.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return svc, nil
}

func (s *PushService) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

func (s *PushService) initSchema() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS push_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stock_code TEXT,
			signal_type TEXT,
			push_time TEXT NOT NULL,
			message TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_log_dedup ON push_log(stock_code, signal_type, push_time)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *PushService) getConfig() models.PushConfig {
	if s == nil || s.configService == nil {
		return models.PushConfig{}
	}
	cfg := s.configService.GetConfig()
	if cfg == nil {
		return models.PushConfig{}
	}
	return cfg.Push
}

// Push 推送一条信号到所有已启用渠道，带防重（同股同信号 N 小时内只推一次）。
func (s *PushService) Push(signal models.PushSignal) models.PushResult {
	result := models.PushResult{Channels: map[string]string{}}
	cfg := s.getConfig()
	if !cfg.Enabled {
		result.Message = "推送未开启"
		return result
	}
	if s.isDuplicate(signal, cfg.DedupHours) {
		result.Skipped = true
		result.Message = "防重跳过：近期已推送过相同信号"
		return result
	}
	return s.dispatch(signal, cfg, result, true)
}

// TestPush 测试推送：忽略总开关与防重，直接向已启用渠道发一条测试消息。
func (s *PushService) TestPush() models.PushResult {
	cfg := s.getConfig()
	signal := models.PushSignal{
		StockName: "连通性测试",
		StockCode: "TEST",
		Type:      "env_change",
		Message:   "这是一条来自 JOEY 的推送测试消息，收到即代表配置正确 ✅",
		Level:     "active",
	}
	return s.dispatch(signal, cfg, models.PushResult{Channels: map[string]string{}}, false)
}

// dispatch 向各启用渠道发送，logOnSuccess 控制是否写防重日志（测试推送不写）。
func (s *PushService) dispatch(signal models.PushSignal, cfg models.PushConfig, result models.PushResult, logOnSuccess bool) models.PushResult {
	title := pushTitle(signal.Type)
	body := fmt.Sprintf("%s(%s)\n%s", signal.StockName, signal.StockCode, signal.Message)
	level := signal.Level
	if level == "" {
		level = "active"
	}

	// 国外渠道(Telegram/Bark)走推送专用代理；国内渠道(飞书/企业微信)走直连/全局，避免被代理路由出国失败。
	foreignClient := s.pushProxyClient(cfg)
	domesticClient := s.httpClient()

	enabled := 0
	if cfg.Bark.Enabled && strings.TrimSpace(cfg.Bark.URL) != "" {
		enabled++
		if err := s.sendBark(foreignClient, cfg.Bark, title, body, level); err != nil {
			result.Channels["bark"] = err.Error()
		} else {
			result.Channels["bark"] = "ok"
			result.Sent = true
		}
	}
	if cfg.Telegram.Enabled && strings.TrimSpace(cfg.Telegram.BotToken) != "" {
		enabled++
		if err := s.sendTelegram(foreignClient, cfg.Telegram, title, body); err != nil {
			result.Channels["telegram"] = err.Error()
		} else {
			result.Channels["telegram"] = "ok"
			result.Sent = true
		}
	}
	if cfg.Feishu.Enabled && strings.TrimSpace(cfg.Feishu.Webhook) != "" {
		enabled++
		if err := s.postJSON(domesticClient, cfg.Feishu.Webhook, feishuPayload(title, body)); err != nil {
			result.Channels["feishu"] = err.Error()
		} else {
			result.Channels["feishu"] = "ok"
			result.Sent = true
		}
	}
	if cfg.WeWork.Enabled && strings.TrimSpace(cfg.WeWork.Webhook) != "" {
		enabled++
		if err := s.postJSON(domesticClient, cfg.WeWork.Webhook, weWorkPayload(title, body)); err != nil {
			result.Channels["weWork"] = err.Error()
		} else {
			result.Channels["weWork"] = "ok"
			result.Sent = true
		}
	}

	switch {
	case enabled == 0:
		result.Message = "没有已启用的推送渠道"
	case result.Sent:
		result.Message = "推送完成"
		if logOnSuccess {
			s.logPush(signal)
		}
	default:
		result.Message = "推送失败：所有渠道均未成功"
	}
	return result
}

func (s *PushService) isDuplicate(signal models.PushSignal, dedupHours int) bool {
	if s == nil || s.db == nil {
		return false
	}
	if dedupHours <= 0 {
		dedupHours = 24
	}
	since := time.Now().Add(-time.Duration(dedupHours) * time.Hour).Format("2006-01-02 15:04:05")
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM push_log
		WHERE stock_code = ? AND signal_type = ? AND push_time > ?`,
		signal.StockCode, signal.Type, since).Scan(&count)
	if err != nil {
		pushLog.Warn("查询防重失败: %v", err)
		return false
	}
	return count > 0
}

func (s *PushService) logPush(signal models.PushSignal) {
	if s == nil || s.db == nil {
		return
	}
	if _, err := s.db.Exec(`INSERT INTO push_log (stock_code, signal_type, push_time, message)
		VALUES (?, ?, ?, ?)`,
		signal.StockCode, signal.Type,
		time.Now().Format("2006-01-02 15:04:05"),
		signal.Message,
	); err != nil {
		pushLog.Warn("写推送日志失败: %v", err)
	}
}

// ---- 各渠道实现 ----

// httpClient 走全局代理设置的 client（国内渠道/兜底用）。
func (s *PushService) httpClient() *http.Client {
	return proxy.GetManager().GetClientWithTimeout(15 * time.Second)
}

// pushProxyClient 国外渠道专用 client：配置了推送代理就用它，否则回退全局代理。
func (s *PushService) pushProxyClient(cfg models.PushConfig) *http.Client {
	raw := strings.TrimSpace(cfg.PushProxyURL)
	if raw == "" {
		return s.httpClient()
	}
	proxyURL, err := url.Parse(raw)
	if err != nil {
		pushLog.Warn("推送代理地址无效(%s)，回退全局代理: %v", raw, err)
		return s.httpClient()
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(proxyURL),
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func (s *PushService) sendBark(client *http.Client, ch models.BarkChannel, title, body, level string) error {
	base := strings.TrimRight(strings.TrimSpace(ch.URL), "/")
	pushURL := fmt.Sprintf("%s/%s/%s?level=%s&group=stock",
		base,
		url.PathEscape(title),
		url.PathEscape(body),
		url.QueryEscape(level),
	)
	resp, err := client.Get(pushURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkHTTP(resp)
}

func (s *PushService) sendTelegram(client *http.Client, ch models.TelegramChannel, title, body string) error {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", strings.TrimSpace(ch.BotToken))
	payload := map[string]any{
		"chat_id": strings.TrimSpace(ch.ChatID),
		"text":    title + "\n" + body,
	}
	return s.postJSON(client, api, payload)
}

func (s *PushService) postJSON(client *http.Client, endpoint string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(strings.TrimSpace(endpoint), "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkHTTP(resp)
}

// 飞书自定义机器人文本消息体
func feishuPayload(title, body string) map[string]any {
	return map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": title + "\n" + body,
		},
	}
}

// 企业微信群机器人文本消息体
func weWorkPayload(title, body string) map[string]any {
	return map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": title + "\n" + body,
		},
	}
}

func checkHTTP(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
}

func pushTitle(signalType string) string {
	switch signalType {
	case models.PushTypeBuyPoint:
		return "📗 买点信号"
	case models.PushTypeStopLoss:
		return "🔴 止损预警"
	case models.PushTypeTakeProfit:
		return "🟢 止盈提醒"
	case models.PushTypeTimeStop:
		return "🟡 时间止损"
	case models.PushTypeEnvChange:
		return "📊 大盘环境变化"
	default:
		return "📌 股票提醒"
	}
}
