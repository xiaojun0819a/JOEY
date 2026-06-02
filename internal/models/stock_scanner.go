package models

// LowBuyScannerRequest 低吸选股 V1 扫描请求
type LowBuyScannerRequest struct {
	Limit int `json:"limit"`
	// 是否包含北交所，默认 false（先聚焦沪深主战场）
	IncludeBeijing bool `json:"includeBeijing"`
	// 是否启用历史质量筛选；默认 false，保持 V1.1 当日规则
	HistoryFilterEnabled bool `json:"historyFilterEnabled"`
	// 连续 N 日换手低于阈值
	HistoryTurnoverDays int `json:"historyTurnoverDays"`
	// 连续换手阈值（百分比）
	HistoryTurnoverMax float64 `json:"historyTurnoverMax"`
	// 主力净流入历史观察天数
	HistoryMainFlowDays int `json:"historyMainFlowDays"`
	// 主力净流入为正的最少天数
	HistoryMainFlowPositiveDays int `json:"historyMainFlowPositiveDays"`
	// 均线周期，默认 MA10
	HistoryMAPeriod int `json:"historyMAPeriod"`
}

// HistoryCollectRequest 历史采集请求
type HistoryCollectRequest struct {
	// 为空则使用今天
	TradeDate string `json:"tradeDate"`
	// 是否包含北交所
	IncludeBeijing bool `json:"includeBeijing"`
	// 后端内部字段：是否自动任务触发
	TriggeredByAuto bool `json:"triggeredByAuto,omitempty"`
}

// HistoryCollectResult 历史采集结果
type HistoryCollectResult struct {
	TradeDate   string `json:"tradeDate"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	DBPath      string `json:"dbPath"`
	TotalCount  int    `json:"totalCount"`
	SavedCount  int    `json:"savedCount"`
	FailedCount int    `json:"failedCount"`
	MAUpdated   bool   `json:"maUpdated"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

// HistoryAutoCollectRequest 自动采集配置请求
type HistoryAutoCollectRequest struct {
	Enabled        bool   `json:"enabled"`
	CollectStart   string `json:"collectStart"`
	CollectEnd     string `json:"collectEnd"`
	IncludeBeijing bool   `json:"includeBeijing"`
}

// HistoryAutoCollectStatus 自动采集状态
type HistoryAutoCollectStatus struct {
	Enabled         bool   `json:"enabled"`
	CollectStart    string `json:"collectStart"`
	CollectEnd      string `json:"collectEnd"`
	IncludeBeijing  bool   `json:"includeBeijing"`
	LastCollectDate string `json:"lastCollectDate"`
	DBPath          string `json:"dbPath"`
	Message         string `json:"message"`
}

// LowBuyScannerResult 低吸选股 V1 扫描结果
type LowBuyScannerResult struct {
	AsOf              string               `json:"asOf"`
	RuleVersion       string               `json:"ruleVersion"`
	UniverseCount     int                  `json:"universeCount"`
	CandidateCount    int                  `json:"candidateCount"`
	SelectedCount     int                  `json:"selectedCount"`
	MarketGatePassed  bool                 `json:"marketGatePassed"`
	MarketGateReasons []string             `json:"marketGateReasons"`
	MarketOverview    LowBuyMarketOverview `json:"marketOverview"`
	Items             []LowBuyScannerItem  `json:"items"`
	Warning           string               `json:"warning,omitempty"`
}

// LowBuyMarketOverview 扫描时的大盘快照
type LowBuyMarketOverview struct {
	ShPrice        float64 `json:"shPrice"`
	ShMA20         float64 `json:"shMA20"`
	LimitUpCount   int     `json:"limitUpCount"`
	LimitDownCount int     `json:"limitDownCount"`
	TotalAmount    float64 `json:"totalAmount"`
}

// LowBuyScannerItem 单条候选结果
type LowBuyScannerItem struct {
	Symbol             string   `json:"symbol"`
	Name               string   `json:"name"`
	Price              float64  `json:"price"`
	ChangePercent      float64  `json:"changePercent"`
	Amount             float64  `json:"amount"`
	TurnoverRate       float64  `json:"turnoverRate"`
	MainNetInflow      float64  `json:"mainNetInflow"`
	MainNetInflowRatio float64  `json:"mainNetInflowRatio"`
	MainFlowSource     string   `json:"mainFlowSource"`
	TotalMarketCap     float64  `json:"totalMarketCap"`
	FloatMarketCap     float64  `json:"floatMarketCap"`
	CapBucket          string   `json:"capBucket"`
	Industry           string   `json:"industry"`
	Score              float64  `json:"score"`
	TriggerCount       int      `json:"triggerCount"`
	Triggers           []string `json:"triggers"`
	Reasons            []string `json:"reasons"`
	RiskFlags          []string `json:"riskFlags"`
	BuyPointHint       string   `json:"buyPointHint"`
	SellPointHint      string   `json:"sellPointHint"`
	StopLossHint       string   `json:"stopLossHint"`
	UpdatedAt          string   `json:"updatedAt"`
}
