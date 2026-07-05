package models

// StrategyPickSnapshot is the normalized record written after any stock picker emits candidates.
type StrategyPickSnapshot struct {
	StrategyID       string   `json:"strategyId"`
	StrategyName     string   `json:"strategyName"`
	SignalDate       string   `json:"signalDate"`
	ScannedAt        string   `json:"scannedAt"`
	Symbol           string   `json:"symbol"`
	Name             string   `json:"name"`
	Rank             int      `json:"rank"`
	Price            float64  `json:"price"`
	ChangePercent    float64  `json:"changePercent"`
	Score            float64  `json:"score"`
	Industry         string   `json:"industry"`
	Amount           float64  `json:"amount"`
	TurnoverRate     float64  `json:"turnoverRate"`
	MainNetInflow    float64  `json:"mainNetInflow"`
	MainNetInflowPct float64  `json:"mainNetInflowPct"`
	MainFlowSource   string   `json:"mainFlowSource"`
	Triggers         []string `json:"triggers"`
	Reasons          []string `json:"reasons"`
	RiskFlags        []string `json:"riskFlags"`
}

type StrategyReviewRequest struct {
	StrategyID    string   `json:"strategyId"`
	StrategyName  string   `json:"strategyName,omitempty"`
	SignalDate    string   `json:"signalDate,omitempty"`
	ReviewDate    string   `json:"reviewDate,omitempty"`
	ReviewSymbols []string `json:"reviewSymbols,omitempty"`
}

type StrategyReviewNews struct {
	Time    string `json:"time"`
	Content string `json:"content"`
	URL     string `json:"url,omitempty"`
}

type StrategyReviewMarket struct {
	ReviewDate      string  `json:"reviewDate"`
	ShPrice         float64 `json:"shPrice"`
	ShChangePercent float64 `json:"shChangePercent"`
	LimitUpCount    int     `json:"limitUpCount"`
	LimitDownCount  int     `json:"limitDownCount"`
	TotalAmount     float64 `json:"totalAmount"`
	Summary         string  `json:"summary"`
}

type StrategyReviewItem struct {
	Symbol              string               `json:"symbol"`
	Name                string               `json:"name"`
	Rank                int                  `json:"rank"`
	Industry            string               `json:"industry"`
	BusinessSummary     string               `json:"businessSummary,omitempty"`
	BusinessSource      string               `json:"businessSource,omitempty"`
	SignalPrice         float64              `json:"signalPrice"`
	SignalChangePercent float64              `json:"signalChangePercent"`
	SignalScore         float64              `json:"signalScore"`
	SignalReasons       []string             `json:"signalReasons"`
	SignalTriggers      []string             `json:"signalTriggers"`
	SignalRisks         []string             `json:"signalRisks"`
	ReviewDate          string               `json:"reviewDate"`
	Open                float64              `json:"open"`
	High                float64              `json:"high"`
	Low                 float64              `json:"low"`
	Close               float64              `json:"close"`
	DayChangePercent    float64              `json:"dayChangePercent"`
	CloseReturnPercent  float64              `json:"closeReturnPercent"`
	HighReturnPercent   float64              `json:"highReturnPercent"`
	TurnoverRate        float64              `json:"turnoverRate"`
	Amount              float64              `json:"amount"`
	MainNetInflow       float64              `json:"mainNetInflow"`
	MainNetInflowPct    float64              `json:"mainNetInflowPct"`
	MainFlowSource      string               `json:"mainFlowSource"`
	KLineSummary        string               `json:"klineSummary"`
	FundSummary         string               `json:"fundSummary"`
	Outcome             string               `json:"outcome"`
	Suggestions         []string             `json:"suggestions"`
	News                []StrategyReviewNews `json:"news"`
}

type StrategyReviewResult struct {
	StrategyID      string               `json:"strategyId"`
	StrategyName    string               `json:"strategyName"`
	SignalDate      string               `json:"signalDate"`
	ReviewDate      string               `json:"reviewDate"`
	GeneratedAt     string               `json:"generatedAt"`
	PickCount       int                  `json:"pickCount"`
	ReviewedCount   int                  `json:"reviewedCount"`
	WinRate         float64              `json:"winRate"`
	AvgCloseReturn  float64              `json:"avgCloseReturn"`
	AvgHighReturn   float64              `json:"avgHighReturn"`
	Hit3Rate        float64              `json:"hit3Rate"`
	Market          StrategyReviewMarket `json:"market"`
	News            []StrategyReviewNews `json:"news"`
	Items           []StrategyReviewItem `json:"items"`
	Optimization    []string             `json:"optimization"`
	Warning         string               `json:"warning,omitempty"`
	DataSourceNotes []string             `json:"dataSourceNotes,omitempty"`
}
