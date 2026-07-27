package services

// 四价回补:治"日常快照采集只写 close,open/high/low 恒 NULL"的历史欠账。
// 快照采集端已改为随行写入 f17/f15/f16(2026-07-24),本模块只负责把存量缺口用行情源日K补上。
// 与 RebuildStockQfq 的关键区别:那条路会整行覆盖 close/volume/amount/pct(为除权回调设计),
// 源K amount 缺失时会把快照真值冲掉;这里只 UPDATE open/high/low 三列,其余列一律不动。
// 口径保护:仅当源K close 与库中 close_price 偏差≤0.5% 才写——近窗口无除权的票两口径同值,
// 除权票历史行已由每日除权刷新/断层自愈重建(自带四价,不在缺失集);仍不匹配=口径漂移,跳过计数,宁缺勿错。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// OHLCBackfillResult 四价回补结果
type OHLCBackfillResult struct {
	WindowStart   string `json:"window_start"`   // 回补窗口最早交易日
	WindowEnd     string `json:"window_end"`     // 回补窗口最晚交易日
	MissingRows   int    `json:"missing_rows"`   // 起始缺四价行数
	Stocks        int    `json:"stocks"`         // 涉及股票数
	UpdatedRows   int    `json:"updated_rows"`   // 补上的行数
	SkipNoKline   int    `json:"skip_no_kline"`  // 源日K无该日/该日缺OHL,跳过
	SkipMismatch  int    `json:"skip_mismatch"`  // close 口径不符(>0.5%)跳过
	FailedStocks  int    `json:"failed_stocks"`  // 拉源日K失败股数
	RemainingRows int    `json:"remaining_rows"` // 收尾复查仍缺的行数
	Elapsed       string `json:"elapsed"`
	Message       string `json:"message"`
}

// BackfillMissingOHLC 回补最近 days 个交易日内缺 开/高/低 的 stock_daily 行(close 全程有值)。
// concurrency 同时约束源K拉取与写事务并发(SQLite 单写者,>2 只会锁冲突),默认2。
func (s *HistoryService) BackfillMissingOHLC(days, concurrency int) OHLCBackfillResult {
	start := time.Now()
	res := OHLCBackfillResult{}
	if s == nil || s.db == nil || s.marketService == nil {
		res.Message = "history service not ready"
		return res
	}
	if days <= 0 {
		days = 40
	}
	if concurrency <= 0 || concurrency > 6 {
		concurrency = 2
	}

	// 1) 最近 days 个交易日 → 窗口下界
	dr, err := s.db.Query(`SELECT DISTINCT trade_date FROM stock_daily ORDER BY trade_date DESC LIMIT ?`, days)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	var dates []string
	for dr.Next() {
		var d string
		if dr.Scan(&d) == nil && d != "" {
			dates = append(dates, d)
		}
	}
	dr.Close()
	if len(dates) == 0 {
		res.Message = "stock_daily 为空,无可回补窗口"
		return res
	}
	sort.Strings(dates)
	res.WindowStart, res.WindowEnd = dates[0], dates[len(dates)-1]

	// 2) 一次捞出窗口内全部缺四价行,按股分组
	missing := map[string][]ohlcMissRow{}
	rows, err := s.db.Query(`SELECT stock_code, trade_date, close_price FROM stock_daily
		WHERE trade_date>=? AND close_price>0
		  AND (open_price IS NULL OR open_price<=0 OR high_price IS NULL OR high_price<=0 OR low_price IS NULL OR low_price<=0)
		ORDER BY stock_code, trade_date`, res.WindowStart)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	for rows.Next() {
		var code, date string
		var closeP float64
		if rows.Scan(&code, &date, &closeP) == nil && code != "" && closeP > 0 {
			missing[code] = append(missing[code], ohlcMissRow{date: date, close: closeP})
			res.MissingRows++
		}
	}
	rows.Close()
	res.Stocks = len(missing)
	if res.MissingRows == 0 {
		res.Elapsed = time.Since(start).Round(time.Second).String()
		res.Message = fmt.Sprintf("窗口 %s~%s 四价已齐,无需回补", res.WindowStart, res.WindowEnd)
		return res
	}
	historyLog.Info("四价回补启动: 窗口 %s~%s, 缺失 %d 行 / %d 只, 并发 %d", res.WindowStart, res.WindowEnd, res.MissingRows, res.Stocks, concurrency)

	codes := make([]string, 0, len(missing))
	for c := range missing {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	// 3) 逐股拉源日K,只补 open/high/low
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	done := 0
	for _, code := range codes {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			up, noK, mism, ferr := s.backfillOHLCForStock(code, missing[code], days+30)
			mu.Lock()
			defer mu.Unlock()
			done++
			if ferr != nil {
				res.FailedStocks++
				if res.FailedStocks <= 20 {
					historyLog.Warn("四价回补 %s 失败: %v", code, ferr)
				}
			} else {
				res.UpdatedRows += up
				res.SkipNoKline += noK
				res.SkipMismatch += mism
			}
			if done%500 == 0 {
				historyLog.Info("四价回补进度 %d/%d: 已补 %d 行, 跳过(无K %d / 口径 %d), 失败 %d 只",
					done, res.Stocks, res.UpdatedRows, res.SkipNoKline, res.SkipMismatch, res.FailedStocks)
			}
		}(code)
	}
	wg.Wait()

	// 4) 收尾复查补齐率
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM stock_daily
		WHERE trade_date>=? AND close_price>0
		  AND (open_price IS NULL OR open_price<=0 OR high_price IS NULL OR high_price<=0 OR low_price IS NULL OR low_price<=0)`,
		res.WindowStart).Scan(&res.RemainingRows); err != nil {
		historyLog.Warn("四价回补收尾复查失败: %v", err)
	}
	res.Elapsed = time.Since(start).Round(time.Second).String()
	fillRate := 0.0
	if res.MissingRows > 0 {
		fillRate = float64(res.MissingRows-res.RemainingRows) / float64(res.MissingRows) * 100
	}
	res.Message = fmt.Sprintf("四价回补完成: 窗口 %s~%s, 起始缺 %d 行/%d 只 → 补 %d 行, 跳过(无K %d/口径 %d), 失败 %d 只, 残留 %d 行, 补齐率 %.1f%%, 耗时 %s",
		res.WindowStart, res.WindowEnd, res.MissingRows, res.Stocks, res.UpdatedRows,
		res.SkipNoKline, res.SkipMismatch, res.FailedStocks, res.RemainingRows, fillRate, res.Elapsed)
	historyLog.Info("%s", res.Message)
	return res
}

// ohlcMissRow 缺四价的一行(close 用于与源K做口径校验)
type ohlcMissRow struct {
	date  string
	close float64
}

// backfillOHLCForStock 单股回补:拉源日K(重试2次),对缺失行做 close 口径校验后 UPDATE 三列。
func (s *HistoryService) backfillOHLCForStock(code string, miss []ohlcMissRow, bars int) (updated, skipNoK, skipMismatch int, err error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(miss) == 0 {
		return 0, 0, 0, nil
	}
	type ohlc struct{ open, high, low, close float64 }
	kmap := map[string]ohlc{}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ks, e := s.marketService.GetKLineData(code, "1d", bars)
		if e == nil && len(ks) > 0 {
			for _, k := range ks {
				d := strings.TrimSpace(k.Time)
				if len(d) >= 10 {
					kmap[d[:10]] = ohlc{open: k.Open, high: k.High, low: k.Low, close: k.Close}
				}
			}
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("kline unavailable: %v (len=%d)", e, len(ks))
		time.Sleep(time.Duration(attempt+1) * 400 * time.Millisecond)
	}
	if lastErr != nil {
		return 0, 0, 0, lastErr
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, 0, err
	}
	// open_price 仍空才写:幂等,且不覆盖并发路径(除权刷新)刚写的新口径值
	upd, err := tx.Prepare(`UPDATE stock_daily SET open_price=?, high_price=?, low_price=?, updated_at=?
		WHERE stock_code=? AND trade_date=? AND (open_price IS NULL OR open_price<=0)`)
	if err != nil {
		tx.Rollback()
		return 0, 0, 0, err
	}
	defer upd.Close()

	nowText := time.Now().Format("2006-01-02 15:04:05")
	for _, m := range miss {
		k, ok := kmap[m.date]
		if !ok || k.open <= 0 || k.high <= 0 || k.low <= 0 || k.close <= 0 {
			skipNoK++
			continue
		}
		if math.Abs(k.close/m.close-1) > 0.005 {
			skipMismatch++
			continue
		}
		r, e := upd.Exec(k.open, k.high, k.low, nowText, code, m.date)
		if e != nil {
			tx.Rollback()
			return 0, 0, 0, e
		}
		if n, _ := r.RowsAffected(); n > 0 {
			updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return updated, skipNoK, skipMismatch, nil
}
