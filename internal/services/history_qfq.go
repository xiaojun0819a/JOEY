package services

// 前复权对齐工程:把 stock_daily 最近 N 根改写为行情源前复权口径,消除"双口径"——
// 本地库原状:不复权(除权后历史不回调)+ 日常采集行 open/high/low 恒 NULL + volume 单位按行来源混乱
// (日采=amount/price≈股,回补=源日K的手)。这使除权股/缺OHL票在本地口径下技术面失真,
// 波段扫描的行情源复核会大量否决(实测 39/123),而复核取数失败时又回退到失真口径 → 结果不稳。
// 本模块:①一次性全市场重建(RebuildAllQfq);②每日采集后的除权检测+增量刷新(DetectExDivAndRefresh)。
// 约定:volume 统一写「股」(源日K为手,×100);只改价格类字段,富字段(行业/主力/换手/市值)原样保留。

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RebuildQfqResult 全市场前复权重建结果
type RebuildQfqResult struct {
	Total    int    `json:"total"`
	OK       int    `json:"ok"`
	Failed   int    `json:"failed"`
	Updated  int    `json:"updated"`  // 覆盖改写的行数
	Inserted int    `json:"inserted"` // 补插的缺失日期行数
	Elapsed  string `json:"elapsed"`
	Message  string `json:"message"`
}

// RebuildStockQfq 单只重建:取行情源前复权日K(bars 根),改写/补插 stock_daily 对应日期的价格类字段,
// 并对覆盖区间重算 MA5/10/20。取数失败重试2次,仍失败返回 error(调用方计 Failed,绝不写半截)。
func (s *HistoryService) RebuildStockQfq(code string, bars int) (int, int, error) {
	if s == nil || s.db == nil || s.marketService == nil {
		return 0, 0, fmt.Errorf("history service not ready")
	}
	if bars <= 0 {
		bars = 420
	}
	code = strings.ToLower(strings.TrimSpace(code))
	var klines []struct {
		date                                string
		open, high, low, close, vol, amount float64
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ks, e := s.marketService.GetKLineData(code, "1d", bars)
		if e == nil && len(ks) >= 30 {
			for _, k := range ks {
				d := strings.TrimSpace(k.Time)
				if len(d) >= 10 {
					klines = append(klines, struct {
						date                                string
						open, high, low, close, vol, amount float64
					}{d[:10], k.Open, k.High, k.Low, k.Close, float64(k.Volume), k.Amount})
				}
			}
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("kline unavailable: %v (len=%d)", e, len(ks))
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	if lastErr != nil {
		return 0, 0, lastErr
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	upd, err := tx.Prepare(`UPDATE stock_daily
		SET close_price=?, open_price=?, high_price=?, low_price=?, volume=?, amount=?, pct_change=?, updated_at=?
		WHERE stock_code=? AND trade_date=?`)
	if err != nil {
		tx.Rollback()
		return 0, 0, err
	}
	defer upd.Close()
	ins, err := tx.Prepare(`INSERT OR IGNORE INTO stock_daily
		(stock_code, trade_date, close_price, open_price, high_price, low_price, volume, amount, pct_change, main_source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'qfq', ?)`)
	if err != nil {
		tx.Rollback()
		return 0, 0, err
	}
	defer ins.Close()

	nowText := time.Now().Format("2006-01-02 15:04:05")
	updated, inserted := 0, 0
	prevClose := 0.0
	firstDate := ""
	for _, k := range klines {
		if k.close <= 0 {
			prevClose = k.close
			continue
		}
		if firstDate == "" {
			firstDate = k.date
		}
		var pct any
		if prevClose > 0 {
			pct = (k.close - prevClose) / prevClose * 100
		}
		prevClose = k.close
		volShares := k.vol * 100 // 源日K为手 → 统一存「股」
		res, e := upd.Exec(k.close, nullableFloat(k.open), nullableFloat(k.high), nullableFloat(k.low),
			volShares, nullableFloat(k.amount), pct, nowText, code, k.date)
		if e != nil {
			tx.Rollback()
			return 0, 0, e
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
			continue
		}
		if _, e := ins.Exec(code, k.date, k.close, nullableFloat(k.open), nullableFloat(k.high), nullableFloat(k.low),
			volShares, nullableFloat(k.amount), pct, nowText); e != nil {
			tx.Rollback()
			return 0, 0, e
		}
		inserted++
	}

	// 覆盖区间重算 MA5/10/20(前复权改写后旧均线全部失效)
	if firstDate != "" {
		if err := recomputeMAForCodeTx(tx, code, firstDate); err != nil {
			tx.Rollback()
			return 0, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return updated, inserted, nil
}

// recomputeMAForCodeTx 事务内重算某股 fromDate 起的 MA5/10/20(一次载入闭区间收盘序列,滚动计算)。
func recomputeMAForCodeTx(tx *sql.Tx, code, fromDate string) error {
	rows, err := tx.Query(`SELECT trade_date, close_price FROM stock_daily
		WHERE stock_code=? AND close_price>0 ORDER BY trade_date ASC`, code)
	if err != nil {
		return err
	}
	type pt struct {
		date  string
		close float64
	}
	pts := make([]pt, 0, 512)
	for rows.Next() {
		var p pt
		var c sql.NullFloat64
		if rows.Scan(&p.date, &c) == nil && c.Valid && c.Float64 > 0 {
			p.close = c.Float64
			pts = append(pts, p)
		}
	}
	rows.Close()
	upd, err := tx.Prepare(`UPDATE stock_daily SET ma5=?, ma10=?, ma20=? WHERE stock_code=? AND trade_date=?`)
	if err != nil {
		return err
	}
	defer upd.Close()
	sum := func(end, n int) any {
		if end+1 < n {
			return nil
		}
		s := 0.0
		for i := end - n + 1; i <= end; i++ {
			s += pts[i].close
		}
		return s / float64(n)
	}
	for i := range pts {
		if pts[i].date < fromDate {
			continue
		}
		if _, err := upd.Exec(sum(i, 5), sum(i, 10), sum(i, 20), code, pts[i].date); err != nil {
			return err
		}
	}
	return nil
}

// RebuildAllQfq 全市场前复权重建:并发拉源日K逐股改写。重活(全A~5400只×一次源请求),
// 只应手动触发或除权季维护;进度打日志(每200只一条)。
func (s *HistoryService) RebuildAllQfq(concurrency, bars int) RebuildQfqResult {
	start := time.Now()
	res := RebuildQfqResult{}
	if s == nil || s.db == nil {
		res.Message = "history service not ready"
		return res
	}
	if concurrency <= 0 || concurrency > 12 {
		concurrency = 6
	}
	rows, err := s.db.Query(`SELECT DISTINCT stock_code FROM stock_daily`)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	codes := []string{}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil && c != "" {
			codes = append(codes, c)
		}
	}
	rows.Close()
	res.Total = len(codes)

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
			u, i, err := s.RebuildStockQfq(code, bars)
			mu.Lock()
			defer mu.Unlock()
			done++
			if err != nil {
				res.Failed++
			} else {
				res.OK++
				res.Updated += u
				res.Inserted += i
			}
			if done%200 == 0 {
				historyLog.Info("前复权重建进度 %d/%d (成功%d 失败%d)", done, res.Total, res.OK, res.Failed)
			}
		}(code)
	}
	wg.Wait()
	res.Elapsed = time.Since(start).Round(time.Second).String()
	res.Message = fmt.Sprintf("前复权重建完成: %d只(成功%d 失败%d), 改写%d行 补插%d行, 耗时%s",
		res.Total, res.OK, res.Failed, res.Updated, res.Inserted, res.Elapsed)
	historyLog.Info("%s", res.Message)
	return res
}

// DetectQfqBoundaryGaps 断层自愈检测:同一股相邻交易日 close 比值 与 记录的 pct_change 相差
// 超过 gapPct 个百分点 = 历史上有除权发生但前复权没有回调(当日检测漏掉后,次日隐含昨收又对上了,
// 永远不会再被 DetectExDivAndRefresh 发现——2026-07-22 实测固化 500+ 只,锦富技术复盘-45%幻影亏损即此)。
// 只扫 sinceDate 起、且早于 excludeDate(当日盘前采集占位行 close=昨收、pct=昨日值,天然假断层)。
func (s *HistoryService) DetectQfqBoundaryGaps(sinceDate, excludeDate string, gapPct float64) []string {
	if s == nil || s.db == nil {
		return nil
	}
	if gapPct <= 0 {
		gapPct = 8
	}
	rows, err := s.db.Query(`
		SELECT DISTINCT stock_code FROM (
			SELECT stock_code, trade_date, pct_change,
				LAG(close_price) OVER (PARTITION BY stock_code ORDER BY trade_date) prev_close,
				close_price
			FROM stock_daily WHERE trade_date>=? AND close_price>0
		) WHERE trade_date<? AND prev_close>0
		  AND ABS((close_price/prev_close-1)*100 - COALESCE(pct_change,(close_price/prev_close-1)*100)) > ?`,
		sinceDate, excludeDate, gapPct)
	if err != nil {
		historyLog.Warn("断层检测查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil && c != "" {
			out = append(out, c)
		}
	}
	return out
}

// RepairQfqGaps 对断层股票逐只重建前复权(420根窗口,与每日刷新同口径)。返回(检测数,修复成功数)。
func (s *HistoryService) RepairQfqGaps(sinceDate, excludeDate string, maxN int) (int, int) {
	dirty := s.DetectQfqBoundaryGaps(sinceDate, excludeDate, 8)
	if len(dirty) == 0 {
		return 0, 0
	}
	if maxN > 0 && len(dirty) > maxN {
		historyLog.Warn("断层修复超上限(%d>%d),本轮只修前%d只,其余下轮继续", len(dirty), maxN, maxN)
		dirty = dirty[:maxN]
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2) // 写事务并发压到2:SQLite单写者,高并发只会锁冲突
	repaired := 0
	var mu sync.Mutex
	for _, code := range dirty {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if _, _, err := s.RebuildStockQfq(code, 420); err == nil {
				mu.Lock()
				repaired++
				mu.Unlock()
			} else {
				historyLog.Warn("断层修复失败 %s: %v", code, err)
			}
		}(code)
	}
	wg.Wait()
	historyLog.Info("前复权断层自愈: 检测%d只, 修复%d只 (范围%s起)", len(dirty), repaired, sinceDate)
	return len(dirty), repaired
}

// DetectExDivAndRefresh 每日采集后的除权检测:快照隐含昨收(price/(1+pct/100)) 与库中上一交易日收盘
// 偏差>0.4% 视为除权/送转,对这些票做单股前复权刷新(把历史回调到新口径)。返回刷新数。
func (s *HistoryService) DetectExDivAndRefresh(tradeDate string, stocks []ScanSnapshotRow) int {
	if s == nil || s.db == nil || len(stocks) == 0 {
		return 0
	}
	var prevDate string
	if err := s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily WHERE trade_date < ?`, tradeDate).Scan(&prevDate); err != nil || prevDate == "" {
		return 0
	}
	prevClose := map[string]float64{}
	rows, err := s.db.Query(`SELECT stock_code, close_price FROM stock_daily WHERE trade_date=? AND close_price>0`, prevDate)
	if err != nil {
		return 0
	}
	for rows.Next() {
		var c string
		var v sql.NullFloat64
		if rows.Scan(&c, &v) == nil && v.Valid {
			prevClose[strings.ToLower(c)] = v.Float64
		}
	}
	rows.Close()

	dirty := []string{}
	for _, st := range stocks {
		if st.Symbol == "" || st.Price <= 0 || st.ChangePercent <= -90 {
			continue
		}
		stored, ok := prevClose[strings.ToLower(st.Symbol)]
		if !ok || stored <= 0 {
			continue
		}
		implied := st.Price / (1 + st.ChangePercent/100)
		if implied <= 0 {
			continue
		}
		if diff := (implied - stored) / stored; diff > 0.004 || diff < -0.004 {
			dirty = append(dirty, st.Symbol)
		}
	}
	if len(dirty) == 0 {
		return 0
	}
	const maxDaily = 120 // 除权季单日几十只属正常;异常爆量(快照口径错乱)时截断防雪崩
	if len(dirty) > maxDaily {
		historyLog.Warn("除权检测异常偏多(%d只>上限%d),疑似快照口径问题,只刷新前%d只", len(dirty), maxDaily, maxDaily)
		dirty = dirty[:maxDaily]
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	refreshed := 0
	var mu sync.Mutex
	for _, code := range dirty {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if _, _, err := s.RebuildStockQfq(code, 420); err == nil {
				mu.Lock()
				refreshed++
				mu.Unlock()
			} else {
				historyLog.Warn("除权刷新失败 %s: %v", code, err)
			}
		}(code)
	}
	wg.Wait()
	historyLog.Info("除权检测: 发现%d只, 前复权刷新成功%d只 (基准日%s)", len(dirty), refreshed, prevDate)
	return refreshed
}
