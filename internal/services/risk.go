package services

import (
	"strings"

	"github.com/run-bigpig/jcp/internal/models"
)

// RiskProfile 通用风控线（稳健口径；短线紧、价值宽）。所有仓位按来源套用，不依赖各策略自身规则。
type RiskProfile struct {
	Name           string
	HardStopPct    float64 // 成本硬止损(负)
	BreakevenAtPct float64 // 盈利达此值→止损上移到成本(保本)
	TrailArmPct    float64 // 盈利达此值→启动移动止损
	TrailDropPct   float64 // 从峰值回落此值→走(让利润奔跑+锁定)
	TPPct          float64 // 止盈封顶(兜底)
	TimeStopDays   int     // 时间止损：持有N日
	TimeStopGain   float64 // 且涨幅<此值则清(0=不设时间止损)
	StructStop     bool    // 结构止损:收盘跌破"确认前低"(5日枢轴,滞后5日确认不重绘,与诚实面板同口径)
	MA5Exit        bool    // 收盘跌破5日线离场(涨停回调成文条款)
	EntryLowStop   bool    // 收盘跌破"信号日低点"离场(捉妖/三倍量成文;自动入盘建仓日=信号日,低点即建仓K最低价)
	PrevLowStop    bool    // 收盘跌破"建仓前一日低点"离场(尾盘6成文"昨日强势阳线低点")
}

// 稳健参数(用户选定)
var riskShortTerm = RiskProfile{Name: "短线", HardStopPct: -8, BreakevenAtPct: 8, TrailArmPct: 10, TrailDropPct: 6, TPPct: 25, TimeStopDays: 10, TimeStopGain: 3}
var riskValue = RiskProfile{Name: "价值", HardStopPct: -15, BreakevenAtPct: 12, TrailArmPct: 20, TrailDropPct: 12, TPPct: 40, TimeStopDays: 0}

// 综合评分仓位:价值宽线 + 结构止损(评分选股用状态门进,风控用同一结构语言出)
var riskComposite = RiskProfile{Name: "综合评分", HardStopPct: -15, BreakevenAtPct: 12, TrailArmPct: 20, TrailDropPct: 12, TPPct: 40, TimeStopDays: 0, StructStop: true}

// ===== 每策略专属纪律线(按策略性格定制,2026-07 用户要求精准化) =====
// 低吸系:买在超跌支撑,破位即错——止损紧(-5%)、反弹到 +15% 就是这类模式的肉,5日没动静清仓
// 低吸系:执行低吸规则 V1.2 原版纪律(-5%止损 / 破10日线次日不收回 / +15%减半余仓续跑 / 5日<3%清)。
// 盘中只挂 -5% 纯硬止损线(保本/移动置0关闭),已确认K上的判定走 SimulatePaperExit 老引擎,与规则说明一字不差。
var riskDipBuy = RiskProfile{Name: "低吸系(V1.2)", HardStopPct: -5, BreakevenAtPct: 0, TrailArmPct: 0, TrailDropPct: 0, TPPct: 15, TimeStopDays: 5, TimeStopGain: 3}

// 尾盘隔日系:买尾盘博次日冲高,持仓周期1-3天——最紧的线,快进快出
// 成文(尾盘6):止损-5%/次日冲高5-6个点止盈/乏力平开走/跌破昨日强阳低点离场
//
// ⚠️2026-07-27 用户定:硬止损 -3% → -5%。顺带消掉一处长期分歧——界面「规则要点」里
// 尾盘买入6 写的一直是"买入价-5%止损",引擎却按 -3% 执行,现在两边对上了。
// 代价要认:止盈封顶仍是 +6%,盈亏比从 1:2 降到约 1:1.2,同样胜率下单笔期望更薄;
// 且 -3% 时代平掉的历史样本与此后的不同口径,计分卡跨这条线比较要留意。
//
// ⚠️2026-07-31 用户定(方案A,保守放宽):保本武装 3%→4%,移动武装 4%→5%、回落 3%→4%。
// 起因是 07-30 三笔尾盘仓次日开盘被保本/移动止损全扫,原线太贴身:峰值 5% 时旧线落在
// +1.85%,正常回抽就破。新线在峰值 5% 时落在 +0.8%,仍锁小利但不至于被日内波动扫掉。
// **止盈封顶维持 +6% 不动**——武装线必须低于封顶才有意义:若把武装线也提到 6%,
// 价格到 6% 会先被止盈平掉,移动止损永远轮不到触发,等于把它废了。
// 注:保本/移动止损这两条**并不在尾盘6的成文条款里**(成文只有 -5%/+6%/1日<1%清/破昨强阳低点),
// 属于标定兜底参数,可调;真要严格对齐铁律应当整条删掉,用户选择保留并放宽。
var riskOvernight = RiskProfile{Name: "尾盘隔日", HardStopPct: -5, BreakevenAtPct: 4, TrailArmPct: 5, TrailDropPct: 4, TPPct: 6, TimeStopDays: 1, TimeStopGain: 1, PrevLowStop: true}

// 涨停回调:接力企稳,失败快走,成了吃一段
// 成文(涨停回调):跌破5日线先走(涨停阳线低点在建仓日之前拿不到精确价,以-6%硬线近似兜底)
var riskPullback = RiskProfile{Name: "涨停回调", HardStopPct: -6, BreakevenAtPct: 6, TrailArmPct: 10, TrailDropPct: 6, TPPct: 20, TimeStopDays: 5, TimeStopGain: 3, MA5Exit: true}

// 三倍量突破:放量突破失败要认,成了给足奔跑空间
// 成文(三倍量5):跌破突破阳线低点或买入价-5%,策略失效
var riskVolBreak = RiskProfile{Name: "三倍量", HardStopPct: -5, BreakevenAtPct: 6, TrailArmPct: 10, TrailDropPct: 7, TPPct: 25, TimeStopDays: 7, TimeStopGain: 3, EntryLowStop: true}

// 游资突破:强势接力,波动大,止损稍宽、利润目标更高
var riskHotMoney = RiskProfile{Name: "游资突破", HardStopPct: -7, BreakevenAtPct: 8, TrailArmPct: 12, TrailDropPct: 8, TPPct: 30, TimeStopDays: 5, TimeStopGain: 3}

// 捉妖系:妖股波动极大,线放最宽,靠移动止损锁利润
// 成文(捉妖9/10):跌破信号日低点或买入价-5%止损;利润靠移动止损锁,不靠扛
// 超跌起爆11(标定兜底,非成文):-6%硬止损/+8%保本/+12%移动(回撤8%)/+25%封顶/6日不涨4%清;
// 破起爆日低点=形态失效(EntryLowStop,自动入盘时建仓日≈信号日)。
var riskOversoldIgnite = RiskProfile{Name: "超跌起爆", HardStopPct: -6, BreakevenAtPct: 8, TrailArmPct: 12, TrailDropPct: 8, TPPct: 25, TimeStopDays: 6, TimeStopGain: 4, EntryLowStop: true}

// 涨停回踩12(照用户成文的交易纪律):结构止损=第三天最低下方1%(EntryLowStop),
// 同时 -4.5% 无条件退出;赚的是隔日承接溢价不是趋势,所以 2 日不破第三天高点就走(时间止损优先于价格)。
var riskLimitupRetrace = RiskProfile{Name: "涨停回踩", HardStopPct: -4.5, BreakevenAtPct: 3, TrailArmPct: 5, TrailDropPct: 3, TPPct: 7, TimeStopDays: 2, TimeStopGain: 0, EntryLowStop: true}

var riskMonster = RiskProfile{Name: "捉妖", HardStopPct: -5, BreakevenAtPct: 10, TrailArmPct: 15, TrailDropPct: 10, TPPct: 40, TimeStopDays: 8, TimeStopGain: 5, EntryLowStop: true}

// 波段:中线趋势仓,带结构止损(收盘破确认前低=趋势结构坏了,无条件走)
var riskWave = RiskProfile{Name: "波段", HardStopPct: -8, BreakevenAtPct: 8, TrailArmPct: 12, TrailDropPct: 8, TPPct: 30, TimeStopDays: 10, TimeStopGain: 3, StructStop: true}

// RiskProfileForSource 按来源选专属纪律线;旧口径别名一并映射;未知来源落短线通用兜底。
func RiskProfileForSource(source string) RiskProfile {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "lowbuy-v1", "lowbuy", "taillazy-v2", "taillazy", "dip-entry-v8", "dip-entry":
		return riskDipBuy // 低吸1 / 低吸2(尾盘懒人) / 低吸入场8
	case "tail-buy-v6", "tail-buy", "latechase-v3", "latechase":
		return riskOvernight // 尾盘买入6 / 尾盘强势3
	case "limit-pullback-v1":
		return riskPullback
	case "triple-volume-v5", "triple-volume":
		return riskVolBreak
	case "hot-money-v7", "hot-money":
		return riskHotMoney
	case "monster-v9", "monster", "捉妖策略6", "monster-v10", "捉妖策略10":
		return riskMonster
	case "oversold-ignite-v1", "oversold-ignite":
		return riskOversoldIgnite
	case "limitup-retrace-v12", "limitup-retrace":
		return riskLimitupRetrace
	case "wave-v1", "wave":
		return riskWave
	case "fund-value", "fund-boom":
		return riskValue
	case "综合评分", "composite":
		return riskComposite
	default:
		return riskShortTerm // 手动/草元残留/未知来源:通用稳健兜底
	}
}

// confirmedPivotLows 确认前低序列:5日枢轴低点,出现5日后才生效(历史不回改)。
// 生效前为 0(调用侧以 >0 判有效)。
func confirmedPivotLows(klines []models.KLineData) []float64 {
	const M = 5
	n := len(klines)
	out := make([]float64, n)
	cur := 0.0
	for t := 0; t < n; t++ {
		p := t - M
		if p >= M {
			isLow := true
			for j := p - M; j <= p+M && j < n; j++ {
				if klines[j].Low < klines[p].Low {
					isLow = false
					break
				}
			}
			if isLow {
				cur = klines[p].Low
			}
		}
		out[t] = cur
	}
	return out
}

// RiskStopLine 返回某仓位"当前应执行的止损价"(用于盘中实时判定与界面展示)。
// peakHigh=入场以来最高价(无则传成本)。
func (p RiskProfile) RiskStopLine(cost, peakHigh float64) (line float64, kind string) {
	line = cost * (1 + p.HardStopPct/100)
	kind = "硬止损"
	// BreakevenAtPct/TrailArmPct 为 0 = 该规则关闭(如低吸系走 V1.2 专属纪律,不叠保本/移动)
	if p.BreakevenAtPct > 0 && peakHigh >= cost*(1+p.BreakevenAtPct/100) && cost > line {
		line, kind = cost, "保本"
	}
	if p.TrailArmPct > 0 && peakHigh >= cost*(1+p.TrailArmPct/100) {
		if tl := peakHigh * (1 - p.TrailDropPct/100); tl > line {
			line, kind = tl, "移动止损"
		}
	}
	return line, kind
}

// EvaluateRiskExit 在已确认日K上按风控线判定离场（保本/移动止损/硬止损/止盈/时间止损）。
func EvaluateRiskExit(p RiskProfile, cost float64, entryDate string, klines []models.KLineData) PaperExitResult {
	if cost <= 0 || len(klines) == 0 {
		return PaperExitResult{}
	}
	start := -1
	for i, k := range klines {
		if k.Time == entryDate {
			start = i
			break
		}
	}
	if start < 0 {
		return PaperExitResult{}
	}
	var pivotLows []float64
	if p.StructStop {
		pivotLows = confirmedPivotLows(klines)
	}
	peak := cost
	for hold := 1; start+hold < len(klines); hold++ {
		k := klines[start+hold]
		if k.High > peak {
			peak = k.High
		}
		stopLine, kind := p.RiskStopLine(cost, peak)
		reason := map[string]string{"硬止损": "stop_loss", "保本": "breakeven", "移动止损": "trail"}[kind]
		if k.Low > 0 && k.Low <= stopLine {
			return paperExitAt(cost, stopLine, k.Time, reason, hold)
		}
		// 结构止损:收盘确认跌破前低(收盘价口径,不吃影线)
		if p.StructStop && pivotLows[start+hold] > 0 && k.Close < pivotLows[start+hold] {
			return paperExitAt(cost, k.Close, k.Time, "struct_stop", hold)
		}
		// 成文结构性止损(均为收盘口径,不吃影线)
		if p.MA5Exit && k.MA5 > 0 && k.Close < k.MA5 {
			return paperExitAt(cost, k.Close, k.Time, "ma5", hold)
		}
		if p.EntryLowStop && klines[start].Low > 0 && k.Close < klines[start].Low {
			return paperExitAt(cost, k.Close, k.Time, "signal_low", hold)
		}
		if p.PrevLowStop && start > 0 && klines[start-1].Low > 0 && k.Close < klines[start-1].Low {
			return paperExitAt(cost, k.Close, k.Time, "prev_low", hold)
		}
		if tp := cost * (1 + p.TPPct/100); k.High >= tp {
			return paperExitAt(cost, tp, k.Time, "take_profit", hold)
		}
		if p.TimeStopDays > 0 && hold >= p.TimeStopDays && (k.Close-cost)/cost*100 < p.TimeStopGain {
			return paperExitAt(cost, k.Close, k.Time, "time_stop", hold)
		}
	}
	return PaperExitResult{}
}

// RiskStep 账户回测里对单个持仓按风控线逐日判定离场（供策略账户套用统一风控）。
func (p RiskProfile) RiskStep(pos *openPos, r btRow, hold int) (reason string, price float64) {
	if pos.peakHigh < pos.entryPrice {
		pos.peakHigh = pos.entryPrice
	}
	if r.high > pos.peakHigh {
		pos.peakHigh = r.high
	}
	line, kind := p.RiskStopLine(pos.entryPrice, pos.peakHigh)
	if r.low > 0 && r.low <= line {
		return map[string]string{"硬止损": "stop_loss", "保本": "breakeven", "移动止损": "trail"}[kind], line
	}
	if tp := pos.entryPrice * (1 + p.TPPct/100); r.high >= tp {
		return "take_profit", tp
	}
	if p.TimeStopDays > 0 && hold >= p.TimeStopDays && (r.close-pos.entryPrice)/pos.entryPrice*100 < p.TimeStopGain {
		return "time_stop", r.close
	}
	return "", 0
}

func paperExitAt(cost, price float64, date, reason string, hold int) PaperExitResult {
	return PaperExitResult{Exited: true, ExitDate: date, ExitPrice: round2(price), Reason: reason,
		NetPct: round2(PaperNetReturnPct(cost, price)), HoldDays: hold}
}
