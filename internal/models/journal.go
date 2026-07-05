package models

// TradeJournalEntry 一笔交易记录（持仓中或已平仓）
type TradeJournalEntry struct {
	ID           int64              `json:"id"`
	StockCode    string             `json:"stockCode"`
	StockName    string             `json:"stockName"`
	BuyDate      string             `json:"buyDate"`                // YYYY-MM-DD
	BuyPrice     float64            `json:"buyPrice"`               // 买入价
	Shares       int64              `json:"shares"`                 // 数量(股)
	SellDate     string             `json:"sellDate"`               // 空=持仓中
	SellPrice    float64            `json:"sellPrice"`              // 卖出价
	CurrentPrice float64            `json:"currentPrice,omitempty"` // 持仓中实时价/已平仓卖价
	Status       string             `json:"status"`                 // open(持仓) / closed(已平)
	PnL          float64            `json:"pnl"`                    // 盈亏额(元)
	PnLPct       float64            `json:"pnlPct"`                 // 盈亏(%)
	HoldDays     int                `json:"holdDays"`               // 持仓自然日
	Source       string             `json:"source"`                 // lowbuy(低吸) / wave(龙头) / manual(手动)
	Note         string             `json:"note"`
	Actions      []TradeActionEntry `json:"actions,omitempty"` // 建仓/加仓/减仓/平仓流水
	CreatedAt    string             `json:"createdAt"`
	UpdatedAt    string             `json:"updatedAt"`
}

// TradeActionEntry 单次买卖流水，用于追溯建仓/加仓/平仓历史。
type TradeActionEntry struct {
	ID          int64   `json:"id"`
	TradeID     int64   `json:"tradeId"`
	StockCode   string  `json:"stockCode"`
	StockName   string  `json:"stockName"`
	Action      string  `json:"action"` // build/add/reduce/close
	TradeDate   string  `json:"tradeDate"`
	Price       float64 `json:"price"`
	Shares      int64   `json:"shares"`
	Amount      float64 `json:"amount"`
	AfterShares int64   `json:"afterShares"`
	AfterCost   float64 `json:"afterCost"`
	Note        string  `json:"note"`
	CreatedAt   string  `json:"createdAt"`
}

// TradeJournalRequest 手动新增/修改一笔
type TradeJournalRequest struct {
	ID        int64   `json:"id"`     // 0=新增，>0=修改
	Action    string  `json:"action"` // build/add/reduce/close/manual；空=兼容旧的整笔录入
	StockCode string  `json:"stockCode"`
	StockName string  `json:"stockName"`
	BuyDate   string  `json:"buyDate"`
	BuyPrice  float64 `json:"buyPrice"`
	Shares    int64   `json:"shares"`
	SellDate  string  `json:"sellDate"`
	SellPrice float64 `json:"sellPrice"`
	Source    string  `json:"source"`
	Note      string  `json:"note"`
}

// TradePeriodStat 某时间段(日/周/月)的汇总
type TradePeriodStat struct {
	Period    string  `json:"period"`    // 标签，如 2026-06-03 / 2026-W23 / 2026-06
	Trades    int     `json:"trades"`    // 平仓笔数
	Wins      int     `json:"wins"`      // 盈利笔数
	WinRate   float64 `json:"winRate"`   // 胜率%
	TotalPnL  float64 `json:"totalPnl"`  // 盈亏额合计
	AvgPnLPct float64 `json:"avgPnlPct"` // 平均盈亏%
}

// TradeJournalSummary 总体汇总
type TradeJournalSummary struct {
	OpenCount    int     `json:"openCount"`    // 持仓中
	ClosedCount  int     `json:"closedCount"`  // 已平仓
	Wins         int     `json:"wins"`         // 盈利笔数
	WinRate      float64 `json:"winRate"`      // 胜率%
	TotalPnL     float64 `json:"totalPnl"`     // 总盈亏额
	AvgPnLPct    float64 `json:"avgPnlPct"`    // 平均盈亏%
	AvgWinPct    float64 `json:"avgWinPct"`    // 盈利单均%
	AvgLossPct   float64 `json:"avgLossPct"`   // 亏损单均%
	ProfitFactor float64 `json:"profitFactor"` // 盈亏比
	AvgHoldDays  float64 `json:"avgHoldDays"`
}

// TradeJournalStats 台账统计（总体 + 日/周/月）
type TradeJournalStats struct {
	Summary TradeJournalSummary `json:"summary"`
	ByDay   []TradePeriodStat   `json:"byDay"`
	ByWeek  []TradePeriodStat   `json:"byWeek"`
	ByMonth []TradePeriodStat   `json:"byMonth"`
}
