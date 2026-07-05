package models

// WaveCandidate 波段选股候选
type WaveCandidate struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Price             float64  `json:"price"`
	Kongpan           float64  `json:"kongpan"` // 控盘度
	Ignite            bool     `json:"ignite"`  // 资金点火/异动
	Date              string   `json:"date"`
	Score             float64  `json:"score"`             // 波段策略1.0评分
	Level             string   `json:"level"`             // 起爆/鱼身/主升/超强
	Phase             string   `json:"phase"`             // 当前阶段说明
	EatFish           bool     `json:"eatFish"`           // 吃鱼身
	RelaxedIgnite     bool     `json:"relaxedIgnite"`     // 异动现主力进(宽口径)
	StrictIgnite      bool     `json:"strictIgnite"`      // 异动起爆(强口径)
	RecentIgnite      bool     `json:"recentIgnite"`      // 近10日出现点火
	MainOpenFish      bool     `json:"mainOpenFish"`      // 主图开仓吃鱼
	TimelyTakeProfit  bool     `json:"timelyTakeProfit"`  // 主图及时止盈
	BreakTakeProfit   bool     `json:"breakTakeProfit"`   // 放量跌破5/10日线强制止盈
	StrongSignal      bool     `json:"strongSignal"`      // 转强/主升/超强当天
	StrongCount       int      `json:"strongCount"`       // 强信号计数
	MainRise          bool     `json:"mainRise"`          // 主升行情
	MainControlStart  bool     `json:"mainControlStart"`  // 主力拉升
	MainControlReduce bool     `json:"mainControlReduce"` // 减仓信号
	BuyState          bool     `json:"buyState"`          // 买/卖状态中的买
	TrendBull         bool     `json:"trendBull"`         // 趋势红
	EnergyBull        bool     `json:"energyBull"`        // 量能红
	MidBull           bool     `json:"midBull"`           // 中期红
	ShortBull         bool     `json:"shortBull"`         // 短期红
	GZ                bool     `json:"gz"`                // 五灯共振
	Reasons           []string `json:"reasons"`           // 入选理由
	Risks             []string `json:"risks"`             // 风险/减仓提示
}

// WaveScanResult 波段扫描结果
type WaveScanResult struct {
	AsOf          string          `json:"asOf"`
	SnapshotAsOf  string          `json:"snapshotAsOf"`
	DataSource    string          `json:"dataSource"`
	UniverseCount int             `json:"universeCount"`
	ScannedCount  int             `json:"scannedCount"`
	PreheatDays   int             `json:"preheatDays"`
	PatchedCount  int             `json:"patchedCount"`
	RecentKCount  int             `json:"recentKCount"`
	GatePassed    bool            `json:"gatePassed"`
	GateBypassed  bool            `json:"gateBypassed"`
	Count         int             `json:"count"`
	Items         []WaveCandidate `json:"items"`
	Message       string          `json:"message"`
}

// BacktestRequest 回测请求
type BacktestRequest struct {
	Days          int      `json:"days"`                   // 回测交易日数，<=0 默认 250
	TopN          int      `json:"topN"`                   // 每个信号日取前 N 只，<=0 默认 5
	EntryRule     string   `json:"entryRule"`              // next_open(次日开盘) | close(当日收盘)，默认 next_open
	MaxMarketCap  float64  `json:"maxMarketCap"`           // 总市值上限(元)，>0 时只选小于此值的票
	SellRule      string   `json:"sellRule"`               // fast(快砍:-5%/破MA10) | patient(耐心:-8%/破MA20)，默认 fast
	MaxPositions  int      `json:"maxPositions"`           // 组合模拟最多同时持仓数，<=0 默认 5
	TakeProfitPct float64  `json:"takeProfitPct"`          // 止盈线(%)，<=0 用默认 15
	StopLossPct   float64  `json:"stopLossPct"`            // 止损线(%，负数)，>=0 用默认 -5
	CostPct       float64  `json:"costPct"`                // 单笔往返成本(%，佣金+印花税+滑点)，0=不计
	GateMode      string   `json:"gateMode"`               // 大盘闸门: ""=不加 | "smart"=小盘breadth+收紧阈值
	Universe      string   `json:"universe"`               // 吃鱼身池子: ""=300/688 | "all"=全A
	Engine        string   `json:"engine"`                 // 引擎: ""=通达信吃鱼身 | "driver"=驾驶舱模型
	MaxChangePct  *float64 `json:"maxChangePct,omitempty"` // 低吸当日涨幅上限%（与扫描器一致），nil=用默认
}

// BacktestTrade 单笔模拟交易
type BacktestTrade struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	SignalDate string  `json:"signalDate"` // 选出日
	EntryDate  string  `json:"entryDate"`
	EntryPrice float64 `json:"entryPrice"`
	ExitDate   string  `json:"exitDate"`
	ExitPrice  float64 `json:"exitPrice"`
	HoldDays   int     `json:"holdDays"`
	ReturnPct  float64 `json:"returnPct"`
	ExitReason string  `json:"exitReason"` // stop_loss/take_profit/turnover/ma10/time_stop/window_end
	Score      float64 `json:"score"`
	Source     string  `json:"source,omitempty"` // lowbuy/wave
}

// BacktestResult 回测汇总
type BacktestResult struct {
	StartDate    string          `json:"startDate"`
	EndDate      string          `json:"endDate"`
	TradingDays  int             `json:"tradingDays"`
	TotalTrades  int             `json:"totalTrades"`
	WinTrades    int             `json:"winTrades"`
	WinRate      float64         `json:"winRate"`      // 胜率 %
	AvgReturn    float64         `json:"avgReturn"`    // 每笔平均收益 %
	AvgWin       float64         `json:"avgWin"`       // 盈利单平均 %
	AvgLoss      float64         `json:"avgLoss"`      // 亏损单平均 %
	ProfitFactor float64         `json:"profitFactor"` // 总盈利/总亏损
	PayoffRatio  float64         `json:"payoffRatio"`  // 赔率 = 盈利单均值/|亏损单均值|
	MaxLossPct   float64         `json:"maxLossPct"`   // 单笔最大亏损 %
	BenchmarkPct float64         `json:"benchmarkPct"` // 同期等权全A每笔平均收益 %（基准）
	ExcessPct    float64         `json:"excessPct"`    // 超额收益/笔 = 期望值 − 基准 %
	MaxDrawdown  float64         `json:"maxDrawdown"`  // 等权逐笔权益最大回撤 %
	AvgHoldDays  float64         `json:"avgHoldDays"`
	TotalReturn  float64         `json:"totalReturn"` // 等权逐笔累计收益 %
	ByReason     map[string]int  `json:"byReason"`    // 离场原因分布
	Trades       []BacktestTrade `json:"trades"`      // 明细（按收益排序，截断）
	Status       string          `json:"status"`
	Message      string          `json:"message"`
}
