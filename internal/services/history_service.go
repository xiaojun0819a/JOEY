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
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
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
	status := s.marketService.GetMarketStatus()
	if !status.IsTradeDay {
		return
	}
	if !isWithinClockWindow(now, cfg.CollectStart, cfg.CollectEnd) {
		return
	}
	today := now.Format("2006-01-02")
	if cfg.LastCollectDate == today {
		return
	}

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

	result := s.CollectDailyHistory(models.HistoryCollectRequest{
		TradeDate:       today,
		IncludeBeijing:  cfg.IncludeBeijing,
		TriggeredByAuto: true,
	})
	if result.Status != "success" && result.Status != "partial" {
		historyLog.Warn("auto history collect failed: %s", result.Message)
		return
	}
	appCfg := s.configService.GetConfig()
	if appCfg == nil {
		return
	}
	appCfg.History.LastCollectDate = today
	if appCfg.History.CollectStart == "" {
		appCfg.History.CollectStart = cfg.CollectStart
	}
	if appCfg.History.CollectEnd == "" {
		appCfg.History.CollectEnd = cfg.CollectEnd
	}
	if err := s.configService.UpdateConfig(appCfg); err != nil {
		historyLog.Warn("save auto history collect date failed: %v", err)
	}
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
