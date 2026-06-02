package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/adk"
	"github.com/run-bigpig/jcp/internal/adk/mcp"
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

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var log = logger.New("app")

const defaultCoreContextCacheTTL = 30 * time.Second

const (
	lowBuyGateLimitUpMin   = 50
	lowBuyGateLimitDownMax = 60
	lowBuyGateAmountMin    = 9e11
)

type coreContextCacheEntry struct {
	context   string
	timestamp time.Time
}

// App struct
type App struct {
	ctx               context.Context
	configService     *services.ConfigService
	marketService     *services.MarketService
	newsService       *services.NewsService
	f10Service        *services.F10Service
	historyService    *services.HistoryService
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

	// 初始化策略服务
	strategyService := services.NewStrategyService(dataDir)

	// 初始化Agent容器（直接从StrategyService获取数据）
	agentContainer := agent.NewContainer()
	agentContainer.LoadAgents(strategyService.GetAllAgents())

	// 初始化更新服务
	updateService := services.NewUpdateService("run-bigpig", "jcp", Version)

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
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

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

	// 初始化并启动市场数据推送服务（需要 context）
	a.marketPusher = services.NewMarketDataPusher(a.marketService, a.configService, a.newsService)
	a.marketPusher.Start(ctx)
	log.Info("市场数据推送服务已启动")

	if a.historyService != nil {
		a.historyService.StartAutoCollect(ctx)
		log.Info("历史数据自动采集检查服务已启动")
	}

	// 启动 OpenClaw 服务（如果已启用）
	cfg := a.configService.GetConfig()
	if cfg.OpenClaw.Enabled && cfg.OpenClaw.Port > 0 {
		if err := a.openClawServer.Start(cfg.OpenClaw.Port, cfg.OpenClaw.APIKey); err != nil {
			log.Warn("OpenClaw 启动失败: %v", err)
		}
	}
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

// GetStockRealTimeData 获取股票实时数据
func (a *App) GetStockRealTimeData(codes []string) []models.Stock {
	stocks, _ := a.marketService.GetStockRealTimeData(codes...)
	return stocks
}

// GetKLineData 获取K线数据
func (a *App) GetKLineData(code string, period string, days int) []models.KLineData {
	data, _ := a.marketService.GetKLineData(code, period, days)
	return data
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
	historyCheckedCount := 0
	historyFailedCount := 0
	historyRejectedCount := 0

	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "V1.1 高胜率短线规则（全A扫描）",
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

	// V1.1 行业白/黑名单（用行业关键词做粗过滤）
	allowIndustryKeywords := []string{"半导体", "军工", "新能源", "医疗", "医药", "器械", "软件", "通信", "元器件", "计算机", "人工智能", "算力"}
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
		if !hitKeyword(industry, allowIndustryKeywords) {
			// 对行业未知/不在白名单的，降级不入候选（V1先严格）
			continue
		}
		// 新规则：当日涨幅 <= +1.5%（低吸不追涨）
		if row.ChangePercent > 1.5 {
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
		// 涨跌结构分
		if chg >= -2.0 && chg < 0 {
			score += 6
		} else if chg >= 0 && chg < 1.0 {
			score += 4
		} else if chg >= 1.0 && chg <= 1.5 {
			score -= 3
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

	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, "已启用V1.1筛选：涨幅[-3%,1.5%]、换手<=8%、主力强度>=1%且净流入>=800万（真实源）、单行业最多3只")
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
	return result
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

// UpdateStockPosition 更新股票持仓信息
func (a *App) UpdateStockPosition(stockCode string, shares int64, costPrice float64) string {
	if a.sessionService == nil {
		return "service not ready"
	}
	if err := a.sessionService.UpdatePosition(stockCode, shares, costPrice); err != nil {
		return err.Error()
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
	runtime.EventsEmit(a.ctx, "strategy:changed", id)
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

// ========== Meeting Room API ==========

// MeetingMessageRequest 会议室消息请求
type MeetingMessageRequest struct {
	StockCode    string   `json:"stockCode"`
	Content      string   `json:"content"`
	MentionIds   []string `json:"mentionIds"`
	ReplyToId    string   `json:"replyToId"`
	ReplyContent string   `json:"replyContent"`
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
		runtime.EventsEmit(a.ctx, "meeting:message:"+stockCode, msg)
	}

	// 进度回调：工具调用、流式输出等细粒度事件
	progressCallback := func(event meeting.ProgressEvent) {
		runtime.EventsEmit(a.ctx, "meeting:progress:"+stockCode, event)
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
	}

	responses, err := a.meetingService.SendMessage(ctx, aiConfig, chatReq)
	if err != nil {
		log.Error("runDirectMeeting error: %v", err)
		return []models.ChatMessage{}
	}

	// 转换并保存响应，同时推送事件
	return a.convertSaveAndEmitResponses(req.StockCode, responses, req.ReplyToId)
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
		runtime.EventsEmit(a.ctx, "meeting:message:"+stockCode, msg)
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
		runtime.EventsEmit(a.ctx, "meeting:progress:"+stockCode, event)
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
		runtime.EventsEmit(a.ctx, "meeting:message:"+stockCode, msg)
		return msg
	}

	// 成功：保存并推送
	a.sessionService.AddMessage(stockCode, msg)
	runtime.EventsEmit(a.ctx, "meeting:message:"+stockCode, msg)
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
		runtime.EventsEmit(a.ctx, "meeting:message:"+stockCode, msg)
	}

	// 进度回调
	progressCallback := func(event meeting.ProgressEvent) {
		runtime.EventsEmit(a.ctx, "meeting:progress:"+stockCode, event)
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
	runtime.BrowserOpenURL(a.ctx, url)
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
	runtime.WindowMinimise(a.ctx)
}

// WindowMaximize 最大化/还原窗口
func (a *App) WindowMaximize() {
	runtime.WindowToggleMaximise(a.ctx)
}

// WindowClose 关闭窗口
func (a *App) WindowClose() {
	runtime.Quit(a.ctx)
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
