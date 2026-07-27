package hottrend

import "time"

// HotItem 热点条目
type HotItem struct {
	ID       string `json:"id"`        // 唯一标识
	Title    string `json:"title"`     // 标题
	URL      string `json:"url"`       // 链接
	HotScore int    `json:"hot_score"` // 热度值
	Rank     int    `json:"rank"`      // 排名
	Platform string `json:"platform"`  // 平台标识
	Extra    string `json:"extra"`     // 附加信息（如热度描述）
}

// HotTrendResult 热点获取结果
type HotTrendResult struct {
	Platform   string    `json:"platform"`    // 平台标识
	PlatformCN string    `json:"platform_cn"` // 平台中文名
	Items      []HotItem `json:"items"`       // 热点列表
	UpdatedAt  time.Time `json:"updated_at"`  // 更新时间
	FromCache  bool      `json:"from_cache"`  // 是否来自缓存
	Error      string    `json:"error"`       // 错误信息
}

// PlatformInfo 平台信息
type PlatformInfo struct {
	ID      string // 平台标识
	Name    string // 平台中文名
	HomeURL string // 平台首页
}

// 支持的平台列表
var SupportedPlatforms = []PlatformInfo{
	{ID: "weibo", Name: "微博热搜", HomeURL: "https://weibo.com"},
	{ID: "zhihu", Name: "知乎热榜", HomeURL: "https://www.zhihu.com"},
	{ID: "bilibili", Name: "B站热搜", HomeURL: "https://www.bilibili.com"},
	{ID: "baidu", Name: "百度热搜", HomeURL: "https://www.baidu.com"},
	{ID: "douyin", Name: "抖音热点", HomeURL: "https://www.douyin.com"},
	{ID: "toutiao", Name: "头条热榜", HomeURL: "https://www.toutiao.com"},
}

// Fetcher 热点数据获取接口
type Fetcher interface {
	// Fetch 获取热点数据
	Fetch() ([]HotItem, error)
	// Platform 返回平台标识
	Platform() string
	// PlatformCN 返回平台中文名
	PlatformCN() string
}
