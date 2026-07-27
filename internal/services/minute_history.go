package services

// 历史分时回补(2026-07-21 建):自采集分时(minute_ticks)只有 2026-07-06 起,更早的历史分时
// 三源实测只有**通达信 GetHistoryMinute(date, code)** 能给(≥2022年,每交易日240条分钟级价/量;
// 东财1分钟仅当天、5分钟仅~1.5个月;腾讯 day/query 无视历史日期只回近5天)。
// 表 minute_history 独立于 minute_ticks(口径不同:这里是TDX分钟线,那边是15s快照),按需回补:
// 默认每交易日只补"当日涨幅榜前N"(尾盘3等策略复盘的原料),不做全市场爆破(5000×N天请求量没必要)。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/injoyai/tdx"
)

type MinuteHistoryPlanDay struct {
	Date  string   // 2026-05-06
	Codes []string // 该日要补的股票
}

type MinuteHistoryBackfillStatus struct {
	Running     bool   `json:"running"`
	StartAt     string `json:"startAt"`
	CurrentDate string `json:"currentDate"`
	DoneStocks  int    `json:"doneStocks"`
	TotalStocks int    `json:"totalStocks"`
	Inserted    int    `json:"inserted"`
	Skipped     int    `json:"skipped"` // 已存在跳过(断点续跑)
	Failed      int    `json:"failed"`
	LastError   string `json:"lastError,omitempty"`
	FinishedAt  string `json:"finishedAt,omitempty"`
}

var (
	mhMu     sync.Mutex
	mhStatus MinuteHistoryBackfillStatus
)

func (s *IntradayService) ensureMinuteHistorySchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS minute_history (
		stock_code TEXT NOT NULL,
		trade_date TEXT NOT NULL,
		minute     TEXT NOT NULL, -- HH:MM
		price      REAL,
		volume     INTEGER,       -- TDX 分钟成交量(该分钟增量,单位=手,与K线口径一致)
		PRIMARY KEY(stock_code, trade_date, minute)
	) WITHOUT ROWID`)
	return err
}

// GetMinuteHistoryBackfillStatus 查询回补进度。
func (s *IntradayService) GetMinuteHistoryBackfillStatus() MinuteHistoryBackfillStatus {
	mhMu.Lock()
	defer mhMu.Unlock()
	return mhStatus
}

// BackfillMinuteHistory 按计划回补历史分时(后台调用方自行 goroutine)。单飞:进行中直接拒绝。
// 断点续跑:该(股,日)已有记录则跳过。TDX 独立连接,节流防打爆公共服务器。
func (s *IntradayService) BackfillMinuteHistory(plan []MinuteHistoryPlanDay) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("intraday db 未就绪")
	}
	mhMu.Lock()
	if mhStatus.Running {
		mhMu.Unlock()
		return fmt.Errorf("已有一轮历史分时回补在进行(%s),请等它结束", mhStatus.CurrentDate)
	}
	total := 0
	for _, d := range plan {
		total += len(d.Codes)
	}
	mhStatus = MinuteHistoryBackfillStatus{Running: true, StartAt: time.Now().Format("2006-01-02 15:04:05"), TotalStocks: total}
	mhMu.Unlock()

	defer func() {
		mhMu.Lock()
		mhStatus.Running = false
		mhStatus.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		mhMu.Unlock()
	}()

	if err := s.ensureMinuteHistorySchema(); err != nil {
		mhMu.Lock()
		mhStatus.LastError = err.Error()
		mhMu.Unlock()
		return err
	}
	cli, err := tdx.DialDefault(tdx.WithRedial())
	if err != nil {
		mhMu.Lock()
		mhStatus.LastError = "TDX 连接失败: " + err.Error()
		mhMu.Unlock()
		return err
	}
	defer cli.Close()

	for _, day := range plan {
		dateCompact := strings.ReplaceAll(day.Date, "-", "")
		mhMu.Lock()
		mhStatus.CurrentDate = day.Date
		mhMu.Unlock()
		for _, code := range day.Codes {
			code = strings.ToLower(strings.TrimSpace(code))
			// 断点续跑:已有该股该日数据则跳过
			var one int
			_ = s.db.QueryRow(`SELECT 1 FROM minute_history WHERE stock_code=? AND trade_date=? LIMIT 1`, code, day.Date).Scan(&one)
			if one == 1 {
				mhMu.Lock()
				mhStatus.DoneStocks++
				mhStatus.Skipped++
				mhMu.Unlock()
				continue
			}
			resp, gerr := cli.GetHistoryMinute(dateCompact, code)
			if gerr != nil || resp == nil || len(resp.List) == 0 {
				mhMu.Lock()
				mhStatus.DoneStocks++
				mhStatus.Failed++
				if gerr != nil {
					mhStatus.LastError = fmt.Sprintf("%s@%s: %v", code, day.Date, gerr)
				}
				mhMu.Unlock()
				time.Sleep(40 * time.Millisecond)
				continue
			}
			tx, terr := s.db.Begin()
			if terr != nil {
				continue
			}
			st, _ := tx.Prepare(`INSERT OR REPLACE INTO minute_history VALUES (?,?,?,?,?)`)
			n := 0
			for _, m := range resp.List {
				mt := strings.TrimSpace(m.Time)
				if mt == "" {
					continue
				}
				if _, e := st.Exec(code, day.Date, mt, m.Price.Float64(), m.Number); e == nil {
					n++
				}
			}
			st.Close()
			_ = tx.Commit()
			mhMu.Lock()
			mhStatus.DoneStocks++
			mhStatus.Inserted += n
			mhMu.Unlock()
			time.Sleep(40 * time.Millisecond) // 节流 ~25 req/s,别打爆公共TDX
		}
	}
	return nil
}

// LoadMinuteHistory 读取某股某日的历史分时(升序)。
func (s *IntradayService) LoadMinuteHistory(code, date string) ([]PriceMinute, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("intraday db 未就绪")
	}
	if err := s.ensureMinuteHistorySchema(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT minute, price, volume FROM minute_history WHERE stock_code=? AND trade_date=? ORDER BY minute`,
		strings.ToLower(strings.TrimSpace(code)), date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceMinute
	for rows.Next() {
		var p PriceMinute
		if rows.Scan(&p.Minute, &p.Price, &p.Volume) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

type PriceMinute struct {
	Minute string  `json:"minute"`
	Price  float64 `json:"price"`
	Volume int64   `json:"volume"`
}

// MinuteHistoryCoverage 覆盖概况(日期数/行数/日期范围)。
func (s *IntradayService) MinuteHistoryCoverage() map[string]any {
	out := map[string]any{}
	if s == nil || s.db == nil {
		return out
	}
	_ = s.ensureMinuteHistorySchema()
	var days, rowsN int
	var minD, maxD string
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT trade_date), COUNT(*), COALESCE(MIN(trade_date),''), COALESCE(MAX(trade_date),'') FROM minute_history`).
		Scan(&days, &rowsN, &minD, &maxD)
	out["days"], out["rows"], out["firstDate"], out["lastDate"] = days, rowsN, minD, maxD
	return out
}
