package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// StrategyReplayFunc 策略历史重算函数:给定信号日与topN,按与线上扫描同口径的规则从本地历史库重算入选名单。
// 返回 (picks, note):note 非空表示重算已执行(即使 picks 为空——空=真·当日未符合);两者都空=该策略不支持重算。
type StrategyReplayFunc func(signalDate string, topN int) ([]models.StrategyPickSnapshot, string)

// SetStrategyReplay 注册某策略的历史重算器(app 层在启动时注入,复盘缺留痕时自动调用)。
func (s *HistoryService) SetStrategyReplay(strategyID string, fn StrategyReplayFunc) {
	if s == nil || fn == nil || strategyID == "" {
		return
	}
	if s.strategyReplays == nil {
		s.strategyReplays = map[string]StrategyReplayFunc{}
	}
	s.strategyReplays[strategyID] = fn
}

// markPicksReplayed 把某策略某日的留痕标记为重算补写(排除出留痕系统上线日判定)。
func (s *HistoryService) markPicksReplayed(strategyID string, signalDate string) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE strategy_scan_picks SET replayed=1 WHERE strategy_id=? AND signal_date=?`, strategyID, signalDate)
}

func (s *HistoryService) SaveStrategyPicks(strategyID string, strategyName string, signalDate string, scannedAt string, picks []models.StrategyPickSnapshot) error {
	if s == nil || s.db == nil || strings.TrimSpace(strategyID) == "" || len(picks) == 0 {
		return nil
	}
	signalDate = normalizeReviewDate(signalDate, time.Now().Format("2006-01-02"))
	if scannedAt == "" {
		scannedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 该日名单若曾是"重算补写"(replayed=1),重写后必须保留标记——复盘的字段补齐步骤会整日重写,
	// 标记一旦丢失,"留痕最早日"被重算数据拉前,其他策略的老日期又被误报"当日未符合"(2026-07-22 实测踩坑)。
	wasReplayed := 0
	_ = tx.QueryRow(`SELECT COALESCE(MAX(replayed),0) FROM strategy_scan_picks WHERE strategy_id=? AND signal_date=?`, strategyID, signalDate).Scan(&wasReplayed)
	// 留痕语义=「当日最后一次扫描的名单」:先清掉当天旧集合再写。
	// 否则同日多次扫描会并集累积(掉榜的旧票不删),复盘"入选N"虚高、胜率样本被盘中噪声污染
	// (实测:某日多次扫描各出10只,留痕并成22只)。
	if _, err := tx.Exec(`DELETE FROM strategy_scan_picks WHERE strategy_id=? AND signal_date=?`, strategyID, signalDate); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO strategy_scan_picks
		(strategy_id, strategy_name, signal_date, scanned_at, stock_code, stock_name, rank, price, change_pct, score, industry, amount, turnover, main_net, main_pct, main_source, triggers_json, reasons_json, risks_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	for idx, pick := range picks {
		code := strings.ToLower(strings.TrimSpace(pick.Symbol))
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(pick.StrategyID))
		}
		if code == "" {
			continue
		}
		rank := pick.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		triggersJSON := marshalStringSlice(pick.Triggers)
		reasonsJSON := marshalStringSlice(pick.Reasons)
		risksJSON := marshalStringSlice(pick.RiskFlags)
		if _, err := stmt.Exec(
			strategyID,
			chooseText(strategyName, pick.StrategyName),
			signalDate,
			scannedAt,
			code,
			pick.Name,
			rank,
			safeReviewFloat(pick.Price),
			safeReviewFloat(pick.ChangePercent),
			safeReviewFloat(pick.Score),
			pick.Industry,
			safeReviewFloat(pick.Amount),
			safeReviewFloat(pick.TurnoverRate),
			safeReviewFloat(pick.MainNetInflow),
			safeReviewFloat(pick.MainNetInflowPct),
			pick.MainFlowSource,
			triggersJSON,
			reasonsJSON,
			risksJSON,
			now,
		); err != nil {
			return err
		}
	}
	if wasReplayed == 1 {
		if _, err := tx.Exec(`UPDATE strategy_scan_picks SET replayed=1 WHERE strategy_id=? AND signal_date=?`, strategyID, signalDate); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateStrategyPicksInPlace 逐行更新已有留痕的补齐字段(评分/名称/行业/价量/资金),行不存在则 INSERT OR IGNORE 补插;
// 全程不动同日其他行——专供复盘补齐回写(它拿到的常是"模拟仓当天新增"过滤后的子集,整日重写=毁真实留痕)。
func (s *HistoryService) UpdateStrategyPicksInPlace(strategyID, strategyName, signalDate string, picks []models.StrategyPickSnapshot) error {
	if s == nil || s.db == nil || strings.TrimSpace(strategyID) == "" || len(picks) == 0 {
		return nil
	}
	signalDate = normalizeReviewDate(signalDate, time.Now().Format("2006-01-02"))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	upd, err := tx.Prepare(`UPDATE strategy_scan_picks SET
		strategy_name=COALESCE(NULLIF(?,''),strategy_name), stock_name=COALESCE(NULLIF(?,''),stock_name),
		price=?, change_pct=?, score=?, industry=COALESCE(NULLIF(?,''),industry),
		amount=?, turnover=?, main_net=?, main_pct=?, main_source=COALESCE(NULLIF(?,''),main_source),
		triggers_json=?, reasons_json=?, risks_json=?
		WHERE strategy_id=? AND signal_date=? AND stock_code=?`)
	if err != nil {
		return err
	}
	defer upd.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	for idx, pick := range picks {
		code := normalizeReviewSymbol(pick.Symbol)
		if code == "" {
			continue
		}
		res, err := upd.Exec(
			chooseText(strategyName, pick.StrategyName), pick.Name,
			safeReviewFloat(pick.Price), safeReviewFloat(pick.ChangePercent), safeReviewFloat(pick.Score), pick.Industry,
			safeReviewFloat(pick.Amount), safeReviewFloat(pick.TurnoverRate), safeReviewFloat(pick.MainNetInflow), safeReviewFloat(pick.MainNetInflowPct), pick.MainFlowSource,
			marshalStringSlice(pick.Triggers), marshalStringSlice(pick.Reasons), marshalStringSlice(pick.RiskFlags),
			strategyID, signalDate, code,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 { // 行不存在(纯模拟仓倒灌票)→补插,不影响他行
			rank := pick.Rank
			if rank <= 0 {
				rank = idx + 1
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO strategy_scan_picks
				(strategy_id, strategy_name, signal_date, scanned_at, stock_code, stock_name, rank, price, change_pct, score, industry, amount, turnover, main_net, main_pct, main_source, triggers_json, reasons_json, risks_json, created_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				strategyID, chooseText(strategyName, pick.StrategyName), signalDate, signalDate, code, pick.Name, rank,
				safeReviewFloat(pick.Price), safeReviewFloat(pick.ChangePercent), safeReviewFloat(pick.Score), pick.Industry,
				safeReviewFloat(pick.Amount), safeReviewFloat(pick.TurnoverRate), safeReviewFloat(pick.MainNetInflow), safeReviewFloat(pick.MainNetInflowPct), pick.MainFlowSource,
				marshalStringSlice(pick.Triggers), marshalStringSlice(pick.Reasons), marshalStringSlice(pick.RiskFlags), now,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *HistoryService) SaveMissingStrategyPicks(strategyID string, strategyName string, signalDate string, scannedAt string, picks []models.StrategyPickSnapshot) error {
	if s == nil || s.db == nil || strings.TrimSpace(strategyID) == "" || len(picks) == 0 {
		return nil
	}
	signalDate = normalizeReviewDate(signalDate, time.Now().Format("2006-01-02"))
	if scannedAt == "" {
		scannedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO strategy_scan_picks
		(strategy_id, strategy_name, signal_date, scanned_at, stock_code, stock_name, rank, price, change_pct, score, industry, amount, turnover, main_net, main_pct, main_source, triggers_json, reasons_json, risks_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	for idx, pick := range picks {
		code := normalizeReviewSymbol(pick.Symbol)
		if code == "" {
			continue
		}
		rank := pick.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		if _, err := stmt.Exec(
			strategyID,
			chooseText(strategyName, pick.StrategyName),
			signalDate,
			scannedAt,
			code,
			pick.Name,
			rank,
			safeReviewFloat(pick.Price),
			safeReviewFloat(pick.ChangePercent),
			safeReviewFloat(pick.Score),
			pick.Industry,
			safeReviewFloat(pick.Amount),
			safeReviewFloat(pick.TurnoverRate),
			safeReviewFloat(pick.MainNetInflow),
			safeReviewFloat(pick.MainNetInflowPct),
			pick.MainFlowSource,
			marshalStringSlice(pick.Triggers),
			marshalStringSlice(pick.Reasons),
			marshalStringSlice(pick.RiskFlags),
			now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *HistoryService) CountStrategyPicks(strategyID string, signalDate string) int {
	if s == nil || s.db == nil {
		return 0
	}
	strategyID = strings.TrimSpace(strategyID)
	signalDate = normalizeReviewDate(signalDate, "")
	if strategyID == "" || signalDate == "" {
		return 0
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM strategy_scan_picks WHERE strategy_id=? AND signal_date=?`, strategyID, signalDate).Scan(&count)
	return count
}

func (s *HistoryService) BuildStrategyNextDayReview(req models.StrategyReviewRequest, news []models.StrategyReviewNews) models.StrategyReviewResult {
	now := time.Now().Format("2006-01-02 15:04:05")
	strategyID := strings.TrimSpace(req.StrategyID)
	result := models.StrategyReviewResult{
		StrategyID:      strategyID,
		StrategyName:    chooseText(req.StrategyName, strategyID),
		GeneratedAt:     now,
		Items:           []models.StrategyReviewItem{},
		News:            trimReviewNews(news, 5),
		Optimization:    []string{},
		DataSourceNotes: []string{"扫描入选记录来自本地 strategy_scan_picks 留痕；若选股日存在模拟仓当天新增，则复盘只统计这批新加股票", "今日K线优先实时日K；资金优先全A快照，缺失时用本地采集表兜底"},
	}
	if s == nil || s.db == nil {
		result.Warning = "历史库未就绪，无法读取昨日策略入选记录"
		return result
	}
	if strategyID == "" {
		result.Warning = "未指定策略"
		return result
	}
	reviewDate := normalizeReviewDate(req.ReviewDate, time.Now().Format("2006-01-02"))
	requestedSignalDate := normalizeReviewDate(req.SignalDate, "")
	signalDate := requestedSignalDate
	if signalDate == "" {
		signalDate = s.latestSignalDateBefore(strategyID, reviewDate)
	}
	if signalDate == "" {
		result.ReviewDate = reviewDate
		result.Warning = "没有找到该策略的历史扫描留痕；请先运行一次该策略，次日收盘后再复盘"
		return result
	}
	result.SignalDate = signalDate
	result.ReviewDate = reviewDate

	// 复盘日必须是交易日:前端默认"选股日+1自然日"会落在周末/节假日(如周五选股→周六复盘),
	// 该日无行情,收益全空。非交易日一律纠为选股日的次一交易日,并在警示中注明。
	if reviewDate != "" {
		var cnt int
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM (SELECT 1 FROM stock_daily WHERE trade_date=? LIMIT 1)`, reviewDate).Scan(&cnt)
		today := time.Now().Format("2006-01-02")
		if cnt == 0 && reviewDate < today { // 今天/未来日无行情属正常(尚未收盘),只纠历史上的非交易日
			if nd := s.nextTradeDateAfter(signalDate); nd != "" && nd != reviewDate {
				result.Warning = combineReviewWarnings(result.Warning,
					fmt.Sprintf("复盘日 %s 不是交易日(周末/节假日),已自动调整为选股日的次一交易日 %s", reviewDate, nd))
				reviewDate = nd
				result.ReviewDate = nd
			}
		}
	}

	picks, err := s.loadStrategyPicks(strategyID, signalDate)
	if err != nil {
		result.Warning = "读取策略扫描留痕失败：" + err.Error()
		return result
	}
	replayAttempted := false
	// 留痕若全是"模拟仓倒灌"(app层在服务前按当日模拟仓补的占位),说明当日真实扫描名单缺失——
	// 支持重算的策略先把全量真实信号算出来存档(学习库/全量复盘要用),倒灌票不在名单内的追加保留。
	// 否则倒灌抢占后重算永不触发,当日只剩你手动入手那几只(2026-07-24 超跌起爆11 4/17 实测)。
	if len(picks) > 0 {
		isPaperBackfill := func(p models.StrategyPickSnapshot) bool {
			for _, t := range p.Triggers {
				if t == "模拟持仓留痕" {
					return true
				}
			}
			return p.MainFlowSource == "paper-position" // 兜底:补齐回写可能已把 main_source 覆写成行情源
		}
		allPaper := true
		for _, p := range picks {
			if !isPaperBackfill(p) {
				allPaper = false
				break
			}
		}
		if allPaper {
			if fn := s.strategyReplays[strategyID]; fn != nil {
				full, note := fn(signalDate, 30)
				if len(full) > 0 {
					if s.SaveStrategyPicks(strategyID, result.StrategyName, signalDate, now, full) == nil {
						s.markPicksReplayed(strategyID, signalDate)
					}
					inFull := map[string]bool{}
					for _, f := range full {
						inFull[strings.ToLower(f.Symbol)] = true
					}
					merged := full
					for _, p := range picks {
						if !inFull[strings.ToLower(p.Symbol)] {
							merged = append(merged, p)
						}
					}
					picks = merged
					result.DataSourceNotes = append(result.DataSourceNotes, fmt.Sprintf("当日仅有模拟仓倒灌留痕,已用历史重算恢复全量信号 %d 只(倒灌票不在名单内的已并入)", len(full)))
				}
				if note != "" {
					result.DataSourceNotes = append(result.DataSourceNotes, note)
				}
				replayAttempted = true
			}
		}
	}
	if len(picks) == 0 {
		rebuilt, note := s.rebuildStrategyPicksForReview(strategyID, result.StrategyName, signalDate)
		if note != "" {
			result.Warning = combineReviewWarnings(result.Warning, note)
		}
		if len(rebuilt) > 0 {
			picks = rebuilt
			if s.SaveStrategyPicks(strategyID, result.StrategyName, signalDate, now, rebuilt) == nil {
				s.markPicksReplayed(strategyID, signalDate) // 重算补写≠实盘留痕,排除出上线日判定
			}
		}
		replayAttempted = note != "" || len(rebuilt) > 0
	}
	// 用户明确选了日期却无留痕:**不再自动跳到别的日子**(用户连续两次被"选7-1跳6-25"硌到)。
	// 严进策略空仓很常见,空仓也是信息——如实报空仓,并列出前后最近有票的日期供改选。
	if len(picks) == 0 && requestedSignalDate != "" {
		// 留痕上线(2026-06-08)之前的日期根本没有扫描记录——那是"无从判定",不是"未符合"。
		// 说成"未符合"会误导用户以为策略扫过没中(2026-07-21 三倍量5 五月实例)。仅波段能用历史库重算(上面已试过)。
		if trailStart := s.earliestTrailDate(); !replayAttempted && trailStart != "" && signalDate < trailStart {
			msg := fmt.Sprintf("%s:选股留痕系统 %s 起才有记录,%s 早于此——当日无扫描记录,无法判定是否符合(不是「未符合」)", result.StrategyName, trailStart, signalDate)
			if first := s.firstSignalDateOf(strategyID); first != "" {
				msg += fmt.Sprintf(";该策略最早可复盘日期: %s", first)
			}
			result.Warning = combineReviewWarnings(result.Warning, msg)
			return result
		}
		prev := s.latestSignalDateBefore(strategyID, signalDate)
		next := s.nextSignalDateAfter(strategyID, signalDate)
		msg := fmt.Sprintf("%s %s 当日未符合(无股票入选)", result.StrategyName, signalDate)
		if prev != "" && prev < signalDate {
			msg += fmt.Sprintf("；上一次入选: %s", prev)
		}
		if next != "" {
			msg += fmt.Sprintf("；下一次入选: %s", next)
		}
		result.Warning = combineReviewWarnings(result.Warning, msg)
		return result
	}
	// 波段策略:旧留痕不带「重点布局」标时,按"截至选股日"的数据补算(30/60分靠行情源近40个交易日窗口,更早的补不了)
	if strategyID == "wave-v1" && len(picks) > 0 {
		if added := s.BackfillKeyLayoutMarks(result.SignalDate, picks); added > 0 {
			result.DataSourceNotes = append(result.DataSourceNotes, fmt.Sprintf("重点布局为按选股日历史数据补算(%d只):日K本地截断+30/60分行情源截断,与实时扫描同一套判定", added))
		}
	}
	if len(picks) == 0 {
		result.Warning = combineReviewWarnings(result.Warning, fmt.Sprintf("%s 没有找到 %s 的入选记录", result.StrategyName, signalDate))
		return result
	}
	if symbols := normalizeReviewSymbolSet(req.ReviewSymbols); len(symbols) > 0 {
		filtered := filterStrategyPicksByReviewSymbols(picks, symbols)
		if len(filtered) == 0 {
			result.Warning = combineReviewWarnings(result.Warning, fmt.Sprintf("%s 在 %s 有当天新增记录，但策略留痕未匹配到这些股票", result.StrategyName, signalDate))
			return result
		}
		if len(filtered) != len(picks) {
			result.DataSourceNotes = append(result.DataSourceNotes, fmt.Sprintf("本次复盘已按选股日%s当天新增股票过滤：%d/%d只", signalDate, len(filtered), len(picks)))
		} else {
			result.DataSourceNotes = append(result.DataSourceNotes, fmt.Sprintf("本次复盘已确认全部%d只均为选股日%s当天新增股票", len(filtered), signalDate))
		}
		picks = filtered
	}
	var enrichNotes []string
	picks, enrichNotes = s.enrichStrategyPicksForReview(strategyID, result.StrategyName, signalDate, picks)
	if len(enrichNotes) > 0 {
		result.DataSourceNotes = append(result.DataSourceNotes, enrichNotes...)
	}
	result.PickCount = len(picks)
	if picks[0].StrategyName != "" {
		result.StrategyName = picks[0].StrategyName
	}

	snapshotMap, snapshotWarn := s.loadReviewSnapshotMap(reviewDate)
	if snapshotWarn != "" {
		result.DataSourceNotes = append(result.DataSourceNotes, snapshotWarn)
	}
	result.Market = s.buildReviewMarket(reviewDate, snapshotMap)

	// 复盘日行情可用性检查:复盘日还没开盘/收盘时(如凌晨看当天),各票会回落用最近数据,
	// 收益退化成假 0%——必须明确警示,否则看起来像"胜率0%"的假统计。
	// 判定:本地历史无该日行 且 实时快照也不是该日的 → 该日尚无行情。
	var reviewDayRows int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM (SELECT 1 FROM stock_daily WHERE trade_date=? LIMIT 200)`, reviewDate).Scan(&reviewDayRows)
	if reviewDayRows < 100 {
		snapIsReviewDay := false
		for _, row := range snapshotMap {
			if len(row.UpdateTime) >= 10 && row.UpdateTime[:10] == reviewDate {
				snapIsReviewDay = true
			}
			break
		}
		if !snapIsReviewDay {
			result.Warning = combineReviewWarnings(result.Warning,
				fmt.Sprintf("⚠️复盘日 %s 尚无行情(未开盘或未收盘),下方收益/胜率暂无意义,请该日收盘后再看", reviewDate))
			result.ReviewPending = true
		}
	}

	totalClose := 0.0
	totalHigh := 0.0
	winCount := 0
	hit3Count := 0
	mainNegative := 0
	closePoor := 0
	for _, pick := range picks {
		item := s.buildReviewItem(pick, reviewDate, snapshotMap, news)
		if strings.TrimSpace(item.ReviewDate) == "" {
			item.ReviewDate = reviewDate
			item.Outcome = "数据不足"
			item.Suggestions = append(item.Suggestions, "今日K线/快照不足，先检查盘后历史采集是否完成")
		}
		if item.CloseReturnPercent > 0 {
			winCount++
		}
		if item.HighReturnPercent >= 3 {
			hit3Count++
		}
		if item.MainNetInflow < 0 {
			mainNegative++
		}
		if item.CloseReturnPercent < -2 {
			closePoor++
		}
		if item.Close != 0 {
			result.ReviewedCount++
			totalClose += item.CloseReturnPercent
			totalHigh += item.HighReturnPercent
		}
		result.Items = append(result.Items, item)
	}
	if result.ReviewedCount > 0 {
		result.AvgCloseReturn = roundReview2(totalClose / float64(result.ReviewedCount))
		result.AvgHighReturn = roundReview2(totalHigh / float64(result.ReviewedCount))
		result.WinRate = roundReview2(float64(winCount) * 100 / float64(result.ReviewedCount))
		result.Hit3Rate = roundReview2(float64(hit3Count) * 100 / float64(result.ReviewedCount))
	}
	result.Optimization = buildStrategyOptimization(result, mainNegative, closePoor)
	if result.ReviewedCount == 0 {
		result.Warning = combineReviewWarnings(result.Warning, "还没有可用的次日/今日收盘数据，盘中只能做临时跟踪，收盘后再复盘更准")
	}
	return result
}

func (s *HistoryService) enrichStrategyPicksForReview(strategyID string, strategyName string, signalDate string, picks []models.StrategyPickSnapshot) ([]models.StrategyPickSnapshot, []string) {
	if s == nil || s.db == nil || len(picks) == 0 {
		return picks, nil
	}
	industryMap := loadIndustryMap()
	var strategyMap map[string]models.StrategyPickSnapshot
	strategyMapLoaded := false
	changed := false
	scoreFilled := 0
	metaFilled := 0
	scoreMissing := 0

	for i := range picks {
		picks[i].Symbol = normalizeReviewSymbol(picks[i].Symbol)
		if picks[i].Symbol == "" {
			continue
		}
		before := picks[i]

		if reviewTextMissing(picks[i].Name) || picks[i].Price <= 0 || picks[i].Amount <= 0 || picks[i].TurnoverRate <= 0 {
			facts, ok := s.loadStrategyPickDailyFacts(picks[i].Symbol, signalDate)
			if ok {
				if reviewTextMissing(picks[i].Name) && facts.Name != "" {
					picks[i].Name = facts.Name
				}
				if picks[i].Price <= 0 && facts.Price > 0 {
					picks[i].Price = facts.Price
				}
				if picks[i].ChangePercent == 0 && facts.ChangePercent != 0 {
					picks[i].ChangePercent = facts.ChangePercent
				}
				if picks[i].Amount <= 0 && facts.Amount > 0 {
					picks[i].Amount = facts.Amount
				}
				if picks[i].TurnoverRate <= 0 && facts.TurnoverRate > 0 {
					picks[i].TurnoverRate = facts.TurnoverRate
				}
				if picks[i].MainNetInflow == 0 && facts.MainNetInflow != 0 {
					picks[i].MainNetInflow = facts.MainNetInflow
				}
				if picks[i].MainNetInflowPct == 0 && facts.MainNetInflowPct != 0 {
					picks[i].MainNetInflowPct = facts.MainNetInflowPct
				}
				if strings.TrimSpace(picks[i].MainFlowSource) == "" && facts.MainFlowSource != "" {
					picks[i].MainFlowSource = facts.MainFlowSource
				}
			}
		}

		if reviewTextMissing(picks[i].Industry) {
			if industry := industryMap[picks[i].Symbol]; !reviewTextMissing(industry) {
				picks[i].Industry = industry
			}
		}

		if needsStrategyScoreEnrichment(picks[i]) {
			if !strategyMapLoaded {
				strategyMap = s.buildStrategyPickEnrichmentMap(strategyID, strategyName, signalDate)
				strategyMapLoaded = true
			}
			if enriched, ok := strategyMap[picks[i].Symbol]; ok && enriched.Score > 0 {
				picks[i] = mergeReviewPickSnapshot(picks[i], enriched)
				scoreFilled++
			} else {
				scoreMissing++
			}
		}

		if !sameStrategyPickForReview(before, picks[i]) {
			changed = true
			if before.Score == picks[i].Score {
				metaFilled++
			}
		}
	}

	notes := []string{}
	if scoreFilled > 0 {
		notes = append(notes, fmt.Sprintf("已按选股日策略规则补齐%d只历史留痕评分，复盘评分与选股扫描评分口径一致", scoreFilled))
	}
	if metaFilled > 0 {
		notes = append(notes, fmt.Sprintf("已补齐%d只股票的名称/行业/价格资金等基础留痕字段", metaFilled))
	}
	if scoreMissing > 0 {
		notes = append(notes, fmt.Sprintf("%d只模拟仓倒灌记录缺少原始扫描分，且当前本地历史无法按该策略还原；已保留为评分暂缺，不伪造分数", scoreMissing))
	}
	if changed {
		// ⚠️必须逐行 UPDATE,绝不能整日 DELETE+重写:复盘常在"模拟仓当天新增"规则下只拿到子集,
		// 整日重写会把真实扫描留痕吃掉(2026-07-24 实测:14:47 的17只被4只子集覆盖,学习库同毁)。
		if err := s.UpdateStrategyPicksInPlace(strategyID, strategyName, signalDate, picks); err != nil {
			notes = append(notes, "复盘留痕补齐后回写失败："+err.Error())
		}
	}
	return picks, notes
}

type strategyPickDailyFacts struct {
	Name             string
	Price            float64
	ChangePercent    float64
	Amount           float64
	TurnoverRate     float64
	MainNetInflow    float64
	MainNetInflowPct float64
	MainFlowSource   string
}

func (s *HistoryService) loadStrategyPickDailyFacts(symbol string, signalDate string) (strategyPickDailyFacts, bool) {
	var facts strategyPickDailyFacts
	if s == nil || s.db == nil || symbol == "" || signalDate == "" {
		return facts, false
	}
	var name, source sql.NullString
	var close, pct, amount, turnover, mainNet, mainPct sql.NullFloat64
	err := s.db.QueryRow(`SELECT stock_name, close_price, pct_change, amount, turnover, main_net, main_pct, main_source
		FROM stock_daily WHERE stock_code=? AND trade_date=?`, symbol, signalDate).
		Scan(&name, &close, &pct, &amount, &turnover, &mainNet, &mainPct, &source)
	if err != nil {
		return facts, false
	}
	if name.Valid {
		facts.Name = name.String
	}
	if close.Valid {
		facts.Price = close.Float64
	}
	if pct.Valid {
		facts.ChangePercent = pct.Float64
	}
	if amount.Valid {
		facts.Amount = amount.Float64
	}
	if turnover.Valid {
		facts.TurnoverRate = turnover.Float64
	}
	if mainNet.Valid {
		facts.MainNetInflow = mainNet.Float64
	}
	if mainPct.Valid {
		facts.MainNetInflowPct = mainPct.Float64
	}
	if source.Valid {
		facts.MainFlowSource = source.String
	}
	return facts, true
}

func (s *HistoryService) buildStrategyPickEnrichmentMap(strategyID string, strategyName string, signalDate string) map[string]models.StrategyPickSnapshot {
	out := map[string]models.StrategyPickSnapshot{}
	switch strings.TrimSpace(strategyID) {
	case "wave-v1":
		res := s.ScanWaveCandidatesOnDate(signalDate, 500, false)
		for idx, item := range res.Items {
			pick := waveCandidateToReviewPick(strategyID, chooseText(strategyName, "波段策略 1.0"), signalDate, idx, item, "复盘按选股日波段1.0规则补齐评分")
			out[pick.Symbol] = pick
		}
	case "lowbuy-v1":
		items, asOf, _ := s.ScanLowBuyOnDate(signalDate, 500, 1.5)
		for idx, item := range items {
			pick := lowBuyItemToReviewPick(strategyID, chooseText(strategyName, "低吸选股策略1"), chooseText(asOf, signalDate), idx, item, "复盘按选股日低吸1规则补齐评分")
			out[pick.Symbol] = pick
		}
	case "taillazy-v2":
		items, asOf, _ := s.ScanTailLazyOnDate(signalDate, 500)
		for idx, item := range items {
			pick := lowBuyItemToReviewPick(strategyID, chooseText(strategyName, "低吸尾盘策略2"), chooseText(asOf, signalDate), idx, item, "复盘按选股日尾盘2规则补齐评分")
			out[pick.Symbol] = pick
		}
	}
	return out
}

func waveCandidateToReviewPick(strategyID string, strategyName string, signalDate string, idx int, item models.WaveCandidate, note string) models.StrategyPickSnapshot {
	reasons := append([]string{}, item.Reasons...)
	if note != "" {
		reasons = append(reasons, note)
	}
	if item.Phase != "" {
		reasons = append(reasons, "阶段："+item.Phase)
	}
	triggers := []string{}
	if note != "" {
		triggers = append(triggers, "历史补算")
	}
	if item.EatFish {
		triggers = append(triggers, "吃鱼身")
	}
	if item.MainOpenFish {
		triggers = append(triggers, "开仓吃鱼")
	}
	if item.RelaxedIgnite {
		triggers = append(triggers, "异动现主力进")
	}
	if item.StrictIgnite {
		triggers = append(triggers, "异动起爆")
	}
	if item.StrongSignal {
		triggers = append(triggers, fmt.Sprintf("%s信号", item.Level))
	}
	if item.GZ {
		triggers = append(triggers, "五灯共振")
	}
	return models.StrategyPickSnapshot{
		StrategyID:       strategyID,
		StrategyName:     strategyName,
		SignalDate:       signalDate,
		ScannedAt:        signalDate,
		Symbol:           normalizeReviewSymbol(item.Code),
		Name:             item.Name,
		Rank:             idx + 1,
		Price:            item.Price,
		Score:            item.Score,
		MainNetInflowPct: item.Kongpan,
		MainFlowSource:   "wave-kongpan-proxy",
		Triggers:         triggers,
		Reasons:          reasons,
		RiskFlags:        item.Risks,
	}
}

func lowBuyItemToReviewPick(strategyID string, strategyName string, signalDate string, idx int, item models.LowBuyScannerItem, note string) models.StrategyPickSnapshot {
	reasons := append([]string{}, item.Reasons...)
	if note != "" {
		reasons = append(reasons, note)
	}
	return models.StrategyPickSnapshot{
		StrategyID:       strategyID,
		StrategyName:     strategyName,
		SignalDate:       signalDate,
		ScannedAt:        signalDate,
		Symbol:           normalizeReviewSymbol(item.Symbol),
		Name:             item.Name,
		Rank:             idx + 1,
		Price:            item.Price,
		ChangePercent:    item.ChangePercent,
		Score:            item.Score,
		Industry:         item.Industry,
		Amount:           item.Amount,
		TurnoverRate:     item.TurnoverRate,
		MainNetInflow:    item.MainNetInflow,
		MainNetInflowPct: item.MainNetInflowRatio,
		MainFlowSource:   chooseText(item.MainFlowSource, "history-replay"),
		Triggers:         item.Triggers,
		Reasons:          reasons,
		RiskFlags:        item.RiskFlags,
	}
}

func mergeReviewPickSnapshot(base models.StrategyPickSnapshot, enriched models.StrategyPickSnapshot) models.StrategyPickSnapshot {
	if enriched.StrategyName != "" {
		base.StrategyName = enriched.StrategyName
	}
	if enriched.Rank > 0 {
		base.Rank = enriched.Rank
	}
	if enriched.Name != "" {
		base.Name = enriched.Name
	}
	if enriched.Price > 0 {
		base.Price = enriched.Price
	}
	if enriched.ChangePercent != 0 {
		base.ChangePercent = enriched.ChangePercent
	}
	if enriched.Score > 0 {
		base.Score = enriched.Score
	}
	if !reviewTextMissing(enriched.Industry) {
		base.Industry = enriched.Industry
	}
	if enriched.Amount > 0 {
		base.Amount = enriched.Amount
	}
	if enriched.TurnoverRate > 0 {
		base.TurnoverRate = enriched.TurnoverRate
	}
	if enriched.MainNetInflow != 0 {
		base.MainNetInflow = enriched.MainNetInflow
	}
	if enriched.MainNetInflowPct != 0 {
		base.MainNetInflowPct = enriched.MainNetInflowPct
	}
	if strings.TrimSpace(enriched.MainFlowSource) != "" {
		base.MainFlowSource = enriched.MainFlowSource
	}
	if len(enriched.Triggers) > 0 {
		base.Triggers = enriched.Triggers
	}
	if len(enriched.Reasons) > 0 {
		base.Reasons = enriched.Reasons
	}
	if len(enriched.RiskFlags) > 0 {
		base.RiskFlags = enriched.RiskFlags
	}
	return base
}

func needsStrategyScoreEnrichment(pick models.StrategyPickSnapshot) bool {
	source := strings.ToLower(strings.TrimSpace(pick.MainFlowSource))
	return pick.Score <= 0 || source == "paper-position"
}

func reviewTextMissing(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "未知" || value == "行业未知" || strings.EqualFold(value, "unknown")
}

func sameStrategyPickForReview(a models.StrategyPickSnapshot, b models.StrategyPickSnapshot) bool {
	return a.Symbol == b.Symbol &&
		a.Name == b.Name &&
		a.Rank == b.Rank &&
		a.Price == b.Price &&
		a.ChangePercent == b.ChangePercent &&
		a.Score == b.Score &&
		a.Industry == b.Industry &&
		a.Amount == b.Amount &&
		a.TurnoverRate == b.TurnoverRate &&
		a.MainNetInflow == b.MainNetInflow &&
		a.MainNetInflowPct == b.MainNetInflowPct &&
		a.MainFlowSource == b.MainFlowSource &&
		strings.Join(a.Triggers, "\x00") == strings.Join(b.Triggers, "\x00") &&
		strings.Join(a.Reasons, "\x00") == strings.Join(b.Reasons, "\x00") &&
		strings.Join(a.RiskFlags, "\x00") == strings.Join(b.RiskFlags, "\x00")
}

func normalizeReviewSymbolSet(symbols []string) map[string]bool {
	out := map[string]bool{}
	for _, symbol := range symbols {
		code := normalizeReviewSymbol(symbol)
		if code != "" {
			out[code] = true
		}
	}
	return out
}

func filterStrategyPicksByReviewSymbols(picks []models.StrategyPickSnapshot, symbols map[string]bool) []models.StrategyPickSnapshot {
	if len(symbols) == 0 {
		return picks
	}
	out := make([]models.StrategyPickSnapshot, 0, len(picks))
	for _, pick := range picks {
		code := normalizeReviewSymbol(pick.Symbol)
		if code == "" || !symbols[code] {
			continue
		}
		pick.Symbol = code
		out = append(out, pick)
	}
	return out
}

func normalizeReviewSymbol(symbol string) string {
	s := strings.ToLower(strings.TrimSpace(symbol))
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		if len(parts) == 2 {
			code := strings.TrimSpace(parts[0])
			market := strings.TrimSpace(parts[1])
			if len(code) >= 6 {
				code = code[len(code)-6:]
			}
			if len(code) == 6 && (market == "sh" || market == "sz" || market == "bj") {
				return market + code
			}
		}
	}
	s = strings.ReplaceAll(s, ".", "")
	if len(s) >= 8 && (strings.HasPrefix(s, "sh") || strings.HasPrefix(s, "sz") || strings.HasPrefix(s, "bj")) {
		return s[:2] + s[len(s)-6:]
	}
	if len(s) >= 6 {
		s = s[len(s)-6:]
	}
	if len(s) != 6 {
		return ""
	}
	switch {
	case strings.HasPrefix(s, "6") || strings.HasPrefix(s, "9"):
		return "sh" + s
	case strings.HasPrefix(s, "8") || strings.HasPrefix(s, "4"):
		return "bj" + s
	default:
		return "sz" + s
	}
}

func (s *HistoryService) rebuildStrategyPicksForReview(strategyID string, strategyName string, signalDate string) ([]models.StrategyPickSnapshot, string) {
	switch strategyID {
	case "wave-v1":
		res := s.ScanWaveCandidatesOnDate(signalDate, 10, false)
		if len(res.Items) == 0 {
			return nil, fmt.Sprintf("%s 没有找到 %s 的原始扫描留痕；已用本地历史库补算，但该日波段策略无命中", chooseText(strategyName, "波段策略 1.0"), signalDate)
		}
		picks := make([]models.StrategyPickSnapshot, 0, len(res.Items))
		for idx, item := range res.Items {
			picks = append(picks, waveCandidateToReviewPick(strategyID, chooseText(strategyName, "波段策略 1.0"), signalDate, idx, item, "复盘缺少原始留痕，已用本地历史库按选股日补算"))
		}
		return picks, fmt.Sprintf("%s 没有找到 %s 的原始扫描留痕，已用本地历史库按该日补算 %d 只", chooseText(strategyName, "波段策略 1.0"), signalDate, len(picks))
	default:
		if fn := s.strategyReplays[strategyID]; fn != nil {
			return fn(signalDate, 10)
		}
		return nil, ""
	}
}

// earliestTrailDate 全部策略里最早的一条留痕日期=留痕系统实际上线日(之前的日期天然无记录)。
func (s *HistoryService) earliestTrailDate() string {
	var date string
	_ = s.db.QueryRow(`SELECT MIN(signal_date) FROM strategy_scan_picks WHERE COALESCE(replayed,0)=0`).Scan(&date)
	return date
}

// firstSignalDateOf 某策略最早一条留痕日期(无则空串)。
func (s *HistoryService) firstSignalDateOf(strategyID string) string {
	var date string
	_ = s.db.QueryRow(`SELECT MIN(signal_date) FROM strategy_scan_picks WHERE strategy_id=?`, strategyID).Scan(&date)
	return date
}

func (s *HistoryService) latestSignalDateBefore(strategyID string, reviewDate string) string {
	var date string
	_ = s.db.QueryRow(`SELECT MAX(signal_date) FROM strategy_scan_picks WHERE strategy_id=? AND signal_date < ?`, strategyID, reviewDate).Scan(&date)
	if date == "" {
		_ = s.db.QueryRow(`SELECT MAX(signal_date) FROM strategy_scan_picks WHERE strategy_id=?`, strategyID).Scan(&date)
	}
	return date
}

// nextSignalDateAfter 该策略在指定日之后最近一次留痕日期(无则空串)。
func (s *HistoryService) nextSignalDateAfter(strategyID string, signalDate string) string {
	var date string
	_ = s.db.QueryRow(`SELECT MIN(signal_date) FROM strategy_scan_picks WHERE strategy_id=? AND signal_date > ?`, strategyID, signalDate).Scan(&date)
	return date
}

func (s *HistoryService) loadStrategyPicks(strategyID string, signalDate string) ([]models.StrategyPickSnapshot, error) {
	rows, err := s.db.Query(`SELECT strategy_id, strategy_name, signal_date, scanned_at, stock_code, stock_name, rank, price, change_pct, score, industry, amount, turnover, main_net, main_pct, main_source, triggers_json, reasons_json, risks_json
		FROM strategy_scan_picks WHERE strategy_id=? AND signal_date=? ORDER BY rank ASC, score DESC`, strategyID, signalDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.StrategyPickSnapshot, 0)
	for rows.Next() {
		var pick models.StrategyPickSnapshot
		var triggersJSON, reasonsJSON, risksJSON string
		if err := rows.Scan(
			&pick.StrategyID, &pick.StrategyName, &pick.SignalDate, &pick.ScannedAt,
			&pick.Symbol, &pick.Name, &pick.Rank, &pick.Price, &pick.ChangePercent, &pick.Score,
			&pick.Industry, &pick.Amount, &pick.TurnoverRate, &pick.MainNetInflow, &pick.MainNetInflowPct, &pick.MainFlowSource,
			&triggersJSON, &reasonsJSON, &risksJSON,
		); err != nil {
			return nil, err
		}
		pick.Triggers = unmarshalStringSlice(triggersJSON)
		pick.Reasons = unmarshalStringSlice(reasonsJSON)
		pick.RiskFlags = unmarshalStringSlice(risksJSON)
		out = append(out, pick)
	}
	return out, rows.Err()
}

// loadReviewSnapshotMap 取"复盘日当天"的行情快照。
// ⚠️实时快照永远是**今天**的数据:复盘历史日时绝不能用它,否则换手/成交额/主力资金/涨跌停家数
// 全是今天的,而收盘价却是复盘日的 —— 同一张卡混两个日期,"换手偏高、资金流出"这种结论
// 就建立在错误日期的数据上。历史日一律返回空,让调用方走日线库(fillReviewItemFromDaily)。
// nextTradeDateAfter 返回 date 之后的第一个交易日(以本地日线库有行情的日期为准;无则空串)。
func (s *HistoryService) nextTradeDateAfter(date string) string {
	if s == nil || s.db == nil || date == "" {
		return ""
	}
	var nd sql.NullString
	if err := s.db.QueryRow(`SELECT MIN(trade_date) FROM stock_daily WHERE trade_date > ?`, date).Scan(&nd); err != nil || !nd.Valid {
		return ""
	}
	return nd.String
}

func (s *HistoryService) loadReviewSnapshotMap(reviewDate string) (map[string]ScanSnapshotRow, string) {
	out := map[string]ScanSnapshotRow{}
	if reviewDate != "" && reviewDate != time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") {
		return out, "" // 复盘历史日:全部字段取当日日线库,不碰实时快照
	}
	if s == nil || s.marketService == nil {
		return out, "行情服务不可用，资金字段仅能依赖本地历史采集"
	}
	rows, err := s.marketService.GetAllAStockSnapshot(false)
	if err != nil {
		return out, "全A快照获取失败，资金字段可能缺失：" + err.Error()
	}
	for _, row := range rows {
		out[strings.ToLower(row.Symbol)] = row
	}
	return out, ""
}

func (s *HistoryService) buildReviewMarket(reviewDate string, snapshots map[string]ScanSnapshotRow) models.StrategyReviewMarket {
	market := models.StrategyReviewMarket{ReviewDate: reviewDate}
	// 复盘历史日:实时指数/实时快照都是"今天"的,用它们等于拿今天的大盘去评价那天的选股。
	if len(snapshots) == 0 && reviewDate != "" {
		s.fillReviewMarketFromDaily(&market, reviewDate)
		return market
	}
	if s != nil && s.marketService != nil {
		if indices, err := s.marketService.GetMarketIndices(); err == nil {
			for _, idx := range indices {
				if idx.Code == "sh000001" || strings.Contains(idx.Name, "上证") {
					market.ShPrice = idx.Price
					market.ShChangePercent = idx.ChangePercent
					break
				}
			}
		}
	}
	for _, row := range snapshots {
		market.TotalAmount += row.Amount
		if row.ChangePercent >= 9.8 {
			market.LimitUpCount++
		}
		if row.ChangePercent <= -9.8 {
			market.LimitDownCount++
		}
	}
	switch {
	case market.LimitDownCount >= 50:
		market.Summary = fmt.Sprintf("大盘偏弱，跌停约%d家；策略信号需要降仓位验证", market.LimitDownCount)
	case market.LimitUpCount >= 60 && market.ShChangePercent >= 0:
		market.Summary = fmt.Sprintf("情绪偏强，涨停约%d家；适合观察强势延续", market.LimitUpCount)
	case market.TotalAmount > 0:
		market.Summary = fmt.Sprintf("成交额约%.0f亿，涨停%d家/跌停%d家", market.TotalAmount/1e8, market.LimitUpCount, market.LimitDownCount)
	default:
		market.Summary = "大盘快照不足，无法完整评估市场环境"
	}
	return market
}

// fillReviewMarketFromDaily 用当日日线库还原复盘日的大盘环境(涨跌停家数/两市成交额/上证涨跌)。
func (s *HistoryService) fillReviewMarketFromDaily(market *models.StrategyReviewMarket, reviewDate string) {
	if s == nil || s.db == nil || market == nil {
		return
	}
	var total, up, down sql.NullFloat64
	err := s.db.QueryRow(`SELECT SUM(amount),
		SUM(CASE WHEN pct_change>=9.8 THEN 1 ELSE 0 END),
		SUM(CASE WHEN pct_change<=-9.8 THEN 1 ELSE 0 END)
		FROM stock_daily WHERE trade_date=?`, reviewDate).Scan(&total, &up, &down)
	if err == nil {
		market.TotalAmount = total.Float64
		market.LimitUpCount = int(up.Float64)
		market.LimitDownCount = int(down.Float64)
	}
	// 上证指数:复盘历史日拿不到 —— 本地 stock_daily/archive 都只存个股不含指数,
	// 而 GetKLineData("sh000001") 的返回是坏的(实测日期跑到 429082 年、收盘出现负数)。
	// 实时指数又只代表今天。所以这里**留空**(前端显示 --),不编一个 0% 冒充当日大盘。
	// 好在闸门与结论都不依赖上证:下面的涨跌停家数/两市成交额由当日 stock_daily 聚合而来,
	// 与波段扫描判大盘用的是同一套口径(见 loadMarketStates)。
	switch {
	case market.LimitDownCount >= 50:
		market.Summary = fmt.Sprintf("复盘日大盘偏弱，跌停约%d家；策略信号需要降仓位验证", market.LimitDownCount)
	case market.LimitUpCount >= 60 && market.ShChangePercent >= 0:
		market.Summary = fmt.Sprintf("复盘日情绪偏强，涨停约%d家；适合观察强势延续", market.LimitUpCount)
	case market.TotalAmount > 0:
		market.Summary = fmt.Sprintf("复盘日成交额约%.0f亿，涨停%d家/跌停%d家", market.TotalAmount/1e8, market.LimitUpCount, market.LimitDownCount)
	default:
		market.Summary = "复盘日大盘数据不足(该日行情尚未采集)"
	}
}

func (s *HistoryService) buildReviewItem(pick models.StrategyPickSnapshot, reviewDate string, snapshots map[string]ScanSnapshotRow, news []models.StrategyReviewNews) models.StrategyReviewItem {
	item := models.StrategyReviewItem{
		Symbol:              pick.Symbol,
		Name:                pick.Name,
		Rank:                pick.Rank,
		Industry:            pick.Industry,
		SignalPrice:         pick.Price,
		SignalChangePercent: pick.ChangePercent,
		SignalScore:         pick.Score,
		SignalReasons:       pick.Reasons,
		SignalTriggers:      pick.Triggers,
		SignalRisks:         pick.RiskFlags,
		MainNetInflow:       pick.MainNetInflow,
		MainNetInflowPct:    pick.MainNetInflowPct,
		MainFlowSource:      pick.MainFlowSource,
		News:                filterReviewNewsForStock(news, pick.Symbol, pick.Name, 3),
	}
	klines := s.loadReviewKLines(pick.Symbol, reviewDate)
	if len(klines) > 0 {
		target := klines[len(klines)-1]
		for _, k := range klines {
			d := normalizeReviewDate(k.Time, "")
			if d != "" && d <= reviewDate {
				target = k
			}
			if d == reviewDate {
				target = k
				break
			}
		}
		item.ReviewDate = normalizeReviewDate(target.Time, reviewDate)
		item.Open = target.Open
		item.High = target.High
		item.Low = target.Low
		item.Close = target.Close
		if item.SignalPrice > 0 {
			item.CloseReturnPercent = roundReview2((item.Close/item.SignalPrice - 1) * 100)
			item.HighReturnPercent = roundReview2((item.High/item.SignalPrice - 1) * 100)
		}
		prevClose := previousCloseBefore(klines, item.ReviewDate)
		if prevClose > 0 {
			item.DayChangePercent = roundReview2((item.Close/prevClose - 1) * 100)
		}
		item.KLineSummary = buildKLineSummary(target, prevClose, item.SignalPrice)
	}
	if snap, ok := snapshots[strings.ToLower(pick.Symbol)]; ok {
		if item.Name == "" {
			item.Name = snap.Name
		}
		item.TurnoverRate = snap.TurnoverRate
		item.Amount = snap.Amount
		item.MainNetInflow = snap.MainNetInflow
		item.MainNetInflowPct = snap.MainNetInflowRatio
		item.MainFlowSource = snap.MainFlowSource
	} else if item.ReviewDate != "" {
		s.fillReviewItemFromDaily(&item)
	}
	item.FundSummary = buildFundSummary(item.MainNetInflow, item.MainNetInflowPct, item.MainFlowSource, item.TurnoverRate, item.Amount)
	item.Outcome = buildReviewOutcome(item.CloseReturnPercent, item.HighReturnPercent)
	item.Suggestions = buildItemSuggestions(item)
	return item
}

func (s *HistoryService) loadReviewKLines(symbol string, reviewDate string) []models.KLineData {
	if s == nil {
		return nil
	}
	// 历史复盘日:必须用本地日K截取到 reviewDate。实时45根窗口从今天往回数,复盘日早于窗口时
	// 匹配不到任何bar,曾静默回落用"今天的bar"冒充复盘日行情——收益变成"至今收益"却标成"次日收益"
	// (2026-07-22 锦富技术 -45.13% 幻影实例)。本地也没有的票宁可空着,绝不拿今天的数据顶。
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	if reviewDate != "" && reviewDate < today {
		ks, err := s.LoadKLineDataUntil(symbol, reviewDate, 45)
		if err != nil || len(ks) == 0 {
			return nil
		}
		return ks // LoadKLineDataUntil 已按日期升序且全部 ≤ reviewDate
	}
	if s.marketService == nil {
		return nil
	}
	klines, err := s.marketService.GetKLineData(symbol, "1d", 45)
	if err != nil || len(klines) == 0 {
		return nil
	}
	sort.Slice(klines, func(i, j int) bool {
		return normalizeReviewDate(klines[i].Time, "") < normalizeReviewDate(klines[j].Time, "")
	})
	return klines
}

func (s *HistoryService) fillReviewItemFromDaily(item *models.StrategyReviewItem) {
	if s == nil || s.db == nil || item == nil || item.ReviewDate == "" {
		return
	}
	var mainSource sql.NullString
	var turnover, amount, mainNet, mainPct, pct sql.NullFloat64
	err := s.db.QueryRow(`SELECT turnover, amount, main_net, main_pct, main_source, pct_change FROM stock_daily WHERE stock_code=? AND trade_date=?`, item.Symbol, item.ReviewDate).
		Scan(&turnover, &amount, &mainNet, &mainPct, &mainSource, &pct)
	if err != nil {
		return
	}
	if turnover.Valid {
		item.TurnoverRate = turnover.Float64
	}
	if amount.Valid {
		item.Amount = amount.Float64
	}
	if mainNet.Valid {
		item.MainNetInflow = mainNet.Float64
	}
	if mainPct.Valid {
		item.MainNetInflowPct = mainPct.Float64
	}
	if mainSource.Valid {
		item.MainFlowSource = mainSource.String
	}
	if pct.Valid && item.DayChangePercent == 0 {
		item.DayChangePercent = pct.Float64
	}
}

func previousCloseBefore(klines []models.KLineData, date string) float64 {
	prev := 0.0
	for _, k := range klines {
		d := normalizeReviewDate(k.Time, "")
		if d >= date {
			return prev
		}
		if k.Close > 0 {
			prev = k.Close
		}
	}
	return prev
}

func buildKLineSummary(k models.KLineData, prevClose float64, signalPrice float64) string {
	parts := []string{}
	if prevClose > 0 && k.Close > 0 {
		parts = append(parts, fmt.Sprintf("当日收盘%+.2f%%", (k.Close/prevClose-1)*100))
	}
	if signalPrice > 0 && k.Close > 0 {
		parts = append(parts, fmt.Sprintf("相对入选价%+.2f%%", (k.Close/signalPrice-1)*100))
	}
	if k.High > 0 && k.Low > 0 {
		parts = append(parts, fmt.Sprintf("振幅%.2f%%", (k.High/k.Low-1)*100))
	}
	if k.Close >= k.Open {
		parts = append(parts, "收阳/承接较好")
	} else {
		parts = append(parts, "收阴/承接偏弱")
	}
	if k.MA10 > 0 {
		if k.Close >= k.MA10 {
			parts = append(parts, "站上MA10")
		} else {
			parts = append(parts, "跌破MA10")
		}
	}
	return strings.Join(parts, "；")
}

func buildFundSummary(mainNet float64, mainPct float64, source string, turnover float64, amount float64) string {
	parts := []string{}
	if source != "" || mainNet != 0 || mainPct != 0 {
		direction := "流入"
		if mainNet < 0 {
			direction = "流出"
		}
		parts = append(parts, fmt.Sprintf("主力%s%s，占比%.2f%%", direction, formatReviewAmount(math.Abs(mainNet)), math.Abs(mainPct)))
		if source != "" {
			parts = append(parts, "来源 "+source)
		}
	} else {
		parts = append(parts, "主力资金暂缺")
	}
	if turnover > 0 {
		parts = append(parts, fmt.Sprintf("换手%.2f%%", turnover))
	}
	if amount > 0 {
		parts = append(parts, "成交额"+formatReviewAmount(amount))
	}
	return strings.Join(parts, "；")
}

func buildReviewOutcome(closeReturn float64, highReturn float64) string {
	switch {
	case closeReturn >= 3:
		return "收盘验证成功"
	case highReturn >= 3 && closeReturn < 1:
		return "盘中给过冲高，收盘回落"
	case closeReturn >= 0:
		return "小幅正反馈"
	case closeReturn <= -3:
		return "失败/需止损纪律"
	default:
		return "偏弱观察"
	}
}

func buildItemSuggestions(item models.StrategyReviewItem) []string {
	out := []string{}
	if item.HighReturnPercent >= 3 && item.CloseReturnPercent < 1 {
		out = append(out, "该票盘中冲高后回落，策略更适合次日分批止盈，不宜死拿到收盘")
	}
	if item.MainNetInflow < 0 && item.CloseReturnPercent < 0 {
		out = append(out, "资金流出且收盘为负，后续可加入“次日资金不转负才续持”的确认")
	}
	if item.TurnoverRate > 10 {
		out = append(out, "换手偏高，疑似分歧放大，类似票可降低评分或缩短持有")
	}
	if strings.Contains(item.KLineSummary, "跌破MA10") {
		out = append(out, "跌破MA10，按纪律应减仓/清仓，不建议摊成本")
	}
	if len(out) == 0 {
		out = append(out, "规则表现正常，保留原入选条件，继续观察同类样本数量")
	}
	return out
}

func buildStrategyOptimization(result models.StrategyReviewResult, mainNegative int, closePoor int) []string {
	out := []string{}
	if result.ReviewedCount == 0 {
		return []string{"先补齐盘后历史采集，再判断策略胜率；当前样本没有收盘结果"}
	}
	if result.AvgHighReturn >= 3 && result.AvgCloseReturn < 1 {
		out = append(out, "平均盘中高点明显好于收盘，策略应偏“次日上午/盘中止盈”，不要默认持到收盘")
	}
	if result.AvgCloseReturn < 0 {
		out = append(out, "本次平均收盘收益为负，建议下次只取评分前3，并提高资金/趋势确认权重")
	}
	if float64(mainNegative) >= float64(result.ReviewedCount)*0.5 {
		out = append(out, "超过一半标的主力净流出，建议加入“资金不转负/流出占比不过大”的二次过滤")
	}
	if float64(closePoor) >= float64(result.ReviewedCount)*0.3 {
		out = append(out, "较多标的收盘跌超2%，建议遇到弱大盘或高跌停日自动降低仓位/减少出票")
	}
	if result.Market.LimitDownCount >= 50 {
		out = append(out, "大盘跌停家数偏高，建议大盘闸门未通过时只观察不买，或仓位减半")
	}
	if len(out) == 0 {
		out = append(out, "本次复盘未发现明显失效点，先维持原规则，继续累积至少20个样本再调阈值")
	}
	return out
}

func filterReviewNewsForStock(news []models.StrategyReviewNews, symbol string, name string, limit int) []models.StrategyReviewNews {
	out := []models.StrategyReviewNews{}
	code := normalizeReviewStockCode(symbol)
	name = strings.TrimSpace(name)
	for _, item := range news {
		content := item.Content
		if (code != "" && strings.Contains(content, code)) || (name != "" && strings.Contains(content, name)) {
			out = append(out, item)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func trimReviewNews(news []models.StrategyReviewNews, limit int) []models.StrategyReviewNews {
	if limit <= 0 || len(news) <= limit {
		return news
	}
	return news[:limit]
}

func marshalStringSlice(values []string) string {
	if values == nil {
		values = []string{}
	}
	data, _ := json.Marshal(values)
	return string(data)
}

func unmarshalStringSlice(raw string) []string {
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return []string{}
	}
	return out
}

func normalizeReviewDate(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		value = value[:10]
	}
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return value
	}
	return fallback
}

func normalizeReviewStockCode(symbol string) string {
	s := strings.ToLower(strings.TrimSpace(symbol))
	if len(s) >= 8 && (strings.HasPrefix(s, "sh") || strings.HasPrefix(s, "sz") || strings.HasPrefix(s, "bj")) {
		return s[2:]
	}
	return s
}

func safeReviewFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func roundReview2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

func chooseText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func formatReviewAmount(value float64) string {
	if value >= 1e8 {
		return fmt.Sprintf("%.2f亿", value/1e8)
	}
	if value >= 1e4 {
		return fmt.Sprintf("%.2f万", value/1e4)
	}
	return fmt.Sprintf("%.0f", value)
}

func combineReviewWarnings(a string, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	if strings.TrimSpace(b) == "" {
		return a
	}
	return a + "；" + b
}
