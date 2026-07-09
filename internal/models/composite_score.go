package models

// 综合评分选股:一票否决做门、三层评分做排序、状态门管执行。
// 权重只允许被前向验证做减法,不允许调参优化。

// CompositeScoreRow 单股评分卡
type CompositeScoreRow struct {
	Symbol        string   `json:"symbol"`
	Name          string   `json:"name"`
	Price         float64  `json:"price"`
	Quality       float64  `json:"quality"`   // 质量分 0-50(基本面)
	Structure     float64  `json:"structure"` // 结构分 0-30(诚实面板事实)
	Catalyst      float64  `json:"catalyst"`  // 催化分 0-20(涨停/龙虎榜/竞价)
	Total         float64  `json:"total"`
	QualityFacts  []string `json:"qualityFacts"`
	StructFacts   []string `json:"structFacts"`
	CatalystFacts []string `json:"catalystFacts"`
	GateOK        bool     `json:"gateOk"`      // 状态门:当日可执行
	GateReasons   []string `json:"gateReasons"` // 不可执行原因(破前低/破季线未修复)
	Vetoed        bool     `json:"vetoed"`
	VetoReasons   []string `json:"vetoReasons"`
	AnnROE        float64  `json:"annRoe"`
	MarketCapYi   float64  `json:"marketCapYi"`
}

// CompositeScoreResult 一次综合评分的输出
type CompositeScoreResult struct {
	RunDate       string              `json:"runDate"`
	Preset        string              `json:"preset"`
	PresetLabel   string              `json:"presetLabel"`
	UniverseCount int                 `json:"universeCount"` // 基本面初筛通过数
	Rows          []CompositeScoreRow `json:"rows"`          // 未否决,按总分降序
	VetoedRows    []CompositeScoreRow `json:"vetoedRows"`    // 基本面已过但被技术否决
	Warning       string              `json:"warning"`
	RulesText     string              `json:"rulesText"`
	SnapshotSaved bool                `json:"snapshotSaved"`
}

// CompositeValidationRow 快照的前向验证(Top10 等权,扣双边成本,对比全市场等权基准)
type CompositeValidationRow struct {
	RunDate     string  `json:"runDate"`
	HorizonDays int     `json:"horizonDays"` // 30 / 60 交易日
	N           int     `json:"n"`           // 参与股数
	PortRet     float64 `json:"portRet"`     // 组合收益 %(已扣成本)
	BenchRet    float64 `json:"benchRet"`    // 全市场等权基准 %
	Excess      float64 `json:"excess"`      // 超额 %
	Matured     bool    `json:"matured"`
	DaysElapsed int     `json:"daysElapsed"` // 已过交易日数
}

// CompositeValidationResult 验证报告
type CompositeValidationResult struct {
	Rows     []CompositeValidationRow `json:"rows"`
	CostNote string                   `json:"costNote"`
	Warning  string                   `json:"warning"`
}
