package services

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// 行业黑名单（与实盘扫描器 RunLowBuyScannerV1 一致：剔除低弹性/规避行业）
var lowBuyBlockIndustryKeywords = []string{"地产", "农业", "林业", "建筑", "零售", "百货"}

var (
	lowBuyIndustryMap  map[string]string
	lowBuyIndustryOnce sync.Once
)

// lowBuyIndustryBlocked 镜像实盘的行业黑名单过滤。
func lowBuyIndustryBlocked(code string) bool {
	lowBuyIndustryOnce.Do(func() { lowBuyIndustryMap = loadIndustryMap() })
	ind := lowBuyIndustryMap[strings.ToLower(code)]
	if ind == "" {
		return false
	}
	for _, kw := range lowBuyBlockIndustryKeywords {
		if strings.Contains(ind, kw) {
			return true
		}
	}
	return false
}

// 卖出纪律阈值（与低吸 SellPointHint/StopLossHint 及盘中监控一致）
const (
	btStopLossPct   = -5.0
	btTakeProfitPct = 15.0
	btTurnoverExit  = 12.0
	btTimeStopDays  = 5
	btTimeStopGain  = 3.0
)

type btRow struct {
	date                                                                  string
	open, high, low, close                                                float64
	turnover, mainNet, mainPct, totalCap, pct, ma10, ma20, amount, volume float64
	hasMain                                                               bool
}

// marketState 某交易日的大盘环境（从全市场截面算出）。
type marketState struct {
	amount    float64 // 两市成交额(元)
	limitUp   int     // 涨停近似(涨幅>=9.8%)
	limitDown int     // 跌停近似(跌幅<=-9.8%)
	breadth   float64 // 小盘股(<=100亿)收盘站上MA20的比例%
}

// computeMarketStates 扫描全部行，逐日汇总大盘环境指标。
func computeMarketStates(series map[string]btSeries, dateSet map[string]bool) map[string]*marketState {
	m := make(map[string]*marketState, len(dateSet))
	smallTotal := make(map[string]int)
	smallAbove := make(map[string]int)
	for _, ser := range series {
		for _, r := range ser.rows {
			if !dateSet[r.date] {
				continue
			}
			st := m[r.date]
			if st == nil {
				st = &marketState{}
				m[r.date] = st
			}
			st.amount += r.amount
			if r.pct >= 9.8 {
				st.limitUp++
			} else if r.pct <= -9.8 {
				st.limitDown++
			}
			if r.totalCap > 0 && r.totalCap <= 100e8 && r.close > 0 && r.ma20 > 0 {
				smallTotal[r.date]++
				if r.close > r.ma20 {
					smallAbove[r.date]++
				}
			}
		}
	}
	for d, st := range m {
		if smallTotal[d] > 0 {
			st.breadth = float64(smallAbove[d]) / float64(smallTotal[d]) * 100
		}
	}
	return m
}

// gatePassSmart 改进版大盘闸门：小盘breadth>MA20 / 涨停>60 / 跌停<50 / 成交额>2.0万亿，动态判定。
// 阈值按最近半年(125交易日)真实分布校准：涨停p25=67、跌停p90=45、成交额p25=2万亿。
func gatePassSmart(st *marketState) bool {
	if st == nil {
		return true
	}
	gateScore, valid := 0, 4
	if st.breadth > 0 {
		if st.breadth >= 50 {
			gateScore++
		}
	} else {
		valid--
	}
	if st.limitUp > 60 {
		gateScore++
	}
	if st.limitDown < 50 {
		gateScore++
	}
	if st.amount > 2e12 {
		gateScore++
	} else if st.amount <= 0 {
		valid--
	}
	if valid < 2 {
		valid = 2
	}
	required := 3
	if valid <= 3 {
		required = 2
	}
	return gateScore >= required
}

type btSeries struct {
	rows []btRow
	idx  map[string]int // date -> 行下标
	name string
}

type cand struct {
	code  string
	score float64
}

// dayCandidates 某交易日按低吸完整规则选出的 TopN 候选（市值/板块/ST/行业黑名单 + 4选3 + 评分）。
func dayCandidates(series map[string]btSeries, signalDate string, maxCap float64, topN int) []cand {
	return dayCandidatesF(series, signalDate, maxCap, topN, nil)
}

// dayCandidatesF 同 dayCandidates，extra 为可选额外过滤（返回 false 则剔除），用于做"加码维度"敏感性回测。
func dayCandidatesF(series map[string]btSeries, signalDate string, maxCap float64, topN int, extra func(btRow) bool) []cand {
	cands := make([]cand, 0, 256)
	for code, ser := range series {
		if isLowBuyBlockedBoardCode(code) {
			continue
		}
		if strings.Contains(strings.ToUpper(ser.name), "ST") {
			continue
		}
		if lowBuyIndustryBlocked(code) { // 行业黑名单（与实盘一致）
			continue
		}
		ri, ok := ser.idx[signalDate]
		if !ok {
			continue
		}
		row := ser.rows[ri]
		if maxCap > 0 && (row.totalCap <= 0 || row.totalCap > maxCap) {
			continue
		}
		if extra != nil && !extra(row) {
			continue
		}
		if lowBuyExtraFilter != nil && !lowBuyExtraFilter(row) { // 测试用全局额外过滤（让组合引擎也能叠加维度）
			continue
		}
		if score, passed := evaluateLowBuyRow(row); passed {
			cands = append(cands, cand{code, score})
		}
	}
	// 评分降序；并列时按代码升序，保证结果确定可复现（避免 map 遍历顺序影响 TopN）
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

// RunBacktest 用低吸规则在历史截面上选股，并按卖出纪律模拟买卖，输出胜率等统计。
func (s *HistoryService) RunBacktest(req models.BacktestRequest) models.BacktestResult {
	res := models.BacktestResult{ByReason: map[string]int{}, Status: "running"}
	if s == nil || s.db == nil {
		res.Status = "failed"
		res.Message = "history db not ready"
		return res
	}
	days := req.Days
	if days <= 0 || days > 520 {
		days = 250
	}
	topN := req.TopN
	if topN <= 0 {
		topN = 5
	}
	entryRule := req.EntryRule
	if entryRule == "" {
		entryRule = "next_open"
	}
	if req.MaxChangePct != nil {
		SetLowBuyMaxPct(*req.MaxChangePct) // 用调用方传入的涨幅上限（与扫描器一致）
	}
	cfg := exitCfgFrom(req)

	// 1) 取窗口内的交易日（最近 days 天）
	dates, err := s.recentTradeDates(days)
	if err != nil || len(dates) < 10 {
		res.Status = "failed"
		res.Message = fmt.Sprintf("交易日不足（%d），无法回测", len(dates))
		return res
	}
	res.StartDate, res.EndDate, res.TradingDays = dates[0], dates[len(dates)-1], len(dates)
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}

	// 2) 载入窗口内全部行到内存（按代码分组）
	series, err := s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		res.Status = "failed"
		res.Message = "载入历史失败: " + err.Error()
		return res
	}

	// 3) 逐日选股 + 模拟
	trades := make([]models.BacktestTrade, 0, 1024)
	for di := 0; di < len(dates)-1; di++ { // 需要 di+1 作为入场日
		signalDate := dates[di]
		cands := dayCandidates(series, signalDate, req.MaxMarketCap, topN)
		for _, c := range cands {
			t := s.simulateTrade(series[c.code], c.code, signalDate, di, dates, entryRule, cfg, c.score)
			if t != nil {
				trades = append(trades, *t)
			}
		}
	}

	s.aggregate(&res, trades, buildMarketIndex(series, dates, dateSet))
	res.Status = "success"
	if res.TotalTrades == 0 {
		res.Message = "窗口内没有任何标的通过低吸规则（可能历史字段未补齐）"
	} else {
		res.Message = fmt.Sprintf("共 %d 笔交易，胜率 %.1f%%，平均收益 %.2f%%", res.TotalTrades, res.WinRate, res.AvgReturn)
	}
	return res
}

// exitState 跨日维护的离场状态（连续破线计数）。
type exitState struct {
	belowMA10 int
	belowMA20 int
}

// exitCfg 离场参数（止盈/止损可调）。
type exitCfg struct {
	rule          string
	stopLossPct   float64 // 负数
	takeProfitPct float64 // 正数
	halfTP        bool    // +15% 止盈减半（余仓继续按其余纪律跑），低吸 fast 默认开
	buyCostRate   float64 // 买入成本率（佣金+滑点）
	sellCostRate  float64 // 卖出成本率（佣金+印花税+滑点）
}

// 真实交易成本（A股散户口径）：
//
//	佣金 万2.5（双边各收），印花税 千1.0（卖出单边），滑点 千1.0（双边各按一档）
const (
	btCommissionRate = 0.00025 // 佣金 万2.5 单边
	btStampRate      = 0.0010  // 印花税 千1.0 卖出单边
	btSlippageRate   = 0.0010  // 滑点 千1.0 单边
)

// 测试用开关（仅 A/B 对比"旧理想化"口径，生产恒为 false）
var (
	btTestNoCost bool
	btTestNoHalf bool
)

// lowBuyExtraFilter 测试用：给组合引擎(RunPortfolioBacktest)叠加额外选股过滤，生产恒为 nil。
var lowBuyExtraFilter func(btRow) bool

// exitCfgFrom 由请求构造离场参数（带 fast/patient 默认值，可被显式参数覆盖）。
func exitCfgFrom(req models.BacktestRequest) exitCfg {
	c := exitCfg{rule: req.SellRule, stopLossPct: btStopLossPct, takeProfitPct: btTakeProfitPct}
	if c.rule == "" {
		c.rule = "fast"
	}
	if c.rule == "patient" {
		c.stopLossPct = -8.0
	}
	if req.StopLossPct < 0 {
		c.stopLossPct = req.StopLossPct
	}
	if req.TakeProfitPct > 0 {
		c.takeProfitPct = req.TakeProfitPct
	}
	// 低吸纪律：+15% 止盈减半（余仓续跑），耐心模式不减半
	c.halfTP = c.rule != "patient"
	// 交易成本：默认真实散户口径；若请求显式给了往返成本，则全部记到卖出端（买入端置0，避免重复）
	c.buyCostRate = btCommissionRate + btSlippageRate               // 买入：佣金+滑点
	c.sellCostRate = btCommissionRate + btStampRate + btSlippageRate // 卖出：佣金+印花税+滑点
	if req.CostPct > 0 {
		c.buyCostRate = 0
		c.sellCostRate = req.CostPct / 100
	}
	// 测试开关：复现"旧理想化"口径做 A/B（生产默认 false）
	if btTestNoCost {
		c.buyCostRate, c.sellCostRate = 0, 0
	}
	if btTestNoHalf {
		c.halfTP = false
	}
	return c
}

// evalExit 判定某根K线是否触发离场，返回原因与离场价（空原因=继续持有）。
//
//	fast   ：止损/止盈(可调) / 换手>12% / 破MA10连续2日 / 5日<3%时间止损
//	patient：止损/止盈(可调) / 破MA20连续2日 / 10日<3%时间止损（给回踩时间）
func evalExit(entry float64, r btRow, hold int, st *exitState, cfg exitCfg) (string, float64) {
	slLine := entry * (1 + cfg.stopLossPct/100)
	tpLine := entry * (1 + cfg.takeProfitPct/100)
	if r.low > 0 && r.low <= slLine {
		return "stop_loss", slLine
	}
	// halfTP 模式下 +15% 不全清（由 simulateTrade 减半处理），仅非减半模式才整笔止盈
	if !cfg.halfTP && r.high > 0 && r.high >= tpLine {
		return "take_profit", tpLine
	}
	if cfg.rule == "patient" {
		if r.ma20 > 0 && r.close < r.ma20 {
			st.belowMA20++
		} else {
			st.belowMA20 = 0
		}
		if st.belowMA20 >= 2 {
			return "ma20", r.close
		}
		if hold >= 10 && (r.close-entry)/entry*100 < btTimeStopGain {
			return "time_stop", r.close
		}
		return "", 0
	}
	// fast
	if r.turnover > btTurnoverExit {
		return "turnover", r.close
	}
	if r.ma10 > 0 && r.close < r.ma10 {
		st.belowMA10++
	} else {
		st.belowMA10 = 0
	}
	if st.belowMA10 >= 2 {
		return "ma10", r.close
	}
	if hold >= btTimeStopDays && (r.close-entry)/entry*100 < btTimeStopGain {
		return "time_stop", r.close
	}
	return "", 0
}

type openPos struct {
	investAmount float64
	entryPrice   float64
	entryDate    string
	hold         int
	st           *exitState
	score        float64
	halfTaken    bool    // 是否已在 +15% 减半
	tpExitPrice  float64 // 减半成交价（用于最终 blended 收益记录）
	// 策略型 profile 状态（仅 profile 驱动的策略账户用）
	signalHigh   float64 // 启动信号日最高（半分位止损）
	signalLow    float64 // 启动信号日最低
	maxClose     float64 // 入场后最高收盘（新高加仓判定）
	addsDone     int     // 已加仓次数
	beArmed      bool    // 保本止损是否已上移
	baseInvest   float64 // 初始投入（加仓基数）
	peakHigh     float64 // 入场后最高价（通用风控移动止损用）
}

// RunPortfolioBacktest 真实组合模拟：固定资金、最多同时持 MaxPositions 只、等权，
// 逐日盯市出真实净值曲线与最大回撤（回答"低回撤"能否成立）。
func (s *HistoryService) RunPortfolioBacktest(req models.BacktestRequest) models.BacktestResult {
	res := models.BacktestResult{ByReason: map[string]int{}, Status: "running"}
	if s == nil || s.db == nil {
		res.Status = "failed"
		res.Message = "history db not ready"
		return res
	}
	days := req.Days
	if days <= 0 || days > 520 {
		days = 250
	}
	topN := req.TopN
	if topN <= 0 {
		topN = 5
	}
	maxPos := req.MaxPositions
	if maxPos <= 0 {
		maxPos = 5
	}
	entryRule := req.EntryRule
	if entryRule == "" {
		entryRule = "next_open"
	}
	cfg := exitCfgFrom(req)

	dates, err := s.recentTradeDates(days)
	if err != nil || len(dates) < 10 {
		res.Status = "failed"
		res.Message = "交易日不足"
		return res
	}
	res.StartDate, res.EndDate, res.TradingDays = dates[0], dates[len(dates)-1], len(dates)
	dateSet := make(map[string]bool, len(dates))
	for _, d := range dates {
		dateSet[d] = true
	}
	series, err := s.loadSeries(dates[0], dates[len(dates)-1], dateSet)
	if err != nil {
		res.Status = "failed"
		res.Message = "载入历史失败: " + err.Error()
		return res
	}

	var states map[string]*marketState
	if req.GateMode == "smart" {
		states = computeMarketStates(series, dateSet)
	}

	costMul := 1.0 - req.CostPct/100 // 往返成本（佣金+印花税+滑点）
	cash := 1.0
	positions := make(map[string]*openPos)
	var pending []cand
	equity := make([]float64, len(dates))
	trades := make([]models.BacktestTrade, 0, 512)

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

	for di, date := range dates {
		// 1) 执行昨日信号的挂单：今日开盘入场
		for _, c := range pending {
			if len(positions) >= maxPos {
				break
			}
			if _, held := positions[c.code]; held {
				continue
			}
			ser := series[c.code]
			ri, ok := ser.idx[date]
			if !ok {
				continue
			}
			entry := ser.rows[ri].open
			if entryRule == "close" || entry <= 0 {
				entry = ser.rows[ri].close
			}
			if entry <= 0 {
				continue
			}
			invest := markEquity(date) / float64(maxPos)
			if invest > cash {
				invest = cash
			}
			if invest <= 1e-9 {
				continue
			}
			cash -= invest
			positions[c.code] = &openPos{investAmount: invest, entryPrice: entry, entryDate: date, st: &exitState{}, score: c.score}
		}
		pending = nil

		// 2) 持仓离场判定（入场当日不判）
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
			r := ser.rows[ri]
			// +15% 减半（仅一次）：卖出半仓回款，余半仓继续按纪律跑（与逐笔引擎一致）
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
				if p.halfTaken { // 两腿份额加权：半仓在 +15%、半仓在 price
					tpRet := (p.tpExitPrice/p.entryPrice*costMul - 1) * 100
					ret = 0.5*tpRet + 0.5*ret
					reason = "half_" + reason
				}
				trades = append(trades, models.BacktestTrade{
					Code: code, Name: ser.name, EntryDate: p.entryDate, EntryPrice: round2(p.entryPrice),
					ExitDate: date, ExitPrice: round2(price), HoldDays: p.hold,
					ReturnPct: round2(ret), ExitReason: reason, Score: p.score,
				})
				delete(positions, code)
			}
		}

		// 3) 盯市
		equity[di] = markEquity(date)

		// 4) 收盘出信号：same_close=当天尾盘收盘价直接进场；否则挂下一日开盘入场单
		gateOK := states == nil || gatePassSmart(states[date])
		if di < len(dates)-1 && len(positions) < maxPos && gateOK {
			slots := maxPos - len(positions)
			added := 0
			for _, c := range dayCandidates(series, date, req.MaxMarketCap, topN) {
				if _, held := positions[c.code]; held {
					continue
				}
				if entryRule == "same_close" {
					ser := series[c.code]
					ri, ok := ser.idx[date]
					if !ok {
						continue
					}
					entry := ser.rows[ri].close
					if entry <= 0 {
						continue
					}
					invest := markEquity(date) / float64(maxPos)
					if invest > cash {
						invest = cash
					}
					if invest <= 1e-9 {
						continue
					}
					cash -= invest
					positions[c.code] = &openPos{investAmount: invest, entryPrice: entry, entryDate: date, st: &exitState{}, score: c.score}
				} else {
					pending = append(pending, c)
				}
				added++
				if added >= slots {
					break
				}
			}
		}
	}

	// 收尾：最后一日收盘平掉剩余持仓
	lastDate := dates[len(dates)-1]
	for code, p := range positions {
		ser := series[code]
		if ri, ok := ser.idx[lastDate]; ok {
			price := ser.rows[ri].close
			cash += p.investAmount * price / p.entryPrice * costMul
			ret := (price/p.entryPrice*costMul - 1) * 100
			reason := "window_end"
			if p.halfTaken {
				tpRet := (p.tpExitPrice/p.entryPrice*costMul - 1) * 100
				ret = 0.5*tpRet + 0.5*ret
				reason = "half_window_end"
			}
			trades = append(trades, models.BacktestTrade{
				Code: code, Name: ser.name, EntryDate: p.entryDate, EntryPrice: round2(p.entryPrice),
				ExitDate: lastDate, ExitPrice: round2(price), HoldDays: p.hold,
				ReturnPct: round2(ret), ExitReason: reason, Score: p.score,
			})
		}
	}

	s.aggregate(&res, trades, buildMarketIndex(series, dates, dateSet))
	// 用真实组合净值覆盖回撤/累计收益
	peak, maxDD := equity[0], 0.0
	for _, e := range equity {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			if dd := (peak - e) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	res.MaxDrawdown = round2(maxDD)
	res.TotalReturn = round2((equity[len(equity)-1] - 1.0) * 100)
	res.Status = "success"
	res.Message = fmt.Sprintf("组合模拟：%d笔，胜率%.1f%%，累计收益%.1f%%，最大回撤%.1f%%",
		res.TotalTrades, res.WinRate, res.TotalReturn, res.MaxDrawdown)
	return res
}

// simulateTrade 入场后按卖出纪律逐日推演到离场。
// entryRule: next_open=次日开盘(默认) | close=次日收盘 | same_close=当天尾盘收盘(信号日收盘价进场)
func (s *HistoryService) simulateTrade(ser btSeries, code, signalDate string, di int, dates []string, entryRule string, cfg exitCfg, score float64) *models.BacktestTrade {
	var entryDate string
	var ei int
	var ok bool
	var entry float64
	if entryRule == "same_close" {
		// 当天尾盘(14:30-15:00)按信号日收盘价分批买入，离场从次日起算
		entryDate = signalDate
		ei, ok = ser.idx[entryDate]
		if !ok {
			return nil
		}
		entry = ser.rows[ei].close
	} else {
		entryDate = dates[di+1]
		ei, ok = ser.idx[entryDate]
		if !ok {
			return nil
		}
		entry = ser.rows[ei].open
		if entryRule == "close" || entry <= 0 {
			entry = ser.rows[ei].close
		}
	}
	if entry <= 0 {
		return nil
	}

	st := &exitState{}
	halfTaken := false
	tpLine := entry * (1 + cfg.takeProfitPct/100)
	for hold := 1; ei+hold < len(ser.rows); hold++ {
		r := ser.rows[ei+hold]
		// +15% 减半：仅触发一次，余仓继续持有按其余纪律跑
		if cfg.halfTP && !halfTaken && r.high > 0 && r.high >= tpLine {
			halfTaken = true
		}
		if reason, price := evalExit(entry, r, hold, st, cfg); reason != "" {
			return s.buildTrade(code, ser.name, signalDate, entryDate, entry, r.date, price, hold, reason, score, cfg, halfTaken, tpLine)
		}
	}

	// 窗口结束仍未离场：按最后一根收盘平仓
	if len(ser.rows)-1 <= ei {
		return nil
	}
	last := ser.rows[len(ser.rows)-1]
	return s.buildTrade(code, ser.name, signalDate, entryDate, entry, last.date, last.close, len(ser.rows)-1-ei, "window_end", score, cfg, halfTaken, tpLine)
}

// tradeNet 计算扣双边成本后的净收益%与展示用离场价。
// halfTaken=true：一半仓在 tpLine(+15%) 先减，另一半在 exitPrice 离场，份额加权。
func tradeNet(entry, exitPrice float64, cfg exitCfg, halfTaken bool, tpLine float64) (net, dispExit float64) {
	buyOutlay := entry * (1 + cfg.buyCostRate) // 含买入成本的实际支出
	var proceeds float64
	if halfTaken {
		proceeds = 0.5*tpLine*(1-cfg.sellCostRate) + 0.5*exitPrice*(1-cfg.sellCostRate)
		dispExit = 0.5*tpLine + 0.5*exitPrice
	} else {
		proceeds = exitPrice * (1 - cfg.sellCostRate)
		dispExit = exitPrice
	}
	net = (proceeds - buyOutlay) / buyOutlay * 100 // 扣双边成本后的净收益%
	return net, dispExit
}

// buildTrade 由 tradeNet 计算净收益并组装一笔交易记录。
func (s *HistoryService) buildTrade(code, name, signalDate, entryDate string, entry float64, exitDate string, exitPrice float64, holdDays int, reason string, score float64, cfg exitCfg, halfTaken bool, tpLine float64) *models.BacktestTrade {
	net, dispExit := tradeNet(entry, exitPrice, cfg, halfTaken, tpLine)
	if halfTaken {
		reason = "half_" + reason // 标记该笔为"减半离场"
	}
	return &models.BacktestTrade{
		Code: code, Name: name, SignalDate: signalDate,
		EntryDate: entryDate, EntryPrice: round2(entry),
		ExitDate: exitDate, ExitPrice: round2(dispExit),
		HoldDays: holdDays, ReturnPct: round2(net), ExitReason: reason, Score: score,
	}
}

// PaperExitCfg 返回与低吸回测一致的退出参数（fast：减半+真实成本）。
func PaperExitCfg() exitCfg { return exitCfgFrom(models.BacktestRequest{SellRule: "fast"}) }

// PaperCostRates 暴露真实成本率给模拟持仓做盈亏/胜率口径对齐。
func PaperCostRates() (buy, sell float64) {
	c := PaperExitCfg()
	return c.buyCostRate, c.sellCostRate
}

// PaperExitResult 模拟持仓前向退出推演结果。
type PaperExitResult struct {
	Exited    bool
	ExitDate  string
	NetPct    float64 // 扣成本净收益%
	ExitPrice float64 // 展示用（减半为两腿加权）的离场价
	Reason    string
	HoldDays  int
}

// SimulatePaperExit 对一笔模拟持仓按低吸退出纪律用真实前向日K推演是否已离场。
// entry=实际买入价（成本价）；entryDate=建仓日；klines=升序日K（需含该建仓日及之后，带 MA10）。
// 仅在“已确认收盘”的历史K线上判定（调用方负责剔除当日盘中未收盘的最后一根）。
// 注：换手>12% 一条需流通股本，日K缺该字段，模拟持仓暂不计该条（回测仍计）。
func SimulatePaperExit(entry float64, entryDate string, klines []models.KLineData) PaperExitResult {
	if entry <= 0 || len(klines) == 0 {
		return PaperExitResult{}
	}
	cfg := PaperExitCfg()
	start := -1
	for i, k := range klines {
		if k.Time == entryDate {
			start = i
			break
		}
	}
	if start < 0 {
		// 建仓日不在K线窗口（可能太久或停牌），无法判定
		return PaperExitResult{}
	}
	st := &exitState{}
	halfTaken := false
	tpLine := entry * (1 + cfg.takeProfitPct/100)
	for hold := 1; start+hold < len(klines); hold++ {
		k := klines[start+hold]
		r := btRow{date: k.Time, open: k.Open, high: k.High, low: k.Low, close: k.Close, ma10: k.MA10}
		if cfg.halfTP && !halfTaken && r.high > 0 && r.high >= tpLine {
			halfTaken = true
		}
		if reason, price := evalExit(entry, r, hold, st, cfg); reason != "" {
			net, disp := tradeNet(entry, price, cfg, halfTaken, tpLine)
			if halfTaken {
				reason = "half_" + reason
			}
			return PaperExitResult{Exited: true, ExitDate: k.Time, NetPct: round2(net), ExitPrice: round2(disp), Reason: reason, HoldDays: hold}
		}
	}
	return PaperExitResult{} // 尚未触发离场
}

// lowBuyBacktestMaxPct 低吸回测的当日涨幅上限（实盘扫描器为 +1.5%，可调以做敏感性回测）。
var lowBuyBacktestMaxPct = 1.5

// SetLowBuyMaxPct 设置低吸回测的涨幅上限（用于对比不同阈值）。
func SetLowBuyMaxPct(v float64) { lowBuyBacktestMaxPct = v }

// evaluateLowBuyRow 镜像 RunLowBuyScannerV1 的个股规则（硬过滤 + 4选3触发 + 评分）。
func evaluateLowBuyRow(r btRow) (float64, bool) {
	// 市值硬筛：与实盘扫描器一致，仅 20~100 亿
	if r.totalCap < 20e8 || r.totalCap > 100e8 {
		return 0, false
	}
	// 硬过滤：涨幅 [-3%, 上限]、换手 ≤8%（之前回测漏了上限，这里补上与实盘一致）
	if r.pct < -3.0 || r.pct > lowBuyBacktestMaxPct || r.turnover > 8.0 {
		return 0, false
	}
	if !r.hasMain {
		return 0, false
	}
	if r.mainPct < 1.0 || r.mainNet < 8e6 {
		return 0, false
	}
	// 4选3触发
	trig := 0
	if r.turnover > 0 && r.turnover <= 2.5 {
		trig++
	}
	if r.mainNet > 0 {
		trig++
	}
	if r.mainPct >= 3.0 {
		trig++
	}
	if r.pct >= -2.0 {
		trig++
	}
	if trig < 3 {
		return 0, false
	}
	// 评分（与扫描器一致）
	score := 50.0
	score += clampF(r.mainPct*3.0, -8, 14)
	score += clampF((3.0-r.turnover)*3.0, -6, 10)
	switch {
	case r.totalCap <= 40e8:
		score += 12
	case r.totalCap <= 60e8:
		score += 10
	case r.totalCap <= 80e8:
		score += 7
	default:
		score += 4
	}
	// 涨跌结构分（与实盘扫描器一致：强烈偏好微跌回踩[-1,0)）
	switch {
	case r.pct >= -1.0 && r.pct < 0:
		score += 12
	case r.pct >= -2.0 && r.pct < -1.0:
		score += 4
	case r.pct >= 0 && r.pct < 1.0:
		score -= 2
	case r.pct >= 1.0:
		score -= 5
	}
	if r.mainNet >= 2e7 {
		score += 8
	} else if r.mainNet >= 1e7 {
		score += 5
	} else if r.mainNet >= 8e6 {
		score += 2
	}
	if r.turnover > 6 {
		score -= 4
	} else if r.turnover > 4 {
		score -= 2
	}
	score = math.Round(clampF(score, 0, 100)*10) / 10
	return score, true
}

// recentTradeDates 取最近 n 个交易日（升序）。
func (s *HistoryService) recentTradeDates(n int) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT trade_date FROM stock_daily ORDER BY trade_date DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, n)
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && d != "" {
			out = append(out, d)
		}
	}
	// 反转为升序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// latestCompleteTradeDate 返回最近一个"采集完整"的交易日(该日股票数 >= minCount)。
// 当天采集若被打断/进行中只入库了少量股票，则自动回退到上一完整交易日。
func (s *HistoryService) latestCompleteTradeDate(minCount int) (string, error) {
	if minCount <= 0 {
		minCount = 1000
	}
	rows, err := s.db.Query(`SELECT trade_date FROM stock_daily
		GROUP BY trade_date HAVING COUNT(DISTINCT stock_code) >= ?
		ORDER BY trade_date DESC LIMIT 1`, minCount)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return "", err
		}
		return d, nil
	}
	// 没有任何"完整"日则退回最新日
	dates, err := s.recentTradeDates(1)
	if err != nil || len(dates) == 0 {
		return "", err
	}
	return dates[len(dates)-1], nil
}

// loadSeries 载入窗口内全部行，按代码分组、按日期升序。
func (s *HistoryService) loadSeries(start, end string, dateSet map[string]bool) (map[string]btSeries, error) {
	rows, err := s.db.Query(`SELECT stock_code, stock_name, trade_date,
		open_price, high_price, low_price, close_price, turnover, main_net, main_pct, total_market_cap, pct_change, ma10, ma20, amount, volume
		FROM stock_daily WHERE trade_date >= ? AND trade_date <= ? ORDER BY stock_code, trade_date`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]btSeries, 5000)
	for rows.Next() {
		var code, name, date string
		var open, high, low, close, turnover, mainNet, mainPct, totalCap, pct, ma10, ma20, amount, volume sql.NullFloat64
		var nameN sql.NullString
		if err := rows.Scan(&code, &nameN, &date, &open, &high, &low, &close, &turnover, &mainNet, &mainPct, &totalCap, &pct, &ma10, &ma20, &amount, &volume); err != nil {
			continue
		}
		if !dateSet[date] || !close.Valid || close.Float64 <= 0 {
			continue
		}
		if nameN.Valid && nameN.String != "" {
			name = nameN.String
		}
		r := btRow{
			date:     date,
			open:     open.Float64,
			high:     high.Float64,
			low:      low.Float64,
			close:    close.Float64,
			turnover: turnover.Float64,
			mainNet:  mainNet.Float64,
			mainPct:  mainPct.Float64,
			totalCap: totalCap.Float64,
			pct:      pct.Float64,
			ma10:     ma10.Float64,
			ma20:     ma20.Float64,
			amount:   amount.Float64,
			volume:   volume.Float64,
			hasMain:  mainNet.Valid,
		}
		ser := out[code]
		ser.rows = append(ser.rows, r)
		if name != "" {
			ser.name = name
		}
		out[code] = ser
	}
	// 建日期索引
	for code, ser := range out {
		ser.idx = make(map[string]int, len(ser.rows))
		for i, r := range ser.rows {
			ser.idx[r.date] = i
		}
		out[code] = ser
	}
	return out, nil
}

// aggregate 计算统计指标。
// BatchLowBuyReplay 区间内逐日跑低吸选股，按机械纪律持有，分年+合计统计胜率/期望/赔率/超额alpha。
func (s *HistoryService) BatchLowBuyReplay(start, end string, topN int, maxChangePct float64) models.LowBuyBatchResult {
	res := models.LowBuyBatchResult{Start: start, End: end, TopN: topN}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	if topN <= 0 {
		topN = 3
	}
	res.TopN = topN
	SetLowBuyMaxPct(maxChangePct)
	cfg := exitCfgFrom(models.BacktestRequest{SellRule: "fast"})

	t0, err := time.Parse("2006-01-02", start)
	if err != nil {
		res.Warning = "起始日期格式错误"
		return res
	}
	loadStart := t0.AddDate(0, 0, -70).Format("2006-01-02")
	te, err2 := time.Parse("2006-01-02", end)
	loadEnd := end
	if err2 == nil {
		loadEnd = te.AddDate(0, 0, 40).Format("2006-01-02") // 让区间末尾的交易也能跑完持有期
	}
	dates, err := s.tradeDatesInRange(loadStart, loadEnd)
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
	idx := buildMarketIndex(series, dates, dateSet)

	allTrades := make([]models.BacktestTrade, 0, 1024)
	byYear := map[string][]models.BacktestTrade{}
	yearOrder := []string{}
	for di := 0; di < len(dates)-1; di++ {
		d := dates[di]
		if d < start || d > end {
			continue
		}
		yr := d[:4]
		if _, ok := byYear[yr]; !ok {
			byYear[yr] = []models.BacktestTrade{}
			yearOrder = append(yearOrder, yr)
		}
		for _, c := range dayCandidates(series, d, 0, topN) {
			if t := s.simulateTrade(series[c.code], c.code, d, di, dates, "next_open", cfg, c.score); t != nil {
				allTrades = append(allTrades, *t)
				byYear[yr] = append(byYear[yr], *t)
			}
		}
	}
	toRow := func(label string, trades []models.BacktestTrade) models.LowBuyBatchRow {
		r := models.BacktestResult{ByReason: map[string]int{}}
		s.aggregate(&r, trades, idx)
		return models.LowBuyBatchRow{
			Label: label, Trades: r.TotalTrades, WinRate: r.WinRate, Expectancy: r.AvgReturn,
			PayoffRatio: r.PayoffRatio, ProfitFactor: r.ProfitFactor, MaxLoss: r.MaxLossPct,
			Benchmark: r.BenchmarkPct, Excess: r.ExcessPct, AvgHold: r.AvgHoldDays,
		}
	}
	sort.Strings(yearOrder)
	for _, y := range yearOrder {
		res.Rows = append(res.Rows, toRow(y, byYear[y]))
	}
	res.Rows = append(res.Rows, toRow("合计", allTrades))
	if len(allTrades) == 0 {
		res.Warning = "该区间无低吸信号（历史字段可能未补齐）"
	}
	return res
}

// buildMarketIndex 构造"等权全A"基准：每个交易日全市场涨跌幅均值，累积成净值水平(起点1.0)。
// 用于算策略的超额收益(alpha)：基准=持有窗口内等权全A涨幅。
func buildMarketIndex(series map[string]btSeries, dates []string, dateSet map[string]bool) map[string]float64 {
	sum := make(map[string]float64, len(dates))
	cnt := make(map[string]int, len(dates))
	for _, ser := range series {
		for _, r := range ser.rows {
			if !dateSet[r.date] || r.close <= 0 {
				continue
			}
			sum[r.date] += r.pct
			cnt[r.date]++
		}
	}
	level := make(map[string]float64, len(dates))
	cur := 1.0
	for _, d := range dates {
		if cnt[d] > 0 {
			cur *= 1 + (sum[d]/float64(cnt[d]))/100
		}
		level[d] = cur
	}
	return level
}

// benchReturn 基准在 [entry, exit] 区间的涨幅%（等权全A）。
func benchReturn(level map[string]float64, entry, exit string) float64 {
	le, ok1 := level[entry]
	lx, ok2 := level[exit]
	if !ok1 || !ok2 || le <= 0 {
		return 0
	}
	return (lx/le - 1) * 100
}

func (s *HistoryService) aggregate(res *models.BacktestResult, trades []models.BacktestTrade, idxLevel map[string]float64) {
	res.TotalTrades = len(trades)
	if len(trades) == 0 {
		return
	}
	var sumRet, sumWin, sumLoss, sumHold, sumBench float64
	var winCnt, lossCnt int
	maxLoss := 0.0
	for _, t := range trades {
		sumRet += t.ReturnPct
		sumHold += float64(t.HoldDays)
		res.ByReason[t.ExitReason]++
		if idxLevel != nil {
			sumBench += benchReturn(idxLevel, t.EntryDate, t.ExitDate)
		}
		if t.ReturnPct < maxLoss {
			maxLoss = t.ReturnPct
		}
		if t.ReturnPct > 0 {
			winCnt++
			sumWin += t.ReturnPct
		} else {
			lossCnt++
			sumLoss += t.ReturnPct
		}
	}
	n := float64(len(trades))
	res.WinTrades = winCnt
	res.WinRate = round2(float64(winCnt) / n * 100)
	res.AvgReturn = round2(sumRet / n) // 期望值/笔
	if winCnt > 0 {
		res.AvgWin = round2(sumWin / float64(winCnt))
	}
	if lossCnt > 0 {
		res.AvgLoss = round2(sumLoss / float64(lossCnt))
	}
	if sumLoss != 0 {
		res.ProfitFactor = round2(sumWin / math.Abs(sumLoss))
	}
	if res.AvgLoss != 0 {
		res.PayoffRatio = round2(res.AvgWin / math.Abs(res.AvgLoss)) // 赔率
	}
	res.MaxLossPct = round2(maxLoss)
	if idxLevel != nil {
		res.BenchmarkPct = round2(sumBench / n)
		res.ExcessPct = round2(res.AvgReturn - res.BenchmarkPct) // 每笔超额(alpha)
	}
	res.AvgHoldDays = round2(sumHold / n)
	res.TotalReturn = round2(sumRet)

	// 等权逐笔权益最大回撤（按入场日排序）
	ordered := make([]models.BacktestTrade, len(trades))
	copy(ordered, trades)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].EntryDate < ordered[j].EntryDate })
	equity, peak, maxDD := 0.0, 0.0, 0.0
	for _, t := range ordered {
		equity += t.ReturnPct
		if equity > peak {
			peak = equity
		}
		if dd := peak - equity; dd > maxDD {
			maxDD = dd
		}
	}
	res.MaxDrawdown = round2(maxDD)

	// 明细按收益排序，截断前 200 条
	sort.Slice(trades, func(i, j int) bool { return trades[i].ReturnPct > trades[j].ReturnPct })
	if len(trades) > 200 {
		trades = trades[:200]
	}
	res.Trades = trades
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

func isLowBuyBlockedBoardCode(symbol string) bool {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) >= 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		code = code[2:]
	}
	// 剔除创业板(30x)与科创板(68x)，与实盘扫描器一致
	return strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68")
}
