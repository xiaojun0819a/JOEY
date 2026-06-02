package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/run-bigpig/jcp/internal/models"
)

// ConfigService 配置服务
type ConfigService struct {
	configPath    string
	watchlistPath string
	config        *models.AppConfig
	watchlist     []models.Stock
	mu            sync.RWMutex
}

// NewConfigService 创建配置服务
func NewConfigService(dataDir string) (*ConfigService, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	cs := &ConfigService{
		configPath:    filepath.Join(dataDir, "config.json"),
		watchlistPath: filepath.Join(dataDir, "watchlist.json"),
	}

	if err := cs.loadConfig(); err != nil {
		return nil, err
	}
	if err := cs.loadWatchlist(); err != nil {
		return nil, err
	}

	return cs, nil
}

// loadConfig 加载配置
func (cs *ConfigService) loadConfig() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	data, err := os.ReadFile(cs.configPath)
	if os.IsNotExist(err) {
		cs.config = cs.defaultConfig()
		return cs.saveConfigLocked()
	}
	if err != nil {
		return err
	}

	var config models.AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// 用于识别字段是否在 JSON 中显式存在（避免把用户明确设置的 false 当成缺失字段）
	var raw struct {
		AIRetryCount        *int    `json:"aiRetryCount"`
		VerboseAgentIO      *bool   `json:"verboseAgentIO"`
		AgentSelectionStyle *string `json:"agentSelectionStyle"`
		EnableSecondReview  *bool   `json:"enableSecondReview"`
		History             struct {
			AutoCollectDaily *bool `json:"autoCollectDaily"`
		} `json:"history"`
		Indicators struct {
			MA struct {
				Enabled *bool `json:"enabled"`
			} `json:"ma"`
			EMA struct {
				Enabled *bool `json:"enabled"`
			} `json:"ema"`
			BOLL struct {
				Enabled *bool `json:"enabled"`
			} `json:"boll"`
			MACD struct {
				Enabled *bool `json:"enabled"`
			} `json:"macd"`
			RSI struct {
				Enabled *bool `json:"enabled"`
			} `json:"rsi"`
			KDJ struct {
				Enabled *bool `json:"enabled"`
			} `json:"kdj"`
		} `json:"indicators"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// 旧配置文件可能缺少 indicators 字段，Go 零值（nil/0/0.0）会导致前端异常
	// 用默认值补全所有未设置的字段
	defaultConfig := cs.defaultConfig()
	if raw.AIRetryCount == nil || config.AIRetryCount <= 0 {
		config.AIRetryCount = defaultConfig.AIRetryCount
	}
	if raw.VerboseAgentIO == nil {
		config.VerboseAgentIO = defaultConfig.VerboseAgentIO
	}
	if raw.AgentSelectionStyle == nil || config.AgentSelectionStyle == "" {
		config.AgentSelectionStyle = defaultConfig.AgentSelectionStyle
	}
	if raw.EnableSecondReview == nil {
		config.EnableSecondReview = defaultConfig.EnableSecondReview
	}
	if raw.History.AutoCollectDaily == nil {
		config.History = defaultConfig.History
	} else {
		if strings.TrimSpace(config.History.CollectStart) == "" {
			config.History.CollectStart = defaultConfig.History.CollectStart
		}
		if strings.TrimSpace(config.History.CollectEnd) == "" {
			config.History.CollectEnd = defaultConfig.History.CollectEnd
		}
	}

	d := defaultConfig.Indicators
	ind := &config.Indicators
	if raw.Indicators.MA.Enabled == nil {
		ind.MA.Enabled = d.MA.Enabled
	}
	if ind.MA.Periods == nil {
		ind.MA.Periods = d.MA.Periods
	}
	if raw.Indicators.EMA.Enabled == nil {
		ind.EMA.Enabled = d.EMA.Enabled
	}
	if ind.EMA.Periods == nil {
		ind.EMA.Periods = d.EMA.Periods
	}
	if raw.Indicators.BOLL.Enabled == nil {
		ind.BOLL.Enabled = d.BOLL.Enabled
	}
	if ind.BOLL.Period == 0 {
		ind.BOLL.Period = d.BOLL.Period
	}
	if ind.BOLL.Multiplier == 0 {
		ind.BOLL.Multiplier = d.BOLL.Multiplier
	}
	if raw.Indicators.MACD.Enabled == nil {
		ind.MACD.Enabled = d.MACD.Enabled
	}
	if ind.MACD.Fast == 0 {
		ind.MACD.Fast = d.MACD.Fast
	}
	if ind.MACD.Slow == 0 {
		ind.MACD.Slow = d.MACD.Slow
	}
	if ind.MACD.Signal == 0 {
		ind.MACD.Signal = d.MACD.Signal
	}
	if raw.Indicators.RSI.Enabled == nil {
		ind.RSI.Enabled = d.RSI.Enabled
	}
	if ind.RSI.Period == 0 {
		ind.RSI.Period = d.RSI.Period
	}
	if raw.Indicators.KDJ.Enabled == nil {
		ind.KDJ.Enabled = d.KDJ.Enabled
	}
	if ind.KDJ.Period == 0 {
		ind.KDJ.Period = d.KDJ.Period
	}
	if ind.KDJ.K == 0 {
		ind.KDJ.K = d.KDJ.K
	}
	if ind.KDJ.D == 0 {
		ind.KDJ.D = d.KDJ.D
	}
	cs.config = &config
	return nil
}

// defaultConfig 默认配置
func (cs *ConfigService) defaultConfig() *models.AppConfig {
	return &models.AppConfig{
		Theme:               "military",
		CandleColorMode:     "red-up",
		AIConfigs:           []models.AIConfig{},
		DefaultAIID:         "",
		AIRetryCount:        2,
		VerboseAgentIO:      false,
		AgentSelectionStyle: models.AgentSelectionBalanced,
		EnableSecondReview:  false,
		Memory: models.MemoryConfig{
			Enabled:           true,
			MaxRecentRounds:   3,
			MaxKeyFacts:       20,
			MaxSummaryLength:  300,
			CompressThreshold: 5,
		},
		Indicators: models.IndicatorConfig{
			MA:   models.MAConfig{Enabled: true, Periods: []int{5, 10, 20}},
			EMA:  models.EMAConfig{Enabled: false, Periods: []int{12, 26}},
			BOLL: models.BOLLConfig{Enabled: false, Period: 20, Multiplier: 2.0},
			MACD: models.MACDConfig{Enabled: true, Fast: 12, Slow: 26, Signal: 9},
			RSI:  models.RSIConfig{Enabled: false, Period: 14},
			KDJ:  models.KDJConfig{Enabled: false, Period: 9, K: 3, D: 3},
		},
		History: models.HistoryConfig{
			AutoCollectDaily: false,
			CollectStart:     "16:00",
			CollectEnd:       "17:00",
			IncludeBeijing:   false,
		},
	}
}

// saveConfigLocked 保存配置(需要已持有锁)
func (cs *ConfigService) saveConfigLocked() error {
	data, err := json.MarshalIndent(cs.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.configPath, data, 0644)
}

// GetConfig 获取配置
func (cs *ConfigService) GetConfig() *models.AppConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config
}

// UpdateConfig 更新配置
func (cs *ConfigService) UpdateConfig(config *models.AppConfig) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.config = config
	return cs.saveConfigLocked()
}

// loadWatchlist 加载自选股列表
func (cs *ConfigService) loadWatchlist() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	data, err := os.ReadFile(cs.watchlistPath)
	if os.IsNotExist(err) {
		// 文件不存在时，初始化为空列表
		cs.watchlist = []models.Stock{}
		return cs.saveWatchlistLocked()
	}
	if err != nil {
		return err
	}

	var watchlist []models.Stock
	if err := json.Unmarshal(data, &watchlist); err != nil {
		return err
	}

	cs.watchlist = watchlist
	return nil
}

// saveWatchlistLocked 保存自选股(需要已持有锁)
func (cs *ConfigService) saveWatchlistLocked() error {
	data, err := json.MarshalIndent(cs.watchlist, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.watchlistPath, data, 0644)
}

// GetWatchlist 获取自选股列表
func (cs *ConfigService) GetWatchlist() []models.Stock {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.watchlist
}

// AddToWatchlist 添加自选股
func (cs *ConfigService) AddToWatchlist(stock models.Stock) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, s := range cs.watchlist {
		if s.Symbol == stock.Symbol {
			return nil
		}
	}
	cs.watchlist = append(cs.watchlist, stock)
	return cs.saveWatchlistLocked()
}

// RemoveFromWatchlist 移除自选股
func (cs *ConfigService) RemoveFromWatchlist(symbol string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i, s := range cs.watchlist {
		if s.Symbol == symbol {
			cs.watchlist = append(cs.watchlist[:i], cs.watchlist[i+1:]...)
			return cs.saveWatchlistLocked()
		}
	}
	return nil
}

// SearchStocks 搜索股票
func (cs *ConfigService) SearchStocks(keyword string, limit int) []StockSearchResult {
	return searchEmbeddedStocks(keyword, limit)
}
