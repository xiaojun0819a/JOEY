package services

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"

	_ "github.com/glebarez/go-sqlite"
)

var journalLog = logger.New("journal")

// JournalService 交易台账：记录真实买卖、统计实盘胜率/盈亏（日/周/月）。
type JournalService struct {
	db *sql.DB
}

func NewJournalService(dataDir string) (*JournalService, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "journal.db"))
	if err != nil {
		return nil, err
	}
	s := &JournalService{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *JournalService) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

func (s *JournalService) initSchema() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stock_code TEXT NOT NULL,
			stock_name TEXT,
			buy_date TEXT,
			buy_price REAL,
			shares INTEGER,
			sell_date TEXT,
			sell_price REAL,
			status TEXT,
			pnl REAL,
			pnl_pct REAL,
			hold_days INTEGER,
			source TEXT,
			note TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_status ON trades(status)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_sell ON trades(sell_date)`,
		`CREATE TABLE IF NOT EXISTS trade_actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trade_id INTEGER,
			stock_code TEXT NOT NULL,
			stock_name TEXT,
			action TEXT,
			trade_date TEXT,
			price REAL,
			shares INTEGER,
			amount REAL,
			after_shares INTEGER,
			after_cost REAL,
			note TEXT,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_actions_trade ON trade_actions(trade_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_actions_stock ON trade_actions(stock_code, trade_date)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}
	if err := s.backfillMissingActions(); err != nil {
		journalLog.Warn("补齐交易流水失败: %v", err)
	}
	return nil
}

func nowStr() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// computePnL 根据买卖价数量计算盈亏额/百分比/持仓天数。
func computePnL(e *models.TradeJournalEntry) {
	if e.Status == "closed" && e.BuyPrice > 0 && e.SellPrice > 0 {
		e.PnLPct = round2((e.SellPrice - e.BuyPrice) / e.BuyPrice * 100)
		if e.Shares > 0 {
			e.PnL = round2((e.SellPrice - e.BuyPrice) * float64(e.Shares))
		} else {
			e.PnL = 0
		}
		e.HoldDays = calendarDays(e.BuyDate, e.SellDate)
	} else {
		e.PnL, e.PnLPct, e.HoldDays = 0, 0, 0
	}
}

func calendarDays(from, to string) int {
	f, err1 := time.Parse("2006-01-02", strings.TrimSpace(from))
	t, err2 := time.Parse("2006-01-02", strings.TrimSpace(to))
	if err1 != nil || err2 != nil {
		return 0
	}
	d := int(t.Sub(f).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return d
}

func weightedCost(oldCost float64, oldShares int64, addPrice float64, addShares int64) float64 {
	if oldShares <= 0 {
		return addPrice
	}
	totalShares := oldShares + addShares
	if totalShares <= 0 {
		return oldCost
	}
	return round2((oldCost*float64(oldShares) + addPrice*float64(addShares)) / float64(totalShares))
}

func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "build", "open", "buy", "建仓":
		return "build"
	case "add", "increase", "加仓":
		return "add"
	case "reduce", "trim", "减仓":
		return "reduce"
	case "close", "sell", "平仓":
		return "close"
	case "manual", "edit":
		return "manual"
	default:
		return ""
	}
}

func (s *JournalService) insertAction(tx *sql.Tx, tradeID int64, code, name, action, tradeDate string, price float64, shares int64, afterShares int64, afterCost float64, note string) {
	if tx == nil || strings.TrimSpace(code) == "" || strings.TrimSpace(action) == "" {
		return
	}
	amount := round2(price * float64(shares))
	_, _ = tx.Exec(`INSERT INTO trade_actions
		(trade_id, stock_code, stock_name, action, trade_date, price, shares, amount, after_shares, after_cost, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tradeID, code, name, action, tradeDate, price, shares, amount, afterShares, afterCost, note, nowStr())
}

func (s *JournalService) backfillMissingActions() error {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT t.id, t.stock_code, COALESCE(t.stock_name,''), COALESCE(t.buy_date,''), COALESCE(t.buy_price,0),
		COALESCE(t.shares,0), COALESCE(t.sell_date,''), COALESCE(t.sell_price,0), COALESCE(t.status,''), COALESCE(t.note,'')
		FROM trades t
		WHERE NOT EXISTS (SELECT 1 FROM trade_actions a WHERE a.trade_id=t.id)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type legacyTrade struct {
		id                            int64
		code, name, buyDate, sellDate string
		status, note                  string
		buyPrice, sellPrice           float64
		shares                        int64
	}
	var list []legacyTrade
	for rows.Next() {
		var t legacyTrade
		if err := rows.Scan(&t.id, &t.code, &t.name, &t.buyDate, &t.buyPrice, &t.shares, &t.sellDate, &t.sellPrice, &t.status, &t.note); err == nil {
			list = append(list, t)
		}
	}
	if len(list) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range list {
		if t.code == "" || t.shares <= 0 || t.buyPrice <= 0 {
			continue
		}
		s.insertAction(tx, t.id, t.code, t.name, "build", t.buyDate, t.buyPrice, t.shares, t.shares, t.buyPrice, t.note)
		if t.status == "closed" && t.sellDate != "" && t.sellPrice > 0 {
			s.insertAction(tx, t.id, t.code, t.name, "close", t.sellDate, t.sellPrice, t.shares, 0, t.buyPrice, t.note)
		}
	}
	return tx.Commit()
}

// OnBuy 持仓建立/更新时记一笔"持仓中"（同股已有未平仓则更新）。
func (s *JournalService) OnBuy(code, name, buyDate string, buyPrice float64, shares int64, source string) {
	if s == nil || s.db == nil || code == "" {
		return
	}
	if strings.TrimSpace(buyDate) == "" {
		buyDate = time.Now().Format("2006-01-02")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	var id int64
	var oldPrice float64
	var oldShares int64
	err = tx.QueryRow(`SELECT id, buy_price, shares FROM trades WHERE stock_code=? AND status='open' ORDER BY id DESC LIMIT 1`, code).Scan(&id, &oldPrice, &oldShares)
	if err == nil && id > 0 {
		action := "add"
		if shares <= oldShares {
			action = "build"
		}
		addShares := shares
		if shares > oldShares {
			addShares = shares - oldShares
			buyPrice = weightedCost(oldPrice, oldShares, buyPrice, addShares)
		}
		tx.Exec(`UPDATE trades SET stock_name=?, buy_price=?, shares=?, source=?, updated_at=? WHERE id=?`,
			name, buyPrice, shares, source, nowStr(), id)
		s.insertAction(tx, id, code, name, action, buyDate, buyPrice, addShares, shares, buyPrice, "")
		_ = tx.Commit()
		return
	}
	now := nowStr()
	res, err := tx.Exec(`INSERT INTO trades
		(stock_code, stock_name, buy_date, buy_price, shares, status, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'open', ?, ?, ?)`,
		code, name, buyDate, buyPrice, shares, source, now, now)
	if err != nil {
		journalLog.Warn("记录买入失败: %v", err)
		return
	}
	id, _ = res.LastInsertId()
	s.insertAction(tx, id, code, name, "build", buyDate, buyPrice, shares, shares, buyPrice, "")
	_ = tx.Commit()
}

// OnSell 平仓：把同股未平仓记录补上卖出价/日期并结算。
func (s *JournalService) OnSell(code, sellDate string, sellPrice float64) {
	if s == nil || s.db == nil || code == "" {
		return
	}
	if strings.TrimSpace(sellDate) == "" {
		sellDate = time.Now().Format("2006-01-02")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	var e models.TradeJournalEntry
	var name, buyDate, source, note sql.NullString
	row := tx.QueryRow(`SELECT id, stock_name, buy_date, buy_price, shares, source, note FROM trades WHERE stock_code=? AND status='open' ORDER BY id DESC LIMIT 1`, code)
	if err := row.Scan(&e.ID, &name, &buyDate, &e.BuyPrice, &e.Shares, &source, &note); err != nil {
		return // 没有未平仓记录，忽略
	}
	e.StockCode = code
	e.StockName, e.BuyDate, e.Source, e.Note = name.String, buyDate.String, source.String, note.String
	e.SellDate, e.SellPrice, e.Status = sellDate, sellPrice, "closed"
	computePnL(&e)
	tx.Exec(`UPDATE trades SET sell_date=?, sell_price=?, status='closed', pnl=?, pnl_pct=?, hold_days=?, updated_at=? WHERE id=?`,
		e.SellDate, e.SellPrice, e.PnL, e.PnLPct, e.HoldDays, nowStr(), e.ID)
	s.insertAction(tx, e.ID, e.StockCode, e.StockName, "close", sellDate, sellPrice, e.Shares, 0, e.BuyPrice, "")
	_ = tx.Commit()
}

// SaveManual 手动新增/修改一笔。
func (s *JournalService) SaveManual(req models.TradeJournalRequest) (models.TradeJournalEntry, error) {
	if s == nil || s.db == nil {
		return models.TradeJournalEntry{}, errors.New("journal not ready")
	}
	if strings.TrimSpace(req.StockCode) == "" {
		return models.TradeJournalEntry{}, errors.New("股票代码不能为空")
	}
	action := normalizeAction(req.Action)
	if action == "build" || action == "add" || action == "reduce" || action == "close" {
		return s.SaveAction(req, action)
	}
	e := models.TradeJournalEntry{
		ID: req.ID, StockCode: req.StockCode, StockName: req.StockName,
		BuyDate: req.BuyDate, BuyPrice: req.BuyPrice, Shares: req.Shares,
		SellDate: strings.TrimSpace(req.SellDate), SellPrice: req.SellPrice,
		Source: req.Source, Note: req.Note,
	}
	if e.Source == "" {
		e.Source = "manual"
	}
	if e.SellDate != "" && e.SellPrice > 0 {
		e.Status = "closed"
	} else {
		e.Status = "open"
	}
	computePnL(&e)
	now := nowStr()
	if req.ID > 0 {
		_, err := s.db.Exec(`UPDATE trades SET stock_code=?, stock_name=?, buy_date=?, buy_price=?, shares=?,
			sell_date=?, sell_price=?, status=?, pnl=?, pnl_pct=?, hold_days=?, source=?, note=?, updated_at=? WHERE id=?`,
			e.StockCode, e.StockName, e.BuyDate, e.BuyPrice, e.Shares, e.SellDate, e.SellPrice, e.Status,
			e.PnL, e.PnLPct, e.HoldDays, e.Source, e.Note, now, req.ID)
		return e, err
	}
	res, err := s.db.Exec(`INSERT INTO trades
		(stock_code, stock_name, buy_date, buy_price, shares, sell_date, sell_price, status, pnl, pnl_pct, hold_days, source, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.StockCode, e.StockName, e.BuyDate, e.BuyPrice, e.Shares, e.SellDate, e.SellPrice, e.Status,
		e.PnL, e.PnLPct, e.HoldDays, e.Source, e.Note, now, now)
	if err != nil {
		return e, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

// SaveAction 以建仓/加仓/减仓/平仓动作写入流水，并同步维护汇总持仓行。
func (s *JournalService) SaveAction(req models.TradeJournalRequest, action string) (models.TradeJournalEntry, error) {
	if s == nil || s.db == nil {
		return models.TradeJournalEntry{}, errors.New("journal not ready")
	}
	code := strings.TrimSpace(req.StockCode)
	if code == "" {
		return models.TradeJournalEntry{}, errors.New("股票代码不能为空")
	}
	if req.Shares <= 0 {
		return models.TradeJournalEntry{}, errors.New("数量必须大于0")
	}
	if req.BuyPrice <= 0 && action != "close" && action != "reduce" {
		return models.TradeJournalEntry{}, errors.New("买入价必须大于0")
	}
	if req.SellPrice <= 0 && (action == "close" || action == "reduce") {
		return models.TradeJournalEntry{}, errors.New("卖出价必须大于0")
	}
	tradeDate := strings.TrimSpace(req.BuyDate)
	if action == "close" || action == "reduce" {
		tradeDate = strings.TrimSpace(req.SellDate)
	}
	if tradeDate == "" {
		tradeDate = time.Now().Format("2006-01-02")
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return models.TradeJournalEntry{}, err
	}
	defer tx.Rollback()

	var e models.TradeJournalEntry
	var existingName, existingBuyDate, existingSource, existingNote sql.NullString
	err = tx.QueryRow(`SELECT id, stock_code, stock_name, buy_date, buy_price, shares, source, note
		FROM trades WHERE stock_code=? AND status='open' ORDER BY id DESC LIMIT 1`, code).
		Scan(&e.ID, &e.StockCode, &existingName, &existingBuyDate, &e.BuyPrice, &e.Shares, &existingSource, &existingNote)
	hasOpen := err == nil && e.ID > 0
	if err != nil && err != sql.ErrNoRows {
		return e, err
	}
	e.StockName, e.BuyDate, e.Source = existingName.String, existingBuyDate.String, existingSource.String
	if e.StockName == "" {
		e.StockName = req.StockName
	}
	if hasOpen {
		e.Status = "open"
	}
	e.Note = existingNote.String

	now := nowStr()
	switch action {
	case "build":
		if hasOpen {
			return e, errors.New("该股票已有持仓，请使用加仓或先平仓")
		}
		e = models.TradeJournalEntry{
			StockCode: code, StockName: req.StockName, BuyDate: tradeDate, BuyPrice: req.BuyPrice,
			Shares: req.Shares, Status: "open", Source: source, Note: req.Note,
		}
		res, err := tx.Exec(`INSERT INTO trades
			(stock_code, stock_name, buy_date, buy_price, shares, status, source, note, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'open', ?, ?, ?, ?)`,
			e.StockCode, e.StockName, e.BuyDate, e.BuyPrice, e.Shares, e.Source, e.Note, now, now)
		if err != nil {
			return e, err
		}
		e.ID, _ = res.LastInsertId()
		s.insertAction(tx, e.ID, e.StockCode, e.StockName, "build", tradeDate, req.BuyPrice, req.Shares, req.Shares, req.BuyPrice, req.Note)
	case "add":
		if !hasOpen {
			return e, errors.New("该股票没有未平仓记录，请先建仓")
		}
		newShares := e.Shares + req.Shares
		newCost := weightedCost(e.BuyPrice, e.Shares, req.BuyPrice, req.Shares)
		if req.StockName != "" {
			e.StockName = req.StockName
		}
		if req.Note != "" {
			e.Note = req.Note
		}
		_, err := tx.Exec(`UPDATE trades SET stock_name=?, buy_price=?, shares=?, source=?, note=?, updated_at=? WHERE id=?`,
			e.StockName, newCost, newShares, source, e.Note, now, e.ID)
		if err != nil {
			return e, err
		}
		e.BuyPrice, e.Shares, e.Source = newCost, newShares, source
		s.insertAction(tx, e.ID, e.StockCode, e.StockName, "add", tradeDate, req.BuyPrice, req.Shares, newShares, newCost, req.Note)
	case "reduce":
		if !hasOpen {
			return e, errors.New("该股票没有未平仓记录，无法减仓")
		}
		if req.StockName != "" {
			e.StockName = req.StockName
		}
		reduceShares := req.Shares
		if reduceShares >= e.Shares {
			return e, errors.New("减仓数量必须小于当前持仓；清空请使用平仓")
		}
		remain := e.Shares - reduceShares
		if req.Note != "" {
			e.Note = req.Note
		}
		_, err := tx.Exec(`UPDATE trades SET stock_name=?, shares=?, note=?, updated_at=? WHERE id=?`, e.StockName, remain, e.Note, now, e.ID)
		if err != nil {
			return e, err
		}
		s.insertAction(tx, e.ID, e.StockCode, e.StockName, "reduce", tradeDate, req.SellPrice, reduceShares, remain, e.BuyPrice, req.Note)
		e.Shares = remain
	case "close":
		if !hasOpen {
			return e, errors.New("该股票没有未平仓记录，无法平仓")
		}
		if req.StockName != "" {
			e.StockName = req.StockName
		}
		closeShares := req.Shares
		if closeShares > e.Shares {
			closeShares = e.Shares
		}
		remain := e.Shares - closeShares
		if req.Note != "" {
			e.Note = req.Note
		}
		if remain > 0 {
			_, err := tx.Exec(`UPDATE trades SET stock_name=?, shares=?, note=?, updated_at=? WHERE id=?`, e.StockName, remain, e.Note, now, e.ID)
			if err != nil {
				return e, err
			}
			s.insertAction(tx, e.ID, e.StockCode, e.StockName, "close", tradeDate, req.SellPrice, closeShares, remain, e.BuyPrice, req.Note)
			e.Shares = remain
		} else {
			e.SellDate, e.SellPrice, e.Status = tradeDate, req.SellPrice, "closed"
			computePnL(&e)
			_, err := tx.Exec(`UPDATE trades SET stock_name=?, sell_date=?, sell_price=?, status='closed', pnl=?, pnl_pct=?, hold_days=?, note=?, updated_at=? WHERE id=?`,
				e.StockName, e.SellDate, e.SellPrice, e.PnL, e.PnLPct, e.HoldDays, e.Note, now, e.ID)
			if err != nil {
				return e, err
			}
			s.insertAction(tx, e.ID, e.StockCode, e.StockName, "close", tradeDate, req.SellPrice, closeShares, 0, e.BuyPrice, req.Note)
		}
	}
	if err := tx.Commit(); err != nil {
		return e, err
	}
	return e, nil
}

// OpenByStock 返回该股票当前最新一笔未平仓汇总记录。
func (s *JournalService) OpenByStock(code string) (models.TradeJournalEntry, bool) {
	var e models.TradeJournalEntry
	if s == nil || s.db == nil {
		return e, false
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return e, false
	}
	var name, buyDate, source, note, createdAt, updatedAt sql.NullString
	var buyPrice sql.NullFloat64
	var shares sql.NullInt64
	err := s.db.QueryRow(`SELECT id, stock_code, stock_name, buy_date, buy_price, shares, source, note, created_at, updated_at
		FROM trades WHERE stock_code=? AND status='open' ORDER BY id DESC LIMIT 1`, code).
		Scan(&e.ID, &e.StockCode, &name, &buyDate, &buyPrice, &shares, &source, &note, &createdAt, &updatedAt)
	if err != nil {
		return e, false
	}
	e.StockName = name.String
	e.BuyDate = buyDate.String
	e.BuyPrice = buyPrice.Float64
	e.Shares = shares.Int64
	e.Status = "open"
	e.Source = source.String
	e.Note = note.String
	e.CreatedAt = createdAt.String
	e.UpdatedAt = updatedAt.String
	return e, true
}

func (s *JournalService) Delete(id int64) error {
	if s == nil || s.db == nil {
		return errors.New("journal not ready")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM trade_actions WHERE trade_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM trades WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// List 列出全部交易（最新在前）。
func (s *JournalService) List() []models.TradeJournalEntry {
	out := make([]models.TradeJournalEntry, 0, 64)
	if s == nil || s.db == nil {
		return out
	}
	rows, err := s.db.Query(`SELECT id, stock_code, stock_name, buy_date, buy_price, shares,
		sell_date, sell_price, status, pnl, pnl_pct, hold_days, source, note, created_at, updated_at
		FROM trades ORDER BY COALESCE(NULLIF(sell_date,''), buy_date) DESC, id DESC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e models.TradeJournalEntry
		var name, buyDate, sellDate, status, source, note, createdAt, updatedAt sql.NullString
		var buyPrice, sellPrice, pnl, pnlPct sql.NullFloat64
		var shares, holdDays sql.NullInt64
		if err := rows.Scan(&e.ID, &e.StockCode, &name, &buyDate, &buyPrice, &shares,
			&sellDate, &sellPrice, &status, &pnl, &pnlPct, &holdDays, &source, &note, &createdAt, &updatedAt); err != nil {
			continue
		}
		e.StockName, e.BuyDate, e.SellDate = name.String, buyDate.String, sellDate.String
		e.Status, e.Source, e.Note = status.String, source.String, note.String
		e.CreatedAt, e.UpdatedAt = createdAt.String, updatedAt.String
		e.BuyPrice, e.SellPrice, e.PnL, e.PnLPct = buyPrice.Float64, sellPrice.Float64, pnl.Float64, pnlPct.Float64
		e.Shares, e.HoldDays = shares.Int64, int(holdDays.Int64)
		out = append(out, e)
	}
	s.attachActions(out)
	return out
}

func (s *JournalService) attachActions(entries []models.TradeJournalEntry) {
	if s == nil || s.db == nil || len(entries) == 0 {
		return
	}
	ids := make([]string, 0, len(entries))
	index := make(map[int64]int, len(entries))
	for i := range entries {
		ids = append(ids, "?")
		index[entries[i].ID] = i
	}
	args := make([]any, 0, len(entries))
	for _, e := range entries {
		args = append(args, e.ID)
	}
	rows, err := s.db.Query(`SELECT id, trade_id, stock_code, stock_name, action, trade_date, price, shares, amount, after_shares, after_cost, note, created_at
		FROM trade_actions WHERE trade_id IN (`+strings.Join(ids, ",")+`) ORDER BY trade_date ASC, id ASC`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a models.TradeActionEntry
		var name, action, tradeDate, note, createdAt sql.NullString
		var price, amount, afterCost sql.NullFloat64
		var shares, afterShares sql.NullInt64
		if err := rows.Scan(&a.ID, &a.TradeID, &a.StockCode, &name, &action, &tradeDate, &price, &shares, &amount, &afterShares, &afterCost, &note, &createdAt); err != nil {
			continue
		}
		a.StockName, a.Action, a.TradeDate, a.Note, a.CreatedAt = name.String, action.String, tradeDate.String, note.String, createdAt.String
		a.Price, a.Amount, a.AfterCost = price.Float64, amount.Float64, afterCost.Float64
		a.Shares, a.AfterShares = shares.Int64, afterShares.Int64
		if i, ok := index[a.TradeID]; ok {
			entries[i].Actions = append(entries[i].Actions, a)
		}
	}
}

// Stats 统计：总体 + 按平仓日的日/周/月汇总。
func (s *JournalService) Stats() models.TradeJournalStats {
	stats := models.TradeJournalStats{}
	list := s.List()

	dayMap := map[string]*models.TradePeriodStat{}
	weekMap := map[string]*models.TradePeriodStat{}
	monthMap := map[string]*models.TradePeriodStat{}

	var winSum, lossSum float64
	var wins, closed, holdSum int
	var pnlPctSum float64
	for _, e := range list {
		if e.Status != "closed" {
			stats.Summary.OpenCount++
			continue
		}
		closed++
		stats.Summary.TotalPnL += e.PnL
		pnlPctSum += e.PnLPct
		holdSum += e.HoldDays
		if e.PnLPct > 0 {
			wins++
			winSum += e.PnLPct
		} else {
			lossSum += e.PnLPct
		}
		dk, wk, mk := periodKeys(e.SellDate)
		addPeriod(dayMap, dk, e)
		addPeriod(weekMap, wk, e)
		addPeriod(monthMap, mk, e)
	}

	stats.Summary.ClosedCount = closed
	stats.Summary.Wins = wins
	if closed > 0 {
		stats.Summary.WinRate = round2(float64(wins) / float64(closed) * 100)
		stats.Summary.AvgPnLPct = round2(pnlPctSum / float64(closed))
		stats.Summary.AvgHoldDays = round2(float64(holdSum) / float64(closed))
	}
	if wins > 0 {
		stats.Summary.AvgWinPct = round2(winSum / float64(wins))
	}
	if closed-wins > 0 {
		stats.Summary.AvgLossPct = round2(lossSum / float64(closed-wins))
	}
	if lossSum != 0 {
		stats.Summary.ProfitFactor = round2(winSum / (-lossSum))
	}

	stats.ByDay = finalizePeriods(dayMap)
	stats.ByWeek = finalizePeriods(weekMap)
	stats.ByMonth = finalizePeriods(monthMap)
	return stats
}

func periodKeys(date string) (day, week, month string) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return date, date, date
	}
	y, w := t.ISOWeek()
	return t.Format("2006-01-02"), fmt.Sprintf("%d-W%02d", y, w), t.Format("2006-01")
}

func addPeriod(m map[string]*models.TradePeriodStat, key string, e models.TradeJournalEntry) {
	p, ok := m[key]
	if !ok {
		p = &models.TradePeriodStat{Period: key}
		m[key] = p
	}
	p.Trades++
	if e.PnLPct > 0 {
		p.Wins++
	}
	p.TotalPnL += e.PnL
	p.AvgPnLPct += e.PnLPct // 暂存累加，finalize 时取平均
}

func finalizePeriods(m map[string]*models.TradePeriodStat) []models.TradePeriodStat {
	out := make([]models.TradePeriodStat, 0, len(m))
	for _, p := range m {
		if p.Trades > 0 {
			p.WinRate = round2(float64(p.Wins) / float64(p.Trades) * 100)
			p.AvgPnLPct = round2(p.AvgPnLPct / float64(p.Trades))
			p.TotalPnL = round2(p.TotalPnL)
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Period > out[j].Period }) // 新→旧
	return out
}
