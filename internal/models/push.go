package models

// 推送信号类型
const (
	PushTypeBuyPoint   = "buy_point"   // 买点信号
	PushTypeStopLoss   = "stop_loss"   // 止损预警
	PushTypeTakeProfit = "take_profit" // 止盈提醒
	PushTypeTimeStop   = "time_stop"   // 时间止损
	PushTypeEnvChange  = "env_change"  // 大盘环境变化
)

// PushSignal 一条待推送的信号
type PushSignal struct {
	StockCode string `json:"stockCode"`
	StockName string `json:"stockName"`
	Type      string `json:"type"`            // 见 PushType* 常量
	Message   string `json:"message"`         // 正文
	Level     string `json:"level,omitempty"` // Bark 级别：active/timeSensitive/passive，默认 active
}

// PushResult 推送结果
type PushResult struct {
	Sent     bool              `json:"sent"`     // 是否实际发送
	Skipped  bool              `json:"skipped"`  // 是否因防重被跳过
	Channels map[string]string `json:"channels"` // 各渠道结果：渠道名 -> "ok" 或错误信息
	Message  string            `json:"message"`
}
