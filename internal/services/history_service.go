package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"

	_ "github.com/glebarez/go-sqlite"
)

var historyLog = logger.New("history")

// HistoryService stores daily market snapshots for backtesting.
type HistoryService struct {
	db            *sql.DB
	marketService *MarketService
	configService *ConfigService
	dbPath        string
	autoCancel    context.CancelFunc
	autoMu        sync.Mutex
	autoRunning   bool
	lastGapCheck  string // 最近一次缺口自愈检查的日期(YYYY-MM-DD)，每天最多跑一次全量回补
}

// NewHistoryService creates the local history store under the app data dir.
func NewHistoryService(dataDir string, marketService *MarketService, configService *ConfigService) (*HistoryService, error) {
	if marketService == nil {
		return nil, errors.New("market service is nil")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "history.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	svc := &HistoryService{
		db:            db,
		marketService: marketService,
		configService: configService,
		dbPath:        dbPath,
	}
	if err := svc.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return svc, nil
}

func (s *HistoryService) Close() {
	if s == nil || s.db == nil {
		return
	}
	s.StopAutoCollect()
	s.db.Close()
}

func (s *HistoryService) DBPath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}

// ResolveTradeDateAtOrBefore returns the latest collected trade date not after date.
func (s *HistoryService) ResolveTradeDateAtOrBefore(date string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("history db not ready")
	}
	date = strings.TrimSpace(date)
	if date == "" {
		return "", errors.New("history date is empty")
	}
	var asOf string
	if err := s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily WHERE trade_date <= ?`, date).Scan(&asOf); err != nil {
		return "", err
	}
	if asOf == "" {
		return "", fmt.Errorf("no history data before %s", date)
	}
	return asOf, nil
}

// LoadScanRowsOnDate loads one market snapshot from local stock_daily for replay-style scanners.
func (s *HistoryService) LoadScanRowsOnDate(date string, includeBeijing bool) ([]ScanSnapshotRow, string, error) {
	asOf, err := s.ResolveTradeDateAtOrBefore(date)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.db.Query(`SELECT stock_code, stock_name, industry, close_price, pct_change, amount, turnover,
			main_net, main_pct, main_source, total_market_cap, float_market_cap
		FROM stock_daily
		WHERE trade_date=? AND close_price>0 AND amount>0
		ORDER BY amount DESC`, asOf)
	if err != nil {
		return nil, asOf, err
	}
	defer rows.Close()

	out := make([]ScanSnapshotRow, 0, 6000)
	for rows.Next() {
		var row ScanSnapshotRow
		var industry, source string
		if err := rows.Scan(
			&row.Symbol, &row.Name, &industry, &row.Price, &row.ChangePercent, &row.Amount, &row.TurnoverRate,
			&row.MainNetInflow, &row.MainNetInflowRatio, &source, &row.TotalMarketCap, &row.FloatMarketCap,
		); err != nil {
			return nil, asOf, err
		}
		if row.Symbol == "" {
			continue
		}
		if !includeBeijing && strings.HasPrefix(row.Symbol, "bj") {
			continue
		}
		row.Industry = industry
		row.MainFlowSource = chooseText(source, "history")
		row.UpdateTime = asOf
		if strings.Contains(strings.ToUpper(row.Name), "ST") {
			row.IsST = true
		}
		out = append(out, row)
	}
	return out, asOf, rows.Err()
}

// LoadScanRowsOnDateForReplay 历史回放专用：只要 close>0 即纳入（不依赖成交额），
// 成交额缺失时用 volume×close 近似（历史库 amount 仅近期有值，但 volume/换手/流通市值全程有值）。
func (s *HistoryService) LoadScanRowsOnDateForReplay(date string, includeBeijing bool) ([]ScanSnapshotRow, string, error) {
	asOf, err := s.ResolveTradeDateAtOrBefore(date)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.db.Query(`SELECT stock_code, stock_name, industry, close_price, pct_change, amount, turnover,
			main_net, main_pct, main_source, total_market_cap, float_market_cap, volume
		FROM stock_daily
		WHERE trade_date=? AND close_price>0`, asOf)
	if err != nil {
		return nil, asOf, err
	}
	defer rows.Close()

	out := make([]ScanSnapshotRow, 0, 6000)
	for rows.Next() {
		var row ScanSnapshotRow
		// 历史回填行可能多列为 NULL，统一用可空类型接收，避免 Scan 报错
		var symbol, name, industry, source sql.NullString
		var price, pct, amount, turnover, mainNet, mainPct, totalCap, floatCap, volume sql.NullFloat64
		if err := rows.Scan(
			&symbol, &name, &industry, &price, &pct, &amount, &turnover,
			&mainNet, &mainPct, &source, &totalCap, &floatCap, &volume,
		); err != nil {
			return nil, asOf, err
		}
		row.Symbol = symbol.String
		row.Name = name.String
		if row.Symbol == "" {
			continue
		}
		if !includeBeijing && strings.HasPrefix(row.Symbol, "bj") {
			continue
		}
		row.Price = price.Float64
		row.ChangePercent = pct.Float64
		row.Amount = amount.Float64
		row.TurnoverRate = turnover.Float64
		row.MainNetInflow = mainNet.Float64
		row.MainNetInflowRatio = mainPct.Float64
		row.TotalMarketCap = totalCap.Float64
		row.FloatMarketCap = floatCap.Float64
		if row.Amount <= 0 && volume.Float64 > 0 && row.Price > 0 {
			row.Amount = volume.Float64 * row.Price // 近似成交额(元)
		}
		row.Industry = industry.String
		row.MainFlowSource = chooseText(source.String, "history")
		row.UpdateTime = asOf
		if strings.Contains(strings.ToUpper(row.Name), "ST") {
			row.IsST = true
		}
		out = append(out, row)
	}
	return out, asOf, rows.Err()
}

// LoadKLineDataUntil returns daily K lines ending at asOf, with no future rows.
func (s *HistoryService) LoadKLineDataUntil(code string, asOf string, limit int) ([]models.KLineData, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("history db not ready")
	}
	if limit <= 0 {
		limit = 320
	}
	rows, err := s.db.Query(`SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, ma5, ma10, ma20
		FROM stock_daily
		WHERE stock_code=? AND trade_date<=? AND close_price>0
		ORDER BY trade_date DESC
		LIMIT ?`, code, asOf, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bars := make([]models.KLineData, 0, limit)
	for rows.Next() {
		var k models.KLineData
		var volume float64
		if err := rows.Scan(&k.Time, &k.Open, &k.High, &k.Low, &k.Close, &volume, &k.Amount, &k.MA5, &k.MA10, &k.MA20); err != nil {
			return nil, err
		}
		if k.Open <= 0 {
			k.Open = k.Close
		}
		if k.High <= 0 {
			k.High = math.Max(k.Open, k.Close)
		}
		if k.Low <= 0 {
			k.Low = math.Min(k.Open, k.Close)
		}
		k.Volume = int64(volume)
		bars = append(bars, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(bars)-1; i < j; i, j = i+1, j-1 {
		bars[i], bars[j] = bars[j], bars[i]
	}
	return bars, nil
}

// RecentTradeDates 返回最近 n 个交易日（升序），供 main 包驱动公式型策略账户。
func (s *HistoryService) RecentTradeDates(n int) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("history db not ready")
	}
	return s.recentTradeDates(n)
}

// LoadAllKLinesAscending 一次性载入区间内全部股票的日K（升序），返回 code->bars 与 code->date->下标索引。
// 供公式型策略账户在内存中逐日回看，避免逐股逐日重复查库。
func (s *HistoryService) LoadAllKLinesAscending(start, end string) (map[string][]models.KLineData, map[string]map[string]int, error) {
	if s == nil || s.db == nil {
		return nil, nil, errors.New("history db not ready")
	}
	rows, err := s.db.Query(`SELECT stock_code, trade_date, open_price, high_price, low_price, close_price, volume, amount, ma5, ma10, ma20
		FROM stock_daily
		WHERE trade_date>=? AND trade_date<=? AND close_price>0
		ORDER BY stock_code, trade_date`, start, end)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	bars := make(map[string][]models.KLineData, 5000)
	idx := make(map[string]map[string]int, 5000)
	for rows.Next() {
		var code string
		var k models.KLineData
		var open, high, low, close, volume, amount, ma5, ma10, ma20 sql.NullFloat64
		if err := rows.Scan(&code, &k.Time, &open, &high, &low, &close, &volume, &amount, &ma5, &ma10, &ma20); err != nil {
			return nil, nil, err
		}
		if code == "" || !close.Valid || close.Float64 <= 0 {
			continue
		}
		k.Close = close.Float64
		k.Open = open.Float64
		k.High = high.Float64
		k.Low = low.Float64
		k.Amount = amount.Float64
		k.MA5, k.MA10, k.MA20 = ma5.Float64, ma10.Float64, ma20.Float64
		if k.Open <= 0 {
			k.Open = k.Close
		}
		if k.High <= 0 {
			k.High = math.Max(k.Open, k.Close)
		}
		if k.Low <= 0 {
			k.Low = math.Min(k.Open, k.Close)
		}
		k.Volume = int64(volume.Float64)
		bars[code] = append(bars[code], k)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for code, bs := range bars {
		m := make(map[string]int, len(bs))
		for i, b := range bs {
			m[b.Time] = i
		}
		idx[code] = m
	}
	return bars, idx, nil
}

// EqualWeightIndexBetween 区间内等权全A指数（每日全市场 pct_change 均值累乘），用作实盘跟踪账户的同期基准。
// 返回 date->指数水平(起点=1.0) 与升序日期列表。
func (s *HistoryService) EqualWeightIndexBetween(start, end string) (map[string]float64, []string, error) {
	if s == nil || s.db == nil {
		return nil, nil, errors.New("history db not ready")
	}
	rows, err := s.db.Query(`SELECT trade_date, AVG(pct_change) FROM stock_daily
		WHERE trade_date>=? AND trade_date<=? AND pct_change IS NOT NULL
		GROUP BY trade_date ORDER BY trade_date`, start, end)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	level := make(map[string]float64, 300)
	dates := make([]string, 0, 300)
	cur := 1.0
	for rows.Next() {
		var d string
		var avg sql.NullFloat64
		if err := rows.Scan(&d, &avg); err != nil {
			return nil, nil, err
		}
		cur *= 1 + avg.Float64/100
		level[d] = cur
		dates = append(dates, d)
	}
	return level, dates, rows.Err()
}

func (s *HistoryService) StartAutoCollect(ctx context.Context) {
	if s == nil || s.configService == nil {
		return
	}
	s.autoMu.Lock()
	if s.autoCancel != nil {
		s.autoMu.Unlock()
		return
	}
	autoCtx, cancel := context.WithCancel(ctx)
	s.autoCancel = cancel
	s.autoMu.Unlock()

	go s.autoCollectLoop(autoCtx)
}

func (s *HistoryService) StopAutoCollect() {
	if s == nil {
		return
	}
	s.autoMu.Lock()
	cancel := s.autoCancel
	s.autoCancel = nil
	s.autoMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *HistoryService) GetAutoCollectStatus() models.HistoryAutoCollectStatus {
	cfg := s.getHistoryConfig()
	return models.HistoryAutoCollectStatus{
		Enabled:         cfg.AutoCollectDaily,
		CollectStart:    cfg.CollectStart,
		CollectEnd:      cfg.CollectEnd,
		IncludeBeijing:  cfg.IncludeBeijing,
		LastCollectDate: cfg.LastCollectDate,
		DBPath:          s.DBPath(),
		Message:         historyAutoCollectMessage(cfg),
	}
}

func (s *HistoryService) UpdateAutoCollect(req models.HistoryAutoCollectRequest) (models.HistoryAutoCollectStatus, error) {
	if s == nil || s.configService == nil {
		return models.HistoryAutoCollectStatus{}, errors.New("history config service not ready")
	}
	cfg := s.configService.GetConfig()
	if cfg == nil {
		return models.HistoryAutoCollectStatus{}, errors.New("config not ready")
	}
	start := normalizeClockHHMM(req.CollectStart, "16:00")
	end := normalizeClockHHMM(req.CollectEnd, "17:00")
	cfg.History.AutoCollectDaily = req.Enabled
	cfg.History.CollectStart = start
	cfg.History.CollectEnd = end
	cfg.History.IncludeBeijing = req.IncludeBeijing
	if err := s.configService.UpdateConfig(cfg); err != nil {
		return models.HistoryAutoCollectStatus{}, err
	}
	return s.GetAutoCollectStatus(), nil
}

func (s *HistoryService) initSchema() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS stock_daily (
			stock_code TEXT NOT NULL,
			trade_date TEXT NOT NULL,
			stock_name TEXT,
			industry TEXT,
			close_price REAL,
			turnover REAL,
			main_net REAL,
			main_pct REAL,
			main_source TEXT,
			amount REAL,
			volume REAL,
			pct_change REAL,
			total_market_cap REAL,
			float_market_cap REAL,
			ma5 REAL,
			ma10 REAL,
			ma20 REAL,
			updated_at TEXT,
			PRIMARY KEY (stock_code, trade_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_daily_date ON stock_daily(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_daily_code_date ON stock_daily(stock_code, trade_date)`,
		`CREATE TABLE IF NOT EXISTS history_collect_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trade_date TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			source TEXT,
			total_count INTEGER DEFAULT 0,
			saved_count INTEGER DEFAULT 0,
			failed_count INTEGER DEFAULT 0,
			status TEXT,
			message TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS strategy_scan_picks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id TEXT NOT NULL,
			strategy_name TEXT,
			signal_date TEXT NOT NULL,
			scanned_at TEXT,
			stock_code TEXT NOT NULL,
			stock_name TEXT,
			rank INTEGER,
			price REAL,
			change_pct REAL,
			score REAL,
			industry TEXT,
			amount REAL,
			turnover REAL,
			main_net REAL,
			main_pct REAL,
			main_source TEXT,
			triggers_json TEXT,
			reasons_json TEXT,
			risks_json TEXT,
			created_at TEXT,
			UNIQUE(strategy_id, signal_date, stock_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_strategy_scan_picks_strategy_date ON strategy_scan_picks(strategy_id, signal_date)`,
		`CREATE TABLE IF NOT EXISTS stock_fundamentals (
			stock_code TEXT NOT NULL,
			report_date TEXT NOT NULL,
			stock_name TEXT,
			roe REAL,           -- 加权ROE %
			rev_yoy REAL,       -- 营收同比 %
			profit_yoy REAL,    -- 净利同比 %
			gross_margin REAL,  -- 销售毛利率 %
			cfps REAL,          -- 每股经营现金流
			eps REAL,           -- 基本每股收益
			notice_date TEXT,   -- 公告日
			updated_at TEXT,
			PRIMARY KEY (stock_code, report_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_fundamentals_report ON stock_fundamentals(report_date)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// 迁移：为回测补充开/高/低价列（已存在则忽略）
	for _, col := range []string{"open_price", "high_price", "low_price"} {
		if _, err := s.db.Exec("ALTER TABLE stock_daily ADD COLUMN " + col + " REAL"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				historyLog.Warn("添加列 %s 失败: %v", col, err)
			}
		}
	}
	// 迁移：基本面二期补资产负债率/净资产/商誉占净资产/股息率列
	for _, col := range []string{"debt_ratio", "equity", "goodwill_ratio", "dividend_yield"} {
		if _, err := s.db.Exec("ALTER TABLE stock_fundamentals ADD COLUMN " + col + " REAL"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				historyLog.Warn("添加列 %s 失败: %v", col, err)
			}
		}
	}
	return nil
}

func (s *HistoryService) autoCollectLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	s.tryAutoCollectOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tryAutoCollectOnce()
		}
	}
}

func (s *HistoryService) tryAutoCollectOnce() {
	if s == nil || s.configService == nil || s.marketService == nil {
		return
	}
	cfg := s.getHistoryConfig()
	if !cfg.AutoCollectDaily {
		return
	}
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	today := now.Format("2006-01-02")

	// 串行化：同一时刻只允许一个采集/回补在跑
	s.autoMu.Lock()
	if s.autoRunning {
		s.autoMu.Unlock()
		return
	}
	s.autoRunning = true
	s.autoMu.Unlock()
	defer func() {
		s.autoMu.Lock()
		s.autoRunning = false
		s.autoMu.Unlock()
	}()

	// 1) 当天收盘快照：今天是交易日、已过采集起点(默认16:00，收盘后)、今天还没采。
	//    只保留下界，不再卡 16:00-17:00 上界——NAS 晚点起来也能补上当天。
	startGate := cfg.CollectStart
	if strings.TrimSpace(startGate) == "" {
		startGate = "15:30"
	}
	if s.marketService.IsTradingDay(now) && cfg.LastCollectDate != today && isWithinClockWindow(now, startGate, "23:59") {
		result := s.CollectDailyHistory(models.HistoryCollectRequest{
			TradeDate:       today,
			IncludeBeijing:  cfg.IncludeBeijing,
			TriggeredByAuto: true,
		})
		if result.Status == "success" || result.Status == "partial" {
			if appCfg := s.configService.GetConfig(); appCfg != nil {
				appCfg.History.LastCollectDate = today
				if appCfg.History.CollectStart == "" {
					appCfg.History.CollectStart = cfg.CollectStart
				}
				if appCfg.History.CollectEnd == "" {
					appCfg.History.CollectEnd = cfg.CollectEnd
				}
				if err := s.configService.UpdateConfig(appCfg); err != nil {
					historyLog.Warn("保存自动采集日期失败: %v", err)
				}
			}
			historyLog.Info("每日快照采集完成: %s (%d 只)", today, result.SavedCount)
		} else {
			historyLog.Warn("每日快照采集失败: %s", result.Message)
		}
	}

	// 2) 缺口自愈：每天最多一次，检测最近交易日(不含今天，今天走快照)是否有缺失，
	//    含数据库内部空洞。有则用日K逐只回补(INSERT OR IGNORE，只补缺失日，幂等)。
	if s.lastGapCheck != today {
		s.lastGapCheck = today
		if gapDays, earliest := s.detectRecentGapDays(now); gapDays > 0 {
			historyLog.Info("检测到历史数据缺口(最早缺 %s)，启动日K回补最近 %d 天…", earliest, gapDays)
			res := s.BackfillAllHistory(gapDays, cfg.IncludeBeijing)
			historyLog.Info("缺口回补结束: %s", res.Message)
		}
	}
}

// detectRecentGapDays 检查最近约 40 个自然日内(不含今天)应有的交易日，
// 是否在 stock_daily 里缺失(含内部空洞)。返回需要回补的天数(自最早缺失日到今天+缓冲，上限60)与最早缺失日。
func (s *HistoryService) detectRecentGapDays(now time.Time) (int, string) {
	if s == nil || s.db == nil || s.marketService == nil {
		return 0, ""
	}
	lookbackStart := now.AddDate(0, 0, -40)
	end := now.AddDate(0, 0, -1) // 今天由快照负责，这里只查历史交易日
	if end.Before(lookbackStart) {
		return 0, ""
	}
	// 期望的交易日集合
	expected := make([]string, 0, 32)
	for d := lookbackStart; !d.After(end); d = d.AddDate(0, 0, 1) {
		if s.marketService.IsTradingDay(d) {
			expected = append(expected, d.Format("2006-01-02"))
		}
	}
	if len(expected) == 0 {
		return 0, ""
	}
	// 每个交易日的行数(一次查询)。按"完整性"判断——只有某一天行数达到
	// 该窗口最热闹一天的 70% 才算采齐，避免被回补中断留下的"半拉子日"骗过去。
	counts := make(map[string]int, len(expected))
	rows, err := s.db.Query(
		`SELECT trade_date, COUNT(*) FROM stock_daily WHERE trade_date BETWEEN ? AND ? GROUP BY trade_date`,
		lookbackStart.Format("2006-01-02"), end.Format("2006-01-02"),
	)
	if err != nil {
		return 0, ""
	}
	defer rows.Close()
	maxCount := 0
	for rows.Next() {
		var d string
		var c int
		if rows.Scan(&d, &c) == nil {
			counts[d] = c
			if c > maxCount {
				maxCount = c
			}
		}
	}
	// 若该窗口内一条数据都没有，视为空库/非常规状态，不触发大回补(交给人工冷启动)
	if maxCount == 0 {
		return 0, ""
	}
	threshold := maxCount * 7 / 10
	earliest := ""
	for _, d := range expected {
		if counts[d] < threshold {
			earliest = d
			break
		}
	}
	if earliest == "" {
		return 0, ""
	}
	t, perr := time.ParseInLocation("2006-01-02", earliest, now.Location())
	if perr != nil {
		return 0, ""
	}
	gapDays := int(now.Sub(t).Hours()/24) + 5
	if gapDays > 60 {
		gapDays = 60
	}
	return gapDays, earliest
}

// CollectDailyHistory captures one full-market snapshot and updates MA columns.
func (s *HistoryService) CollectDailyHistory(req models.HistoryCollectRequest) models.HistoryCollectResult {
	start := time.Now()
	tradeDate := strings.TrimSpace(req.TradeDate)
	if tradeDate == "" {
		tradeDate = start.Format("2006-01-02")
	}
	result := models.HistoryCollectResult{
		TradeDate: tradeDate,
		StartedAt: start.Format("2006-01-02 15:04:05"),
		DBPath:    s.DBPath(),
		Status:    "running",
	}
	if s == nil || s.db == nil || s.marketService == nil {
		result.Status = "failed"
		result.Message = "history service not ready"
		return result
	}

	runID := s.insertRun(result.TradeDate, result.StartedAt, "snapshot")
	stocks, err := s.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		s.finishRun(runID, result)
		return result
	}
	result.TotalCount = len(stocks)

	tx, err := s.db.Begin()
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		s.finishRun(runID, result)
		return result
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO stock_daily
		(stock_code, trade_date, stock_name, industry, close_price, turnover, main_net, main_pct, main_source,
		 amount, volume, pct_change, total_market_cap, float_market_cap, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		result.Status = "failed"
		result.Message = err.Error()
		s.finishRun(runID, result)
		return result
	}

	nowText := time.Now().Format("2006-01-02 15:04:05")
	for _, stock := range stocks {
		if stock.Symbol == "" || stock.Price <= 0 {
			result.FailedCount++
			continue
		}
		volumeEstimate := 0.0
		if stock.Price > 0 {
			volumeEstimate = stock.Amount / stock.Price
		}
		_, execErr := stmt.Exec(
			stock.Symbol,
			tradeDate,
			stock.Name,
			stock.Industry,
			stock.Price,
			stock.TurnoverRate,
			nullableFloat(stock.MainNetInflow),
			nullableFloat(stock.MainNetInflowRatio),
			stock.MainFlowSource,
			stock.Amount,
			volumeEstimate,
			stock.ChangePercent,
			stock.TotalMarketCap,
			stock.FloatMarketCap,
			nowText,
		)
		if execErr != nil {
			result.FailedCount++
			continue
		}
		result.SavedCount++
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		s.finishRun(runID, result)
		return result
	}

	if err := s.UpdateMovingAverages(tradeDate); err != nil {
		result.Status = "partial"
		result.Message = "采集已保存，均线回写失败: " + err.Error()
	} else {
		result.MAUpdated = true
		result.Status = "success"
		result.Message = fmt.Sprintf("已采集 %d 条，写入 %d 条", result.TotalCount, result.SavedCount)
	}
	result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	s.finishRun(runID, result)
	return result
}

// BackfillHistory 用日K历史逐只回补 stock_daily，用于冷启动时一次性补足回测所需的历史。
// 与 CollectDailyHistory（全A当日快照）互补：快照只有当天，回补能往前补 N 个交易日。
// 写入用 INSERT OR IGNORE，只填补缺失日期，不覆盖已有的快照行（快照含主力资金流等更全字段）。
func (s *HistoryService) BackfillHistory(req models.HistoryBackfillRequest) models.HistoryBackfillResult {
	start := time.Now()
	result := models.HistoryBackfillResult{
		StartedAt: start.Format("2006-01-02 15:04:05"),
		DBPath:    s.DBPath(),
		Status:    "running",
	}
	if s == nil || s.db == nil || s.marketService == nil {
		result.Status = "failed"
		result.Message = "history service not ready"
		result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		return result
	}

	days := req.Days
	if days <= 0 {
		days = 250
	}
	throttle := time.Duration(req.ThrottleMs) * time.Millisecond
	if throttle <= 0 {
		throttle = 150 * time.Millisecond
	}

	codes := dedupCodes(req.Codes)
	result.TotalCodes = len(codes)
	if len(codes) == 0 {
		result.Status = "failed"
		result.Message = "没有待回补的股票代码"
		result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		return result
	}

	runID := s.insertRun(fmt.Sprintf("backfill x%d", len(codes)), result.StartedAt, "backfill")

	for i, code := range codes {
		klines, err := s.marketService.GetKLineData(code, "1d", days)
		if err != nil || len(klines) == 0 {
			result.FailedCodes++
			if err != nil {
				historyLog.Warn("回补 %s 失败: %v", code, err)
			}
			if i < len(codes)-1 {
				time.Sleep(throttle)
			}
			continue
		}

		saved, earliest, latest, writeErr := s.writeBackfillRows(code, klines)
		if writeErr != nil {
			result.FailedCodes++
			historyLog.Warn("回补 %s 写库失败: %v", code, writeErr)
			if i < len(codes)-1 {
				time.Sleep(throttle)
			}
			continue
		}
		result.OKCodes++
		result.SavedRows += saved
		if earliest != "" && (result.EarliestDate == "" || earliest < result.EarliestDate) {
			result.EarliestDate = earliest
		}
		if latest > result.LatestDate {
			result.LatestDate = latest
		}
		if i < len(codes)-1 {
			time.Sleep(throttle)
		}
	}

	result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	switch {
	case result.OKCodes == 0:
		result.Status = "failed"
		result.Message = fmt.Sprintf("回补全部失败，共 %d 只", result.TotalCodes)
	case result.FailedCodes > 0:
		result.Status = "partial"
		result.Message = fmt.Sprintf("回补 %d/%d 只成功，写入 %d 行，区间 %s ~ %s", result.OKCodes, result.TotalCodes, result.SavedRows, result.EarliestDate, result.LatestDate)
	default:
		result.Status = "success"
		result.Message = fmt.Sprintf("回补 %d 只完成，写入 %d 行，区间 %s ~ %s", result.OKCodes, result.SavedRows, result.EarliestDate, result.LatestDate)
	}

	s.finishRun(runID, models.HistoryCollectResult{
		TradeDate:   result.LatestDate,
		StartedAt:   result.StartedAt,
		FinishedAt:  result.FinishedAt,
		TotalCount:  result.TotalCodes,
		SavedCount:  result.OKCodes,
		FailedCount: result.FailedCodes,
		Status:      result.Status,
		Message:     result.Message,
	})
	return result
}

// CountTradingDaysSince 统计 buyDate(不含)之后到今天(含)的交易日数，用于持仓天数。
// 数据取自已采集的 stock_daily 交易日集合；buyDate 当天买入记为持仓 0 日。
func (s *HistoryService) CountTradingDaysSince(buyDate string) int {
	if s == nil || s.db == nil {
		return 0
	}
	buyDate = strings.TrimSpace(buyDate)
	if buyDate == "" {
		return 0
	}
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT trade_date) FROM stock_daily WHERE trade_date > ? AND trade_date <= ?`,
		buyDate, today,
	).Scan(&n)
	if err != nil {
		return 0
	}
	return n
}

// BackfillAllHistory 回补全A（按需含北交所）的历史行情，days<=0 默认 250。
func (s *HistoryService) BackfillAllHistory(days int, includeBeijing bool) models.HistoryBackfillResult {
	if s == nil || s.marketService == nil {
		return models.HistoryBackfillResult{
			Status:     "failed",
			Message:    "history service not ready",
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	stocks, err := s.marketService.GetAllAStockSnapshot(includeBeijing)
	if err != nil {
		return models.HistoryBackfillResult{
			Status:     "failed",
			Message:    "获取全A代码失败: " + err.Error(),
			StartedAt:  time.Now().Format("2006-01-02 15:04:05"),
			FinishedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
	}
	codes := make([]string, 0, len(stocks))
	for _, st := range stocks {
		if st.Symbol != "" {
			codes = append(codes, st.Symbol)
		}
	}
	return s.BackfillHistory(models.HistoryBackfillRequest{Codes: codes, Days: days})
}

// EnrichForBacktest 为回测补齐历史字段：开/高/低价(日K)、主力净流入(腾讯历史资金流)、
// 并据近期股本反推换手率/市值/主力强度，全部回写到已有的 kline 历史行。
func (s *HistoryService) EnrichForBacktest(req models.HistoryBackfillRequest) models.HistoryBackfillResult {
	start := time.Now()
	result := models.HistoryBackfillResult{
		StartedAt: start.Format("2006-01-02 15:04:05"),
		DBPath:    s.DBPath(),
		Status:    "running",
	}
	if s == nil || s.db == nil || s.marketService == nil {
		result.Status = "failed"
		result.Message = "history service not ready"
		result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		return result
	}
	days := req.Days
	if days <= 0 || days > 520 {
		days = 250 // 新浪资金流可取约 2 年(500日)
	}
	throttle := time.Duration(req.ThrottleMs) * time.Millisecond
	if throttle <= 0 {
		throttle = 200 * time.Millisecond
	}
	codes := dedupCodes(req.Codes)
	result.TotalCodes = len(codes)
	if len(codes) == 0 {
		result.Status = "failed"
		result.Message = "没有待处理的股票代码"
		result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		return result
	}
	runID := s.insertRun(fmt.Sprintf("enrich x%d", len(codes)), result.StartedAt, "enrich")

	for i, code := range codes {
		saved, earliest, latest, err := s.enrichOne(code, days)
		if err != nil {
			result.FailedCodes++
		} else {
			result.OKCodes++
			result.SavedRows += saved
			if earliest != "" && (result.EarliestDate == "" || earliest < result.EarliestDate) {
				result.EarliestDate = earliest
			}
			if latest > result.LatestDate {
				result.LatestDate = latest
			}
		}
		if i < len(codes)-1 {
			time.Sleep(throttle)
		}
	}

	result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	switch {
	case result.OKCodes == 0:
		result.Status = "failed"
		result.Message = fmt.Sprintf("回测数据补齐全部失败，共 %d 只", result.TotalCodes)
	case result.FailedCodes > 0:
		result.Status = "partial"
		result.Message = fmt.Sprintf("补齐 %d/%d 只，更新 %d 行，区间 %s ~ %s", result.OKCodes, result.TotalCodes, result.SavedRows, result.EarliestDate, result.LatestDate)
	default:
		result.Status = "success"
		result.Message = fmt.Sprintf("补齐 %d 只完成，更新 %d 行，区间 %s ~ %s", result.OKCodes, result.SavedRows, result.EarliestDate, result.LatestDate)
	}
	s.finishRun(runID, models.HistoryCollectResult{
		TradeDate: result.LatestDate, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
		TotalCount: result.TotalCodes, SavedCount: result.OKCodes, FailedCount: result.FailedCodes,
		Status: result.Status, Message: result.Message,
	})
	return result
}

// enrichOne 处理单只：拉日K(OHL)+历史资金流，反推换手/市值，UPDATE 回写。
func (s *HistoryService) enrichOne(code string, days int) (int, string, string, error) {
	// 1) 近期股本：从含市值的快照行反推（总股本、流通股本，视为近似恒定）
	var totalShares, floatShares float64
	var tmc, fmc, refClose float64
	row := s.db.QueryRow(`SELECT total_market_cap, float_market_cap, close_price FROM stock_daily
		WHERE stock_code=? AND total_market_cap IS NOT NULL AND total_market_cap>0 AND close_price>0
		ORDER BY trade_date DESC LIMIT 1`, code)
	if err := row.Scan(&tmc, &fmc, &refClose); err == nil && refClose > 0 {
		totalShares = tmc / refClose
		floatShares = fmc / refClose
	}

	// 2) 日K(OHL/量额/均线)
	klines, kerr := s.marketService.GetKLineData(code, "1d", days+10)
	if kerr != nil || len(klines) == 0 {
		return 0, "", "", fmt.Errorf("kline unavailable: %v", kerr)
	}

	// 3) 历史主力净流入：新浪优先（可取约1年），失败回退腾讯（≤50日）
	flowMap := make(map[string]float64)
	pts, ferr := s.marketService.fetchSinaHistoryFundFlow(code, days)
	if ferr != nil || len(pts) == 0 {
		pts, _ = s.marketService.fetchQQHistoryFundFlow(code, days)
	}
	for _, p := range pts {
		flowMap[p.Date] = p.MainNetInflow
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, "", "", err
	}
	// 缺失日期先插基础K线行（补深历史），再更新富字段
	insStmt, err := tx.Prepare(`INSERT OR IGNORE INTO stock_daily
		(stock_code, trade_date, close_price, open_price, high_price, low_price, amount, volume, pct_change, ma5, ma10, ma20, main_source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'kline', ?)`)
	if err != nil {
		tx.Rollback()
		return 0, "", "", err
	}
	defer insStmt.Close()
	stmt, err := tx.Prepare(`UPDATE stock_daily
		SET open_price=?, high_price=?, low_price=?, main_net=?, main_pct=?, turnover=?, total_market_cap=?, float_market_cap=?
		WHERE stock_code=? AND trade_date=? AND main_source='kline'`)
	if err != nil {
		tx.Rollback()
		return 0, "", "", err
	}
	defer stmt.Close()

	nowText := time.Now().Format("2006-01-02 15:04:05")
	saved := 0
	earliest, latest := "", ""
	prevClose := 0.0
	for _, k := range klines {
		date := strings.TrimSpace(k.Time)
		if date == "" || k.Close <= 0 {
			prevClose = k.Close
			continue
		}
		pct := math.NaN()
		if prevClose > 0 {
			pct = (k.Close - prevClose) / prevClose * 100
		}
		prevClose = k.Close
		if _, e := insStmt.Exec(code, date, k.Close, nullableFloat(k.Open), nullableFloat(k.High), nullableFloat(k.Low),
			nullableFloat(k.Amount), float64(k.Volume), nullableFloat(pct), nullableFloat(k.MA5), nullableFloat(k.MA10), nullableFloat(k.MA20), nowText); e != nil {
			tx.Rollback()
			return 0, "", "", e
		}
		var mainNet, mainPct, turnover, totCap, fltCap any = nil, nil, nil, nil, nil
		if v, ok := flowMap[date]; ok {
			mainNet = v
			if k.Amount > 0 {
				mainPct = v / k.Amount * 100 // 主力净占成交额% ≈ 主力强度
			}
		}
		if floatShares > 0 && k.Volume > 0 {
			turnover = float64(k.Volume) * 100 / floatShares * 100 // 成交量(手→股)/流通股
		}
		if totalShares > 0 {
			totCap = totalShares * k.Close
		}
		if floatShares > 0 {
			fltCap = floatShares * k.Close
		}
		res, execErr := stmt.Exec(
			nullableFloat(k.Open), nullableFloat(k.High), nullableFloat(k.Low),
			mainNet, mainPct, turnover, totCap, fltCap,
			code, date,
		)
		if execErr != nil {
			tx.Rollback()
			return 0, "", "", execErr
		}
		if n, _ := res.RowsAffected(); n > 0 {
			saved++
		}
		if earliest == "" || date < earliest {
			earliest = date
		}
		if date > latest {
			latest = date
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", "", err
	}
	return saved, earliest, latest, nil
}

// writeBackfillRows 把单只日K写入 stock_daily（只填缺失日期），返回写入行数与日期区间。
func (s *HistoryService) writeBackfillRows(code string, klines []models.KLineData) (int, string, string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "", "", err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO stock_daily
		(stock_code, trade_date, close_price, open_price, high_price, low_price, amount, volume, pct_change, ma5, ma10, ma20, main_source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, "", "", err
	}
	defer stmt.Close()

	nowText := time.Now().Format("2006-01-02 15:04:05")
	saved := 0
	earliest, latest := "", ""
	prevClose := 0.0
	for _, k := range klines {
		date := strings.TrimSpace(k.Time)
		if date == "" || k.Close <= 0 {
			prevClose = k.Close
			continue
		}
		// 涨跌幅由相邻日K自算（快照行有自带涨跌幅，IGNORE 不会覆盖）
		pct := math.NaN()
		if prevClose > 0 {
			pct = (k.Close - prevClose) / prevClose * 100
		}
		prevClose = k.Close

		res, execErr := stmt.Exec(
			code,
			date,
			k.Close,
			nullableFloat(k.Open),
			nullableFloat(k.High),
			nullableFloat(k.Low),
			nullableFloat(k.Amount),
			float64(k.Volume),
			nullableFloat(pct),
			nullableFloat(k.MA5),
			nullableFloat(k.MA10),
			nullableFloat(k.MA20),
			"kline",
			nowText,
		)
		if execErr != nil {
			tx.Rollback()
			return 0, "", "", execErr
		}
		if n, _ := res.RowsAffected(); n > 0 {
			saved++
		}
		if earliest == "" || date < earliest {
			earliest = date
		}
		if date > latest {
			latest = date
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", "", err
	}
	return saved, earliest, latest, nil
}

// dedupCodes 清洗去重股票代码，保持入参顺序。
func dedupCodes(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// UpdateMovingAverages recalculates MA5/MA10/MA20 for every stock up to date.
func (s *HistoryService) UpdateMovingAverages(date string) error {
	if s == nil || s.db == nil {
		return errors.New("history db not ready")
	}
	codesRows, err := s.db.Query(`SELECT DISTINCT stock_code FROM stock_daily WHERE trade_date <= ?`, date)
	if err != nil {
		return err
	}
	defer codesRows.Close()

	codes := make([]string, 0, 6000)
	for codesRows.Next() {
		var code string
		if err := codesRows.Scan(&code); err == nil && code != "" {
			codes = append(codes, code)
		}
	}
	if err := codesRows.Err(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	updateStmt, err := tx.Prepare(`UPDATE stock_daily SET ma5=?, ma10=?, ma20=? WHERE stock_code=? AND trade_date=?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer updateStmt.Close()

	for _, code := range codes {
		rows, err := tx.Query(`SELECT trade_date, close_price
			FROM stock_daily
			WHERE stock_code=? AND trade_date<=?
			ORDER BY trade_date DESC
			LIMIT 20`, code, date)
		if err != nil {
			tx.Rollback()
			return err
		}
		points := make([]historyClosePoint, 0, 20)
		for rows.Next() {
			var p historyClosePoint
			if err := rows.Scan(&p.Date, &p.Close); err == nil && p.Close > 0 {
				points = append(points, p)
			}
		}
		rows.Close()
		if len(points) == 0 {
			continue
		}
		ma5 := avgClose(points, 5)
		ma10 := avgClose(points, 10)
		ma20 := avgClose(points, 20)
		if _, err := updateStmt.Exec(nullableFloat(ma5), nullableFloat(ma10), nullableFloat(ma20), code, points[0].Date); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *HistoryService) insertRun(tradeDate string, startedAt string, source string) int64 {
	if s == nil || s.db == nil {
		return 0
	}
	res, err := s.db.Exec(`INSERT INTO history_collect_runs (trade_date, started_at, source, status)
		VALUES (?, ?, ?, ?)`, tradeDate, startedAt, source, "running")
	if err != nil {
		historyLog.Warn("insert history run failed: %v", err)
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

func (s *HistoryService) finishRun(runID int64, result models.HistoryCollectResult) {
	if s == nil || s.db == nil || runID <= 0 {
		return
	}
	finishedAt := result.FinishedAt
	if finishedAt == "" {
		finishedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if _, err := s.db.Exec(`UPDATE history_collect_runs
		SET finished_at=?, total_count=?, saved_count=?, failed_count=?, status=?, message=?
		WHERE id=?`,
		finishedAt,
		result.TotalCount,
		result.SavedCount,
		result.FailedCount,
		result.Status,
		result.Message,
		runID,
	); err != nil {
		historyLog.Warn("finish history run failed: %v", err)
	}
}

func (s *HistoryService) getHistoryConfig() models.HistoryConfig {
	cfg := models.HistoryConfig{
		AutoCollectDaily: false,
		CollectStart:     "16:00",
		CollectEnd:       "17:00",
		IncludeBeijing:   false,
	}
	if s == nil || s.configService == nil {
		return cfg
	}
	appCfg := s.configService.GetConfig()
	if appCfg == nil {
		return cfg
	}
	cfg = appCfg.History
	cfg.CollectStart = normalizeClockHHMM(cfg.CollectStart, "16:00")
	cfg.CollectEnd = normalizeClockHHMM(cfg.CollectEnd, "17:00")
	return cfg
}

func normalizeClockHHMM(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if _, err := parseHHMM(value); err == nil {
		return value
	}
	return fallback
}

func parseHHMM(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time: %s", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid time: %s", value)
	}
	return hour*60 + minute, nil
}

func isWithinClockWindow(now time.Time, start string, end string) bool {
	startMin, startErr := parseHHMM(normalizeClockHHMM(start, "16:00"))
	endMin, endErr := parseHHMM(normalizeClockHHMM(end, "17:00"))
	if startErr != nil || endErr != nil || endMin <= startMin {
		startMin = 16 * 60
		endMin = 17 * 60
	}
	current := now.Hour()*60 + now.Minute()
	return current >= startMin && current < endMin
}

func historyAutoCollectMessage(cfg models.HistoryConfig) string {
	if !cfg.AutoCollectDaily {
		return "盘后自动采集未开启"
	}
	last := cfg.LastCollectDate
	if strings.TrimSpace(last) == "" {
		last = "暂无"
	}
	return fmt.Sprintf("盘后自动采集已开启：%s-%s，最近采集 %s", cfg.CollectStart, cfg.CollectEnd, last)
}

type historyClosePoint struct {
	Date  string
	Close float64
}

func avgClose(points []historyClosePoint, n int) float64 {
	if len(points) < n {
		return math.NaN()
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += points[i].Close
	}
	return sum / float64(n)
}

func nullableFloat(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}
