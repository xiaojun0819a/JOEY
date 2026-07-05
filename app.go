package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	stdruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/adk"
	"github.com/run-bigpig/jcp/internal/adk/mcp"
	"github.com/run-bigpig/jcp/internal/adk/openai"
	"github.com/run-bigpig/jcp/internal/adk/tools"
	"github.com/run-bigpig/jcp/internal/agent"
	"github.com/run-bigpig/jcp/internal/embed"
	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/meeting"
	"github.com/run-bigpig/jcp/internal/memory"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/openclaw"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"
	"github.com/run-bigpig/jcp/internal/services"
	"github.com/run-bigpig/jcp/internal/services/hottrend"

	"github.com/run-bigpig/jcp/internal/rt"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

var log = logger.New("app")

const defaultCoreContextCacheTTL = 30 * time.Second
const boardReportTimeout = 2 * time.Minute
const boardReportGenerateTimeout = 15 * time.Minute // 老陈完整报告=7个F10工具轮次+超长流式输出，gpt-5.5过lk888实测>9.5分钟仍在正常产出(流式已解决网关5分钟非流式硬超时)，给足15分钟

const (
	// 按最近半年(125交易日)真实分布校准：涨停中位88/p25=67、跌停中位14/p90=45、成交额中位2.39万亿/p25=2万亿
	lowBuyGateLimitUpMin   = 60
	lowBuyGateLimitDownMax = 50
	lowBuyGateAmountMin    = 2e12 // 2.0万亿
)

type coreContextCacheEntry struct {
	context   string
	timestamp time.Time
}

// AskBoardReportRequest Board 问答请求
type AskBoardReportRequest struct {
	StockCode string `json:"stockCode"`
	Report    string `json:"report"`
	Question  string `json:"question"`
}

// AskBoardReportResponse Board 问答响应
type AskBoardReportResponse struct {
	Success   bool   `json:"success"`
	StockCode string `json:"stockCode,omitempty"`
	Answer    string `json:"answer,omitempty"`
	ModelName string `json:"modelName,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GenerateBoardReportRequest 看板完整报告生成请求
type GenerateBoardReportRequest struct {
	StockCode string `json:"stockCode"`
	StockName string `json:"stockName"`
	Period    string `json:"period,omitempty"`
}

// GenerateBoardReportResponse 看板完整报告生成响应
type GenerateBoardReportResponse struct {
	Success     bool   `json:"success"`
	StockCode   string `json:"stockCode,omitempty"`
	StockName   string `json:"stockName,omitempty"`
	Report      string `json:"report,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	ModelName   string `json:"modelName,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty"`
	Error       string `json:"error,omitempty"`
}

// App struct
type App struct {
	ctx               context.Context
	remoteMode        bool   // true=桌面探测到 NAS 后端可达,本地进瘦身模式(不启调度器),前端路由到 NAS
	remoteConfigured  bool   // true=配置了 remoteBackendUrl(不论是否连上);连不上则回落但需提示用户
	remoteURL         string // 解析后的远程后端地址
	configService     *services.ConfigService
	marketService     *services.MarketService
	newsService       *services.NewsService
	f10Service        *services.F10Service
	historyService    *services.HistoryService
	archiveService    *services.ArchiveService // 1991-2025 全量历史档案(archive.db)
	pushService       *services.PushService
	monitorService    *services.MonitorService
	journalService    *services.JournalService
	paperService      *services.PaperService
	hotTrendService   *hottrend.HotTrendService
	longHuBangService *services.LongHuBangService
	marketPusher      *services.MarketDataPusher
	meetingService    *meeting.Service
	sessionService    *services.SessionService
	strategyService   *services.StrategyService
	agentContainer    *agent.Container
	toolRegistry      *tools.Registry
	mcpManager        *mcp.Manager
	memoryManager     *memory.Manager
	updateService     *services.UpdateService
	openClawServer    *openclaw.Server

	// 会议取消管理
	meetingCancels   map[string]context.CancelFunc
	meetingCancelsMu sync.RWMutex

	coreContextCacheTTL time.Duration
	coreContextCache    map[string]coreContextCacheEntry
	coreContextCacheMu  sync.RWMutex

	regimeCache   *models.MarketRegime
	regimeCacheAt time.Time
	regimeCacheMu sync.Mutex

	styleCache   *models.MarketStylePreference
	styleCacheAt time.Time
	styleCacheMu sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	dataDir := paths.GetDataDir()

	// 初始化文件日志
	if err := logger.InitFileLogger(filepath.Join(dataDir, "logs")); err != nil {
		log.Error("初始化文件日志失败: %v", err)
	}
	logger.SetGlobalLevel(logger.DEBUG)

	// 初始化配置服务
	configService, err := services.NewConfigService(dataDir)
	if err != nil {
		panic(err)
	}

	// 初始化研报服务
	researchReportService := services.NewResearchReportService()

	// 初始化 F10 服务
	f10Service := services.NewF10Service()

	// 初始化舆情热点服务
	hotTrendSvc, err := hottrend.NewHotTrendService()
	if err != nil {
		log.Warn("HotTrend service error: %v", err)
	}

	marketService := services.NewMarketService()
	newsService := services.NewNewsService()
	historyService, err := services.NewHistoryService(dataDir, marketService, configService)
	if err != nil {
		log.Warn("History service error: %v", err)
	}
	archiveService := services.NewArchiveService()

	// 初始化推送服务
	pushService, err := services.NewPushService(dataDir, configService)
	if err != nil {
		log.Warn("Push service error: %v", err)
	}

	// 初始化龙虎榜服务
	longHuBangService := services.NewLongHuBangService()

	// 初始化工具注册中心
	toolRegistry := tools.NewRegistry(marketService, newsService, configService, researchReportService, f10Service, hotTrendSvc, longHuBangService)

	// 初始化 MCP 管理器
	mcpManager := mcp.NewManager()
	if err := mcpManager.LoadConfigs(configService.GetConfig().MCPServers); err != nil {
		log.Warn("MCP load error: %v", err)
	}

	// 初始化会议室服务
	meetingService := meeting.NewServiceFull(toolRegistry, mcpManager)

	// 初始化记忆管理器
	var memoryManager *memory.Manager
	memConfig := configService.GetConfig().Memory
	if memConfig.Enabled {
		memoryManager = memory.NewManagerWithConfig(dataDir, memory.Config{
			MaxRecentRounds:   memConfig.MaxRecentRounds,
			MaxKeyFacts:       memConfig.MaxKeyFacts,
			MaxSummaryLength:  memConfig.MaxSummaryLength,
			CompressThreshold: memConfig.CompressThreshold,
		})
		meetingService.SetMemoryManager(memoryManager)

		if memConfig.AIConfigID != "" {
			for i := range configService.GetConfig().AIConfigs {
				if configService.GetConfig().AIConfigs[i].ID == memConfig.AIConfigID {
					meetingService.SetMemoryAIConfig(&configService.GetConfig().AIConfigs[i])
					log.Info("Memory LLM: %s", configService.GetConfig().AIConfigs[i].ModelName)
					break
				}
			}
		}
		log.Info("Memory manager enabled")
	}

	// 设置 Moderator AI 配置
	if configService.GetConfig().ModeratorAIID != "" {
		for i := range configService.GetConfig().AIConfigs {
			if configService.GetConfig().AIConfigs[i].ID == configService.GetConfig().ModeratorAIID {
				meetingService.SetModeratorAIConfig(&configService.GetConfig().AIConfigs[i])
				log.Info("Moderator LLM: %s", configService.GetConfig().AIConfigs[i].ModelName)
				break
			}
		}
	} else {
		meetingService.SetModeratorAIConfig(nil)
	}
	meetingService.SetRetryCount(configService.GetConfig().AIRetryCount)
	meetingService.SetVerboseAgentIO(configService.GetConfig().VerboseAgentIO)
	meetingService.SetAgentSelectionStyle(configService.GetConfig().AgentSelectionStyle)
	meetingService.SetEnableSecondReview(configService.GetConfig().EnableSecondReview)

	// 初始化Session服务
	sessionService := services.NewSessionService(dataDir)

	// 初始化交易台账服务
	journalService, err := services.NewJournalService(dataDir)
	if err != nil {
		log.Warn("Journal service error: %v", err)
	}

	paperService, err := services.NewPaperService(dataDir)
	if err != nil {
		log.Warn("Paper service error: %v", err)
	}

	// 初始化盘中信号监控服务（按低吸纪律盯持仓）
	monitorService := services.NewMonitorService(sessionService, marketService, historyService, pushService, configService)

	// 初始化策略服务
	strategyService := services.NewStrategyService(dataDir)

	// 初始化Agent容器（直接从StrategyService获取数据）
	agentContainer := agent.NewContainer()
	agentContainer.LoadAgents(strategyService.GetAllAgents())

	// 初始化更新服务(自更新从本人 fork 的 Release 拉取,不再指向上游 run-bigpig)
	updateService := services.NewUpdateService("xiaojun0819a", "jcp", Version)

	// 初始化 OpenClaw 服务
	openClawServer := openclaw.NewServer(meetingService, agentContainer, func(aiConfigID string) *models.AIConfig {
		cfg := configService.GetConfig()
		if aiConfigID == "" {
			aiConfigID = cfg.DefaultAIID
		}
		for i := range cfg.AIConfigs {
			if cfg.AIConfigs[i].ID == aiConfigID {
				return &cfg.AIConfigs[i]
			}
		}
		return nil
	}, func(code string) (*models.Stock, error) {
		stocks, err := marketService.GetStockRealTimeData(code)
		if err != nil {
			return nil, err
		}
		if len(stocks) == 0 {
			return nil, nil
		}
		return &stocks[0], nil
	})

	log.Info("所有服务初始化完成")

	return &App{
		configService:       configService,
		marketService:       marketService,
		newsService:         newsService,
		f10Service:          f10Service,
		historyService:      historyService,
		archiveService:      archiveService,
		pushService:         pushService,
		monitorService:      monitorService,
		journalService:      journalService,
		paperService:        paperService,
		hotTrendService:     hotTrendSvc,
		longHuBangService:   longHuBangService,
		meetingService:      meetingService,
		sessionService:      sessionService,
		strategyService:     strategyService,
		agentContainer:      agentContainer,
		toolRegistry:        toolRegistry,
		mcpManager:          mcpManager,
		memoryManager:       memoryManager,
		updateService:       updateService,
		openClawServer:      openClawServer,
		meetingCancels:      make(map[string]context.CancelFunc),
		coreContextCacheTTL: defaultCoreContextCacheTTL,
		coreContextCache:    make(map[string]coreContextCacheEntry),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
// runtimeWirer 由构建变体注入：桌面版(runtime_wails.go)在 startup 时把 rt 接到真 Wails runtime；
// headless 版在自己的入口里直接接 WS/stdout，不用这个钩子。
var runtimeWirer func(ctx context.Context)

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 把运行时事件/日志接到当前变体的实现（桌面=Wails，headless=WS）
	if runtimeWirer != nil {
		runtimeWirer(ctx)
	}

	// 初始化代理配置
	proxy.GetManager().SetConfig(&a.configService.GetConfig().Proxy)

	// 初始化 MCP 管理器（绑定主 context，预创建 toolset）
	if a.mcpManager != nil {
		if err := a.mcpManager.Initialize(ctx); err != nil {
			log.Warn("MCP 初始化失败: %v", err)
		}
	}

	// 设置 Meeting 服务的 AI 配置解析器
	if a.meetingService != nil {
		a.meetingService.SetAIConfigResolver(a.getAIConfigByID)
	}

	// 初始化更新服务
	if a.updateService != nil {
		a.updateService.Startup(ctx)
	}

	// 探测远程后端(NAS)。可达则进"瘦身模式"：本地不启任何后台调度器/采集/推送，
	// 这些交给 NAS 后端独占运行，前端把 RPC+事件路由到 NAS，避免双后端重复采集/推送。
	a.detectRemoteBackend()
	if a.remoteMode {
		log.Info("远程后端可达(%s)，进入瘦身模式：本地后台服务不启动", a.remoteURL)
		return
	}

	// 初始化并启动市场数据推送服务（需要 context）
	a.marketPusher = services.NewMarketDataPusher(a.marketService, a.configService, a.newsService)
	a.marketPusher.Start(ctx)
	log.Info("市场数据推送服务已启动")

	if a.historyService != nil {
		a.historyService.StartAutoCollect(ctx)
		log.Info("历史数据自动采集检查服务已启动")
	}

	// 启动时对模拟持仓按低吸退出纪律自动平仓一次（用真实前向日K，仅确认收盘）
	if a.paperService != nil {
		go func() {
			if n := a.ApplyPaperExitRules(); n > 0 {
				log.Info("模拟持仓启动自动平仓 %d 笔", n)
			}
		}()
	}

	if a.monitorService != nil {
		// 注入尾盘买点扫描回调（14:00 触发，扫描内部已按 Top3 推送）
		a.monitorService.SetBuyScanFunc(func() {
			a.RunLowBuyScannerV1(models.LowBuyScannerRequest{Limit: 5})
		})
		// 注入盘后波段策略扫描回调（17:30 触发，全A、加闸门、Top5 推送）
		a.monitorService.SetWaveScanFunc(func() {
			a.RunWaveScanner()
		})
		a.monitorService.Start(ctx)
		log.Info("盘中信号监控服务已启动")
	}
	a.startTailForwardScheduler()
	log.Info("2:30实盘向前验证调度器已启动")

	// 启动 OpenClaw 服务（如果已启用）
	cfg := a.configService.GetConfig()
	if cfg.OpenClaw.Enabled && cfg.OpenClaw.Port > 0 {
		if err := a.openClawServer.Start(cfg.OpenClaw.Port, cfg.OpenClaw.APIKey); err != nil {
			log.Warn("OpenClaw 启动失败: %v", err)
		}
	}
}

// allowRemoteBackend 仅桌面版(runtime_wails.go init)置为 true。
// headless/NAS 后端是权威后端，绝不能因探测到自己而进瘦身模式，所以默认 false。
var allowRemoteBackend = false

// detectRemoteBackend 探测配置的远程后端(NAS)是否可达。短超时，不阻塞启动。
func (a *App) detectRemoteBackend() {
	if !allowRemoteBackend {
		return // headless 后端：永不委托给远程
	}
	var lanURL, pubURL string
	if a.configService != nil {
		cfg := a.configService.GetConfig()
		lanURL = strings.TrimSpace(cfg.RemoteBackendURL)
		pubURL = strings.TrimSpace(cfg.RemoteBackendPublicURL)
	}
	if lanURL == "" && pubURL == "" {
		return // 未配置远程后端 = 本地全量模式(默认行为，零回归)
	}
	a.remoteConfigured = true

	probe := func(base string, timeout time.Duration) bool {
		base = strings.TrimRight(base, "/")
		client := &http.Client{Timeout: timeout}
		resp, err := client.Get(base + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}

	// 先内网(快)，失败再公网(Cloudflare 隧道，在外也能连)
	if lanURL != "" && probe(lanURL, 2*time.Second) {
		a.remoteMode = true
		a.remoteURL = strings.TrimRight(lanURL, "/")
		return
	}
	if pubURL != "" && probe(pubURL, 5*time.Second) {
		a.remoteMode = true
		a.remoteURL = strings.TrimRight(pubURL, "/")
		log.Info("内网后端不可达，改走公网隧道: %s", a.remoteURL)
		return
	}
	if a.remoteURL == "" {
		if lanURL != "" {
			a.remoteURL = strings.TrimRight(lanURL, "/")
		} else {
			a.remoteURL = strings.TrimRight(pubURL, "/")
		}
	}
	log.Warn("远程后端探测失败(内网%s/公网%s)，回落本地全量模式", lanURL, pubURL)
}

// BackendMode 供前端查询当前后端模式，决定是否把调用路由到 NAS。
type BackendMode struct {
	Mode  string `json:"mode"`  // "local"(纯本地) | "remote"(连上 NAS) | "fallback"(配了 NAS 但连不上，改动不同步)
	URL   string `json:"url"`   // remote/fallback 时为实际选用的 NAS 地址(内网或公网隧道)
	Token string `json:"token"` // 访问令牌,前端请求 NAS 时带上(X-JCP-Token)
}

// GetBackendMode 前端启动时调用：remote 则装 RPC/WS 代理指向 NAS；
// fallback 表示配了远程但没连上(用本地旧数据，需提示用户改动不会同步)；local 为纯本地。
func (a *App) GetBackendMode() BackendMode {
	token := ""
	if a.configService != nil {
		token = strings.TrimSpace(a.configService.GetConfig().RemoteBackendToken)
	}
	if a.remoteMode {
		return BackendMode{Mode: "remote", URL: a.remoteURL, Token: token}
	}
	if a.remoteConfigured {
		return BackendMode{Mode: "fallback", URL: a.remoteURL}
	}
	return BackendMode{Mode: "local"}
}

// shutdown 应用关闭时调用
func (a *App) shutdown(ctx context.Context) {
	log.Info("应用正在关闭...")
	if a.openClawServer != nil {
		a.openClawServer.Stop()
	}
	if a.marketPusher != nil {
		a.marketPusher.Stop()
	}
	if a.historyService != nil {
		a.historyService.Close()
	}
	if a.monitorService != nil {
		a.monitorService.Stop()
	}
	if a.pushService != nil {
		a.pushService.Close()
	}
	if a.journalService != nil {
		a.journalService.Close()
	}
	if a.paperService != nil {
		a.paperService.Close()
	}
	logger.Close()
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return "Hello " + name + ", It's show time!"
}

// GetConfig 获取配置
func (a *App) GetConfig() *models.AppConfig {
	return a.configService.GetConfig()
}

// UpdateConfig 更新配置
func (a *App) UpdateConfig(config *models.AppConfig) string {
	if err := a.configService.UpdateConfig(config); err != nil {
		return err.Error()
	}
	// 重新加载 MCP 配置
	if a.mcpManager != nil && config.MCPServers != nil {
		if err := a.mcpManager.LoadConfigs(config.MCPServers); err != nil {
			log.Warn("MCP reload error: %v", err)
		}
	}
	// 更新代理配置
	proxy.GetManager().SetConfig(&config.Proxy)
	// 更新记忆管理器的 LLM 配置
	if a.meetingService != nil && config.Memory.AIConfigID != "" {
		for i := range config.AIConfigs {
			if config.AIConfigs[i].ID == config.Memory.AIConfigID {
				a.meetingService.SetMemoryAIConfig(&config.AIConfigs[i])
				break
			}
		}
	} else if a.meetingService != nil {
		a.meetingService.SetMemoryAIConfig(nil)
	}
	// 更新 Moderator AI 配置
	if a.meetingService != nil && config.ModeratorAIID != "" {
		for i := range config.AIConfigs {
			if config.AIConfigs[i].ID == config.ModeratorAIID {
				a.meetingService.SetModeratorAIConfig(&config.AIConfigs[i])
				break
			}
		}
	} else if a.meetingService != nil {
		a.meetingService.SetModeratorAIConfig(nil)
	}
	if a.meetingService != nil {
		a.meetingService.SetRetryCount(config.AIRetryCount)
		a.meetingService.SetVerboseAgentIO(config.VerboseAgentIO)
		a.meetingService.SetAgentSelectionStyle(config.AgentSelectionStyle)
		a.meetingService.SetEnableSecondReview(config.EnableSecondReview)
	}
	// 更新 OpenClaw 服务配置（热更新）
	a.applyOpenClawConfig(&config.OpenClaw)
	return "success"
}

// applyOpenClawConfig 应用 OpenClaw 配置变更
func (a *App) applyOpenClawConfig(cfg *models.OpenClawConfig) {
	if a.openClawServer == nil {
		return
	}
	if !cfg.Enabled {
		a.openClawServer.Stop()
		return
	}
	if cfg.Port <= 0 {
		return
	}
	// 端口或密钥变更时重启
	if a.openClawServer.IsRunning() {
		if a.openClawServer.GetPort() != cfg.Port {
			a.openClawServer.Restart(cfg.Port, cfg.APIKey)
		}
	} else {
		a.openClawServer.Start(cfg.Port, cfg.APIKey)
	}
}

// GetOpenClawStatus 获取 OpenClaw 服务状态
func (a *App) GetOpenClawStatus() map[string]any {
	if a.openClawServer == nil {
		return map[string]any{"running": false}
	}
	return map[string]any{
		"running": a.openClawServer.IsRunning(),
		"port":    a.openClawServer.GetPort(),
	}
}

// mergeRealtimeStock 合并实时行情字段，保留本地静态字段
func (a *App) mergeRealtimeStock(base models.Stock, rt models.Stock) models.Stock {
	merged := base
	if rt.Name != "" {
		merged.Name = rt.Name
	}
	merged.Price = rt.Price
	merged.Change = rt.Change
	merged.ChangePercent = rt.ChangePercent
	merged.Volume = rt.Volume
	merged.Amount = rt.Amount
	merged.Open = rt.Open
	merged.High = rt.High
	merged.Low = rt.Low
	merged.PreClose = rt.PreClose
	return merged
}

// GetWatchlist 获取自选股列表（附带实时行情）
func (a *App) GetWatchlist() []models.Stock {
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		log.Warn("同步交易台账组失败: %v", err)
	}
	list := a.configService.GetWatchlist()
	if len(list) == 0 {
		return list
	}

	// 收集所有股票代码，拉一次实时行情
	codes := make([]string, len(list))
	for i, s := range list {
		codes[i] = s.Symbol
	}
	realtime, err := a.marketService.GetStockRealTimeData(codes...)
	if err != nil || len(realtime) == 0 {
		return list
	}

	// 用实时数据填充
	rtMap := make(map[string]models.Stock, len(realtime))
	for _, s := range realtime {
		rtMap[s.Symbol] = s
	}
	result := make([]models.Stock, len(list))
	for i, s := range list {
		if rt, ok := rtMap[s.Symbol]; ok {
			result[i] = a.mergeRealtimeStock(s, rt)
		} else {
			result[i] = s
		}
	}
	return result
}

// AddToWatchlist 添加自选股
func (a *App) AddToWatchlist(stock models.Stock) string {
	if err := a.configService.AddToWatchlist(stock); err != nil {
		return err.Error()
	}
	// 同步添加到推送订阅
	a.marketPusher.AddSubscription(stock.Symbol)
	return "success"
}

// RemoveFromWatchlist 移除自选股
func (a *App) RemoveFromWatchlist(symbol string) string {
	if err := a.configService.RemoveFromWatchlist(symbol); err != nil {
		return err.Error()
	}
	// 同步移除推送订阅
	a.marketPusher.RemoveSubscription(symbol)
	// 清空该股票的聊天记录
	a.sessionService.ClearMessages(symbol)
	// 同步清除该股票的记忆
	if a.memoryManager != nil {
		if err := a.memoryManager.DeleteMemory(symbol); err != nil {
			log.Error("delete memory error: %v", err)
		}
	}
	return "success"
}

// GetStockGroups 返回自选分组映射 symbol -> [分组ID]（lowbuy/wave）
func (a *App) GetStockGroups() map[string][]string {
	if a.configService == nil {
		return map[string][]string{}
	}
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		log.Warn("同步交易台账组失败: %v", err)
	}
	return a.configService.GetStockGroups()
}

// SetStockGroups 设置某只股票所属分组（覆盖式，空数组=从所有分组移除）
func (a *App) SetStockGroups(symbol string, groups []string) string {
	if a.configService == nil {
		return "config service not ready"
	}
	if err := a.configService.SetStockGroups(symbol, groups); err != nil {
		return err.Error()
	}
	return "success"
}

// GetStockGroupDefs 返回自定义分组定义列表
func (a *App) GetStockGroupDefs() []models.StockGroup {
	if a.configService == nil {
		return []models.StockGroup{}
	}
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		log.Warn("同步交易台账组失败: %v", err)
	}
	return a.configService.GetStockGroupDefs()
}

// AddStockGroupDef 新建分组，返回新分组(含ID)
func (a *App) AddStockGroupDef(name string) *models.StockGroup {
	if a.configService == nil {
		return nil
	}
	g, err := a.configService.AddStockGroupDef(name)
	if err != nil {
		return nil
	}
	return &g
}

// RenameStockGroupDef 重命名分组
func (a *App) RenameStockGroupDef(id, name string) string {
	if a.configService == nil {
		return "config service not ready"
	}
	if err := a.configService.RenameStockGroupDef(id, name); err != nil {
		return err.Error()
	}
	return "success"
}

// DeleteStockGroupDef 删除分组
func (a *App) DeleteStockGroupDef(id string) string {
	if a.configService == nil {
		return "config service not ready"
	}
	if err := a.configService.DeleteStockGroupDef(id); err != nil {
		return err.Error()
	}
	return "success"
}

// SyncTradeJournalWatchGroup 将交易台账里的股票同步到自选分组"交易台账组"。
func (a *App) SyncTradeJournalWatchGroup() string {
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		return err.Error()
	}
	return "success"
}

func (a *App) syncTradeJournalWatchGroup() error {
	if a == nil || a.configService == nil {
		return errors.New("config service not ready")
	}
	if a.journalService == nil {
		return nil
	}

	entries := a.journalService.List()
	byCode := make(map[string]models.Stock)
	for _, entry := range entries {
		code := normalizeJournalStockSymbol(entry.StockCode)
		if code == "" {
			continue
		}
		name := strings.TrimSpace(entry.StockName)
		if name == "" {
			name = code
		}
		if existing, ok := byCode[code]; ok && strings.TrimSpace(existing.Name) != "" && existing.Name != code {
			continue
		}
		byCode[code] = models.Stock{
			Symbol: code,
			Name:   name,
		}
	}

	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	members := make([]models.Stock, 0, len(codes))
	for _, code := range codes {
		members = append(members, byCode[code])
	}

	if err := a.configService.SyncStockGroupMembers(services.TradeJournalGroupID, services.TradeJournalGroupName, members); err != nil {
		return err
	}
	if a.marketPusher != nil {
		for _, stock := range members {
			a.marketPusher.AddSubscription(stock.Symbol)
		}
	}
	return nil
}

func normalizeJournalStockSymbol(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "sh") || strings.HasPrefix(value, "sz") || strings.HasPrefix(value, "bj") {
		if len(value) >= 8 {
			return value[:8]
		}
		return value
	}
	code := normalizeAStockCode(value)
	if len(code) != 6 {
		return value
	}
	switch {
	case strings.HasPrefix(code, "6"), strings.HasPrefix(code, "5"), strings.HasPrefix(code, "9"):
		return "sh" + code
	case strings.HasPrefix(code, "8"), strings.HasPrefix(code, "4"):
		return "bj" + code
	default:
		return "sz" + code
	}
}

// GetMarketRegime 返回当前大盘牛熊环境（缓存5分钟，避免频繁全A快照）
func (a *App) GetMarketRegime() models.MarketRegime {
	a.regimeCacheMu.Lock()
	if a.regimeCache != nil && time.Since(a.regimeCacheAt) < 5*time.Minute {
		r := *a.regimeCache
		a.regimeCacheMu.Unlock()
		return r
	}
	a.regimeCacheMu.Unlock()

	if a.marketService == nil {
		return models.MarketRegime{Regime: "neutral", Emoji: "⚖️", Label: "数据不可用"}
	}
	snap, err := a.marketService.BuildScanMarketSnapshot()
	if err != nil || snap.TotalAmount <= 0 {
		// 失败时返回上次缓存(若有)
		a.regimeCacheMu.Lock()
		defer a.regimeCacheMu.Unlock()
		if a.regimeCache != nil {
			return *a.regimeCache
		}
		return models.MarketRegime{Regime: "neutral", Emoji: "⚖️", Label: "数据不可用"}
	}

	r := classifyMarketRegime(snap.LimitUpCount, snap.LimitDownCount, snap.TotalAmount, snap.ShPrice, snap.ShMA20)
	a.regimeCacheMu.Lock()
	a.regimeCache = &r
	a.regimeCacheAt = time.Now()
	a.regimeCacheMu.Unlock()
	return r
}

// classifyMarketRegime 按近半年(125交易日)真实分布判定牛熊。
// 涨停:p10=51/p25=67/中位88/p75=106；跌停:p90=45；成交额:p25≈2万亿/p75≈2.8万亿。
func classifyMarketRegime(limitUp, limitDown int, amount, shPrice, shMA20 float64) models.MarketRegime {
	amountYi := amount / 1e8
	aboveMA20 := shPrice > 0 && shMA20 > 0 && shPrice >= shMA20
	diff := limitUp - limitDown

	regime := "neutral"
	emoji := "⚖️"
	label := "震荡"

	switch {
	case (limitDown > 45 && amount < 2e12) || (limitDown > 90):
		// 跌停飙升 + 缩量 / 极端杀跌 → 熊
		regime, emoji, label = "bear", "🐻", "偏熊"
	case limitUp > 105 || (diff >= 50 && amount >= 2.8e12):
		// 涨停密集 / 多空差大且放量 → 牛
		regime, emoji, label = "bull", "🐂", "偏牛"
	case limitUp < 55 || amount < 2e12:
		// 涨停冰点或缩量 → 偏弱
		regime, emoji, label = "bear", "🐻", "偏弱"
	default:
		regime, emoji, label = "neutral", "⚖️", "震荡"
	}

	// 指数滞后提示：个股活跃但上证未站上MA20 = 结构市
	if regime == "bull" && !aboveMA20 {
		label = "偏牛·结构市"
	}

	return models.MarketRegime{
		Regime: regime, Emoji: emoji, Label: label,
		LimitUp: limitUp, LimitDown: limitDown, AmountYi: amountYi,
		ShPrice: shPrice, ShMA20: shMA20, AboveMA20: aboveMA20,
		AsOf: time.Now().Format("15:04"), Available: true,
	}
}

// GetMarketStylePreference 返回市场风格偏好（大盘/中盘/小盘/微盘）
func (a *App) GetMarketStylePreference() models.MarketStylePreference {
	a.styleCacheMu.Lock()
	if a.styleCache != nil && time.Since(a.styleCacheAt) < 5*time.Minute {
		s := *a.styleCache
		a.styleCacheMu.Unlock()
		return s
	}
	a.styleCacheMu.Unlock()

	if a.marketService == nil {
		return models.MarketStylePreference{Label: "数据不可用", Available: false}
	}
	items, note := a.buildMarketStyleItems()
	pref := classifyMarketStylePreference(items)
	pref.DataNote = note
	pref.AsOf = time.Now().Format("15:04")
	pref.Available = len(items) >= 2
	fallback := a.GetMarketRegime()
	pref.RegimeFallback = &fallback
	if !pref.Available && pref.Label == "" {
		pref.Label = fallback.Label
		pref.SubLabel = "风格数据不足"
	}

	a.styleCacheMu.Lock()
	a.styleCache = &pref
	a.styleCacheAt = time.Now()
	a.styleCacheMu.Unlock()
	return pref
}

func (a *App) buildMarketStyleItems() ([]models.MarketStyleItem, string) {
	items := make([]models.MarketStyleItem, 0, 4)
	indexCodes := []struct {
		key       string
		name      string
		indexName string
		code      string
	}{
		{key: "large", name: "大盘股", indexName: "沪深300", code: "sh000300"},
		{key: "mid", name: "中盘股", indexName: "中证500", code: "sh000905"},
		{key: "small", name: "小盘股", indexName: "中证1000", code: "sh000852"},
	}

	if indices, err := a.marketService.GetMarketStyleIndices(); err == nil {
		byCode := map[string]models.MarketIndex{}
		for _, index := range indices {
			byCode[strings.ToLower(strings.TrimSpace(index.Code))] = index
		}
		for _, idx := range indexCodes {
			if index, ok := byCode[idx.code]; ok && index.Price > 0 && !math.IsNaN(index.ChangePercent) && !math.IsInf(index.ChangePercent, 0) {
				items = append(items, models.MarketStyleItem{
					Key:           idx.key,
					Name:          idx.name,
					IndexName:     idx.indexName,
					Code:          idx.code,
					ChangePercent: round2Float(index.ChangePercent),
					Source:        "指数实时行情",
				})
			}
		}
	}

	note := "微盘股=全A总市值<50亿、非ST、价格有效样本等权涨跌幅代理"
	if rows, err := a.marketService.GetAllAStockSnapshot(false); err == nil {
		sum := 0.0
		count := 0
		for _, row := range rows {
			if row.Price <= 0 || row.TotalMarketCap <= 0 || row.TotalMarketCap >= 50e8 || row.IsST {
				continue
			}
			if math.IsNaN(row.ChangePercent) || math.IsInf(row.ChangePercent, 0) {
				continue
			}
			sum += row.ChangePercent
			count++
		}
		if count > 0 {
			items = append(items, models.MarketStyleItem{
				Key:           "micro",
				Name:          "微盘股",
				IndexName:     "微盘股",
				Code:          "micro-cap<50亿",
				ChangePercent: round2Float(sum / float64(count)),
				Source:        fmt.Sprintf("全A快照等权代理，样本%d只", count),
			})
			note = fmt.Sprintf("%s，当前样本%d只", note, count)
		}
	} else {
		note = "微盘股代理计算失败：" + err.Error()
	}

	return items, note
}

func classifyMarketStylePreference(items []models.MarketStyleItem) models.MarketStylePreference {
	result := models.MarketStylePreference{Items: items}
	if len(items) == 0 {
		result.Label = "数据不可用"
		result.SubLabel = "等待风格指数"
		return result
	}

	strong := items[0]
	weak := items[0]
	allUp := true
	allDown := true
	for _, item := range items {
		if item.ChangePercent > strong.ChangePercent {
			strong = item
		}
		if item.ChangePercent < weak.ChangePercent {
			weak = item
		}
		if item.ChangePercent <= 0 {
			allUp = false
		}
		if item.ChangePercent >= 0 {
			allDown = false
		}
	}

	gap := strong.ChangePercent - weak.ChangePercent
	result.StrengthGap = round2Float(gap)
	result.StrongKey = strong.Key
	result.WeakKey = weak.Key

	significant := gap >= 1.0
	switch {
	case allUp && significant:
		result.Scenario = "全面上涨且显著差异"
		result.Label = strong.Name + "更强"
		result.SubLabel = weak.Name + "相对弱势"
	case allUp:
		result.Scenario = "全面上涨但差异不显著"
		result.Label = "无明显偏好"
		result.SubLabel = "整体风格上涨"
	case !allUp && !allDown && significant:
		result.Scenario = "涨跌分化且显著差异"
		result.Label = strong.Name + "更强"
		result.SubLabel = weak.Name + "弱势"
	case !allUp && !allDown:
		result.Scenario = "涨跌分化但差异不显著"
		result.Label = "无明显偏好"
		result.SubLabel = strong.Name + "相对强势"
	case allDown && significant:
		result.Scenario = "全面下跌且显著差异"
		result.Label = weak.Name + "领跌"
		result.SubLabel = strong.Name + "相对抗跌"
	default:
		result.Scenario = "全面下跌且差异不显著"
		result.Label = "无明显偏好"
		result.SubLabel = "风格整体下跌"
	}

	return result
}

// GetStockRealTimeData 获取股票实时数据
func (a *App) GetStockRealTimeData(codes []string) []models.Stock {
	stocks, _ := a.marketService.GetStockRealTimeData(codes...)
	return stocks
}

// GetKLineData 获取K线数据
func (a *App) GetKLineData(code string, period string, days int) []models.KLineData {
	// 长周期日K优先走本地历史档案(1991-2025 全量,行情源最多只给几百根)。
	// 档案止于 2025-12-31,近期部分用行情源补齐并在衔接日按收盘价比例对齐前复权口径。
	if period == "1d" && days > 500 && a.archiveService != nil && a.archiveService.Available() {
		if merged := a.klineFromArchive(code, days); len(merged) > 0 {
			return merged
		}
	}
	data, _ := a.marketService.GetKLineData(code, period, days)
	return data
}

// klineFromArchive 档案K线 + 行情源近期数据拼接(前复权口径对齐)。
func (a *App) klineFromArchive(code string, days int) []models.KLineData {
	archived, err := a.archiveService.KLine(code, "", "", days)
	if err != nil || len(archived) == 0 {
		return nil
	}
	// 行情源近期(最多600根,覆盖档案截止后的部分)
	recent, _ := a.marketService.GetKLineData(code, "1d", 600)
	if len(recent) == 0 {
		return archived
	}
	firstRecent := recent[0].Time
	// 找重叠日,按收盘比对齐(两侧都是前复权,但基准日不同;有分红送转会差一个比例)
	ratio := 1.0
	for i := len(archived) - 1; i >= 0; i-- {
		if archived[i].Time == firstRecent && archived[i].Close > 0 {
			ratio = recent[0].Close / archived[i].Close
			break
		}
		if archived[i].Time < firstRecent {
			break
		}
	}
	var merged []models.KLineData
	for _, k := range archived {
		if k.Time >= firstRecent {
			break
		}
		if ratio != 1.0 {
			k.Open = round2(k.Open * ratio)
			k.High = round2(k.High * ratio)
			k.Low = round2(k.Low * ratio)
			k.Close = round2(k.Close * ratio)
		}
		merged = append(merged, k)
	}
	merged = append(merged, recent...)
	if days > 0 && len(merged) > days {
		merged = merged[len(merged)-days:]
	}
	return merged
}

// ========== 历史档案 API(archive.db, 1991-2025 全量) ==========

// GetArchiveKLine 档案前复权日K。start/end 形如 2020-01-01 可空,days<=0 不限根数。
func (a *App) GetArchiveKLine(code, startDate, endDate string, days int) []models.KLineData {
	if a.archiveService == nil {
		return nil
	}
	data, err := a.archiveService.KLine(code, startDate, endDate, days)
	if err != nil {
		log.Warn("档案K线查询失败 %s: %v", code, err)
		return nil
	}
	return data
}

// GetArchiveBars 档案全列记录(含换手/量比/PE/PB/PS/股息率/市值/复权因子/涨跌停)。
func (a *App) GetArchiveBars(code, startDate, endDate string, limit int) []services.ArchiveBar {
	if a.archiveService == nil {
		return nil
	}
	bars, err := a.archiveService.Bars(code, startDate, endDate, limit)
	if err != nil {
		log.Warn("档案查询失败 %s: %v", code, err)
		return nil
	}
	return bars
}

// GetArchiveStockInfo 单只档案覆盖(起止日期/行数)。
func (a *App) GetArchiveStockInfo(code string) *services.ArchiveStockInfo {
	if a.archiveService == nil {
		return nil
	}
	info, err := a.archiveService.StockInfo(code)
	if err != nil {
		return nil
	}
	return info
}

// GetArchiveCoverage 档案库整体覆盖统计。
func (a *App) GetArchiveCoverage() services.ArchiveCoverage {
	if a.archiveService == nil {
		return services.ArchiveCoverage{}
	}
	return a.archiveService.Coverage()
}

// GetOrderBook 获取盘口数据（真实五档）
func (a *App) GetOrderBook(code string) models.OrderBook {
	orderBook, _ := a.marketService.GetRealOrderBook(code)
	return orderBook
}

// SearchStocks 搜索股票
func (a *App) SearchStocks(keyword string) []services.StockSearchResult {
	return a.marketService.SearchStocks(keyword, 20)
}

// GetMarketStatus 获取市场状态
func (a *App) GetMarketStatus() services.MarketStatus {
	return a.marketService.GetMarketStatus()
}

// GetMarketIndices 获取大盘指数
func (a *App) GetMarketIndices() []models.MarketIndex {
	indices, _ := a.marketService.GetMarketIndices()
	return indices
}

// CollectDailyHistory 采集全A每日快照并写入本地历史库
func (a *App) CollectDailyHistory(req models.HistoryCollectRequest) models.HistoryCollectResult {
	if a.historyService == nil {
		return models.HistoryCollectResult{
			TradeDate:  req.TradeDate,
			Status:     "failed",
			Message:    "历史采集服务未初始化",
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	return a.historyService.CollectDailyHistory(req)
}

// PushSignal 向已启用的推送渠道发送一条信号（带防重）
func (a *App) PushSignal(signal models.PushSignal) models.PushResult {
	if a.pushService == nil {
		return models.PushResult{Message: "推送服务未初始化"}
	}
	return a.pushService.Push(signal)
}

// TestPush 发送一条测试推送，验证各渠道配置是否正确
func (a *App) TestPush() models.PushResult {
	if a.pushService == nil {
		return models.PushResult{Message: "推送服务未初始化"}
	}
	return a.pushService.TestPush()
}

// RunPositionMonitorOnce 手动跑一次盘中持仓监控，返回触发信号数（用于测试/即时检查）
func (a *App) RunPositionMonitorOnce() int {
	if a.monitorService == nil {
		return 0
	}
	return a.monitorService.MonitorPositionsIntraday()
}

// RunAfterMarketCheckOnce 手动跑一次盘后时间止损检查，返回触发信号数
func (a *App) RunAfterMarketCheckOnce() int {
	if a.monitorService == nil {
		return 0
	}
	return a.monitorService.RunAfterMarketCheck()
}

// BackfillHistory 用日K历史回补指定股票的历史行情到本地库（冷启动补足回测数据）
func (a *App) BackfillHistory(req models.HistoryBackfillRequest) models.HistoryBackfillResult {
	if a.historyService == nil {
		return models.HistoryBackfillResult{
			Status:     "failed",
			Message:    "历史采集服务未初始化",
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	return a.historyService.BackfillHistory(req)
}

// BackfillWatchlistHistory 回补自选股的历史行情，days<=0 时默认一年（250 个交易日）
func (a *App) BackfillWatchlistHistory(days int) models.HistoryBackfillResult {
	if a.historyService == nil {
		return models.HistoryBackfillResult{
			Status:     "failed",
			Message:    "历史采集服务未初始化",
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	codes := make([]string, 0)
	for _, s := range a.configService.GetWatchlist() {
		if s.Symbol != "" {
			codes = append(codes, s.Symbol)
		}
	}
	return a.historyService.BackfillHistory(models.HistoryBackfillRequest{Codes: codes, Days: days})
}

// EnrichBacktestData 为全A补齐回测所需字段（开高低/主力资金流/换手/市值），days<=0 默认120
func (a *App) EnrichBacktestData(days int, includeBeijing bool) models.HistoryBackfillResult {
	if a.historyService == nil || a.marketService == nil {
		return models.HistoryBackfillResult{Status: "failed", Message: "服务未初始化"}
	}
	stocks, err := a.marketService.GetAllAStockSnapshot(includeBeijing)
	if err != nil {
		return models.HistoryBackfillResult{Status: "failed", Message: "获取全A代码失败: " + err.Error()}
	}
	codes := make([]string, 0, len(stocks))
	for _, st := range stocks {
		if st.Symbol != "" {
			codes = append(codes, st.Symbol)
		}
	}
	return a.historyService.EnrichForBacktest(models.HistoryBackfillRequest{Codes: codes, Days: days})
}

// RunBacktest 用低吸规则在历史上回测，返回胜率/盈亏比等统计
func (a *App) RunBacktest(req models.BacktestRequest) models.BacktestResult {
	if a.historyService == nil {
		return models.BacktestResult{Status: "failed", Message: "历史服务未初始化"}
	}
	return a.historyService.RunBacktest(req)
}

// RunPortfolioBacktest 真实组合回测（固定资金/限同时持仓），返回真实净值与最大回撤
func (a *App) RunPortfolioBacktest(req models.BacktestRequest) models.BacktestResult {
	if a.historyService == nil {
		return models.BacktestResult{Status: "failed", Message: "历史服务未初始化"}
	}
	return a.historyService.RunPortfolioBacktest(req)
}

// RunWaveBacktest 龙头吃鱼身策略回测（300/688）
func (a *App) RunWaveBacktest(req models.BacktestRequest) models.BacktestResult {
	if a.historyService == nil {
		return models.BacktestResult{Status: "failed", Message: "历史服务未初始化"}
	}
	return a.historyService.RunWaveBacktest(req)
}

// RunWaveScanner 波段策略1.0选股扫描，全A、加闸门，并把命中标的推送(source=wave)
func (a *App) RunWaveScanner() models.WaveScanResult {
	if a.historyService == nil {
		return models.WaveScanResult{Message: "历史服务未初始化"}
	}
	res := a.historyService.ScanWaveCandidates(10, true)
	a.saveWaveStrategyPicks("wave-v1", "波段策略 1.0", res)
	a.pushWaveSignals(res.Items)
	return res
}

// RunWaveScannerWithGate 波段策略1.0选股扫描。useGate=false 时仅临时绕过大盘闸门，不改全局规则、不推送。
func (a *App) RunWaveScannerWithGate(useGate bool) models.WaveScanResult {
	if a.historyService == nil {
		return models.WaveScanResult{Message: "历史服务未初始化"}
	}
	res := a.historyService.ScanWaveCandidates(10, useGate)
	if !useGate {
		res.GatePassed = false
		res.GateBypassed = true
		if res.Message == "" {
			res.Message = fmt.Sprintf("已临时打开闸门筛选，命中 %d 只；只作观察，不代表大盘环境通过", res.Count)
		} else {
			res.Message += "；已临时打开闸门，仅作观察"
		}
		a.saveWaveStrategyPicks("wave-v1", "波段策略 1.0", res)
		return res
	}
	a.pushWaveSignals(res.Items)
	a.saveWaveStrategyPicks("wave-v1", "波段策略 1.0", res)
	return res
}

// pushWaveSignals 把波段命中标的异步推送为买点信号（与低吸独立，复用防重）
func (a *App) pushWaveSignals(items []models.WaveCandidate) {
	if a == nil || a.pushService == nil || len(items) == 0 {
		return
	}
	const maxPush = 5
	batch := items
	if len(batch) > maxPush {
		batch = batch[:maxPush]
	}
	go func() {
		for i, item := range batch {
			if item.Code == "" {
				continue
			}
			msg := fmt.Sprintf("【波段策略1.0】%s · 现价 %.2f · 评分 %.1f · 控盘度 %.0f", item.Level, item.Price, item.Score, item.Kongpan)
			if item.Phase != "" {
				msg += " · " + item.Phase
			}
			if len(item.Reasons) > 0 {
				msg += "\n" + strings.Join(item.Reasons, "；")
			}
			msg += "\n纪律:不追高；GZ转弱/减仓信号/跌破10日线优先处理"
			a.pushService.Push(models.PushSignal{
				StockCode: item.Code, StockName: item.Name,
				Type: models.PushTypeBuyPoint, Message: msg, Level: "active",
			})
			if i < len(batch)-1 {
				time.Sleep(400 * time.Millisecond)
			}
		}
	}()
}

// BackfillAllHistory 回补全A历史行情，days<=0 时默认一年（250 个交易日）
func (a *App) BackfillAllHistory(days int, includeBeijing bool) models.HistoryBackfillResult {
	if a.historyService == nil {
		return models.HistoryBackfillResult{
			Status:     "failed",
			Message:    "历史采集服务未初始化",
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	return a.historyService.BackfillAllHistory(days, includeBeijing)
}

// GetHistoryAutoCollectStatus 获取历史数据自动采集状态
func (a *App) GetHistoryAutoCollectStatus() models.HistoryAutoCollectStatus {
	if a.historyService == nil {
		return models.HistoryAutoCollectStatus{
			Enabled:      false,
			CollectStart: "16:00",
			CollectEnd:   "17:00",
			Message:      "历史采集服务未初始化",
		}
	}
	return a.historyService.GetAutoCollectStatus()
}

// UpdateHistoryAutoCollect 更新历史数据自动采集配置
func (a *App) UpdateHistoryAutoCollect(req models.HistoryAutoCollectRequest) models.HistoryAutoCollectStatus {
	if a.historyService == nil {
		return models.HistoryAutoCollectStatus{
			Enabled:      false,
			CollectStart: "16:00",
			CollectEnd:   "17:00",
			Message:      "历史采集服务未初始化",
		}
	}
	status, err := a.historyService.UpdateAutoCollect(req)
	if err != nil {
		status.Message = "保存自动采集配置失败：" + err.Error()
	}
	return status
}

// RunLowBuyScannerV1 全A低吸选股扫描（V1.1 高胜率短线规则）
func (a *App) RunLowBuyScannerV1(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	historyOpts := normalizeLowBuyHistoryOptions(req)
	// 当日涨幅上限（可自定义，默认 +1.5%；回测最优≈0=只买当天不涨的票）
	maxChangePct := 1.5
	if req.MaxChangePct != nil {
		maxChangePct = *req.MaxChangePct
	}
	historyCheckedCount := 0
	historyFailedCount := 0
	historyRejectedCount := 0

	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "V1.2 高胜率短线规则（全A·回踩偏好·Top3推送）",
		Items:       []models.LowBuyScannerItem{},
		Warning:     "",
	}

	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)

	marketSnap, marketErr := a.marketService.BuildScanMarketSnapshot()
	marketPrimaryFailed := marketErr != nil
	// 同源回退：若大盘统计字段异常，用全A快照反算
	if (marketSnap.TotalAmount <= 0 || (marketSnap.LimitUpCount == 0 && marketSnap.LimitDownCount == 0)) && len(snapshots) > 0 {
		var fallbackAmount float64
		var fallbackLimitUp int
		var fallbackLimitDown int
		for _, row := range snapshots {
			fallbackAmount += row.Amount
			if row.ChangePercent >= 9.8 {
				fallbackLimitUp++
			}
			if row.ChangePercent <= -9.8 {
				fallbackLimitDown++
			}
		}
		// 仅在回退数据有效时覆盖
		if fallbackAmount > 0 {
			marketSnap.TotalAmount = fallbackAmount
		}
		if fallbackLimitUp > 0 || fallbackLimitDown > 0 {
			marketSnap.LimitUpCount = fallbackLimitUp
			marketSnap.LimitDownCount = fallbackLimitDown
		}
	}
	result.MarketOverview = models.LowBuyMarketOverview{
		ShPrice:        marketSnap.ShPrice,
		ShMA20:         marketSnap.ShMA20,
		LimitUpCount:   marketSnap.LimitUpCount,
		LimitDownCount: marketSnap.LimitDownCount,
		TotalAmount:    marketSnap.TotalAmount,
	}
	marketSnapshotReady := isPlausibleIndexPoint(marketSnap.ShPrice) &&
		isPlausibleIndexPoint(marketSnap.ShMA20) &&
		marketSnap.TotalAmount > 0 &&
		(marketSnap.LimitUpCount > 0 || marketSnap.LimitDownCount > 0)
	if marketPrimaryFailed {
		if marketSnapshotReady {
			result.Warning = combineWarnings(result.Warning, "大盘主源波动，已自动回退补齐")
		} else {
			result.Warning = combineWarnings(result.Warning, "大盘快照获取失败，已降级为仅个股规则评分")
		}
	}

	marketReasons := make([]string, 0, 4)
	gateScore := 0
	if isPlausibleIndexPoint(marketSnap.ShPrice) && isPlausibleIndexPoint(marketSnap.ShMA20) && marketSnap.ShPrice > marketSnap.ShMA20 {
		gateScore++
		marketReasons = append(marketReasons, "上证指数站上20日均线")
	} else if isPlausibleIndexPoint(marketSnap.ShPrice) && isPlausibleIndexPoint(marketSnap.ShMA20) {
		marketReasons = append(marketReasons, "上证指数未站上20日均线")
	} else {
		marketReasons = append(marketReasons, "上证指数数据异常（已忽略该子条件）")
	}
	if marketSnap.LimitUpCount > lowBuyGateLimitUpMin {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("涨停家数 %d > %d", marketSnap.LimitUpCount, lowBuyGateLimitUpMin))
	} else {
		marketReasons = append(marketReasons, fmt.Sprintf("涨停家数 %d 未达 > %d", marketSnap.LimitUpCount, lowBuyGateLimitUpMin))
	}
	if marketSnap.LimitDownCount < lowBuyGateLimitDownMax {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("跌停家数 %d < %d", marketSnap.LimitDownCount, lowBuyGateLimitDownMax))
	} else {
		marketReasons = append(marketReasons, fmt.Sprintf("跌停家数 %d 未达 < %d", marketSnap.LimitDownCount, lowBuyGateLimitDownMax))
	}
	// 两市成交额阈值
	if marketSnap.TotalAmount > lowBuyGateAmountMin {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("两市成交额 %.0f 亿 > %.0f 亿", marketSnap.TotalAmount/1e8, lowBuyGateAmountMin/1e8))
	} else if marketSnap.TotalAmount > 0 {
		marketReasons = append(marketReasons, fmt.Sprintf("两市成交额 %.0f 亿 未达 > %.0f 亿", marketSnap.TotalAmount/1e8, lowBuyGateAmountMin/1e8))
	} else {
		marketReasons = append(marketReasons, "两市成交额数据异常（已忽略该子条件）")
	}
	// 按“有效条件数”动态判定，防止上游脏数据导致整关误杀
	validGateCount := 4
	if !isPlausibleIndexPoint(marketSnap.ShPrice) || !isPlausibleIndexPoint(marketSnap.ShMA20) {
		validGateCount--
	}
	if marketSnap.TotalAmount <= 0 {
		validGateCount--
	}
	if validGateCount < 2 {
		validGateCount = 2
	}
	required := 3
	if validGateCount == 3 {
		required = 2
	}
	if validGateCount == 2 {
		required = 2
	}
	result.MarketGatePassed = gateScore >= required
	result.MarketGateReasons = marketReasons

	if marketSnap.TotalAmount <= 0 {
		result.Warning = combineWarnings(result.Warning, "两市成交额统计仍为空，可能为上游盘后异常或接口限流")
	}

	// 补行业映射（来自 embedded stock_basic）
	industryMap := buildIndustryMapFromEmbedded()

	// V1.1 行业黑名单：只剔除低弹性/规避行业，不再要求命中白名单。
	blockIndustryKeywords := []string{"地产", "农业", "林业", "建筑", "零售", "百货"}

	candidates := make([]models.LowBuyScannerItem, 0, 256)
	for _, row := range snapshots {
		industry := industryMap[row.Symbol]
		if industry == "" {
			industry = "未知"
		}

		// 第一层：硬过滤
		if row.Price <= 0 || row.Amount <= 0 {
			continue
		}
		if row.IsST {
			continue
		}
		if isLowBuyBlockedBoard(row.Symbol) {
			continue
		}
		if strings.HasPrefix(strings.ToLower(row.Symbol), "bj") && !req.IncludeBeijing {
			continue
		}

		// 市值过滤（20~100 亿）
		totalCap := row.TotalMarketCap
		if totalCap <= 0 {
			continue
		}
		if totalCap < 20e8 || totalCap > 100e8 {
			continue
		}

		if hitKeyword(industry, blockIndustryKeywords) {
			continue
		}
		// 当日涨幅上限（见函数顶部 maxChangePct）
		if row.ChangePercent > maxChangePct {
			continue
		}
		// 新规则：当日涨跌 >= -3%
		if row.ChangePercent < -3.0 {
			continue
		}
		// 新规则：当日换手率 <= 8%
		if row.TurnoverRate > 8.0 {
			continue
		}

		// 触发逻辑：4选3
		triggers := make([]string, 0, 4)
		reasons := make([]string, 0, 8)
		riskFlags := make([]string, 0, 4)

		// 主力硬过滤：真实源 + 强度 >= 1% + 主力净流入 >= 800万
		hasMainFlow := !math.IsNaN(row.MainNetInflow) && !math.IsNaN(row.MainNetInflowRatio)
		isRealMainFlowSource := row.MainFlowSource == "eastmoney" || row.MainFlowSource == "tencent-fundflow"
		if !hasMainFlow || !isRealMainFlowSource {
			continue
		}
		if row.MainNetInflowRatio < 1.0 {
			continue
		}
		if row.MainNetInflow < 8e6 {
			continue
		}

		// 触发信号（基础）：4选3
		// 1) 当日换手率 <= 2.5%
		if row.TurnoverRate > 0 && row.TurnoverRate <= 2.5 {
			triggers = append(triggers, "换手收敛(<=2.5%)")
		}
		// 2) 主力净流入为正
		if row.MainNetInflow > 0 {
			switch row.MainFlowSource {
			case "eastmoney", "tencent-fundflow":
				triggers = append(triggers, "主力净流入为正")
			default:
				triggers = append(triggers, "主力代理资金为正")
			}
		}
		// 3) 主力强度 >= 3%
		if row.MainNetInflowRatio >= 3.0 {
			triggers = append(triggers, "主力强度>=3%")
		}
		// 4) 当日涨跌 >= -2%
		if row.ChangePercent >= -2.0 {
			triggers = append(triggers, "当日涨跌>=-2%")
		}

		if len(triggers) < 3 {
			continue
		}

		if historyOpts.Enabled {
			history, err := a.loadLowBuyStockHistory(row, historyOpts)
			if err != nil {
				historyFailedCount++
				continue
			}
			historyCheckedCount++
			historyTriggers, historyReasons, historyRisks, passed := evaluateLowBuyHistory(history, historyOpts)
			if !passed {
				historyRejectedCount++
				continue
			}
			triggers = append(triggers, historyTriggers...)
			reasons = append(reasons, historyReasons...)
			riskFlags = append(riskFlags, historyRisks...)
		}

		// 评分
		// 基础分 50 + 资金分 + 换手分 + 市值分 + 强弱分
		score := 50.0
		flowRatio := row.MainNetInflowRatio
		turnover := row.TurnoverRate
		chg := row.ChangePercent

		if hasMainFlow {
			score += clamp(flowRatio*3.0, -8, 14)
		}
		score += clamp((3.0-turnover)*3.0, -6, 10)
		// 市值分层
		switch {
		case totalCap <= 40e8:
			score += 12
		case totalCap <= 60e8:
			score += 10
		case totalCap <= 80e8:
			score += 7
		default:
			score += 4
		}
		// 涨跌结构分（回测验证：强烈偏好"微跌回踩"[-1,0)，压低"已翻红"[0,1)；这是逐年最稳健的维度）
		switch {
		case chg >= -1.0 && chg < 0:
			score += 12 // 微跌回踩——甜区，名次置顶
		case chg >= -2.0 && chg < -1.0:
			score += 4 // 跌得稍深，次优
		case chg >= 0 && chg < 1.0:
			score -= 2 // 已翻红——回测最差档，压低名次
		case chg >= 1.0:
			score -= 5 // 已明显上涨，最不该低吸
		}
		// 主力金额分
		if row.MainNetInflow >= 2e7 {
			score += 8
		} else if row.MainNetInflow >= 1e7 {
			score += 5
		} else if row.MainNetInflow >= 8e6 {
			score += 2
		}
		if turnover > 6 {
			score -= 4
			riskFlags = append(riskFlags, "换手偏高(>6%)")
		} else if turnover > 4 {
			score -= 2
			riskFlags = append(riskFlags, "换手偏高(>4%)")
		}
		if hasMainFlow && flowRatio < 0 {
			switch row.MainFlowSource {
			case "eastmoney", "tencent-fundflow":
				riskFlags = append(riskFlags, "主力资金流出")
			default:
				riskFlags = append(riskFlags, "主力代理流出")
			}
		}
		score = math.Round(clamp(score, 0, 100)*10) / 10

		reasons = append(reasons, fmt.Sprintf("触发信号：%s", strings.Join(triggers, " / ")))
		reasons = append(reasons, fmt.Sprintf("总市值 %.1f 亿，流通市值 %.1f 亿", totalCap/1e8, row.FloatMarketCap/1e8))
		if hasMainFlow {
			switch row.MainFlowSource {
			case "eastmoney":
				reasons = append(reasons, fmt.Sprintf("东财主力净流入 %.2f 亿，主力净占比 %.2f%%", row.MainNetInflow/1e8, flowRatio))
			case "tencent-fundflow":
				reasons = append(reasons, fmt.Sprintf("腾讯主力净流入 %.2f 亿，主力强弱 %.2f%%", row.MainNetInflow/1e8, flowRatio))
			default:
				reasons = append(reasons, fmt.Sprintf("主力代理净流入 %.2f 亿，主力代理强弱 %.2f%%", row.MainNetInflow/1e8, flowRatio))
			}
		} else {
			reasons = append(reasons, "主力净流入：当前数据源不可用（非0值，属缺失）")
		}
		reasons = append(reasons, fmt.Sprintf("换手率 %.2f%%，当日涨跌 %.2f%%，主力净流入门槛 >= 0.08 亿", turnover, chg))

		capBucket := classifyCapBucket(totalCap)

		item := models.LowBuyScannerItem{
			Symbol:             row.Symbol,
			Name:               row.Name,
			Price:              row.Price,
			ChangePercent:      chg,
			Amount:             row.Amount,
			TurnoverRate:       turnover,
			MainNetInflow:      row.MainNetInflow,
			MainNetInflowRatio: flowRatio,
			MainFlowSource:     row.MainFlowSource,
			TotalMarketCap:     totalCap,
			FloatMarketCap:     row.FloatMarketCap,
			CapBucket:          capBucket,
			Industry:           industry,
			Score:              score,
			TriggerCount:       len(triggers),
			Triggers:           triggers,
			Reasons:            reasons,
			RiskFlags:          riskFlags,
			BuyPointHint:       "尾盘14:30-15:00分批，优先回踩不破均线结构",
			SellPointHint:      "3日累计+15%减半；单日换手>12%先走；持仓5日涨幅<3%清仓",
			StopLossHint:       "跌破买入价-5%无条件止损；跌破10日线次日不收回离场",
			UpdatedAt:          chooseFirstNonEmpty(row.UpdateTime, result.AsOf),
		}
		candidates = append(candidates, item)
	}

	result.CandidateCount = len(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].TriggerCount == candidates[j].TriggerCount {
				return candidates[i].MainNetInflow > candidates[j].MainNetInflow
			}
			return candidates[i].TriggerCount > candidates[j].TriggerCount
		}
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	// 新规则3：单行业候选 <= 3 只
	industryCounts := make(map[string]int, 32)
	filtered := make([]models.LowBuyScannerItem, 0, len(candidates))
	for _, item := range candidates {
		ind := strings.TrimSpace(item.Industry)
		if ind == "" {
			ind = "未知"
		}
		if industryCounts[ind] >= 3 {
			continue
		}
		industryCounts[ind]++
		filtered = append(filtered, item)
	}
	candidates = filtered

	// 为最终展示的候选并发计算 MA10 状态（站上/跌破），作界面直观提示（仅展示，不淘汰）
	a.fillLowBuyMA10Status(candidates)

	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf("已启用V1.1筛选：剔除300/688开头、行业黑名单、涨幅[-3%%,%.1f%%]、换手<=8%%、主力强度>=1%%且净流入>=800万（真实源）、单行业最多3只", maxChangePct))
	if historyOpts.Enabled {
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf(
			"已追加历史维度：连续%d日换手<%.2f%%、近%d日主力至少%d日净流入为正、站上MA%d；已验证%d只，剔除%d只，历史质量数据拉取失败%d只",
			historyOpts.TurnoverDays,
			historyOpts.TurnoverMax,
			historyOpts.MainFlowDays,
			historyOpts.MainFlowPositiveDays,
			historyOpts.MAPeriod,
			historyCheckedCount,
			historyRejectedCount,
			historyFailedCount,
		))
	}
	if hasMainFlowMissing(candidates) {
		result.Warning = combineWarnings(result.Warning, "主力资金字段存在缺失，当前以量价与市值规则降级评分")
	} else if usedMainFlowProxy(candidates) {
		result.Warning = combineWarnings(result.Warning, "主力数据部分来自腾讯盘口强弱代理（非东财主力净流入原值）")
	}
	if usedQQFundFlow(candidates) {
		result.Warning = combineWarnings(result.Warning, "主力数据已补齐腾讯资金流接口（真实主力净流入）")
	}

	a.saveLowBuyStrategyPicks("lowbuy-v1", "低吸选股策略1", result)
	// 扫描入选标的异步推送为买点信号（复用 24h 防重，推送未开启时内部直接跳过）
	a.pushScannerSignals(result.Items)

	return result
}

// RunLimitPullbackScanner 涨停回调低吸：先找近期涨停强启动，再等缩量回踩和均线承接。
func (a *App) RunLimitPullbackScanner(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "涨停回调低吸 V1.1（趋势闸门 + 近期涨停强启动 + 缩量洗盘 + 站稳5/10日线）",
		Items:       []models.LowBuyScannerItem{},
		Warning:     "",
	}

	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)

	marketSnap, marketErr := a.marketService.BuildScanMarketSnapshot()
	if marketErr != nil {
		result.Warning = combineWarnings(result.Warning, "大盘快照获取失败，已降级为仅个股涨停回调结构评分")
	}
	if (marketSnap.TotalAmount <= 0 || (marketSnap.LimitUpCount == 0 && marketSnap.LimitDownCount == 0)) && len(snapshots) > 0 {
		var fallbackAmount float64
		var fallbackLimitUp int
		var fallbackLimitDown int
		for _, row := range snapshots {
			fallbackAmount += row.Amount
			if row.ChangePercent >= 9.8 {
				fallbackLimitUp++
			}
			if row.ChangePercent <= -9.8 {
				fallbackLimitDown++
			}
		}
		if fallbackAmount > 0 {
			marketSnap.TotalAmount = fallbackAmount
		}
		if fallbackLimitUp > 0 || fallbackLimitDown > 0 {
			marketSnap.LimitUpCount = fallbackLimitUp
			marketSnap.LimitDownCount = fallbackLimitDown
		}
	}
	result.MarketOverview = models.LowBuyMarketOverview{
		ShPrice:        marketSnap.ShPrice,
		ShMA20:         marketSnap.ShMA20,
		LimitUpCount:   marketSnap.LimitUpCount,
		LimitDownCount: marketSnap.LimitDownCount,
		TotalAmount:    marketSnap.TotalAmount,
	}

	marketReasons := make([]string, 0, 4)
	gateScore := 0
	if isPlausibleIndexPoint(marketSnap.ShPrice) && isPlausibleIndexPoint(marketSnap.ShMA20) && marketSnap.ShPrice > marketSnap.ShMA20 {
		gateScore++
		marketReasons = append(marketReasons, "上证指数站上20日均线")
	} else if isPlausibleIndexPoint(marketSnap.ShPrice) && isPlausibleIndexPoint(marketSnap.ShMA20) {
		marketReasons = append(marketReasons, "上证指数未站上20日均线")
	} else {
		marketReasons = append(marketReasons, "上证指数数据异常（已忽略该子条件）")
	}
	if marketSnap.LimitUpCount > 30 {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("涨停家数 %d > 30，短线情绪可用", marketSnap.LimitUpCount))
	} else {
		marketReasons = append(marketReasons, fmt.Sprintf("涨停家数 %d 未达 > 30", marketSnap.LimitUpCount))
	}
	if marketSnap.LimitDownCount < lowBuyGateLimitDownMax {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("跌停家数 %d < %d", marketSnap.LimitDownCount, lowBuyGateLimitDownMax))
	} else {
		marketReasons = append(marketReasons, fmt.Sprintf("跌停家数 %d 未达 < %d", marketSnap.LimitDownCount, lowBuyGateLimitDownMax))
	}
	if marketSnap.TotalAmount > 8e11 {
		gateScore++
		marketReasons = append(marketReasons, fmt.Sprintf("两市成交额 %.0f 亿 > 8000 亿", marketSnap.TotalAmount/1e8))
	} else if marketSnap.TotalAmount > 0 {
		marketReasons = append(marketReasons, fmt.Sprintf("两市成交额 %.0f 亿 未达 > 8000 亿", marketSnap.TotalAmount/1e8))
	} else {
		marketReasons = append(marketReasons, "两市成交额数据异常（已忽略该子条件）")
	}
	result.MarketGatePassed = gateScore >= 3
	result.MarketGateReasons = marketReasons

	industryMap := buildIndustryMapFromEmbedded()
	candidates := make([]models.LowBuyScannerItem, 0, 128)
	checkedDaily := 0
	dailyFailed := 0

	for _, row := range snapshots {
		if row.Price <= 0 || row.Amount <= 0 || row.IsST {
			continue
		}
		if strings.HasPrefix(strings.ToLower(row.Symbol), "bj") && !req.IncludeBeijing {
			continue
		}
		// 图中模型偏强势启动后的回踩，先剔除连续加速和高波动板，主战场放在沪深主板。
		if isLimitPullbackBlockedBoard(row.Symbol) {
			continue
		}
		if row.ChangePercent > 5.5 || row.ChangePercent < -5.0 {
			continue
		}
		if row.TurnoverRate > 14 {
			continue
		}

		daily, err := a.marketService.GetKLineData(row.Symbol, "1d", 72)
		if err != nil || len(daily) < 65 {
			dailyFailed++
			continue
		}
		checkedDaily++

		industry := industryMap[row.Symbol]
		if industry == "" {
			industry = row.Industry
		}
		if industry == "" {
			industry = "未知"
		}

		item, ok := evaluateLimitPullbackRow(row, industry, daily, result.AsOf)
		if !ok {
			continue
		}
		candidates = append(candidates, item)
	}

	result.CandidateCount = len(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].TriggerCount == candidates[j].TriggerCount {
				return candidates[i].Amount > candidates[j].Amount
			}
			return candidates[i].TriggerCount > candidates[j].TriggerCount
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf("涨停回调低吸：已加入趋势闸门，剔除震荡下跌/空头排列中的涨停反抽；近2-8日涨停/准涨停，启动日放量，涨停后缩量回调且收盘站稳5/10日线；已验证日K%d只，日K失败%d只", checkedDaily, dailyFailed))
	a.saveLowBuyStrategyPicks("limit-pullback-v1", "涨停回调低吸4", result)
	return result
}

// RunTripleVolumeScannerV5 三倍量策略5：未涨停阳线 + 成交量>=前一日3倍 + 一阳穿5/10/20/30日线。
func (a *App) RunTripleVolumeScannerV5(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "三倍量策略5（主板10cm口径：未涨停阳线 + 成交量>=前一日3倍 + 一阳穿MA5/10/20/30）",
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}

	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)
	if marketSnap, marketErr := a.marketService.BuildScanMarketSnapshot(); marketErr == nil {
		result.MarketOverview = models.LowBuyMarketOverview{
			ShPrice:        marketSnap.ShPrice,
			ShMA20:         marketSnap.ShMA20,
			LimitUpCount:   marketSnap.LimitUpCount,
			LimitDownCount: marketSnap.LimitDownCount,
			TotalAmount:    marketSnap.TotalAmount,
		}
	}

	industryMap := buildIndustryMapFromEmbedded()
	candidates := make([]models.LowBuyScannerItem, 0, 128)
	checkedDaily := 0
	dailyFailed := 0

	for _, row := range snapshots {
		if row.Price <= 0 || row.Amount <= 0 || row.IsST {
			continue
		}
		if strings.HasPrefix(strings.ToLower(row.Symbol), "bj") && !req.IncludeBeijing {
			continue
		}
		if isTripleVolumeBlockedBoard(row.Symbol) {
			continue
		}

		daily, derr := a.marketService.GetKLineData(row.Symbol, "1d", 45)
		if derr != nil || len(daily) < 31 {
			dailyFailed++
			continue
		}
		checkedDaily++

		industry := chooseFirstNonEmpty(industryMap[row.Symbol], row.Industry, "未知")
		item, ok := evaluateTripleVolumeRow(row, industry, daily, result.AsOf)
		if !ok {
			continue
		}
		candidates = append(candidates, item)
	}

	result.CandidateCount = len(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].TriggerCount == candidates[j].TriggerCount {
				return candidates[i].Amount > candidates[j].Amount
			}
			return candidates[i].TriggerCount > candidates[j].TriggerCount
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf("三倍量策略5：主板10cm口径，剔除ST/北交/创业板/科创板；日K验证%d只，日K失败%d只。买点不追当天突破，重点看次日缩量回调不破突破成本线", checkedDaily, dailyFailed))
	a.saveLowBuyStrategyPicks("triple-volume-v5", "三倍量策略5", result)
	return result
}

// RunTailBuyScannerV6 尾盘买入策略6：昨日强势资金触发，今日阴线回踩，尾盘观察低吸。
func (a *App) RunTailBuyScannerV6(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runFormulaScanner(req, formulaScannerSpec{
		ID:          "tail-buy-v6",
		Name:        "尾盘买入策略6",
		RuleVersion: "尾盘买入策略6（昨日资金强势触发 + 今日阴线回踩，尾盘确认承接）",
		KLineDays:   280,
		Warning:     "尾盘买入策略6：按通达信公式本地复刻，CAPITAL 用流通市值/价格估算，主板口径自动剔除ST/北交/300/301/688/689",
		Evaluate:    evaluateTailBuyV6Row,
	})
}

// RunHotMoneyBreakoutScannerV7 游资突破策略7：缩量/放量涨停结构 + 分时量能强弱代理 + 流通股本约束。
func (a *App) RunHotMoneyBreakoutScannerV7(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runFormulaScanner(req, formulaScannerSpec{
		ID:          "hot-money-v7",
		Name:        "游资突破策略7",
		RuleVersion: "游资突破策略7（游资涨停结构 + 量能倍率1-5 + 流通股本分档）",
		KLineDays:   48,
		Warning:     "游资突破策略7：DYNAINFO(58) 用流通股本(万股)代理；按主板10cm涨停结构执行，自动剔除ST/北交/300/301/688/689",
		Evaluate:    evaluateHotMoneyV7Row,
	})
}

// RunDipEntryScannerV8 低吸入场策略8：三类短线反转信号至少2类共振。
func (a *App) RunDipEntryScannerV8(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runFormulaScanner(req, formulaScannerSpec{
		ID:          "dip-entry-v8",
		Name:        "低吸入场策略8",
		RuleVersion: "低吸入场策略8（RSI短线反转 + 快速RSI过线 + 动能底背离，三选二）",
		KLineDays:   96,
		Warning:     "低吸入场策略8：按通达信SMA/EMA逻辑复刻，自动剔除ST/北交/300/301/688/689；信号是反转入场，不等同于追涨突破",
		Evaluate:    evaluateDipEntryV8Row,
	})
}

// RunMonsterScannerV9 捉妖策略9：原“捉妖选股”公式的代理复刻版，保留之前可出票逻辑并压低低点反抽泛滥。
func (a *App) RunMonsterScannerV9(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runFormulaScanner(req, formulaScannerSpec{
		ID:          "monster-v9",
		Name:        "捉妖策略9",
		RuleVersion: "捉妖策略9（代理修正版：放量突破前高 + 突破后第2日确认 + 精选60日低点反抽）",
		KLineDays:   320,
		Warning:     "捉妖策略9：恢复此前可出票的代理复刻逻辑；60日低点反抽加成交额/换手/跌幅阀门防止泛滥；自动剔除ST/北交/300/301/688/689",
		Evaluate:    evaluateMonsterV9Row,
	})
}

// RunMonsterScannerV10 捉妖策略10：严格按用户提供的通达信“捉妖选股”公式本地复刻。
func (a *App) RunMonsterScannerV10(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runFormulaScanner(req, formulaScannerSpec{
		ID:          "monster-v10",
		Name:        "捉妖策略10",
		RuleVersion: "捉妖策略10（通达信公式严格复刻：GGZY_ZS=FILTER(GGZY_IG=1,3)）",
		KLineDays:   360,
		Warning:     "捉妖策略10：按通达信公式逐项复刻；MACD.GGZY_A8按标准MACD柱承接，BOLL.UB为20日布林上轨，CAPITAL用流通市值/价格估算；自动剔除ST/北交/300/301/688/689",
		Evaluate:    evaluateMonsterV10Row,
	})
}

type formulaScannerSpec struct {
	ID          string
	Name        string
	RuleVersion string
	KLineDays   int
	Warning     string
	Evaluate    func(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool)
}

func (a *App) runFormulaScanner(req models.LowBuyScannerRequest, spec formulaScannerSpec) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	if spec.KLineDays <= 0 {
		spec.KLineDays = 120
	}

	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: spec.RuleVersion,
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}

	historyPickDate := strings.TrimSpace(req.HistoryPickDate)
	useHistoryPick := spec.ID == "monster-v9" && historyPickDate != ""
	var snapshots []services.ScanSnapshotRow
	var historyAsOf string
	var err error
	if useHistoryPick {
		if a.historyService == nil {
			result.Warning = "历史库未初始化，无法进行历史时间选股"
			return result
		}
		snapshots, historyAsOf, err = a.historyService.LoadScanRowsOnDate(historyPickDate, req.IncludeBeijing)
		if err != nil {
			result.Warning = "历史时间选股读取失败：" + err.Error()
			return result
		}
		result.AsOf = historyAsOf
		result.RuleVersion = result.RuleVersion + " · 历史时间选股 " + historyAsOf
	} else {
		snapshots, err = a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
		if err != nil {
			result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
			return result
		}
	}
	result.UniverseCount = len(snapshots)
	if !useHistoryPick {
		if marketSnap, marketErr := a.marketService.BuildScanMarketSnapshot(); marketErr == nil {
			result.MarketOverview = models.LowBuyMarketOverview{
				ShPrice:        marketSnap.ShPrice,
				ShMA20:         marketSnap.ShMA20,
				LimitUpCount:   marketSnap.LimitUpCount,
				LimitDownCount: marketSnap.LimitDownCount,
				TotalAmount:    marketSnap.TotalAmount,
			}
		}
	} else {
		result.MarketOverview = models.LowBuyMarketOverview{
			TotalAmount: sumSnapshotAmount(snapshots),
		}
	}

	industryMap := buildIndustryMapFromEmbedded()
	candidates := make([]models.LowBuyScannerItem, 0, 128)
	checkedDaily := 0
	dailyFailed := 0
	// 并行逐股拉K线评估：原串行~5000次请求要数分钟，并发压到几分钟内完成（2:30 窗口内跑得完）。
	// 控制并发度避免打爆行情源。
	const fscanWorkers = 28
	var mu sync.Mutex
	jobs := make(chan services.ScanSnapshotRow, fscanWorkers*2)
	var wg sync.WaitGroup
	for w := 0; w < fscanWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if row.Price <= 0 || row.Amount <= 0 || row.IsST {
					continue
				}
				if isFormulaScannerBlockedBoard(row.Symbol) {
					continue
				}
				var daily []models.KLineData
				var derr error
				if useHistoryPick {
					daily, derr = a.historyService.LoadKLineDataUntil(row.Symbol, historyAsOf, spec.KLineDays)
				} else {
					daily, derr = a.marketService.GetKLineData(row.Symbol, "1d", spec.KLineDays)
				}
				if derr != nil || len(daily) < minInt(spec.KLineDays/2, 30) {
					mu.Lock()
					dailyFailed++
					mu.Unlock()
					continue
				}
				industry := chooseFirstNonEmpty(industryMap[row.Symbol], row.Industry, "未知")
				item, ok := spec.Evaluate(row, industry, daily, result.AsOf)
				mu.Lock()
				checkedDaily++
				if ok {
					candidates = append(candidates, item)
				}
				mu.Unlock()
			}
		}()
	}
	for _, row := range snapshots {
		jobs <- row
	}
	close(jobs)
	wg.Wait()

	result.CandidateCount = len(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].TriggerCount == candidates[j].TriggerCount {
				return candidates[i].Amount > candidates[j].Amount
			}
			return candidates[i].TriggerCount > candidates[j].TriggerCount
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result.Items = candidates
	result.SelectedCount = len(candidates)
	if useHistoryPick {
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf("%s；历史时间选股=%s，仅使用%s及以前数据；日K验证%d只，日K失败%d只", spec.Warning, historyPickDate, historyAsOf, checkedDaily, dailyFailed))
	} else {
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf("%s；日K验证%d只，日K失败%d只", spec.Warning, checkedDaily, dailyFailed))
	}
	a.saveLowBuyStrategyPicks(spec.ID, spec.Name, result)
	return result
}

func sumSnapshotAmount(rows []services.ScanSnapshotRow) float64 {
	total := 0.0
	for _, row := range rows {
		if row.Amount > 0 {
			total += row.Amount
		}
	}
	return total
}

// RunCaoYuanStandardScanner4A 草元标准4A：normal接口结果反推的“深度超跌 + 贴近地板 + 当日止跌”策略。
func (a *App) RunCaoYuanStandardScanner4A(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runCaoYuanScanner(req, "standard4a")
}

// RunCaoYuanZhuangScanner4B 草元抓庄4B：ZZ接口结果反推的“90日涨停记忆 + 深跌企稳 + 控盘代理”策略。
func (a *App) RunCaoYuanZhuangScanner4B(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	return a.runCaoYuanScanner(req, "zhuang4b")
}

func (a *App) runCaoYuanScanner(req models.LowBuyScannerRequest, mode string) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	result := models.LowBuyScannerResult{
		AsOf:  start.Format("2006-01-02 15:04:05"),
		Items: []models.LowBuyScannerItem{},
	}
	switch mode {
	case "zhuang4b":
		if req.CaoYuanStrict {
			result.RuleVersion = "草元抓庄4B·严选（ZZ严格档反推：90日涨停记忆 + 更深跌幅 + 高控盘代理强确认）"
		} else {
			result.RuleVersion = "草元抓庄4B·标准（ZZ反推：90日涨停记忆 + 深跌企稳 + 高控盘代理）"
		}
	default:
		if req.CaoYuanStrict {
			result.RuleVersion = "草元标准4A·严选（normal严格档反推：深度超跌 + 放量翻红 + 强拐点确认）"
		} else {
			result.RuleVersion = "草元标准4A·标准（normal反推：深度超跌 + 贴近地板 + 当日止跌）"
		}
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}

	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)
	industryMap := buildIndustryMapFromEmbedded()

	rough := make([]services.ScanSnapshotRow, 0, 512)
	for _, row := range snapshots {
		if row.Price <= 0 || row.Amount <= 0 || row.IsST {
			continue
		}
		if !isCaoYuanAllowedBoard(row.Symbol) {
			continue
		}
		if row.TotalMarketCap < 20e8 || row.TotalMarketCap > 100e8 || row.FloatMarketCap < 20e8 || row.FloatMarketCap > 100e8 {
			continue
		}
		if mode == "zhuang4b" {
			if row.ChangePercent < 0 || row.TurnoverRate <= 0 || row.TurnoverRate > 8 || row.VolumeRatio <= 0 || row.VolumeRatio > 2.5 {
				continue
			}
		} else if req.CaoYuanStrict {
			if row.ChangePercent < 0.2 || row.TurnoverRate <= 0 || row.TurnoverRate > 8 || row.VolumeRatio <= 0 || row.VolumeRatio > 3.2 {
				continue
			}
		} else {
			if row.ChangePercent < 0 || row.TurnoverRate <= 0 || row.TurnoverRate > 3 || row.VolumeRatio <= 0 || row.VolumeRatio > 1.5 {
				continue
			}
		}
		rough = append(rough, row)
	}

	type caoYuanEvalResult struct {
		item models.LowBuyScannerItem
		ok   bool
		err  error
	}
	jobs := make(chan services.ScanSnapshotRow)
	out := make(chan caoYuanEvalResult)
	workerCount := 8
	if len(rough) < workerCount {
		workerCount = len(rough)
	}
	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				industry := chooseFirstNonEmpty(row.Industry, industryMap[row.Symbol], "未知")
				item, ok, evalErr := a.evaluateCaoYuanRow(row, industry, mode, req.CaoYuanStrict, result.AsOf)
				out <- caoYuanEvalResult{item: item, ok: ok, err: evalErr}
			}
		}()
	}
	go func() {
		for _, row := range rough {
			jobs <- row
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()

	klineFailedCount := 0
	candidates := make([]models.LowBuyScannerItem, 0, len(rough))
	for r := range out {
		if r.err != nil {
			klineFailedCount++
			continue
		}
		if r.ok {
			candidates = append(candidates, r.item)
		}
	}
	result.CandidateCount = len(candidates)

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].TriggerCount == candidates[j].TriggerCount {
				if candidates[i].FloatMarketCap == candidates[j].FloatMarketCap {
					return candidates[i].ChangePercent < candidates[j].ChangePercent
				}
				return candidates[i].FloatMarketCap < candidates[j].FloatMarketCap
			}
			return candidates[i].TriggerCount > candidates[j].TriggerCount
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result.Items = candidates
	result.SelectedCount = len(candidates)

	if mode == "zhuang4b" {
		level := "标准"
		if req.CaoYuanStrict {
			level = "严选"
		}
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf("4B为独立抓庄规则（%s层级）：预筛%d只，日K验证入选%d只；90日涨停记忆由本地日K真实推导，高控盘为本地代理，不等同草元原站 is_gao_kong 原值", level, len(rough), result.SelectedCount))
	} else {
		level := "标准"
		if req.CaoYuanStrict {
			level = "严选"
		}
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf("4A为独立标准规则（%s层级）：预筛%d只，日K验证入选%d只；基于草元normal结果反推，不使用草元原站账号接口或抓包token", level, len(rough), result.SelectedCount))
	}
	if klineFailedCount > 0 {
		result.Warning = combineWarnings(result.Warning, fmt.Sprintf("日K拉取失败/不足%d只，已跳过", klineFailedCount))
	}
	strategyID := "caoyuan-standard4a"
	strategyName := "草元标准 4A"
	if req.CaoYuanStrict {
		strategyID = "caoyuan-standard4a-strict"
		strategyName = "草元标准 4A 严选"
	}
	if mode == "zhuang4b" {
		strategyID = "caoyuan-zhuang4b"
		strategyName = "草元抓庄 4B"
		if req.CaoYuanStrict {
			strategyID = "caoyuan-zhuang4b-strict"
			strategyName = "草元抓庄 4B 严选"
		}
	}
	a.saveLowBuyStrategyPicks(strategyID, strategyName, result)
	return result
}

type caoYuanKMetrics struct {
	Close          float64
	Pct5           float64
	Pct10          float64
	Pct20          float64
	Pct30          float64
	MA5            float64
	MA10           float64
	MA20           float64
	Low20          float64
	High60         float64
	DistLow20      float64
	Drawdown60     float64
	Has90dLimitUp  bool
	GaoKongProxy   bool
	BelowAllMA     bool
	RecentSwingPct float64
}

func (a *App) evaluateCaoYuanRow(row services.ScanSnapshotRow, industry string, mode string, strict bool, asOf string) (models.LowBuyScannerItem, bool, error) {
	daily, err := a.marketService.GetKLineData(row.Symbol, "1d", 125)
	if err != nil {
		return models.LowBuyScannerItem{}, false, err
	}
	metrics, err := buildCaoYuanMetrics(row, daily)
	if err != nil {
		return models.LowBuyScannerItem{}, false, err
	}

	if mode == "zhuang4b" {
		if metrics.Pct10 > -3.0 || metrics.Pct20 > -10.0 || metrics.Close >= metrics.MA20 || metrics.DistLow20 > 10 || !metrics.Has90dLimitUp {
			return models.LowBuyScannerItem{}, false, nil
		}
		if strict && !passesCaoYuanZhuangStrict(row, metrics) {
			return models.LowBuyScannerItem{}, false, nil
		}
	} else {
		if metrics.Pct10 > -1.5 || metrics.Pct20 > -8.0 || metrics.Close >= metrics.MA20 || metrics.DistLow20 > 8 {
			return models.LowBuyScannerItem{}, false, nil
		}
		if strict && !passesCaoYuanStandardStrict(row, metrics) {
			return models.LowBuyScannerItem{}, false, nil
		}
	}

	mainNet := safeFiniteFloat(row.MainNetInflow)
	mainRatio := safeFiniteFloat(row.MainNetInflowRatio)
	mainSource := row.MainFlowSource
	if strings.TrimSpace(mainSource) == "" || (mainNet == 0 && mainRatio == 0) {
		mainSource = "caoyuan-not-required"
	}

	var score float64
	var triggers []string
	var reasons []string
	var risks []string
	var buyHint string
	var sellHint string
	var stopHint string
	if mode == "zhuang4b" {
		score, triggers, reasons, risks = scoreCaoYuanZhuang4B(row, metrics, strict)
		buyHint = "尾盘14:30-15:00观察承接，优先等缩量不破当日均价/前低后小仓试错"
		sellHint = "反抽到MA10/MA20附近分批止盈；放量滞涨、长上影或涨停记忆兑现后减仓"
		stopHint = "跌破近20日低点或买入价-5%离场；次日不能修复弱势则不恋战"
	} else {
		score, triggers, reasons, risks = scoreCaoYuanStandard4A(row, metrics, strict)
		buyHint = "尾盘14:30-15:00确认不再破低后分批，定位超跌止跌低吸"
		sellHint = "反抽3-8%先落袋一半；触及MA10/MA20或放量滞涨先走"
		stopHint = "跌破近20日低点或买入价-5%无条件止损；次日不修复减仓"
	}

	levelLabel := "标准"
	if strict {
		levelLabel = "严选"
	}
	reasons = append([]string{
		fmt.Sprintf("草元%s·%s反推：总市值%.1f亿，流通市值%.1f亿，板块允许，ST/科创/北交已剔除", map[bool]string{true: "抓庄4B", false: "标准4A"}[mode == "zhuang4b"], levelLabel, row.TotalMarketCap/1e8, row.FloatMarketCap/1e8),
		fmt.Sprintf("涨跌结构：当日%+.2f%%，5日%+.2f%%，10日%+.2f%%，20日%+.2f%%，30日%+.2f%%", row.ChangePercent, metrics.Pct5, metrics.Pct10, metrics.Pct20, metrics.Pct30),
		fmt.Sprintf("地板距离：距20日低点%.2f%%，距60日高点%+.2f%%，MA5/10/20=%.2f/%.2f/%.2f", metrics.DistLow20, metrics.Drawdown60, metrics.MA5, metrics.MA10, metrics.MA20),
		fmt.Sprintf("量能：换手%.2f%%，量比%.2f，成交额%s", row.TurnoverRate, row.VolumeRatio, formatAmountCN(row.Amount)),
	}, reasons...)
	if mode == "zhuang4b" {
		if metrics.GaoKongProxy {
			reasons = append(reasons, "高控盘代理：90日涨停记忆 + 深回撤 + 贴近低点 + 缩量，疑似主力低位控盘/吸筹结构")
		} else {
			reasons = append(reasons, "高控盘代理未完全满足：仅保留90日涨停记忆与深跌企稳，不冒充原站is_gao_kong真值")
		}
	}
	if row.MainFlowSource == "eastmoney" || row.MainFlowSource == "tencent-fundflow" {
		reasons = append(reasons, fmt.Sprintf("资金补充：%s 主力净流入%s，净占比%.2f%%（草元策略不把主力流入作为硬门槛）", row.MainFlowSource, formatAmountCN(mainNet), mainRatio))
	} else {
		reasons = append(reasons, "资金补充：主力净流入非本策略硬门槛；缺失时不以0冒充真实资金")
	}

	ma10Status := "broke"
	if metrics.Close >= metrics.MA10 {
		ma10Status = "hold"
	}
	return models.LowBuyScannerItem{
		Symbol:             row.Symbol,
		Name:               row.Name,
		Price:              row.Price,
		ChangePercent:      row.ChangePercent,
		Amount:             row.Amount,
		TurnoverRate:       row.TurnoverRate,
		MainNetInflow:      mainNet,
		MainNetInflowRatio: mainRatio,
		MainFlowSource:     mainSource,
		TotalMarketCap:     row.TotalMarketCap,
		FloatMarketCap:     row.FloatMarketCap,
		CapBucket:          classifyCapBucket(row.TotalMarketCap),
		Industry:           industry,
		Score:              score,
		TriggerCount:       len(triggers),
		Triggers:           triggers,
		Reasons:            reasons,
		RiskFlags:          risks,
		BuyPointHint:       buyHint,
		SellPointHint:      sellHint,
		StopLossHint:       stopHint,
		MA10:               metrics.MA10,
		MA10Status:         ma10Status,
		UpdatedAt:          chooseFirstNonEmpty(row.UpdateTime, asOf),
	}, true, nil
}

func passesCaoYuanStandardStrict(row services.ScanSnapshotRow, m caoYuanKMetrics) bool {
	// 草元 normal 严格档(is_new_strict_b=1)样本特征：当日翻红更明显、量能放大、30日跌幅更深。
	return row.ChangePercent >= 0.2 &&
		m.Pct10 <= -3.0 &&
		m.Pct20 <= -12.0 &&
		m.Pct30 <= -14.0 &&
		row.TurnoverRate >= 0.8 &&
		row.TurnoverRate <= 8.0 &&
		row.VolumeRatio >= 0.8 &&
		row.VolumeRatio <= 3.2
}

func passesCaoYuanZhuangStrict(row services.ScanSnapshotRow, m caoYuanKMetrics) bool {
	// 草元 ZZ 严格档样本更少：保留90日涨停记忆，并要求深跌、翻红、控盘代理更强。
	return row.ChangePercent >= 0.5 &&
		m.Pct10 <= -3.0 &&
		m.Pct20 <= -14.0 &&
		m.Pct30 <= -14.0 &&
		row.TurnoverRate >= 0.8 &&
		row.TurnoverRate <= 8.0 &&
		row.VolumeRatio >= 0.8 &&
		row.VolumeRatio <= 2.5 &&
		m.GaoKongProxy
}

func scoreCaoYuanStandard4A(row services.ScanSnapshotRow, m caoYuanKMetrics, strict bool) (float64, []string, []string, []string) {
	triggers := []string{
		"市值20-100亿",
		"10日下跌<=-1.5%",
		"20日跌幅<=-8%",
		"MA20下方",
		"贴近20日低点",
		"当日止跌翻红",
		"缩量温和",
	}
	if strict {
		triggers = append(triggers, "严选：放量翻红", "严选：30日深跌")
	}
	reasons := make([]string, 0, 3)
	risks := make([]string, 0, 3)
	score := 50.0
	score += clamp((-8.0-m.Pct20)*1.2, 0, 18)
	score += clamp((-1.5-m.Pct10)*0.8, 0, 12)
	switch {
	case m.DistLow20 <= 2:
		score += 16
	case m.DistLow20 <= 4:
		score += 10
	case m.DistLow20 <= 8:
		score += 5
	default:
		score -= 6
	}
	switch {
	case m.Drawdown60 <= -25:
		score += 10
	case m.Drawdown60 <= -18:
		score += 7
	case m.Drawdown60 <= -12:
		score += 4
	}
	if row.ChangePercent >= 0 && row.ChangePercent <= 2 {
		score += 8
	} else if row.ChangePercent > 2 {
		score -= 4
		risks = append(risks, "当日涨幅偏大，低吸性价比下降")
	}
	switch {
	case row.TurnoverRate <= 1.5:
		score += 8
	case row.TurnoverRate <= 2.5:
		score += 5
	default:
		score += 2
	}
	if row.VolumeRatio >= 0.6 && row.VolumeRatio <= 1.2 {
		score += 6
	} else if row.VolumeRatio <= 1.5 {
		score += 3
	}
	switch {
	case row.FloatMarketCap <= 40e8:
		score += 8
	case row.FloatMarketCap <= 60e8:
		score += 6
	default:
		score += 3
	}
	if m.BelowAllMA {
		reasons = append(reasons, "收盘在MA5/MA10/MA20下方，符合草元左侧超跌底部特征")
	} else {
		risks = append(risks, "未完全压在MA5/10/20下方，可能已进入反抽段")
	}
	if m.RecentSwingPct > 7 {
		score -= 4
		risks = append(risks, "振幅偏大，低吸承接不够平稳")
	}
	if strict {
		score += 6
		reasons = append(reasons, "严选层级：当日翻红、量比/换手放大、30日跌幅更深，属于强拐点确认")
	}
	return math.Round(clamp(score, 0, 100)*10) / 10, triggers, reasons, risks
}

func scoreCaoYuanZhuang4B(row services.ScanSnapshotRow, m caoYuanKMetrics, strict bool) (float64, []string, []string, []string) {
	triggers := []string{
		"市值20-100亿",
		"90日涨停记忆",
		"10日跌幅<=-3%",
		"20日跌幅<=-10%",
		"MA20下方",
		"贴近20日低点",
		"量能不过热",
	}
	if strict {
		triggers = append(triggers, "严选：高控盘强确认", "严选：20/30日深跌")
	}
	reasons := make([]string, 0, 3)
	risks := make([]string, 0, 3)
	score := 52.0
	score += 18
	score += clamp((-10.0-m.Pct20)*1.0, 0, 18)
	score += clamp((-3.0-m.Pct10)*0.8, 0, 12)
	switch {
	case m.DistLow20 <= 2:
		score += 12
	case m.DistLow20 <= 5:
		score += 8
	case m.DistLow20 <= 10:
		score += 4
	}
	if m.Drawdown60 <= -25 {
		score += 10
	} else if m.Drawdown60 <= -18 {
		score += 7
	} else if m.Drawdown60 <= -12 {
		score += 4
	}
	if m.GaoKongProxy {
		score += 12
		triggers = append(triggers, "高控盘代理")
	} else {
		score += 3
		risks = append(risks, "高控盘仅为代理弱确认")
	}
	if row.TurnoverRate <= 3 {
		score += 8
	} else if row.TurnoverRate <= 5 {
		score += 2
		risks = append(risks, "换手高于草元ZZ主体分布")
	} else {
		score -= 4
		risks = append(risks, "换手明显偏高，可能不是低位控盘")
	}
	if row.VolumeRatio <= 1.5 {
		score += 6
	} else {
		score -= 3
		risks = append(risks, "量比高于ZZ主体分布")
	}
	if row.ChangePercent > 2.5 {
		score -= 5
		risks = append(risks, "当日涨幅偏大，抓庄低吸变追反弹")
	}
	if m.BelowAllMA {
		reasons = append(reasons, "均线仍压制，属于深跌后的左侧企稳，不是右侧追高")
	}
	if strict {
		score += 8
		reasons = append(reasons, "严选层级：90日涨停记忆基础上叠加高控盘代理、20/30日深跌和翻红确认")
	}
	return math.Round(clamp(score, 0, 100)*10) / 10, triggers, reasons, risks
}

func buildCaoYuanMetrics(row services.ScanSnapshotRow, daily []models.KLineData) (caoYuanKMetrics, error) {
	if len(daily) < 31 {
		return caoYuanKMetrics{}, fmt.Errorf("日K不足31根")
	}
	klines := append([]models.KLineData(nil), daily...)
	last := len(klines) - 1
	if row.Price > 0 {
		klines[last].Close = row.Price
		if klines[last].High < row.Price {
			klines[last].High = row.Price
		}
		if klines[last].Low <= 0 || klines[last].Low > row.Price {
			klines[last].Low = row.Price
		}
	}
	closePrice := klines[last].Close
	if closePrice <= 0 {
		return caoYuanKMetrics{}, fmt.Errorf("日K收盘为空")
	}
	ma5, _, ok5 := computeLowBuyMA(klines, 5)
	ma10, _, ok10 := computeLowBuyMA(klines, 10)
	ma20, _, ok20 := computeLowBuyMA(klines, 20)
	if !ok5 || !ok10 || !ok20 || ma20 <= 0 {
		return caoYuanKMetrics{}, fmt.Errorf("均线计算失败")
	}
	pctAt := func(days int) (float64, bool) {
		idx := len(klines) - 1 - days
		if idx < 0 || klines[idx].Close <= 0 {
			return 0, false
		}
		return (closePrice/klines[idx].Close - 1) * 100, true
	}
	p5, ok5p := pctAt(5)
	p10, ok10p := pctAt(10)
	p20, ok20p := pctAt(20)
	p30, ok30p := pctAt(30)
	if !ok5p || !ok10p || !ok20p || !ok30p {
		return caoYuanKMetrics{}, fmt.Errorf("涨跌幅窗口不足")
	}
	low20 := math.MaxFloat64
	start20 := len(klines) - 20
	if start20 < 0 {
		start20 = 0
	}
	recentHigh := 0.0
	recentLow := math.MaxFloat64
	for _, k := range klines[start20:] {
		if k.Low > 0 && k.Low < low20 {
			low20 = k.Low
		}
		if k.High > recentHigh {
			recentHigh = k.High
		}
		if k.Low > 0 && k.Low < recentLow {
			recentLow = k.Low
		}
	}
	if low20 == math.MaxFloat64 || low20 <= 0 {
		return caoYuanKMetrics{}, fmt.Errorf("20日低点计算失败")
	}
	high60 := 0.0
	start60 := len(klines) - 60
	if start60 < 0 {
		start60 = 0
	}
	for _, k := range klines[start60:] {
		if k.High > high60 {
			high60 = k.High
		}
	}
	if high60 <= 0 {
		return caoYuanKMetrics{}, fmt.Errorf("60日高点计算失败")
	}
	hasLimit := hasCaoYuan90dLimitUp(row.Symbol, klines)
	distLow20 := (closePrice/low20 - 1) * 100
	drawdown60 := (closePrice/high60 - 1) * 100
	recentSwing := 0.0
	if recentHigh > 0 && recentLow > 0 && recentLow < math.MaxFloat64 {
		recentSwing = (recentHigh/recentLow - 1) * 100
	}
	gaoProxy := hasLimit && drawdown60 <= -12 && distLow20 <= 6 && row.TurnoverRate > 0 && row.TurnoverRate <= 3.5 && row.VolumeRatio > 0 && row.VolumeRatio <= 1.6
	return caoYuanKMetrics{
		Close:          closePrice,
		Pct5:           p5,
		Pct10:          p10,
		Pct20:          p20,
		Pct30:          p30,
		MA5:            ma5,
		MA10:           ma10,
		MA20:           ma20,
		Low20:          low20,
		High60:         high60,
		DistLow20:      distLow20,
		Drawdown60:     drawdown60,
		Has90dLimitUp:  hasLimit,
		GaoKongProxy:   gaoProxy,
		BelowAllMA:     closePrice < ma5 && closePrice < ma10 && closePrice < ma20,
		RecentSwingPct: recentSwing,
	}, nil
}

func hasCaoYuan90dLimitUp(symbol string, klines []models.KLineData) bool {
	if len(klines) < 2 {
		return false
	}
	threshold := 9.5
	code := normalizeAStockCode(symbol)
	if strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") {
		threshold = 19.5
	}
	start := len(klines) - 90
	if start < 1 {
		start = 1
	}
	for i := start; i < len(klines); i++ {
		prev := klines[i-1].Close
		if prev <= 0 || klines[i].Close <= 0 {
			continue
		}
		if (klines[i].Close/prev-1)*100 >= threshold {
			return true
		}
	}
	return false
}

func isCaoYuanAllowedBoard(symbol string) bool {
	lower := strings.ToLower(strings.TrimSpace(symbol))
	if strings.HasPrefix(lower, "bj") {
		return false
	}
	code := normalizeAStockCode(symbol)
	if code == "" || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
		return false
	}
	return strings.HasPrefix(code, "60") || strings.HasPrefix(code, "00") || strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301")
}

func normalizeAStockCode(symbol string) string {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) >= 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		code = code[2:]
	}
	if idx := strings.Index(code, "."); idx > 0 {
		code = code[:idx]
	}
	return code
}

func safeFiniteFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// tailLazyBlockedBoard 剔除创业板(30x)/科创(68x/689)/北交所——它们涨跌停机制不同，本策略阈值不适用。
func tailLazyBlockedBoard(symbol string) bool {
	c := strings.ToLower(strings.TrimSpace(symbol))
	if len(c) >= 2 && (strings.HasPrefix(c, "sh") || strings.HasPrefix(c, "sz") || strings.HasPrefix(c, "bj")) {
		c = c[2:]
	}
	return strings.HasPrefix(c, "30") || strings.HasPrefix(c, "68") || strings.HasPrefix(c, "8") || strings.HasPrefix(c, "4")
}

// evaluateTailLazyDaily 用日K验证尾盘懒人策略的技术条件。
// 返回 通过、ma10、ma20、形态类型(1强/2中)、理由、风险。
func evaluateTailLazyDaily(daily []models.KLineData) (bool, float64, float64, int, []string, []string) {
	n := len(daily)
	if n < 21 {
		return false, 0, 0, 0, nil, []string{"日K不足21根"}
	}
	ma10, _, ok10 := computeLowBuyMA(daily, 10)
	ma20, _, ok20 := computeLowBuyMA(daily, 20)
	if !ok10 || !ok20 || ma10 <= 0 || ma20 <= 0 {
		return false, ma10, ma20, 0, nil, []string{"均线数据不足"}
	}
	today := daily[n-1]
	reasons := make([]string, 0, 6)
	risks := make([]string, 0, 2)

	// 步骤4：多头排列 MA10>MA20
	if ma10 <= ma20 {
		return false, ma10, ma20, 0, nil, []string{"非多头排列(MA10<=MA20)"}
	}
	// 步骤9：形态——精准判定(用影线/收盘 vs MA10/MA20)
	formType, formDesc := services.ClassifyTailForm(today.Open, today.High, today.Low, today.Close, ma10, ma20)
	if formType == 0 {
		return false, ma10, ma20, 0, nil, []string{formDesc}
	}
	if formType == 2 {
		risks = append(risks, "破10日线，靠20日线支撑(中等强度)")
	}
	// 步骤5：当日最高=近5日新高
	for j := n - 5; j < n-1; j++ {
		if daily[j].High >= today.High {
			return false, ma10, ma20, formType, nil, []string{"当日最高非近5日新高"}
		}
	}
	// 步骤6/7：近20日至少一次涨幅>8%，且无单日跌幅<-8%
	pctAt := func(j int) float64 {
		if j < 1 || daily[j-1].Close <= 0 {
			return 0
		}
		return (daily[j].Close/daily[j-1].Close - 1) * 100
	}
	has8 := false
	for j := n - 20; j < n; j++ {
		p := pctAt(j)
		if p > 8.0 {
			has8 = true
		}
		if p < -8.0 {
			return false, ma10, ma20, formType, nil, []string{"近20日有单日跌幅<-8%(抗跌性差)"}
		}
	}
	if !has8 {
		return false, ma10, ma20, formType, nil, []string{"近20日无涨幅>8%(股性未激活)"}
	}
	// 步骤8：前7个交易日(不含今日)无涨停
	for j := n - 8; j < n-1; j++ {
		if pctAt(j) >= 9.8 {
			return false, ma10, ma20, formType, nil, []string{"前7日内出现涨停(过热)"}
		}
	}
	// 步骤10：上影线不长于实体
	body := math.Abs(today.Close - today.Open)
	upper := today.High - math.Max(today.Open, today.Close)
	if upper > body {
		return false, ma10, ma20, formType, nil, []string{"上影线长于实体(冲高回落，抛压大)"}
	}

	if formType == 1 {
		reasons = append(reasons, "形态：完整站上10/20日线(强)")
	} else {
		reasons = append(reasons, "形态：破10线回踩20线反弹(中)")
	}
	reasons = append(reasons,
		fmt.Sprintf("多头排列 MA10 %.2f > MA20 %.2f", ma10, ma20),
		"当日创近5日新高 · 近20日有>8%涨幅(股性活)",
		"近20日无<-8%大跌、前7日无涨停、上影≤实体")
	return true, ma10, ma20, formType, reasons, risks
}

// RunTailLazyScannerV2 尾盘懒人策略：打强→尾盘买入、次日上午止盈。
// 量比1-2.5 / 涨幅3-6% / 换手5-10% / 多头排列 / 近5日新高 / 近20日有>8%且无<-8% / 前7日无涨停 / 上影≤实体。
func (a *App) RunTailLazyScannerV2(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "尾盘懒人 V2（量比1-2.5 + 涨幅3-6% + 换手5-10% + 多头排列/新高/股性/形态）",
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}
	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)
	industryMap := buildIndustryMapFromEmbedded()

	// 第一层：快照粗筛（量比/涨幅/换手 + 板块/ST）
	ranked := make([]services.ScanSnapshotRow, 0, 256)
	for _, row := range snapshots {
		if row.Price <= 0 || row.Amount <= 0 || row.IsST {
			continue
		}
		if tailLazyBlockedBoard(row.Symbol) {
			continue
		}
		if row.VolumeRatio <= 1.0 || row.VolumeRatio >= 2.5 {
			continue
		}
		if row.ChangePercent <= 3.0 || row.ChangePercent >= 6.0 {
			continue
		}
		if row.TurnoverRate <= 5.0 || row.TurnoverRate >= 10.0 {
			continue
		}
		ranked = append(ranked, row)
	}
	// 按涨幅惯性排序，取前 80 做技术验证（控制 K 线拉取量）
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].ChangePercent > ranked[j].ChangePercent })
	result.CandidateCount = len(ranked)
	const techCap = 80
	if len(ranked) > techCap {
		ranked = ranked[:techCap]
	}

	// 第二层：逐只日K技术验证
	candidates := make([]models.LowBuyScannerItem, 0, 64)
	for _, row := range ranked {
		daily, derr := a.marketService.GetKLineData(row.Symbol, "1d", 45)
		if derr != nil || len(daily) < 21 {
			continue
		}
		pass, ma10, ma20, formType, reasons, risks := evaluateTailLazyDaily(daily)
		if !pass {
			continue
		}
		industry := industryMap[row.Symbol]
		if industry == "" {
			industry = "未知"
		}
		// 评分：base + 量比甜区 + 涨幅惯性 + 形态强弱
		score := 50.0
		score += clamp((row.VolumeRatio-1.0)*8, 0, 12)  // 量比越接近2越好
		score += clamp((row.ChangePercent-3.0)*3, 0, 9) // 涨幅惯性
		if formType == 1 {
			score += 12 // 完整站上(强)
		} else {
			score += 5 // 回踩20线(中)
		}
		score += clamp((10.0-row.TurnoverRate)*1.0, 0, 5) // 换手不过热加分
		score = math.Round(clamp(score, 0, 100)*10) / 10

		reasons = append([]string{
			fmt.Sprintf("涨幅 %.2f%% · 量比 %.2f · 换手 %.2f%%", row.ChangePercent, row.VolumeRatio, row.TurnoverRate),
		}, reasons...)

		ma10Status := "broke"
		if row.Price >= ma10 {
			ma10Status = "hold"
		}
		triggers := []string{"量比1-2.5", "涨幅3-6%", "换手5-10%", "多头排列", "近5日新高", "股性激活(20日>8%)"}
		if formType == 1 {
			triggers = append(triggers, "站上10/20线(强)")
		} else {
			triggers = append(triggers, "回踩20线反弹(中)")
		}
		if risks == nil {
			risks = []string{}
		}
		candidates = append(candidates, models.LowBuyScannerItem{
			Symbol:         row.Symbol,
			Name:           row.Name,
			Price:          row.Price,
			ChangePercent:  row.ChangePercent,
			Amount:         row.Amount,
			TurnoverRate:   row.TurnoverRate,
			MainNetInflow:  row.MainNetInflow,
			TotalMarketCap: row.TotalMarketCap,
			FloatMarketCap: row.FloatMarketCap,
			CapBucket:      classifyCapBucket(row.TotalMarketCap),
			Industry:       industry,
			Score:          score,
			TriggerCount:   len(triggers),
			Triggers:       triggers,
			Reasons:        reasons,
			RiskFlags:      risks,
			BuyPointHint:   "尾盘14:30-15:00分批买入，次日上午冲高止盈",
			SellPointHint:  "次日上午冲高5-6个点止盈；冲高乏力/平开即保本走",
			StopLossHint:   "次日不冲高反走弱，或跌破买入价-3%，止损离场",
			MA10:           ma10,
			MA10Status:     ma10Status,
			UpdatedAt:      start.Format("15:04:05"),
		})
		_ = ma20
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf(
		"尾盘懒人V2：量比(1,2.5)/涨幅(3%%,6%%)/换手(5%%,10%%) 粗筛 %d 只，日K技术验证后入选 %d 只（剔除创业板/科创/北交所）",
		result.CandidateCount, result.SelectedCount))
	a.saveLowBuyStrategyPicks("taillazy-v2", "低吸尾盘策略2", result)
	return result
}

// RunTailLazyReplayOnDate 历史复盘：指定交易日按尾盘懒人规则筛选，带次日表现（用 history.db）。
func (a *App) RunTailLazyReplayOnDate(date string, limit int) models.LowBuyScannerResult {
	result := models.LowBuyScannerResult{
		AsOf:        date,
		RuleVersion: "尾盘懒人 V2 · 历史复盘（指定日筛选 + 次日表现，量比用成交额代理）",
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.historyService == nil {
		result.Warning = "历史服务未初始化"
		return result
	}
	items, asOf, warn := a.historyService.ScanTailLazyOnDate(date, limit)
	result.AsOf = asOf
	result.Items = items
	result.SelectedCount = len(items)
	result.CandidateCount = len(items)
	if warn != "" {
		result.Warning = warn
	} else {
		result.Warning = fmt.Sprintf("历史复盘 %s：尾盘懒人规则入选 %d 只（量比用成交额代理，形态为系统近似，建议人工抽查K线）", asOf, len(items))
	}
	return result
}

// RunFundamentalScan 基本面初筛(框架步骤①②)：按模板A(value)/B(boom)筛全市场最新财务。
func (a *App) RunFundamentalScan(preset string) models.FundamentalScanResult {
	if a == nil || a.historyService == nil {
		return models.FundamentalScanResult{Warning: "历史服务未就绪"}
	}
	return a.historyService.RunFundamentalScan(preset)
}

// RefreshFundamentals 拉取最近报告期的全市场业绩报表入库(从近到远尝试，取到为止)。
func (a *App) RefreshFundamentals() string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	for _, rd := range recentReportDates() {
		n, warn := a.historyService.FetchAndStoreFundamentals(rd)
		if n > 0 {
			nb, _ := a.historyService.FetchAndStoreBalance(rd)
			// 商誉/分红按报告期从近到远试，取到为止(它们更新最新报告期行)
			var ng, nd int
			for _, r2 := range recentReportDates() {
				if ng == 0 {
					ng, _ = a.historyService.FetchAndStoreGoodwill(r2)
				}
				if nd == 0 {
					nd, _ = a.historyService.FetchAndStoreDividend(r2)
				}
				if ng > 0 && nd > 0 {
					break
				}
			}
			return fmt.Sprintf("已更新 %s 财务%d (负债表%d 商誉%d 分红%d)", rd, n, nb, ng, nd)
		}
		if warn != "" && !strings.Contains(warn, "success") {
			// 网络等错误直接返回；空数据则继续试更早报告期
			if strings.Contains(warn, "失败") {
				return warn
			}
		}
	}
	return "暂无可用财务报告期数据"
}

// RefreshFundamentalsHistory 批量拉取多期业绩报表(2022Q4起所有季度末)，支撑连续多年ROE与滚动回测。
func (a *App) RefreshFundamentalsHistory() string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	total, periods := 0, 0
	for _, rd := range quarterEndsSince(2022) {
		n, _ := a.historyService.FetchAndStoreFundamentals(rd)
		if n > 0 {
			total += n
			periods++
		}
	}
	return fmt.Sprintf("已拉取 %d 个报告期，累计 %d 条财务", periods, total)
}

// quarterEndsSince 生成 fromYear 起到今天的所有季度末(升序)。
func quarterEndsSince(fromYear int) []string {
	now := time.Now()
	var out []string
	for y := fromYear; y <= now.Year(); y++ {
		for _, q := range []struct{ m, d int }{{3, 31}, {6, 30}, {9, 30}, {12, 31}} {
			t := time.Date(y, time.Month(q.m), q.d, 0, 0, 0, 0, time.Local)
			if t.Before(now) {
				out = append(out, t.Format("2006-01-02"))
			}
		}
	}
	return out
}

// recentReportDates 生成最近若干个季度末(降序)，用于尝试拉取最新财报。
func recentReportDates() []string {
	now := time.Now()
	qs := []struct{ m, d int }{{12, 31}, {9, 30}, {6, 30}, {3, 31}}
	var out []string
	for y := now.Year(); y >= now.Year()-1; y-- {
		for _, q := range qs {
			t := time.Date(y, time.Month(q.m), q.d, 0, 0, 0, 0, time.Local)
			if t.Before(now) {
				out = append(out, t.Format("2006-01-02"))
			}
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// RunTailForwardScan 2:30 实盘向前验证：扫游资7信号 → 实时盘口判"能否买进(非封死涨停)"
// → 可买的(autoBuy=true 时)按当前价记入模拟持仓。这是验证 2:30 入场口径的唯一诚实办法
// (历史分时取不到，只能用今天往后的真实数据向前积累)。
func (a *App) RunTailForwardScan(strategy string, autoBuy bool) models.TailForwardResult {
	res := models.TailForwardResult{Strategy: strategy, Auto: autoBuy, AsOf: time.Now().Format("2006-01-02 15:04:05")}
	if a == nil || a.marketService == nil {
		res.Warning = "行情服务未就绪"
		return res
	}
	// 目前仅游资7；后续可扩展到其它策略
	scan := a.RunHotMoneyBreakoutScannerV7(models.LowBuyScannerRequest{Limit: 20})
	source := "hot-money-v7"
	if scan.Warning != "" {
		res.Warning = scan.Warning
	}
	// 已持仓去重
	held := map[string]bool{}
	if a.paperService != nil {
		for _, p := range a.paperService.OpenPositions() {
			held[strings.ToLower(p.Symbol)] = true
		}
	}
	const maxPick = 6
	for i, it := range scan.Items {
		if i >= maxPick {
			break
		}
		buyable, reason := a.isBuyableNow(it.Symbol, it.Price, it.ChangePercent)
		c := models.TailForwardCandidate{
			Symbol: it.Symbol, Name: it.Name, Price: round2(it.Price), ChangePct: round2(it.ChangePercent),
			Score: it.Score, Buyable: buyable, Reason: reason, AlreadyHeld: held[strings.ToLower(it.Symbol)],
		}
		if buyable {
			res.BuyableCount++
		} else {
			res.SealedCount++
		}
		if autoBuy && buyable && !c.AlreadyHeld && it.Price > 0 && a.paperService != nil {
			if _, err := a.paperService.Add(it.Symbol, it.Name, source, it.Price, 1000); err == nil {
				c.Added = true
				res.AddedCount++
				held[strings.ToLower(it.Symbol)] = true
			}
		}
		res.Candidates = append(res.Candidates, c)
	}
	return res
}

// GetTailForwardConfig 读取 2:30 向前验证配置。
func (a *App) GetTailForwardConfig() models.TailForwardConfig {
	if a == nil || a.configService == nil {
		return models.TailForwardConfig{}
	}
	return a.configService.GetConfig().TailForward
}

// SetTailForwardConfig 保存 2:30 向前验证配置(定时开关 + 自动/清单模式)。
func (a *App) SetTailForwardConfig(enabled, auto bool) string {
	if a == nil || a.configService == nil {
		return "配置服务未就绪"
	}
	cfg := a.configService.GetConfig()
	cfg.TailForward = models.TailForwardConfig{Enabled: enabled, Auto: auto}
	if err := a.configService.UpdateConfig(cfg); err != nil {
		return err.Error()
	}
	return ""
}

// startTailForwardScheduler 交易日 14:30 自动触发 2:30 向前验证(按配置 auto/清单)。
// 每分钟检查一次，命中窗口且当日未触发则执行。
func (a *App) startTailForwardScheduler() {
	go func() {
		cst := time.FixedZone("CST", 8*3600)
		lastFired := ""
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			cfg := a.GetTailForwardConfig()
			now := time.Now().In(cst)
			today := now.Format("2006-01-02")
			mins := now.Hour()*60 + now.Minute()
			// 14:30 之后、当天未跑过、且开启 → 触发（窗口放宽到 14:30 起，错过也补跑一次，避免漏掉整天）
			if cfg.Enabled && lastFired != today && mins >= 14*60+30 {
				if a.marketService != nil {
					if st := a.marketService.GetMarketStatus(); !st.IsTradeDay {
						lastFired = today
						if a.ctx != nil {
							rt.LogInfof("2:30自动选股: 今日非交易日(%s)，跳过", st.StatusText)
						}
						<-tk.C
						continue
					}
				}
				lastFired = today // 先占位，避免长扫描期间重复触发
				if a.ctx != nil {
					rt.LogInfof("2:30多策略自动选股开始(%s)…", now.Format("15:04"))
				}
				res := a.RunTailForwardScanAll(cfg.Auto)
				if a.ctx != nil {
					rt.LogInfof("2:30多策略自动选股已触发(%s): 可买%d 封死%d 自动记入%d(auto=%v)",
						now.Format("15:04"), res.BuyableCount, res.SealedCount, res.AddedCount, cfg.Auto)
				}
			}
			<-tk.C
		}
	}()
}

// tailScanSpec 一个参与 2:30 自动选股的策略。
type tailScanSpec struct {
	source, label string
	topN          int
	scan          func(models.LowBuyScannerRequest) models.LowBuyScannerResult
}

// tailScanSpecs 全部参与 2:30 自动选股的技术策略（基本面是长线，不在每日尾盘闭环里）。
func (a *App) tailScanSpecs() []tailScanSpec {
	return []tailScanSpec{
		{"lowbuy-v1", "低吸1", 3, a.RunLowBuyScannerV1},
		{"taillazy-v2", "低吸2", 3, a.RunTailLazyScannerV2},
		{"limit-pullback-v1", "涨停回调4", 3, a.RunLimitPullbackScanner},
		{"triple-volume-v5", "三倍量5", 3, a.RunTripleVolumeScannerV5},
		{"tail-buy-v6", "尾盘买入6", 3, a.RunTailBuyScannerV6},
		{"hot-money-v7", "游资突破7", 3, a.RunHotMoneyBreakoutScannerV7},
		{"dip-entry-v8", "低吸入场8", 3, a.RunDipEntryScannerV8},
		{"monster-v9", "捉妖9", 3, a.RunMonsterScannerV9},
		{"monster-v10", "捉妖10", 3, a.RunMonsterScannerV10},
	}
}

// RunTailForwardScanAll 2:30 多策略自动选股：每个技术策略各取 TopN → 实时盘口判可成交 →
// (autoBuy 时)按策略来源记入模拟持仓。风控引擎负责后续止盈止损平仓。
func (a *App) RunTailForwardScanAll(autoBuy bool) models.TailForwardResult {
	res := models.TailForwardResult{Strategy: "all", Auto: autoBuy, AsOf: time.Now().Format("2006-01-02 15:04:05")}
	if a == nil || a.marketService == nil {
		res.Warning = "行情服务未就绪"
		return res
	}
	held := map[string]bool{}
	if a.paperService != nil {
		for _, p := range a.paperService.OpenPositions() {
			held[strings.ToLower(p.Symbol)+"|"+p.Source] = true
		}
	}
	for _, spec := range a.tailScanSpecs() {
		scan := spec.scan(models.LowBuyScannerRequest{Limit: 10})
		added := 0
		for _, it := range scan.Items {
			if added >= spec.topN {
				break
			}
			buyable, reason := a.isBuyableNow(it.Symbol, it.Price, it.ChangePercent)
			key := strings.ToLower(it.Symbol) + "|" + spec.source
			c := models.TailForwardCandidate{
				Symbol: it.Symbol, Name: it.Name, Source: spec.source, SourceLabel: spec.label,
				Price: round2(it.Price), ChangePct: round2(it.ChangePercent), Score: it.Score,
				Buyable: buyable, Reason: reason, AlreadyHeld: held[key],
			}
			if buyable {
				res.BuyableCount++
			} else {
				res.SealedCount++
			}
			if autoBuy && buyable && !c.AlreadyHeld && it.Price > 0 && a.paperService != nil {
				if _, err := a.paperService.Add(it.Symbol, it.Name, spec.source, it.Price, 1000); err == nil {
					c.Added = true
					res.AddedCount++
					held[key] = true
				}
			}
			res.Candidates = append(res.Candidates, c)
			added++
		}
	}
	return res
}

// isBuyableNow 判断当前能否买进：非涨停一律可买；涨停则看盘口是否还有卖盘(封死=无卖盘=买不进)。
func (a *App) isBuyableNow(symbol string, price, changePct float64) (bool, string) {
	if changePct < 9.8 { // 主板未涨停，正常可买
		return true, "可买"
	}
	ob, err := a.marketService.GetRealOrderBook(symbol)
	if err != nil {
		return false, "盘口取数失败，按封死处理"
	}
	var askSize int64
	for _, ask := range ob.Asks {
		askSize += ask.Size
	}
	if askSize <= 0 {
		return false, "封死涨停·无卖盘买不进"
	}
	return true, "涨停有卖盘·可买"
}

// RunPaperStrategyAccount 实盘跟踪账户：由"我加进模拟持仓的票"按策略(source)分组驱动。
// 买点=加入时间/成本价；从加入日起用 history.db 每日真实收盘价逐日盯市(今日用实时价)；
// 已平仓的按平仓价/平仓日冻结为已实现；扣双边成本，与模拟持仓同口径；基准=同期等权全A。
func (a *App) RunPaperStrategyAccount(source string) models.StrategyAccountResult {
	res := models.StrategyAccountResult{Strategy: source}
	if a == nil || a.historyService == nil || a.paperService == nil {
		res.Warning = "服务未初始化"
		return res
	}
	all := a.paperService.List()
	pos := make([]models.PaperPosition, 0, len(all))
	for _, p := range all {
		if (source == "" || source == "all" || p.Source == source) && p.Shares > 0 && p.CostPrice > 0 {
			p.OpenDate = datePart(p.OpenDate)
			p.CloseDate = datePart(p.CloseDate)
			pos = append(pos, p)
		}
	}
	if len(pos) == 0 {
		res.Warning = "该策略下还没有模拟持仓——先在选股结果里「加模拟持仓」"
		return res
	}

	buy, sell := services.PaperCostRates()
	minDate := pos[0].OpenDate
	openSyms := make([]string, 0, len(pos))
	for _, p := range pos {
		if p.OpenDate != "" && p.OpenDate < minDate {
			minDate = p.OpenDate
		}
		if p.Status == "open" {
			openSyms = append(openSyms, p.Symbol)
		}
	}
	latestDates, _ := a.historyService.RecentTradeDates(1)
	if len(latestDates) == 0 {
		res.Warning = "历史库无交易日"
		return res
	}
	latest := latestDates[len(latestDates)-1]

	idxLevel, axis, err := a.historyService.EqualWeightIndexBetween(minDate, latest)
	if err != nil || len(axis) == 0 {
		res.Warning = "载入基准失败"
		return res
	}
	axisPos := make(map[string]int, len(axis))
	for i, d := range axis {
		axisPos[d] = i
	}
	idxOf := func(date string) int { // 首个 >= date 的轴下标
		for i, d := range axis {
			if d >= date {
				return i
			}
		}
		return len(axis) - 1
	}

	// 各持仓标的的逐日收盘价（缺失日向前填充）
	filled := map[string]map[string]float64{}
	for _, p := range pos {
		if _, ok := filled[p.Symbol]; ok {
			continue
		}
		bars, _ := a.historyService.LoadKLineDataUntil(p.Symbol, latest, len(axis)+10)
		raw := make(map[string]float64, len(bars))
		for _, b := range bars {
			raw[b.Time] = b.Close
		}
		m := make(map[string]float64, len(axis))
		last := 0.0
		for _, d := range axis {
			if c, ok := raw[d]; ok && c > 0 {
				last = c
			}
			m[d] = last
		}
		filled[p.Symbol] = m
	}
	// 未平仓标的的实时价（与模拟持仓同源）
	rt := map[string]float64{}
	if len(openSyms) > 0 && a.marketService != nil {
		if data, e := a.marketService.GetStockRealTimeData(openSyms...); e == nil {
			for _, st := range data {
				rt[st.Symbol] = st.Price
			}
		}
	}

	// 逐日盯市净值（本金=各票成本含买入费之和，未到买入日的部分按预留现金计入，曲线起点平直）
	capital := 0.0
	for _, p := range pos {
		capital += p.CostPrice * (1 + buy) * float64(p.Shares)
	}
	equity := make([]models.AccountEquityPoint, 0, len(axis))
	for _, d := range axis {
		eq := 0.0
		for _, p := range pos {
			outlay := p.CostPrice * (1 + buy) * float64(p.Shares)
			if d < p.OpenDate {
				eq += outlay // 尚未买入，作预留现金
				continue
			}
			if p.Status == "closed" && p.CloseDate != "" && d >= p.CloseDate {
				eq += p.ClosePrice * (1 - sell) * float64(p.Shares) // 已实现，冻结
				continue
			}
			px := filled[p.Symbol][d]
			if d == latest && p.Status == "open" {
				if r := rt[p.Symbol]; r > 0 {
					px = r
				}
			}
			if px <= 0 {
				px = p.CostPrice
			}
			eq += px * (1 - sell) * float64(p.Shares)
		}
		equity = append(equity, models.AccountEquityPoint{Date: d, Value: round2(eq)})
	}

	final := equity[len(equity)-1].Value
	cashRealized := 0.0

	// 当前持仓 / 平仓记录 / 计分卡
	var closedNet []float64
	closedHold := 0
	for _, p := range pos {
		holdEnd := latest
		if p.Status == "closed" && p.CloseDate != "" {
			holdEnd = p.CloseDate
		}
		holdDays := idxOf(holdEnd) - idxOf(p.OpenDate)
		if holdDays < 0 {
			holdDays = 0
		}
		if p.Status == "open" {
			cur := filled[p.Symbol][latest]
			if r := rt[p.Symbol]; r > 0 {
				cur = r
			}
			if cur <= 0 {
				cur = p.CostPrice
			}
			res.Holdings = append(res.Holdings, models.AccountHolding{
				Symbol: p.Symbol, Name: p.Name, EntryDate: p.OpenDate,
				EntryPrice: round2(p.CostPrice), CurrentPrice: round2(cur), HoldDays: holdDays,
				UnrealizedPct: round2(services.PaperNetReturnPct(p.CostPrice, cur)),
				Value:         round2(cur * float64(p.Shares)),
			})
		} else {
			netPct := services.PaperNetReturnPct(p.CostPrice, p.ClosePrice)
			closedNet = append(closedNet, netPct)
			closedHold += holdDays
			cashRealized += p.ClosePrice * (1 - sell) * float64(p.Shares)
			res.Trades = append(res.Trades, models.AccountTrade{
				Symbol: p.Symbol, Name: p.Name, EntryDate: p.OpenDate, ExitDate: p.CloseDate,
				EntryPrice: round2(p.CostPrice), ExitPrice: round2(p.ClosePrice), HoldDays: holdDays,
				ReturnPct: round2(netPct), ExitReason: chooseFirstNonEmpty(p.ExitReason, "manual"),
			})
		}
	}
	sort.Slice(res.Holdings, func(i, j int) bool { return res.Holdings[i].UnrealizedPct > res.Holdings[j].UnrealizedPct })
	sort.Slice(res.Trades, func(i, j int) bool { return res.Trades[i].ExitDate > res.Trades[j].ExitDate })

	// 计分卡（已平仓口径）
	win, sumWin, sumLoss := 0, 0.0, 0.0
	for _, r := range closedNet {
		if r > 0 {
			win++
			sumWin += r
		} else {
			sumLoss += -r
		}
	}
	nClosed := len(closedNet)
	if nClosed > 0 {
		res.ClosedTrades = nClosed
		res.WinRate = round2(float64(win) / float64(nClosed) * 100)
		sum := 0.0
		for _, r := range closedNet {
			sum += r
		}
		res.Expectancy = round2(sum / float64(nClosed))
		res.AvgHoldDays = round2(float64(closedHold) / float64(nClosed))
		avgWin := 0.0
		if win > 0 {
			avgWin = sumWin / float64(win)
		}
		avgLoss := 0.0
		if nClosed-win > 0 {
			avgLoss = sumLoss / float64(nClosed-win)
		}
		if avgLoss > 0 {
			res.PayoffRatio = round2(avgWin / avgLoss)
		}
		if sumLoss > 0 {
			res.ProfitFactor = round2(sumWin / sumLoss)
		} else if sumWin > 0 {
			res.ProfitFactor = 99.99
		}
	}

	// 最大回撤
	peak, maxDD := capital, 0.0
	for _, e := range equity {
		if e.Value > peak {
			peak = e.Value
		}
		if peak > 0 {
			if dd := (peak - e.Value) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}

	bm := 0.0
	if l0 := idxLevel[axis[0]]; l0 > 0 {
		bm = (idxLevel[latest]/l0 - 1) * 100
	}
	res.Capital = round2(capital)
	res.StartDate, res.EndDate = axis[0], latest
	res.FinalEquity = round2(final)
	res.ReturnPct = round2((final/capital - 1) * 100)
	res.MaxDrawdown = round2(maxDD)
	res.Benchmark = round2(bm)
	res.Excess = round2((final/capital-1)*100 - bm)
	res.Cash = round2(cashRealized)
	res.Equity = equity
	return res
}

func datePart(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// RunStrategyAccount 策略账户：固定10万本金、尾盘进场、自动买卖、Top3/限6仓/冷却3日。
func (a *App) RunStrategyAccount(strategy string, days int) models.StrategyAccountResult {
	return a.runStrategyAccount(strategy, days, false)
}

// RunStrategyAccountRisk 同上，但所有持仓改套统一通用风控线(短线稳健)平仓，用于对比净值曲线。
func (a *App) RunStrategyAccountRisk(strategy string, days int) models.StrategyAccountResult {
	return a.runStrategyAccount(strategy, days, true)
}

func (a *App) runStrategyAccount(strategy string, days int, useRisk bool) models.StrategyAccountResult {
	if a == nil || a.historyService == nil {
		return models.StrategyAccountResult{Strategy: strategy, Warning: "历史服务未初始化"}
	}
	var res models.StrategyAccountResult
	if spec, ok := formulaAccountSpecs[strategy]; ok {
		signals, warn := a.computeFormulaSignals(strategy, spec, days)
		if warn != "" {
			return models.StrategyAccountResult{Strategy: strategy, Warning: warn}
		}
		res = a.historyService.RunAccountFromSignals(strategy, days, signals, accountExitProfile(strategy), useRisk)
	} else {
		if strategy != "taillazy" {
			strategy = "lowbuy"
		}
		res = a.historyService.RunStrategyAccount(strategy, days, useRisk)
	}
	a.applyLiveHoldingPrices(&res)
	return res
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// applyLiveHoldingPrices 把策略账户里"当前还持有"的票现价改为实时报价，与模拟持仓同一行情源对齐。
// 历史平仓记录仍用当时收盘价（回测必须）；非交易时段实时价≈最近收盘价，不会产生偏差。
func (a *App) applyLiveHoldingPrices(res *models.StrategyAccountResult) {
	if res == nil || len(res.Holdings) == 0 || a.marketService == nil {
		return
	}
	codes := make([]string, 0, len(res.Holdings))
	for _, h := range res.Holdings {
		codes = append(codes, h.Symbol)
	}
	priceMap := map[string]float64{}
	if rt, err := a.marketService.GetStockRealTimeData(codes...); err == nil {
		for _, st := range rt {
			priceMap[st.Symbol] = st.Price
		}
	}
	if len(priceMap) == 0 {
		return // 实时源不可用，保留历史收盘价
	}
	for i := range res.Holdings {
		h := &res.Holdings[i]
		live := priceMap[h.Symbol]
		if live <= 0 || h.CurrentPrice <= 0 || h.EntryPrice <= 0 {
			continue
		}
		// 按实时价重算市值（Value 原本=投入额×旧现价/成本，等比缩放到实时价）
		h.Value = round2(h.Value * live / h.CurrentPrice)
		h.CurrentPrice = round2(live)
		h.UnrealizedPct = round2((live/h.EntryPrice - 1) * 100)
	}
	sort.Slice(res.Holdings, func(i, j int) bool { return res.Holdings[i].UnrealizedPct > res.Holdings[j].UnrealizedPct })

	// 期末净值 = 现金 + 实时持仓市值；同步收益/超额，并把净值曲线最后一点拉到实时
	final := res.Cash
	for _, h := range res.Holdings {
		final += h.Value
	}
	res.FinalEquity = round2(final)
	if res.Capital > 0 {
		res.ReturnPct = round2((final/res.Capital - 1) * 100)
		res.Excess = round2(res.ReturnPct - res.Benchmark)
	}
	if n := len(res.Equity); n > 0 {
		res.Equity[n-1].Value = round2(final)
	}
	// 用实时收尾后的曲线重算最大回撤
	peak, maxDD := res.Capital, 0.0
	for _, e := range res.Equity {
		if e.Value > peak {
			peak = e.Value
		}
		if peak > 0 {
			if dd := (peak - e.Value) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	res.MaxDrawdown = round2(maxDD)
}

// accountExitProfile 返回某策略在账户里使用的专属离场/加仓规则；nil=用统一低吸机械纪律。
func accountExitProfile(strategy string) *services.StrategyTradeProfile {
	switch strategy {
	case "hotmoney":
		return services.HotMoney7Profile()
	default:
		return nil
	}
}

// formulaEvalFn 公式型策略的逐股评估器签名（与 formulaScannerSpec.Evaluate 一致）。
type formulaEvalFn func(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool)

// formulaAccountSpec 公式型策略接入账户所需的评估器与回看深度。
type formulaAccountSpec struct {
	eval     formulaEvalFn
	lookback int // 评估器所需的历史K线回看（交易日）
	topN     int
}

// formulaAccountSpecs 已接入策略账户的公式型策略。key 与前端 StrategyAccountDialog 的 STRATS 对应。
var formulaAccountSpecs = map[string]formulaAccountSpec{
	"hotmoney": {eval: evaluateHotMoneyV7Row, lookback: 30, topN: 3}, // 游资突破策略7
	"monster":  {eval: evaluateMonsterV9Row, lookback: 320, topN: 3}, // 捉妖策略9
	"dipentry": {eval: evaluateDipEntryV8Row, lookback: 96, topN: 3}, // 低吸入场策略8
}

// computeFormulaSignals 在内存中逐交易日跑公式型评估器，算出每日 TopN 候选信号。
// 一次性载入区间全量日K，按日用历史快照(含流通市值/换手)评估，多核并行。
func (a *App) computeFormulaSignals(strategy string, spec formulaAccountSpec, days int) (map[string][]models.AccountCandidate, string) {
	if days <= 0 || days > 520 {
		days = 250
	}
	allDates, err := a.historyService.RecentTradeDates(days + spec.lookback)
	if err != nil || len(allDates) < 25 {
		return nil, "交易日不足"
	}
	tradeStart := len(allDates) - days
	if tradeStart < 0 {
		tradeStart = 0
	}
	tradeDates := allDates[tradeStart:]

	bars, barIdx, err := a.historyService.LoadAllKLinesAscending(allDates[0], allDates[len(allDates)-1])
	if err != nil {
		return nil, "载入历史K线失败：" + err.Error()
	}

	industryMap := buildIndustryMapFromEmbedded()
	workers := stdruntime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 12 {
		workers = 12
	}

	signals := make(map[string][]models.AccountCandidate, len(tradeDates))
	for _, date := range tradeDates {
		snaps, asOf, derr := a.historyService.LoadScanRowsOnDateForReplay(date, false)
		if derr != nil || asOf != date || len(snaps) == 0 {
			continue
		}
		cands := evaluateFormulaUniverse(snaps, bars, barIdx, industryMap, date, spec, workers)
		sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
		if len(cands) > spec.topN {
			cands = cands[:spec.topN]
		}
		signals[date] = cands
	}
	return signals, ""
}

// computeFormulaSignalsAll 计算 fromDate 起全部交易日的公式型策略信号（滚动多年回测：一次算、按年切片）。
func (a *App) computeFormulaSignalsAll(strategy string, spec formulaAccountSpec, fromDate string) (map[string][]models.AccountCandidate, string) {
	allDates, err := a.historyService.RecentTradeDates(2000)
	if err != nil || len(allDates) < 50 {
		return nil, "交易日不足"
	}
	bars, barIdx, err := a.historyService.LoadAllKLinesAscending(allDates[0], allDates[len(allDates)-1])
	if err != nil {
		return nil, "载入历史K线失败：" + err.Error()
	}
	industryMap := buildIndustryMapFromEmbedded()
	workers := stdruntime.NumCPU()
	if workers < 2 {
		workers = 2
	} else if workers > 12 {
		workers = 12
	}
	signals := make(map[string][]models.AccountCandidate, len(allDates))
	for _, date := range allDates {
		if date < fromDate {
			continue
		}
		snaps, asOf, derr := a.historyService.LoadScanRowsOnDateForReplay(date, false)
		if derr != nil || asOf != date || len(snaps) == 0 {
			continue
		}
		cands := evaluateFormulaUniverse(snaps, bars, barIdx, industryMap, date, spec, workers)
		sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
		if len(cands) > spec.topN {
			cands = cands[:spec.topN]
		}
		signals[date] = cands
	}
	return signals, ""
}

// evaluateFormulaUniverse 对某交易日全市场快照并行跑评估器，返回命中候选。
func evaluateFormulaUniverse(snaps []services.ScanSnapshotRow, bars map[string][]models.KLineData, barIdx map[string]map[string]int,
	industryMap map[string]string, date string, spec formulaAccountSpec, workers int) []models.AccountCandidate {
	var mu sync.Mutex
	out := make([]models.AccountCandidate, 0, 64)
	jobs := make(chan services.ScanSnapshotRow, workers*2)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if row.Price <= 0 || row.IsST {
					continue
				}
				if isFormulaScannerBlockedBoard(row.Symbol) {
					continue
				}
				idxMap, ok := barIdx[row.Symbol]
				if !ok {
					continue
				}
				ri, ok := idxMap[date]
				if !ok {
					continue
				}
				daily := bars[row.Symbol][:ri+1]
				industry := chooseFirstNonEmpty(industryMap[row.Symbol], row.Industry, "未知")
				item, hit := spec.eval(row, industry, daily, date)
				if !hit {
					continue
				}
				mu.Lock()
				out = append(out, models.AccountCandidate{Symbol: row.Symbol, Score: item.Score})
				mu.Unlock()
			}
		}()
	}
	for _, row := range snaps {
		jobs <- row
	}
	close(jobs)
	wg.Wait()
	return out
}

// RunLowBuyReplayOnDate 低吸单日历史复盘：指定日选Top + 机械纪律持有结果。
func (a *App) RunLowBuyReplayOnDate(date string, limit int, maxChangePct float64) models.LowBuyScannerResult {
	result := models.LowBuyScannerResult{
		AsOf:        date,
		RuleVersion: "低吸 V1.2 · 历史复盘（指定日Top + 机械纪律持有结果）",
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.historyService == nil {
		result.Warning = "历史服务未初始化"
		return result
	}
	if maxChangePct == 0 {
		maxChangePct = 1.5
	}
	items, asOf, warn := a.historyService.ScanLowBuyOnDate(date, limit, maxChangePct)
	result.AsOf = asOf
	result.Items = items
	result.SelectedCount = len(items)
	result.CandidateCount = len(items)
	if warn != "" {
		result.Warning = warn
	} else {
		result.Warning = fmt.Sprintf("低吸历史复盘 %s：选出 %d 只，已按机械纪律(T+1开盘进/扣成本)算持有结果", asOf, len(items))
	}
	return result
}

// RunLowBuyBatchReplay 低吸批量历史复盘：区间整体胜率/期望/赔率/超额alpha（按年份+合计）。
func (a *App) RunLowBuyBatchReplay(start, end string, topN int, maxChangePct float64) models.LowBuyBatchResult {
	if a == nil || a.historyService == nil {
		return models.LowBuyBatchResult{Start: start, End: end, Warning: "历史服务未初始化"}
	}
	if maxChangePct == 0 {
		maxChangePct = 1.5
	}
	return a.historyService.BatchLowBuyReplay(start, end, topN, maxChangePct)
}

// RunTailLazyBatchReplay 尾盘懒人批量历史复盘：区间内整体真实命中率/期望（按年份+合计）。
func (a *App) RunTailLazyBatchReplay(start, end string) models.TailLazyBatchResult {
	if a == nil || a.historyService == nil {
		return models.TailLazyBatchResult{Start: start, End: end, Warning: "历史服务未初始化"}
	}
	return a.historyService.BatchTailLazyReplay(start, end)
}

// RunLateDayChaseScanner 尾盘强势股扫描。
// 镜像手工流程：14:30 看涨幅榜前 60，筛涨幅/量比/换手/流通市值，再验证日K与分时强度。
func (a *App) RunLateDayChaseScanner(req models.LateDayChaseScannerRequest) models.LateDayChaseScannerResult {
	start := time.Now()
	opts := normalizeLateDayChaseRequest(req)
	result := models.LateDayChaseScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "尾盘强势股 V1（涨幅榜60 + 3%-5% + 量比/换手/流通市值 + 日K/分时确认）",
		RankLimit:   opts.RankLimit,
		Items:       []models.LateDayChaseScannerItem{},
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}

	snapshots, err := a.marketService.GetAllAStockSnapshot(opts.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)
	industryMap := buildIndustryMapFromEmbedded()

	// 先在全A里按"涨幅3-5%+量比+换手+流通市值"粗筛出候选，再按涨幅排序取前 RankLimit 做技术验证。
	// （旧逻辑是先取涨幅榜前N再筛3-5%，热市涨幅榜前N全是≥5%涨停，导致3-5%的票被排除→0只）
	ranked := make([]services.ScanSnapshotRow, 0, 256)
	for _, row := range snapshots {
		if row.Price <= 0 || row.Amount <= 0 || row.IsST {
			continue
		}
		if strings.HasPrefix(strings.ToLower(row.Symbol), "bj") && !opts.IncludeBeijing {
			continue
		}
		if row.ChangePercent < opts.MinChangePct || row.ChangePercent > opts.MaxChangePct {
			continue
		}
		if row.VolumeRatio < opts.MinVolumeRatio {
			continue
		}
		if row.TurnoverRate < opts.MinTurnoverRate || row.TurnoverRate > opts.MaxTurnoverRate {
			continue
		}
		if row.FloatMarketCap < opts.MinFloatCap || row.FloatMarketCap > opts.MaxFloatCap {
			continue
		}
		ranked = append(ranked, row)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].ChangePercent == ranked[j].ChangePercent {
			return ranked[i].Amount > ranked[j].Amount
		}
		return ranked[i].ChangePercent > ranked[j].ChangePercent
	})
	result.CandidateCount = len(ranked) // 粗筛通过总数
	if len(ranked) > opts.RankLimit {
		ranked = ranked[:opts.RankLimit]
	}
	result.RankedCount = len(ranked)

	indexIntradayReturn, indexErr := a.loadIndexIntradayReturn()
	if indexErr != nil {
		result.Warning = combineWarnings(result.Warning, "上证分时强弱对比不可用："+indexErr.Error())
	}

	items := make([]models.LateDayChaseScannerItem, 0, opts.Limit)
	technicalChecked := 0
	technicalFailed := 0
	for rank, row := range ranked {

		industry := industryMap[row.Symbol]
		if industry == "" {
			industry = "未知"
		}

		daily, dailyErr := a.marketService.GetKLineData(row.Symbol, "1d", 45)
		intraday, intradayErr := a.marketService.GetKLineData(row.Symbol, "1m", 260)
		if dailyErr != nil || intradayErr != nil {
			technicalFailed++
			continue
		}
		technicalChecked++

		volumePassed, volumeReason, volumeRisk := evaluateLateDayVolumeStep(daily)
		maPassed, ma5, ma10, ma20, maReason, maRisk := evaluateLateDayMABullish(daily, row.Price)
		intradayEval := evaluateLateDayIntraday(intraday, indexIntradayReturn)
		intradayPassed := intradayEval.Passed
		if indexErr != nil {
			intradayPassed = intradayEval.AboveAvgPassed
			intradayEval.Reasons = append(intradayEval.Reasons, "大盘分时不可用，已仅按站上均价线判断")
		}

		if !volumePassed || !maPassed || !intradayPassed {
			continue
		}
		if opts.RequireBuySignal && !intradayEval.BuySignalReady {
			continue
		}

		triggers := []string{
			"涨幅3%-5%",
			"量比>=1",
			"换手5%-10%",
			"流通市值50-200亿",
			"成交量台阶式上升",
			"均线多头排列",
			"分时强于大盘",
		}
		if intradayEval.BuySignalReady {
			triggers = append(triggers, "尾盘新高回踩均价线不破")
		}

		reasons := make([]string, 0, 10)
		reasons = append(reasons,
			fmt.Sprintf("涨幅榜第%d名，涨幅 %.2f%%，量比 %.2f", rank+1, row.ChangePercent, row.VolumeRatio),
			fmt.Sprintf("换手 %.2f%%，流通市值 %.1f 亿，成交额 %s", row.TurnoverRate, row.FloatMarketCap/1e8, formatAmountCN(row.Amount)),
			volumeReason,
			maReason,
		)
		reasons = append(reasons, intradayEval.Reasons...)

		risks := make([]string, 0, 4)
		if volumeRisk != "" {
			risks = append(risks, volumeRisk)
		}
		if maRisk != "" {
			risks = append(risks, maRisk)
		}
		risks = append(risks, intradayEval.RiskFlags...)
		if row.ChangePercent >= opts.MaxChangePct-0.2 {
			risks = append(risks, "接近5%上限，避免情绪回落时追高")
		}
		if row.TurnoverRate >= opts.MaxTurnoverRate-0.5 {
			risks = append(risks, "换手接近10%，留意尾盘派发")
		}

		score := scoreLateDayChase(row, intradayEval, indexIntradayReturn, volumePassed, maPassed)
		item := models.LateDayChaseScannerItem{
			Symbol:                 row.Symbol,
			Name:                   row.Name,
			Rank:                   rank + 1,
			Price:                  row.Price,
			ChangePercent:          row.ChangePercent,
			VolumeRatio:            row.VolumeRatio,
			TurnoverRate:           row.TurnoverRate,
			Amount:                 row.Amount,
			TotalMarketCap:         row.TotalMarketCap,
			FloatMarketCap:         row.FloatMarketCap,
			Industry:               industry,
			Score:                  score,
			VolumeStepPassed:       volumePassed,
			MABullishPassed:        maPassed,
			IntradayStrengthPassed: intradayPassed,
			BuySignalReady:         intradayEval.BuySignalReady,
			IntradayAboveAvgRatio:  intradayEval.AboveAvgRatio,
			StockIntradayReturn:    intradayEval.StockReturn,
			IndexIntradayReturn:    indexIntradayReturn,
			MA5:                    ma5,
			MA10:                   ma10,
			MA20:                   ma20,
			LastHighTime:           intradayEval.LastHighTime,
			Triggers:               triggers,
			Reasons:                reasons,
			RiskFlags:              risks,
			BuyPointHint:           lateDayBuyPointHint(intradayEval.BuySignalReady),
			StopLossHint:           "次日不冲高或跌破尾盘均价线先减；跌破买入价-3%执行止损",
			UpdatedAt:              chooseFirstNonEmpty(row.UpdateTime, result.AsOf),
		}
		items = append(items, item)
	}

	result.CandidateCount = technicalChecked
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			if items[i].BuySignalReady == items[j].BuySignalReady {
				return items[i].VolumeRatio > items[j].VolumeRatio
			}
			return items[i].BuySignalReady
		}
		return items[i].Score > items[j].Score
	})
	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	result.Items = items
	result.SelectedCount = len(items)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf(
		"规则：全A粗筛涨幅%.1f%%~%.1f%%、量比>=%.1f、换手%.1f%%~%.1f%%、流通市值%.0f~%.0f亿 → 命中%d只，取前%d只技术验证（验证%d只，失败%d只）",
		opts.MinChangePct,
		opts.MaxChangePct,
		opts.MinVolumeRatio,
		opts.MinTurnoverRate,
		opts.MaxTurnoverRate,
		opts.MinFloatCap/1e8,
		opts.MaxFloatCap/1e8,
		result.CandidateCount,
		opts.RankLimit,
		technicalChecked,
		technicalFailed,
	))
	if opts.RequireBuySignal {
		result.Warning = combineWarnings(result.Warning, "已开启只看尾盘买点触发")
	}
	a.saveLateDayStrategyPicks("latechase-v3", "尾盘强势策略3", result)
	return result
}

// pushScannerSignals 把扫描入选标的作为买点信号推送。
// 后台异步执行、逐条节流，避免阻塞扫描返回与触发 Telegram 限流；防重由推送服务按股票去重。
func (a *App) pushScannerSignals(items []models.LowBuyScannerItem) {
	if a == nil || a.pushService == nil || len(items) == 0 {
		return
	}
	// 只推送评分最高的前 3 只（回测验证：集中前3质量优于摊薄前5；result.Items 已按评分降序）
	const maxPush = 3
	batch := items
	if len(batch) > maxPush {
		batch = batch[:maxPush]
	}
	go func() {
		for i, item := range batch {
			if strings.TrimSpace(item.Symbol) == "" {
				continue
			}
			a.pushService.Push(models.PushSignal{
				StockCode: item.Symbol,
				StockName: item.Name,
				Type:      models.PushTypeBuyPoint,
				Message:   buildScannerPushMessage(item),
				Level:     "active",
			})
			if i < len(batch)-1 {
				time.Sleep(400 * time.Millisecond) // 节流，规避 Telegram 同会话限流
			}
		}
	}()
}

// buildScannerPushMessage 组装扫描信号的推送正文。
func buildScannerPushMessage(item models.LowBuyScannerItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "现价 %.2f (%+.2f%%)", item.Price, item.ChangePercent)
	if strings.TrimSpace(item.Industry) != "" {
		fmt.Fprintf(&b, " · %s", item.Industry)
	}
	fmt.Fprintf(&b, "\n评分 %.1f · 命中 %d 条", item.Score, item.TriggerCount)
	if len(item.Reasons) > 0 {
		fmt.Fprintf(&b, "\n理由：%s", strings.Join(item.Reasons, "；"))
	}
	if strings.TrimSpace(item.BuyPointHint) != "" {
		fmt.Fprintf(&b, "\n买点：%s", item.BuyPointHint)
	}
	if strings.TrimSpace(item.SellPointHint) != "" {
		fmt.Fprintf(&b, "\n卖点：%s", item.SellPointHint)
	}
	if strings.TrimSpace(item.StopLossHint) != "" {
		fmt.Fprintf(&b, "\n止损：%s", item.StopLossHint)
	}
	if len(item.RiskFlags) > 0 {
		fmt.Fprintf(&b, "\n⚠️ %s", strings.Join(item.RiskFlags, "；"))
	}
	return b.String()
}

// GetStrategyNextDayReview 生成策略次日收盘复盘：读取昨日策略扫描留痕，再补今日K线、资金、大盘和快讯。
func (a *App) GetStrategyNextDayReview(req models.StrategyReviewRequest) models.StrategyReviewResult {
	if a == nil || a.historyService == nil {
		return models.StrategyReviewResult{
			StrategyID:   req.StrategyID,
			StrategyName: req.StrategyName,
			Warning:      "历史服务未初始化，无法生成策略复盘",
			Items:        []models.StrategyReviewItem{},
			Optimization: []string{},
		}
	}
	news := make([]models.StrategyReviewNews, 0, 20)
	if a.newsService != nil {
		if rows, err := a.newsService.GetTelegraphList(); err == nil {
			for _, row := range rows {
				news = append(news, models.StrategyReviewNews{Time: row.Time, Content: row.Content, URL: row.URL})
			}
		}
	}
	if strings.TrimSpace(req.StrategyName) == "" {
		req.StrategyName = strategyReviewName(req.StrategyID)
	}
	req.ReviewSymbols = a.strategyReviewPaperSymbols(req)
	a.seedStrategyReviewPicksFromPaper(req)
	result := a.historyService.BuildStrategyNextDayReview(req, news)
	a.enrichStrategyReviewBusiness(&result)
	return result
}

func (a *App) strategyReviewPaperSymbols(req models.StrategyReviewRequest) []string {
	if a == nil || a.paperService == nil {
		return nil
	}
	strategyID := strings.TrimSpace(req.StrategyID)
	signalDate := normalizeStrategyReviewDateInput(req.SignalDate)
	if strategyID == "" || signalDate == "" {
		return nil
	}
	positions := a.paperService.List()
	if len(positions) == 0 {
		return nil
	}
	seen := map[string]bool{}
	symbols := make([]string, 0)
	for _, p := range positions {
		if normalizeStrategyReviewDateInput(p.OpenDate) != signalDate {
			continue
		}
		if !paperSourceMatchesStrategy(strategyID, p.Source) {
			continue
		}
		code := normalizeJournalStockSymbol(p.Symbol)
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(p.Symbol))
		}
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		symbols = append(symbols, code)
	}
	return symbols
}

func (a *App) enrichStrategyReviewBusiness(result *models.StrategyReviewResult) {
	if a == nil || result == nil || len(result.Items) == 0 {
		return
	}
	industryMap := buildIndustryMapFromEmbedded()
	for i := range result.Items {
		code := normalizeJournalStockSymbol(result.Items[i].Symbol)
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(result.Items[i].Symbol))
		}
		if strings.TrimSpace(result.Items[i].Industry) == "" || result.Items[i].Industry == "未知" || result.Items[i].Industry == "行业未知" {
			if industry := strings.TrimSpace(industryMap[code]); industry != "" {
				result.Items[i].Industry = industry
			}
		}
	}
	if a.f10Service == nil {
		return
	}

	type businessFill struct {
		Index   int
		Summary string
		Source  string
	}
	sem := make(chan struct{}, 4)
	out := make(chan businessFill, len(result.Items))
	var wg sync.WaitGroup
	for i := range result.Items {
		code := normalizeJournalStockSymbol(result.Items[i].Symbol)
		if code == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, symbol string) {
			defer wg.Done()
			sem <- struct{}{}
			summary, source := a.lookupStrategyReviewBusinessSummary(symbol)
			<-sem
			if summary != "" {
				out <- businessFill{Index: idx, Summary: summary, Source: source}
			}
		}(i, code)
	}
	wg.Wait()
	close(out)
	for fill := range out {
		if fill.Index >= 0 && fill.Index < len(result.Items) {
			result.Items[fill.Index].BusinessSummary = fill.Summary
			result.Items[fill.Index].BusinessSource = fill.Source
		}
	}
}

func (a *App) lookupStrategyReviewBusinessSummary(code string) (string, string) {
	if a == nil || a.f10Service == nil || strings.TrimSpace(code) == "" {
		return "", ""
	}
	if business, err := a.f10Service.GetBusinessAnalysisByCode(code); err == nil {
		if summary := extractStrategyReviewBusinessScope(business.Scope); summary != "" {
			return summary, "F10经营分析"
		}
	}
	if company, err := a.f10Service.GetCompanySurveyByCode(code); err == nil {
		if summary := extractStrategyReviewCompanyBusiness(company); summary != "" {
			return summary, "F10公司概况"
		}
	}
	return "", ""
}

func extractStrategyReviewBusinessScope(rows []map[string]any) string {
	for _, row := range rows {
		if normalized, ok := row["normalized"].(map[string]any); ok {
			if summary := cleanStrategyReviewBusinessText(strategyReviewAnyString(normalized["businessScope"])); summary != "" {
				return summary
			}
		}
		for _, key := range []string{"businessScope", "BUSINESS_SCOPE", "MAIN_BUSINESS", "ZYFW"} {
			if summary := cleanStrategyReviewBusinessText(strategyReviewAnyString(row[key])); summary != "" {
				return summary
			}
		}
	}
	return ""
}

func extractStrategyReviewCompanyBusiness(company map[string]any) string {
	for _, key := range []string{"MAIN_BUSINESS", "BUSINESS_SCOPE", "MAINBUSINESS", "ZYFW", "经营范围", "主营业务"} {
		if summary := cleanStrategyReviewBusinessText(strategyReviewAnyString(company[key])); summary != "" {
			return summary
		}
	}
	return ""
}

func cleanStrategyReviewBusinessText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || text == "-" || text == "--" {
		return ""
	}
	const maxRunes = 96
	runes := []rune(text)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return text
}

func strategyReviewAnyString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case float64:
		if math.Mod(v, 1) == 0 {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%g", v)
	case float32:
		f := float64(v)
		if math.Mod(f, 1) == 0 {
			return fmt.Sprintf("%.0f", f)
		}
		return fmt.Sprintf("%g", f)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (a *App) seedStrategyReviewPicksFromPaper(req models.StrategyReviewRequest) {
	if a == nil || a.historyService == nil || a.paperService == nil {
		return
	}
	strategyID := strings.TrimSpace(req.StrategyID)
	signalDate := normalizeStrategyReviewDateInput(req.SignalDate)
	if strategyID == "" || signalDate == "" {
		return
	}
	positions := a.paperService.List()
	if len(positions) == 0 {
		return
	}
	picks := make([]models.StrategyPickSnapshot, 0)
	seen := map[string]bool{}
	for _, p := range positions {
		if normalizeStrategyReviewDateInput(p.OpenDate) != signalDate {
			continue
		}
		if !paperSourceMatchesStrategy(strategyID, p.Source) {
			continue
		}
		code := normalizeJournalStockSymbol(p.Symbol)
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(p.Symbol))
		}
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		price := p.CostPrice
		if price <= 0 {
			price = p.OpenPrice
		}
		reasons := []string{fmt.Sprintf("来自模拟持仓记录：%s 加入，来源 %s", signalDate, chooseFirstNonEmpty(p.Source, strategyID))}
		if p.Status == "closed" {
			reasons = append(reasons, "该模拟持仓已平仓")
		}
		picks = append(picks, models.StrategyPickSnapshot{
			StrategyID:     strategyID,
			StrategyName:   strategyReviewName(strategyID),
			SignalDate:     signalDate,
			ScannedAt:      signalDate,
			Symbol:         code,
			Name:           p.Name,
			Rank:           len(picks) + 1,
			Price:          price,
			Score:          0,
			MainFlowSource: "paper-position",
			Triggers:       []string{"模拟持仓留痕"},
			Reasons:        reasons,
		})
	}
	if len(picks) == 0 {
		return
	}
	if err := a.historyService.SaveMissingStrategyPicks(strategyID, strategyReviewName(strategyID), signalDate, signalDate, picks); err != nil {
		log.Warn("从模拟持仓补齐策略复盘留痕失败[%s %s]: %v", strategyID, signalDate, err)
		return
	}
	log.Info("从模拟持仓补齐缺失策略复盘留痕[%s %s] %d只", strategyID, signalDate, len(picks))
}

func normalizeStrategyReviewDateInput(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "/", "-")
	if len(raw) >= 10 {
		candidate := raw[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	for _, layout := range []string{"2006-1-2", "20060102"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return raw
}

func paperSourceMatchesStrategy(strategyID string, source string) bool {
	strategyID = strings.ToLower(strings.TrimSpace(strategyID))
	source = strings.ToLower(strings.TrimSpace(source))
	if strategyID == "" || source == "" {
		return false
	}
	if strategyID == source {
		return true
	}
	aliases := map[string][]string{
		"lowbuy-v1":                 {"lowbuy", "低吸1", "低吸选股策略1"},
		"limit-pullback-v1":         {"limit-pullback", "涨停回调", "涨停回调低吸4"},
		"triple-volume-v5":          {"triple-volume", "三倍量", "三倍量策略5"},
		"tail-buy-v6":               {"tail-buy", "尾盘买入", "尾盘买入策略6"},
		"hot-money-v7":              {"hot-money", "游资突破", "游资突破策略7"},
		"dip-entry-v8":              {"dip-entry", "低吸入场", "低吸入场策略8"},
		"monster-v9":                {"monster", "捉妖", "捉妖策略9", "捉妖策略6"},
		"monster-v10":               {"monster-v10", "捉妖10", "捉妖策略10"},
		"taillazy-v2":               {"taillazy", "低吸2", "低吸尾盘策略2"},
		"latechase-v3":              {"latechase", "尾盘3", "尾盘强势策略3"},
		"wave-v1":                   {"wave", "波段", "波段策略1.0"},
		"caoyuan-standard4a":        {"草元标准", "草元标准4a", "草元标准 4a"},
		"caoyuan-standard4a-strict": {"草元标准", "草元标准严选", "草元标准4a严选", "草元标准 4a 严选"},
		"caoyuan-zhuang4b":          {"草元抓庄", "草元抓庄4b", "草元抓庄 4b"},
		"caoyuan-zhuang4b-strict":   {"草元抓庄", "草元抓庄严选", "草元抓庄4b严选", "草元抓庄 4b 严选"},
	}
	for _, alias := range aliases[strategyID] {
		if source == alias {
			return true
		}
	}
	return false
}

func (a *App) saveLowBuyStrategyPicks(strategyID string, strategyName string, result models.LowBuyScannerResult) {
	if a == nil || a.historyService == nil || len(result.Items) == 0 {
		return
	}
	picks := make([]models.StrategyPickSnapshot, 0, len(result.Items))
	for idx, item := range result.Items {
		picks = append(picks, models.StrategyPickSnapshot{
			StrategyID:       strategyID,
			StrategyName:     strategyName,
			SignalDate:       result.AsOf,
			ScannedAt:        result.AsOf,
			Symbol:           item.Symbol,
			Name:             item.Name,
			Rank:             idx + 1,
			Price:            item.Price,
			ChangePercent:    item.ChangePercent,
			Score:            item.Score,
			Industry:         item.Industry,
			Amount:           item.Amount,
			TurnoverRate:     item.TurnoverRate,
			MainNetInflow:    item.MainNetInflow,
			MainNetInflowPct: item.MainNetInflowRatio,
			MainFlowSource:   item.MainFlowSource,
			Triggers:         item.Triggers,
			Reasons:          item.Reasons,
			RiskFlags:        item.RiskFlags,
		})
	}
	if err := a.historyService.SaveStrategyPicks(strategyID, strategyName, result.AsOf, result.AsOf, picks); err != nil {
		log.Warn("保存策略扫描留痕失败[%s]: %v", strategyID, err)
	}
}

func (a *App) saveLateDayStrategyPicks(strategyID string, strategyName string, result models.LateDayChaseScannerResult) {
	if a == nil || a.historyService == nil || len(result.Items) == 0 {
		return
	}
	picks := make([]models.StrategyPickSnapshot, 0, len(result.Items))
	for idx, item := range result.Items {
		picks = append(picks, models.StrategyPickSnapshot{
			StrategyID:     strategyID,
			StrategyName:   strategyName,
			SignalDate:     result.AsOf,
			ScannedAt:      result.AsOf,
			Symbol:         item.Symbol,
			Name:           item.Name,
			Rank:           idx + 1,
			Price:          item.Price,
			ChangePercent:  item.ChangePercent,
			Score:          item.Score,
			Industry:       item.Industry,
			Amount:         item.Amount,
			TurnoverRate:   item.TurnoverRate,
			Triggers:       item.Triggers,
			Reasons:        item.Reasons,
			RiskFlags:      item.RiskFlags,
			MainFlowSource: "scan-snapshot",
		})
	}
	if err := a.historyService.SaveStrategyPicks(strategyID, strategyName, result.AsOf, result.AsOf, picks); err != nil {
		log.Warn("保存策略扫描留痕失败[%s]: %v", strategyID, err)
	}
}

func (a *App) saveWaveStrategyPicks(strategyID string, strategyName string, result models.WaveScanResult) {
	if a == nil || a.historyService == nil || len(result.Items) == 0 {
		return
	}
	signalDate := chooseFirstNonEmpty(result.AsOf, result.SnapshotAsOf, time.Now().Format("2006-01-02"))
	picks := make([]models.StrategyPickSnapshot, 0, len(result.Items))
	for idx, item := range result.Items {
		reasons := append([]string{}, item.Reasons...)
		if result.GateBypassed {
			reasons = append(reasons, "临时打开大盘闸门筛选，仅作观察留痕")
		}
		if item.Phase != "" {
			reasons = append(reasons, "阶段："+item.Phase)
		}
		triggers := make([]string, 0, 8)
		if result.GateBypassed {
			triggers = append(triggers, "临时开闸")
		}
		if item.EatFish {
			triggers = append(triggers, "吃鱼身")
		}
		if item.MainOpenFish {
			triggers = append(triggers, "开仓吃鱼")
		}
		if item.RelaxedIgnite {
			triggers = append(triggers, "异动现主力进")
		}
		if item.StrictIgnite {
			triggers = append(triggers, "异动起爆")
		}
		if item.StrongSignal {
			triggers = append(triggers, fmt.Sprintf("%s信号", item.Level))
		}
		if item.GZ {
			triggers = append(triggers, "五灯共振")
		}
		picks = append(picks, models.StrategyPickSnapshot{
			StrategyID:       strategyID,
			StrategyName:     strategyName,
			SignalDate:       signalDate,
			ScannedAt:        result.AsOf,
			Symbol:           item.Code,
			Name:             item.Name,
			Rank:             idx + 1,
			Price:            item.Price,
			Score:            item.Score,
			MainNetInflowPct: item.Kongpan,
			MainFlowSource:   "wave-kongpan-proxy",
			Triggers:         triggers,
			Reasons:          reasons,
			RiskFlags:        item.Risks,
		})
	}
	if err := a.historyService.SaveStrategyPicks(strategyID, strategyName, signalDate, result.AsOf, picks); err != nil {
		log.Warn("保存策略扫描留痕失败[%s]: %v", strategyID, err)
	}
}

func strategyReviewName(strategyID string) string {
	switch strings.TrimSpace(strategyID) {
	case "lowbuy-v1":
		return "低吸选股策略1"
	case "taillazy-v2":
		return "低吸尾盘策略2"
	case "latechase-v3":
		return "尾盘强势策略3"
	case "limit-pullback-v1":
		return "涨停回调低吸4"
	case "triple-volume-v5":
		return "三倍量策略5"
	case "tail-buy-v6":
		return "尾盘买入策略6"
	case "hot-money-v7":
		return "游资突破策略7"
	case "dip-entry-v8":
		return "低吸入场策略8"
	case "monster-v9":
		return "捉妖策略9"
	case "monster-v10":
		return "捉妖策略10"
	case "caoyuan-standard4a":
		return "草元标准 4A"
	case "caoyuan-standard4a-strict":
		return "草元标准 4A 严选"
	case "caoyuan-zhuang4b":
		return "草元抓庄 4B"
	case "caoyuan-zhuang4b-strict":
		return "草元抓庄 4B 严选"
	case "wave-v1":
		return "波段策略 1.0"
	default:
		return strategyID
	}
}

// GetF10Overview 获取 F10 综合数据
func (a *App) GetF10Overview(code string) models.F10Overview {
	if a.f10Service == nil {
		return models.F10Overview{
			Code:   code,
			Errors: map[string]string{"service": "F10 服务未初始化"},
		}
	}
	result, err := a.f10Service.GetOverview(code)
	if err != nil {
		return models.F10Overview{
			Code:   code,
			Errors: map[string]string{"request": err.Error()},
		}
	}
	return result
}

func classifyCapBucket(totalCap float64) string {
	switch {
	case totalCap < 50e8:
		return "微盘"
	case totalCap < 100e8:
		return "小盘"
	case totalCap < 300e8:
		return "中小盘"
	case totalCap < 800e8:
		return "中盘"
	case totalCap < 2000e8:
		return "大盘"
	default:
		return "超大盘"
	}
}

func hitKeyword(text string, keywords []string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, kw := range keywords {
		if strings.Contains(t, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func isLowBuyBlockedBoard(symbol string) bool {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) >= 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		code = code[2:]
	}
	// 剔除创业板(30x:300/301/302)与科创板(68x:688/689)
	return strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68")
}

func clamp(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func chooseFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func combineWarnings(curr string, next string) string {
	if strings.TrimSpace(curr) == "" {
		return next
	}
	if strings.TrimSpace(next) == "" {
		return curr
	}
	return curr + "；" + next
}

func hasMainFlowMissing(items []models.LowBuyScannerItem) bool {
	for _, item := range items {
		if math.IsNaN(item.MainNetInflow) || math.IsNaN(item.MainNetInflowRatio) {
			return true
		}
	}
	return false
}

func usedMainFlowProxy(items []models.LowBuyScannerItem) bool {
	for _, item := range items {
		if item.MainFlowSource == "tencent-pk-proxy" {
			return true
		}
	}
	return false
}

func usedQQFundFlow(items []models.LowBuyScannerItem) bool {
	for _, item := range items {
		if item.MainFlowSource == "tencent-fundflow" {
			return true
		}
	}
	return false
}

type lowBuyHistoryOptions struct {
	Enabled              bool
	TurnoverDays         int
	TurnoverMax          float64
	MainFlowDays         int
	MainFlowPositiveDays int
	MAPeriod             int
}

type lateDayChaseOptions struct {
	Limit            int
	RankLimit        int
	IncludeBeijing   bool
	MinChangePct     float64
	MaxChangePct     float64
	MinVolumeRatio   float64
	MinTurnoverRate  float64
	MaxTurnoverRate  float64
	MinFloatCap      float64
	MaxFloatCap      float64
	RequireBuySignal bool
}

type lateDayIntradayEval struct {
	Passed         bool
	AboveAvgPassed bool
	BuySignalReady bool
	AboveAvgRatio  float64
	StockReturn    float64
	LastHighTime   string
	Reasons        []string
	RiskFlags      []string
}

func normalizeLateDayChaseRequest(req models.LateDayChaseScannerRequest) lateDayChaseOptions {
	opts := lateDayChaseOptions{
		Limit:            req.Limit,
		RankLimit:        req.RankLimit,
		IncludeBeijing:   req.IncludeBeijing,
		MinChangePct:     req.MinChangePct,
		MaxChangePct:     req.MaxChangePct,
		MinVolumeRatio:   req.MinVolumeRatio,
		MinTurnoverRate:  req.MinTurnoverRate,
		MaxTurnoverRate:  req.MaxTurnoverRate,
		MinFloatCap:      req.MinFloatCap,
		MaxFloatCap:      req.MaxFloatCap,
		RequireBuySignal: req.RequireBuySignal,
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}
	if opts.RankLimit <= 0 {
		opts.RankLimit = 60
	}
	if opts.RankLimit > 300 {
		opts.RankLimit = 300
	}
	if opts.MinChangePct == 0 {
		opts.MinChangePct = 3.0
	}
	if opts.MaxChangePct == 0 {
		opts.MaxChangePct = 5.0
	}
	if opts.MaxChangePct > 5.0 {
		opts.MaxChangePct = 5.0
	}
	if opts.MinChangePct > opts.MaxChangePct {
		opts.MinChangePct = opts.MaxChangePct
	}
	if opts.MinVolumeRatio <= 0 {
		opts.MinVolumeRatio = 1.0
	}
	if opts.MinTurnoverRate <= 0 {
		opts.MinTurnoverRate = 5.0
	}
	if opts.MaxTurnoverRate <= 0 {
		opts.MaxTurnoverRate = 10.0
	}
	if opts.MinTurnoverRate > opts.MaxTurnoverRate {
		opts.MinTurnoverRate = opts.MaxTurnoverRate
	}
	if opts.MinFloatCap <= 0 {
		opts.MinFloatCap = 50e8
	}
	if opts.MaxFloatCap <= 0 {
		opts.MaxFloatCap = 200e8
	}
	if opts.MinFloatCap > opts.MaxFloatCap {
		opts.MinFloatCap = opts.MaxFloatCap
	}
	return opts
}

func (a *App) loadIndexIntradayReturn() (float64, error) {
	klines, err := a.marketService.GetKLineData("sh000001", "1m", 260)
	if err != nil {
		return 0, err
	}
	return computeIntradayReturn(klines)
}

func computeIntradayReturn(klines []models.KLineData) (float64, error) {
	if len(klines) == 0 {
		return 0, fmt.Errorf("分时为空")
	}
	first := klines[0].Open
	if first <= 0 {
		first = klines[0].Close
	}
	last := klines[len(klines)-1].Close
	if first <= 0 || last <= 0 {
		return 0, fmt.Errorf("分时价格为空")
	}
	return (last/first - 1) * 100, nil
}

func evaluateLateDayVolumeStep(klines []models.KLineData) (bool, string, string) {
	if len(klines) < 8 {
		return false, "", "日K数量不足，无法判断成交量台阶"
	}
	recent := klines[len(klines)-4:]
	early := averageKLineVolume(klines[len(klines)-8 : len(klines)-4])
	firstHalf := averageKLineVolume(recent[:2])
	secondHalf := averageKLineVolume(recent[2:])
	latest := float64(recent[len(recent)-1].Volume)
	if early <= 0 || firstHalf <= 0 || secondHalf <= 0 || latest <= 0 {
		return false, "", "成交量数据缺失"
	}
	passed := firstHalf > early*1.05 && secondHalf > firstHalf*1.05 && latest >= firstHalf*0.95
	reason := fmt.Sprintf("成交量台阶：前4日均量 %.0f，近前2日均量 %.0f，近后2日均量 %.0f", early, firstHalf, secondHalf)
	if !passed {
		return false, reason, "成交量未呈台阶式放大"
	}
	return true, reason, ""
}

func averageKLineVolume(klines []models.KLineData) float64 {
	if len(klines) == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for _, k := range klines {
		if k.Volume <= 0 {
			continue
		}
		sum += float64(k.Volume)
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func evaluateLateDayMABullish(klines []models.KLineData, price float64) (bool, float64, float64, float64, string, string) {
	if len(klines) < 20 {
		return false, 0, 0, 0, "", "日K不足20根，无法判断均线"
	}
	ma5 := simpleMA(klines, 5)
	ma10 := simpleMA(klines, 10)
	ma20 := simpleMA(klines, 20)
	if ma5 <= 0 || ma10 <= 0 || ma20 <= 0 || price <= 0 {
		return false, ma5, ma10, ma20, "", "均线或价格数据缺失"
	}
	aboveMA := price > ma5 && price > ma10 && price > ma20
	bullish := ma5 > ma10 && ma10 > ma20
	reason := fmt.Sprintf("均线：现价 %.2f > MA5 %.2f / MA10 %.2f / MA20 %.2f，MA5>MA10>MA20", price, ma5, ma10, ma20)
	if !aboveMA || !bullish {
		risk := "均线未形成多头排列"
		if !aboveMA {
			risk = "股价未站上关键均线"
		}
		return false, ma5, ma10, ma20, reason, risk
	}
	return true, ma5, ma10, ma20, reason, ""
}

func simpleMA(klines []models.KLineData, period int) float64 {
	if period <= 0 || len(klines) < period {
		return 0
	}
	sum := 0.0
	for _, k := range klines[len(klines)-period:] {
		if k.Close <= 0 {
			return 0
		}
		sum += k.Close
	}
	return sum / float64(period)
}

func evaluateLateDayIntraday(klines []models.KLineData, indexReturn float64) lateDayIntradayEval {
	eval := lateDayIntradayEval{
		Reasons:   make([]string, 0, 3),
		RiskFlags: make([]string, 0, 3),
	}
	if len(klines) < 20 {
		eval.RiskFlags = append(eval.RiskFlags, "分时数据不足")
		return eval
	}

	aboveCount := 0
	validCount := 0
	lateStart := 0
	for i, k := range klines {
		if isLateDayMinute(k.Time) {
			lateStart = i
			break
		}
	}
	if lateStart == 0 && len(klines) > 30 {
		lateStart = len(klines) - 30
	}

	dayHigh := -math.MaxFloat64
	lastHighIndex := -1
	for i, k := range klines {
		if k.Close > 0 && k.Avg > 0 {
			validCount++
			if k.Close >= k.Avg {
				aboveCount++
			}
		}
		if k.High > dayHigh {
			dayHigh = k.High
			lastHighIndex = i
		}
	}
	if validCount > 0 {
		eval.AboveAvgRatio = float64(aboveCount) / float64(validCount)
	}
	eval.AboveAvgPassed = eval.AboveAvgRatio >= 0.85
	if lastHighIndex >= 0 {
		eval.LastHighTime = klines[lastHighIndex].Time
	}
	if r, err := computeIntradayReturn(klines); err == nil {
		eval.StockReturn = r
	}
	strongerThanIndex := eval.StockReturn > indexReturn+0.5
	eval.Passed = eval.AboveAvgPassed && strongerThanIndex
	eval.Reasons = append(eval.Reasons, fmt.Sprintf(
		"分时：%.0f%% 时间站上均价线，个股分时 %.2f%% vs 上证 %.2f%%",
		eval.AboveAvgRatio*100,
		eval.StockReturn,
		indexReturn,
	))

	if !eval.AboveAvgPassed {
		eval.RiskFlags = append(eval.RiskFlags, "分时未全天站稳均价线")
	}
	if !strongerThanIndex {
		eval.RiskFlags = append(eval.RiskFlags, "分时强度未明显强于上证")
	}

	eval.BuySignalReady = detectLateDayBuySignal(klines, lateStart)
	if eval.BuySignalReady {
		eval.Reasons = append(eval.Reasons, "尾盘出现日内新高后回踩均价线不破")
	} else {
		eval.Reasons = append(eval.Reasons, "尾盘买点未完全触发，建议等新高回踩均价线不破")
	}
	return eval
}

func isLateDayMinute(text string) bool {
	if len(text) < 16 {
		return false
	}
	tail := text[len(text)-8:]
	if len(tail) < 5 {
		return false
	}
	hhmm := tail[:5]
	return hhmm >= "14:30" && hhmm <= "15:00"
}

func detectLateDayBuySignal(klines []models.KLineData, lateStart int) bool {
	if len(klines) < 8 {
		return false
	}
	if lateStart < 1 {
		lateStart = len(klines) - 30
		if lateStart < 1 {
			lateStart = 1
		}
	}
	prevHigh := -math.MaxFloat64
	for _, k := range klines[:lateStart] {
		if k.High > prevHigh {
			prevHigh = k.High
		}
	}
	newHighIndex := -1
	for i := lateStart; i < len(klines); i++ {
		if klines[i].High > prevHigh && klines[i].Close > 0 {
			newHighIndex = i
			break
		}
	}
	if newHighIndex < 0 || newHighIndex >= len(klines)-1 {
		return false
	}
	for _, k := range klines[newHighIndex+1:] {
		if k.Avg <= 0 || k.Close <= 0 || k.Low <= 0 {
			continue
		}
		nearAvg := math.Abs(k.Low-k.Avg)/k.Avg <= 0.006
		holdsAvg := k.Close >= k.Avg
		if nearAvg && holdsAvg {
			return true
		}
	}
	return false
}

func scoreLateDayChase(row services.ScanSnapshotRow, intraday lateDayIntradayEval, indexReturn float64, volumePassed bool, maPassed bool) float64 {
	score := 50.0
	score += clamp((row.ChangePercent-3.0)*8.0, 0, 16)
	score += clamp((row.VolumeRatio-1.0)*8.0, 0, 14)
	score += clamp((row.TurnoverRate-5.0)*2.0, 0, 10)
	score += clamp((200e8-row.FloatMarketCap)/150e8*8.0, 0, 8)
	score += clamp(intraday.AboveAvgRatio*12.0, 0, 12)
	score += clamp((intraday.StockReturn-indexReturn)*2.0, -4, 8)
	if volumePassed {
		score += 6
	}
	if maPassed {
		score += 8
	}
	if intraday.BuySignalReady {
		score += 10
	}
	if row.ChangePercent > 4.8 {
		score -= 4
	}
	if row.TurnoverRate > 9.5 {
		score -= 4
	}
	return math.Round(clamp(score, 0, 100)*10) / 10
}

func lateDayBuyPointHint(ready bool) string {
	if ready {
		return "尾盘新高后回踩均价线不破，可按计划小仓试错"
	}
	return "继续等：尾盘创日内新高后，回踩黄色均价线不破再考虑"
}

func formatAmountCN(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	abs := math.Abs(value)
	sign := ""
	if value < 0 {
		sign = "-"
	}
	switch {
	case abs >= 1e8:
		return fmt.Sprintf("%s%.2f亿", sign, abs/1e8)
	case abs >= 1e4:
		return fmt.Sprintf("%s%.2f万", sign, abs/1e4)
	default:
		return fmt.Sprintf("%s%.0f", sign, abs)
	}
}

type lowBuyStockHistory struct {
	Turnovers      []float64
	MainNetInflows []float64
	ClosePrice     float64
	MA             float64
	MAPeriod       int
}

func normalizeLowBuyHistoryOptions(req models.LowBuyScannerRequest) lowBuyHistoryOptions {
	opts := lowBuyHistoryOptions{
		Enabled:              req.HistoryFilterEnabled,
		TurnoverDays:         req.HistoryTurnoverDays,
		TurnoverMax:          req.HistoryTurnoverMax,
		MainFlowDays:         req.HistoryMainFlowDays,
		MainFlowPositiveDays: req.HistoryMainFlowPositiveDays,
		MAPeriod:             req.HistoryMAPeriod,
	}
	if opts.TurnoverDays <= 0 {
		opts.TurnoverDays = 3
	}
	if opts.TurnoverDays > 20 {
		opts.TurnoverDays = 20
	}
	if opts.TurnoverMax <= 0 {
		opts.TurnoverMax = 3
	}
	if opts.TurnoverMax > 20 {
		opts.TurnoverMax = 20
	}
	if opts.MainFlowDays <= 0 {
		opts.MainFlowDays = 3
	}
	if opts.MainFlowDays > 20 {
		opts.MainFlowDays = 20
	}
	if opts.MainFlowPositiveDays <= 0 {
		opts.MainFlowPositiveDays = 2
	}
	if opts.MainFlowPositiveDays > opts.MainFlowDays {
		opts.MainFlowPositiveDays = opts.MainFlowDays
	}
	if opts.MAPeriod <= 0 {
		opts.MAPeriod = 10
	}
	if opts.MAPeriod > 60 {
		opts.MAPeriod = 60
	}
	return opts
}

func (a *App) loadLowBuyStockHistory(row services.ScanSnapshotRow, opts lowBuyHistoryOptions) (lowBuyStockHistory, error) {
	needKDays := opts.MAPeriod
	if opts.TurnoverDays > needKDays {
		needKDays = opts.TurnoverDays
	}
	needKDays += 8
	klines, err := a.marketService.GetKLineData(row.Symbol, "1d", needKDays)
	if err != nil {
		return lowBuyStockHistory{}, fmt.Errorf("日K拉取失败: %w", err)
	}
	if len(klines) < opts.MAPeriod || len(klines) < opts.TurnoverDays {
		return lowBuyStockHistory{}, fmt.Errorf("日K数量不足")
	}
	if row.FloatMarketCap <= 0 {
		return lowBuyStockHistory{}, fmt.Errorf("流通市值为空，无法估算历史换手")
	}

	turnovers := make([]float64, 0, opts.TurnoverDays)
	for _, k := range klines[len(klines)-opts.TurnoverDays:] {
		if k.Amount <= 0 || k.Close <= 0 {
			return lowBuyStockHistory{}, fmt.Errorf("日K成交额/收盘价为空，无法估算历史换手")
		}
		turnovers = append(turnovers, (k.Amount/row.FloatMarketCap)*100)
	}

	ma, closePrice, ok := computeLowBuyMA(klines, opts.MAPeriod)
	if !ok {
		return lowBuyStockHistory{}, fmt.Errorf("MA%d计算失败", opts.MAPeriod)
	}

	flows, err := a.marketService.GetQQMainFlowHistory(row.Symbol, opts.MainFlowDays)
	if err != nil {
		return lowBuyStockHistory{}, fmt.Errorf("历史主力拉取失败: %w", err)
	}
	if len(flows) < opts.MainFlowDays {
		return lowBuyStockHistory{}, fmt.Errorf("历史主力数量不足")
	}
	mainFlows := make([]float64, 0, opts.MainFlowDays)
	for _, p := range flows[len(flows)-opts.MainFlowDays:] {
		mainFlows = append(mainFlows, p.MainNetInflow)
	}

	return lowBuyStockHistory{
		Turnovers:      turnovers,
		MainNetInflows: mainFlows,
		ClosePrice:     closePrice,
		MA:             ma,
		MAPeriod:       opts.MAPeriod,
	}, nil
}

func evaluateLowBuyHistory(history lowBuyStockHistory, opts lowBuyHistoryOptions) ([]string, []string, []string, bool) {
	triggers := make([]string, 0, 3)
	reasons := make([]string, 0, 3)
	risks := make([]string, 0, 2)

	turnoverPassed := true
	maxTurnover := 0.0
	for _, t := range history.Turnovers {
		if t > maxTurnover {
			maxTurnover = t
		}
		if t >= opts.TurnoverMax {
			turnoverPassed = false
		}
	}
	if turnoverPassed {
		triggers = append(triggers, fmt.Sprintf("连续%d日换手<%.2f%%", opts.TurnoverDays, opts.TurnoverMax))
		reasons = append(reasons, fmt.Sprintf("历史换手验证：近%d日估算最大换手 %.2f%%，低于 %.2f%%", opts.TurnoverDays, maxTurnover, opts.TurnoverMax))
	} else {
		risks = append(risks, fmt.Sprintf("历史换手未收敛(最大%.2f%%)", maxTurnover))
	}

	positiveDays := 0
	for _, flow := range history.MainNetInflows {
		if flow > 0 {
			positiveDays++
		}
	}
	flowPassed := positiveDays >= opts.MainFlowPositiveDays
	if flowPassed {
		triggers = append(triggers, fmt.Sprintf("近%d日主力%d日为正", opts.MainFlowDays, positiveDays))
		reasons = append(reasons, fmt.Sprintf("历史主力验证：近%d日主力净流入为正 %d 日，要求至少 %d 日", opts.MainFlowDays, positiveDays, opts.MainFlowPositiveDays))
	} else {
		risks = append(risks, fmt.Sprintf("历史主力连续性不足(%d/%d)", positiveDays, opts.MainFlowDays))
	}

	maPassed := history.ClosePrice > history.MA
	if maPassed {
		triggers = append(triggers, fmt.Sprintf("站上MA%d", opts.MAPeriod))
		reasons = append(reasons, fmt.Sprintf("均线验证：收盘 %.2f > MA%d %.2f", history.ClosePrice, opts.MAPeriod, history.MA))
	} else {
		risks = append(risks, fmt.Sprintf("未站上MA%d", opts.MAPeriod))
	}

	return triggers, reasons, risks, turnoverPassed && flowPassed && maPassed
}

func computeLowBuyMA(klines []models.KLineData, period int) (float64, float64, bool) {
	if period <= 0 || len(klines) < period {
		return 0, 0, false
	}
	start := len(klines) - period
	sum := 0.0
	for _, k := range klines[start:] {
		if k.Close <= 0 {
			return 0, 0, false
		}
		sum += k.Close
	}
	closePrice := klines[len(klines)-1].Close
	return sum / float64(period), closePrice, closePrice > 0
}

// fillLowBuyMA10Status 为展示候选并发计算 MA10 状态（站上/跌破），仅作界面提示，不淘汰。
func (a *App) fillLowBuyMA10Status(items []models.LowBuyScannerItem) {
	if a == nil || a.marketService == nil || len(items) == 0 {
		return
	}
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			klines, err := a.marketService.GetKLineData(items[idx].Symbol, "1d", 15)
			if err != nil || len(klines) < 10 {
				return
			}
			ma, closePrice, ok := computeLowBuyMA(klines, 10)
			if !ok || ma <= 0 {
				return
			}
			items[idx].MA10 = ma
			if closePrice >= ma {
				items[idx].MA10Status = "hold" // 站上(未破)
			} else {
				items[idx].MA10Status = "broke" // 跌破
			}
		}(i)
	}
	wg.Wait()
}

type limitPullbackMetrics struct {
	LimitIndex        int
	DaysSinceLimit    int
	LimitPct          float64
	LimitVolumeRatio  float64
	LimitHigh         float64
	LimitLow          float64
	LimitClose        float64
	LimitAmount       float64
	PullbackMinLow    float64
	PullbackMaxHigh   float64
	PullbackAvgAmount float64
	AmountShrinkRatio float64
	Close             float64
	MA5               float64
	MA10              float64
	MA20              float64
	MA30              float64
	MA60              float64
	NearMA5Pct        float64
	NearMA10Pct       float64
	BreakoutHigh      bool
	StandingMA5       bool
	StandingMA10      bool
	HalfPosition      float64
	HalfPositionHeld  bool
	StopLoss          float64
	PullbackRangePct  float64
	CurrentChangePct  float64
	Trend20Pct        float64
	MA20SlopePct      float64
	TrendGateReason   string
}

func evaluateLimitPullbackRow(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	m, ok := buildLimitPullbackMetrics(row, daily)
	if !ok {
		return models.LowBuyScannerItem{}, false
	}

	triggers := make([]string, 0, 8)
	reasons := make([]string, 0, 8)
	risks := make([]string, 0, 4)
	score := 45.0

	triggers = append(triggers, fmt.Sprintf("%d日前涨停/准涨停", m.DaysSinceLimit))
	triggers = append(triggers, "趋势闸门通过")
	score += 14
	if m.LimitVolumeRatio >= 1.5 {
		triggers = append(triggers, "启动日放量>=1.5倍")
		score += clamp((m.LimitVolumeRatio-1.0)*6, 4, 12)
	}
	if m.AmountShrinkRatio > 0 && m.AmountShrinkRatio <= 0.72 {
		triggers = append(triggers, "回调缩量")
		score += clamp((0.85-m.AmountShrinkRatio)*30, 4, 12)
	}
	if m.StandingMA5 {
		triggers = append(triggers, "站稳MA5")
		score += 8
	}
	if m.StandingMA10 {
		triggers = append(triggers, "站稳MA10")
		score += 8
	}
	if m.HalfPositionHeld {
		triggers = append(triggers, "未破涨停半分位")
		score += 8
	}
	if m.NearMA5Pct <= 2.5 || m.NearMA10Pct <= 2.5 {
		triggers = append(triggers, "贴近均线低吸区")
		score += 7
	}
	if m.BreakoutHigh {
		triggers = append(triggers, "放量突破回调高点")
		score += 10
	}

	if len(triggers) < 5 {
		return models.LowBuyScannerItem{}, false
	}

	switch {
	case m.CurrentChangePct >= -2.5 && m.CurrentChangePct <= 2.5:
		score += 6
	case m.CurrentChangePct > 2.5 && m.CurrentChangePct <= 5.5:
		score -= 4
	case m.CurrentChangePct < -2.5:
		score -= 5
	}
	if row.TurnoverRate > 10 {
		score -= 6
		risks = append(risks, "换手偏高，低吸承接不够安静")
	}
	if !m.StandingMA5 {
		risks = append(risks, "未站稳5日线，买点需等待修复")
	}
	if m.PullbackRangePct > 18 {
		score -= 6
		risks = append(risks, "涨停后回撤偏深，强势结构降级")
	}
	if m.DaysSinceLimit <= 1 {
		score -= 4
		risks = append(risks, "涨停后洗盘不足，容易变成追高")
	}

	score = math.Round(clamp(score, 0, 100)*10) / 10

	reasons = append(reasons, fmt.Sprintf("强启动：%d日前涨幅%.2f%%，成交额为前5日均值%.2f倍", m.DaysSinceLimit, m.LimitPct, m.LimitVolumeRatio))
	reasons = append(reasons, fmt.Sprintf("趋势闸门：%s，近20日涨跌%.2f%%，MA20斜率%.2f%%", m.TrendGateReason, m.Trend20Pct, m.MA20SlopePct))
	reasons = append(reasons, fmt.Sprintf("缩量回调：涨停后均额/启动日=%.0f%%，回撤幅度%.2f%%", m.AmountShrinkRatio*100, m.PullbackRangePct))
	reasons = append(reasons, fmt.Sprintf("均线承接：收盘%.2f，MA5 %.2f，MA10 %.2f，MA20 %.2f，MA30 %.2f，MA60 %.2f", m.Close, m.MA5, m.MA10, m.MA20, m.MA30, m.MA60))
	reasons = append(reasons, fmt.Sprintf("低吸区域：距MA5 %.2f%%，距MA10 %.2f%%，涨停半分位%.2f", m.NearMA5Pct, m.NearMA10Pct, m.HalfPosition))
	if m.BreakoutHigh {
		reasons = append(reasons, "确认信号：今日突破涨停后回调区间高点，可视为二次启动确认")
	} else {
		reasons = append(reasons, "确认信号：当前偏低吸观察，优先等尾盘站稳均线或放量突破回调高点")
	}

	return models.LowBuyScannerItem{
		Symbol:             row.Symbol,
		Name:               row.Name,
		Price:              row.Price,
		ChangePercent:      row.ChangePercent,
		Amount:             row.Amount,
		TurnoverRate:       row.TurnoverRate,
		MainNetInflow:      row.MainNetInflow,
		MainNetInflowRatio: row.MainNetInflowRatio,
		MainFlowSource:     chooseFirstNonEmpty(row.MainFlowSource, "not-required"),
		TotalMarketCap:     row.TotalMarketCap,
		FloatMarketCap:     row.FloatMarketCap,
		CapBucket:          classifyCapBucket(row.TotalMarketCap),
		Industry:           industry,
		Score:              score,
		TriggerCount:       len(triggers),
		Triggers:           triggers,
		Reasons:            reasons,
		RiskFlags:          risks,
		BuyPointHint:       "尾盘14:30后低吸：回踩5/10日线或涨停半分位不破；若放量突破回调高点，可等回踩确认",
		SellPointHint:      "冲高放量滞涨、长上影或高位跌破5日线先减；连续加速用5日线跟踪",
		StopLossHint:       fmt.Sprintf("跌破低吸位或5日线先走；放量跌破涨停阳线低点 %.2f，模型失效", m.StopLoss),
		MA10:               m.MA10,
		MA10Status:         map[bool]string{true: "hold", false: "broke"}[m.StandingMA10],
		UpdatedAt:          chooseFirstNonEmpty(row.UpdateTime, asOf),
	}, true
}

type tripleVolumeMetrics struct {
	Close          float64
	Open           float64
	High           float64
	Low            float64
	PrevClose      float64
	PctChange      float64
	VolumeRatio    float64
	AmountRatio    float64
	BodyPct        float64
	UpperShadowPct float64
	DistMA5Pct     float64
	DistMA10Pct    float64
	DistMA20Pct    float64
	DistMA30Pct    float64
	AvgMADistPct   float64
	MA5            float64
	MA10           float64
	MA20           float64
	MA30           float64
	CostLine       float64
	StopLoss       float64
}

func evaluateTripleVolumeRow(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	m, ok := buildTripleVolumeMetrics(daily)
	if !ok {
		return models.LowBuyScannerItem{}, false
	}

	triggers := []string{"阳线未涨停", "成交量>=前一日3倍", "一阳穿MA5/10/20/30"}
	reasons := []string{
		fmt.Sprintf("三倍量：今日成交量/昨日=%.2f倍，成交额/昨日=%.2f倍", m.VolumeRatio, m.AmountRatio),
		fmt.Sprintf("一阳穿线：收盘%.2f 同时上穿 MA5 %.2f / MA10 %.2f / MA20 %.2f / MA30 %.2f", m.Close, m.MA5, m.MA10, m.MA20, m.MA30),
		fmt.Sprintf("K线结构：开%.2f 收%.2f，涨跌%.2f%%，实体%.2f%%，上影%.2f%%", m.Open, m.Close, m.PctChange, m.BodyPct, m.UpperShadowPct),
		fmt.Sprintf("次日观察：突破日成本线约%.2f，防守低点%.2f", m.CostLine, m.StopLoss),
	}
	risks := make([]string, 0, 4)

	score := 52.0
	score += 14 // 完整穿越四条均线是该策略的核心结构分。
	score += clamp(10+(m.VolumeRatio-3)*5, 10, 22)
	score += clamp(m.BodyPct*2.2, 0, 9)
	switch {
	case m.AvgMADistPct <= 3:
		score += 8
	case m.AvgMADistPct <= 6:
		score += 4
	default:
		score -= 5
		risks = append(risks, "突破后偏离均线较远，次日不适合追高")
	}
	switch {
	case row.Amount >= 2e8:
		score += 5
	case row.Amount >= 8e7:
		score += 3
	case row.Amount > 0 && row.Amount < 3e7:
		score -= 4
		risks = append(risks, "成交额偏小，隔日承接需确认")
	}
	switch {
	case row.TotalMarketCap > 0 && row.TotalMarketCap <= 100e8:
		score += 5
	case row.TotalMarketCap > 100e8 && row.TotalMarketCap <= 300e8:
		score += 2
	case row.TotalMarketCap >= 800e8:
		score -= 3
		risks = append(risks, "市值偏大，三倍量弹性可能被摊薄")
	}
	if m.UpperShadowPct >= 4 {
		score -= 6
		risks = append(risks, "上影线偏长，存在冲高回落")
	}
	if m.PctChange >= 8.5 {
		score -= 5
		risks = append(risks, "接近涨停，次日回撤波动会更大")
	}
	if m.VolumeRatio >= 8 {
		score -= 4
		risks = append(risks, "量能过度放大，需防次日放量滞涨")
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10

	displayPrice := row.Price
	if displayPrice <= 0 {
		displayPrice = m.Close
	}
	displayChange := row.ChangePercent
	if math.Abs(displayChange) < 0.0001 {
		displayChange = m.PctChange
	}
	mainSource := chooseFirstNonEmpty(row.MainFlowSource, "not-required")
	if row.MainNetInflow != 0 || row.MainNetInflowRatio != 0 {
		reasons = append(reasons, fmt.Sprintf("资金快照：主力净流入%s，占比%.2f%%（%s）", formatAmountCN(row.MainNetInflow), row.MainNetInflowRatio, mainSource))
	}

	return models.LowBuyScannerItem{
		Symbol:             row.Symbol,
		Name:               row.Name,
		Price:              displayPrice,
		ChangePercent:      displayChange,
		Amount:             row.Amount,
		TurnoverRate:       row.TurnoverRate,
		MainNetInflow:      row.MainNetInflow,
		MainNetInflowRatio: row.MainNetInflowRatio,
		MainFlowSource:     mainSource,
		TotalMarketCap:     row.TotalMarketCap,
		FloatMarketCap:     row.FloatMarketCap,
		CapBucket:          classifyCapBucket(row.TotalMarketCap),
		Industry:           industry,
		Score:              score,
		TriggerCount:       len(triggers),
		Triggers:           triggers,
		Reasons:            reasons,
		RiskFlags:          risks,
		BuyPointHint:       fmt.Sprintf("不追当天长阳；次日缩量回调且不破突破日成本线 %.2f，再看分时承接低吸", m.CostLine),
		SellPointHint:      "次日放量滞涨、冲高长上影或跌回均线下方先减；快速拉升3%-8%分批兑现",
		StopLossHint:       fmt.Sprintf("跌破突破阳线低点 %.2f 或买入价-5%%，策略失效", m.StopLoss),
		MA10:               m.MA10,
		MA10Status:         map[bool]string{true: "hold", false: "broke"}[m.Close >= m.MA10],
		UpdatedAt:          chooseFirstNonEmpty(row.UpdateTime, asOf),
	}, true
}

func buildTripleVolumeMetrics(daily []models.KLineData) (tripleVolumeMetrics, bool) {
	if len(daily) < 31 {
		return tripleVolumeMetrics{}, false
	}
	bars := append([]models.KLineData(nil), daily...)
	sort.SliceStable(bars, func(i, j int) bool {
		return strings.TrimSpace(bars[i].Time) < strings.TrimSpace(bars[j].Time)
	})
	lastIdx := len(bars) - 1
	prevIdx := lastIdx - 1
	cur := bars[lastIdx]
	prev := bars[prevIdx]
	if cur.Open <= 0 || cur.Close <= 0 || cur.High <= 0 || cur.Low <= 0 || prev.Close <= 0 || prev.Volume <= 0 || cur.Volume <= 0 {
		return tripleVolumeMetrics{}, false
	}

	ma5, ok5 := klineMAAt(bars, lastIdx, 5)
	ma10, ok10 := klineMAAt(bars, lastIdx, 10)
	ma20, ok20 := klineMAAt(bars, lastIdx, 20)
	ma30, ok30 := klineMAAt(bars, lastIdx, 30)
	prevMA5, prevOK5 := klineMAAt(bars, prevIdx, 5)
	prevMA10, prevOK10 := klineMAAt(bars, prevIdx, 10)
	prevMA20, prevOK20 := klineMAAt(bars, prevIdx, 20)
	prevMA30, prevOK30 := klineMAAt(bars, prevIdx, 30)
	if !ok5 || !ok10 || !ok20 || !ok30 || !prevOK5 || !prevOK10 || !prevOK20 || !prevOK30 {
		return tripleVolumeMetrics{}, false
	}

	positiveCandle := cur.Close > cur.Open && cur.Close < prev.Close*1.095
	volumeRatio := float64(cur.Volume) / float64(prev.Volume)
	tripleVolume := volumeRatio >= 3
	crossAll := cur.Close > ma5 && prev.Close <= prevMA5 &&
		cur.Close > ma10 && prev.Close <= prevMA10 &&
		cur.Close > ma20 && prev.Close <= prevMA20 &&
		cur.Close > ma30 && prev.Close <= prevMA30
	if !positiveCandle || !tripleVolume || !crossAll {
		return tripleVolumeMetrics{}, false
	}

	pctChange := (cur.Close/prev.Close - 1) * 100
	amountRatio := 0.0
	if prev.Amount > 0 && cur.Amount > 0 {
		amountRatio = cur.Amount / prev.Amount
	}
	bodyPct := (cur.Close - cur.Open) / prev.Close * 100
	upperShadowPct := 0.0
	if cur.High > cur.Close {
		upperShadowPct = (cur.High - cur.Close) / prev.Close * 100
	}
	distMA5 := (cur.Close/ma5 - 1) * 100
	distMA10 := (cur.Close/ma10 - 1) * 100
	distMA20 := (cur.Close/ma20 - 1) * 100
	distMA30 := (cur.Close/ma30 - 1) * 100
	avgMADist := (math.Abs(distMA5) + math.Abs(distMA10) + math.Abs(distMA20) + math.Abs(distMA30)) / 4
	costLine := (cur.Open + cur.Close) / 2
	if cur.Amount > 0 && cur.Volume > 0 {
		avgPrice := cur.Amount / float64(cur.Volume)
		if avgPrice > cur.Low*0.8 && avgPrice < cur.High*1.2 {
			costLine = avgPrice
		}
	}

	return tripleVolumeMetrics{
		Close:          cur.Close,
		Open:           cur.Open,
		High:           cur.High,
		Low:            cur.Low,
		PrevClose:      prev.Close,
		PctChange:      pctChange,
		VolumeRatio:    volumeRatio,
		AmountRatio:    amountRatio,
		BodyPct:        bodyPct,
		UpperShadowPct: upperShadowPct,
		DistMA5Pct:     distMA5,
		DistMA10Pct:    distMA10,
		DistMA20Pct:    distMA20,
		DistMA30Pct:    distMA30,
		AvgMADistPct:   avgMADist,
		MA5:            ma5,
		MA10:           ma10,
		MA20:           ma20,
		MA30:           ma30,
		CostLine:       costLine,
		StopLoss:       cur.Low,
	}, true
}

func klineMAAt(klines []models.KLineData, endIdx int, period int) (float64, bool) {
	if period <= 0 || endIdx < 0 || endIdx >= len(klines) || endIdx-period+1 < 0 {
		return 0, false
	}
	sum := 0.0
	for i := endIdx - period + 1; i <= endIdx; i++ {
		if klines[i].Close <= 0 {
			return 0, false
		}
		sum += klines[i].Close
	}
	return sum / float64(period), true
}

func evaluateTailBuyV6Row(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	bars := formulaSortedBars(daily)
	if len(bars) < 245 {
		return models.LowBuyScannerItem{}, false
	}
	last := len(bars) - 1
	prev := last - 1
	cur := bars[last]
	if cur.Close <= 0 || cur.Open <= 0 || cur.Close >= cur.Open {
		return models.LowBuyScannerItem{}, false
	}
	m, ok := buildTailBuyV6Metrics(row, bars, prev)
	if !ok || !m.Signal {
		return models.LowBuyScannerItem{}, false
	}

	pullbackPct := (cur.Close/m.TriggerClose - 1) * 100
	score := 54.0
	score += 16
	score += clamp((m.TriggerTurnover-5)*1.2, 0, 10)
	score += clamp((m.TriggerChangePct-5)*2.0, 0, 10)
	if pullbackPct >= -4 && pullbackPct <= -0.5 {
		score += 9
	} else if pullbackPct < -6 {
		score -= 8
	}
	if cur.Close >= m.TriggerCostLine*0.98 {
		score += 7
	}
	if row.Amount >= 8e7 {
		score += 4
	}
	risks := make([]string, 0, 3)
	if pullbackPct < -6 {
		risks = append(risks, "今日回踩过深，尾盘承接不稳不进")
	}
	if row.TurnoverRate > 12 {
		score -= 4
		risks = append(risks, "今日换手偏高，可能不是安静回踩")
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10

	triggers := []string{"昨日资金强势触发", "今日阴线回踩", "昨日换手>5%"}
	if cur.Close >= m.TriggerCostLine*0.98 {
		triggers = append(triggers, "不破昨日成本线")
	}
	reasons := []string{
		fmt.Sprintf("昨日触发：涨幅%.2f%%，换手估算%.2f%%，WPZY强势值 %.2f / 回撤压力 %.2f", m.TriggerChangePct, m.TriggerTurnover, m.WPZYII, m.WPZYP1),
		fmt.Sprintf("今日回踩：开%.2f 收%.2f，相对昨日收盘 %.2f%%，昨日成本线约%.2f", cur.Open, cur.Close, pullbackPct, m.TriggerCostLine),
		"公式节奏：REF(WPZY_NP,1) 后今日 C<O，定位尾盘观察承接，不追昨日强阳",
	}
	return formulaScannerItem(row, industry, asOf, cur, score, triggers, reasons, risks,
		fmt.Sprintf("尾盘14:30后观察：缩量回踩且不破昨日成本线 %.2f，再分批试错", m.TriggerCostLine),
		"次日冲高3%-6%或放量滞涨先兑现；不能快速修复昨日强势位则降级",
		fmt.Sprintf("跌破昨日强势阳线低点 %.2f 或买入价-5%%止损", m.TriggerLow),
	), true
}

type tailBuyV6Metrics struct {
	Signal           bool
	WPZYII           float64
	WPZYP1           float64
	TriggerClose     float64
	TriggerLow       float64
	TriggerCostLine  float64
	TriggerTurnover  float64
	TriggerChangePct float64
}

func buildTailBuyV6Metrics(row services.ScanSnapshotRow, bars []models.KLineData, idx int) (tailBuyV6Metrics, bool) {
	if idx < 240 || idx >= len(bars) {
		return tailBuyV6Metrics{}, false
	}
	n := len(bars)
	highs, lows, closes, opens, _, _ := formulaSeries(bars)
	pw := make([]float64, n)
	yq := make([]float64, n)
	ii := make([]float64, n)
	p1 := make([]float64, n)
	i7 := make([]float64, n)
	for i := 0; i < n; i++ {
		pw[i] = (closes[i] + highs[i] + lows[i]) / 3
		yq[i], _ = formulaHHVAt(closes, i, 20)
		llv240, okL := formulaLLVAt(lows, i, 240)
		hhv240, okH := formulaHHVAt(highs, i, 240)
		if okL && okH && llv240 > 0 && closes[i] > 0 {
			ii[i] = (closes[i] - llv240) / llv240 * 100
			p1[i] = (hhv240 - closes[i]) / closes[i] * 100
		}
		if i >= 20 {
			nt := 0.0
			downDM := 0.0
			for j := i - 19; j <= i; j++ {
				if j <= 0 {
					continue
				}
				tr := math.Max(math.Max(highs[j]-lows[j], math.Abs(highs[j]-closes[j-1])), math.Abs(closes[j-1]-lows[j]))
				nt += tr
				upMove := highs[j] - highs[j-1]
				downMove := lows[j-1] - lows[j]
				if downMove > 0 && downMove > upMove {
					downDM += downMove
				}
			}
			if nt > 0 {
				i7[i] = downDM / nt / 2 * 100
			}
		}
	}
	to := formulaEMA(yq, 30)
	bo := formulaEMA(yq, 120)
	n0 := formulaEMA(pw, 10)
	raw := make([]bool, n)
	for i := 240; i < n; i++ {
		if i == 0 || n0[i] <= 0 || to[i] <= 0 || bo[i] <= 0 || closes[i-1] <= 0 {
			continue
		}
		js := ii[i] > 15 && ii[i] > i7[i]
		qi := ii[i] > 30 && ii[i] > p1[i]
		hhvII10, _ := formulaHHVAt(ii, i, 10)
		turnover := formulaTurnoverPct(row, bars[i])
		// 实战收紧：把 JS/QI 作为强势阶段门，后面的量价/位置闸门统一生效。
		raw[i] = (js || qi) &&
			ii[i] >= hhvII10 &&
			turnover > 5 &&
			opens[i]/n0[i] < 1.08 &&
			opens[i]/to[i] < 1.10 &&
			opens[i]/bo[i] < 1.20 &&
			closes[i] > to[i] &&
			closes[i] >= closes[i-1]*1.05 &&
			p1[i] < 60 &&
			ii[i] < 120
	}
	if !formulaFilterAt(raw, idx, 30) {
		return tailBuyV6Metrics{}, false
	}
	trigger := bars[idx]
	costLine := (trigger.Open + trigger.Close) / 2
	if trigger.Amount > 0 && trigger.Volume > 0 {
		avg := trigger.Amount / float64(trigger.Volume)
		if avg > trigger.Low*0.8 && avg < trigger.High*1.2 {
			costLine = avg
		}
	}
	changePct := 0.0
	if idx > 0 && bars[idx-1].Close > 0 {
		changePct = (trigger.Close/bars[idx-1].Close - 1) * 100
	}
	return tailBuyV6Metrics{
		Signal:           true,
		WPZYII:           ii[idx],
		WPZYP1:           p1[idx],
		TriggerClose:     trigger.Close,
		TriggerLow:       trigger.Low,
		TriggerCostLine:  costLine,
		TriggerTurnover:  formulaTurnoverPct(row, trigger),
		TriggerChangePct: changePct,
	}, true
}

func evaluateHotMoneyV7Row(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	bars := formulaSortedBars(daily)
	if len(bars) < 8 {
		return models.LowBuyScannerItem{}, false
	}
	last := len(bars) - 1
	cur := bars[last]
	if cur.Close <= 0 || cur.Open <= 0 {
		return models.LowBuyScannerItem{}, false
	}
	_, _, closes, _, volumes, _ := formulaSeries(bars)
	closeHigh := formulaCloseNearHigh(cur)
	pct1 := formulaPctChange(bars, last)
	pct2 := formulaPctChange(bars, last-1)
	pct3 := formulaPctChange(bars, last-2)
	pct4 := formulaPctChange(bars, last-3)

	lzy11 := pct1 > 6.5 && pct1 < 11.8 && closeHigh && pct2 < 6.5
	prevMA5Vol, okMA := formulaMAAtFloat(volumes, last-1, 5)
	lzyUY := lzy11 && volumes[last] < volumes[last-1]
	lzyL8 := pct1 > 7.5 && pct2 < 9.5 && pct3 > 7.5 && pct4 < 7.5 && closeHigh
	lzy1W := pct1 > 7.5 && pct2 < 7.5 && pct3 < 7.5 && pct4 > 7.5 && formulaPctChange(bars, last-4) < 7.5 && closeHigh
	q3 := (lzy11 && cur.Close > cur.Open && okMA && volumes[last] > prevMA5Vol*2) ||
		(lzyUY && cur.Close > cur.Open) ||
		lzyL8 ||
		lzy1W
	sumPrev5 := 0.0
	for i := last - 5; i < last; i++ {
		if i >= 0 {
			sumPrev5 += volumes[i]
		}
	}
	aw := 0.0
	if sumPrev5 > 0 {
		aw = volumes[last] * 5 / sumPrev5
	}
	floatSharesWan := formulaFloatSharesWan(row)
	shareGate := (floatSharesWan > 10000 && cur.Close > 20) ||
		(floatSharesWan > 30000 && cur.Close > 10 && cur.Close < 20) ||
		(floatSharesWan > 80000 && cur.Close < 10)
	if !q3 || aw < 1 || aw > 5 || !shareGate || last >= len(closes) {
		return models.LowBuyScannerItem{}, false
	}

	triggers := make([]string, 0, 5)
	if lzy11 {
		triggers = append(triggers, "涨停/准涨停启动")
	}
	if lzyUY {
		triggers = append(triggers, "缩量封高")
	}
	if lzyL8 || lzy1W {
		triggers = append(triggers, "游资连阳结构")
	}
	triggers = append(triggers, "量能倍率1-5", "流通股本达标")
	score := 52.0 + float64(len(triggers))*7
	score += clamp((pct1-6.5)*2, 0, 10)
	score += clamp((3.0-math.Abs(aw-2.2))*3, 0, 8)
	if cur.Close == cur.High || closeHigh {
		score += 6
	}
	if row.Amount >= 2e8 {
		score += 5
	}
	risks := []string{}
	if pct1 > 10.5 {
		score -= 4
		risks = append(risks, "涨幅接近涨停，隔日分歧会更大")
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10
	reasons := []string{
		fmt.Sprintf("游资结构：今日涨幅%.2f%%，收盘贴近最高价，前序涨停组合命中", pct1),
		fmt.Sprintf("量能：LZY_AW≈%.2f（要求1-5），今日量/5日均量≈%.2f", aw, volumes[last]*5/math.Max(sumPrev5, 1)),
		fmt.Sprintf("流通股本代理：%.0f万股，价格%.2f，满足公式分档", floatSharesWan, cur.Close),
	}
	return formulaScannerItem(row, industry, asOf, cur, score, triggers, reasons, risks,
		"尾盘或次日只做回踩承接，不追一字/秒板；分时回踩均价线不破再考虑",
		"次日高开冲板失败、放量开板或长上影先走",
		"跌破启动阳线半分位或买入价-5%止损",
	), true
}

func evaluateDipEntryV8Row(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	bars := formulaSortedBars(daily)
	if len(bars) < 60 {
		return models.LowBuyScannerItem{}, false
	}
	_, _, closes, _, _, _ := formulaSeries(bars)
	n := len(closes)
	last := n - 1
	diff := make([]float64, n)
	gain := make([]float64, n)
	absDiff := make([]float64, n)
	for i := 1; i < n; i++ {
		diff[i] = closes[i] - closes[i-1]
		if diff[i] > 0 {
			gain[i] = diff[i]
		}
		absDiff[i] = math.Abs(diff[i])
	}
	vs := formulaRatioSeries(formulaSMA(gain, 5, 1), formulaSMA(absDiff, 5, 1), 100)
	twoT := formulaRatioSeries(formulaSMA(gain, 8, 1), formulaSMA(absDiff, 8, 1), 100)
	z4 := formulaRatioSeries(formulaSMA(gain, 4.1, 1), formulaSMA(absDiff, 4.1, 1), 100)
	emaDiff := formulaEMA(formulaEMA(diff, 6), 6)
	emaAbs := formulaEMA(formulaEMA(absDiff, 6), 6)
	ws := formulaRatioSeries(emaDiff, emaAbs, 100)
	maWS2 := formulaMASeries(ws, 2)

	cd := formulaCrossConstAt(vs, 20, last) && formulaCrossAt(vs, twoT, last) && vs[last] < 50
	kk := formulaCrossConstAt(z4, 11, last)
	wsLow2, _ := formulaLLVAt(ws, last, 2)
	wsLow7, _ := formulaLLVAt(ws, last, 7)
	wsNeg2 := formulaCountAt(ws, last, 2, func(v float64) bool { return v < 0 }) > 0
	tv := wsLow2 == wsLow7 && wsNeg2 && formulaCrossAt(ws, maWS2, last)

	tvRaw := make([]bool, n)
	for i := 7; i < n; i++ {
		l2, _ := formulaLLVAt(ws, i, 2)
		l7, _ := formulaLLVAt(ws, i, 7)
		tvRaw[i] = l2 == l7 &&
			formulaCountAt(ws, i, 2, func(v float64) bool { return v < 0 }) > 0 &&
			formulaCrossAt(ws, maWS2, i)
	}
	ej := tv && formulaFilterAt(tvRaw, last, 5)
	hits := 0
	triggers := make([]string, 0, 3)
	if cd {
		hits++
		triggers = append(triggers, "RSI5上穿20且强于RSI8")
	}
	if kk {
		hits++
		triggers = append(triggers, "快速RSI上穿11")
	}
	if ej {
		hits++
		triggers = append(triggers, "动能底背离反转")
	}
	if hits < 2 {
		return models.LowBuyScannerItem{}, false
	}

	cur := bars[last]
	score := 50.0 + float64(hits)*14
	score += clamp((50-vs[last])*0.25, 0, 8)
	if row.ChangePercent >= -3 && row.ChangePercent <= 3 {
		score += 6
	}
	if row.TurnoverRate > 0 && row.TurnoverRate <= 8 {
		score += 4
	}
	if row.ChangePercent > 6 {
		score -= 8
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10
	risks := []string{}
	if row.ChangePercent > 4 {
		risks = append(risks, "反转信号当天涨幅偏大，低吸性价比下降")
	}
	reasons := []string{
		fmt.Sprintf("三类反转信号命中%d/3：RSI5 %.2f，RSI8 %.2f，快速RSI %.2f，WS %.2f", hits, vs[last], twoT[last], z4[last], ws[last]),
		"公式条件：GQZY_CD、GQZY_KK、GQZY_8J 至少两类共振",
	}
	return formulaScannerItem(row, industry, asOf, cur, score, triggers, reasons, risks,
		"低吸入场：尾盘确认不再破日内低点，或次日回踩不破信号日低点再进",
		"反弹触及MA10/MA20或冲高放量滞涨分批止盈",
		fmt.Sprintf("跌破信号日低点 %.2f 或买入价-5%%止损", cur.Low),
	), true
}

func evaluateMonsterV9Row(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	bars := formulaSortedBars(daily)
	if len(bars) < 80 {
		return models.LowBuyScannerItem{}, false
	}
	highs, lows, closes, _, volumes, amounts := formulaSeries(bars)
	n := len(closes)
	last := n - 1
	cur := bars[last]
	ma4 := formulaMASeries(closes, 4)
	ma8 := formulaMASeries(closes, 8)
	ma20 := formulaMASeries(closes, 20)
	ma60 := formulaMASeries(closes, 60)
	macdHist := formulaMACDHist(closes)
	bollUB := formulaBollUpper(closes, 20, 2)
	cci := formulaCCI(highs, lows, closes, 14)
	exp25 := formulaEMA(closes, 25)

	breakoutRaw := make([]bool, n)
	breakoutLevel := make([]float64, n)
	for i := 21; i < n; i++ {
		level, ok := formulaHHVAt(highs, i-1, 20)
		if !ok || level <= 0 || volumes[i-1] <= 0 {
			continue
		}
		breakoutLevel[i] = level
		breakoutRaw[i] = closes[i] > level && closes[i-1] <= level && volumes[i] >= volumes[i-1]*1.9
	}
	daysSinceBreakout := formulaBarsLastAt(breakoutRaw, last)
	breakout := breakoutRaw[last]
	day2Confirm := daysSinceBreakout == 2 && closes[last] > ma5Safe(closes, last)

	rare := monsterRareAwakeningAt(bars, ma4, ma8, ma20, ma60, macdHist, last)

	range20 := 0.0
	if hh, okH := formulaHHVAt(highs, last, 20); okH {
		if ll, okL := formulaLLVAt(lows, last, 20); okL && ll > 0 {
			range20 = (hh - ll) / ll * 100
		}
	}
	expSlope := 0.0
	if last >= 20 && exp25[last-20] > 0 {
		expSlope = (exp25[last]/exp25[last-20] - 1) * 100
	}
	macdHigh, _ := formulaHHVAt(macdHist, last, minInt(300, n))
	bollBreak := false
	if last > 0 && bollUB[last] > 0 && bollUB[last-1] > 0 {
		bollBreak = closes[last] > bollUB[last] && closes[last-1] <= bollUB[last-1] && range20 < 45 && macdHist[last] >= macdHigh*0.92
	}

	amount := row.Amount
	if amount <= 0 && last < len(amounts) {
		amount = amounts[last]
	}
	turnover := row.TurnoverRate
	if turnover <= 0 {
		turnover = formulaTurnoverPct(row, cur)
	}
	lowFloor := 0.0
	if floor, ok := formulaLLVAt(lows, last, 60); ok {
		lowFloor = floor
	}
	lowReversal := lowFloor > 0 &&
		lows[last] <= lowFloor*1.005 &&
		cci[last] <= -80 &&
		row.ChangePercent >= -4 &&
		row.ChangePercent <= 2 &&
		amount >= 5e9 &&
		(turnover <= 0 || turnover <= 3.2)

	triggers := make([]string, 0, 5)
	if rare {
		triggers = append(triggers, "长期沉寂后突然转强")
	}
	if breakout {
		triggers = append(triggers, "放量突破前高")
	}
	if day2Confirm {
		triggers = append(triggers, "突破后第2日确认")
	}
	if bollBreak {
		triggers = append(triggers, "布林收敛爆发")
	}
	if lowReversal {
		triggers = append(triggers, "60日低点反抽")
	}
	if len(triggers) == 0 {
		return models.LowBuyScannerItem{}, false
	}

	level := breakoutLevel[last]
	if daysSinceBreakout <= last && daysSinceBreakout >= 0 && level <= 0 {
		level = breakoutLevel[last-daysSinceBreakout]
	}
	if level <= 0 {
		level, _ = formulaHHVAt(highs, maxInt(0, last-1), 20)
	}

	score := 56.0
	if rare {
		score += 14
	}
	if breakout {
		score += 10
	}
	if day2Confirm {
		score += 5
	}
	if bollBreak {
		score += 10
	}
	if lowReversal {
		score += 5
	}
	if amount >= 1e9 && (breakout || rare || bollBreak) {
		score += 5
	}
	if row.ChangePercent >= 9.5 {
		score -= 5
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10

	risks := []string{}
	if lowReversal && len(triggers) == 1 {
		risks = append(risks, "仅低点反抽命中，妖股强度不足，需等放量确认")
	}
	if breakout && row.ChangePercent >= 9.5 {
		risks = append(risks, "突破日接近涨停，次日若不能继续放量容易分歧")
	}
	reasons := []string{
		fmt.Sprintf("捉妖代理：近20日振幅%.2f%%，EXPMA25斜率%.2f%%，MACD柱当前/高点 %.2f/%.2f", range20, expSlope, macdHist[last], macdHigh),
		fmt.Sprintf("突破观察：前高%.2f，距上次放量突破%d日，CCI %.2f", level, daysSinceBreakout, cci[last]),
		"说明：恢复此前选出样本的代理复刻逻辑；低点反抽已增加成交额/换手/跌幅阀门，避免弱市泛滥",
	}
	if lowReversal {
		reasons = append(reasons, fmt.Sprintf("低点反抽阀门：成交额%.2f亿，换手%.2f%%，60日地板%.2f", amount/1e8, turnover, lowFloor))
	}
	return formulaScannerItem(row, industry, asOf, cur, score, triggers, reasons, risks,
		"放量突破票只看尾盘能否站稳前高；低点反抽票等放量确认再试错",
		"突破次日冲高封不住或放量长上影先减；低点反抽反弹到MA10/MA20先看兑现",
		"跌破信号日低点或买入价-5%止损",
	), true
}

func evaluateMonsterV10Row(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	bars := formulaSortedBars(daily)
	if len(bars) < 220 {
		return models.LowBuyScannerItem{}, false
	}
	highs, lows, closes, opens, volumes, _ := formulaSeries(bars)
	n := len(closes)
	last := n - 1
	cur := bars[last]
	if last < 1 || cur.Close <= 0 || cur.Low <= 0 {
		return models.LowBuyScannerItem{}, false
	}

	ma4 := formulaMASeries(closes, 4)
	ma8 := formulaMASeries(closes, 8)
	ma20 := formulaMASeries(closes, 20)
	ma60 := formulaMASeries(closes, 60)
	macdHist := formulaMACDHist(closes)
	bollUB := formulaBollUpper(closes, 20, 2)
	expma25 := formulaEMA(closes, 25)
	cci := formulaCCI(highs, lows, closes, 14)

	ggzy8x := make([]bool, n)
	ggzy9k := make([]bool, n)
	limitUp95 := make([]bool, n)
	pct8to95 := make([]bool, n)
	pct7to8 := make([]bool, n)
	pct6to7 := make([]bool, n)
	pct5to6 := make([]bool, n)
	for i := 1; i < n; i++ {
		minShort := math.Min(math.Min(ma4[i], ma8[i]), ma20[i])
		ggzy8x[i] = ma60[i] > 0 && minShort > ma60[i]
		prevClose := closes[i-1]
		if prevClose <= 0 {
			continue
		}
		ggzy9k[i] = closes[i] > prevClose*1.05
		limitUp95[i] = closes[i] > prevClose*1.095 && formulaCloseEqualsHigh(bars[i])
		pct8to95[i] = closes[i] < prevClose*1.095 && closes[i] > prevClose*1.08
		pct7to8[i] = closes[i] < prevClose*1.08 && closes[i] > prevClose*1.07
		pct6to7[i] = closes[i] < prevClose*1.07 && closes[i] > prevClose*1.06
		pct5to6[i] = closes[i] < prevClose*1.06 && closes[i] > prevClose*1.05
	}

	ggzy5n := formulaBarsLastSeries(limitUp95)
	ggzy3e := formulaBarsLastSeries(pct8to95)
	ggzyt5 := formulaBarsLastSeries(pct7to8)
	ggzyte := formulaBarsLastSeries(pct6to7)
	ggzyyx := formulaBarsLastSeries(pct5to6)
	ggzyyi := make([]bool, n)
	for i := 1; i < n; i++ {
		ggzyyi[i] = ggzy5n[i-1] > 100 &&
			ggzy3e[i-1] > 100 &&
			ggzyt5[i-1] > 100 &&
			ggzyte[i-1] > 100 &&
			ggzyyx[i-1] > 80 &&
			ggzy9k[i]
	}

	ggzyF3 := formulaLLVSeries(macdHist, 200)
	ggzy00 := formulaHHVSeries(macdHist, 200)
	rareSeed := make([]bool, n)
	for i := range rareSeed {
		rareSeed[i] = ggzyyi[i] && ggzy00[i] < 60 && ggzyF3[i] > -55
	}
	ggzy0m := formulaBarsLastSeries(rareSeed)

	ggzy3v := make([]int, n)
	ggzyddBase := make([]bool, n)
	ggzydd := make([]bool, n)
	for i := range ggzy3v {
		period := ggzy0m[i] + 1
		if period <= 0 || period > i+1 {
			period = i + 1
		}
		ggzy3v[i] = formulaBarsSinceFirstInPeriodAt(ggzy8x, i, period)
		ggzyddBase[i] = (ggzy0m[i] == 0 && ggzy8x[i]) || ggzy3v[i] == 0
		ggzydd[i] = formulaCrossBoolConstAt(ggzyddBase, 0.5, i)
	}
	ggzyir := make([]bool, n)
	for i := range ggzyir {
		ggzyir[i] = formulaCountBoolAt(ggzydd, i, 30) == 2 && ggzydd[i]
	}

	ggzy0r := make([]bool, n)
	for i := 20; i < n; i++ {
		hhv21, ok := formulaHHVAt(highs, i, 21)
		ggzy0r[i] = ok && nearlyEqual(highs[i-10], hhv21, 1e-6)
	}
	ggzygk := make([]bool, n)
	for i := range ggzygk {
		ggzygk[i] = formulaFilterAt(ggzy0r, i, 10)
	}
	ggzy0a := make([]bool, n)
	for i := 21; i < n; i++ {
		ggzy0a[i] = ggzygk[i-21]
	}
	ggzyzr := make([]float64, n)
	for i := range ggzyzr {
		barsLast := formulaBarsLastAt(ggzy0a, i)
		refIdx := i - barsLast
		if refIdx >= 0 && refIdx < n {
			ggzyzr[i] = highs[refIdx]
		}
	}
	ggzyyq := make([]bool, n)
	ggzyy3 := make([]bool, n)
	ggzy0s := make([]bool, n)
	for i := 1; i < n; i++ {
		ggzyyq[i] = volumes[i-1] > 0 && volumes[i]/volumes[i-1] >= 1.9
		ggzyy3[i] = ggzyzr[i] > 0 && closes[i] > ggzyzr[i] && closes[i-1] <= ggzyzr[i-1]
		ggzy0s[i] = ggzyyq[i] && ggzyy3[i]
	}
	ggzyiv := formulaBarsLastSeries(ggzy0s)

	ggzy4b := make([]int, n)
	ggzy41 := make([]int, n)
	ggzywk := make([]bool, n)
	ubRising := make([]bool, n)
	ubRisingCross := make([]bool, n)
	for i := 1; i < n; i++ {
		ubRising[i] = bollUB[i] >= bollUB[i-1] && bollUB[i] > 0
		ubRisingCross[i] = formulaCrossBoolConstAt(ubRising, 0.5, i)
	}
	ggzy41 = formulaBarsLastSeries(ubRisingCross)
	ggzy4tWindow := maxInt(1, minInt(59, n))
	ggzy4t, _ := formulaLLVAt(lows, last, ggzy4tWindow)
	ggzy4z := make([]bool, n)
	for i := 1; i < n; i++ {
		hhvBars, ok := formulaHHVBarsAt(macdHist, i, minInt(300, i+1))
		if ok {
			ggzy4b[i] = hhvBars
		}
		refMacdIdx := i - ggzy4b[i]
		macdPeakRetest := refMacdIdx >= 0 && macdHist[i] >= macdHist[refMacdIdx]
		macdPeakRetestPrev := false
		if i > 0 {
			prevHHVBars, prevOK := formulaHHVBarsAt(macdHist, i-1, minInt(300, i))
			prevRefIdx := i - 1 - prevHHVBars
			macdPeakRetestPrev = prevOK && prevRefIdx >= 0 && macdHist[i-1] >= macdHist[prevRefIdx]
		}

		period := ggzy41[i] + 1
		if period <= 0 || period > i+1 {
			period = i + 1
		}
		ky := formulaEveryAt(i, period, func(j int) bool {
			if j <= 0 || closes[j-1] <= 0 {
				return false
			}
			ratio := closes[j] / closes[j-1]
			return ratio >= 0.97 && ratio <= 1.05
		})
		hhv, okH := formulaHHVAt(highs, i, period)
		llv, okL := formulaLLVAt(lows, i, period)
		rangePct := 999.0
		if okH && okL && llv > 0 {
			rangePct = (hhv - llv) / llv * 100
		}
		ho := formulaEveryAt(i, period, func(j int) bool {
			return bollUB[j] > 0 && math.Max(closes[j], opens[j])/bollUB[j] < 1.02
		})
		refExpIdx := i - ggzy41[i]
		ci := 0.0
		if refExpIdx >= 0 && expma25[refExpIdx] > 0 {
			ci = math.Atan((expma25[i]/expma25[refExpIdx]-1)*100) * 180 / math.Pi
		}
		ggzywk[i] = macdPeakRetest && !macdPeakRetestPrev && rangePct < 17 && ky && ho
		ggzy4z[i] = ggzywk[i] && ci > 75
	}

	ggzyzt := make([]bool, n)
	ggzycr := make([]bool, n)
	for i := range ggzyzt {
		// 原公式为 CROSS(GGZY_YI,9.8)，GGZY_YI 是布尔量，严格执行后不会触发。
		ggzyzt[i] = false
		ggzycr[i] = false
	}

	ggzyig := make([]bool, n)
	currBarsCountTarget := ggzyiv[last] + 2
	for i := range ggzyig {
		currBarsCount := n - i
		currBarsHit := currBarsCount == currBarsCountTarget
		lowBreakHit := cci[i] <= 100 && ggzy4t > 0 && lows[i] <= ggzy4t && currBarsCount <= 60
		ggzyig[i] = ggzycr[i] || ggzy4z[i] || currBarsHit || ggzyir[i] || lowBreakHit
	}
	if !formulaFilterAt(ggzyig, last, 3) {
		return models.LowBuyScannerItem{}, false
	}

	triggers := make([]string, 0, 5)
	if ggzy4z[last] {
		triggers = append(triggers, "GGZY_4Z：布林收敛+MACD新高+EXPMA强角度")
	}
	if ggzyir[last] {
		triggers = append(triggers, "GGZY_IR：DD信号30日内第2次")
	}
	if cci[last] <= 100 && ggzy4t > 0 && lows[last] <= ggzy4t {
		triggers = append(triggers, "CCI<=100且跌至60日低位线")
	}
	if currBarsCountTarget == 1 {
		triggers = append(triggers, "CURRBARSCOUNT确认")
	}
	if len(triggers) == 0 {
		triggers = append(triggers, "GGZY_ZS过滤信号")
	}

	hhv300, _ := formulaHHVAt(macdHist, last, minInt(300, n))
	hhvBarsLast, _ := formulaHHVBarsAt(macdHist, last, minInt(300, n))
	rangeSinceUB := 0.0
	if period := ggzy41[last] + 1; period > 0 {
		hhv, okH := formulaHHVAt(highs, last, minInt(period, n))
		llv, okL := formulaLLVAt(lows, last, minInt(period, n))
		if okH && okL && llv > 0 {
			rangeSinceUB = (hhv - llv) / llv * 100
		}
	}

	score := 55.0
	if ggzy4z[last] {
		score += 22
	}
	if ggzyir[last] {
		score += 14
	}
	if cci[last] <= 100 && ggzy4t > 0 && lows[last] <= ggzy4t {
		score += 8
	}
	if rareSeed[last] {
		score += 8
	}
	if row.Amount >= 1e9 {
		score += 4
	}
	if formulaPctChange(bars, last) > 9.5 {
		score -= 5
	}
	score = math.Round(clamp(score, 0, 100)*10) / 10

	reasons := []string{
		fmt.Sprintf("严格公式：GGZY_ZS=FILTER(GGZY_IG=1,3) 当前触发；GGZY_IG=%t，FILTER周期=3", ggzyig[last]),
		fmt.Sprintf("核心变量：YI=%t，0M=%d，3V=%d，DD=%t，IR=%t，4Z=%t", ggzyyi[last], ggzy0m[last], ggzy3v[last], ggzydd[last], ggzyir[last], ggzy4z[last]),
		fmt.Sprintf("MACD/BOLL：MACD柱%.2f，300日高点%.2f（距今%d日），BOLL上轨%.2f，41=%d，区间振幅%.2f%%", macdHist[last], hhv300, hhvBarsLast, bollUB[last], ggzy41[last], rangeSinceUB),
		fmt.Sprintf("低位项：CCI %.2f，低点%.2f，4T低位线%.2f", cci[last], lows[last], ggzy4t),
	}
	risks := make([]string, 0, 3)
	if len(triggers) == 1 && strings.Contains(triggers[0], "低位") {
		risks = append(risks, "仅低位项触发，需等待放量转强确认")
	}
	if row.ChangePercent >= 8 {
		risks = append(risks, "当日涨幅较高，次日分歧风险上升")
	}

	return formulaScannerItem(row, industry, asOf, cur, score, triggers, reasons, risks,
		"按策略10只看公式信号日后的承接：次日不追高，回踩不破信号日中位/低点再考虑",
		"放量冲高回落、跌回信号日实体内或MACD柱回落时先减",
		fmt.Sprintf("跌破信号日低点 %.2f 或买入价-5%%止损", cur.Low),
	), true
}

func ma5Safe(closes []float64, idx int) float64 {
	ma, ok := formulaMAAtFloat(closes, idx, 5)
	if ok {
		return ma
	}
	if idx >= 0 && idx < len(closes) {
		return closes[idx]
	}
	return 0
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func formulaScannerItem(row services.ScanSnapshotRow, industry string, asOf string, cur models.KLineData, score float64, triggers []string, reasons []string, risks []string, buy string, sell string, stop string) models.LowBuyScannerItem {
	price := row.Price
	if price <= 0 {
		price = cur.Close
	}
	change := row.ChangePercent
	if math.Abs(change) < 0.0001 && cur.Close > 0 {
		change = formulaKLineChangeFromBar(cur, cur.Close)
	}
	ma10, _ := klineMAAt([]models.KLineData{cur}, 0, 1)
	_ = ma10
	return models.LowBuyScannerItem{
		Symbol:             row.Symbol,
		Name:               row.Name,
		Price:              price,
		ChangePercent:      change,
		Amount:             row.Amount,
		TurnoverRate:       row.TurnoverRate,
		MainNetInflow:      row.MainNetInflow,
		MainNetInflowRatio: row.MainNetInflowRatio,
		MainFlowSource:     chooseFirstNonEmpty(row.MainFlowSource, "not-required"),
		TotalMarketCap:     row.TotalMarketCap,
		FloatMarketCap:     row.FloatMarketCap,
		CapBucket:          classifyCapBucket(row.TotalMarketCap),
		Industry:           industry,
		Score:              score,
		TriggerCount:       len(triggers),
		Triggers:           triggers,
		Reasons:            reasons,
		RiskFlags:          risks,
		BuyPointHint:       buy,
		SellPointHint:      sell,
		StopLossHint:       stop,
		UpdatedAt:          chooseFirstNonEmpty(row.UpdateTime, asOf),
	}
}

func formulaSortedBars(daily []models.KLineData) []models.KLineData {
	bars := append([]models.KLineData(nil), daily...)
	sort.SliceStable(bars, func(i, j int) bool {
		return strings.TrimSpace(bars[i].Time) < strings.TrimSpace(bars[j].Time)
	})
	return bars
}

func formulaSeries(bars []models.KLineData) ([]float64, []float64, []float64, []float64, []float64, []float64) {
	n := len(bars)
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	opens := make([]float64, n)
	volumes := make([]float64, n)
	amounts := make([]float64, n)
	for i, b := range bars {
		highs[i] = b.High
		lows[i] = b.Low
		closes[i] = b.Close
		opens[i] = b.Open
		volumes[i] = float64(b.Volume)
		amounts[i] = b.Amount
	}
	return highs, lows, closes, opens, volumes, amounts
}

func formulaEMA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 || period <= 0 {
		return out
	}
	alpha := 2.0 / float64(period+1)
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = alpha*values[i] + (1-alpha)*out[i-1]
	}
	return out
}

func formulaSMA(values []float64, period float64, weight float64) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 || period <= 0 {
		return out
	}
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = (weight*values[i] + (period-weight)*out[i-1]) / period
	}
	return out
}

func formulaMASeries(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	for i := range values {
		if ma, ok := formulaMAAtFloat(values, i, period); ok {
			out[i] = ma
		}
	}
	return out
}

func formulaMAAtFloat(values []float64, idx int, period int) (float64, bool) {
	if period <= 0 || idx < 0 || idx >= len(values) || idx-period+1 < 0 {
		return 0, false
	}
	sum := 0.0
	for i := idx - period + 1; i <= idx; i++ {
		sum += values[i]
	}
	return sum / float64(period), true
}

func formulaHHVAt(values []float64, idx int, period int) (float64, bool) {
	if period <= 0 || idx < 0 || idx >= len(values) {
		return 0, false
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	maxV := -math.MaxFloat64
	for i := start; i <= idx; i++ {
		if values[i] > maxV {
			maxV = values[i]
		}
	}
	return maxV, maxV > -math.MaxFloat64
}

func formulaLLVAt(values []float64, idx int, period int) (float64, bool) {
	if period <= 0 || idx < 0 || idx >= len(values) {
		return 0, false
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	minV := math.MaxFloat64
	for i := start; i <= idx; i++ {
		if values[i] < minV {
			minV = values[i]
		}
	}
	return minV, minV < math.MaxFloat64
}

func formulaHHVSeries(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	for i := range values {
		if v, ok := formulaHHVAt(values, i, period); ok {
			out[i] = v
		}
	}
	return out
}

func formulaLLVSeries(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	for i := range values {
		if v, ok := formulaLLVAt(values, i, period); ok {
			out[i] = v
		}
	}
	return out
}

func formulaCrossAt(a []float64, b []float64, idx int) bool {
	return idx > 0 && idx < len(a) && idx < len(b) && a[idx] > b[idx] && a[idx-1] <= b[idx-1]
}

func formulaCrossConstAt(a []float64, level float64, idx int) bool {
	return idx > 0 && idx < len(a) && a[idx] > level && a[idx-1] <= level
}

func formulaCrossBoolConstAt(raw []bool, level float64, idx int) bool {
	if idx <= 0 || idx >= len(raw) {
		return false
	}
	cur := 0.0
	if raw[idx] {
		cur = 1
	}
	prev := 0.0
	if raw[idx-1] {
		prev = 1
	}
	return cur > level && prev <= level
}

func formulaFilterAt(raw []bool, idx int, period int) bool {
	if idx < 0 || idx >= len(raw) || !raw[idx] {
		return false
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	for i := start; i < idx; i++ {
		if raw[i] {
			return false
		}
	}
	return true
}

func formulaBarsLastAt(raw []bool, idx int) int {
	if idx < 0 || idx >= len(raw) {
		return 999999
	}
	for i := idx; i >= 0; i-- {
		if raw[i] {
			return idx - i
		}
	}
	return 999999
}

func formulaBarsLastSeries(raw []bool) []int {
	out := make([]int, len(raw))
	lastTrue := -1
	for i, v := range raw {
		if v {
			lastTrue = i
			out[i] = 0
			continue
		}
		if lastTrue < 0 {
			out[i] = 999999
		} else {
			out[i] = i - lastTrue
		}
	}
	return out
}

func formulaBarsSinceFirstInPeriodAt(raw []bool, idx int, period int) int {
	if idx < 0 || idx >= len(raw) || period <= 0 {
		return 999999
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	for i := start; i <= idx; i++ {
		if raw[i] {
			return idx - i
		}
	}
	return 999999
}

func formulaEveryAt(idx int, period int, pred func(int) bool) bool {
	if idx < 0 || period <= 0 || pred == nil {
		return false
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	for i := start; i <= idx; i++ {
		if !pred(i) {
			return false
		}
	}
	return true
}

func formulaCountAt(values []float64, idx int, period int, pred func(float64) bool) int {
	if idx < 0 || idx >= len(values) || period <= 0 {
		return 0
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	count := 0
	for i := start; i <= idx; i++ {
		if pred(values[i]) {
			count++
		}
	}
	return count
}

func formulaCountBoolAt(raw []bool, idx int, period int) int {
	if idx < 0 || idx >= len(raw) || period <= 0 {
		return 0
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	count := 0
	for i := start; i <= idx; i++ {
		if raw[i] {
			count++
		}
	}
	return count
}

func formulaHHVBarsAt(values []float64, idx int, period int) (int, bool) {
	if idx < 0 || idx >= len(values) || period <= 0 {
		return 0, false
	}
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	maxV := -math.MaxFloat64
	maxIdx := -1
	for i := start; i <= idx; i++ {
		if values[i] >= maxV {
			maxV = values[i]
			maxIdx = i
		}
	}
	if maxIdx < 0 {
		return 0, false
	}
	return idx - maxIdx, true
}

func nearlyEqual(a float64, b float64, eps float64) bool {
	if eps <= 0 {
		eps = 1e-9
	}
	return math.Abs(a-b) <= eps
}

func formulaRatioSeries(num []float64, den []float64, scale float64) []float64 {
	n := minInt(len(num), len(den))
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if den[i] != 0 {
			out[i] = num[i] / den[i] * scale
		}
	}
	return out
}

func formulaTurnoverPct(row services.ScanSnapshotRow, bar models.KLineData) float64 {
	if row.FloatMarketCap > 0 && bar.Amount > 0 {
		return bar.Amount / row.FloatMarketCap * 100
	}
	if row.TurnoverRate > 0 {
		return row.TurnoverRate
	}
	return 0
}

func formulaFloatSharesWan(row services.ScanSnapshotRow) float64 {
	capValue := row.FloatMarketCap
	if capValue <= 0 {
		capValue = row.TotalMarketCap
	}
	if capValue <= 0 || row.Price <= 0 {
		return 0
	}
	return capValue / row.Price / 10000
}

func formulaPctChange(bars []models.KLineData, idx int) float64 {
	if idx <= 0 || idx >= len(bars) || bars[idx-1].Close <= 0 {
		return 0
	}
	return (bars[idx].Close/bars[idx-1].Close - 1) * 100
}

func formulaKLineChangeFromBar(bar models.KLineData, fallback float64) float64 {
	if bar.Open > 0 {
		return (fallback/bar.Open - 1) * 100
	}
	return 0
}

func formulaCloseNearHigh(bar models.KLineData) bool {
	if bar.High <= 0 || bar.Close <= 0 {
		return false
	}
	return math.Abs(bar.High-bar.Close)/bar.High <= 0.002
}

func formulaCloseEqualsHigh(bar models.KLineData) bool {
	if bar.High <= 0 || bar.Close <= 0 {
		return false
	}
	return nearlyEqual(bar.Close, bar.High, math.Max(0.0001, bar.High*0.00001))
}

func formulaMACDHist(closes []float64) []float64 {
	ema12 := formulaEMA(closes, 12)
	ema26 := formulaEMA(closes, 26)
	dif := make([]float64, len(closes))
	for i := range closes {
		dif[i] = ema12[i] - ema26[i]
	}
	dea := formulaEMA(dif, 9)
	hist := make([]float64, len(closes))
	for i := range closes {
		hist[i] = (dif[i] - dea[i]) * 2 * 100
	}
	return hist
}

func formulaBollUpper(closes []float64, period int, width float64) []float64 {
	out := make([]float64, len(closes))
	for i := range closes {
		ma, ok := formulaMAAtFloat(closes, i, period)
		if !ok {
			continue
		}
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := closes[j] - ma
			sum += diff * diff
		}
		out[i] = ma + width*math.Sqrt(sum/float64(period))
	}
	return out
}

func formulaCCI(highs []float64, lows []float64, closes []float64, period int) []float64 {
	n := minInt(len(highs), minInt(len(lows), len(closes)))
	tp := make([]float64, n)
	for i := 0; i < n; i++ {
		tp[i] = (highs[i] + lows[i] + closes[i]) / 3
	}
	out := make([]float64, n)
	for i := period - 1; i < n; i++ {
		ma, ok := formulaMAAtFloat(tp, i, period)
		if !ok {
			continue
		}
		dev := 0.0
		for j := i - period + 1; j <= i; j++ {
			dev += math.Abs(tp[j] - ma)
		}
		dev /= float64(period)
		if dev > 0 {
			out[i] = (tp[i] - ma) / (0.015 * dev)
		}
	}
	return out
}

func monsterRareAwakeningAt(bars []models.KLineData, ma4 []float64, ma8 []float64, ma20 []float64, ma60 []float64, macdHist []float64, idx int) bool {
	if idx <= 100 || idx >= len(bars) || idx >= len(ma60) || bars[idx-1].Close <= 0 {
		return false
	}
	pct := formulaPctChange(bars, idx)
	if pct <= 5 {
		return false
	}
	quiet := true
	for i := idx - 100; i < idx; i++ {
		if formulaPctChange(bars, i) > 5 {
			quiet = false
			break
		}
	}
	if !quiet {
		return false
	}
	maTrend := math.Min(math.Min(ma4[idx], ma8[idx]), ma20[idx]) > ma60[idx] && ma60[idx] > 0
	histHigh, okH := formulaHHVAt(macdHist, idx, minInt(200, idx+1))
	histLow, okL := formulaLLVAt(macdHist, idx, minInt(200, idx+1))
	return maTrend && okH && okL && histHigh < 60 && histLow > -55
}

func isFormulaScannerBlockedBoard(symbol string) bool {
	return isTripleVolumeBlockedBoard(symbol)
}

func buildLimitPullbackMetrics(row services.ScanSnapshotRow, daily []models.KLineData) (limitPullbackMetrics, bool) {
	if len(daily) < 15 {
		return limitPullbackMetrics{}, false
	}
	lastIdx := len(daily) - 1
	limitIdx := -1
	limitPct := 0.0

	start := lastIdx - 8
	if start < 1 {
		start = 1
	}
	end := lastIdx - 1
	if end < start {
		return limitPullbackMetrics{}, false
	}
	for i := end; i >= start; i-- {
		pct := klineChangePct(daily, i)
		if pct >= 9.2 {
			limitIdx = i
			limitPct = pct
			break
		}
	}
	if limitIdx < 0 {
		return limitPullbackMetrics{}, false
	}

	daysSinceLimit := lastIdx - limitIdx
	if daysSinceLimit < 2 || daysSinceLimit > 8 {
		return limitPullbackMetrics{}, false
	}
	limitBar := daily[limitIdx]
	if limitBar.Close <= 0 || limitBar.Amount <= 0 {
		return limitPullbackMetrics{}, false
	}

	preAmountAvg := averageKLineAmount(daily, maxInt(0, limitIdx-5), limitIdx)
	if preAmountAvg <= 0 {
		return limitPullbackMetrics{}, false
	}
	limitVolumeRatio := limitBar.Amount / preAmountAvg
	if limitVolumeRatio < 1.25 {
		return limitPullbackMetrics{}, false
	}

	pullbackBars := daily[limitIdx+1:]
	if len(pullbackBars) < 2 {
		return limitPullbackMetrics{}, false
	}
	pullbackAvgAmount := 0.0
	pullbackMinLow := math.MaxFloat64
	pullbackMaxHigh := 0.0
	pullbackMaxHighBeforeToday := 0.0
	for _, k := range pullbackBars {
		if k.Close <= 0 || k.Low <= 0 || k.High <= 0 {
			return limitPullbackMetrics{}, false
		}
		pullbackAvgAmount += k.Amount
		if k.Low < pullbackMinLow {
			pullbackMinLow = k.Low
		}
		if k.High > pullbackMaxHigh {
			pullbackMaxHigh = k.High
		}
	}
	for _, k := range daily[limitIdx+1 : lastIdx] {
		if k.High > pullbackMaxHighBeforeToday {
			pullbackMaxHighBeforeToday = k.High
		}
	}
	pullbackAvgAmount /= float64(len(pullbackBars))
	if pullbackAvgAmount <= 0 {
		return limitPullbackMetrics{}, false
	}
	amountShrinkRatio := pullbackAvgAmount / limitBar.Amount
	if amountShrinkRatio > 0.92 {
		return limitPullbackMetrics{}, false
	}

	ma5, closePrice, ok5 := computeLowBuyMA(daily, 5)
	ma10, _, ok10 := computeLowBuyMA(daily, 10)
	ma20, _, ok20 := computeLowBuyMA(daily, 20)
	ma30, _, ok30 := computeLowBuyMA(daily, 30)
	ma60, _, ok60 := computeLowBuyMA(daily, 60)
	if !ok5 || !ok10 || !ok20 || !ok30 || !ok60 {
		return limitPullbackMetrics{}, false
	}
	if closePrice <= 0 || closePrice < ma10*0.985 {
		return limitPullbackMetrics{}, false
	}
	trend20Pct := 0.0
	if len(daily) > 20 && daily[lastIdx-20].Close > 0 {
		trend20Pct = (closePrice/daily[lastIdx-20].Close - 1) * 100
	}
	ma20SlopePct := 0.0
	if len(daily) >= 31 {
		prevMA20 := averageKLineClose(daily, lastIdx-29, lastIdx-9)
		if prevMA20 > 0 {
			ma20SlopePct = (ma20/prevMA20 - 1) * 100
		}
	}
	shortAlignment := ma5 < ma10*0.995 && ma10 < ma20*0.995
	belowMA20 := closePrice < ma20
	midLongPressure := ma20 < ma30*0.995 && closePrice < ma30
	longDownChannel := ma30 < ma60*0.985 && closePrice < ma60
	fallingMA20 := ma20SlopePct < -1.5 && closePrice < ma30
	deep20dDown := trend20Pct < -4.0 && closePrice < ma20*1.02
	if shortAlignment || belowMA20 || midLongPressure || longDownChannel || fallingMA20 || deep20dDown {
		return limitPullbackMetrics{}, false
	}
	trendGateReason := "收盘站上MA20/MA30，且中长期均线未形成下降通道"
	if ma5 >= ma10*0.995 && ma10 >= ma20*0.995 && ma20 >= ma30*0.995 {
		trendGateReason = "MA5/MA10/MA20/MA30多头或准多头排列"
	}

	halfPosition := (limitBar.High + limitBar.Low) / 2
	halfHeld := pullbackMinLow >= halfPosition*0.985
	stopLoss := limitBar.Low
	if stopLoss <= 0 {
		stopLoss = ma10
	}

	current := daily[lastIdx]
	breakoutHigh := pullbackMaxHighBeforeToday > 0 && current.Close > pullbackMaxHighBeforeToday*0.995 && current.Amount >= pullbackAvgAmount*1.15
	pullbackRangePct := 0.0
	if limitBar.High > 0 {
		pullbackRangePct = (limitBar.High - pullbackMinLow) / limitBar.High * 100
	}
	if pullbackRangePct > 24 {
		return limitPullbackMetrics{}, false
	}

	return limitPullbackMetrics{
		LimitIndex:        limitIdx,
		DaysSinceLimit:    daysSinceLimit,
		LimitPct:          limitPct,
		LimitVolumeRatio:  limitVolumeRatio,
		LimitHigh:         limitBar.High,
		LimitLow:          limitBar.Low,
		LimitClose:        limitBar.Close,
		LimitAmount:       limitBar.Amount,
		PullbackMinLow:    pullbackMinLow,
		PullbackMaxHigh:   pullbackMaxHigh,
		PullbackAvgAmount: pullbackAvgAmount,
		AmountShrinkRatio: amountShrinkRatio,
		Close:             closePrice,
		MA5:               ma5,
		MA10:              ma10,
		MA20:              ma20,
		MA30:              ma30,
		MA60:              ma60,
		NearMA5Pct:        math.Abs(closePrice-ma5) / ma5 * 100,
		NearMA10Pct:       math.Abs(closePrice-ma10) / ma10 * 100,
		BreakoutHigh:      breakoutHigh,
		StandingMA5:       closePrice >= ma5*0.995,
		StandingMA10:      closePrice >= ma10*0.985,
		HalfPosition:      halfPosition,
		HalfPositionHeld:  halfHeld,
		StopLoss:          stopLoss,
		PullbackRangePct:  pullbackRangePct,
		CurrentChangePct:  row.ChangePercent,
		Trend20Pct:        trend20Pct,
		MA20SlopePct:      ma20SlopePct,
		TrendGateReason:   trendGateReason,
	}, true
}

func klineChangePct(klines []models.KLineData, idx int) float64 {
	if idx <= 0 || idx >= len(klines) {
		return 0
	}
	preClose := klines[idx-1].Close
	if preClose <= 0 {
		return 0
	}
	return (klines[idx].Close/preClose - 1) * 100
}

func averageKLineAmount(klines []models.KLineData, start int, end int) float64 {
	if start < 0 {
		start = 0
	}
	if end > len(klines) {
		end = len(klines)
	}
	if start >= end {
		return 0
	}
	sum := 0.0
	count := 0
	for i := start; i < end; i++ {
		if klines[i].Amount <= 0 {
			continue
		}
		sum += klines[i].Amount
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func averageKLineClose(klines []models.KLineData, start int, end int) float64 {
	if start < 0 {
		start = 0
	}
	if end > len(klines) {
		end = len(klines)
	}
	if start >= end {
		return 0
	}
	sum := 0.0
	count := 0
	for i := start; i < end; i++ {
		if klines[i].Close <= 0 {
			continue
		}
		sum += klines[i].Close
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func isLimitPullbackBlockedBoard(symbol string) bool {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) >= 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		code = code[2:]
	}
	return strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689")
}

func isTripleVolumeBlockedBoard(symbol string) bool {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) >= 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		if strings.HasPrefix(code, "bj") {
			return true
		}
		code = code[2:]
	}
	return strings.HasPrefix(code, "300") ||
		strings.HasPrefix(code, "301") ||
		strings.HasPrefix(code, "688") ||
		strings.HasPrefix(code, "689") ||
		strings.HasPrefix(code, "8") ||
		strings.HasPrefix(code, "4")
}

func buildIndustryMapFromEmbedded() map[string]string {
	m := make(map[string]string, 8000)
	type stockBasicData struct {
		Data struct {
			Fields []string        `json:"fields"`
			Items  [][]interface{} `json:"items"`
		} `json:"data"`
	}
	var basic stockBasicData
	if err := json.Unmarshal(embed.StockBasicJSON, &basic); err != nil {
		return m
	}
	symbolIdx, tsCodeIdx, industryIdx := -1, -1, -1
	for i, field := range basic.Data.Fields {
		switch field {
		case "symbol":
			symbolIdx = i
		case "ts_code":
			tsCodeIdx = i
		case "industry":
			industryIdx = i
		}
	}
	if symbolIdx < 0 || industryIdx < 0 {
		return m
	}
	for _, item := range basic.Data.Items {
		if symbolIdx >= len(item) || industryIdx >= len(item) {
			continue
		}
		code, _ := item[symbolIdx].(string)
		industry, _ := item[industryIdx].(string)
		if code == "" || industry == "" {
			continue
		}
		prefix := "sz"
		if tsCodeIdx >= 0 && tsCodeIdx < len(item) {
			tsCode, _ := item[tsCodeIdx].(string)
			switch {
			case strings.HasSuffix(strings.ToUpper(tsCode), ".SH"):
				prefix = "sh"
			case strings.HasSuffix(strings.ToUpper(tsCode), ".BJ"):
				prefix = "bj"
			}
		} else {
			if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
				prefix = "sh"
			} else if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
				prefix = "bj"
			}
		}
		m[prefix+code] = industry
	}
	return m
}

func isPlausibleIndexPoint(v float64) bool {
	return v > 10 && v < 30000
}

// GetF10Valuation 获取估值快照
func (a *App) GetF10Valuation(code string) models.StockValuation {
	if a.f10Service == nil {
		return models.StockValuation{}
	}
	valuation, err := a.f10Service.GetValuationByCode(code)
	if err != nil {
		log.Error("GetF10Valuation error: %v", err)
	}
	return valuation
}

// getDefaultAIConfig 获取默认AI配置
func (a *App) getDefaultAIConfig(config *models.AppConfig) *models.AIConfig {
	for i := range config.AIConfigs {
		if config.AIConfigs[i].ID == config.DefaultAIID {
			return &config.AIConfigs[i]
		}
		if config.AIConfigs[i].IsDefault {
			return &config.AIConfigs[i]
		}
	}
	if len(config.AIConfigs) > 0 {
		return &config.AIConfigs[0]
	}
	return nil
}

// getAIConfigByID 根据ID获取AI配置，找不到则返回默认配置
func (a *App) getAIConfigByID(aiConfigID string) *models.AIConfig {
	config := a.configService.GetConfig()
	// 如果指定了ID，尝试查找
	if aiConfigID != "" {
		for i := range config.AIConfigs {
			if config.AIConfigs[i].ID == aiConfigID {
				return &config.AIConfigs[i]
			}
		}
	}
	// 找不到则返回默认配置
	return a.getDefaultAIConfig(config)
}

// ========== Session API ==========

// GetOrCreateSession 获取或创建Session
func (a *App) GetOrCreateSession(stockCode, stockName string) *models.StockSession {
	if a.sessionService == nil {
		return nil
	}
	session, _ := a.sessionService.GetOrCreateSession(stockCode, stockName)
	return session
}

// GetHeldPositions 只读列出所有持仓(Shares>0)的股票，供自选列表按盈亏排序，不创建新 session。
func (a *App) GetHeldPositions() []models.HeldPosition {
	if a.sessionService == nil {
		return nil
	}
	return a.sessionService.ListPositions()
}

// GetSessionMessages 获取Session消息
func (a *App) GetSessionMessages(stockCode string) []models.ChatMessage {
	if a.sessionService == nil {
		return nil
	}
	return a.sessionService.GetMessages(stockCode)
}

// ClearSessionMessages 清空Session消息
func (a *App) ClearSessionMessages(stockCode string) string {
	if a.sessionService == nil {
		return "service not ready"
	}
	if err := a.sessionService.ClearMessages(stockCode); err != nil {
		return err.Error()
	}
	// 同步清除该股票的记忆
	if a.memoryManager != nil {
		if err := a.memoryManager.DeleteMemory(stockCode); err != nil {
			log.Error("delete memory error: %v", err)
		}
	}
	return "success"
}

// UpdateStockPosition 更新股票持仓信息（建仓时同步记入交易台账）
func (a *App) UpdateStockPosition(stockCode string, shares int64, costPrice float64, buyDate string) string {
	if a.sessionService == nil {
		return "service not ready"
	}
	if err := a.sessionService.UpdatePosition(stockCode, shares, costPrice, buyDate); err != nil {
		return err.Error()
	}
	// 建仓/加仓 → 台账记一笔"持仓中"
	if shares > 0 && costPrice > 0 && a.journalService != nil {
		name := stockCode
		if sess := a.sessionService.GetSession(stockCode); sess != nil && sess.StockName != "" {
			name = sess.StockName
		}
		a.journalService.OnBuy(stockCode, name, buyDate, costPrice, shares, "manual")
		if err := a.syncTradeJournalWatchGroup(); err != nil {
			log.Warn("同步交易台账组失败: %v", err)
		}
	}
	return "success"
}

// SellStockPosition 卖出/清仓：清掉持仓并把台账那笔结算（sellPrice<=0 时用实时价）
func (a *App) SellStockPosition(stockCode string, sellPrice float64, sellDate string) string {
	if a.sessionService == nil {
		return "service not ready"
	}
	if sellPrice <= 0 && a.marketService != nil {
		if stocks, err := a.marketService.GetStockRealTimeData(stockCode); err == nil && len(stocks) > 0 && stocks[0].Price > 0 {
			sellPrice = stocks[0].Price
		}
	}
	if a.journalService != nil {
		a.journalService.OnSell(stockCode, sellDate, sellPrice)
		if err := a.syncTradeJournalWatchGroup(); err != nil {
			log.Warn("同步交易台账组失败: %v", err)
		}
	}
	if err := a.sessionService.UpdatePosition(stockCode, 0, 0, ""); err != nil {
		return err.Error()
	}
	return "success"
}

// ========== 交易台账 API ==========

// ========== 模拟持仓 ==========

// AddPaperPosition 新建一笔模拟持仓（默认成本=现价、1000股；source=来源筛选系统）
func (a *App) AddPaperPosition(symbol, name, source string, costPrice float64, shares int64) string {
	if a.paperService == nil {
		return "模拟持仓服务未初始化"
	}
	if _, err := a.paperService.Add(symbol, name, source, costPrice, shares); err != nil {
		return err.Error()
	}
	return "success"
}

// ListPaperPositions 返回所有模拟持仓，并按实时行情填充现价/盈亏
func (a *App) ListPaperPositions() []models.PaperPosition {
	if a.paperService == nil {
		return []models.PaperPosition{}
	}
	list := a.paperService.List()
	if len(list) == 0 {
		return list
	}
	// 拉未平仓标的的实时价
	codes := make([]string, 0, len(list))
	for _, p := range list {
		if p.Status == "open" {
			codes = append(codes, p.Symbol)
		}
	}
	priceMap := map[string]float64{}
	if len(codes) > 0 && a.marketService != nil {
		if rt, err := a.marketService.GetStockRealTimeData(codes...); err == nil {
			for _, st := range rt {
				priceMap[st.Symbol] = st.Price
			}
		}
	}
	for i := range list {
		p := &list[i]
		if p.Status == "closed" {
			p.CurrentPrice = p.ClosePrice
			// 扣双边真实成本的净收益（与回测/胜率同口径）
			p.ProfitPct = services.PaperNetReturnPct(p.CostPrice, p.ClosePrice)
			p.ProfitAmount = (p.ClosePrice - p.CostPrice) * float64(p.Shares)
			continue
		}
		cur := priceMap[p.Symbol]
		if cur <= 0 {
			cur = p.OpenPrice
		}
		p.CurrentPrice = cur
		// 盘中"若现在卖出"的净收益（已扣双边成本）
		p.ProfitPct = services.PaperNetReturnPct(p.CostPrice, cur)
		p.ProfitAmount = (cur - p.CostPrice) * float64(p.Shares)
		// 风控线（成本硬止损 / 止盈封顶；移动止损由引擎动态执行）
		if p.CostPrice > 0 {
			prof := services.RiskProfileForSource(p.Source)
			p.RiskKind = prof.Name
			p.StopPrice = round2(p.CostPrice * (1 + prof.HardStopPct/100))
			p.TpPrice = round2(p.CostPrice * (1 + prof.TPPct/100))
		}
	}
	return list
}

func shouldApplyLowBuyPaperExit(source string) bool {
	key := strings.ToLower(strings.TrimSpace(source))
	switch key {
	case "lowbuy-v1", "lowbuy", "limit-pullback-v1", "taillazy-v2", "taillazy", "latechase-v3", "latechase":
		return true
	default:
		return false
	}
}

// GetPaperRiskSummary 组合风控概览：单票/板块集中度 + 组合浮盈 + 从峰值回撤预警(稳健口径)。
func (a *App) GetPaperRiskSummary() models.PaperRiskSummary {
	const singleCap, sectorCap, ddAlert = 10.0, 40.0, 15.0
	res := models.PaperRiskSummary{SingleCap: singleCap, SectorCap: sectorCap, DrawdownAlertPct: ddAlert}
	if a == nil || a.paperService == nil {
		return res
	}
	list := a.ListPaperPositions()
	industryMap := buildIndustryMapFromEmbedded()
	totalVal, totalCost := 0.0, 0.0
	type pv struct {
		name string
		val  float64
		ind  string
	}
	var items []pv
	sectorVal := map[string]float64{}
	for _, p := range list {
		if p.Status != "open" || p.CurrentPrice <= 0 {
			continue
		}
		val := p.CurrentPrice * float64(p.Shares)
		totalVal += val
		totalCost += p.CostPrice * float64(p.Shares)
		ind := chooseFirstNonEmpty(industryMap[p.Symbol], "其他")
		sectorVal[ind] += val
		items = append(items, pv{p.Name, val, ind})
		res.PositionCount++
	}
	if totalVal <= 0 {
		return res
	}
	res.TotalValue = round2(totalVal)
	res.TotalCost = round2(totalCost)
	if totalCost > 0 {
		res.ProfitPct = round2((totalVal/totalCost - 1) * 100)
	}
	// 单票集中度
	for _, it := range items {
		pct := it.val / totalVal * 100
		if pct > res.MaxSinglePct {
			res.MaxSinglePct = round2(pct)
		}
		if pct > singleCap {
			res.SingleOver = append(res.SingleOver, models.RiskConcentration{Name: it.name, Pct: round2(pct)})
		}
	}
	// 板块集中度
	for ind, v := range sectorVal {
		pct := v / totalVal * 100
		res.SectorTop = append(res.SectorTop, models.RiskConcentration{Name: ind, Pct: round2(pct)})
		if pct > sectorCap {
			res.SectorOver = append(res.SectorOver, models.RiskConcentration{Name: ind, Pct: round2(pct)})
		}
	}
	sort.Slice(res.SectorTop, func(i, j int) bool { return res.SectorTop[i].Pct > res.SectorTop[j].Pct })
	if len(res.SectorTop) > 5 {
		res.SectorTop = res.SectorTop[:5]
	}
	sort.Slice(res.SingleOver, func(i, j int) bool { return res.SingleOver[i].Pct > res.SingleOver[j].Pct })
	// 回撤预警（净值快照峰值）
	today := time.Now().Format("2006-01-02")
	a.paperService.RecordEquity(today, totalVal)
	peak := a.paperService.EquityPeak()
	res.PeakValue = round2(peak)
	if peak > 0 {
		res.DrawdownFromPeak = round2((peak - totalVal) / peak * 100)
		res.DrawdownAlert = res.DrawdownFromPeak >= ddAlert
	}
	// 预警汇总
	for _, s := range res.SingleOver {
		res.Warnings = append(res.Warnings, fmt.Sprintf("单票超限：%s 占%.1f%%(>%.0f%%)", s.Name, s.Pct, singleCap))
	}
	for _, s := range res.SectorOver {
		res.Warnings = append(res.Warnings, fmt.Sprintf("板块超限：%s 占%.1f%%(>%.0f%%)", s.Name, s.Pct, sectorCap))
	}
	if res.DrawdownAlert {
		res.Warnings = append(res.Warnings, fmt.Sprintf("组合从峰值回撤%.1f%%(>%.0f%%) — 减仓避险", res.DrawdownFromPeak, ddAlert))
	}
	return res
}

// riskReasonCN 风控离场原因中文。
func riskReasonCN(r string) string {
	switch r {
	case "stop_loss":
		return "止损"
	case "breakeven":
		return "保本离场"
	case "trail":
		return "移动止损"
	case "take_profit":
		return "止盈封顶"
	case "time_stop":
		return "时间止损"
	}
	return r
}

// ApplyPaperExitRules 通用风控引擎：对所有未平仓模拟持仓套统一风控线(短线紧/价值宽，稳健参数)自动平仓+推送预警。
// 成本硬止损/保本/移动止损用实时价盘中触发；止盈/时间止损在已确认收盘K上触发。返回本次自动平仓笔数。
func (a *App) ApplyPaperExitRules() int {
	if a.paperService == nil || a.marketService == nil {
		return 0
	}
	opens := a.paperService.OpenPositions()
	if len(opens) == 0 {
		return 0
	}
	today := time.Now().Format("2006-01-02")
	closed := 0

	codes := make([]string, 0, len(opens))
	for _, p := range opens {
		if p.CostPrice > 0 {
			codes = append(codes, p.Symbol)
		}
	}
	priceMap := map[string]float64{}
	if stocks, err := a.marketService.GetStockRealTimeData(codes...); err == nil {
		for _, st := range stocks {
			priceMap[strings.ToLower(strings.TrimSpace(st.Symbol))] = st.Price
		}
	}

	doClose := func(p models.PaperPosition, price float64, date, reason string, hold int) {
		if a.paperService.CloseOn(p.ID, price, date, reason) != nil {
			return
		}
		closed++
		net := services.PaperNetReturnPct(p.CostPrice, price)
		rt.LogInfof("风控平仓: %s(%s) %s @%.2f 净%.2f%%", p.Name, p.Symbol, reason, price, net)
		if a.pushService != nil {
			a.pushService.Push(models.PushSignal{
				StockCode: p.Symbol, StockName: p.Name, Type: "risk-exit", Level: "timeSensitive",
				Message: fmt.Sprintf("⚠️风控平仓 %s %s @%.2f 净%+.2f%%", p.Name, riskReasonCN(reason), price, net),
			})
		}
	}

	for _, p := range opens {
		if p.CostPrice <= 0 || p.OpenDate == "" {
			continue
		}
		prof := services.RiskProfileForSource(p.Source)
		klines, err := a.marketService.GetKLineData(p.Symbol, "1d", 120)
		if err != nil || len(klines) == 0 {
			continue
		}
		confirmed := klines
		if last := klines[len(klines)-1]; last.Time >= today {
			confirmed = klines[:len(klines)-1]
		}
		// 入场以来最高价(已确认K)
		peak := p.CostPrice
		started := false
		for _, k := range confirmed {
			if k.Time == p.OpenDate {
				started = true
			}
			if started && k.High > peak {
				peak = k.High
			}
		}
		// 1) 实时价跌破当前止损线(硬止损/保本/移动止损)→盘中即走
		if rt := priceMap[strings.ToLower(strings.TrimSpace(p.Symbol))]; rt > 0 {
			line, kind := prof.RiskStopLine(p.CostPrice, peak)
			if rt <= line {
				reason := map[string]string{"硬止损": "stop_loss", "保本": "breakeven", "移动止损": "trail"}[kind]
				doClose(p, rt, today, reason, 0)
				continue
			}
		}
		// 2) 已确认K上判定(止盈/时间止损/收盘破线)
		if len(confirmed) > 0 {
			if r := services.EvaluateRiskExit(prof, p.CostPrice, p.OpenDate, confirmed); r.Exited {
				doClose(p, r.ExitPrice, r.ExitDate, r.Reason, r.HoldDays)
			}
		}
	}
	return closed
}

// UpdatePaperPosition 修改成本价/数量
func (a *App) UpdatePaperPosition(id int64, costPrice float64, shares int64) string {
	if a.paperService == nil {
		return "模拟持仓服务未初始化"
	}
	if err := a.paperService.Update(id, costPrice, shares); err != nil {
		return err.Error()
	}
	return "success"
}

// ClosePaperPosition 手动平仓（记录卖出价 → 计入胜率，扣成本净收益口径）
func (a *App) ClosePaperPosition(id int64, closePrice float64) string {
	if a.paperService == nil {
		return "模拟持仓服务未初始化"
	}
	if err := a.paperService.ClosePosition(id, closePrice, ""); err != nil {
		return err.Error()
	}
	return "success"
}

// ReopenPaperPosition 撤回平仓，恢复为未平仓，不再计入已平仓胜率样本。
func (a *App) ReopenPaperPosition(id int64) string {
	if a.paperService == nil {
		return "模拟持仓服务未初始化"
	}
	if err := a.paperService.Reopen(id); err != nil {
		return err.Error()
	}
	return "success"
}

// DeletePaperPosition 删除一笔
func (a *App) DeletePaperPosition(id int64) string {
	if a.paperService == nil {
		return "模拟持仓服务未初始化"
	}
	if err := a.paperService.Delete(id); err != nil {
		return err.Error()
	}
	return "success"
}

// GetPaperStats 按筛选系统统计胜率
func (a *App) GetPaperStats() models.PaperStats {
	if a.paperService == nil {
		return models.PaperStats{}
	}
	return a.paperService.Stats()
}

// GetTradeJournal 返回全部交易记录（最新在前）
func (a *App) GetTradeJournal() []models.TradeJournalEntry {
	if a.journalService == nil {
		return []models.TradeJournalEntry{}
	}
	list := a.journalService.List()
	a.syncSessionPositionsFromJournalList(list)
	a.fillOpenTradeFloatingPnL(list)
	return list
}

// GetTradeJournalStats 返回台账统计（总体 + 日/周/月）
func (a *App) GetTradeJournalStats() models.TradeJournalStats {
	if a.journalService == nil {
		return models.TradeJournalStats{}
	}
	stats := a.journalService.Stats()
	list := a.journalService.List()
	a.fillOpenTradeFloatingPnL(list)
	var openPnl float64
	var openPnlPctSum float64
	var openHoldDays int
	var openWithPrice int
	for _, e := range list {
		if e.Status != "open" {
			continue
		}
		if e.CurrentPrice > 0 && e.BuyPrice > 0 {
			openPnl += e.PnL
			openPnlPctSum += e.PnLPct
			openWithPrice++
		}
		openHoldDays += e.HoldDays
	}
	if openWithPrice > 0 {
		stats.Summary.TotalPnL = round2Float(stats.Summary.TotalPnL + openPnl)
		if stats.Summary.ClosedCount == 0 {
			stats.Summary.AvgPnLPct = round2Float(openPnlPctSum / float64(openWithPrice))
		}
	}
	openCount := stats.Summary.OpenCount
	if openCount > 0 && stats.Summary.ClosedCount == 0 {
		stats.Summary.AvgHoldDays = round2Float(float64(openHoldDays) / float64(openCount))
	}
	return stats
}

func (a *App) fillOpenTradeFloatingPnL(list []models.TradeJournalEntry) {
	if len(list) == 0 || a.marketService == nil {
		return
	}
	codes := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for i := range list {
		if list[i].Status != "open" || strings.TrimSpace(list[i].StockCode) == "" {
			continue
		}
		code := strings.TrimSpace(list[i].StockCode)
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return
	}
	priceMap := map[string]float64{}
	if stocks, err := a.marketService.GetStockRealTimeData(codes...); err == nil {
		for _, st := range stocks {
			if st.Price > 0 {
				priceMap[st.Symbol] = st.Price
			}
		}
	}
	today := time.Now().Format("2006-01-02")
	for i := range list {
		e := &list[i]
		if e.Status != "open" {
			e.CurrentPrice = e.SellPrice
			continue
		}
		cur := priceMap[e.StockCode]
		if cur <= 0 {
			cur = e.BuyPrice
		}
		e.CurrentPrice = cur
		if e.BuyPrice > 0 {
			e.PnLPct = round2Float((cur - e.BuyPrice) / e.BuyPrice * 100)
		}
		if e.Shares > 0 {
			e.PnL = round2Float((cur - e.BuyPrice) * float64(e.Shares))
		}
		e.HoldDays = calendarDaysForJournal(e.BuyDate, today)
	}
}

func calendarDaysForJournal(from, to string) int {
	f, err1 := time.Parse("2006-01-02", strings.TrimSpace(from))
	t, err2 := time.Parse("2006-01-02", strings.TrimSpace(to))
	if err1 != nil || err2 != nil {
		return 0
	}
	d := int(t.Sub(f).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

func round2Float(v float64) float64 {
	return math.Round(v*100) / 100
}

func (a *App) syncSessionPositionFromJournal(entry models.TradeJournalEntry) {
	if a.sessionService == nil {
		return
	}
	code := strings.TrimSpace(entry.StockCode)
	if code == "" {
		return
	}
	name := strings.TrimSpace(entry.StockName)
	if name == "" {
		name = code
	}
	if entry.Status == "open" && entry.Shares > 0 {
		if _, err := a.sessionService.GetOrCreateSession(code, name); err != nil {
			log.Warn("同步台账持仓失败，创建会话异常: %s %v", code, err)
			return
		}
		if err := a.sessionService.UpdatePosition(code, entry.Shares, entry.BuyPrice, entry.BuyDate); err != nil {
			log.Warn("同步台账持仓失败: %s %v", code, err)
		}
		return
	}
	if a.sessionService.GetSession(code) == nil {
		return
	}
	if err := a.sessionService.UpdatePosition(code, 0, 0, ""); err != nil {
		log.Warn("清空台账持仓失败: %s %v", code, err)
	}
}

func (a *App) syncSessionPositionsFromJournalList(list []models.TradeJournalEntry) {
	if a.sessionService == nil || len(list) == 0 {
		return
	}
	byCode := make(map[string]models.TradeJournalEntry)
	for _, entry := range list {
		code := strings.TrimSpace(entry.StockCode)
		if code == "" {
			continue
		}
		entry.StockCode = code
		current, exists := byCode[code]
		if entry.Status == "open" && entry.Shares > 0 {
			byCode[code] = entry
			continue
		}
		if !exists {
			byCode[code] = models.TradeJournalEntry{
				StockCode: code,
				StockName: entry.StockName,
				Status:    "closed",
			}
			continue
		}
		if current.Status != "open" && strings.TrimSpace(current.StockName) == "" {
			current.StockName = entry.StockName
			byCode[code] = current
		}
	}
	for _, entry := range byCode {
		a.syncSessionPositionFromJournal(entry)
	}
}

func (a *App) syncSessionPositionForStock(stockCode, stockName string) {
	code := strings.TrimSpace(stockCode)
	if code == "" {
		return
	}
	if a.journalService != nil {
		if entry, ok := a.journalService.OpenByStock(code); ok {
			a.syncSessionPositionFromJournal(entry)
			return
		}
	}
	a.syncSessionPositionFromJournal(models.TradeJournalEntry{
		StockCode: code,
		StockName: stockName,
		Status:    "closed",
	})
}

// SaveTradeJournal 手动新增/修改一笔
func (a *App) SaveTradeJournal(req models.TradeJournalRequest) string {
	if a.journalService == nil {
		return "台账服务未初始化"
	}
	var oldStockCode, oldStockName string
	if req.ID > 0 {
		for _, old := range a.journalService.List() {
			if old.ID == req.ID {
				oldStockCode = old.StockCode
				oldStockName = old.StockName
				break
			}
		}
	}
	entry, err := a.journalService.SaveManual(req)
	if err != nil {
		return err.Error()
	}
	a.syncSessionPositionForStock(entry.StockCode, entry.StockName)
	if strings.TrimSpace(oldStockCode) != "" && strings.TrimSpace(oldStockCode) != strings.TrimSpace(entry.StockCode) {
		a.syncSessionPositionForStock(oldStockCode, oldStockName)
	}
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		log.Warn("同步交易台账组失败: %v", err)
	}
	return "success"
}

// DeleteTradeJournal 删除一笔
func (a *App) DeleteTradeJournal(id int64) string {
	if a.journalService == nil {
		return "台账服务未初始化"
	}
	var stockCode, stockName string
	for _, entry := range a.journalService.List() {
		if entry.ID == id {
			stockCode = entry.StockCode
			stockName = entry.StockName
			break
		}
	}
	if err := a.journalService.Delete(id); err != nil {
		return err.Error()
	}
	a.syncSessionPositionForStock(stockCode, stockName)
	if err := a.syncTradeJournalWatchGroup(); err != nil {
		log.Warn("同步交易台账组失败: %v", err)
	}
	return "success"
}

// ========== Agent Config API ==========

// GetAgentConfigs 获取所有已启用的Agent配置
func (a *App) GetAgentConfigs() []models.AgentConfig {
	return a.strategyService.GetEnabledAgents()
}

// AddAgentConfig 添加Agent配置到当前策略
func (a *App) AddAgentConfig(config models.AgentConfig) string {
	agent := models.StrategyAgent{
		ID:          config.ID,
		Name:        config.Name,
		Role:        config.Role,
		Avatar:      config.Avatar,
		Color:       config.Color,
		Instruction: config.Instruction,
		Tools:       config.Tools,
		MCPServers:  config.MCPServers,
		Enabled:     config.Enabled,
	}
	if err := a.strategyService.AddAgentToActiveStrategy(agent); err != nil {
		return err.Error()
	}
	a.agentContainer.LoadAgents(a.strategyService.GetAllAgents())
	return "success"
}

// UpdateAgentConfig 更新当前策略中的Agent配置
func (a *App) UpdateAgentConfig(config models.AgentConfig) string {
	agent := models.StrategyAgent{
		ID:          config.ID,
		Name:        config.Name,
		Role:        config.Role,
		Avatar:      config.Avatar,
		Color:       config.Color,
		Instruction: config.Instruction,
		Tools:       config.Tools,
		MCPServers:  config.MCPServers,
		Enabled:     config.Enabled,
	}
	if err := a.strategyService.UpdateAgentInActiveStrategy(agent); err != nil {
		return err.Error()
	}
	a.agentContainer.LoadAgents(a.strategyService.GetAllAgents())
	return "success"
}

// DeleteAgentConfig 从当前策略删除Agent配置
func (a *App) DeleteAgentConfig(id string) string {
	if err := a.strategyService.DeleteAgentFromActiveStrategy(id); err != nil {
		return err.Error()
	}
	a.agentContainer.LoadAgents(a.strategyService.GetAllAgents())
	return "success"
}

// ========== Strategy API ==========

// GetStrategies 获取所有策略
func (a *App) GetStrategies() []models.Strategy {
	return a.strategyService.GetAllStrategies()
}

// GetActiveStrategyID 获取当前激活策略ID
func (a *App) GetActiveStrategyID() string {
	return a.strategyService.GetActiveID()
}

// SetActiveStrategy 设置当前激活策略
func (a *App) SetActiveStrategy(id string) string {
	if err := a.strategyService.SetActiveStrategy(id); err != nil {
		return err.Error()
	}
	// 重新加载Agent容器
	a.agentContainer.LoadAgents(a.strategyService.GetAllAgents())
	// 通知前端策略已切换
	rt.Emit("strategy:changed", id)
	return "success"
}

// AddStrategy 添加策略
func (a *App) AddStrategy(strategy models.Strategy) string {
	if err := a.strategyService.AddStrategy(strategy); err != nil {
		return err.Error()
	}
	return "success"
}

// UpdateStrategy 更新策略
func (a *App) UpdateStrategy(strategy models.Strategy) string {
	if err := a.strategyService.UpdateStrategy(strategy); err != nil {
		return err.Error()
	}
	return "success"
}

// DeleteStrategy 删除策略
func (a *App) DeleteStrategy(id string) string {
	if err := a.strategyService.DeleteStrategy(id); err != nil {
		return err.Error()
	}
	return "success"
}

// GenerateStrategyRequest AI生成策略请求
type GenerateStrategyRequest struct {
	Prompt string `json:"prompt"`
}

// GenerateStrategyResponse AI生成策略响应
type GenerateStrategyResponse struct {
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Strategy  models.Strategy `json:"strategy,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
}

// GenerateStrategy AI生成策略
func (a *App) GenerateStrategy(req GenerateStrategyRequest) GenerateStrategyResponse {
	// 获取策略生成AI配置（优先使用 StrategyAIID，否则使用默认）
	config := a.configService.GetConfig()
	var aiConfig *models.AIConfig
	targetAIID := config.StrategyAIID
	if targetAIID == "" {
		targetAIID = config.DefaultAIID
	}
	for i := range config.AIConfigs {
		if config.AIConfigs[i].ID == targetAIID {
			aiConfig = &config.AIConfigs[i]
			break
		}
	}
	if aiConfig == nil && len(config.AIConfigs) > 0 {
		aiConfig = &config.AIConfigs[0]
	}
	if aiConfig == nil {
		return GenerateStrategyResponse{Success: false, Error: "未配置AI服务"}
	}

	// 创建LLM
	ctx := context.Background()
	factory := adk.NewModelFactory()
	llm, err := factory.CreateModel(ctx, aiConfig)
	if err != nil {
		return GenerateStrategyResponse{Success: false, Error: err.Error()}
	}

	// 构建生成输入
	input := services.GenerateInput{
		Prompt: req.Prompt,
	}

	// 获取可用工具列表
	for _, t := range a.toolRegistry.GetAllToolInfos() {
		input.Tools = append(input.Tools, services.ToolInfoForGen{
			Name:        t.Name,
			Description: t.Description,
		})
	}

	// 获取已启用的MCP服务器列表
	for _, m := range config.MCPServers {
		if m.Enabled {
			// 获取该服务器的工具列表
			var toolNames []string
			if tools, err := a.mcpManager.GetServerTools(m.ID); err == nil {
				for _, t := range tools {
					toolNames = append(toolNames, t.Name)
				}
			}
			input.MCPServers = append(input.MCPServers, services.MCPInfoForGen{
				ID:    m.ID,
				Name:  m.Name,
				Tools: toolNames,
			})
		}
	}

	// 设置LLM并生成策略
	a.strategyService.SetLLM(llm)
	result, err := a.strategyService.Generate(ctx, input)
	if err != nil {
		return GenerateStrategyResponse{Success: false, Error: err.Error()}
	}

	// 保存策略
	if err := a.strategyService.AddStrategy(result.Strategy); err != nil {
		return GenerateStrategyResponse{Success: false, Error: err.Error()}
	}

	return GenerateStrategyResponse{
		Success:   true,
		Strategy:  result.Strategy,
		Reasoning: result.Reasoning,
	}
}

// EnhancePromptRequest 提示词增强请求
type EnhancePromptRequest struct {
	OriginalPrompt string `json:"originalPrompt"`
	AgentRole      string `json:"agentRole"`
	AgentName      string `json:"agentName"`
}

// EnhancePromptResponse 提示词增强响应
type EnhancePromptResponse struct {
	Success        bool   `json:"success"`
	EnhancedPrompt string `json:"enhancedPrompt,omitempty"`
	Error          string `json:"error,omitempty"`
}

// EnhancePrompt 增强Agent提示词
func (a *App) EnhancePrompt(req EnhancePromptRequest) EnhancePromptResponse {
	// 获取策略生成AI配置（优先使用 StrategyAIID，否则使用默认）
	config := a.configService.GetConfig()
	var aiConfig *models.AIConfig
	targetAIID := config.StrategyAIID
	if targetAIID == "" {
		targetAIID = config.DefaultAIID
	}
	for i := range config.AIConfigs {
		if config.AIConfigs[i].ID == targetAIID {
			aiConfig = &config.AIConfigs[i]
			break
		}
	}
	if aiConfig == nil && len(config.AIConfigs) > 0 {
		aiConfig = &config.AIConfigs[0]
	}
	if aiConfig == nil {
		return EnhancePromptResponse{Success: false, Error: "未配置AI服务"}
	}

	// 创建LLM
	ctx := context.Background()
	factory := adk.NewModelFactory()
	llm, err := factory.CreateModel(ctx, aiConfig)
	if err != nil {
		return EnhancePromptResponse{Success: false, Error: err.Error()}
	}

	// 设置LLM并增强提示词
	a.strategyService.SetLLM(llm)
	input := services.EnhancePromptInput{
		OriginalPrompt: req.OriginalPrompt,
		AgentRole:      req.AgentRole,
		AgentName:      req.AgentName,
	}
	result, err := a.strategyService.EnhancePrompt(ctx, input)
	if err != nil {
		return EnhancePromptResponse{Success: false, Error: err.Error()}
	}

	return EnhancePromptResponse{
		Success:        true,
		EnhancedPrompt: result.EnhancedPrompt,
	}
}

// GenerateBoardReport 使用老陈的完整提示词生成看板报告
func (a *App) GenerateBoardReport(req GenerateBoardReportRequest) GenerateBoardReportResponse {
	stockCode := strings.TrimSpace(req.StockCode)
	stockName := strings.TrimSpace(req.StockName)
	if stockCode == "" {
		return GenerateBoardReportResponse{Success: false, Error: "stockCode 不能为空"}
	}

	config := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(config)
	if aiConfig == nil {
		return GenerateBoardReportResponse{Success: false, StockCode: stockCode, StockName: stockName, Error: "未配置AI服务"}
	}

	stocks, _ := a.marketService.GetStockRealTimeData(stockCode)
	var stock models.Stock
	if len(stocks) > 0 {
		stock = stocks[0]
	}
	if stock.Symbol == "" {
		stock.Symbol = stockCode
	}
	if strings.TrimSpace(stock.Name) == "" {
		stock.Name = stockName
	}
	if stockName == "" {
		stockName = stock.Name
	}

	agentCfg, ok := a.findBoardReportAgent()
	if !ok {
		return GenerateBoardReportResponse{Success: false, StockCode: stockCode, StockName: stockName, Error: "未找到老陈基本面分析师配置"}
	}
	agentCfg.Tools = ensureBoardReportTools(agentCfg.Tools)

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, boardReportGenerateTimeout)
	defer cancel()

	position := a.sessionService.GetPosition(stockCode)
	coreContext := a.buildCoreContext(stockCode, stock, position)
	query := buildGenerateBoardReportQuery(stock, strings.TrimSpace(req.Period))
	chatReq := meeting.ChatRequest{
		StockCode:    stockCode,
		Stock:        stock,
		Agents:       []models.AgentConfig{agentCfg},
		Query:        query,
		CoreContext:  coreContext,
		Position:     position,
		AgentTimeout: boardReportGenerateTimeout,
		// 空回调强制流式：报告输出巨大，非流式会撞 AI 网关约5分钟的响应硬超时(实测每次都在5-6.5分钟被502掐死)
		Progress:     func(meeting.ProgressEvent) {},
	}

	responses, err := a.meetingService.SendMessage(ctx, aiConfig, chatReq)
	if err != nil {
		return GenerateBoardReportResponse{Success: false, StockCode: stockCode, StockName: stockName, AgentID: agentCfg.ID, AgentName: agentCfg.Name, Error: err.Error()}
	}

	var report string
	for _, resp := range responses {
		if strings.TrimSpace(resp.Content) != "" {
			report = strings.TrimSpace(openai.FilterVendorToolCallMarkers(resp.Content))
			break
		}
		if strings.TrimSpace(resp.Error) != "" {
			return GenerateBoardReportResponse{Success: false, StockCode: stockCode, StockName: stockName, AgentID: agentCfg.ID, AgentName: agentCfg.Name, Error: resp.Error}
		}
	}
	if report == "" {
		return GenerateBoardReportResponse{Success: false, StockCode: stockCode, StockName: stockName, AgentID: agentCfg.ID, AgentName: agentCfg.Name, Error: "老陈未返回有效报告"}
	}

	modelName := aiConfig.ModelName
	if agentAI := a.getAIConfigByID(agentCfg.AIConfigID); agentAI != nil {
		modelName = agentAI.ModelName
	}

	generatedAt := time.Now().Format("2006-01-02 15:04:05")
	// 生成一次要8分钟,落库缓存;同票同周期覆盖保留最新一份,重启/关弹窗后可秒回显
	if a.paperService != nil {
		if err := a.paperService.SaveBoardReport(services.BoardReportRecord{
			StockCode:   stockCode,
			Period:      strings.TrimSpace(req.Period),
			StockName:   stockName,
			Report:      report,
			AgentID:     agentCfg.ID,
			AgentName:   agentCfg.Name,
			ModelName:   modelName,
			GeneratedAt: generatedAt,
		}); err != nil {
			log.Warn("缓存看板报告失败 %s: %v", stockCode, err)
		}
	}

	return GenerateBoardReportResponse{
		Success:     true,
		StockCode:   stockCode,
		StockName:   stockName,
		Report:      report,
		AgentID:     agentCfg.ID,
		AgentName:   agentCfg.Name,
		ModelName:   modelName,
		GeneratedAt: generatedAt,
	}
}

// GetCachedBoardReportResponse 看板报告缓存查询响应
type GetCachedBoardReportResponse struct {
	Success     bool   `json:"success"`
	Found       bool   `json:"found"`
	StockCode   string `json:"stockCode,omitempty"`
	StockName   string `json:"stockName,omitempty"`
	Report      string `json:"report,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	ModelName   string `json:"modelName,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty"`
	Error       string `json:"error,omitempty"`
}

// GetCachedBoardReport 查询同票同周期的老陈完整报告缓存(未命中 found=false,不算错误)
func (a *App) GetCachedBoardReport(stockCode, period string) GetCachedBoardReportResponse {
	stockCode = strings.TrimSpace(stockCode)
	if stockCode == "" {
		return GetCachedBoardReportResponse{Success: false, Error: "stockCode 不能为空"}
	}
	if a.paperService == nil {
		return GetCachedBoardReportResponse{Success: true, Found: false, StockCode: stockCode}
	}
	rec, err := a.paperService.GetBoardReport(stockCode, strings.TrimSpace(period))
	if err != nil {
		return GetCachedBoardReportResponse{Success: false, StockCode: stockCode, Error: err.Error()}
	}
	if rec == nil {
		return GetCachedBoardReportResponse{Success: true, Found: false, StockCode: stockCode}
	}
	return GetCachedBoardReportResponse{
		Success:     true,
		Found:       true,
		StockCode:   rec.StockCode,
		StockName:   rec.StockName,
		Report:      rec.Report,
		AgentID:     rec.AgentID,
		AgentName:   rec.AgentName,
		ModelName:   rec.ModelName,
		GeneratedAt: rec.GeneratedAt,
	}
}

func (a *App) findBoardReportAgent() (models.AgentConfig, bool) {
	if agent := a.strategyService.GetAgentByID("fundamental"); agent != nil {
		return *agent, true
	}
	for _, agent := range a.strategyService.GetAllAgents() {
		if strings.Contains(agent.Name, "老陈") || strings.Contains(agent.Role, "基本面") {
			return agent, true
		}
	}
	return models.AgentConfig{}, false
}

func ensureBoardReportTools(tools []string) []string {
	required := []string{
		"get_stock_realtime",
		"get_market_indices",
		"get_kline_data",
		"get_news",
		"get_stock_announcements",
		"get_research_report",
		"get_report_content",
		"get_f10_overview",
		"get_f10_company",
		"get_f10_financials",
		"get_f10_main_indicators",
		"get_f10_valuation",
		"get_f10_valuation_trend",
		"get_f10_industry_compare",
		"get_f10_business",
		"get_f10_institutions",
		"get_f10_performance",
		"get_f10_shareholder_numbers",
		"get_f10_shareholder_changes",
		"get_f10_fund_flow",
	}
	seen := make(map[string]struct{}, len(tools)+len(required))
	merged := make([]string, 0, len(tools)+len(required))
	for _, name := range tools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	for _, name := range required {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return merged
}

func buildGenerateBoardReportQuery(stock models.Stock, period string) string {
	symbol := strings.TrimSpace(stock.Symbol)
	name := strings.TrimSpace(stock.Name)
	if period == "" {
		period = "日K/最近六个月看板"
	}

	var sb strings.Builder
	sb.WriteString("请使用你自己的老陈完整提示词，对当前股票生成一份【完整版看板报告】。\n")
	fmt.Fprintf(&sb, "股票代码：%s\n", symbol)
	if name != "" {
		fmt.Fprintf(&sb, "股票名称：%s\n", name)
	}
	sb.WriteString("计划交易周期：1-2周 / 1-3月；风险偏好：激进。\n")
	fmt.Fprintf(&sb, "当前看图周期：%s。\n\n", period)
	sb.WriteString("要求：\n")
	sb.WriteString("1. 必须先调用可用工具获取最新行情、财务、估值、业务、研报/新闻、K线或同行对比数据；数据不可得就明确写“数据不可得”，不要编。\n")
	sb.WriteString("2. 按你提示词里的完整框架输出，不要压缩成短回复，不受150字限制。\n")
	sb.WriteString("3. 关键数字要具体，结论要明确，判断标注置信度或概率。\n")
	sb.WriteString("4. 输出为可直接展示在看板里的中文 Markdown 报告，包含：行业与大环境、公司基本面、主营结构、管理层、财务体检、估值对比、短线博弈、三种情景、操作建议、反向思考、3-5句总结。\n")
	sb.WriteString("5. 如果工具数据与当前界面价格不一致，说明数据时间口径，优先以最新工具数据为准。")
	return sb.String()
}

// AskBoardReport 基于研报/摘要回答股票问题
func (a *App) AskBoardReport(req AskBoardReportRequest) AskBoardReportResponse {
	report := strings.TrimSpace(req.Report)
	question := strings.TrimSpace(req.Question)
	stockCode := strings.TrimSpace(req.StockCode)
	if report == "" || question == "" {
		return AskBoardReportResponse{Success: false, StockCode: stockCode, Error: "report 和 question 不能为空"}
	}

	config := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(config)
	if aiConfig == nil {
		return AskBoardReportResponse{Success: false, StockCode: stockCode, Error: "未配置AI服务"}
	}

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, boardReportTimeout)
	defer cancel()

	factory := adk.NewModelFactory()
	llm, err := factory.CreateModel(ctx, aiConfig)
	if err != nil {
		return AskBoardReportResponse{Success: false, StockCode: stockCode, Error: err.Error()}
	}

	prompt := buildBoardReportPrompt(stockCode, report, question)
	reqLLM := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(prompt)}},
		},
	}

	var result strings.Builder
	for resp, genErr := range llm.GenerateContent(ctx, reqLLM, false) {
		if genErr != nil {
			return AskBoardReportResponse{Success: false, StockCode: stockCode, Error: genErr.Error()}
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Thought {
					continue
				}
				if part.Text != "" {
					result.WriteString(part.Text)
				}
			}
		}
	}

	answer := strings.TrimSpace(openai.FilterVendorToolCallMarkers(result.String()))
	if answer == "" {
		answer = "不知道。"
	}

	return AskBoardReportResponse{
		Success:   true,
		StockCode: stockCode,
		Answer:    answer,
		ModelName: aiConfig.ModelName,
	}
}

func buildBoardReportPrompt(stockCode, report, question string) string {
	var sb strings.Builder
	sb.WriteString("你是股票研报问答助手。\n")
	sb.WriteString("只能基于【研报内容】和【当前问题】回答，不要引入外部信息，不知道就直接说不知道。\n")
	sb.WriteString("输出要求：简洁，优先给结论；如有依据，只引用研报中的信息；不要编造。\n\n")
	if strings.TrimSpace(stockCode) != "" {
		fmt.Fprintf(&sb, "股票代码：%s\n\n", strings.TrimSpace(stockCode))
	}
	sb.WriteString("【研报内容】\n")
	sb.WriteString(strings.TrimSpace(report))
	sb.WriteString("\n\n【当前问题】\n")
	sb.WriteString(strings.TrimSpace(question))
	sb.WriteString("\n\n【回答】")
	return sb.String()
}

// ========== Meeting Room API ==========

// MeetingMessageRequest 会议室消息请求
type MeetingMessageRequest struct {
	StockCode    string   `json:"stockCode"`
	Content      string   `json:"content"`
	MentionIds   []string `json:"mentionIds"`
	ReplyToId    string   `json:"replyToId"`
	ReplyContent string   `json:"replyContent"`
	Battle       bool     `json:"battle"` // 一键Battle：并行结束后追加比分裁决
}

// cancelMeetingInternal 内部取消会议方法
func (a *App) cancelMeetingInternal(stockCode string) {
	a.meetingCancelsMu.Lock()
	if cancel, ok := a.meetingCancels[stockCode]; ok {
		cancel()
		delete(a.meetingCancels, stockCode)
	}
	a.meetingCancelsMu.Unlock()
}

// CancelMeeting 取消指定股票的会议（前端调用）
func (a *App) CancelMeeting(stockCode string) bool {
	a.cancelMeetingInternal(stockCode)
	log.Info("会议已取消: %s", stockCode)
	return true
}

// SendMeetingMessage 发送会议室消息（@指定成员回复）
func (a *App) SendMeetingMessage(req MeetingMessageRequest) []models.ChatMessage {
	// 获取Session
	session := a.sessionService.GetSession(req.StockCode)
	if session == nil {
		log.Warn("session not found: %s", req.StockCode)
		return []models.ChatMessage{}
	}

	// 取消之前该股票的会议（如果有）
	a.cancelMeetingInternal(req.StockCode)

	// 创建可取消的 context
	meetingCtx, cancel := context.WithCancel(a.ctx)
	a.meetingCancelsMu.Lock()
	a.meetingCancels[req.StockCode] = cancel
	a.meetingCancelsMu.Unlock()

	// 会议结束后清理
	defer func() {
		a.meetingCancelsMu.Lock()
		delete(a.meetingCancels, req.StockCode)
		a.meetingCancelsMu.Unlock()
	}()

	// 先保存用户消息
	userMsg := models.ChatMessage{
		AgentID:   "user",
		AgentName: "老韭菜",
		Content:   req.Content,
		ReplyTo:   req.ReplyToId,
		Mentions:  req.MentionIds,
	}
	a.sessionService.AddMessage(req.StockCode, userMsg)

	// 获取股票数据
	stocks, _ := a.marketService.GetStockRealTimeData(req.StockCode)
	var stock models.Stock
	if len(stocks) > 0 {
		stock = stocks[0]
	}

	// 获取默认AI配置
	config := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(config)
	if aiConfig == nil {
		log.Warn("no AI config found")
		return []models.ChatMessage{}
	}

	// 获取持仓信息
	position := a.sessionService.GetPosition(req.StockCode)
	coreContext := a.buildCoreContext(req.StockCode, stock, position)

	// 判断是否为智能模式（无 @ 任何人）
	if len(req.MentionIds) == 0 {
		return a.runSmartMeeting(meetingCtx, req.StockCode, stock, req.Content, coreContext, aiConfig, position)
	}

	// 原有逻辑：@ 指定专家
	return a.runDirectMeeting(meetingCtx, req, stock, coreContext, aiConfig, position)
}

// runSmartMeeting 智能会议模式
func (a *App) runSmartMeeting(ctx context.Context, stockCode string, stock models.Stock, query string, coreContext string, aiConfig *models.AIConfig, position *models.StockPosition) []models.ChatMessage {
	allAgents := a.strategyService.GetEnabledAgents()
	chatReq := meeting.ChatRequest{
		StockCode:   stockCode,
		Stock:       stock,
		Query:       query,
		CoreContext: coreContext,
		AllAgents:   allAgents,
		Position:    position,
	}

	// 响应回调：每次发言完成后推送
	respCallback := func(resp meeting.ChatResponse) {
		msg := models.ChatMessage{
			AgentID:     resp.AgentID,
			AgentName:   resp.AgentName,
			Role:        resp.Role,
			Content:     resp.Content,
			Round:       resp.Round,
			MsgType:     resp.MsgType,
			Error:       resp.Error,
			MeetingMode: resp.MeetingMode,
		}
		a.sessionService.AddMessage(stockCode, msg)
		rt.Emit("meeting:message:"+stockCode, msg)
	}

	// 进度回调：工具调用、流式输出等细粒度事件
	progressCallback := func(event meeting.ProgressEvent) {
		rt.Emit("meeting:progress:"+stockCode, event)
	}

	responses, err := a.meetingService.RunSmartMeetingWithCallback(ctx, aiConfig, chatReq, respCallback, progressCallback)
	if err != nil {
		log.Error("runSmartMeeting error: %v", err)
		return []models.ChatMessage{}
	}

	// 返回所有响应（前端可能已通过事件收到，这里作为备份）
	var messages []models.ChatMessage
	for _, resp := range responses {
		messages = append(messages, models.ChatMessage{
			AgentID:     resp.AgentID,
			AgentName:   resp.AgentName,
			Role:        resp.Role,
			Content:     resp.Content,
			Round:       resp.Round,
			MsgType:     resp.MsgType,
			Error:       resp.Error,
			MeetingMode: resp.MeetingMode,
		})
	}
	return messages
}

// runDirectMeeting 直接 @ 指定专家模式（带事件推送）
func (a *App) runDirectMeeting(ctx context.Context, req MeetingMessageRequest, stock models.Stock, coreContext string, aiConfig *models.AIConfig, position *models.StockPosition) []models.ChatMessage {
	agentConfigs := a.strategyService.GetAgentsByIDs(req.MentionIds)
	if len(agentConfigs) == 0 {
		return []models.ChatMessage{}
	}

	chatReq := meeting.ChatRequest{
		Stock:        stock,
		Agents:       agentConfigs,
		Query:        req.Content,
		ReplyContent: req.ReplyContent,
		CoreContext:  coreContext,
		Position:     position,
		Battle:       req.Battle,
	}

	responses, err := a.meetingService.SendMessage(ctx, aiConfig, chatReq)
	if err != nil {
		log.Error("runDirectMeeting error: %v", err)
		return []models.ChatMessage{}
	}

	// 转换并保存响应，同时推送事件
	messages := a.convertSaveAndEmitResponses(req.StockCode, responses, req.ReplyToId)

	// 一键Battle：并行结束后追加"比分 + 主持人裁决"
	if req.Battle && len(responses) > 0 {
		if verdict, ok := a.meetingService.BattleVerdict(ctx, aiConfig, &stock, req.Content, responses); ok {
			vMsg := models.ChatMessage{
				AgentID:     verdict.AgentID,
				AgentName:   verdict.AgentName,
				Role:        verdict.Role,
				Content:     verdict.Content,
				MsgType:     verdict.MsgType,
				MeetingMode: verdict.MeetingMode,
			}
			a.sessionService.AddMessage(req.StockCode, vMsg)
			rt.Emit("meeting:message:"+req.StockCode, vMsg)
			messages = append(messages, vMsg)
		}
	}

	return messages
}

// convertSaveAndEmitResponses 转换响应、保存并推送事件（统一体验）
func (a *App) convertSaveAndEmitResponses(stockCode string, responses []meeting.ChatResponse, replyTo string) []models.ChatMessage {
	var messages []models.ChatMessage
	for _, resp := range responses {
		msg := models.ChatMessage{
			AgentID:     resp.AgentID,
			AgentName:   resp.AgentName,
			Role:        resp.Role,
			Content:     resp.Content,
			ReplyTo:     replyTo,
			Round:       resp.Round,
			MsgType:     resp.MsgType,
			Error:       resp.Error,
			MeetingMode: resp.MeetingMode,
		}
		// 保存单条消息
		a.sessionService.AddMessage(stockCode, msg)
		// 推送事件（与智能模式一致）
		rt.Emit("meeting:message:"+stockCode, msg)
		messages = append(messages, msg)
	}
	return messages
}

func (a *App) buildCoreContext(stockCode string, stock models.Stock, position *models.StockPosition) string {
	var sections []string

	if section := buildCoreQuoteSection(stock); section != "" {
		sections = append(sections, section)
	}
	if section := buildCorePositionSection(position, stock); section != "" {
		sections = append(sections, section)
	}
	if a.marketService != nil {
		if section := buildCoreMarketStatusSection(a.marketService.GetMarketStatus()); section != "" {
			sections = append(sections, section)
		}
	}
	if section := a.getOrBuildCoreRemoteContext(stockCode, func() (string, bool, error) {
		return a.buildCoreRemoteContext(stockCode)
	}); section != "" {
		sections = append(sections, section)
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func (a *App) buildCoreRemoteContext(stockCode string) (string, bool, error) {
	if a.marketService == nil && a.f10Service == nil {
		return "", false, nil
	}

	var sections []string
	var errs []error
	hasRemoteData := false

	if a.marketService != nil {
		if indices, err := a.marketService.GetMarketIndices(); err == nil {
			if section := buildCoreIndicesSection(indices); section != "" {
				sections = append(sections, section)
				hasRemoteData = true
			}
		} else {
			errs = append(errs, fmt.Errorf("indices: %w", err))
		}
		if announcements, err := a.marketService.GetStockAnnouncements(stockCode, 1, 3); err == nil {
			if section := buildCoreAnnouncementsSection(announcements); section != "" {
				sections = append(sections, section)
				hasRemoteData = true
			}
		} else if strings.TrimSpace(stockCode) != "" {
			errs = append(errs, fmt.Errorf("announcements: %w", err))
		}
	}
	if a.f10Service != nil {
		if valuation, err := a.f10Service.GetValuationByCode(stockCode); err == nil {
			if section := buildCoreValuationSection(valuation); section != "" {
				sections = append(sections, section)
				hasRemoteData = true
			}
		} else if strings.TrimSpace(stockCode) != "" {
			errs = append(errs, fmt.Errorf("valuation: %w", err))
		}
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n")), hasRemoteData, errors.Join(errs...)
}

func (a *App) getOrBuildCoreRemoteContext(stockCode string, builder func() (string, bool, error)) string {
	if strings.TrimSpace(stockCode) == "" {
		contextText, _, _ := builder()
		return contextText
	}

	if cached, ok := a.getCoreContextCache(stockCode); ok {
		if time.Since(cached.timestamp) <= a.coreContextCacheTTL {
			return cached.context
		}
	}

	contextText, hasRemoteData, err := builder()
	if strings.TrimSpace(contextText) != "" && (err == nil || hasRemoteData) {
		a.setCoreContextCache(stockCode, contextText)
		return contextText
	}
	if err != nil {
		if cached, ok := a.getCoreContextCache(stockCode); ok && strings.TrimSpace(cached.context) != "" {
			log.Warn("coreContext refresh failed for %s, use stale cache: %v", stockCode, err)
			return cached.context
		}
		log.Warn("coreContext refresh failed for %s: %v", stockCode, err)
	}
	return contextText
}

func (a *App) getCoreContextCache(stockCode string) (coreContextCacheEntry, bool) {
	a.coreContextCacheMu.RLock()
	defer a.coreContextCacheMu.RUnlock()
	entry, ok := a.coreContextCache[stockCode]
	return entry, ok
}

func (a *App) setCoreContextCache(stockCode string, contextText string) {
	a.coreContextCacheMu.Lock()
	defer a.coreContextCacheMu.Unlock()
	a.coreContextCache[stockCode] = coreContextCacheEntry{
		context:   contextText,
		timestamp: time.Now(),
	}
}

func buildCoreQuoteSection(stock models.Stock) string {
	if strings.TrimSpace(stock.Symbol) == "" && strings.TrimSpace(stock.Name) == "" {
		return ""
	}
	lines := []string{
		fmt.Sprintf("【标的快照】%s (%s)", stock.Name, stock.Symbol),
		fmt.Sprintf("现价 %.2f，涨跌幅 %.2f%%，涨跌 %.2f", stock.Price, stock.ChangePercent, stock.Change),
	}
	rangeParts := make([]string, 0, 4)
	if stock.Open > 0 {
		rangeParts = append(rangeParts, fmt.Sprintf("开盘 %.2f", stock.Open))
	}
	if stock.High > 0 {
		rangeParts = append(rangeParts, fmt.Sprintf("最高 %.2f", stock.High))
	}
	if stock.Low > 0 {
		rangeParts = append(rangeParts, fmt.Sprintf("最低 %.2f", stock.Low))
	}
	if stock.PreClose > 0 {
		rangeParts = append(rangeParts, fmt.Sprintf("昨收 %.2f", stock.PreClose))
	}
	if len(rangeParts) > 0 {
		lines = append(lines, strings.Join(rangeParts, "，"))
	}
	if stock.Volume > 0 || stock.Amount > 0 {
		lines = append(lines, fmt.Sprintf("成交量 %d，成交额 %.2f", stock.Volume, stock.Amount))
	}
	metaParts := make([]string, 0, 2)
	if strings.TrimSpace(stock.Sector) != "" {
		metaParts = append(metaParts, "板块 "+stock.Sector)
	}
	if strings.TrimSpace(stock.MarketCap) != "" {
		metaParts = append(metaParts, "市值 "+stock.MarketCap)
	}
	if len(metaParts) > 0 {
		lines = append(lines, strings.Join(metaParts, "，"))
	}
	return strings.Join(lines, "\n")
}

func buildCorePositionSection(position *models.StockPosition, stock models.Stock) string {
	if position == nil || position.Shares <= 0 {
		return ""
	}
	costAmount := float64(position.Shares) * position.CostPrice
	marketValue := float64(position.Shares) * stock.Price
	pnl := marketValue - costAmount
	pnlRatio := 0.0
	if costAmount > 0 {
		pnlRatio = pnl / costAmount * 100
	}
	return fmt.Sprintf("【用户持仓】持有 %d 股，成本价 %.2f，按现价估算市值 %.2f，浮盈亏 %.2f（%.2f%%）", position.Shares, position.CostPrice, marketValue, pnl, pnlRatio)
}

func buildCoreMarketStatusSection(status services.MarketStatus) string {
	return fmt.Sprintf("【市场状态】%s（status=%s，交易日=%t）", status.StatusText, status.Status, status.IsTradeDay)
}

func buildCoreIndicesSection(indices []models.MarketIndex) string {
	if len(indices) == 0 {
		return ""
	}
	parts := make([]string, 0, minInt(len(indices), 3))
	for _, idx := range indices {
		if idx.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %.2f (%.2f%%)", idx.Name, idx.Price, idx.ChangePercent))
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "【大盘环境】" + strings.Join(parts, "；")
}

func buildCoreValuationSection(valuation models.StockValuation) string {
	parts := make([]string, 0, 5)
	if valuation.PETTM != 0 {
		parts = append(parts, fmt.Sprintf("PE(TTM) %.2f", valuation.PETTM))
	}
	if valuation.PB != 0 {
		parts = append(parts, fmt.Sprintf("PB %.2f", valuation.PB))
	}
	if valuation.TotalMarketCap != 0 {
		parts = append(parts, fmt.Sprintf("总市值 %.2f亿", valuation.TotalMarketCap/1e8))
	}
	if valuation.FloatMarketCap != 0 {
		parts = append(parts, fmt.Sprintf("流通市值 %.2f亿", valuation.FloatMarketCap/1e8))
	}
	if valuation.TurnoverRate != 0 {
		parts = append(parts, fmt.Sprintf("换手率 %.2f%%", valuation.TurnoverRate))
	}
	if len(parts) == 0 {
		return ""
	}
	return "【估值摘要】" + strings.Join(parts, "，")
}

func buildCoreAnnouncementsSection(data models.StockAnnouncements) string {
	if len(data.Items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(data.Items))
	for _, item := range data.Items {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		title := item.Title
		if len([]rune(title)) > 28 {
			title = string([]rune(title)[:28]) + "..."
		}
		if item.NoticeDate != "" {
			parts = append(parts, fmt.Sprintf("%s %s", item.NoticeDate, title))
		} else {
			parts = append(parts, title)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "【最新公告】" + strings.Join(parts, "；")
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// RetryAgent 重试单个失败的专家（前端手动触发）
func (a *App) RetryAgent(stockCode string, agentId string, query string) models.ChatMessage {
	// 获取股票数据
	stocks, _ := a.marketService.GetStockRealTimeData(stockCode)
	var stock models.Stock
	if len(stocks) > 0 {
		stock = stocks[0]
	}

	// 获取 AI 配置
	config := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(config)
	if aiConfig == nil {
		log.Warn("RetryAgent: no AI config")
		return models.ChatMessage{AgentID: agentId, Error: "未配置 AI 服务"}
	}

	// 获取专家配置
	agents := a.strategyService.GetAgentsByIDs([]string{agentId})
	if len(agents) == 0 {
		log.Warn("RetryAgent: agent not found: %s", agentId)
		return models.ChatMessage{AgentID: agentId, Error: "专家不存在"}
	}
	agentCfg := agents[0]

	position := a.sessionService.GetPosition(stockCode)

	// 进度回调
	progressCallback := func(event meeting.ProgressEvent) {
		rt.Emit("meeting:progress:"+stockCode, event)
	}

	resp, err := a.meetingService.RetrySingleAgent(a.ctx, aiConfig, &agentCfg, &stock, query, progressCallback, position)

	msg := models.ChatMessage{
		AgentID:     resp.AgentID,
		AgentName:   resp.AgentName,
		Role:        resp.Role,
		Content:     resp.Content,
		Round:       resp.Round,
		MsgType:     resp.MsgType,
		Error:       resp.Error,
		MeetingMode: resp.MeetingMode,
	}

	if err != nil {
		log.Error("RetryAgent failed: %v", err)
		rt.Emit("meeting:message:"+stockCode, msg)
		return msg
	}

	// 成功：保存并推送
	a.sessionService.AddMessage(stockCode, msg)
	rt.Emit("meeting:message:"+stockCode, msg)
	return msg
}

// RetryAgentAndContinue 重试失败专家并继续执行剩余专家（前端手动触发）
func (a *App) RetryAgentAndContinue(stockCode string) []models.ChatMessage {
	if !a.meetingService.HasInterruptedMeeting(stockCode) {
		log.Warn("RetryAgentAndContinue: no interrupted meeting for %s", stockCode)
		return []models.ChatMessage{}
	}

	// 创建可取消的 context
	meetingCtx, cancel := context.WithCancel(a.ctx)
	a.meetingCancelsMu.Lock()
	a.meetingCancels[stockCode] = cancel
	a.meetingCancelsMu.Unlock()

	defer func() {
		a.meetingCancelsMu.Lock()
		delete(a.meetingCancels, stockCode)
		a.meetingCancelsMu.Unlock()
	}()

	// 响应回调
	respCallback := func(resp meeting.ChatResponse) {
		msg := models.ChatMessage{
			AgentID:     resp.AgentID,
			AgentName:   resp.AgentName,
			Role:        resp.Role,
			Content:     resp.Content,
			Round:       resp.Round,
			MsgType:     resp.MsgType,
			Error:       resp.Error,
			MeetingMode: resp.MeetingMode,
		}
		a.sessionService.AddMessage(stockCode, msg)
		rt.Emit("meeting:message:"+stockCode, msg)
	}

	// 进度回调
	progressCallback := func(event meeting.ProgressEvent) {
		rt.Emit("meeting:progress:"+stockCode, event)
	}

	responses, err := a.meetingService.ContinueMeeting(meetingCtx, stockCode, respCallback, progressCallback)
	if err != nil {
		log.Error("RetryAgentAndContinue error: %v", err)
		return []models.ChatMessage{}
	}

	var messages []models.ChatMessage
	for _, resp := range responses {
		messages = append(messages, models.ChatMessage{
			AgentID:     resp.AgentID,
			AgentName:   resp.AgentName,
			Role:        resp.Role,
			Content:     resp.Content,
			Round:       resp.Round,
			MsgType:     resp.MsgType,
			Error:       resp.Error,
			MeetingMode: resp.MeetingMode,
		})
	}
	return messages
}

// CancelInterruptedMeeting 取消中断的会议（用户放弃重试）
func (a *App) CancelInterruptedMeeting(stockCode string) bool {
	a.meetingService.CancelInterruptedMeeting(stockCode)
	return true
}

// ========== News API ==========

// GetTelegraphList 获取快讯列表
func (a *App) GetTelegraphList() []services.Telegraph {
	telegraphs, err := a.newsService.GetTelegraphList()
	if err != nil {
		return []services.Telegraph{}
	}
	return telegraphs
}

// OpenURL 在浏览器中打开URL
func (a *App) OpenURL(url string) {
	rt.BrowserOpenURL(url)
}

// ========== Tools API ==========

// GetAvailableTools 获取可用的内置工具列表
func (a *App) GetAvailableTools() []tools.ToolInfo {
	return a.toolRegistry.GetAllToolInfos()
}

// ========== MCP API ==========

// GetMCPServers 获取 MCP 服务器配置列表
func (a *App) GetMCPServers() []models.MCPServerConfig {
	config := a.configService.GetConfig()
	if config.MCPServers == nil {
		return []models.MCPServerConfig{}
	}
	return config.MCPServers
}

// AddMCPServer 添加 MCP 服务器配置
func (a *App) AddMCPServer(server models.MCPServerConfig) string {
	config := a.configService.GetConfig()
	config.MCPServers = append(config.MCPServers, server)
	if err := a.configService.UpdateConfig(config); err != nil {
		return err.Error()
	}
	// 重新加载 MCP 配置
	if err := a.mcpManager.LoadConfigs(config.MCPServers); err != nil {
		return err.Error()
	}
	return "success"
}

// UpdateMCPServer 更新 MCP 服务器配置
func (a *App) UpdateMCPServer(server models.MCPServerConfig) string {
	config := a.configService.GetConfig()
	for i, s := range config.MCPServers {
		if s.ID == server.ID {
			config.MCPServers[i] = server
			break
		}
	}
	if err := a.configService.UpdateConfig(config); err != nil {
		return err.Error()
	}
	if err := a.mcpManager.LoadConfigs(config.MCPServers); err != nil {
		return err.Error()
	}
	return "success"
}

// DeleteMCPServer 删除 MCP 服务器配置
func (a *App) DeleteMCPServer(id string) string {
	config := a.configService.GetConfig()
	var newServers []models.MCPServerConfig
	for _, s := range config.MCPServers {
		if s.ID != id {
			newServers = append(newServers, s)
		}
	}
	config.MCPServers = newServers
	if err := a.configService.UpdateConfig(config); err != nil {
		return err.Error()
	}
	if err := a.mcpManager.LoadConfigs(config.MCPServers); err != nil {
		return err.Error()
	}
	return "success"
}

// GetMCPStatus 获取所有 MCP 服务器连接状态
func (a *App) GetMCPStatus() []mcp.ServerStatus {
	return a.mcpManager.GetAllStatus()
}

// TestMCPConnection 测试指定 MCP 服务器连接
func (a *App) TestMCPConnection(serverID string) *mcp.ServerStatus {
	return a.mcpManager.TestConnection(serverID)
}

// TestAIConnection 测试 AI 配置连通性
// 连接成功后自动检测是否支持 system role，并持久化结果
func (a *App) TestAIConnection(config models.AIConfig) string {
	factory := adk.NewModelFactory()
	ctx := context.Background()
	if err := factory.TestConnection(ctx, &config); err != nil {
		log.Error("AI 连接测试失败 [%s]: %v", config.Name, err)
		return err.Error()
	}
	log.Info("AI 连接测试成功 [%s]", config.Name)

	// 连接成功后，探测是否支持 system role
	noSystemRole := factory.DetectSystemRoleSupport(ctx, &config)
	config.NoSystemRole = noSystemRole

	// 持久化检测结果到配置
	if appConfig := a.configService.GetConfig(); appConfig != nil {
		for i := range appConfig.AIConfigs {
			if appConfig.AIConfigs[i].ID == config.ID {
				appConfig.AIConfigs[i].NoSystemRole = noSystemRole
				if err := a.configService.UpdateConfig(appConfig); err != nil {
					log.Warn("保存 NoSystemRole 检测结果失败: %v", err)
				} else {
					log.Info("模型 [%s] NoSystemRole=%v 已保存", config.Name, noSystemRole)
				}
				break
			}
		}
	}

	return "success"
}

// GetMCPServerTools 获取指定 MCP 服务器的工具列表
func (a *App) GetMCPServerTools(serverID string) []mcp.ToolInfo {
	tools, err := a.mcpManager.GetServerTools(serverID)
	if err != nil {
		return []mcp.ToolInfo{}
	}
	return tools
}

// ========== Window Control API ==========

// WindowMinimize 最小化窗口
func (a *App) WindowMinimize() {
	rt.WindowMinimise()
}

// WindowMaximize 最大化/还原窗口
func (a *App) WindowMaximize() {
	rt.WindowToggleMaximise()
}

// WindowClose 关闭窗口
func (a *App) WindowClose() {
	rt.Quit()
}

// ========== HotTrend API ==========

// GetHotTrendPlatforms 获取支持的热点平台列表
func (a *App) GetHotTrendPlatforms() []hottrend.PlatformInfo {
	return hottrend.SupportedPlatforms
}

// GetHotTrend 获取单个平台的热点数据
func (a *App) GetHotTrend(platform string) hottrend.HotTrendResult {
	if a.hotTrendService == nil {
		return hottrend.HotTrendResult{Platform: platform, Error: "服务未初始化"}
	}
	return a.hotTrendService.GetHotTrend(platform)
}

// GetAllHotTrends 获取所有平台的热点数据
func (a *App) GetAllHotTrends() []hottrend.HotTrendResult {
	if a.hotTrendService == nil {
		return []hottrend.HotTrendResult{}
	}
	return a.hotTrendService.GetAllHotTrends()
}

// ========== Update API ==========

// CheckForUpdate 检查更新
func (a *App) CheckForUpdate() services.UpdateInfo {
	if a.updateService == nil {
		return services.UpdateInfo{Error: "更新服务未初始化"}
	}
	return a.updateService.CheckForUpdate()
}

// DoUpdate 执行更新
func (a *App) DoUpdate() string {
	if a.updateService == nil {
		return "更新服务未初始化"
	}
	if err := a.updateService.Update(); err != nil {
		return err.Error()
	}
	return "success"
}

// RestartApp 重启应用
func (a *App) RestartApp() string {
	if a.updateService == nil {
		return "更新服务未初始化"
	}
	if err := a.updateService.RestartApplication(); err != nil {
		return err.Error()
	}
	return "success"
}

// GetCurrentVersion 获取当前版本
func (a *App) GetCurrentVersion() string {
	if a.updateService == nil {
		return "unknown"
	}
	return a.updateService.GetCurrentVersion()
}

// GetTradeDates 获取交易日列表
func (a *App) GetTradeDates(days int) []string {
	if a.marketService == nil {
		return nil
	}
	dates, err := a.marketService.GetTradeDates(days)
	if err != nil {
		return nil
	}
	return dates
}

// GetTradingSchedule 获取交易时间表
func (a *App) GetTradingSchedule() *services.TradingSchedule {
	if a.marketService == nil {
		return nil
	}
	schedule := a.marketService.GetTradingSchedule()
	return &schedule
}

// GetLongHuBangList 获取龙虎榜列表
func (a *App) GetLongHuBangList(pageSize, pageNumber int, tradeDate string) *services.LongHuBangListResult {
	if a.longHuBangService == nil {
		return nil
	}
	result, err := a.longHuBangService.GetLongHuBangList(pageSize, pageNumber, tradeDate)
	if err != nil {
		log.Error("获取龙虎榜失败: %v", err)
		return nil
	}
	return result
}

// GetLongHuBangDetail 获取龙虎榜营业部明细
func (a *App) GetLongHuBangDetail(code, tradeDate string) []models.LongHuBangDetail {
	if a.longHuBangService == nil {
		return nil
	}
	details, err := a.longHuBangService.GetStockDetail(code, tradeDate)
	if err != nil {
		log.Error("获取龙虎榜明细失败: %v", err)
		return nil
	}
	return details
}

// GetBoardFundFlow 获取板块资金流
func (a *App) GetBoardFundFlow(category string, page, pageSize int) models.BoardFundFlowList {
	if a.marketService == nil {
		return models.BoardFundFlowList{}
	}
	data, err := a.marketService.GetBoardFundFlowList(category, page, pageSize)
	if err != nil {
		log.Error("获取板块资金流失败: %v", err)
		return models.BoardFundFlowList{Category: category}
	}
	return data
}

// GetBoardFundFlowOverview 获取板块主力净流入概览
func (a *App) GetBoardFundFlowOverview(category string, limit int) models.BoardFundFlowOverview {
	if a.marketService == nil {
		return models.BoardFundFlowOverview{Category: category}
	}
	data, err := a.marketService.GetBoardFundFlowOverview(category, limit)
	if err != nil {
		log.Error("获取板块主力净流入概览失败: %v", err)
		return models.BoardFundFlowOverview{Category: category}
	}
	return data
}

// GetBoardFundFlowTracking 获取板块主力资金实时追踪曲线
func (a *App) GetBoardFundFlowTracking(category string, limit int, interval string) models.BoardFundFlowTracking {
	if a.marketService == nil {
		return models.BoardFundFlowTracking{Category: category}
	}
	data, err := a.marketService.GetBoardFundFlowTracking(category, limit, interval)
	if err != nil {
		log.Error("获取板块主力资金实时追踪失败: %v", err)
		return models.BoardFundFlowTracking{Category: category, Warning: err.Error()}
	}
	return data
}

// GetMarketChangeDistribution 获取全A涨跌分布
func (a *App) GetMarketChangeDistribution(includeBeijing bool) models.MarketChangeDistribution {
	if a.marketService == nil {
		return models.MarketChangeDistribution{}
	}
	data, err := a.marketService.GetMarketChangeDistribution(includeBeijing)
	if err != nil {
		log.Error("获取涨跌分布失败: %v", err)
		return models.MarketChangeDistribution{}
	}
	return data
}

// GetStockMoves 获取盘口异动
func (a *App) GetStockMoves(moveType string, page, pageSize int) models.StockMoveList {
	if a.marketService == nil {
		return models.StockMoveList{MoveType: moveType}
	}
	data, err := a.marketService.GetStockMovesList(moveType, page, pageSize)
	if err != nil {
		log.Error("获取盘口异动失败: %v", err)
		return models.StockMoveList{MoveType: moveType}
	}
	return data
}

// GetBoardLeaders 获取板块龙头候选
func (a *App) GetBoardLeaders(boardCode string, limit int) models.BoardLeaderList {
	if a.marketService == nil {
		return models.BoardLeaderList{BoardCode: boardCode}
	}
	data, err := a.marketService.GetBoardLeaders(boardCode, limit)
	if err != nil {
		log.Error("获取板块龙头失败: %v", err)
		return models.BoardLeaderList{BoardCode: boardCode}
	}
	return data
}

// NotifyFrontendReady 前端通知已准备好，开始推送数据
func (a *App) NotifyFrontendReady() {
	if a.marketPusher != nil {
		a.marketPusher.SetReady()
	}
}
