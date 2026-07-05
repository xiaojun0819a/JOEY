package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

const (
	TradeJournalGroupID   = "trade-journal"
	TradeJournalGroupName = "交易台账组"
)

// ConfigService 配置服务
type ConfigService struct {
	configPath    string
	watchlistPath string
	groupsPath    string
	groupDefsPath string
	config        *models.AppConfig
	watchlist     []models.Stock
	stockGroups   map[string][]string // symbol -> 分组ID列表
	groupDefs     []models.StockGroup // 用户自定义分组定义
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
		groupsPath:    filepath.Join(dataDir, "watchlist_groups.json"),
		groupDefsPath: filepath.Join(dataDir, "watchlist_group_defs.json"),
		stockGroups:   make(map[string][]string),
	}

	if err := cs.loadConfig(); err != nil {
		return nil, err
	}
	if err := cs.loadWatchlist(); err != nil {
		return nil, err
	}
	cs.loadStockGroups()
	cs.loadStockGroupDefs()

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
		Push: models.PushConfig{
			Enabled:    false,
			DedupHours: 24,
			Monitor: models.MonitorConfig{
				Enabled:          false,
				IntervalMinutes:  15,
				AfterMarketCheck: true,
			},
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

// SyncStockGroupMembers 确保指定分组存在，并把 members 同步为该分组的完整成员。
// 只增删该 groupID，不影响股票已有的其他分组；也不会删除自选股本身。
func (cs *ConfigService) SyncStockGroupMembers(groupID, groupName string, members []models.Stock) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	groupID = strings.TrimSpace(groupID)
	groupName = strings.TrimSpace(groupName)
	if groupID == "" || groupName == "" {
		return fmt.Errorf("分组ID和名称不能为空")
	}

	defsChanged := false
	legacyGroupID := ""
	found := false
	for i := range cs.groupDefs {
		if cs.groupDefs[i].ID == groupID {
			found = true
			if cs.groupDefs[i].Name != groupName {
				cs.groupDefs[i].Name = groupName
				defsChanged = true
			}
			break
		}
		if cs.groupDefs[i].Name == groupName && legacyGroupID == "" {
			legacyGroupID = cs.groupDefs[i].ID
			cs.groupDefs[i].ID = groupID
			found = true
			defsChanged = true
			break
		}
	}
	if !found {
		cs.groupDefs = append(cs.groupDefs, models.StockGroup{ID: groupID, Name: groupName})
		defsChanged = true
	}

	targetSymbols := make(map[string]models.Stock)
	for _, stock := range members {
		symbol := normalizeWatchSymbol(stock.Symbol)
		if symbol == "" {
			continue
		}
		stock.Symbol = symbol
		if strings.TrimSpace(stock.Name) == "" {
			stock.Name = symbol
		}
		targetSymbols[symbol] = stock
	}

	watchChanged := false
	for symbol, stock := range targetSymbols {
		idx := -1
		for i := range cs.watchlist {
			if normalizeWatchSymbol(cs.watchlist[i].Symbol) == symbol {
				idx = i
				break
			}
		}
		if idx < 0 {
			cs.watchlist = append(cs.watchlist, stock)
			watchChanged = true
		} else {
			existing := cs.watchlist[idx]
			updated := existing
			if updated.Symbol != symbol {
				updated.Symbol = symbol
			}
			if strings.TrimSpace(updated.Name) == "" || updated.Name == existing.Symbol || updated.Name == symbol {
				if strings.TrimSpace(stock.Name) != "" {
					updated.Name = stock.Name
				}
			}
			if strings.TrimSpace(updated.Sector) == "" && strings.TrimSpace(stock.Sector) != "" {
				updated.Sector = stock.Sector
			}
			if updated != existing {
				cs.watchlist[idx] = updated
				watchChanged = true
			}
		}
	}

	groupsChanged := false
	if legacyGroupID != "" && legacyGroupID != groupID {
		for symbol, groups := range cs.stockGroups {
			if !stringSliceContains(groups, legacyGroupID) {
				continue
			}
			next := make([]string, 0, len(groups))
			for _, gid := range groups {
				if gid == legacyGroupID {
					gid = groupID
				}
				if gid != "" && !stringSliceContains(next, gid) {
					next = append(next, gid)
				}
			}
			cs.stockGroups[symbol] = next
			groupsChanged = true
		}
	}
	for symbol := range targetSymbols {
		current := normalizeGroupList(cs.stockGroups[symbol])
		if !stringSliceContains(current, groupID) {
			current = append(current, groupID)
			cs.stockGroups[symbol] = current
			groupsChanged = true
		}
	}
	for symbol, groups := range cs.stockGroups {
		normalizedSymbol := normalizeWatchSymbol(symbol)
		if _, shouldKeep := targetSymbols[normalizedSymbol]; shouldKeep {
			if normalizedSymbol != symbol {
				next := normalizeGroupList(groups)
				delete(cs.stockGroups, symbol)
				cs.stockGroups[normalizedSymbol] = next
				groupsChanged = true
			}
			continue
		}
		if !stringSliceContains(groups, groupID) {
			continue
		}
		next := make([]string, 0, len(groups))
		for _, gid := range groups {
			if gid != groupID && !stringSliceContains(next, gid) {
				next = append(next, gid)
			}
		}
		if len(next) == 0 {
			delete(cs.stockGroups, symbol)
		} else {
			cs.stockGroups[symbol] = next
		}
		groupsChanged = true
	}

	if defsChanged {
		if err := cs.saveStockGroupDefsLocked(); err != nil {
			return err
		}
	}
	if watchChanged {
		if err := cs.saveWatchlistLocked(); err != nil {
			return err
		}
	}
	if groupsChanged {
		if err := cs.saveStockGroupsLocked(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveFromWatchlist 移除自选股
func (cs *ConfigService) RemoveFromWatchlist(symbol string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i, s := range cs.watchlist {
		if s.Symbol == symbol {
			cs.watchlist = append(cs.watchlist[:i], cs.watchlist[i+1:]...)
			if _, ok := cs.stockGroups[symbol]; ok {
				delete(cs.stockGroups, symbol)
				_ = cs.saveStockGroupsLocked()
			}
			return cs.saveWatchlistLocked()
		}
	}
	return nil
}

// loadStockGroups 加载自选分组映射(symbol -> 分组ID列表)
func (cs *ConfigService) loadStockGroups() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	data, err := os.ReadFile(cs.groupsPath)
	if err != nil {
		cs.stockGroups = make(map[string][]string)
		return
	}
	var m map[string][]string
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		cs.stockGroups = make(map[string][]string)
		return
	}
	cs.stockGroups = m
}

// saveStockGroupsLocked 保存分组映射(需已持锁)
func (cs *ConfigService) saveStockGroupsLocked() error {
	data, err := json.MarshalIndent(cs.stockGroups, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.groupsPath, data, 0644)
}

// GetStockGroups 返回 symbol -> 分组ID列表 的副本
func (cs *ConfigService) GetStockGroups() map[string][]string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	out := make(map[string][]string, len(cs.stockGroups))
	for k, v := range cs.stockGroups {
		if len(v) == 0 {
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// SetStockGroups 设置某只股票所属分组(覆盖式)；空列表表示从所有分组移除
func (cs *ConfigService) SetStockGroups(symbol string, groups []string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if symbol == "" {
		return nil
	}
	// 仅保留已定义的分组ID，去重
	valid := map[string]bool{}
	for _, d := range cs.groupDefs {
		valid[d.ID] = true
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(groups))
	for _, g := range groups {
		if valid[g] && !seen[g] {
			seen[g] = true
			clean = append(clean, g)
		}
	}
	if len(clean) == 0 {
		delete(cs.stockGroups, symbol)
	} else {
		cs.stockGroups[symbol] = clean
	}
	return cs.saveStockGroupsLocked()
}

// ===== 分组定义(用户自定义) =====

// loadStockGroupDefs 加载分组定义；首次无文件则种入默认 低吸/波段
func (cs *ConfigService) loadStockGroupDefs() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	data, err := os.ReadFile(cs.groupDefsPath)
	if err == nil {
		var defs []models.StockGroup
		if err := json.Unmarshal(data, &defs); err == nil && defs != nil {
			cs.groupDefs = defs
			return
		}
	}
	// 默认分组，保持已有 watchlist_groups.json 中的 lowbuy/wave 可用
	cs.groupDefs = []models.StockGroup{
		{ID: "lowbuy", Name: "低吸"},
		{ID: "wave", Name: "波段"},
	}
	_ = cs.saveStockGroupDefsLocked()
}

func (cs *ConfigService) saveStockGroupDefsLocked() error {
	data, err := json.MarshalIndent(cs.groupDefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.groupDefsPath, data, 0644)
}

// GetStockGroupDefs 返回分组定义副本
func (cs *ConfigService) GetStockGroupDefs() []models.StockGroup {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	out := make([]models.StockGroup, len(cs.groupDefs))
	copy(out, cs.groupDefs)
	return out
}

// AddStockGroupDef 新建分组，返回新分组(含生成的ID)
func (cs *ConfigService) AddStockGroupDef(name string) (models.StockGroup, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return models.StockGroup{}, fmt.Errorf("分组名不能为空")
	}
	for _, d := range cs.groupDefs {
		if d.Name == name {
			return d, nil // 同名则复用
		}
	}
	g := models.StockGroup{ID: fmt.Sprintf("g%d", time.Now().UnixNano()), Name: name}
	cs.groupDefs = append(cs.groupDefs, g)
	if err := cs.saveStockGroupDefsLocked(); err != nil {
		return models.StockGroup{}, err
	}
	return g, nil
}

// RenameStockGroupDef 重命名分组
func (cs *ConfigService) RenameStockGroupDef(id, name string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("分组名不能为空")
	}
	for i := range cs.groupDefs {
		if cs.groupDefs[i].ID == id {
			cs.groupDefs[i].Name = name
			return cs.saveStockGroupDefsLocked()
		}
	}
	return fmt.Errorf("分组不存在")
}

// DeleteStockGroupDef 删除分组，并从所有股票映射中剔除该分组
func (cs *ConfigService) DeleteStockGroupDef(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	idx := -1
	for i := range cs.groupDefs {
		if cs.groupDefs[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	cs.groupDefs = append(cs.groupDefs[:idx], cs.groupDefs[idx+1:]...)
	// 清理映射
	changed := false
	for sym, gs := range cs.stockGroups {
		next := make([]string, 0, len(gs))
		for _, g := range gs {
			if g != id {
				next = append(next, g)
			}
		}
		if len(next) != len(gs) {
			changed = true
			if len(next) == 0 {
				delete(cs.stockGroups, sym)
			} else {
				cs.stockGroups[sym] = next
			}
		}
	}
	if changed {
		_ = cs.saveStockGroupsLocked()
	}
	return cs.saveStockGroupDefsLocked()
}

func normalizeWatchSymbol(symbol string) string {
	return strings.ToLower(strings.TrimSpace(symbol))
}

func normalizeGroupList(groups []string) []string {
	out := make([]string, 0, len(groups))
	for _, gid := range groups {
		gid = strings.TrimSpace(gid)
		if gid == "" || stringSliceContains(out, gid) {
			continue
		}
		out = append(out, gid)
	}
	return out
}

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// SearchStocks 搜索股票
func (cs *ConfigService) SearchStocks(keyword string, limit int) []StockSearchResult {
	return searchEmbeddedStocks(keyword, limit)
}
