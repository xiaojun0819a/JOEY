package models

// AIProvider AI服务提供商类型
type AIProvider string

const (
	AIProviderOpenAI    AIProvider = "openai"
	AIProviderGemini    AIProvider = "gemini"
	AIProviderVertexAI  AIProvider = "vertexai"
	AIProviderAnthropic AIProvider = "anthropic"
)

type OpenAITokenParamMode string

const (
	OpenAITokenParamAuto                OpenAITokenParamMode = "auto"
	OpenAITokenParamMaxTokens           OpenAITokenParamMode = "max_tokens"
	OpenAITokenParamMaxCompletionTokens OpenAITokenParamMode = "max_completion_tokens"
)

// AgentSelectionStyle 小韭菜选人风格
type AgentSelectionStyle string

const (
	AgentSelectionBalanced     AgentSelectionStyle = "balanced"
	AgentSelectionConservative AgentSelectionStyle = "conservative"
	AgentSelectionAggressive   AgentSelectionStyle = "aggressive"
)

// AIConfig AI服务配置
type AIConfig struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Provider       AIProvider           `json:"provider"`
	BaseURL        string               `json:"baseUrl"`
	APIKey         string               `json:"apiKey"`
	ModelName      string               `json:"modelName"`
	MaxTokens      int                  `json:"maxTokens"`
	TokenParamMode OpenAITokenParamMode `json:"tokenParamMode"`
	Temperature    float64              `json:"temperature"`
	Timeout        int                  `json:"timeout"`
	IsDefault      bool                 `json:"isDefault"`
	// OpenAI Responses API 开关
	UseResponses bool `json:"useResponses"`
	// 不支持 system role（自动检测，用户不可见）
	NoSystemRole bool `json:"noSystemRole"`
	// Vertex AI 专用字段
	Project         string `json:"project"`
	Location        string `json:"location"`
	CredentialsJSON string `json:"credentialsJson"`
}

// MCPTransportType MCP传输类型
type MCPTransportType string

const (
	MCPTransportHTTP    MCPTransportType = "http"    // StreamableHTTP 传输
	MCPTransportSSE     MCPTransportType = "sse"     // SSE 传输（已废弃）
	MCPTransportCommand MCPTransportType = "command" // 命令行传输
)

// MCPServerConfig MCP服务器配置
type MCPServerConfig struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	TransportType MCPTransportType `json:"transportType"`
	Endpoint      string           `json:"endpoint"`   // HTTP/SSE 端点 URL
	Command       string           `json:"command"`    // 命令行传输的命令
	Args          []string         `json:"args"`       // 命令行参数
	ToolFilter    []string         `json:"toolFilter"` // 工具过滤列表（空则全部）
	Enabled       bool             `json:"enabled"`    // 是否启用
}

// AppConfig 应用配置
type AppConfig struct {
	Theme               string              `json:"theme"`           // 主题色: military, ocean, purple, orange, dark
	CandleColorMode     string              `json:"candleColorMode"` // 涨跌颜色模式: red-up(红涨绿跌) / green-up(绿涨红跌)
	AIConfigs           []AIConfig          `json:"aiConfigs"`
	DefaultAIID         string              `json:"defaultAiId"`
	StrategyAIID        string              `json:"strategyAiId"`  // 策略生成用AI
	ModeratorAIID       string              `json:"moderatorAiId"` // 意图分析(小韭菜)用AI
	AIRetryCount        int                 `json:"aiRetryCount"`
	VerboseAgentIO      bool                `json:"verboseAgentIO"`
	AgentSelectionStyle AgentSelectionStyle `json:"agentSelectionStyle"`
	EnableSecondReview  bool                `json:"enableSecondReview"`
	MCPServers          []MCPServerConfig   `json:"mcpServers"` // MCP服务器配置列表
	Memory              MemoryConfig        `json:"memory"`     // 记忆管理配置
	Proxy               ProxyConfig         `json:"proxy"`      // 代理配置
	Layout              LayoutConfig        `json:"layout"`     // 界面布局配置
	OpenClaw            OpenClawConfig      `json:"openClaw"`   // OpenClaw 服务配置
	Indicators          IndicatorConfig     `json:"indicators"` // 技术指标配置
	History             HistoryConfig       `json:"history"`     // 历史数据采集配置
	Push                PushConfig          `json:"push"`        // 信号推送配置
	TailForward         TailForwardConfig   `json:"tailForward"`      // 2:30 实盘向前验证配置
	RemoteBackendURL       string           `json:"remoteBackendUrl"`       // 远程后端(NAS)内网地址,如 http://192.168.1.4:8810。非空且可达则桌面进"瘦身模式",前端路由到 NAS;空=本地全量
	RemoteBackendPublicURL string           `json:"remoteBackendPublicUrl"` // 公网地址(Cloudflare 隧道,如 https://jcp.junai.uk)。内网探测失败时尝试,在外也能连 NAS
	RemoteBackendToken     string           `json:"remoteBackendToken"`     // 访问令牌,与 NAS 的 JCP_TOKEN 一致;公网暴露必须设置
	RemoteUsers            []RemoteUser     `json:"remoteUsers"`            // 访客账号(分发给他人的 app 用账号密码登录,权限受限)
	RegisterInviteCode     string           `json:"registerInviteCode"`     // 自助注册邀请码;空=开放注册,非空则 Register 必须携带一致的邀请码
}

// RemoteUser 远程访客账号(密码存 SHA256 十六进制)
type RemoteUser struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Trusted      bool   `json:"trusted"` // 信任账号:免资源类防线(重采集/未来的AI配额),但安全类限制(配置/密钥/账号管理)不变
}

// TailForwardConfig 2:30 实盘向前验证（尾盘买点闭环）配置
type TailForwardConfig struct {
	Enabled bool `json:"enabled"` // 2:30 定时扫描总开关
	Auto    bool `json:"auto"`    // true=自动记入模拟持仓；false=仅出候选清单待确认
}

// PushConfig 信号推送配置（支持 Bark / Telegram / 飞书 / 企业微信）
type PushConfig struct {
	Enabled      bool            `json:"enabled"`      // 总开关
	DedupHours   int             `json:"dedupHours"`   // 同股同信号防重小时数，<=0 默认 24
	PushProxyURL string          `json:"pushProxyUrl"` // 国外渠道(Telegram/Bark)专用代理，留空则用全局代理
	Bark         BarkChannel     `json:"bark"`
	Telegram     TelegramChannel `json:"telegram"`
	Feishu       WebhookChannel  `json:"feishu"`
	WeWork       WebhookChannel  `json:"weWork"`
	Monitor      MonitorConfig   `json:"monitor"` // 盘中持仓监控
}

// MonitorConfig 盘中信号监控配置（持仓盯盘 + 时间止损）
type MonitorConfig struct {
	Enabled          bool `json:"enabled"`          // 总开关
	IntervalMinutes  int  `json:"intervalMinutes"`  // 盘中检查间隔(分钟)，<=0 默认 30
	AfterMarketCheck bool `json:"afterMarketCheck"` // 16:00 盘后时间止损检查
}

// BarkChannel Bark 推送渠道
type BarkChannel struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"` // 完整地址含 key，如 https://api.day.app/XXXXXX（也支持自建服务）
}

// TelegramChannel Telegram Bot 推送渠道
type TelegramChannel struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"botToken"` // BotFather 给的 token
	ChatID   string `json:"chatId"`   // 目标 chat id
}

// WebhookChannel 通用 Webhook 渠道（飞书自定义机器人 / 企业微信群机器人）
type WebhookChannel struct {
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook"` // 机器人 Webhook 地址
}

// ProxyMode 代理模式
type ProxyMode string

const (
	ProxyModeNone   ProxyMode = "none"   // 无代理，直连
	ProxyModeSystem ProxyMode = "system" // 使用系统代理
	ProxyModeCustom ProxyMode = "custom" // 自定义代理
)

// ProxyConfig 代理配置
type ProxyConfig struct {
	Mode      ProxyMode `json:"mode"`
	CustomURL string    `json:"customUrl"` // 自定义代理地址
}

// MemoryConfig 记忆管理配置
type MemoryConfig struct {
	Enabled           bool   `json:"enabled"`           // 是否启用记忆管理
	AIConfigID        string `json:"aiConfigId"`        // 使用的 LLM 配置 ID（空则使用默认）
	MaxRecentRounds   int    `json:"maxRecentRounds"`   // 保留最近几轮讨论
	MaxKeyFacts       int    `json:"maxKeyFacts"`       // 最大关键事实数
	MaxSummaryLength  int    `json:"maxSummaryLength"`  // 摘要最大字数
	CompressThreshold int    `json:"compressThreshold"` // 触发压缩的轮次数
}

// LayoutConfig 界面布局配置
type LayoutConfig struct {
	LeftPanelWidth    int `json:"leftPanelWidth"`    // 左侧面板宽度(px)
	RightPanelWidth   int `json:"rightPanelWidth"`   // 右侧面板宽度(px)
	BottomPanelHeight int `json:"bottomPanelHeight"` // 底部面板高度(px)
	WindowWidth       int `json:"windowWidth"`       // 窗口宽度(px)
	WindowHeight      int `json:"windowHeight"`      // 窗口高度(px)
}

// OpenClawConfig OpenClaw 服务配置
type OpenClawConfig struct {
	Enabled bool   `json:"enabled"` // 是否启用
	Port    int    `json:"port"`    // 监听端口
	APIKey  string `json:"apiKey"`  // API 鉴权密钥（可选）
}

// HistoryConfig 历史数据采集配置
type HistoryConfig struct {
	AutoCollectDaily bool   `json:"autoCollectDaily"` // 是否每日盘后自动采集
	CollectStart     string `json:"collectStart"`     // 开始时间 HH:MM
	CollectEnd       string `json:"collectEnd"`       // 结束时间 HH:MM
	IncludeBeijing   bool   `json:"includeBeijing"`   // 是否包含北交所
	LastCollectDate  string `json:"lastCollectDate"`  // 上次自动采集日期
}

// IndicatorConfig 技术指标配置
type IndicatorConfig struct {
	MA   MAConfig   `json:"ma"`
	EMA  EMAConfig  `json:"ema"`
	BOLL BOLLConfig `json:"boll"`
	MACD MACDConfig `json:"macd"`
	RSI  RSIConfig  `json:"rsi"`
	KDJ  KDJConfig  `json:"kdj"`
}

type MAConfig struct {
	Enabled bool  `json:"enabled"`
	Periods []int `json:"periods"` // 默认 [5, 10, 20]
}

type EMAConfig struct {
	Enabled bool  `json:"enabled"`
	Periods []int `json:"periods"` // 默认 [12, 26]
}

type BOLLConfig struct {
	Enabled    bool    `json:"enabled"`
	Period     int     `json:"period"`     // 默认 20
	Multiplier float64 `json:"multiplier"` // 默认 2.0
}

type MACDConfig struct {
	Enabled bool `json:"enabled"`
	Fast    int  `json:"fast"`   // 默认 12
	Slow    int  `json:"slow"`   // 默认 26
	Signal  int  `json:"signal"` // 默认 9
}

type RSIConfig struct {
	Enabled bool `json:"enabled"`
	Period  int  `json:"period"` // 默认 14
}

type KDJConfig struct {
	Enabled bool `json:"enabled"`
	Period  int  `json:"period"` // 默认 9
	K       int  `json:"k"`      // 默认 3
	D       int  `json:"d"`      // 默认 3
}
