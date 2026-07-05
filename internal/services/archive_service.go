package services

// ArchiveService 全量历史行情档案(archive.db,独立于热库 history.db)。
// 数据源:全A 1991-2025 逐日全列 CSV(OHLC/换手/量比/PE/PB/PS/股息率/股本/市值/复权因子/涨跌停)。
// 表 stock_history,PK(code,trade_date),code 为 app 口径(sz000002/sh600519/bj920703)。

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/glebarez/go-sqlite"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
)

type ArchiveService struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

func NewArchiveService() *ArchiveService {
	return &ArchiveService{path: filepath.Join(paths.GetDataDir(), "archive.db")}
}

// open 惰性打开(只读场景)。库文件不存在返回明确错误,不影响应用其它功能。
func (s *ArchiveService) open() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	if _, err := os.Stat(s.path); err != nil {
		return nil, fmt.Errorf("历史档案库不存在: %s", s.path)
	}
	db, err := sql.Open("sqlite", s.path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	s.db = db
	return db, nil
}

// Available 档案库是否就绪。
func (s *ArchiveService) Available() bool {
	_, err := s.open()
	return err == nil
}

// ArchiveBar 全列日线记录
type ArchiveBar struct {
	Code         string  `json:"code"`
	TradeDate    string  `json:"tradeDate"`
	Name         string  `json:"name"`
	Open         float64 `json:"open"`
	High         float64 `json:"high"`
	Low          float64 `json:"low"`
	Close        float64 `json:"close"`
	PrevClose    float64 `json:"prevClose"`
	PctChg       float64 `json:"pctChg"`
	Volume       float64 `json:"volume"` // 手
	Amount       float64 `json:"amount"` // 千元
	Turnover     float64 `json:"turnover"`
	VolumeRatio  float64 `json:"volumeRatio"`
	PE           float64 `json:"pe"`
	PETTM        float64 `json:"peTtm"`
	PB           float64 `json:"pb"`
	PS           float64 `json:"ps"`
	DivYield     float64 `json:"divYield"`
	TotalMcap    float64 `json:"totalMcap"` // 万元
	FloatMcap    float64 `json:"floatMcap"` // 万元
	AdjFactor    float64 `json:"adjFactor"`
	LimitUp      float64 `json:"limitUp"`
	LimitDown    float64 `json:"limitDown"`
}

func nvf(v sql.NullFloat64) float64 {
	if v.Valid {
		return v.Float64
	}
	return 0
}

// Bars 按区间取全列记录(升序)。start/end 形如 2020-01-01,可空;limit<=0 不限。
func (s *ArchiveService) Bars(code, start, end string, limit int) ([]ArchiveBar, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return nil, errors.New("code 不能为空")
	}
	q := `SELECT code,trade_date,name,open,high,low,close,prev_close,pct_chg,volume,amount,
		turnover,volume_ratio,pe,pe_ttm,pb,ps,div_yield,total_mcap,float_mcap,adj_factor,limit_up,limit_down
		FROM stock_history WHERE code=?`
	args := []interface{}{code}
	if start != "" {
		q += " AND trade_date>=?"
		args = append(args, start)
	}
	if end != "" {
		q += " AND trade_date<=?"
		args = append(args, end)
	}
	q += " ORDER BY trade_date ASC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveBar
	for rows.Next() {
		var b ArchiveBar
		var name sql.NullString
		var open, high, low, close_, prev, pct, vol, amt, to, vr, pe, pettm, pb, ps, dy, tm, fm, af, lu, ld sql.NullFloat64
		if err := rows.Scan(&b.Code, &b.TradeDate, &name, &open, &high, &low, &close_, &prev, &pct, &vol, &amt,
			&to, &vr, &pe, &pettm, &pb, &ps, &dy, &tm, &fm, &af, &lu, &ld); err != nil {
			continue
		}
		b.Name = name.String
		b.Open, b.High, b.Low, b.Close, b.PrevClose = nvf(open), nvf(high), nvf(low), nvf(close_), nvf(prev)
		b.PctChg, b.Volume, b.Amount, b.Turnover, b.VolumeRatio = nvf(pct), nvf(vol), nvf(amt), nvf(to), nvf(vr)
		b.PE, b.PETTM, b.PB, b.PS, b.DivYield = nvf(pe), nvf(pettm), nvf(pb), nvf(ps), nvf(dy)
		b.TotalMcap, b.FloatMcap, b.AdjFactor, b.LimitUp, b.LimitDown = nvf(tm), nvf(fm), nvf(af), nvf(lu), nvf(ld)
		out = append(out, b)
	}
	return out, nil
}

// KLine 返回前复权日K(升序)。qfq = 原价 × 复权因子 ÷ 档案内最新复权因子。
// days>0 时取最近 days 根(在 start/end 过滤之后)。
func (s *ArchiveService) KLine(code, start, end string, days int) ([]models.KLineData, error) {
	bars, err := s.Bars(code, start, end, 0)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, nil
	}
	if days > 0 && len(bars) > days {
		bars = bars[len(bars)-days:]
	}
	// 最新复权因子取该股档案里最后一根(不受区间过滤影响,单查一次)
	db, _ := s.open()
	var latestFactor float64 = 1
	_ = db.QueryRow(`SELECT adj_factor FROM stock_history WHERE code=? ORDER BY trade_date DESC LIMIT 1`, strings.ToLower(code)).Scan(&latestFactor)
	if latestFactor <= 0 {
		latestFactor = 1
	}
	out := make([]models.KLineData, 0, len(bars))
	for _, b := range bars {
		if b.Close <= 0 { // 停牌/无效行(CSV 价格为空)不进K线
			continue
		}
		f := b.AdjFactor
		if f <= 0 {
			f = latestFactor // 缺因子按不复权
		}
		r := f / latestFactor
		out = append(out, models.KLineData{
			Time:   b.TradeDate,
			Open:   round3(b.Open * r),
			High:   round3(b.High * r),
			Low:    round3(b.Low * r),
			Close:  round3(b.Close * r),
			Volume: int64(b.Volume),
		})
	}
	return out, nil
}

func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}

// StockInfo 单只覆盖情况
type ArchiveStockInfo struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	FirstDate string `json:"firstDate"`
	LastDate  string `json:"lastDate"`
	Rows      int    `json:"rows"`
}

func (s *ArchiveService) StockInfo(code string) (*ArchiveStockInfo, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	info := &ArchiveStockInfo{Code: strings.ToLower(strings.TrimSpace(code))}
	err = db.QueryRow(`SELECT MIN(trade_date), MAX(trade_date), COUNT(*),
		(SELECT name FROM stock_history WHERE code=?1 ORDER BY trade_date DESC LIMIT 1)
		FROM stock_history WHERE code=?1`, info.Code).
		Scan(&info.FirstDate, &info.LastDate, &info.Rows, &info.Name)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Coverage 全库覆盖统计
type ArchiveCoverage struct {
	Available bool   `json:"available"`
	Stocks    int    `json:"stocks"`
	Rows      int64  `json:"rows"`
	MinDate   string `json:"minDate"`
	MaxDate   string `json:"maxDate"`
	Path      string `json:"path"`
	SizeMB    int64  `json:"sizeMb"`
}

func (s *ArchiveService) Coverage() ArchiveCoverage {
	cov := ArchiveCoverage{Path: s.path}
	db, err := s.open()
	if err != nil {
		return cov
	}
	cov.Available = true
	_ = db.QueryRow(`SELECT COUNT(DISTINCT code), COUNT(*), MIN(trade_date), MAX(trade_date) FROM stock_history`).
		Scan(&cov.Stocks, &cov.Rows, &cov.MinDate, &cov.MaxDate)
	if st, err := os.Stat(s.path); err == nil {
		cov.SizeMB = st.Size() / 1048576
	}
	return cov
}
