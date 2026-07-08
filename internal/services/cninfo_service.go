package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// CninfoAnnouncement 巨潮公告条目
type CninfoAnnouncement struct {
	Title string `json:"title"`
	Time  string `json:"time"` // yyyy-MM-dd
	URL   string `json:"url"`  // PDF 直链
}

// CninfoResult 巨潮公告查询结果
type CninfoResult struct {
	Code          string               `json:"code"`
	SearchKey     string               `json:"searchKey"`
	Total         int                  `json:"total"`
	Announcements []CninfoAnnouncement `json:"announcements"`
}

// CninfoService 巨潮资讯(证监会指定披露平台)公告查询。
// 相比东财公告源的增量:官方一手、支持关键词(如"问询函""减持")过滤。
type CninfoService struct {
	client *http.Client

	mu        sync.Mutex
	orgIDs    map[string]string // 6位代码 → orgId
	orgIDsAt  time.Time
}

var (
	defaultCninfoOnce sync.Once
	defaultCninfo     *CninfoService
)

// DefaultCninfoService 进程级单例
func DefaultCninfoService() *CninfoService {
	defaultCninfoOnce.Do(func() {
		defaultCninfo = &CninfoService{
			client: proxy.GetManager().GetClientWithTimeout(15 * time.Second),
		}
	})
	return defaultCninfo
}

// orgID 查代码对应的巨潮 orgId(全量表约6000条,24小时缓存)
func (c *CninfoService) orgID(pureCode string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.orgIDs != nil && time.Since(c.orgIDsAt) < 24*time.Hour {
		if id, ok := c.orgIDs[pureCode]; ok {
			return id, nil
		}
		return "", fmt.Errorf("巨潮无此代码: %s", pureCode)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://www.cninfo.com.cn/new/data/szse_stock.json", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("巨潮代码表请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	var payload struct {
		StockList []struct {
			Code  string `json:"code"`
			OrgID string `json:"orgId"`
		} `json:"stockList"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("巨潮代码表解析失败: %w", err)
	}
	if len(payload.StockList) == 0 {
		return "", fmt.Errorf("巨潮代码表为空")
	}
	ids := make(map[string]string, len(payload.StockList))
	for _, s := range payload.StockList {
		ids[s.Code] = s.OrgID
	}
	c.orgIDs = ids
	c.orgIDsAt = time.Now()
	if id, ok := ids[pureCode]; ok {
		return id, nil
	}
	return "", fmt.Errorf("巨潮无此代码: %s", pureCode)
}

// QueryAnnouncements 查询个股公告。searchKey 可选(如"问询函""减持""回购"),空=全部;limit 1-30。
func (c *CninfoService) QueryAnnouncements(code, searchKey string, limit int) (CninfoResult, error) {
	pure := strings.TrimLeft(strings.ToLower(strings.TrimSpace(code)), "shzbj")
	if len(pure) != 6 {
		return CninfoResult{}, fmt.Errorf("无法识别股票代码: %s", code)
	}
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	orgID, err := c.orgID(pure)
	if err != nil {
		return CninfoResult{}, err
	}
	column := "szse"
	if strings.HasPrefix(pure, "6") {
		column = "sse"
	}

	form := url.Values{}
	form.Set("pageNum", "1")
	form.Set("pageSize", fmt.Sprintf("%d", limit))
	form.Set("column", column)
	form.Set("tabName", "fulltext")
	form.Set("stock", pure+","+orgID)
	form.Set("searchkey", strings.TrimSpace(searchKey))
	form.Set("seDate", "")

	req, err := http.NewRequest(http.MethodPost, "http://www.cninfo.com.cn/new/hisAnnouncement/query", strings.NewReader(form.Encode()))
	if err != nil {
		return CninfoResult{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return CninfoResult{}, fmt.Errorf("巨潮查询失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return CninfoResult{}, err
	}

	var payload struct {
		TotalAnnouncement int `json:"totalAnnouncement"`
		Announcements     []struct {
			Title      string `json:"announcementTitle"`
			TimeMillis int64  `json:"announcementTime"`
			AdjunctURL string `json:"adjunctUrl"`
		} `json:"announcements"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CninfoResult{}, fmt.Errorf("巨潮响应解析失败: %w", err)
	}

	out := CninfoResult{Code: pure, SearchKey: strings.TrimSpace(searchKey), Total: payload.TotalAnnouncement}
	for _, a := range payload.Announcements {
		title := strings.NewReplacer("<em>", "", "</em>", "").Replace(strings.TrimSpace(a.Title))
		item := CninfoAnnouncement{Title: title}
		if a.TimeMillis > 0 {
			item.Time = time.UnixMilli(a.TimeMillis).Format("2006-01-02")
		}
		if a.AdjunctURL != "" {
			item.URL = "http://static.cninfo.com.cn/" + strings.TrimPrefix(a.AdjunctURL, "/")
		}
		out.Announcements = append(out.Announcements, item)
	}
	return out, nil
}
