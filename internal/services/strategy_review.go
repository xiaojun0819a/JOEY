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

	picks, err := s.loadStrategyPicks(strategyID, signalDate)
	if err != nil {
		result.Warning = "读取策略扫描留痕失败：" + err.Error()
		return result
	}
	if len(picks) == 0 {
		rebuilt, note := s.rebuildStrategyPicksForReview(strategyID, result.StrategyName, signalDate)
		if note != "" {
			result.Warning = combineReviewWarnings(result.Warning, note)
		}
		if len(rebuilt) > 0 {
			picks = rebuilt
			_ = s.SaveStrategyPicks(strategyID, result.StrategyName, signalDate, now, rebuilt)
		}
	}
	if len(picks) == 0 && requestedSignalDate != "" {
		fallbackDate := s.latestSignalDateBefore(strategyID, reviewDate)
		if fallbackDate != "" && fallbackDate != signalDate {
			fallbackPicks, fallbackErr := s.loadStrategyPicks(strategyID, fallbackDate)
			if fallbackErr == nil && len(fallbackPicks) > 0 {
				result.SignalDate = fallbackDate
				result.Warning = combineReviewWarnings(result.Warning, fmt.Sprintf("%s 没有找到 %s 的入选记录，已切换到最近一次留痕 %s", result.StrategyName, signalDate, fallbackDate))
				picks = fallbackPicks
			}
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

	snapshotMap, snapshotWarn := s.loadReviewSnapshotMap()
	if snapshotWarn != "" {
		result.DataSourceNotes = append(result.DataSourceNotes, snapshotWarn)
	}
	result.Market = s.buildReviewMarket(reviewDate, snapshotMap)

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
		if err := s.SaveStrategyPicks(strategyID, strategyName, signalDate, signalDate, picks); err != nil {
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
		return nil, ""
	}
}

func (s *HistoryService) latestSignalDateBefore(strategyID string, reviewDate string) string {
	var date string
	_ = s.db.QueryRow(`SELECT MAX(signal_date) FROM strategy_scan_picks WHERE strategy_id=? AND signal_date < ?`, strategyID, reviewDate).Scan(&date)
	if date == "" {
		_ = s.db.QueryRow(`SELECT MAX(signal_date) FROM strategy_scan_picks WHERE strategy_id=?`, strategyID).Scan(&date)
	}
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

func (s *HistoryService) loadReviewSnapshotMap() (map[string]ScanSnapshotRow, string) {
	out := map[string]ScanSnapshotRow{}
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
	if s == nil || s.marketService == nil {
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
