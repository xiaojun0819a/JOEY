package services

// 全策略共享自学习(2026-07-24,由超跌起爆11管线泛化,用户:"所有策略都要像11一样自学习"):
// 同一套 特征提取(股票状态通用数学)/次日结果配对/扣费超额口径/分桶纪律,按 strategy_id 分仓统计。
// 纪律与11一致:硬门槛永不自动改;结论必须带样本量;两侧n≥20且差≥15pp才算达标发现;
// 口径=扣费超额(次日收益−当日全市场等权中位−0.2%往返成本),裸红盘率仅对照。

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *HistoryService) initStrategyLearnSchema() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS strategy_learn_samples (
		strategy_id TEXT NOT NULL,
		signal_date TEXT NOT NULL,
		symbol TEXT NOT NULL,
		name TEXT,
		score REAL, pct REAL, vol_mult REAL, vol_dry REAL,
		drawdown REAL, rise_from_base REAL, lamps INTEGER, kongpan REAL,
		strong_count INTEGER, upper_shadow REAL, limit_up INTEGER,
		turnover REAL, ma5_dist REAL, ma10_dist REAL,
		cross_hits INTEGER DEFAULT -1, lhb INTEGER DEFAULT -1, lhb_net REAL DEFAULT 0,
		news_count INTEGER DEFAULT -1, main_pct REAL DEFAULT -999, bench_ret REAL DEFAULT -999,
		next_open_ret REAL, next_close_ret REAL, next_high_ret REAL,
		created_at TEXT,
		PRIMARY KEY(strategy_id, signal_date, symbol)
	)`); err != nil {
		return err
	}
	return nil
}

// MigrateOversoldLearnToShared 把11的旧学习库并入共享表(幂等,一次性)。
func (s *HistoryService) MigrateOversoldLearnToShared() int {
	if s == nil || s.db == nil {
		return 0
	}
	if err := s.initStrategyLearnSchema(); err != nil {
		return 0
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO strategy_learn_samples
		(strategy_id, signal_date, symbol, name, score, pct, vol_mult, vol_dry, drawdown, rise_from_base,
		 lamps, kongpan, strong_count, upper_shadow, limit_up, turnover, ma5_dist, ma10_dist,
		 cross_hits, lhb, lhb_net, news_count, main_pct, bench_ret,
		 next_open_ret, next_close_ret, next_high_ret, created_at)
		SELECT 'oversold-ignite-v1', signal_date, symbol, name, score, pct, vol_mult, vol_dry, drawdown, rise_from_base,
		 lamps, kongpan, strong_count, upper_shadow, limit_up, turnover, ma5_dist, ma10_dist,
		 COALESCE(cross_hits,-1), COALESCE(lhb,-1), COALESCE(lhb_net,0), COALESCE(news_count,-1), COALESCE(main_pct,-999), COALESCE(bench_ret,-999),
		 next_open_ret, next_close_ret, next_high_ret, created_at
		FROM oversold_learn_samples`)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// LearnableStrategyIDs 留痕里出现过的全部策略。
func (s *HistoryService) LearnableStrategyIDs() []string {
	out := []string{}
	if s == nil || s.db == nil {
		return out
	}
	rows, err := s.db.Query(`SELECT DISTINCT strategy_id FROM strategy_scan_picks ORDER BY strategy_id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// StrategyLearnPending 某策略已有留痕、次日已定型、尚未入共享学习库的 (signal_date, symbol)。
func (s *HistoryService) StrategyLearnPending(strategyID string) []struct{ SignalDate, Symbol, Name string } {
	out := []struct{ SignalDate, Symbol, Name string }{}
	if s == nil || s.db == nil {
		return out
	}
	if err := s.initStrategyLearnSchema(); err != nil {
		return out
	}
	rows, err := s.db.Query(`
		SELECT p.signal_date, p.stock_code, COALESCE(p.stock_name,'')
		FROM strategy_scan_picks p
		LEFT JOIN strategy_learn_samples l ON l.strategy_id=p.strategy_id AND l.signal_date=p.signal_date AND l.symbol=p.stock_code
		WHERE p.strategy_id=? AND l.symbol IS NULL
		  AND EXISTS (SELECT 1 FROM stock_daily d WHERE d.stock_code=p.stock_code AND d.trade_date>p.signal_date AND d.close_price>0)
		ORDER BY p.signal_date`, strategyID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r struct{ SignalDate, Symbol, Name string }
		if rows.Scan(&r.SignalDate, &r.Symbol, &r.Name) == nil {
			out = append(out, r)
		}
	}
	return out
}

// InsertStrategyLearnSample 落一条共享学习样本(幂等)。
func (s *HistoryService) InsertStrategyLearnSample(strategyID string, m OversoldLearnSample) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("db not ready")
	}
	if err := s.initStrategyLearnSchema(); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO strategy_learn_samples
		(strategy_id,signal_date,symbol,name,score,pct,vol_mult,vol_dry,drawdown,rise_from_base,lamps,kongpan,strong_count,upper_shadow,limit_up,turnover,ma5_dist,ma10_dist,cross_hits,lhb,lhb_net,news_count,main_pct,bench_ret,next_open_ret,next_close_ret,next_high_ret,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		strategyID, m.SignalDate, m.Symbol, m.Name, m.Score, m.Pct, m.VolMult, m.VolDry, m.Drawdown, m.RiseFromBase,
		m.Lamps, m.Kongpan, m.StrongCount, m.UpperShadow, m.LimitUp, m.Turnover, m.MA5Dist, m.MA10Dist,
		m.CrossHits, m.Lhb, m.LhbNet, m.NewsCount, m.MainPct, m.BenchRet,
		m.NextOpenRet, m.NextCloseRet, m.NextHighRet, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

// SetStrategyLearnNewsCount 写快讯匹配数(仅最新交易日现场匹配,前向积累)。
func (s *HistoryService) SetStrategyLearnNewsCount(strategyID, signalDate, symbol string, n int) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE strategy_learn_samples SET news_count=? WHERE strategy_id=? AND signal_date=? AND symbol=?`, n, strategyID, signalDate, symbol)
}

// RefreshStrategyLearnFactors 全表刷新系统信息流因子(共振排除样本自身策略;龙虎榜按覆盖期;主力取当日真值)。
func (s *HistoryService) RefreshStrategyLearnFactors() {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE strategy_learn_samples SET cross_hits=(
		SELECT COUNT(DISTINCT p.strategy_id) FROM strategy_scan_picks p
		WHERE p.signal_date=strategy_learn_samples.signal_date AND p.stock_code=strategy_learn_samples.symbol
		  AND p.strategy_id<>strategy_learn_samples.strategy_id)`)
	var lhbMin, lhbMax sql.NullString
	_ = s.db.QueryRow(`SELECT MIN(trade_date), MAX(trade_date) FROM lhb_daily`).Scan(&lhbMin, &lhbMax)
	if lhbMin.Valid && lhbMax.Valid {
		_, _ = s.db.Exec(`UPDATE strategy_learn_samples SET
			lhb=CASE WHEN EXISTS(SELECT 1 FROM lhb_daily d WHERE d.trade_date=signal_date AND d.code=symbol) THEN 1 ELSE 0 END,
			lhb_net=COALESCE((SELECT d.net_buy FROM lhb_daily d WHERE d.trade_date=signal_date AND d.code=symbol),0)
			WHERE signal_date BETWEEN ? AND ?`, lhbMin.String, lhbMax.String)
		_, _ = s.db.Exec(`UPDATE strategy_learn_samples SET lhb=-1, lhb_net=0 WHERE signal_date NOT BETWEEN ? AND ?`, lhbMin.String, lhbMax.String)
	}
	_, _ = s.db.Exec(`UPDATE strategy_learn_samples SET main_pct=COALESCE(
		(SELECT d.main_pct FROM stock_daily d WHERE d.stock_code=symbol AND d.trade_date=signal_date AND d.main_pct IS NOT NULL), -999)`)
}

// RefreshStrategyLearnBenchmarks 给缺基准样本回填 bench_ret(全市场等权中位,与11同口径)。
func (s *HistoryService) RefreshStrategyLearnBenchmarks() {
	if s == nil || s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT strategy_id, signal_date, symbol FROM strategy_learn_samples WHERE bench_ret<=-900`)
	if err != nil {
		return
	}
	type sk struct{ id, d, sym string }
	pend := []sk{}
	for rows.Next() {
		var r sk
		if rows.Scan(&r.id, &r.d, &r.sym) == nil {
			pend = append(pend, r)
		}
	}
	rows.Close()
	if len(pend) == 0 {
		return
	}
	medCache := map[string]float64{}
	filled := 0
	for _, p := range pend {
		var nd sql.NullString
		_ = s.db.QueryRow(`SELECT MIN(trade_date) FROM stock_daily WHERE stock_code=? AND trade_date>? AND close_price>0`, p.sym, p.d).Scan(&nd)
		if !nd.Valid || nd.String == "" {
			continue
		}
		med, ok := medCache[nd.String]
		if !ok {
			m, good := s.marketMedianPct(nd.String)
			if !good {
				continue
			}
			med = m
			medCache[nd.String] = m
		}
		if _, err := s.db.Exec(`UPDATE strategy_learn_samples SET bench_ret=? WHERE strategy_id=? AND signal_date=? AND symbol=?`, med, p.id, p.d, p.sym); err == nil {
			filled++
		}
	}
	if filled > 0 {
		historyLog.Info("共享学习库基准回填 %d 笔", filled)
	}
}

// BuildStrategyLearnReportFor 单策略学习日报(与11同引擎/同纪律)。返回(文本,样本数,红盘率)。
func (s *HistoryService) BuildStrategyLearnReportFor(strategyID, strategyName string) (string, int, float64) {
	if s == nil || s.db == nil {
		return "学习库未就绪", 0, 0
	}
	if err := s.initStrategyLearnSchema(); err != nil {
		return "学习库未就绪: " + err.Error(), 0, 0
	}
	var total int
	var winRate, avgRet, avgHigh sql.NullFloat64
	_ = s.db.QueryRow(`SELECT COUNT(*), AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END), AVG(next_close_ret), AVG(next_high_ret)
		FROM strategy_learn_samples WHERE strategy_id=?`, strategyID).Scan(&total, &winRate, &avgRet, &avgHigh)
	if total == 0 {
		return fmt.Sprintf("【%s·学习日报】暂无样本(留痕次日定型后自动入库;历史留痕会在每日学习时一次性回填)", strategyName), 0, 0
	}
	var exN int
	var exAvg, exWin sql.NullFloat64
	_ = s.db.QueryRow(`SELECT COUNT(*), AVG(next_close_ret-bench_ret-?), AVG(CASE WHEN next_close_ret-bench_ret-?>0 THEN 100.0 ELSE 0 END)
		FROM strategy_learn_samples WHERE strategy_id=? AND bench_ret>-900`, oversoldCostPct, oversoldCostPct, strategyID).Scan(&exN, &exAvg, &exWin)

	dims := []struct {
		title string
		expr  string
	}{
		{"五灯红数", `CASE WHEN lamps>=5 THEN '5红' WHEN lamps=4 THEN '4红' ELSE '≤3红' END`},
		{"信号日涨幅", `CASE WHEN limit_up=1 THEN '涨停级' WHEN pct>=5 THEN '5-9.5%' WHEN pct>=0 THEN '0-5%' ELSE '收跌' END`},
		{"换手率", `CASE WHEN turnover>=15 THEN '≥15%' WHEN turnover>=8 THEN '8-15%' WHEN turnover>0 THEN '<8%' ELSE '缺失' END`},
		{"控盘度", `CASE WHEN kongpan>=60 THEN '≥60' WHEN kongpan>=45 THEN '45-60' ELSE '<45' END`},
		{"短线能量", `CASE WHEN strong_count>=2 THEN '≥2次' WHEN strong_count=1 THEN '1次' ELSE '0次' END`},
		{"上影线", `CASE WHEN upper_shadow<=1 THEN '≤1%' WHEN upper_shadow<=4 THEN '1-4%' ELSE '>4%' END`},
		{"距MA5", `CASE WHEN ma5_dist>=15 THEN '≥15%' WHEN ma5_dist>=8 THEN '8-15%' WHEN ma5_dist>=0 THEN '0-8%' ELSE '线下' END`},
		{"250日回撤", `CASE WHEN drawdown>=55 THEN '≥55%' WHEN drawdown>=38 THEN '38-55%' ELSE '<38%' END`},
		{"爆量倍数", `CASE WHEN vol_mult>=8 THEN '≥8倍' WHEN vol_mult>=3 THEN '3-8倍' WHEN vol_mult>=1.5 THEN '1.5-3倍' ELSE '<1.5倍' END`},
		{"其他策略共振", `CASE WHEN cross_hits<0 THEN '不可得' WHEN cross_hits=0 THEN '独家' WHEN cross_hits=1 THEN '1策略共振' ELSE '≥2策略共振' END`},
		{"龙虎榜", `CASE WHEN lhb=1 AND lhb_net>0 THEN '上榜净买' WHEN lhb=1 THEN '上榜净卖' WHEN lhb=0 THEN '未上榜' ELSE '不可得' END`},
		{"主力资金", `CASE WHEN main_pct<=-900 THEN '缺失' WHEN main_pct>0 THEN '净流入' ELSE '净流出' END`},
		{"消息面", `CASE WHEN news_count<0 THEN '不可得' WHEN news_count=0 THEN '无快讯' ELSE '有快讯' END`},
		{"AI报告概率", `CASE WHEN ai_prob<0 OR ai_prob IS NULL THEN '不可得' WHEN ai_prob>=60 THEN '≥60%' WHEN ai_prob>=50 THEN '50-60%' ELSE '<50%' END`},
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【%s·学习日报】样本 %d 笔 | 次日红盘率 %.1f%% | 次日收盘均值 %+.2f%% | 次日高点均值 %+.2f%%\n",
		strategyName, total, winRate.Float64, avgRet.Float64, avgHigh.Float64)
	if exN > 0 {
		fmt.Fprintf(&b, "★ 扣费超额口径(n=%d):超额均值 %+.2f%%/笔 | 超额胜率 %.1f%%(跑赢当日全市场中位并覆盖0.2%%成本才计胜)\n", exN, exAvg.Float64, exWin.Float64)
	}
	b.WriteString("(达标发现要求两侧样本≥20且红盘率差≥15点;更小样本标注噪声级)\n\n")

	type learnBucketStat struct {
		Label                    string
		N                        int
		WinRate, AvgRet, AvgHigh float64
	}
	type finding struct {
		text string
		gap  float64
	}
	findings := []finding{}
	for _, d := range dims {
		rows, err := s.db.Query(fmt.Sprintf(`SELECT %s b, COUNT(*), AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END), AVG(next_close_ret), AVG(next_high_ret)
			FROM strategy_learn_samples WHERE strategy_id=? GROUP BY b ORDER BY b`, d.expr), strategyID)
		if err != nil {
			continue
		}
		stats := []learnBucketStat{}
		for rows.Next() {
			var st learnBucketStat
			if rows.Scan(&st.Label, &st.N, &st.WinRate, &st.AvgRet, &st.AvgHigh) == nil {
				stats = append(stats, st)
			}
		}
		rows.Close()
		if len(stats) < 2 {
			continue
		}
		fmt.Fprintf(&b, "◆ %s:", d.title)
		for _, st := range stats {
			fmt.Fprintf(&b, "  [%s] n=%d 红盘%.0f%% 均值%+.2f%%;", st.Label, st.N, st.WinRate, st.AvgRet)
		}
		b.WriteString("\n")
		sort.Slice(stats, func(i, j int) bool { return stats[i].WinRate > stats[j].WinRate })
		best, worst := stats[0], stats[len(stats)-1]
		gap := best.WinRate - worst.WinRate
		if best.N >= 20 && worst.N >= 20 && gap >= 15 {
			findings = append(findings, finding{
				text: fmt.Sprintf("%s:[%s]红盘率 %.0f%%(n=%d) vs [%s] %.0f%%(n=%d),差 %.0f 点", d.title, best.Label, best.WinRate, best.N, worst.Label, worst.WinRate, worst.N, gap),
				gap:  gap,
			})
		}
	}
	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool { return findings[i].gap > findings[j].gap })
		b.WriteString("\n★ 达标发现(可作为该策略评分权重调优依据;硬门槛不自动改):\n")
		for i, f := range findings {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, f.text)
		}
	} else {
		b.WriteString("\n★ 暂无达标发现——继续攒样本,别急着下结论。\n")
	}
	// 待确认的进化提案(确认后评分层自动应用)
	if rows, err := s.db.Query(`SELECT id,dim,bucket,delta,action,finding FROM strategy_evolution_proposals WHERE strategy_id=? AND status='pending' ORDER BY id`, strategyID); err == nil {
		first := true
		for rows.Next() {
			var pid int64
			var dim, bucket, action, finding string
			var delta float64
			if rows.Scan(&pid, &dim, &bucket, &delta, &action, &finding) != nil {
				continue
			}
			if first {
				b.WriteString("\n🧬 待确认进化提案(对助手说\"采纳提案#编号\"或在提案列表裁决):\n")
				first = false
			}
			act := fmt.Sprintf("评分+%.1f", delta)
			if action != "score" {
				act = "观察级"
			}
			fmt.Fprintf(&b, "  #%d %s[%s] %s — %s\n", pid, dim, bucket, act, finding)
		}
		rows.Close()
	}
	// 已生效调优的应用后跟踪(样本≥10才展示)
	if rows, err := s.db.Query(`SELECT dim,bucket,delta,version,confirmed_at FROM strategy_score_tunings WHERE strategy_id=? AND delta>0`, strategyID); err == nil {
		for rows.Next() {
			var dim, bucket, cAt string
			var delta float64
			var ver int
			if rows.Scan(&dim, &bucket, &delta, &ver, &cAt) != nil {
				continue
			}
			var nA int
			var wA sql.NullFloat64
			_ = s.db.QueryRow(`SELECT COUNT(*),AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END) FROM strategy_learn_samples WHERE strategy_id=? AND signal_date>?`, strategyID, cAt[:10]).Scan(&nA, &wA)
			if nA >= 10 {
				fmt.Fprintf(&b, "\n⛳ 调优v%d(%s[%s]+%.1f,%s生效)后新样本 %d 笔,红盘率 %.1f%%——持续跟踪,退化会提示回滚\n", ver, dim, bucket, delta, cAt[:10], nA, wA.Float64)
			}
		}
		rows.Close()
	}
	return b.String(), total, winRate.Float64
}

// BuildLearnOverview 全策略学习总览(每策略一行:样本量/红盘率/扣费超额)。
func (s *HistoryService) BuildLearnOverview(nameOf func(string) string) string {
	if s == nil || s.db == nil {
		return "学习库未就绪"
	}
	if err := s.initStrategyLearnSchema(); err != nil {
		return "学习库未就绪"
	}
	rows, err := s.db.Query(`SELECT strategy_id, COUNT(*), AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END),
		SUM(CASE WHEN bench_ret>-900 THEN 1 ELSE 0 END),
		AVG(CASE WHEN bench_ret>-900 THEN next_close_ret-bench_ret-? ELSE NULL END)
		FROM strategy_learn_samples GROUP BY strategy_id ORDER BY COUNT(*) DESC`, oversoldCostPct)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("【全策略学习总览】(扣费超额=次日收益−全市场中位−0.2%成本)\n")
	for rows.Next() {
		var id string
		var n, exN int
		var win, exAvg sql.NullFloat64
		if rows.Scan(&id, &n, &win, &exN, &exAvg) != nil {
			continue
		}
		name := id
		if nameOf != nil {
			if nm := nameOf(id); nm != "" {
				name = nm
			}
		}
		fmt.Fprintf(&b, "  %-10s 样本%4d | 红盘率%5.1f%% | 扣费超额均值 %+6.2f%%/笔 (n=%d)\n", name, n, win.Float64, exAvg.Float64, exN)
	}
	return b.String()
}
