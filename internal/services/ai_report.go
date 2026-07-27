package services

// AI深度诊断报告因子(2026-07-24 用户接入):V5.0模板 docx 的结构化摘录入库,
// 学习库按"报告日在信号日前5天~次日内"匹配为样本因子(ai_fish/ai_prob)。
// 定位=审计这类报告本身的含金量(概率校准):报告说主升浪57%,实际兑现多少,数据说了算。
// 覆盖稀疏是常态(-1=不可得单列分桶),不参与自动进化(扫描时点报告未必存在)。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type AIReportNote struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	ReportDate string  `json:"reportDate"`
	FishIndex  float64 `json:"fishIndex"` // 鱼身指数 0-100
	Prob       float64 `json:"prob"`      // 主升浪概率 %
	Rating     string  `json:"rating"`    // 综合评级(观察池/重点关注…)
	Phase      string  `json:"phase"`     // 阶段判断
}

func (s *HistoryService) initAIReportSchema() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS ai_report_notes (
		code TEXT NOT NULL,
		report_date TEXT NOT NULL,
		name TEXT,
		fish_index REAL,
		prob REAL,
		rating TEXT,
		phase TEXT,
		created_at TEXT,
		PRIMARY KEY(code, report_date)
	)`); err != nil {
		return err
	}
	for _, col := range []string{"ai_fish REAL DEFAULT -1", "ai_prob REAL DEFAULT -1"} {
		if _, err := s.db.Exec("ALTER TABLE strategy_learn_samples ADD COLUMN " + col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				historyLog.Warn("学习库加AI因子列失败 %s: %v", col, err)
			}
		}
	}
	return nil
}

// UpsertAIReport 报告摘录入库,并即时回填匹配样本的因子。返回(是否新建, 回填样本数)。
func (s *HistoryService) UpsertAIReport(n AIReportNote) (bool, int, error) {
	if s == nil || s.db == nil {
		return false, 0, fmt.Errorf("db not ready")
	}
	if err := s.initAIReportSchema(); err != nil {
		return false, 0, err
	}
	code := strings.ToLower(strings.TrimSpace(n.Code))
	if code == "" || n.ReportDate == "" {
		return false, 0, fmt.Errorf("code/report_date 必填")
	}
	res, err := s.db.Exec(`INSERT OR REPLACE INTO ai_report_notes(code,report_date,name,fish_index,prob,rating,phase,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, code, n.ReportDate, n.Name, n.FishIndex, n.Prob, n.Rating, n.Phase,
		time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return false, 0, err
	}
	created, _ := res.RowsAffected()
	filled := s.RefreshAIReportFactors()
	return created > 0, filled, nil
}

// RefreshAIReportFactors 给缺AI因子的样本回填:取"报告日 ∈ [信号日-5天, 信号日+1天]"内最近一份报告。
// 报告晚于次日(结果已定型后才写)不匹配——时点纪律,防事后诸葛。
func (s *HistoryService) RefreshAIReportFactors() int {
	if s == nil || s.db == nil {
		return 0
	}
	if err := s.initAIReportSchema(); err != nil {
		return 0
	}
	res, err := s.db.Exec(`UPDATE strategy_learn_samples SET
		ai_fish = COALESCE((SELECT r.fish_index FROM ai_report_notes r WHERE r.code=symbol
			AND r.report_date <= date(signal_date, '+1 day') AND r.report_date >= date(signal_date, '-5 day')
			ORDER BY r.report_date DESC LIMIT 1), -1),
		ai_prob = COALESCE((SELECT r.prob FROM ai_report_notes r WHERE r.code=symbol
			AND r.report_date <= date(signal_date, '+1 day') AND r.report_date >= date(signal_date, '-5 day')
			ORDER BY r.report_date DESC LIMIT 1), -1)
		WHERE ai_prob IS NULL OR ai_prob <= -1 OR EXISTS (SELECT 1 FROM ai_report_notes r2 WHERE r2.code=symbol)`)
	if err != nil {
		historyLog.Warn("AI报告因子回填失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	var matched int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM strategy_learn_samples WHERE ai_prob>=0`).Scan(&matched)
	_ = n
	return matched
}

// AIReportCalibration 概率校准审计:按报告"主升浪概率"分桶看实际次日表现——报告含金量的直接证据。
func (s *HistoryService) AIReportCalibration() string {
	if s == nil || s.db == nil {
		return "未就绪"
	}
	if err := s.initAIReportSchema(); err != nil {
		return err.Error()
	}
	var total int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM ai_report_notes`).Scan(&total)
	var b strings.Builder
	fmt.Fprintf(&b, "【AI报告概率校准】报告库 %d 份\n", total)
	rows, err := s.db.Query(`SELECT CASE WHEN ai_prob>=60 THEN '概率≥60%' WHEN ai_prob>=50 THEN '50-60%' ELSE '<50%' END b,
		COUNT(*), AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END), AVG(next_close_ret),
		AVG(CASE WHEN bench_ret>-900 THEN next_close_ret-bench_ret-0.2 ELSE NULL END)
		FROM strategy_learn_samples WHERE ai_prob>=0 GROUP BY b ORDER BY b DESC`)
	if err != nil {
		return b.String() + err.Error()
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var label string
		var cnt int
		var win, avg float64
		var ex sql.NullFloat64
		if rows.Scan(&label, &cnt, &win, &avg, &ex) != nil {
			continue
		}
		n++
		fmt.Fprintf(&b, "  [%s] 样本%d 次日红盘%.0f%% 均值%+.2f%% 扣费超额%+.2f%%\n", label, cnt, win, avg, ex.Float64)
	}
	if n == 0 {
		b.WriteString("  (还没有报告匹配到学习样本;继续攒,n≥20 才有结论)\n")
	} else {
		b.WriteString("  纪律:两侧n≥20且差≥15点才算报告有分辨力;更小样本一律噪声级。\n")
	}
	return b.String()
}
