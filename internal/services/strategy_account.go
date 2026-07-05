package services

import (
	"sort"

	"github.com/run-bigpig/jcp/internal/models"
)

const (
	accountCapital  = 100000.0
	accountTopN     = 3
	accountMaxPos   = 6
	accountCooldown = 3
	accountCostMul  = 1.0 - 0.35/100
)

// dayCandidatesTailLazy 某交易日按尾盘懒人规则选 TopN（用于策略账户）。
func dayCandidatesTailLazy(series map[string]btSeries, date string, topN int) []cand {
	cands := make([]cand, 0, 128)
	for code, ser := range series {
		if tailReplayBlockedBoard(code) {
			continue
		}
		if containsST(ser.name) {
			continue
		}
		ri, ok := ser.idx[date]
		if !ok || ri < 20 {
			continue
		}
		if pass, _, _, score, _, _, _ := evaluateTailLazyBtRow(ser.rows, ri); pass {
			cands = append(cands, cand{code, score})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].code < cands[j].code
	})
	if len(cands) > topN {
		cands = cands[:topN]
	}
	return cands
}

func containsST(name string) bool {
	for i := 0; i+1 < len(name); i++ {
		if (name[i] == 'S' || name[i] == 's') && (name[i+1] == 'T' || name[i+1] == 't') {
			return true
		}
	}
	return false
}

// loadAccountSeries 载入账户回测所需的交易日、价量序列与等权全A基准。
func (s *HistoryService) loadAccountSeries(days int) (dates []string, series map[string]btSeries, idx map[string]float64, warn string) {
	if days <= 0 || days > 520 {
		days = 250
	}
	var err error
	dates, err = s.recentTradeDates(days)
	if err != nil || len(dates) < 25 {
		return nil, nil, nil, "交易日不足"
	}
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err = s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		return nil, nil, nil, "载入历史失败：" + err.Error()
	}
	idx = buildMarketIndex(series, dates, dateSet)
	return dates, series, idx, ""
}

// loadAccountSeriesRange 载入指定日期区间 [start,end] 的序列与基准（滚动多年回测用）。
func (s *HistoryService) loadAccountSeriesRange(start, end string) (dates []string, series map[string]btSeries, idx map[string]float64, warn string) {
	rows, err := s.db.Query(`SELECT DISTINCT trade_date FROM stock_daily WHERE trade_date>=? AND trade_date<=? ORDER BY trade_date`, start, end)
	if err != nil {
		return nil, nil, nil, "查询交易日失败：" + err.Error()
	}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && d != "" {
			dates = append(dates, d)
		}
	}
	rows.Close()
	if len(dates) < 25 {
		return nil, nil, nil, "该区间交易日不足"
	}
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err = s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		return nil, nil, nil, "载入历史失败：" + err.Error()
	}
	idx = buildMarketIndex(series, dates, dateSet)
	return dates, series, idx, ""
}

// RunStrategyAccountRange 在指定日期区间跑 低吸/尾盘 账户（滚动多年回测）。
func (s *HistoryService) RunStrategyAccountRange(strategy, start, end string) models.StrategyAccountResult {
	res := models.StrategyAccountResult{Strategy: strategy, Capital: accountCapital}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	SetLowBuyMaxPct(1.5)
	dates, series, idx, warn := s.loadAccountSeriesRange(start, end)
	if warn != "" {
		res.Warning = warn
		return res
	}
	selectFn := func(date string) []cand {
		if strategy == "taillazy" {
			return dayCandidatesTailLazy(series, date, accountTopN)
		}
		return dayCandidates(series, date, 0, accountTopN)
	}
	return s.runAccountCore(strategy, dates, series, idx, selectFn, nil, nil)
}

// RunAccountFromSignalsRange 在指定区间用预算信号跑公式型账户（滚动多年回测）。
func (s *HistoryService) RunAccountFromSignalsRange(strategy, start, end string, signals map[string][]models.AccountCandidate, profile *StrategyTradeProfile) models.StrategyAccountResult {
	res := models.StrategyAccountResult{Strategy: strategy, Capital: accountCapital}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	dates, series, idx, warn := s.loadAccountSeriesRange(start, end)
	if warn != "" {
		res.Warning = warn
		return res
	}
	selectFn := func(date string) []cand {
		sig := signals[date]
		out := make([]cand, 0, len(sig))
		for _, c := range sig {
			out = append(out, cand{c.Symbol, c.Score})
		}
		return out
	}
	return s.runAccountCore(strategy, dates, series, idx, selectFn, profile, nil)
}

// riskProfileIf 按 useRisk 返回通用短线风控线（账户里都是短线策略）。
func riskProfileIf(useRisk bool) *RiskProfile {
	if !useRisk {
		return nil
	}
	rp := riskShortTerm
	return &rp
}

// RunStrategyAccount 固定本金·自动买卖的策略账户模拟（低吸 / 尾盘懒人，O(1) 评估器）。
// 尾盘收盘价进场（14:30-15:00 实操口径）、Top3、限6仓、冷却3日、机械纪律自动平仓、扣成本0.35%。
func (s *HistoryService) RunStrategyAccount(strategy string, days int, useRisk bool) models.StrategyAccountResult {
	res := models.StrategyAccountResult{Strategy: strategy, Capital: accountCapital}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	SetLowBuyMaxPct(1.5)
	dates, series, idx, warn := s.loadAccountSeries(days)
	if warn != "" {
		res.Warning = warn
		return res
	}
	selectFn := func(date string) []cand {
		if strategy == "taillazy" {
			return dayCandidatesTailLazy(series, date, accountTopN)
		}
		return dayCandidates(series, date, 0, accountTopN)
	}
	return s.runAccountCore(strategy, dates, series, idx, selectFn, nil, riskProfileIf(useRisk))
}

// RunAccountFromSignals 用外部预先算好的每日候选信号驱动账户模拟（供 main 包的公式型策略复用账户机制）。
// profile 非空时启用该策略专属的"加—减—卖—止损+大盘闸"引擎，否则用统一低吸机械纪律；useRisk 时改套通用风控线。
func (s *HistoryService) RunAccountFromSignals(strategy string, days int, signals map[string][]models.AccountCandidate, profile *StrategyTradeProfile, useRisk bool) models.StrategyAccountResult {
	res := models.StrategyAccountResult{Strategy: strategy, Capital: accountCapital}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	dates, series, idx, warn := s.loadAccountSeries(days)
	if warn != "" {
		res.Warning = warn
		return res
	}
	selectFn := func(date string) []cand {
		sig := signals[date]
		out := make([]cand, 0, len(sig))
		for _, c := range sig {
			out = append(out, cand{c.Symbol, c.Score})
		}
		return out
	}
	return s.runAccountCore(strategy, dates, series, idx, selectFn, profile, riskProfileIf(useRisk))
}

// runAccountCore 账户模拟核心：尾盘收盘价进场 + 机械纪律自动平仓 + 盯市净值曲线。
func (s *HistoryService) runAccountCore(strategy string, dates []string, series map[string]btSeries, idx map[string]float64, selectFn func(date string) []cand, profile *StrategyTradeProfile, riskProf *RiskProfile) models.StrategyAccountResult {
	const capital = accountCapital
	const topN, maxPos, cooldown = accountTopN, accountMaxPos, accountCooldown
	const costMul = accountCostMul
	res := models.StrategyAccountResult{Strategy: strategy, Capital: capital}
	cfg := exitCfgFrom(models.BacktestRequest{SellRule: "fast"})
	var weakDays map[string]bool
	if profile != nil && profile.MarketGate {
		weakDays = computeWeakDays(series, dates)
	}

	cash := capital
	positions := map[string]*openPos{}
	lastExit := map[string]int{}
	equity := make([]models.AccountEquityPoint, 0, len(dates))
	trades := make([]models.AccountTrade, 0, 256)
	var btTrades []models.BacktestTrade

	markEquity := func(date string) float64 {
		e := cash
		for code, p := range positions {
			ser := series[code]
			if ri, ok := ser.idx[date]; ok && p.entryPrice > 0 {
				e += p.investAmount * ser.rows[ri].close / p.entryPrice
			} else {
				e += p.investAmount
			}
		}
		return e
	}

	// 次日进场机制：信号日仅排程，次日开盘按可成交价买入
	type pendingEntry struct {
		code                     string
		score, sClose, sHi, sLow float64
	}
	pending := map[string][]pendingEntry{}
	entryNextOpen := profile != nil && profile.EntryNextOpen
	openPosAt := func(code string, score, entry, sHi, sLow float64, date string) {
		invest := markEquity(date) / float64(maxPos)
		if invest > cash {
			invest = cash
		}
		if invest <= 1e-9 {
			return
		}
		cash -= invest
		positions[code] = &openPos{
			investAmount: invest, entryPrice: entry, entryDate: date, st: &exitState{}, score: score,
			baseInvest: invest, maxClose: entry, signalHigh: sHi, signalLow: sLow,
		}
	}

	for di, date := range dates {
		// 0) 执行排程到今日的次日进场（按今日开盘价，回踩确认可选）
		if entryNextOpen {
			for _, pe := range pending[date] {
				if len(positions) >= maxPos {
					break
				}
				if _, held := positions[pe.code]; held {
					continue
				}
				if le, ok := lastExit[pe.code]; ok && di-le <= cooldown {
					continue
				}
				ser := series[pe.code]
				ri, ok := ser.idx[date]
				if !ok {
					continue
				}
				openPx := ser.rows[ri].open
				if openPx <= 0 {
					continue
				}
				if profile.EntryPullbackMaxGapPct > 0 && pe.sClose > 0 &&
					openPx > pe.sClose*(1+profile.EntryPullbackMaxGapPct/100) {
					continue // 次日高开太多，无回踩，放弃
				}
				openPosAt(pe.code, pe.score, openPx, pe.sHi, pe.sLow, date)
			}
			delete(pending, date)
		}

		// 1) 持仓离场判定（入场当日不判）
		for code, p := range positions {
			if p.entryDate == date {
				continue
			}
			ser := series[code]
			ri, ok := ser.idx[date]
			if !ok {
				continue
			}
			p.hold++

			// —— 通用风控覆盖（套用后忽略策略自身离场/加仓，只走风控线）——
			if riskProf != nil {
				if reason, price := riskProf.RiskStep(p, ser.rows[ri], p.hold); reason != "" {
					cash += p.investAmount * price / p.entryPrice * costMul
					ret := (price/p.entryPrice*costMul - 1) * 100
					trades = append(trades, models.AccountTrade{
						Symbol: code, Name: ser.name, EntryDate: p.entryDate, ExitDate: date,
						EntryPrice: round2(p.entryPrice), ExitPrice: round2(price), HoldDays: p.hold,
						ReturnPct: round2(ret), ExitReason: reason,
					})
					btTrades = append(btTrades, models.BacktestTrade{Code: code, EntryDate: p.entryDate, ExitDate: date, ReturnPct: round2(ret), HoldDays: p.hold})
					lastExit[code] = di
					delete(positions, code)
				}
				continue
			}

			// —— profile 驱动的策略专属引擎 ——
			if profile != nil {
				action, reason, price := profile.step(p, ser.rows, ri, p.hold, 0, weakDays[date])
				switch action {
				case "add":
					addCash := p.baseInvest * profile.AddFraction
					if addCash > cash {
						addCash = cash
					}
					if addCash > 1e-9 && price > 0 && p.entryPrice > 0 {
						newShares := p.investAmount/p.entryPrice + addCash/price
						p.investAmount += addCash
						p.entryPrice = p.investAmount / newShares // 加权平均成本（止损线随之上移）
						cash -= addCash
						p.addsDone++
					}
				case "half":
					if !p.halfTaken {
						p.halfTaken = true
						p.tpExitPrice = price
						half := p.investAmount * 0.5
						cash += half * price / p.entryPrice * costMul
						p.investAmount -= half
					}
				case "exit":
					cash += p.investAmount * price / p.entryPrice * costMul
					ret := (price/p.entryPrice*costMul - 1) * 100
					if p.halfTaken {
						tpRet := (p.tpExitPrice/p.entryPrice*costMul - 1) * 100
						ret = 0.5*tpRet + 0.5*ret
						reason = "half_" + reason
					}
					trades = append(trades, models.AccountTrade{
						Symbol: code, Name: ser.name, EntryDate: p.entryDate, ExitDate: date,
						EntryPrice: round2(p.entryPrice), ExitPrice: round2(price), HoldDays: p.hold,
						ReturnPct: round2(ret), ExitReason: reason,
					})
					btTrades = append(btTrades, models.BacktestTrade{
						Code: code, EntryDate: p.entryDate, ExitDate: date, ReturnPct: round2(ret), HoldDays: p.hold,
					})
					lastExit[code] = di
					delete(positions, code)
				}
				continue
			}

			r := ser.rows[ri]
			if cfg.halfTP && !p.halfTaken && r.high > 0 {
				tpLine := p.entryPrice * (1 + cfg.takeProfitPct/100)
				if r.high >= tpLine {
					p.halfTaken = true
					p.tpExitPrice = tpLine
					half := p.investAmount * 0.5
					cash += half * tpLine / p.entryPrice * costMul
					p.investAmount -= half
				}
			}
			if reason, price := evalExit(p.entryPrice, r, p.hold, p.st, cfg); reason != "" {
				cash += p.investAmount * price / p.entryPrice * costMul
				ret := (price/p.entryPrice*costMul - 1) * 100
				if p.halfTaken {
					tpRet := (p.tpExitPrice/p.entryPrice*costMul - 1) * 100
					ret = 0.5*tpRet + 0.5*ret
					reason = "half_" + reason
				}
				trades = append(trades, models.AccountTrade{
					Symbol: code, Name: ser.name, EntryDate: p.entryDate, ExitDate: date,
					EntryPrice: round2(p.entryPrice), ExitPrice: round2(price), HoldDays: p.hold,
					ReturnPct: round2(ret), ExitReason: reason,
				})
				btTrades = append(btTrades, models.BacktestTrade{
					Code: code, EntryDate: p.entryDate, ExitDate: date, ReturnPct: round2(ret), HoldDays: p.hold,
				})
				lastExit[code] = di
				delete(positions, code)
			}
		}

		// 2) 选 Top 候选：次日进场模式仅排程到明日，否则当天尾盘收盘价买入
		if di < len(dates)-1 {
			if entryNextOpen {
				nd := dates[di+1]
				for _, c := range selectFn(date) {
					ser := series[c.code]
					ri, ok := ser.idx[date]
					if !ok {
						continue
					}
					sr := ser.rows[ri]
					pending[nd] = append(pending[nd], pendingEntry{c.code, c.score, sr.close, sr.high, sr.low})
				}
			} else if len(positions) < maxPos {
				slots := maxPos - len(positions)
				added := 0
				for _, c := range selectFn(date) {
					if _, held := positions[c.code]; held {
						continue
					}
					if le, ok := lastExit[c.code]; ok && di-le <= cooldown {
						continue
					}
					ser := series[c.code]
					ri, ok := ser.idx[date]
					if !ok {
						continue
					}
					entry := ser.rows[ri].close
					if entry <= 0 {
						continue
					}
					// 入场可成交过滤：封死涨停的信号日，尾盘买不进，跳过
					if profile != nil && profile.EntrySkipSealedLimit && isSealedLimitUp(ser.rows[ri]) {
						continue
					}
					sr := ser.rows[ri]
					openPosAt(c.code, c.score, entry, sr.high, sr.low, date)
					added++
					if added >= slots {
						break
					}
				}
			}
		}

		// 3) 盯市
		equity = append(equity, models.AccountEquityPoint{Date: date, Value: round2(markEquity(date))})
	}

	endDate := dates[len(dates)-1]
	final := markEquity(endDate)

	// 当前持仓快照
	for code, p := range positions {
		ser := series[code]
		cur := p.entryPrice
		if ri, ok := ser.idx[endDate]; ok {
			cur = ser.rows[ri].close
		}
		res.Holdings = append(res.Holdings, models.AccountHolding{
			Symbol: code, Name: ser.name, EntryDate: p.entryDate,
			EntryPrice: round2(p.entryPrice), CurrentPrice: round2(cur), HoldDays: p.hold,
			UnrealizedPct: round2((cur/p.entryPrice - 1) * 100),
			Value:         round2(p.investAmount * cur / p.entryPrice),
		})
	}
	sort.Slice(res.Holdings, func(i, j int) bool { return res.Holdings[i].UnrealizedPct > res.Holdings[j].UnrealizedPct })

	// 平仓记录（最近在前）
	for i := len(trades) - 1; i >= 0; i-- {
		res.Trades = append(res.Trades, trades[i])
	}

	// 计分卡
	scard := models.BacktestResult{ByReason: map[string]int{}}
	s.aggregate(&scard, btTrades, idx)

	// 最大回撤
	peak, maxDD := capital, 0.0
	for _, e := range equity {
		if e.Value > peak {
			peak = e.Value
		}
		if peak > 0 {
			if dd := (peak - e.Value) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	mkt := (idx[endDate]/idx[dates[0]] - 1) * 100

	res.StartDate, res.EndDate = dates[0], endDate
	res.FinalEquity = round2(final)
	res.ReturnPct = round2((final/capital - 1) * 100)
	res.MaxDrawdown = round2(maxDD)
	res.Benchmark = round2(mkt)
	res.Excess = round2((final/capital-1)*100 - mkt)
	res.Cash = round2(cash)
	res.ClosedTrades = scard.TotalTrades
	res.WinRate = scard.WinRate
	res.Expectancy = scard.AvgReturn
	res.PayoffRatio = scard.PayoffRatio
	res.ProfitFactor = scard.ProfitFactor
	res.AvgHoldDays = scard.AvgHoldDays
	res.Equity = equity
	return res
}
