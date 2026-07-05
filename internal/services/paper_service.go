package services

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"

	_ "github.com/glebarez/go-sqlite"
)

var paperLog = logger.New("paper")

// PaperService 模拟持仓：纸上交易记录 + 按筛选系统统计胜率。
type PaperService struct {
	db *sql.DB
}

func NewPaperService(dataDir string) (*PaperService, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "paper.db"))
	if err != nil {
		return nil, err
	}
	s := &PaperService{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *PaperService) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

func (s *PaperService) initSchema() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS paper_positions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			name TEXT,
			source TEXT,
			cost_price REAL,
			shares INTEGER,
			open_date TEXT,
			open_price REAL,
			status TEXT,
			close_price REAL,
			close_date TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS equity_snapshots (
			snap_date TEXT PRIMARY KEY,
			equity REAL
		)`,
		`CREATE TABLE IF NOT EXISTS board_reports (
			stock_code TEXT NOT NULL,
			period TEXT NOT NULL DEFAULT '',
			stock_name TEXT,
			report TEXT NOT NULL,
			agent_id TEXT,
			agent_name TEXT,
			model_name TEXT,
			generated_at TEXT,
			PRIMARY KEY(stock_code, period)
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}
	// 迁移：补 exit_reason 列（旧库无此列时 ALTER，已存在则忽略报错）
	if _, err := s.db.Exec(`ALTER TABLE paper_positions ADD COLUMN exit_reason TEXT DEFAULT ''`); err != nil {
		paperLog.Debug("exit_reason 列已存在或无需迁移: %v", err)
	}
	return nil
}

// RecordEquity 记录某日组合净值快照（回撤预警用）。
func (s *PaperService) RecordEquity(date string, equity float64) {
	if s == nil || s.db == nil || equity <= 0 {
		return
	}
	s.db.Exec(`INSERT OR REPLACE INTO equity_snapshots(snap_date, equity) VALUES(?,?)`, date, equity)
}

// EquityPeak 返回历史净值峰值（无记录返回 0）。
func (s *PaperService) EquityPeak() float64 {
	if s == nil || s.db == nil {
		return 0
	}
	var peak float64
	s.db.QueryRow(`SELECT MAX(equity) FROM equity_snapshots`).Scan(&peak)
	return peak
}

func today() string { return time.Now().Format("2006-01-02") }

// paperNetReturn 扣双边真实成本后的净收益%（与低吸回测同口径，胜率才可比）。
func paperNetReturn(cost, exit float64) float64 {
	if cost <= 0 {
		return 0
	}
	buy, sell := PaperCostRates()
	outlay := cost * (1 + buy)
	return (exit*(1-sell) - outlay) / outlay * 100
}

// PaperNetReturnPct 导出版（供 app 层计算盘中/平仓净收益）。
func PaperNetReturnPct(cost, exit float64) float64 { return paperNetReturn(cost, exit) }

// Add 新建一笔模拟持仓。shares<=0 默认 1000；costPrice<=0 用 openPrice。
func (s *PaperService) Add(symbol, name, source string, costPrice float64, shares int64) (int64, error) {
	if shares <= 0 {
		shares = 1000
	}
	if costPrice <= 0 {
		costPrice = 0
	}
	res, err := s.db.Exec(
		`INSERT INTO paper_positions(symbol,name,source,cost_price,shares,open_date,open_price,status,close_price,close_date)
		 VALUES(?,?,?,?,?,?,?,?,0,'')`,
		symbol, name, source, costPrice, shares, today(), costPrice, "open",
	)
	if err != nil {
		paperLog.Error("add paper position error: %v", err)
		return 0, err
	}
	return res.LastInsertId()
}

// List 返回所有模拟持仓（未平仓在前，按 open_date 倒序）。
func (s *PaperService) List() []models.PaperPosition {
	rows, err := s.db.Query(`SELECT id,symbol,name,source,cost_price,shares,open_date,open_price,status,close_price,close_date,COALESCE(exit_reason,'')
		FROM paper_positions ORDER BY (status='open') DESC, id DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.PaperPosition
	for rows.Next() {
		var p models.PaperPosition
		if err := rows.Scan(&p.ID, &p.Symbol, &p.Name, &p.Source, &p.CostPrice, &p.Shares,
			&p.OpenDate, &p.OpenPrice, &p.Status, &p.ClosePrice, &p.CloseDate, &p.ExitReason); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Update 修改成本价/持仓数量。
func (s *PaperService) Update(id int64, costPrice float64, shares int64) error {
	if shares <= 0 {
		shares = 1000
	}
	_, err := s.db.Exec(`UPDATE paper_positions SET cost_price=?, shares=? WHERE id=?`, costPrice, shares, id)
	return err
}

// Close 平仓（记录卖出价 → 计入胜率）。reason 为空=手动平仓。
func (s *PaperService) ClosePosition(id int64, closePrice float64, reason string) error {
	_, err := s.db.Exec(`UPDATE paper_positions SET status='closed', close_price=?, close_date=?, exit_reason=? WHERE id=?`,
		closePrice, today(), reason, id)
	return err
}

// CloseOn 按指定平仓日落库（自动平仓用，平仓日=纪律触发的真实收盘日）。
func (s *PaperService) CloseOn(id int64, closePrice float64, closeDate, reason string) error {
	_, err := s.db.Exec(`UPDATE paper_positions SET status='closed', close_price=?, close_date=?, exit_reason=? WHERE id=?`,
		closePrice, closeDate, reason, id)
	return err
}

// Reopen 撤回平仓：恢复为未平仓，并清除平仓价/日期/退出原因。
func (s *PaperService) Reopen(id int64) error {
	_, err := s.db.Exec(`UPDATE paper_positions SET status='open', close_price=0, close_date='', exit_reason='' WHERE id=?`, id)
	return err
}

// OpenPositions 返回所有未平仓持仓（自动平仓引擎遍历用）。
func (s *PaperService) OpenPositions() []models.PaperPosition {
	var out []models.PaperPosition
	for _, p := range s.List() {
		if p.Status == "open" {
			out = append(out, p)
		}
	}
	return out
}

// Delete 删除一笔模拟持仓。
func (s *PaperService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM paper_positions WHERE id=?`, id)
	return err
}

// Stats 按筛选系统统计计分卡（已平仓、扣成本净收益口径，与低吸回测一致）。
func (s *PaperService) Stats() models.PaperStats {
	list := s.List()
	bySrc := map[string]*models.PaperSourceStat{}
	order := []string{}
	// 逐来源累加：盈利单/亏损单和、单笔最大亏损
	sumWin := map[string]float64{}
	sumLoss := map[string]float64{}
	winCnt := map[string]int{}
	lossCnt := map[string]int{}
	maxLoss := map[string]float64{}
	totalClosed, totalWin := 0, 0
	openCount := 0
	var gSumWin, gSumLoss, gSumRet float64
	var gWinCnt, gLossCnt int
	gMaxLoss := 0.0
	for _, p := range list {
		src := p.Source
		if src == "" {
			src = "manual"
		}
		st, ok := bySrc[src]
		if !ok {
			st = &models.PaperSourceStat{Source: src}
			bySrc[src] = st
			order = append(order, src)
		}
		st.Total++
		if p.Status == "open" {
			openCount++
			continue
		}
		st.Closed++
		totalClosed++
		ret := paperNetReturn(p.CostPrice, p.ClosePrice)
		st.TotalReturn += ret
		gSumRet += ret
		if ret > 0 {
			st.Win++
			totalWin++
			sumWin[src] += ret
			winCnt[src]++
			gSumWin += ret
			gWinCnt++
		} else {
			sumLoss[src] += ret
			lossCnt[src]++
			gSumLoss += ret
			gLossCnt++
			if ret < maxLoss[src] {
				maxLoss[src] = ret
			}
			if ret < gMaxLoss {
				gMaxLoss = ret
			}
		}
	}
	out := models.PaperStats{OpenCount: openCount, ClosedCount: totalClosed, MaxLoss: round2(gMaxLoss)}
	if totalClosed > 0 {
		out.WinRate = round2(float64(totalWin) * 100 / float64(totalClosed))
		out.Expectancy = round2(gSumRet / float64(totalClosed))
	}
	if gWinCnt > 0 && gLossCnt > 0 {
		aw := gSumWin / float64(gWinCnt)
		al := gSumLoss / float64(gLossCnt)
		if al != 0 {
			out.PayoffRatio = round2(aw / math.Abs(al))
		}
	}
	if gSumLoss != 0 {
		out.ProfitFactor = round2(gSumWin / math.Abs(gSumLoss))
	}
	for _, src := range order {
		st := bySrc[src]
		if st.Closed > 0 {
			st.WinRate = round2(float64(st.Win) * 100 / float64(st.Closed))
			st.AvgReturn = round2(st.TotalReturn / float64(st.Closed))
		}
		if winCnt[src] > 0 {
			st.AvgWin = round2(sumWin[src] / float64(winCnt[src]))
		}
		if lossCnt[src] > 0 {
			st.AvgLoss = round2(sumLoss[src] / float64(lossCnt[src]))
		}
		if st.AvgLoss != 0 {
			st.PayoffRatio = round2(st.AvgWin / math.Abs(st.AvgLoss))
		}
		if sumLoss[src] != 0 {
			st.ProfitFactor = round2(sumWin[src] / math.Abs(sumLoss[src]))
		}
		st.MaxLoss = round2(maxLoss[src])
		out.BySource = append(out.BySource, *st)
	}
	sort.Slice(out.BySource, func(i, j int) bool { return out.BySource[i].Total > out.BySource[j].Total })
	return out
}

// BoardReportRecord 老陈完整报告缓存：同票同周期只保留最新一份。
type BoardReportRecord struct {
	StockCode   string `json:"stockCode"`
	Period      string `json:"period"`
	StockName   string `json:"stockName"`
	Report      string `json:"report"`
	AgentID     string `json:"agentId"`
	AgentName   string `json:"agentName"`
	ModelName   string `json:"modelName"`
	GeneratedAt string `json:"generatedAt"`
}

// SaveBoardReport 写入/覆盖报告缓存（同 stock_code+period upsert）。
func (s *PaperService) SaveBoardReport(r BoardReportRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO board_reports(stock_code, period, stock_name, report, agent_id, agent_name, model_name, generated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		r.StockCode, r.Period, r.StockName, r.Report, r.AgentID, r.AgentName, r.ModelName, r.GeneratedAt,
	)
	return err
}

// GetBoardReport 读取报告缓存，未命中返回 nil。
func (s *PaperService) GetBoardReport(stockCode, period string) (*BoardReportRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var r BoardReportRecord
	err := s.db.QueryRow(
		`SELECT stock_code, period, COALESCE(stock_name,''), report, COALESCE(agent_id,''), COALESCE(agent_name,''), COALESCE(model_name,''), COALESCE(generated_at,'')
		 FROM board_reports WHERE stock_code = ? AND period = ?`,
		stockCode, period,
	).Scan(&r.StockCode, &r.Period, &r.StockName, &r.Report, &r.AgentID, &r.AgentName, &r.ModelName, &r.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
