package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// MarginDay 两融单日汇总
type MarginDay struct {
	Date      string  `json:"date"`
	RZYE      float64 `json:"rzye"`      // 融资余额(元)
	RQYE      float64 `json:"rqye"`      // 融券余额(元)
	RZRQTotal float64 `json:"rzrqTotal"` // 两融余额合计(元)
}

// SectorMove 板块涨跌
type SectorMove struct {
	Name      string  `json:"name"`
	ChangePct float64 `json:"changePct"`
}

// MarketMood 市场情绪面快照
type MarketMood struct {
	Date         string       `json:"date"`         // 涨跌分布对应日期
	UpCount      int          `json:"upCount"`      // 上涨家数
	DownCount    int          `json:"downCount"`    // 下跌家数
	FlatCount    int          `json:"flatCount"`    // 平盘家数
	StrongCount  int          `json:"strongCount"`  // 涨幅≥7%家数(近涨停梯队)
	WeakCount    int          `json:"weakCount"`    // 跌幅≤-7%家数
	MarginTrend  []MarginDay  `json:"marginTrend"`  // 近5个交易日两融余额(旧→新)
	TopSectors   []SectorMove `json:"topSectors"`   // 行业板块涨幅前5
	BottomSectors []SectorMove `json:"bottomSectors"` // 行业板块跌幅前5
	FetchedAt    string       `json:"fetchedAt"`
}

// MarketMoodService 市场情绪面(东财开放接口:涨跌分布/两融/行业板块)
type MarketMoodService struct {
	client *http.Client

	mu      sync.Mutex
	cache   *MarketMood
	cacheAt time.Time
}

var (
	defaultMoodOnce sync.Once
	defaultMood     *MarketMoodService
)

// DefaultMarketMoodService 进程级单例
func DefaultMarketMoodService() *MarketMoodService {
	defaultMoodOnce.Do(func() {
		defaultMood = &MarketMoodService{
			client: proxy.GetManager().GetClientWithTimeout(10 * time.Second),
		}
	})
	return defaultMood
}

func (s *MarketMoodService) fetch(rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// GetMarketMood 拉取市场情绪面快照(3分钟缓存)。三路数据独立容错:某一路失败不影响其余。
func (s *MarketMoodService) GetMarketMood() (MarketMood, error) {
	s.mu.Lock()
	if s.cache != nil && time.Since(s.cacheAt) < 3*time.Minute {
		cached := *s.cache
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	out := MarketMood{FetchedAt: time.Now().Format("2006-01-02 15:04:05")}
	var errs []string

	if err := s.fillUpDown(&out); err != nil {
		errs = append(errs, "涨跌分布:"+err.Error())
	}
	if err := s.fillMargin(&out); err != nil {
		errs = append(errs, "两融:"+err.Error())
	}
	if err := s.fillSectors(&out); err != nil {
		errs = append(errs, "板块:"+err.Error())
	}
	// 全部失败才报错;部分成功照常返回
	if len(errs) == 3 {
		return MarketMood{}, fmt.Errorf("情绪面数据全部获取失败: %s", strings.Join(errs, "; "))
	}

	s.mu.Lock()
	snapshot := out
	s.cache = &snapshot
	s.cacheAt = time.Now()
	s.mu.Unlock()
	return out, nil
}

// fillUpDown 全市场涨跌家数分布(push2ex getTopicZDFenBu,fenbu 直方图 key=涨跌幅整数档)
func (s *MarketMoodService) fillUpDown(out *MarketMood) error {
	body, err := s.fetch("https://push2ex.eastmoney.com/getTopicZDFenBu?ut=7eea3edcaed734bea9cbfc24409ed989&dpt=wz.ztzt", "https://quote.eastmoney.com/")
	if err != nil {
		return err
	}
	var payload struct {
		Data struct {
			QDate int              `json:"qdate"`
			Fenbu []map[string]int `json:"fenbu"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	if len(payload.Data.Fenbu) == 0 {
		return fmt.Errorf("空分布")
	}
	for _, kv := range payload.Data.Fenbu {
		for k, n := range kv {
			bucket, err := strconv.Atoi(k)
			if err != nil {
				continue
			}
			switch {
			case bucket > 0:
				out.UpCount += n
				if bucket >= 7 {
					out.StrongCount += n
				}
			case bucket < 0:
				out.DownCount += n
				if bucket <= -7 {
					out.WeakCount += n
				}
			default:
				out.FlatCount += n
			}
		}
	}
	if d := payload.Data.QDate; d > 0 {
		out.Date = fmt.Sprintf("%04d-%02d-%02d", d/10000, d/100%100, d%100)
	}
	return nil
}

// fillMargin 沪深两融余额近5日(东财 datacenter,JSONP 需剥壳)
func (s *MarketMoodService) fillMargin(out *MarketMood) error {
	q := url.Values{}
	q.Set("callback", "cb")
	q.Set("reportName", "RPTA_RZRQ_LSHJ")
	q.Set("columns", "ALL")
	q.Set("pageNumber", "1")
	q.Set("pageSize", "5")
	q.Set("sortColumns", "dim_date")
	q.Set("sortTypes", "-1")
	body, err := s.fetch("https://datacenter-web.eastmoney.com/api/data/v1/get?"+q.Encode(), "https://data.eastmoney.com/")
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(body))
	text = strings.TrimPrefix(text, "cb(")
	text = strings.TrimSuffix(text, ");")
	text = strings.TrimSuffix(text, ")")
	var payload struct {
		Result struct {
			Data []struct {
				DimDate string  `json:"DIM_DATE"`
				RZYE    float64 `json:"RZYE"`
				RQYE    float64 `json:"RQYE"`
				RZRQYE  float64 `json:"RZRQYE"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return err
	}
	if len(payload.Result.Data) == 0 {
		return fmt.Errorf("空数据")
	}
	days := make([]MarginDay, 0, len(payload.Result.Data))
	for _, d := range payload.Result.Data {
		date := d.DimDate
		if len(date) >= 10 {
			date = date[:10]
		}
		total := d.RZRQYE
		if total == 0 {
			total = d.RZYE + d.RQYE
		}
		days = append(days, MarginDay{Date: date, RZYE: d.RZYE, RQYE: d.RQYE, RZRQTotal: total})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	out.MarginTrend = days
	return nil
}

// fillSectors 行业板块涨跌榜(push2 clist,fs=m:90 t:2 行业板块)
func (s *MarketMoodService) fillSectors(out *MarketMood) error {
	// push2 主站近期对裸调 502,延迟节点 push2delay 稳定;板块涨跌榜延迟几秒可接受
	base := "https://push2delay.eastmoney.com/api/qt/clist/get?pn=1&pz=100&po=1&np=1&ut=8dec03ba335b81bf4ebdf7b29ec27d15&fltt=2&invt=2&fid=f3&fs=" + url.QueryEscape("m:90 t:2 f:!50") + "&fields=f3,f14"
	body, err := s.fetch(base, "https://quote.eastmoney.com/")
	if err != nil {
		return err
	}
	var payload struct {
		Data struct {
			Diff []struct {
				F3  float64 `json:"f3"`
				F14 string  `json:"f14"`
			} `json:"diff"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	rows := payload.Data.Diff
	if len(rows) == 0 {
		return fmt.Errorf("空板块列表")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].F3 > rows[j].F3 })
	for i := 0; i < len(rows) && i < 5; i++ {
		out.TopSectors = append(out.TopSectors, SectorMove{Name: rows[i].F14, ChangePct: rows[i].F3})
	}
	for i := len(rows) - 1; i >= 0 && len(out.BottomSectors) < 5; i-- {
		out.BottomSectors = append(out.BottomSectors, SectorMove{Name: rows[i].F14, ChangePct: rows[i].F3})
	}
	return nil
}
