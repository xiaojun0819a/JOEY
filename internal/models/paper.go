package models

// PaperPosition 模拟持仓（纸上交易，用于验证各筛选系统胜率）
type PaperPosition struct {
	ID         int64   `json:"id"`
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	Source     string  `json:"source"` // lowbuy/wave/latechase/manual
	CostPrice  float64 `json:"costPrice"`
	Shares     int64   `json:"shares"`
	OpenDate   string  `json:"openDate"`
	OpenPrice  float64 `json:"openPrice"`  // 加入时现价（基准参考）
	Status     string  `json:"status"`     // open/closed
	ClosePrice float64 `json:"closePrice"` // 平仓价
	CloseDate  string  `json:"closeDate"`
	ExitReason string  `json:"exitReason"` // 自动平仓原因(stop_loss/ma10/time_stop/half_*等)，手动平仓为空
	// 实时字段（由前端按现价填充，不入库）
	CurrentPrice  float64 `json:"currentPrice,omitempty"`
	ProfitPct     float64 `json:"profitPct,omitempty"`
	ProfitAmount  float64 `json:"profitAmount,omitempty"`
	// 风控线（运行时计算，不入库）
	RiskKind  string  `json:"riskKind,omitempty"`  // 风控口径：短线/价值
	StopPrice float64 `json:"stopPrice,omitempty"` // 止损价(成本硬止损)
	TpPrice   float64 `json:"tpPrice,omitempty"`   // 止盈封顶价
}

// RiskConcentration 集中度项
type RiskConcentration struct {
	Name string  `json:"name"`
	Pct  float64 `json:"pct"`
}

// PaperRiskSummary 组合风控概览（仓位集中度 + 回撤预警）
type PaperRiskSummary struct {
	PositionCount    int                 `json:"positionCount"`
	TotalCost        float64             `json:"totalCost"`
	TotalValue       float64             `json:"totalValue"`
	ProfitPct        float64             `json:"profitPct"`        // 组合浮盈率(相对总成本)
	SingleCap        float64             `json:"singleCap"`        // 单票上限%
	SectorCap        float64             `json:"sectorCap"`        // 板块上限%
	DrawdownAlertPct float64             `json:"drawdownAlertPct"` // 回撤预警线%
	MaxSinglePct     float64             `json:"maxSinglePct"`
	SingleOver       []RiskConcentration `json:"singleOver"` // 单票超限(>上限)
	SectorTop        []RiskConcentration `json:"sectorTop"`  // 板块占比Top
	SectorOver       []RiskConcentration `json:"sectorOver"` // 板块超限
	PeakValue        float64             `json:"peakValue"`
	DrawdownFromPeak float64             `json:"drawdownFromPeak"` // 从净值峰值回撤%
	DrawdownAlert    bool                `json:"drawdownAlert"`
	Warnings         []string            `json:"warnings"`
}

// PaperSourceStat 按筛选系统分组的胜率统计（扣成本净收益口径，与回测一致）
type PaperSourceStat struct {
	Source      string  `json:"source"`
	Total       int     `json:"total"`       // 该来源总笔数
	Closed      int     `json:"closed"`      // 已平仓笔数
	Win         int     `json:"win"`         // 盈利笔数（已平仓，净收益>0）
	WinRate     float64 `json:"winRate"`     // 胜率 %（已平仓口径）
	AvgReturn   float64 `json:"avgReturn"`   // 期望值/笔 %（已平仓净收益均值）
	TotalReturn float64 `json:"totalReturn"` // 累计收益 %（已平仓逐笔相加）
	AvgWin      float64 `json:"avgWin"`      // 盈利单均值 %
	AvgLoss     float64 `json:"avgLoss"`     // 亏损单均值 %
	PayoffRatio float64 `json:"payoffRatio"` // 赔率 = AvgWin/|AvgLoss|
	ProfitFactor float64 `json:"profitFactor"` // 总盈利/总亏损
	MaxLoss     float64 `json:"maxLoss"`     // 单笔最大亏损 %
}

// PaperStats 模拟持仓总览（计分卡口径）
type PaperStats struct {
	OpenCount    int               `json:"openCount"`
	ClosedCount  int               `json:"closedCount"`
	WinRate      float64           `json:"winRate"`      // 总胜率 %（已平仓）
	Expectancy   float64           `json:"expectancy"`   // 期望值/笔 %（已平仓净收益均值）
	PayoffRatio  float64           `json:"payoffRatio"`  // 总赔率
	ProfitFactor float64           `json:"profitFactor"` // 总盈利因子
	MaxLoss      float64           `json:"maxLoss"`      // 单笔最大亏损 %
	BySource     []PaperSourceStat `json:"bySource"`
}
