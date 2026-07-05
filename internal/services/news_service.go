package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// Telegraph 快讯数据结构
type Telegraph struct {
	Time    string `json:"time"`
	Content string `json:"content"`
	URL     string `json:"url"`
}

// NewsService 资讯服务
type NewsService struct {
	client *http.Client

	// 缓存
	telegraphs    []Telegraph
	lastFetchTime time.Time
	mu            sync.RWMutex
}

// NewNewsService 创建资讯服务
func NewNewsService() *NewsService {
	return &NewsService{
		client:     proxy.GetManager().GetClientWithTimeout(10 * time.Second),
		telegraphs: make([]Telegraph, 0),
	}
}

// GetTelegraphList 获取财联社快讯列表
func (s *NewsService) GetTelegraphList() ([]Telegraph, error) {
	// 检查缓存，30秒内不重复请求
	s.mu.RLock()
	if time.Since(s.lastFetchTime) < 30*time.Second && len(s.telegraphs) > 0 {
		result := make([]Telegraph, len(s.telegraphs))
		copy(result, s.telegraphs)
		s.mu.RUnlock()
		return result, nil
	}
	s.mu.RUnlock()

	// 东财 7x24 快讯 JSON 接口。cls.cn 已改成 JS SPA，静态 HTML 里没有快讯条目(goquery 爬到 0 条)，
	// 改用东财返回 JSONP(var ajaxResult={...LivesList:[...]})的稳定接口。
	req, err := http.NewRequest("GET", "https://newsapi.eastmoney.com/kuaixun/v1/getlist_102_ajaxResult_50_1_.html", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://kuaixun.eastmoney.com/")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 剥掉 JSONP 包裹：var ajaxResult={...};
	raw := string(body)
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[idx:]
	}
	raw = strings.TrimRight(strings.TrimSpace(raw), ";")

	var parsed struct {
		LivesList []struct {
			Digest   string `json:"digest"`
			Title    string `json:"title"`
			ShowTime string `json:"showtime"`
			URLW     string `json:"url_w"`
		} `json:"LivesList"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}

	telegraphs := make([]Telegraph, 0, len(parsed.LivesList))
	for i, item := range parsed.LivesList {
		if i >= 30 {
			break
		}
		content := cleanContent(strings.TrimSpace(item.Digest))
		if content == "" {
			content = cleanContent(strings.TrimSpace(item.Title))
		}
		if content == "" {
			continue
		}
		// showtime "2026-07-02 00:42:15" → "00:42"
		t := item.ShowTime
		if len(t) >= 16 {
			t = t[11:16]
		}
		telegraphs = append(telegraphs, Telegraph{
			Time:    t,
			Content: content,
			URL:     item.URLW,
		})
	}

	// 在财联社快讯前，注入 AI 卡口选股线索（来自同 NAS 的 aicardmap 情报雷达）。
	// 环境变量 JCP_SIGNALS_URL 未配置或抓取失败时静默跳过，不影响原快讯。
	telegraphs = append(s.fetchCardMapSignals(), telegraphs...)

	// 更新缓存
	s.mu.Lock()
	s.telegraphs = telegraphs
	s.lastFetchTime = time.Now()
	s.mu.Unlock()

	return telegraphs, nil
}

// cardMapSignal 对应 aicardmap /api/jcp-signals 返回的单条信号。
type cardMapSignal struct {
	NodeName string `json:"nodeName"`
	Category string `json:"category"`
	Direction string `json:"direction"`
	Tighten  int    `json:"tighten"`
	Loosen   int    `json:"loosen"`
	Count    int    `json:"count"`
	Stocks   []struct {
		Name   string `json:"name"`
		Ticker string `json:"ticker"`
		Role   string `json:"role"`
	} `json:"stocks"`
	Headline string `json:"headline"`
	URL      string `json:"url"`
	Date     string `json:"date"`
}

// fetchCardMapSignals 从 aicardmap 拉取「卡口→A股」选股线索，转成快讯条目。
// 只注入前若干条，Time 用「AI」前缀标识区别于真实快讯；任何异常都返回空切片。
func (s *NewsService) fetchCardMapSignals() []Telegraph {
	url := strings.TrimSpace(os.Getenv("JCP_SIGNALS_URL"))
	if url == "" {
		return nil
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	// 走本机直连（127.0.0.1），不经代理
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var parsed struct {
		OK      bool            `json:"ok"`
		Signals []cardMapSignal `json:"signals"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
		return nil
	}

	dirLabel := map[string]string{"tighten": "趋紧", "loosen": "趋松", "neutral": "中性"}
	out := make([]Telegraph, 0, len(parsed.Signals))
	for i, sig := range parsed.Signals {
		if i >= 6 { // 最多注入 6 条，避免淹没真实快讯
			break
		}
		if len(sig.Stocks) == 0 {
			continue
		}
		names := make([]string, 0, len(sig.Stocks))
		for _, st := range sig.Stocks {
			names = append(names, st.Name)
		}
		content := fmt.Sprintf("【AI卡口·%s】%s（%s）近期%d条情报%s · A股关联：%s",
			dirLabel[sig.Direction], sig.NodeName, sig.Category, sig.Count,
			dirLabel[sig.Direction], strings.Join(names, "、"))
		if sig.Headline != "" {
			content += " ｜ " + sig.Headline
		}
		out = append(out, Telegraph{
			Time:    "AI卡口",
			Content: cleanContent(content),
			URL:     sig.URL,
		})
	}
	return out
}

// GetLatestTelegraph 获取最新一条快讯
func (s *NewsService) GetLatestTelegraph() *Telegraph {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.telegraphs) > 0 {
		return &s.telegraphs[0]
	}
	return nil
}

// cleanContent 清理内容中的多余空白字符
func cleanContent(s string) string {
	// 替换多个空白字符为单个空格
	s = strings.Join(strings.Fields(s), " ")
	// 移除特殊字符
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}
