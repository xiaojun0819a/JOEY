package models

// 选股体感练习的数据模型。DrillPick 只装信号日证据(评分/涨幅/换手/主力/触发信号/逻辑/风险),
// 刻意不含任何次日字段——练习靠盲选,后端不发未来。

type DrillPick struct {
	Symbol      string   `json:"symbol"`
	Name        string   `json:"name"`
	Industry    string   `json:"industry"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
	Price       float64  `json:"price"`       // 信号日 14:50 入选价(练习的成本价)
	SignalClose float64  `json:"signalClose"` // 信号日收盘 = 次日分时的「昨收」基准(回补源没带 pct,得靠它现算)
	ChangePct   float64  `json:"changePct"`   // 信号日涨幅%
	Turnover    float64  `json:"turnover"`    // 信号日换手%
	MainPct     float64  `json:"mainPct"`     // 主力净占比%
	MainNet     float64  `json:"mainNet"`     // 主力净额(元)
	Triggers    []string `json:"triggers"`
	Reasons     []string `json:"reasons"`
	Risks       []string `json:"risks"`
}

type DrillSession struct {
	StrategyID     string      `json:"strategyId"`
	StrategyName   string      `json:"strategyName"`
	SignalDate     string      `json:"signalDate"`
	NextDate       string      `json:"nextDate"` // 次日(分时磁带的日期),前端据此取回放数据
	Picks          []DrillPick `json:"picks"`
	AvailableDates []string    `json:"availableDates"`
	Warning        string      `json:"warning"`
}
