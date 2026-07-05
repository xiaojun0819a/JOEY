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
}

// 稳健参数(用户选定)
var riskShortTerm = RiskProfile{Name: "短线", HardStopPct: -8, BreakevenAtPct: 8, TrailArmPct: 10, TrailDropPct: 6, TPPct: 25, TimeStopDays: 10, TimeStopGain: 3}
var riskValue = RiskProfile{Name: "价值", HardStopPct: -15, BreakevenAtPct: 12, TrailArmPct: 20, TrailDropPct: 12, TPPct: 40, TimeStopDays: 0}

// RiskProfileForSource 按来源选风控口径：基本面价值/景气走"价值"宽线，其余短线技术走"短线"紧线。
func RiskProfileForSource(source string) RiskProfile {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "fund-value", "fund-boom":
		return riskValue
	default:
		return riskShortTerm
	}
}

// RiskStopLine 返回某仓位"当前应执行的止损价"(用于盘中实时判定与界面展示)。
// peakHigh=入场以来最高价(无则传成本)。
func (p RiskProfile) RiskStopLine(cost, peakHigh float64) (line float64, kind string) {
	line = cost * (1 + p.HardStopPct/100)
	kind = "硬止损"
	if peakHigh >= cost*(1+p.BreakevenAtPct/100) && cost > line {
		line, kind = cost, "保本"
	}
	if peakHigh >= cost*(1+p.TrailArmPct/100) {
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
