package services

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// tailReplayBlockedBoard 剔除创业板(30x)/科创(68x)/北交所(8x)/老三板(4x)。
func tailReplayBlockedBoard(code string) bool {
	c := strings.ToLower(strings.TrimSpace(code))
	if len(c) >= 2 && (strings.HasPrefix(c, "sh") || strings.HasPrefix(c, "sz") || strings.HasPrefix(c, "bj")) {
		c = c[2:]
	}
	return strings.HasPrefix(c, "30") || strings.HasPrefix(c, "68") || strings.HasPrefix(c, "8") || strings.HasPrefix(c, "4")
}

func replayCapBucket(totalCap float64) string {
	switch {
	case totalCap <= 0:
		return ""
	case totalCap < 50e8:
		return "微盘"
	case totalCap < 100e8:
		return "小盘"
	case totalCap < 300e8:
		return "中小盘"
	case totalCap < 800e8:
		return "中盘"
	default:
		return "大盘"
	}
}

// ClassifyTailForm 精准判定尾盘懒人形态（用影线/收盘 vs MA10/MA20）。
// 返回 formType：1=强、2=中(破10线下影回踩20线反身)、0=剔除；以及说明。
func ClassifyTailForm(open, high, low, close, ma10, ma20 float64) (int, string) {
	if ma10 <= 0 || ma20 <= 0 || ma10 <= ma20 {
		return 0, "非多头排列(MA10<=MA20)"
	}
	rng := high - low
	// 反身向上：收盘落在当日振幅的中上部（≥40%位置）
	closeStrong := rng <= 0 || (close-low)/rng >= 0.4
	switch {
	case close >= ma10:
		// 收盘站上10线即为强（不论下影插多深；下影插到20线再拉回是更强的反包）
		if low >= ma10 {
			return 1, "整根站上10/20线(强)"
		}
		return 1, "收在10线上、下影回踩拉回(强)"
	case close >= ma20 && low <= ma20*1.01 && closeStrong:
		// 收盘真跌破10线、但守住20线且下影回踩20线反身收上
		return 2, "破10线、收在20线上回踩反弹(中)"
	default:
		return 0, "弱形态：收盘跌破20线 / 破10线未回踩反身"
	}
}

func avgCloseBt(rows []btRow, ri, period int) float64 {
	if ri-period+1 < 0 {
		return 0
	}
	sum := 0.0
	for j := ri - period + 1; j <= ri; j++ {
		if rows[j].close <= 0 {
			return 0
		}
		sum += rows[j].close
	}
	return sum / float64(period)
}

// tradeDatesInRange 区间内的交易日（升序）。
func (s *HistoryService) tradeDatesInRange(start, end string) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT trade_date FROM stock_daily WHERE trade_date>=? AND trade_date<=? ORDER BY trade_date ASC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && d != "" {
			out = append(out, d)
		}
	}
	return out, nil
}

// evaluateTailLazyBtRow 在历史 btRow 序列上验证尾盘懒人规则（量比用成交额代理）。
func evaluateTailLazyBtRow(rows []btRow, ri int) (bool, int, float64, float64, []string, []string, float64) {
	if ri < 20 || ri >= len(rows) {
		return false, 0, 0, 0, nil, nil, 0
	}
	today := rows[ri]
	// 涨幅3-6% / 换手5-10%
	if today.pct <= 3.0 || today.pct >= 6.0 {
		return false, 0, 0, 0, nil, nil, 0
	}
	if today.turnover <= 5.0 || today.turnover >= 10.0 {
		return false, 0, 0, 0, nil, nil, 0
	}
	// 量比还原：成交量 ≈ 成交额 / 当日均价((高+低+收)/3)，再按"收盘量比=今量/近5日均量"。
	// （库里 volume 列单位不一致不可靠；amount 与价格可靠，反算出真成交量。）
	volOf := func(r btRow) float64 {
		tp := (r.high + r.low + r.close) / 3
		if tp <= 0 {
			tp = r.close
		}
		if tp <= 0 || r.amount <= 0 {
			return 0
		}
		return r.amount / tp
	}
	var sumVol float64
	cnt := 0
	for j := ri - 5; j < ri; j++ {
		if j >= 0 {
			if v := volOf(rows[j]); v > 0 {
				sumVol += v
				cnt++
			}
		}
	}
	todayVol := volOf(today)
	if cnt == 0 || sumVol <= 0 || todayVol <= 0 {
		return false, 0, 0, 0, nil, nil, 0
	}
	volRatio := todayVol / (sumVol / float64(cnt))
	if volRatio <= 1.0 || volRatio >= 2.5 {
		return false, 0, 0, 0, nil, nil, 0
	}
	// 均线
	ma10 := avgCloseBt(rows, ri, 10)
	ma20 := avgCloseBt(rows, ri, 20)
	if ma10 <= 0 || ma20 <= 0 || ma10 <= ma20 {
		return false, 0, 0, 0, nil, nil, ma10
	}
	// 形态（精准：用影线/收盘 vs MA10/MA20）
	formType, _ := ClassifyTailForm(today.open, today.high, today.low, today.close, ma10, ma20)
	if formType == 0 {
		return false, 0, 0, 0, nil, nil, ma10
	}
	// 近5日新高
	for j := ri - 4; j < ri; j++ {
		if j >= 0 && rows[j].high >= today.high {
			return false, formType, volRatio, 0, nil, nil, ma10
		}
	}
	// 近20日有>8%、无<-8%
	has8 := false
	for j := ri - 19; j <= ri; j++ {
		if j < 0 {
			continue
		}
		p := rows[j].pct
		if p > 8.0 {
			has8 = true
		}
		if p < -8.0 {
			return false, formType, volRatio, 0, nil, nil, ma10
		}
	}
	if !has8 {
		return false, formType, volRatio, 0, nil, nil, ma10
	}
	// 前7日无涨停
	for j := ri - 7; j < ri; j++ {
		if j >= 0 && rows[j].pct >= 9.8 {
			return false, formType, volRatio, 0, nil, nil, ma10
		}
	}
	// 上影线≤实体
	body := math.Abs(today.close - today.open)
	upper := today.high - math.Max(today.open, today.close)
	if upper > body {
		return false, formType, volRatio, 0, nil, nil, ma10
	}
	// 评分
	score := 50.0
	score += clampF((volRatio-1.0)*8, 0, 12)
	score += clampF((today.pct-3.0)*3, 0, 9)
	if formType == 1 {
		score += 12
	} else {
		score += 5
	}
	score += clampF((10.0-today.turnover)*1.0, 0, 5)
	score = math.Round(clampF(score, 0, 100)*10) / 10

	reasons := []string{fmt.Sprintf("涨幅 %.2f%% · 量比≈%.2f · 换手 %.2f%%", today.pct, volRatio, today.turnover)}
	risks := []string{}
	if formType == 1 {
		reasons = append(reasons, "形态：站上10/20线(强)")
	} else {
		reasons = append(reasons, "形态：破10线回踩20线反弹(中)")
		risks = append(risks, "破10日线，靠20日线支撑(中等强度)")
	}
	reasons = append(reasons, "多头排列 · 近5日新高 · 近20日有>8%且无<-8% · 前7日无涨停 · 上影≤实体")
	return true, formType, volRatio, score, reasons, risks, ma10
}

// tailBatchAcc 批量复盘累加器
type tailBatchAcc struct {
	n, hit3, hit5, win        int
	sumHigh, sumOpen, sumClose, sumNet float64
}

func (a *tailBatchAcc) add(nextHigh, nextOpen, nextClose, cost float64) {
	a.n++
	if nextHigh >= 3 {
		a.hit3++
	}
	if nextHigh >= 5 {
		a.hit5++
	}
	a.sumHigh += nextHigh
	a.sumOpen += nextOpen
	a.sumClose += nextClose
	net := nextClose
	if nextHigh >= 3 {
		net = 3.0
	}
	net -= cost
	if net > 0 {
		a.win++
	}
	a.sumNet += net
}

func (a *tailBatchAcc) row(label string) models.TailLazyBatchRow {
	if a.n == 0 {
		return models.TailLazyBatchRow{Label: label}
	}
	f := float64(a.n)
	return models.TailLazyBatchRow{
		Label: label, Samples: a.n,
		Hit3Rate: round2(float64(a.hit3) * 100 / f), Hit5Rate: round2(float64(a.hit5) * 100 / f),
		AvgHigh: round2(a.sumHigh / f), AvgOpen: round2(a.sumOpen / f), AvgClose: round2(a.sumClose / f),
		TpWinRate: round2(float64(a.win) * 100 / f), TpExpectancy: round2(a.sumNet / f),
	}
}

// BatchTailLazyReplay 区间内逐日跑尾盘懒人规则，统计次日表现的整体真实命中率/期望（按年份+合计）。
func (s *HistoryService) BatchTailLazyReplay(start, end string) models.TailLazyBatchResult {
	res := models.TailLazyBatchResult{Start: start, End: end}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	t0, err1 := time.Parse("2006-01-02", start)
	if err1 != nil {
		res.Warning = "起始日期格式错误"
		return res
	}
	// 载入窗口：起点往前 70 自然日(算均线/20日) ~ 终点
	loadStart := t0.AddDate(0, 0, -70).Format("2006-01-02")
	dates, err := s.tradeDatesInRange(loadStart, end)
	if err != nil || len(dates) < 25 {
		res.Warning = "区间交易日不足"
		return res
	}
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err := s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		res.Warning = "载入历史失败：" + err.Error()
		return res
	}
	const cost = 0.35
	all := &tailBatchAcc{}
	byYear := map[string]*tailBatchAcc{}
	yearOrder := []string{}
	for di := 20; di < len(dates)-1; di++ {
		d := dates[di]
		if d < start || d > end { // 信号日限定在 [start,end]
			continue
		}
		yr := d[:4]
		ya := byYear[yr]
		if ya == nil {
			ya = &tailBatchAcc{}
			byYear[yr] = ya
			yearOrder = append(yearOrder, yr)
		}
		for code, ser := range series {
			if tailReplayBlockedBoard(code) || strings.Contains(strings.ToUpper(ser.name), "ST") {
				continue
			}
			ri, ok := ser.idx[d]
			if !ok || ri < 20 || ri+1 >= len(ser.rows) {
				continue
			}
			pass, _, _, _, _, _, _ := evaluateTailLazyBtRow(ser.rows, ri)
			if !pass {
				continue
			}
			today := ser.rows[ri]
			if today.close <= 0 {
				continue
			}
			nd := ser.rows[ri+1]
			nh := (nd.high/today.close - 1) * 100
			no := (nd.open/today.close - 1) * 100
			nc := (nd.close/today.close - 1) * 100
			all.add(nh, no, nc, cost)
			ya.add(nh, no, nc, cost)
		}
	}
	sort.Strings(yearOrder)
	for _, y := range yearOrder {
		res.Rows = append(res.Rows, byYear[y].row(y))
	}
	res.Rows = append(res.Rows, all.row("合计"))
	if all.n == 0 {
		res.Warning = "该区间无信号"
	}
	return res
}

// ScanLowBuyOnDate 低吸历史复盘：在指定交易日按低吸规则选Top，并带出按机械纪律的持有结果。
func (s *HistoryService) ScanLowBuyOnDate(date string, limit int, maxChangePct float64) ([]models.LowBuyScannerItem, string, string) {
	if s == nil || s.db == nil {
		return nil, "", "历史库未就绪"
	}
	if limit <= 0 {
		limit = 30
	}
	SetLowBuyMaxPct(maxChangePct)
	cfg := exitCfgFrom(models.BacktestRequest{SellRule: "fast"})
	var asOf string
	if err := s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily WHERE trade_date <= ?`, date).Scan(&asOf); err != nil || asOf == "" {
		return nil, "", "该日期无历史数据（库自 2023-03 起）"
	}
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, asOf, "日期解析失败"
	}
	startDate := t.AddDate(0, 0, -70).Format("2006-01-02")
	endDate := t.AddDate(0, 0, 40).Format("2006-01-02") // 往后留出持有期
	dates, err := s.tradeDatesInRange(startDate, endDate)
	if err != nil || len(dates) < 21 {
		return nil, asOf, "该日附近交易日不足"
	}
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err := s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		return nil, asOf, "载入历史失败：" + err.Error()
	}
	di := -1
	for i, d := range dates {
		if d == asOf {
			di = i
			break
		}
	}
	if di < 0 || di >= len(dates)-1 {
		return nil, asOf, "该日不是交易日或无次日数据"
	}
	items := make([]models.LowBuyScannerItem, 0, limit)
	for _, c := range dayCandidates(series, asOf, 0, limit) {
		ser := series[c.code]
		ri, ok := ser.idx[asOf]
		if !ok {
			continue
		}
		row := ser.rows[ri]
		item := models.LowBuyScannerItem{
			Symbol: c.code, Name: ser.name, Price: row.close,
			ChangePercent: row.pct, Amount: row.amount, TurnoverRate: row.turnover,
			MainNetInflow: row.mainNet, MainNetInflowRatio: row.mainPct,
			TotalMarketCap: row.totalCap, CapBucket: replayCapBucket(row.totalCap),
			Score: round2(c.score), TriggerCount: 0, Triggers: []string{}, Reasons: []string{}, RiskFlags: []string{},
			BuyPointHint:  "尾盘14:30-15:00分批，或次日开盘进",
			SellPointHint: "3日累计+15%减半；换手>12%先走；持仓5日涨幅<3%清仓",
			StopLossHint:  "跌破买入价-5%止损；跌破10日线次日不收回离场",
		}
		// 机械纪律持有结果
		if tr := s.simulateTrade(ser, c.code, asOf, di, dates, "next_open", cfg, c.score); tr != nil {
			item.ReplayExitDate = tr.ExitDate
			item.ReplayHoldDays = tr.HoldDays
			item.ReplayReturnPct = tr.ReturnPct
			item.ReplayExitReason = tr.ExitReason
		}
		items = append(items, item)
	}
	// 名称兜底 + ST 复查
	cleaned := items[:0]
	for i := range items {
		if strings.TrimSpace(items[i].Name) == "" {
			var nm string
			if s.db.QueryRow(`SELECT stock_name FROM stock_daily WHERE stock_code=? AND stock_name IS NOT NULL AND stock_name!='' ORDER BY trade_date DESC LIMIT 1`, items[i].Symbol).Scan(&nm) == nil && nm != "" {
				items[i].Name = nm
			}
		}
		cleaned = append(cleaned, items[i])
	}
	return cleaned, asOf, ""
}

// ScanTailLazyOnDate 历史复盘：在指定交易日按尾盘懒人规则筛选并带出次日表现。
func (s *HistoryService) ScanTailLazyOnDate(date string, limit int) ([]models.LowBuyScannerItem, string, string) {
	if s == nil || s.db == nil {
		return nil, "", "历史库未就绪"
	}
	if limit <= 0 {
		limit = 30
	}
	// 解析到 <= date 的最近交易日
	var asOf string
	if err := s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily WHERE trade_date <= ?`, date).Scan(&asOf); err != nil || asOf == "" {
		return nil, "", "该日期无历史数据（库区间 2023-03 起）"
	}
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, asOf, "日期解析失败"
	}
	startDate := t.AddDate(0, 0, -70).Format("2006-01-02")
	endDate := t.AddDate(0, 0, 12).Format("2006-01-02")
	dates, err := s.tradeDatesInRange(startDate, endDate)
	if err != nil || len(dates) < 21 {
		return nil, asOf, "该日附近交易日不足（需≥21个）"
	}
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err := s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		return nil, asOf, "载入历史失败：" + err.Error()
	}

	items := make([]models.LowBuyScannerItem, 0, 64)
	for code, ser := range series {
		if tailReplayBlockedBoard(code) {
			continue
		}
		if strings.Contains(strings.ToUpper(ser.name), "ST") {
			continue
		}
		ri, ok := ser.idx[asOf]
		if !ok || ri < 20 {
			continue
		}
		pass, formType, _, score, reasons, risks, ma10 := evaluateTailLazyBtRow(ser.rows, ri)
		if !pass {
			continue
		}
		today := ser.rows[ri]
		triggers := []string{"量比1-2.5", "涨幅3-6%", "换手5-10%", "多头排列", "近5日新高", "股性激活(20日>8%)"}
		if formType == 1 {
			triggers = append(triggers, "站上10/20线(强)")
		} else {
			triggers = append(triggers, "回踩20线反弹(中)")
		}
		ma10Status := "broke"
		if today.close >= ma10 {
			ma10Status = "hold"
		}
		item := models.LowBuyScannerItem{
			Symbol: code, Name: ser.name, Price: today.close,
			ChangePercent: today.pct, Amount: today.amount, TurnoverRate: today.turnover,
			MainNetInflow: today.mainNet, TotalMarketCap: today.totalCap,
			CapBucket: replayCapBucket(today.totalCap), Score: score,
			TriggerCount: len(triggers), Triggers: triggers, Reasons: reasons, RiskFlags: risks,
			BuyPointHint:  "尾盘14:30-15:00分批买入，次日上午冲高止盈",
			SellPointHint: "次日上午冲高5-6个点止盈；冲高乏力/平开即保本走",
			StopLossHint:  "次日走弱或跌破买入价-3%，止损离场",
			MA10:          ma10, MA10Status: ma10Status,
		}
		// 次日表现（以当日收盘为买入价）
		if ri+1 < len(ser.rows) && today.close > 0 {
			nd := ser.rows[ri+1]
			item.NextDate = nd.date
			item.NextOpenGainPct = round2((nd.open/today.close - 1) * 100)
			item.NextHighGainPct = round2((nd.high/today.close - 1) * 100)
			item.NextCloseGainPct = round2((nd.close/today.close - 1) * 100)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	if len(items) > limit {
		items = items[:limit]
	}
	// 名称兜底 + ST 复查（历史回补行常缺 stock_name，导致循环里的 ST 过滤失效）
	cleaned := items[:0]
	for i := range items {
		if strings.TrimSpace(items[i].Name) == "" {
			var nm string
			if s.db.QueryRow(`SELECT stock_name FROM stock_daily WHERE stock_code=? AND stock_name IS NOT NULL AND stock_name!='' ORDER BY trade_date DESC LIMIT 1`, items[i].Symbol).Scan(&nm) == nil && nm != "" {
				items[i].Name = nm
			}
		}
		if strings.Contains(strings.ToUpper(items[i].Name), "ST") {
			continue // 补全名称后剔除 ST/*ST
		}
		cleaned = append(cleaned, items[i])
	}
	return cleaned, asOf, ""
}
