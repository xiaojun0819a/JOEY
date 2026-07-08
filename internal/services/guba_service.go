package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// GubaPost 股吧帖子
type GubaPost struct {
	Title    string `json:"title"`
	Time     string `json:"time"`     // 发帖时间(东财给的相对/绝对串)
	Clicks   int    `json:"clicks"`   // 阅读数
	Comments int    `json:"comments"` // 评论数
	URL      string `json:"url"`
}

// GubaSummary 个股股吧舆情摘要
type GubaSummary struct {
	Code      string     `json:"code"`
	BarName   string     `json:"barName"`
	Posts     []GubaPost `json:"posts"`
	FetchedAt string     `json:"fetchedAt"`
}

// GubaService 东财股吧舆情(列表页内嵌 article_list JSON,无需登录)
type GubaService struct {
	client *http.Client

	mu      sync.Mutex
	cache   map[string]GubaSummary
	cacheAt map[string]time.Time
}

var (
	defaultGubaOnce sync.Once
	defaultGuba     *GubaService
)

// DefaultGubaService 进程级单例(免去 App 结构体接线,避免与其他改动冲突)
func DefaultGubaService() *GubaService {
	defaultGubaOnce.Do(func() {
		defaultGuba = &GubaService{
			client:  proxy.GetManager().GetClientWithTimeout(10 * time.Second),
			cache:   make(map[string]GubaSummary),
			cacheAt: make(map[string]time.Time),
		}
	})
	return defaultGuba
}

// extractGubaJSON 从页面提取 article_list={...} 的 JSON:
// 定位标记后做字符串感知的花括号配平(标题里可能含引号/转义,不能用简单正则)。
func extractGubaJSON(body []byte) []byte {
	const marker = "article_list="
	i := strings.Index(string(body), marker)
	if i < 0 {
		return nil
	}
	s := body[i+len(marker):]
	if len(s) == 0 || s[0] != '{' {
		return nil
	}
	depth, inStr, esc := 0, false, false
	for j := 0; j < len(s); j++ {
		ch := s[j]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:j+1]
			}
		}
	}
	return nil
}

// GetGubaSummary 抓取个股股吧首页热帖(2分钟缓存)。code 支持 sh600519/600519 两种。
func (g *GubaService) GetGubaSummary(code string, limit int) (GubaSummary, error) {
	pure := strings.TrimLeft(strings.ToLower(strings.TrimSpace(code)), "shzbj")
	if len(pure) != 6 {
		return GubaSummary{}, fmt.Errorf("无法识别股票代码: %s", code)
	}
	if limit <= 0 || limit > 30 {
		limit = 10
	}

	g.mu.Lock()
	if at, ok := g.cacheAt[pure]; ok && time.Since(at) < 2*time.Minute {
		cached := g.cache[pure]
		g.mu.Unlock()
		if len(cached.Posts) > limit {
			cached.Posts = cached.Posts[:limit]
		}
		return cached, nil
	}
	g.mu.Unlock()

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://guba.eastmoney.com/list,%s.html", pure), nil)
	if err != nil {
		return GubaSummary{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://guba.eastmoney.com/")
	resp, err := g.client.Do(req)
	if err != nil {
		return GubaSummary{}, fmt.Errorf("股吧请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return GubaSummary{}, err
	}

	raw := extractGubaJSON(body)
	if raw == nil {
		return GubaSummary{}, fmt.Errorf("股吧页面结构变化,未找到帖子数据")
	}
	var payload struct {
		Re []struct {
			PostID       int64  `json:"post_id"`
			PostTitle    string `json:"post_title"`
			BarName      string `json:"stockbar_name"`
			ClickCount   int    `json:"post_click_count"`
			CommentCount int    `json:"post_comment_count"`
			PublishTime  string `json:"post_publish_time"`
		} `json:"re"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return GubaSummary{}, fmt.Errorf("股吧数据解析失败: %w", err)
	}

	out := GubaSummary{Code: pure, FetchedAt: time.Now().Format("2006-01-02 15:04:05")}
	for _, p := range payload.Re {
		title := strings.TrimSpace(p.PostTitle)
		if title == "" {
			continue
		}
		if out.BarName == "" {
			out.BarName = p.BarName
		}
		out.Posts = append(out.Posts, GubaPost{
			Title:    title,
			Time:     p.PublishTime,
			Clicks:   p.ClickCount,
			Comments: p.CommentCount,
			URL:      fmt.Sprintf("https://guba.eastmoney.com/news,%s,%d.html", pure, p.PostID),
		})
	}
	if len(out.Posts) == 0 {
		return GubaSummary{}, fmt.Errorf("股吧无帖子数据")
	}

	g.mu.Lock()
	g.cache[pure] = out
	g.cacheAt[pure] = time.Now()
	g.mu.Unlock()

	if len(out.Posts) > limit {
		out.Posts = out.Posts[:limit]
	}
	return out, nil
}
