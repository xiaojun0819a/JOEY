package services

// IntradayService 实时采集集合竞价与全市场分时数据(真实行情源:东财全A快照,腾讯兜底)。
// 背景:任何数据源都不提供历史竞价/分时,想做竞价/分时策略必须自己攒数据集。
// 采集节奏(仅交易日):
//   09:15:00-09:25:59  竞价线:每5s 全A快照 → auction_ticks;≥09:25 的第一笔另存 auction_final(定型)
//   09:30-11:30, 13:00-15:00  分时线:每15s 全A快照 → minute_ticks
//   竞价+盘中          重点池线:每3s 自选+持仓批量报价 → focus_ticks(Level-1 源头≈3s,已到物理极限)
// 数据落独立库 intraday.db,不占热库。全市场保留400天、重点池180天,15:10 自动清理。

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const (
	auctionTickInterval = 5 * time.Second  // 全市场竞价线(单轮快照实测1.6-3.1s)
	minuteTickInterval  = 15 * time.Second // 全市场分时线
	focusTickInterval   = 3 * time.Second  // 重点池(自选+持仓):A股 Level-1 源头≈3s一跳,再快无增量
	focusPoolCap        = 400              // 重点池上限
	intradayKeepDays    = 400              // 全市场线保留天数(15s粒度约100GB/400天,NAS 2.9T 无压力)
	focusKeepDays       = 180              // 重点池3s粒度更密,保留半年
)

var intradayLog = func(format string, args ...interface{}) { log.Info(format, args...) }

type IntradayService struct {
	db            *sql.DB
	marketService *MarketService
	mu            sync.Mutex
	lastAuction   time.Time
	lastMinute    time.Time
	lastFocus     time.Time
	finalDone     map[string]bool // trade_date -> 竞价定型已存
	focusPool     func() []string // 重点池提供者(自选+持仓),由 App 注入
}

// SetFocusPool 注入重点池代码提供者(3秒线只采这些票)。
func (s *IntradayService) SetFocusPool(fn func() []string) {
	s.mu.Lock()
	s.focusPool = fn
	s.mu.Unlock()
}

func NewIntradayService(dataDir string, ms *MarketService) (*IntradayService, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "intraday.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS auction_ticks (
			trade_date TEXT NOT NULL, stock_code TEXT NOT NULL, tick_time TEXT NOT NULL,
			price REAL, pct REAL, volume REAL, amount REAL,
			PRIMARY KEY (trade_date, stock_code, tick_time)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS auction_final (
			trade_date TEXT NOT NULL, stock_code TEXT NOT NULL,
			name TEXT, price REAL, pct REAL, volume REAL, amount REAL,
			volume_ratio REAL, float_mcap REAL,
			PRIMARY KEY (trade_date, stock_code)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS minute_ticks (
			trade_date TEXT NOT NULL, stock_code TEXT NOT NULL, tick_time TEXT NOT NULL,
			price REAL, pct REAL, cum_volume REAL, cum_amount REAL,
			PRIMARY KEY (trade_date, stock_code, tick_time)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS focus_ticks (
			trade_date TEXT NOT NULL, stock_code TEXT NOT NULL, tick_time TEXT NOT NULL,
			price REAL, pct REAL, cum_volume REAL, cum_amount REAL,
			PRIMARY KEY (trade_date, stock_code, tick_time)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_auction_ticks_dt ON auction_ticks(trade_date, tick_time)`,
		`CREATE INDEX IF NOT EXISTS idx_minute_ticks_dt ON minute_ticks(trade_date, tick_time)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("intraday schema: %w", err)
		}
	}
	return &IntradayService{db: db, marketService: ms, finalDone: map[string]bool{}}, nil
}

func (s *IntradayService) Close() {
	if s != nil && s.db != nil {
		_ = s.db.Close()
	}
}

// Start 启动采集调度(15s 心跳,按时段分发)。仅应在权威后端(NAS headless/本地全量)调用。
func (s *IntradayService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Second) // 1s 心跳,各线按自己的间隔判 due
		defer ticker.Stop()
		cleaned := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().In(time.FixedZone("CST", 8*3600))
				if !s.marketService.IsTradingDay(now) {
					continue
				}
				hm := now.Format("15:04:05")
				today := now.Format("2006-01-02")
				inAuction := hm >= "09:15:00" && hm < "09:26:00"
				inTrading := (hm >= "09:30:00" && hm < "11:31:00") || (hm >= "13:00:00" && hm < "15:01:00")

				// 重点池 3s 线(竞价+盘中都采)
				if inAuction || inTrading {
					s.mu.Lock()
					dueF := time.Since(s.lastFocus) >= focusTickInterval-200*time.Millisecond
					if dueF {
						s.lastFocus = time.Now()
					}
					s.mu.Unlock()
					if dueF {
						go s.collectFocus(today, now.Format("15:04:05"))
					}
				}
				switch {
				case inAuction:
					s.mu.Lock()
					due := time.Since(s.lastAuction) >= auctionTickInterval-200*time.Millisecond
					if due {
						s.lastAuction = time.Now()
					}
					s.mu.Unlock()
					if due {
						go s.collectAuction(today, now.Format("15:04:05"))
					}
				case inTrading:
					s.mu.Lock()
					due := time.Since(s.lastMinute) >= minuteTickInterval-200*time.Millisecond
					if due {
						s.lastMinute = time.Now()
					}
					s.mu.Unlock()
					if due {
						go s.collectMinute(today, now.Format("15:04:05"))
					}
				case hm >= "15:10:00" && hm < "15:20:00" && cleaned != today:
					cleaned = today
					s.cleanup(now)
				}
			}
		}
	}()
	intradayLog("竞价/分时采集器已启动(竞价 %v/分时 %v/重点池 %v)", auctionTickInterval, minuteTickInterval, focusTickInterval)
}

func (s *IntradayService) snapshot() ([]ScanSnapshotRow, error) {
	return s.marketService.GetAllAStockSnapshot(false)
}

func (s *IntradayService) collectAuction(date, tickTime string) {
	rows, err := s.snapshot()
	if err != nil || len(rows) == 0 {
		intradayLog("竞价快照失败 %s %s: %v", date, tickTime, err)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	st, _ := tx.Prepare(`INSERT OR REPLACE INTO auction_ticks VALUES (?,?,?,?,?,?,?)`)
	n := 0
	for _, r := range rows {
		if r.Price <= 0 {
			continue
		}
		if _, err := st.Exec(date, r.Symbol, tickTime, r.Price, r.ChangePercent, r.Volume, r.Amount); err == nil {
			n++
		}
	}
	st.Close()
	// ≥09:25 的第一笔存定型表
	if tickTime >= "09:25:00" && !s.finalDone[date] {
		fs, _ := tx.Prepare(`INSERT OR REPLACE INTO auction_final VALUES (?,?,?,?,?,?,?,?,?)`)
		for _, r := range rows {
			if r.Price <= 0 {
				continue
			}
			_, _ = fs.Exec(date, r.Symbol, r.Name, r.Price, r.ChangePercent, r.Volume, r.Amount, r.VolumeRatio, r.FloatMarketCap)
		}
		fs.Close()
		s.finalDone[date] = true
		intradayLog("竞价定型快照已存 %s %s: %d 只", date, tickTime, n)
	}
	if err := tx.Commit(); err != nil {
		intradayLog("竞价落库失败: %v", err)
		return
	}
	if tickTime < "09:25:00" {
		intradayLog("竞价快照 %s %s: %d 只", date, tickTime, n)
	}
}

func (s *IntradayService) collectMinute(date, hhmm string) {
	rows, err := s.snapshot()
	if err != nil || len(rows) == 0 {
		intradayLog("分时快照失败 %s %s: %v", date, hhmm, err)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	st, _ := tx.Prepare(`INSERT OR REPLACE INTO minute_ticks VALUES (?,?,?,?,?,?,?)`)
	n := 0
	for _, r := range rows {
		if r.Price <= 0 {
			continue
		}
		if _, err := st.Exec(date, r.Symbol, hhmm, r.Price, r.ChangePercent, r.Volume, r.Amount); err == nil {
			n++
		}
	}
	st.Close()
	if err := tx.Commit(); err != nil {
		intradayLog("分时落库失败: %v", err)
	}
	_ = n
}

// collectFocus 重点池 3s 线:自选+持仓的实时批量报价(腾讯批量,60只/请求,0.06s级)。
func (s *IntradayService) collectFocus(date, tickTime string) {
	s.mu.Lock()
	provider := s.focusPool
	s.mu.Unlock()
	if provider == nil {
		return
	}
	codes := provider()
	if len(codes) == 0 {
		return
	}
	if len(codes) > focusPoolCap {
		codes = codes[:focusPoolCap]
	}
	stocks, err := s.marketService.GetStockRealTimeData(codes...)
	if err != nil || len(stocks) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	st, _ := tx.Prepare(`INSERT OR REPLACE INTO focus_ticks VALUES (?,?,?,?,?,?,?)`)
	for _, r := range stocks {
		if r.Price <= 0 {
			continue
		}
		_, _ = st.Exec(date, r.Symbol, tickTime, r.Price, r.ChangePercent, float64(r.Volume), r.Amount)
	}
	st.Close()
	_ = tx.Commit()
}

func (s *IntradayService) cleanup(now time.Time) {
	cut := now.AddDate(0, 0, -intradayKeepDays).Format("2006-01-02")
	for _, tbl := range []string{"auction_ticks", "auction_final", "minute_ticks"} {
		if res, err := s.db.Exec("DELETE FROM "+tbl+" WHERE trade_date < ?", cut); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				intradayLog("清理 %s %d 行(< %s)", tbl, n, cut)
			}
		}
	}
	focusCut := now.AddDate(0, 0, -focusKeepDays).Format("2006-01-02")
	if res, err := s.db.Exec("DELETE FROM focus_ticks WHERE trade_date < ?", focusCut); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			intradayLog("清理 focus_ticks %d 行(< %s)", n, focusCut)
		}
	}
}

// ---------- 查询 ----------

// AuctionFinalRow 竞价定型记录
type AuctionFinalRow struct {
	StockCode   string  `json:"stockCode"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Pct         float64 `json:"pct"`
	Volume      float64 `json:"volume"`
	Amount      float64 `json:"amount"`
	VolumeRatio float64 `json:"volumeRatio"`
	FloatMcap   float64 `json:"floatMcap"`
}

// AuctionFinal 某日竞价定型全表(按竞价金额降序,limit<=0 取500)。
func (s *IntradayService) AuctionFinal(date string, limit int) ([]AuctionFinalRow, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT stock_code,name,price,pct,volume,amount,volume_ratio,float_mcap
		FROM auction_final WHERE trade_date=? ORDER BY amount DESC LIMIT ?`, date, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuctionFinalRow
	for rows.Next() {
		var r AuctionFinalRow
		if rows.Scan(&r.StockCode, &r.Name, &r.Price, &r.Pct, &r.Volume, &r.Amount, &r.VolumeRatio, &r.FloatMcap) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// IntradayTick 分时点
type IntradayTick struct {
	Time   string  `json:"time"`
	Price  float64 `json:"price"`
	Pct    float64 `json:"pct"`
	Volume float64 `json:"volume"`
	Amount float64 `json:"amount"`
}

// StockIntraday 某股某日分时序列(含竞价段)。
func (s *IntradayService) StockIntraday(code, date string) (auction []IntradayTick, minutes []IntradayTick, err error) {
	q1, err := s.db.Query(`SELECT tick_time,price,pct,volume,amount FROM auction_ticks
		WHERE trade_date=? AND stock_code=? ORDER BY tick_time`, date, code)
	if err != nil {
		return nil, nil, err
	}
	defer q1.Close()
	for q1.Next() {
		var t IntradayTick
		if q1.Scan(&t.Time, &t.Price, &t.Pct, &t.Volume, &t.Amount) == nil {
			auction = append(auction, t)
		}
	}
	q2, err := s.db.Query(`SELECT tick_time,price,pct,cum_volume,cum_amount FROM minute_ticks
		WHERE trade_date=? AND stock_code=? ORDER BY tick_time`, date, code)
	if err != nil {
		return auction, nil, err
	}
	defer q2.Close()
	for q2.Next() {
		var t IntradayTick
		if q2.Scan(&t.Time, &t.Price, &t.Pct, &t.Volume, &t.Amount) == nil {
			minutes = append(minutes, t)
		}
	}
	return auction, minutes, nil
}

// StockFocusTicks 某股某日重点池 3s 线(仅自选/持仓股有)。
func (s *IntradayService) StockFocusTicks(code, date string) ([]IntradayTick, error) {
	q, err := s.db.Query(`SELECT tick_time,price,pct,cum_volume,cum_amount FROM focus_ticks
		WHERE trade_date=? AND stock_code=? ORDER BY tick_time`, date, code)
	if err != nil {
		return nil, err
	}
	defer q.Close()
	var out []IntradayTick
	for q.Next() {
		var t IntradayTick
		if q.Scan(&t.Time, &t.Price, &t.Pct, &t.Volume, &t.Amount) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// IntradayCoverage 采集覆盖统计
type IntradayCoverage struct {
	AuctionDays  int    `json:"auctionDays"`
	MinuteDays   int    `json:"minuteDays"`
	AuctionRows  int64  `json:"auctionRows"`
	FinalRows    int64  `json:"finalRows"`
	MinuteRows   int64  `json:"minuteRows"`
	FocusRows    int64  `json:"focusRows"`
	FirstDate    string `json:"firstDate"`
	LastDate     string `json:"lastDate"`
	LastTickTime string `json:"lastTickTime"`
}

func (s *IntradayService) Coverage() IntradayCoverage {
	var c IntradayCoverage
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT trade_date), COUNT(*) FROM auction_ticks`).Scan(&c.AuctionDays, &c.AuctionRows)
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT trade_date), COUNT(*) FROM minute_ticks`).Scan(&c.MinuteDays, &c.MinuteRows)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM auction_final`).Scan(&c.FinalRows)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM focus_ticks`).Scan(&c.FocusRows)
	_ = s.db.QueryRow(`SELECT COALESCE(MIN(trade_date),''), COALESCE(MAX(trade_date),'') FROM (
		SELECT trade_date FROM auction_ticks UNION SELECT trade_date FROM minute_ticks)`).Scan(&c.FirstDate, &c.LastDate)
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(tick_time),'') FROM minute_ticks WHERE trade_date=(SELECT MAX(trade_date) FROM minute_ticks)`).Scan(&c.LastTickTime)
	return c
}
