package services

// 体感练习磁带回补计划(2026-07-25):练习只需要「被策略选中的票在次日」的分时,
// 不必全市场硬补 —— 实测 2026-07-06 之前的全部留痕票也才 1089 个股日(全市场一个月是 9 万+)。
// 这里从留痕表反推出 (次日, 代码集) 计划,交给既有 BackfillMinuteHistory 跑(断点续跑,已有的自动跳过)。

import "time"

// DrillTapePlan 列出 [start,end] 内信号日对应的「次日 + 该日要补的票」。
// 只算真实留痕(排除模拟持仓倒灌行);同一次日的多策略选票自动合并去重。
func (s *HistoryService) DrillTapePlan(start, end string) []MinuteHistoryPlanDay {
	out := []MinuteHistoryPlanDay{}
	if s == nil || s.db == nil {
		return out
	}
	rows, err := s.db.Query(`
		SELECT (SELECT MIN(d.trade_date) FROM stock_daily d WHERE d.trade_date > p.signal_date) AS next_date,
		       p.stock_code
		FROM strategy_scan_picks p
		WHERE p.signal_date >= ? AND p.signal_date <= ?
		  AND COALESCE(p.triggers_json,'') NOT LIKE '%' || ? || '%'
		GROUP BY next_date, p.stock_code
		ORDER BY next_date`, start, end, paperBackfillTag)
	if err != nil {
		return out
	}
	defer rows.Close()
	order := []string{}
	byDate := map[string][]string{}
	for rows.Next() {
		var date, code string
		if rows.Scan(&date, &code) != nil || date == "" || code == "" {
			continue
		}
		if _, ok := byDate[date]; !ok {
			order = append(order, date)
		}
		byDate[date] = append(byDate[date], code)
	}
	for _, d := range order {
		out = append(out, MinuteHistoryPlanDay{Date: d, Codes: byDate[d]})
	}
	return out
}

// ⚠️绝不要对 minute_history 按 trade_date 做聚合/范围查询:它的主键是
// (stock_code, trade_date, minute),**trade_date 不是最左列**,连 MIN(trade_date) 都得扫全表
// (千万行冷盘几分钟不回,RPC 直接挂死;GetHistoryMinuteCoverage 早就栽过同一个跟头)。
// 按 (code,date) 点查走主键前缀很快,所以「取某只票某天的磁带」没问题,**只有按日汇总不行**。
// 因此覆盖情况改用下面这张小登记表回答:回补成功一天就登记一天,查它是毫秒级。

func (s *IntradayService) initDrillTapeDays() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS drill_tape_days (
		trade_date TEXT PRIMARY KEY,
		stocks INTEGER,
		filled_at TEXT
	)`)
	return err
}

// MarkDrillTapeDays 登记「这些交易日的练习磁带已补」。
func (s *IntradayService) MarkDrillTapeDays(plan []MinuteHistoryPlanDay) {
	if s == nil || s.db == nil {
		return
	}
	if err := s.initDrillTapeDays(); err != nil {
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, d := range plan {
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO drill_tape_days(trade_date, stocks, filled_at) VALUES(?,?,?)`,
			d.Date, len(d.Codes), now)
	}
}

// DrillTapeDays 已登记的练习磁带日集合(表极小,直接全取)。
func (s *IntradayService) DrillTapeDays() map[string]bool {
	out := map[string]bool{}
	if s == nil || s.db == nil {
		return out
	}
	if err := s.initDrillTapeDays(); err != nil {
		return out
	}
	rows, err := s.db.Query(`SELECT trade_date FROM drill_tape_days`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			out[d] = true
		}
	}
	return out
}
