package services

// 选股体感练习·取数层(2026-07-25 用户提:"先只给所选日 2:50 选出的股票,我先选,再按次日分时手动卖出")。
// 铁律:本文件的返回**绝不含任何次日价格**——练习要盲,后端就不发未来数据,连 devtools 都翻不出来。
// 次日行情由前端另取「分时磁带」(GetStockIntraday)逐点揭示,揭到哪看到哪。
// 取数直读留痕表(当日 14:50 真实选出的票),不走复盘接口——复盘有"当日模拟仓倒灌则只看这批"的收窄
// 规则,会把 23 只真实留痕缩成 4 只手工加的仓(2026-07-23 实例),练习必须看全量。

import (
	"database/sql"
	"encoding/json"

	"github.com/run-bigpig/jcp/internal/models"
)

// paperBackfillTag 倒灌行的指纹:模拟持仓补写进留痕的行,不是策略真选的,练习要排除。
const paperBackfillTag = "模拟持仓留痕"

func decodeJSONList(s sql.NullString) []string {
	if !s.Valid || s.String == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		return nil
	}
	return out
}

// LoadDrillPicks 读某策略某日的真实留痕(排除倒灌行),按 rank 排。
func (s *HistoryService) LoadDrillPicks(strategyID, signalDate string) ([]models.DrillPick, error) {
	out := []models.DrillPick{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(`
		SELECT stock_code, COALESCE(stock_name,''), COALESCE(industry,''), COALESCE(rank,0),
		       COALESCE(score,0), COALESCE(price,0), COALESCE(change_pct,0),
		       COALESCE(turnover,0), COALESCE(main_pct,0), COALESCE(main_net,0),
		       COALESCE((SELECT d.close_price FROM stock_daily d
		                 WHERE d.stock_code=strategy_scan_picks.stock_code AND d.trade_date=strategy_scan_picks.signal_date), 0),
		       triggers_json, reasons_json, risks_json
		FROM strategy_scan_picks
		WHERE strategy_id=? AND signal_date=?
		  AND COALESCE(triggers_json,'') NOT LIKE '%' || ? || '%'
		ORDER BY CASE WHEN COALESCE(rank,0)>0 THEN rank ELSE 9999 END, score DESC`,
		strategyID, signalDate, paperBackfillTag)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p models.DrillPick
		var trig, reas, risk sql.NullString
		if err := rows.Scan(&p.Symbol, &p.Name, &p.Industry, &p.Rank, &p.Score, &p.Price,
			&p.ChangePct, &p.Turnover, &p.MainPct, &p.MainNet, &p.SignalClose, &trig, &reas, &risk); err != nil {
			continue
		}
		p.Triggers = decodeJSONList(trig)
		p.Reasons = decodeJSONList(reas)
		p.Risks = decodeJSONList(risk)
		out = append(out, p)
	}
	// 历史重算写进来的行 stock_name 是空的(replayed=1 那批),用最新交易日的代码→名字表回填,
	// 否则练习卡片只剩"sh600280 · 百货"没有股票名,根本没法凭手感挑。
	missing := false
	for i := range out {
		if out[i].Name == "" {
			missing = true
			break
		}
	}
	if missing {
		if nameMap := s.latestNameMap(); len(nameMap) > 0 {
			for i := range out {
				if out[i].Name == "" {
					out[i].Name = nameMap[out[i].Symbol]
				}
			}
		}
	}
	return out, nil
}

// DrillDatePair 一个可练信号日 + 它的次日(次日决定去哪找分时磁带)。
type DrillDatePair struct{ Date, NextDate string }

// DrillDatesFor 列出该策略「有真实留痕、且次日行情已出」的信号日(新→旧),连次日一起给。
// 次日已出 = 库里存在比它更晚的交易日;不做这层过滤的话,选到最新交易日会拿不到磁带。
func (s *HistoryService) DrillDatesFor(strategyID string, limit int) []DrillDatePair {
	out := []DrillDatePair{}
	if s == nil || s.db == nil {
		return out
	}
	if limit <= 0 {
		limit = 60
	}
	var maxDate string
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(trade_date),'') FROM stock_daily`).Scan(&maxDate)
	if maxDate == "" {
		return out
	}
	rows, err := s.db.Query(`
		SELECT signal_date,
		       COALESCE((SELECT MIN(d.trade_date) FROM stock_daily d WHERE d.trade_date > signal_date), '')
		FROM strategy_scan_picks
		WHERE strategy_id=? AND signal_date < ?
		  AND COALESCE(triggers_json,'') NOT LIKE '%' || ? || '%'
		GROUP BY signal_date ORDER BY signal_date DESC LIMIT ?`,
		strategyID, maxDate, paperBackfillTag, limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p DrillDatePair
		if rows.Scan(&p.Date, &p.NextDate) == nil && p.NextDate != "" {
			out = append(out, p)
		}
	}
	return out
}

// LatestTradeDate 库里最新的交易日(走 idx_stock_daily_date 覆盖索引,毫秒级)。
func (s *HistoryService) LatestTradeDate() string {
	if s == nil || s.db == nil {
		return ""
	}
	var d sql.NullString
	_ = s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily`).Scan(&d)
	if d.Valid {
		return d.String
	}
	return ""
}

// NextTradeDateAfter 库里 date 之后的第一个交易日("次日",停牌跨日自然顺延)。
func (s *HistoryService) NextTradeDateAfter(date string) string {
	if s == nil || s.db == nil {
		return ""
	}
	var d sql.NullString
	_ = s.db.QueryRow(`SELECT MIN(trade_date) FROM stock_daily WHERE trade_date > ?`, date).Scan(&d)
	if d.Valid {
		return d.String
	}
	return ""
}

// PrevCloseOn 取某股某交易日的收盘价,用作次日分时的「昨收」基准(分时磁带自带 pct,此值仅兜底)。
func (s *HistoryService) PrevCloseOn(code, date string) float64 {
	if s == nil || s.db == nil {
		return 0
	}
	var v sql.NullFloat64
	_ = s.db.QueryRow(`SELECT close_price FROM stock_daily WHERE stock_code=? AND trade_date=?`, code, date).Scan(&v)
	if v.Valid {
		return v.Float64
	}
	return 0
}
