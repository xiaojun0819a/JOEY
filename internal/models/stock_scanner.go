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
	// 当日涨幅上限(%)；nil 时用默认 +1.5。回测最优≈0（只买当天不涨的票）
	MaxChangePct *float64 `json:"maxChangePct,omitempty"`
	// 草元4A/4B是否启用严选二次筛选；默认 false=标准层级。
	CaoYuanStrict bool `json:"caoYuanStrict,omitempty"`
	// 历史时间选股：指定 YYYY-MM-DD 时，只使用该日及以前数据重算策略。
	HistoryPickDate string `json:"historyPickDate,omitempty"`
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

// HistoryBackfillRequest 历史回补请求：用日K逐只回补 stock_daily
type HistoryBackfillRequest struct {
	// 待回补的股票代码（如 sh600519、sz000001），为空时由上层填充
	Codes []string `json:"codes"`
	// 回补的交易日条数，<=0 时默认 250（约一年）
	Days int `json:"days"`
	// 逐只之间的间隔毫秒，<=0 时默认 150，用于防限流
	ThrottleMs int `json:"throttleMs,omitempty"`
}

// HistoryBackfillResult 历史回补结果
type HistoryBackfillResult struct {
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	DBPath       string `json:"dbPath"`
	TotalCodes   int    `json:"totalCodes"`
	OKCodes      int    `json:"okCodes"`
	FailedCodes  int    `json:"failedCodes"`
	SavedRows    int    `json:"savedRows"`
	EarliestDate string `json:"earliestDate"`
	LatestDate   string `json:"latestDate"`
	Status       string `json:"status"`
	Message      string `json:"message"`
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

// AccountHolding 策略账户当前持仓
type AccountHolding struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	EntryDate     string  `json:"entryDate"`
	EntryPrice    float64 `json:"entryPrice"`
	CurrentPrice  float64 `json:"currentPrice"`
	HoldDays      int     `json:"holdDays"`
	UnrealizedPct float64 `json:"unrealizedPct"` // 浮动盈亏%
	Value         float64 `json:"value"`         // 当前市值(元)
}

// AccountTrade 策略账户已平仓记录
type AccountTrade struct {
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	EntryDate  string  `json:"entryDate"`
	ExitDate   string  `json:"exitDate"`
	EntryPrice float64 `json:"entryPrice"`
	ExitPrice  float64 `json:"exitPrice"`
	HoldDays   int     `json:"holdDays"`
	ReturnPct  float64 `json:"returnPct"`
	ExitReason string  `json:"exitReason"`
}

// AccountEquityPoint 净值曲线点
type AccountEquityPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"` // 账户净值(元)
}

// AccountCandidate 某交易日由策略选出的候选(由 main 包评估器算出，喂给账户模拟器)
type AccountCandidate struct {
	Symbol string  `json:"symbol"`
	Score  float64 `json:"score"`
}

// FundamentalCandidate 基本面初筛候选(步骤①②输出)
type FundamentalCandidate struct {
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	MarketCapYi float64 `json:"marketCapYi"` // 总市值(亿)
	AnnROE      float64 `json:"annRoe"`      // 年化ROE %
	RevYoY      float64 `json:"revYoY"`      // 营收同比 %
	ProfitYoY   float64 `json:"profitYoY"`   // 净利同比 %
	GrossMargin float64 `json:"grossMargin"` // 毛利率 %
	CFPS        float64 `json:"cfps"`        // 每股经营现金流
	EPS         float64 `json:"eps"`
	AmountYi    float64 `json:"amountYi"` // 当日成交额(亿)
	DebtRatio     float64 `json:"debtRatio"`     // 资产负债率 %
	ValPctile     float64 `json:"valPctile"`     // 估值历史分位 %(近250日市值分位代理PE分位)
	GoodwillRatio float64 `json:"goodwillRatio"` // 商誉占净资产 %
	DividendYield float64 `json:"dividendYield"` // 股息率 %
	Score         float64 `json:"score"`
	Passed      []string `json:"passed"` // 命中的规则
}

// FundamentalScanResult 基本面初筛结果
type FundamentalScanResult struct {
	Preset        string                 `json:"preset"`     // value(长线价值) / boom(中线景气)
	PresetLabel   string                 `json:"presetLabel"`
	ReportDate    string                 `json:"reportDate"`
	UniverseCount int                    `json:"universeCount"`
	Candidates    []FundamentalCandidate `json:"candidates"`
	Warning       string                 `json:"warning"`
	RulesText     string                 `json:"rulesText"` // 规则说明
}

// FundamentalRollingPeriod 基本面滚动回测的单个持有期
type FundamentalRollingPeriod struct {
	Label        string   `json:"label"`
	ReportDate   string   `json:"reportDate"`   // 用哪期财报筛
	StartDate    string   `json:"startDate"`    // 建仓日(财报公布后)
	EndDate      string   `json:"endDate"`      // 持有到
	BasketCount  int      `json:"basketCount"`  // 入选只数
	BasketReturn float64  `json:"basketReturn"` // 等权篮子净收益 %
	MarketReturn float64  `json:"marketReturn"` // 同期等权全A %
	Alpha        float64  `json:"alpha"`        // 超额 %
	TopNames     []string `json:"topNames"`     // 篮子样例
}

// FundamentalRollingResult 基本面滚动逐年回测
type FundamentalRollingResult struct {
	Preset      string                     `json:"preset"`
	PresetLabel string                     `json:"presetLabel"`
	Periods     []FundamentalRollingPeriod `json:"periods"`
	Warning     string                     `json:"warning"`
}

// TailForwardCandidate 2:30 实盘向前验证的单个候选(含可成交判定)
type TailForwardCandidate struct {
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	Source      string  `json:"source"`     // 来源策略(多策略扫描时标注)
	SourceLabel string  `json:"sourceLabel"`
	Price       float64 `json:"price"`
	ChangePct   float64 `json:"changePct"`
	Score       float64 `json:"score"`
	Buyable     bool    `json:"buyable"`     // 当前是否能买进(非封死涨停)
	Reason      string  `json:"reason"`      // 可买/封死涨停·无卖盘 等
	AlreadyHeld bool    `json:"alreadyHeld"` // 模拟持仓里是否已有该票
	Added       bool    `json:"added"`       // 本次是否已自动记入
}

// TailForwardResult 2:30 向前验证结果
type TailForwardResult struct {
	AsOf         string                 `json:"asOf"`
	Strategy     string                 `json:"strategy"`
	Auto         bool                   `json:"auto"`         // 是否自动记入模式
	Candidates   []TailForwardCandidate `json:"candidates"`
	BuyableCount int                    `json:"buyableCount"` // 可买数
	SealedCount  int                    `json:"sealedCount"`  // 封死买不进数
	AddedCount   int                    `json:"addedCount"`   // 本次自动记入数
	Warning      string                 `json:"warning"`
}

// StrategyAccountResult 策略账户(固定本金·自动买卖)结果
type StrategyAccountResult struct {
	Strategy     string               `json:"strategy"` // lowbuy | taillazy
	Capital      float64              `json:"capital"`  // 起始本金
	StartDate    string               `json:"startDate"`
	EndDate      string               `json:"endDate"`
	FinalEquity  float64              `json:"finalEquity"`  // 期末净值(元)
	ReturnPct    float64              `json:"returnPct"`    // 总收益%
	MaxDrawdown  float64              `json:"maxDrawdown"`  // 最大回撤%
	Benchmark    float64              `json:"benchmark"`    // 同期等权全A%
	Excess       float64              `json:"excess"`       // 超额(收益-基准)%
	Cash         float64              `json:"cash"`         // 当前现金
	ClosedTrades int                  `json:"closedTrades"` // 已平仓笔数
	WinRate      float64              `json:"winRate"`
	Expectancy   float64              `json:"expectancy"`   // 期望值/笔%
	PayoffRatio  float64              `json:"payoffRatio"`
	ProfitFactor float64              `json:"profitFactor"`
	AvgHoldDays  float64              `json:"avgHoldDays"`
	Holdings     []AccountHolding     `json:"holdings"` // 当前持仓
	Trades       []AccountTrade       `json:"trades"`   // 平仓记录(最近在前)
	Equity       []AccountEquityPoint `json:"equity"`   // 净值曲线
	Warning      string               `json:"warning"`
}

// LowBuyBatchRow 低吸批量复盘 · 单分组(年份/合计)统计（机械止损止盈口径 + alpha）
type LowBuyBatchRow struct {
	Label        string  `json:"label"`
	Trades       int     `json:"trades"`
	WinRate      float64 `json:"winRate"`      // 胜率%
	Expectancy   float64 `json:"expectancy"`   // 期望值/笔%（扣成本净收益均值）
	PayoffRatio  float64 `json:"payoffRatio"`  // 赔率
	ProfitFactor float64 `json:"profitFactor"` // 盈利因子
	MaxLoss      float64 `json:"maxLoss"`      // 单笔最大亏损%
	Benchmark    float64 `json:"benchmark"`    // 同期等权全A/笔%
	Excess       float64 `json:"excess"`       // 超额alpha/笔%
	AvgHold      float64 `json:"avgHold"`      // 平均持有天
}

// LowBuyBatchResult 低吸批量复盘结果
type LowBuyBatchResult struct {
	Start   string           `json:"start"`
	End     string           `json:"end"`
	TopN    int              `json:"topN"`
	Rows    []LowBuyBatchRow `json:"rows"`
	Warning string           `json:"warning"`
}

// TailLazyBatchRow 尾盘懒人批量复盘 · 单分组(年份/合计)统计
type TailLazyBatchRow struct {
	Label        string  `json:"label"`        // 年份或"合计"
	Samples      int     `json:"samples"`      // 信号数
	Hit3Rate     float64 `json:"hit3Rate"`     // 次日最高≥3%命中%
	Hit5Rate     float64 `json:"hit5Rate"`     // 次日最高≥5%命中%
	AvgHigh      float64 `json:"avgHigh"`      // 次日最高均值%
	AvgOpen      float64 `json:"avgOpen"`      // 次日开盘均值%
	AvgClose     float64 `json:"avgClose"`     // 次日收盘均值%
	TpWinRate    float64 `json:"tpWinRate"`    // 止盈3%模型胜率%
	TpExpectancy float64 `json:"tpExpectancy"` // 止盈3%模型期望%/笔（扣成本）
}

// TailLazyBatchResult 尾盘懒人批量复盘结果
type TailLazyBatchResult struct {
	Start   string             `json:"start"`
	End     string             `json:"end"`
	Rows    []TailLazyBatchRow `json:"rows"` // 各年份 + 合计
	Warning string             `json:"warning"`
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
	MA10               float64  `json:"ma10"`       // 10日均线值
	MA10Status         string   `json:"ma10Status"` // hold=站上(未破) | broke=跌破 | ""=未知/数据不足
	// 历史复盘：次日表现（以当日收盘为买入价）
	NextDate         string  `json:"nextDate,omitempty"`         // 次日交易日
	NextOpenGainPct  float64 `json:"nextOpenGainPct,omitempty"`  // 次日开盘涨幅%
	NextHighGainPct  float64 `json:"nextHighGainPct,omitempty"`  // 次日最高涨幅%（理想止盈）
	NextCloseGainPct float64 `json:"nextCloseGainPct,omitempty"` // 次日收盘涨幅%
	// 低吸历史复盘：按机械纪律的持有结果（T+1开盘进、扣成本净收益）
	ReplayExitDate   string  `json:"replayExitDate,omitempty"`
	ReplayHoldDays   int     `json:"replayHoldDays,omitempty"`
	ReplayReturnPct  float64 `json:"replayReturnPct,omitempty"`  // 扣成本净收益%
	ReplayExitReason string  `json:"replayExitReason,omitempty"` // stop_loss/ma10/turnover/time_stop/take_profit/half_*/window_end
	UpdatedAt        string  `json:"updatedAt"`
}

// LateDayChaseScannerRequest 尾盘强势股扫描请求。
// 默认镜像“14:30 输入 60 打开涨幅榜”的手工流程：取涨幅榜前 60，
// 硬筛涨幅 3%~5%、量比、换手、流通市值，再验证日K/分时结构。
type LateDayChaseScannerRequest struct {
	Limit          int  `json:"limit"`
	RankLimit      int  `json:"rankLimit"`
	IncludeBeijing bool `json:"includeBeijing"`

	MinChangePct    float64 `json:"minChangePct"`
	MaxChangePct    float64 `json:"maxChangePct"`
	MinVolumeRatio  float64 `json:"minVolumeRatio"`
	MinTurnoverRate float64 `json:"minTurnoverRate"`
	MaxTurnoverRate float64 `json:"maxTurnoverRate"`
	MinFloatCap     float64 `json:"minFloatCap"`
	MaxFloatCap     float64 `json:"maxFloatCap"`

	// 开启后只返回“尾盘创新高后回踩均价线不破”的已触发标的；默认 false，只作为买点状态提示。
	RequireBuySignal bool `json:"requireBuySignal"`
}

// LateDayChaseScannerResult 尾盘强势股扫描结果。
type LateDayChaseScannerResult struct {
	AsOf           string                    `json:"asOf"`
	RuleVersion    string                    `json:"ruleVersion"`
	UniverseCount  int                       `json:"universeCount"`
	RankLimit      int                       `json:"rankLimit"`
	RankedCount    int                       `json:"rankedCount"`
	CandidateCount int                       `json:"candidateCount"`
	SelectedCount  int                       `json:"selectedCount"`
	Items          []LateDayChaseScannerItem `json:"items"`
	Warning        string                    `json:"warning,omitempty"`
}

// LateDayChaseScannerItem 尾盘强势股单条候选。
type LateDayChaseScannerItem struct {
	Symbol                 string   `json:"symbol"`
	Name                   string   `json:"name"`
	Rank                   int      `json:"rank"`
	Price                  float64  `json:"price"`
	ChangePercent          float64  `json:"changePercent"`
	VolumeRatio            float64  `json:"volumeRatio"`
	TurnoverRate           float64  `json:"turnoverRate"`
	Amount                 float64  `json:"amount"`
	TotalMarketCap         float64  `json:"totalMarketCap"`
	FloatMarketCap         float64  `json:"floatMarketCap"`
	Industry               string   `json:"industry"`
	Score                  float64  `json:"score"`
	VolumeStepPassed       bool     `json:"volumeStepPassed"`
	MABullishPassed        bool     `json:"maBullishPassed"`
	IntradayStrengthPassed bool     `json:"intradayStrengthPassed"`
	BuySignalReady         bool     `json:"buySignalReady"`
	IntradayAboveAvgRatio  float64  `json:"intradayAboveAvgRatio"`
	StockIntradayReturn    float64  `json:"stockIntradayReturn"`
	IndexIntradayReturn    float64  `json:"indexIntradayReturn"`
	MA5                    float64  `json:"ma5"`
	MA10                   float64  `json:"ma10"`
	MA20                   float64  `json:"ma20"`
	LastHighTime           string   `json:"lastHighTime"`
	Triggers               []string `json:"triggers"`
	Reasons                []string `json:"reasons"`
	RiskFlags              []string `json:"riskFlags"`
	BuyPointHint           string   `json:"buyPointHint"`
	StopLossHint           string   `json:"stopLossHint"`
	UpdatedAt              string   `json:"updatedAt"`
}
