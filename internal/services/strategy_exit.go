package services

// StrategyTradeProfile 一套可机械执行的"加—减—卖—止损+大盘闸"规则。
// 阈值均可按策略单独配置；step() 在持仓的每个交易日返回一个动作。
type StrategyTradeProfile struct {
	Name string

	// 止损
	HardStopPct      float64 // 硬止损(负数，如 -5)
	UseSignalMidStop bool    // 跌破启动阳线半分位
	BreakevenAtPct   float64 // 盈利达此值后止损上移到保本(0=关闭)

	// 强势离场
	GapFailExit      bool    // 次日高开冲板失败(高开+收阴)
	GapPct           float64 // 高开门槛(如 3)
	VolOpenBreak     bool    // 放量开板
	VolBreakMult     float64 // 量 ≥ N×5日均量
	VolBreakFallback float64 // 收盘 ≤ 最高×此值(如 0.97)
	MarketGate       bool    // 大盘情绪退潮则离场

	// 减仓/止盈
	UpperShadowHalf bool    // 长上影减半
	UpperShadowPct  float64 // 上影门槛(如 3)
	HalfTPPct       float64 // +X% 减半(如 15)
	TrailAfterHalf  bool    // 减半后跌破前一日低点跟踪止盈

	// 加仓(只补涨)
	AddOnLimitUp    bool    // 继续涨停加仓
	AddOnNewHighVol bool    // 突破新高+放量加仓
	AddVolMult      float64 // 新高加仓的放量门槛(如 1.5)
	AddFraction     float64 // 每次加仓投入=初始仓×此比例(如 0.5)
	MaxAdds         int     // 最多加仓次数

	// 到期
	TimeStopDays int // 持有 N 日兜底清仓

	// 入场可成交过滤
	EntrySkipSealedLimit bool // 信号日封死涨停(收在涨停价)则跳过——尾盘那笔买不进

	// 入场时点（默认信号日尾盘收盘价；游资类封死涨停买不进，改为次日可成交价）
	EntryNextOpen          bool    // 次日开盘价进场（不追板）
	EntryPullbackMaxGapPct float64 // >0 时仅在"次日高开≤此值"才进（回踩确认，自动剔除连板一字）
}

// isSealedLimitUp 判断某日是否"封死涨停"：主板涨停且收盘贴在当日最高（封板未开），
// 这种票尾盘按收盘价买不进，应排除入场。准涨停/盘中开过板的涨停(收盘<最高)则可成交。
func isSealedLimitUp(r btRow) bool {
	return r.pct >= btLimitUpPct && r.high > 0 && r.close >= r.high*0.998
}

// HotMoney7Profile 游资突破策略7 的离场/止损规则。
// 补仓经回测证伪：涨停加仓不可成交且降质，新高加仓买在脉冲顶 —— 两条都关（profile C 最优：
// 601笔 期望+0.63% PF1.32 回撤13.58% 超额+49.9%，全面优于带补仓）。保留离场/止损/涨停豁免/保本。
func HotMoney7Profile() *StrategyTradeProfile {
	return &StrategyTradeProfile{
		Name:             "游资突破7",
		HardStopPct:      -5,
		UseSignalMidStop: true,
		BreakevenAtPct:   8,
		GapFailExit:      true,
		GapPct:           3,
		VolOpenBreak:     true,
		VolBreakMult:     2.0,
		VolBreakFallback: 0.97,
		MarketGate:       true,
		UpperShadowHalf:  true,
		UpperShadowPct:   3,
		HalfTPPct:        15,
		TrailAfterHalf:   true,
		// 补仓全关（回测证伪，见上）
		AddOnLimitUp:    false,
		AddOnNewHighVol: false,
		MaxAdds:         0,
		TimeStopDays:    8,
		EntrySkipSealedLimit: false,
		// 信号日封死涨停买不进 → 账户回测改用"可成交的次日回踩"口径(高开≤3%)，
		// 显示诚实地板(超额约-48%)而非+89%的不可成交幻觉。真实2:30入场需实盘分时向前验证。
		EntryNextOpen:          true,
		EntryPullbackMaxGapPct: 3,
	}
}

const btLimitUpPct = 9.7 // 主板涨停近似阈值

// step 在持仓第 hold 日(对应 rows[ri])给出动作。
// 返回 action ∈ {"", "add", "half", "exit"}；exit/half 同时给出 reason 与成交价 price。
// rtPrice>0 时(实时)用于更激进的硬止损判定；回测传 0。marketWeak=该日大盘退潮。
func (p *StrategyTradeProfile) step(pos *openPos, rows []btRow, ri, hold int, rtPrice float64, marketWeak bool) (action, reason string, price float64) {
	if ri <= 0 || ri >= len(rows) {
		return "", "", 0
	}
	r := rows[ri]
	avg := pos.entryPrice // entryPrice 始终维护为加权平均成本
	if avg <= 0 {
		return "", "", 0
	}
	isNewHigh := pos.maxClose > 0 && r.close > pos.maxClose
	if r.close > pos.maxClose {
		pos.maxClose = r.close
	}
	limitUp := r.pct >= btLimitUpPct

	// 保本武装：盈利触及阈值后，把止损线上移到成本
	if p.BreakevenAtPct > 0 && !pos.beArmed && r.high >= avg*(1+p.BreakevenAtPct/100) {
		pos.beArmed = true
	}

	// 1) 硬止损 / 保本止损（实时价更激进）
	stopLine := avg * (1 + p.HardStopPct/100)
	if pos.beArmed && avg > stopLine {
		stopLine = avg
	}
	low := r.low
	if rtPrice > 0 && rtPrice < low {
		low = rtPrice
	}
	if low > 0 && low <= stopLine {
		return "exit", "stop_loss", stopLine
	}

	// 2) 跌破启动阳线半分位（结构止损）
	if p.UseSignalMidStop && pos.signalHigh > 0 {
		if mid := (pos.signalHigh + pos.signalLow) / 2; r.close < mid {
			return "exit", "mid_break", r.close
		}
	}

	avgVol5 := avgVolBefore(rows, ri, 5)

	// 涨停豁免：当日涨停跳过所有减仓/弱势信号（但仍允许加仓）
	if !(limitUp) {
		// 3) 高开冲板失败
		if p.GapFailExit {
			if pc := rows[ri-1].close; pc > 0 && r.open/pc-1 >= p.GapPct/100 && r.close < r.open {
				return "exit", "gap_fail", r.close
			}
		}
		// 4) 放量开板
		if p.VolOpenBreak && avgVol5 > 0 && r.volume >= avgVol5*p.VolBreakMult && r.high > 0 && r.close <= r.high*p.VolBreakFallback {
			return "exit", "vol_break", r.close
		}
		// 5) 大盘退潮
		if p.MarketGate && marketWeak {
			return "exit", "market_weak", r.close
		}
		// 6) 减半后跟踪：跌破前一日低点
		if p.TrailAfterHalf && pos.halfTaken {
			if pl := rows[ri-1].low; pl > 0 && r.close < pl {
				return "exit", "trail", r.close
			}
		}
		// 7) +15% 减半
		if p.HalfTPPct > 0 && !pos.halfTaken {
			if tp := avg * (1 + p.HalfTPPct/100); r.high >= tp {
				return "half", "take_profit", tp
			}
		}
		// 8) 长上影减半
		if p.UpperShadowHalf && !pos.halfTaken && r.high > 0 &&
			(r.high-r.close)/r.close >= p.UpperShadowPct/100 && r.close <= (r.high+r.low)/2 {
			return "half", "upper_shadow", r.close
		}
	}

	// 9) 加仓（只补涨）
	if pos.addsDone < p.MaxAdds {
		if p.AddOnLimitUp && limitUp {
			return "add", "add_limitup", r.close
		}
		if p.AddOnNewHighVol && isNewHigh && avgVol5 > 0 && r.volume >= avgVol5*p.AddVolMult {
			return "add", "add_newhigh", r.close
		}
	}

	// 10) 到期兜底
	if p.TimeStopDays > 0 && hold >= p.TimeStopDays {
		return "exit", "time_stop", r.close
	}
	return "", "", 0
}

// avgVolBefore 计算 rows[ri-n..ri-1] 的平均成交量（不含当日）。
func avgVolBefore(rows []btRow, ri, n int) float64 {
	sum, cnt := 0.0, 0
	for i := ri - n; i < ri; i++ {
		if i >= 0 && rows[i].volume > 0 {
			sum += rows[i].volume
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}

// computeWeakDays 标记"情绪退潮"交易日：当日涨停家数 < 0.6×近5日均值。
func computeWeakDays(series map[string]btSeries, dates []string) map[string]bool {
	limitUp := make(map[string]int, len(dates))
	for _, ser := range series {
		for _, r := range ser.rows {
			if r.pct >= btLimitUpPct {
				limitUp[r.date]++
			}
		}
	}
	weak := make(map[string]bool, len(dates))
	for i, d := range dates {
		if i < 5 {
			continue
		}
		sum := 0
		for j := i - 5; j < i; j++ {
			sum += limitUp[dates[j]]
		}
		avg := float64(sum) / 5
		if avg > 0 && float64(limitUp[d]) < avg*0.6 {
			weak[d] = true
		}
	}
	return weak
}
