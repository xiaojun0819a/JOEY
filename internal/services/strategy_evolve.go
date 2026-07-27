package services

// 策略进化提案制(2026-07-24 用户定:"自动进化前先拿数据告诉我缘由和逻辑,我确认后再进化"):
// 学习库出现达标发现 → 自动生成【进化提案】(数据依据+建议动作+预期逻辑) → 推送等确认 →
// 用户确认后写入 strategy_score_tunings,扫描评分层自动应用(带版本与理由展示),并跟踪应用前后表现。
// 边界:只有「评分权重」可经确认后自动进化;「硬门槛」类发现只出观察级提案,永不自动改。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// 可自动应用的特征(扫描结果条目上当场可算,不需要重载K线):
// turnover=信号日换手 / limitup=信号日涨幅档 / mainpct=主力净占比方向 / crosshits=其他策略同日共振数
var evolvableFeatures = map[string]bool{"换手率": true, "信号日涨幅": true, "主力资金": true, "其他策略共振": true}

type EvolutionProposal struct {
	ID         int64   `json:"id"`
	StrategyID string  `json:"strategyId"`
	Dim        string  `json:"dim"`
	Bucket     string  `json:"bucket"`
	Delta      float64 `json:"delta"`
	Finding    string  `json:"finding"`
	Action     string  `json:"action"` // score=确认后自动应用评分加成 / observe=仅观察(涉硬门槛或不可当场计算)
	Status     string  `json:"status"` // pending / applied / rejected
	CreatedAt  string  `json:"createdAt"`
}

type ScoreTuning struct {
	StrategyID  string
	Dim         string
	Bucket      string
	Delta       float64
	Evidence    string
	ConfirmedAt string
	Version     int
}

func (s *HistoryService) initEvolveSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS strategy_evolution_proposals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id TEXT NOT NULL,
			dim TEXT NOT NULL,
			bucket TEXT NOT NULL,
			delta REAL,
			finding TEXT,
			action TEXT,
			status TEXT DEFAULT 'pending',
			created_at TEXT,
			decided_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS strategy_score_tunings (
			strategy_id TEXT NOT NULL,
			dim TEXT NOT NULL,
			bucket TEXT NOT NULL,
			delta REAL,
			evidence TEXT,
			confirmed_at TEXT,
			version INTEGER,
			PRIMARY KEY(strategy_id, dim, bucket)
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}
	// 种子:11 的换手加分已在 2026-07-22 直接内置进代码(学习调优v1.1),登记占位防重复提案。
	_, _ = s.db.Exec(`INSERT OR IGNORE INTO strategy_score_tunings(strategy_id,dim,bucket,delta,evidence,confirmed_at,version)
		VALUES('oversold-ignite-v1','换手率','≥15%',0,'已内置于代码(学习调优v1.1,+0~6分线性),此记录仅防重复提案','2026-07-22 22:33',1)`)
	return nil
}

// learnFindings 从共享学习库为某策略计算达标发现(两侧n≥20且红盘差≥15pp),供提案生成。
func (s *HistoryService) learnFindings(strategyID string) []struct {
	Dim, BestLabel, WorstLabel string
	BestN, WorstN              int
	BestWin, WorstWin, Gap     float64
	BestEx                     sql.NullFloat64
} {
	out := []struct {
		Dim, BestLabel, WorstLabel string
		BestN, WorstN              int
		BestWin, WorstWin, Gap     float64
		BestEx                     sql.NullFloat64
	}{}
	dims := []struct{ title, expr string }{
		{"五灯红数", `CASE WHEN lamps>=5 THEN '5红' WHEN lamps=4 THEN '4红' ELSE '≤3红' END`},
		{"信号日涨幅", `CASE WHEN limit_up=1 THEN '涨停级' WHEN pct>=5 THEN '5-9.5%' WHEN pct>=0 THEN '0-5%' ELSE '收跌' END`},
		{"换手率", `CASE WHEN turnover>=15 THEN '≥15%' WHEN turnover>=8 THEN '8-15%' WHEN turnover>0 THEN '<8%' ELSE '缺失' END`},
		{"控盘度", `CASE WHEN kongpan>=60 THEN '≥60' WHEN kongpan>=45 THEN '45-60' ELSE '<45' END`},
		{"短线能量", `CASE WHEN strong_count>=2 THEN '≥2次' WHEN strong_count=1 THEN '1次' ELSE '0次' END`},
		{"上影线", `CASE WHEN upper_shadow<=1 THEN '≤1%' WHEN upper_shadow<=4 THEN '1-4%' ELSE '>4%' END`},
		{"250日回撤", `CASE WHEN drawdown>=55 THEN '≥55%' WHEN drawdown>=38 THEN '38-55%' ELSE '<38%' END`},
		{"爆量倍数", `CASE WHEN vol_mult>=8 THEN '≥8倍' WHEN vol_mult>=3 THEN '3-8倍' WHEN vol_mult>=1.5 THEN '1.5-3倍' ELSE '<1.5倍' END`},
		{"其他策略共振", `CASE WHEN cross_hits<0 THEN '不可得' WHEN cross_hits=0 THEN '独家' WHEN cross_hits=1 THEN '1策略共振' ELSE '≥2策略共振' END`},
		{"主力资金", `CASE WHEN main_pct<=-900 THEN '缺失' WHEN main_pct>0 THEN '净流入' ELSE '净流出' END`},
	}
	for _, d := range dims {
		rows, err := s.db.Query(fmt.Sprintf(`SELECT %s b, COUNT(*),
			AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END),
			AVG(CASE WHEN bench_ret>-900 THEN next_close_ret-bench_ret-%f ELSE NULL END)
			FROM strategy_learn_samples WHERE strategy_id=? GROUP BY b`, d.expr, oversoldCostPct), strategyID)
		if err != nil {
			continue
		}
		type bs struct {
			label string
			n     int
			win   float64
			ex    sql.NullFloat64
		}
		stats := []bs{}
		for rows.Next() {
			var b bs
			if rows.Scan(&b.label, &b.n, &b.win, &b.ex) == nil {
				if b.label != "不可得" && b.label != "缺失" {
					stats = append(stats, b)
				}
			}
		}
		rows.Close()
		if len(stats) < 2 {
			continue
		}
		best, worst := stats[0], stats[0]
		for _, b := range stats[1:] {
			if b.win > best.win {
				best = b
			}
			if b.win < worst.win {
				worst = b
			}
		}
		gap := best.win - worst.win
		if best.n >= 20 && worst.n >= 20 && gap >= 15 {
			out = append(out, struct {
				Dim, BestLabel, WorstLabel string
				BestN, WorstN              int
				BestWin, WorstWin, Gap     float64
				BestEx                     sql.NullFloat64
			}{d.title, best.label, worst.label, best.n, worst.n, best.win, worst.win, gap, best.ex})
		}
	}
	return out
}

// GenerateEvolutionProposals 为全部策略生成待确认进化提案(去重:已有同键调优或未决提案则跳过)。
// 返回新提案条数与摘要。
func (s *HistoryService) GenerateEvolutionProposals(nameOf func(string) string) (int, string) {
	if s == nil || s.db == nil {
		return 0, ""
	}
	if err := s.initEvolveSchema(); err != nil {
		return 0, err.Error()
	}
	ids := []string{}
	rows, err := s.db.Query(`SELECT DISTINCT strategy_id FROM strategy_learn_samples`)
	if err != nil {
		return 0, err.Error()
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	added := 0
	var summary strings.Builder
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, sid := range ids {
		for _, f := range s.learnFindings(sid) {
			var exists int
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM strategy_score_tunings WHERE strategy_id=? AND dim=? AND bucket=?`, sid, f.Dim, f.BestLabel).Scan(&exists)
			if exists > 0 {
				continue
			}
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM strategy_evolution_proposals WHERE strategy_id=? AND dim=? AND bucket=? AND status='pending'`, sid, f.Dim, f.BestLabel).Scan(&exists)
			if exists > 0 {
				continue
			}
			action := "observe"
			delta := 0.0
			if evolvableFeatures[f.Dim] {
				action = "score"
				delta = f.Gap / 5
				if delta < 2 {
					delta = 2
				}
				if delta > 6 {
					delta = 6
				}
				delta = float64(int(delta*2+0.5)) / 2 // 0.5步进
			}
			exTxt := "超额均值不可得"
			if f.BestEx.Valid {
				exTxt = fmt.Sprintf("该桶扣费超额均值 %+.2f%%/笔", f.BestEx.Float64)
			}
			finding := fmt.Sprintf("[%s] 红盘率 %.0f%%(n=%d) vs [%s] %.0f%%(n=%d),差 %.0f 个点;%s。统计口径=次日收盘,相关性非因果",
				f.BestLabel, f.BestWin, f.BestN, f.WorstLabel, f.WorstWin, f.WorstN, f.Gap, exTxt)
			res, err := s.db.Exec(`INSERT INTO strategy_evolution_proposals(strategy_id,dim,bucket,delta,finding,action,status,created_at)
				VALUES(?,?,?,?,?,?,'pending',?)`, sid, f.Dim, f.BestLabel, delta, finding, action, now)
			if err != nil {
				continue
			}
			pid, _ := res.LastInsertId()
			added++
			name := sid
			if nameOf != nil && nameOf(sid) != "" {
				name = nameOf(sid)
			}
			if action == "score" {
				fmt.Fprintf(&summary, "#%d %s·%s[%s] 建议评分+%.1f — %s\n", pid, name, f.Dim, f.BestLabel, delta, finding)
			} else {
				fmt.Fprintf(&summary, "#%d %s·%s[%s] 观察级(涉硬门槛/不可当场计算,不自动应用) — %s\n", pid, name, f.Dim, f.BestLabel, finding)
			}
		}
	}
	return added, summary.String()
}

// ListEvolutionProposals 文本列出提案(status 空=全部)。
func (s *HistoryService) ListEvolutionProposals(status string, nameOf func(string) string) string {
	if s == nil || s.db == nil {
		return "未就绪"
	}
	if err := s.initEvolveSchema(); err != nil {
		return err.Error()
	}
	q := `SELECT id,strategy_id,dim,bucket,delta,finding,action,status,created_at FROM strategy_evolution_proposals`
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT 50`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("【策略进化提案】(确认: DecideEvolutionProposal(id,true/false);score=确认后评分层自动应用)\n")
	n := 0
	for rows.Next() {
		var p EvolutionProposal
		if rows.Scan(&p.ID, &p.StrategyID, &p.Dim, &p.Bucket, &p.Delta, &p.Finding, &p.Action, &p.Status, &p.CreatedAt) != nil {
			continue
		}
		n++
		name := p.StrategyID
		if nameOf != nil && nameOf(p.StrategyID) != "" {
			name = nameOf(p.StrategyID)
		}
		act := fmt.Sprintf("评分+%.1f", p.Delta)
		if p.Action != "score" {
			act = "观察级"
		}
		fmt.Fprintf(&b, "#%d [%s] %s·%s[%s] %s — %s (%s)\n", p.ID, p.Status, name, p.Dim, p.Bucket, act, p.Finding, p.CreatedAt[:16])
	}
	if n == 0 {
		b.WriteString("(暂无)\n")
	}
	return b.String()
}

// DecideEvolutionProposal 用户裁决:accept 且 action=score → 写入调优表即刻生效(版本+1);否则标记 rejected。
func (s *HistoryService) DecideEvolutionProposal(id int64, accept bool) string {
	if s == nil || s.db == nil {
		return "未就绪"
	}
	if err := s.initEvolveSchema(); err != nil {
		return err.Error()
	}
	var p EvolutionProposal
	err := s.db.QueryRow(`SELECT id,strategy_id,dim,bucket,delta,finding,action,status FROM strategy_evolution_proposals WHERE id=?`, id).
		Scan(&p.ID, &p.StrategyID, &p.Dim, &p.Bucket, &p.Delta, &p.Finding, &p.Action, &p.Status)
	if err != nil {
		return fmt.Sprintf("提案#%d 不存在", id)
	}
	if p.Status != "pending" {
		return fmt.Sprintf("提案#%d 已处理过(%s),不可重复裁决", id, p.Status)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if !accept {
		_, _ = s.db.Exec(`UPDATE strategy_evolution_proposals SET status='rejected', decided_at=? WHERE id=?`, now, id)
		return fmt.Sprintf("提案#%d 已拒绝", id)
	}
	if p.Action != "score" {
		_, _ = s.db.Exec(`UPDATE strategy_evolution_proposals SET status='applied', decided_at=? WHERE id=?`, now, id)
		return fmt.Sprintf("提案#%d 为观察级,已标记确认(不涉自动评分变更;若要动硬门槛请单独指示)", id)
	}
	var ver int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM strategy_score_tunings WHERE strategy_id=?`, p.StrategyID).Scan(&ver)
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO strategy_score_tunings(strategy_id,dim,bucket,delta,evidence,confirmed_at,version)
		VALUES(?,?,?,?,?,?,?)`, p.StrategyID, p.Dim, p.Bucket, p.Delta, p.Finding, now, ver); err != nil {
		return err.Error()
	}
	_, _ = s.db.Exec(`UPDATE strategy_evolution_proposals SET status='applied', decided_at=? WHERE id=?`, now, id)
	return fmt.Sprintf("提案#%d 已确认并生效:%s %s[%s] 评分+%.1f(调优v%d)。扫描结果会在理由里标注本条来源;应用前后表现进入日报对比", id, p.StrategyID, p.Dim, p.Bucket, p.Delta, ver)
}

// LoadScoreTunings 某策略已确认的评分调优(delta>0 才需应用)。
func (s *HistoryService) LoadScoreTunings(strategyID string) []ScoreTuning {
	out := []ScoreTuning{}
	if s == nil || s.db == nil {
		return out
	}
	if err := s.initEvolveSchema(); err != nil {
		return out
	}
	rows, err := s.db.Query(`SELECT strategy_id,dim,bucket,delta,evidence,confirmed_at,version FROM strategy_score_tunings WHERE strategy_id=? AND delta>0`, strategyID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t ScoreTuning
		if rows.Scan(&t.StrategyID, &t.Dim, &t.Bucket, &t.Delta, &t.Evidence, &t.ConfirmedAt, &t.Version) == nil {
			out = append(out, t)
		}
	}
	return out
}

// CountCrossHitsOn 某股某日被其他策略(排除 exclude)选中的家数——供扫描时算"共振"调优桶。
func (s *HistoryService) CountCrossHitsOn(symbol, date, exclude string) int {
	if s == nil || s.db == nil {
		return 0
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT strategy_id) FROM strategy_scan_picks WHERE signal_date=? AND stock_code=? AND strategy_id<>?`,
		date, strings.ToLower(strings.TrimSpace(symbol)), exclude).Scan(&n)
	return n
}
