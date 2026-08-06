package main

// 日K数据新鲜度自检(2026-07-31 建)。
//
// 起因:盘后自动采集被关掉(config.autoCollectDaily=false),stock_daily 从 2026-07-24 起
// **连断 4 个交易日没人发现**,直到用户问"体感练习为什么只能选到 07-23"才暴露。
// 而这份数据是很多东西的地基:体感练习、次日复盘、学习日报、扣费超额基准(全市场等权中位)、
// 各策略历史回放——断了它们全部静默停在那天,界面上不会报错,只是"没有更新的日子可选"。
//
// 静默失效比报错危险得多。所以加这道自检:每交易日两个时点主动比对,落后就推送。
//   09:10 开盘前:此时应有"上一个交易日"的数据 —— 让你在开盘前就知道地基是旧的
//   17:20 采集窗口(16:00-17:00)之后:此时应有"今天"的数据 —— 当天就抓到采集失败
//
// 刻意不自动修:采集是重任务(全市场回补实测 55 分钟),自动触发可能撞上别的任务或反复空跑;
// 告警里写清楚差几天、开关状态,由人决定何时补。

import (
	"fmt"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// dataHealthMaxLagDays 允许落后的交易日数。1 = 落后 1 个交易日就告警(当天采集失败即报)。
const dataHealthMaxLagDays = 1

// lastSettledTradeDate 返回"此刻应该已经有数据的最近交易日":
// 收盘(15:00)之后的交易日算今天,否则算上一个交易日。
func (a *App) lastSettledTradeDate(now time.Time) string {
	if a == nil || a.marketService == nil {
		return ""
	}
	d := now
	if !(a.marketService.IsTradingDay(d) && d.Hour()*60+d.Minute() >= 15*60) {
		d = d.AddDate(0, 0, -1)
	}
	for i := 0; i < 15; i++ { // 往回找最近的交易日(跨长假最多 15 天)
		if a.marketService.IsTradingDay(d) {
			return d.Format("2006-01-02")
		}
		d = d.AddDate(0, 0, -1)
	}
	return ""
}

// tradingDaysBetween 数 from(不含)到 to(含)之间有几个交易日。
func (a *App) tradingDaysBetween(from, to string) int {
	if a == nil || a.marketService == nil || from == "" || to == "" {
		return 0
	}
	f, err1 := time.ParseInLocation("2006-01-02", from, time.Local)
	t, err2 := time.ParseInLocation("2006-01-02", to, time.Local)
	if err1 != nil || err2 != nil {
		return 0
	}
	n := 0
	for d := f.AddDate(0, 0, 1); !d.After(t) && n < 60; d = d.AddDate(0, 0, 1) {
		if a.marketService.IsTradingDay(d) {
			n++
		}
	}
	return n
}

// checkDataFreshness 比对日K库最新日期与"应有日期",落后就推送告警。返回落后的交易日数(0=正常)。
func (a *App) checkDataFreshness() int {
	if a == nil || a.historyService == nil || a.marketService == nil {
		return 0
	}
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	want := a.lastSettledTradeDate(now)
	have := a.historyService.LatestTradeDate()
	if want == "" || have == "" || have >= want {
		return 0
	}
	lag := a.tradingDaysBetween(have, want)
	if lag < dataHealthMaxLagDays {
		return 0
	}

	collect := "未知"
	if st := a.GetHistoryAutoCollectStatus(); st.CollectStart != "" {
		if st.Enabled {
			collect = fmt.Sprintf("已开启(%s-%s),上次成功 %s", st.CollectStart, st.CollectEnd, st.LastCollectDate)
		} else {
			collect = "⚠️已关闭 —— 这就是原因"
		}
	}
	msg := fmt.Sprintf(
		"日K库最新 %s,应有 %s(落后 %d 个交易日)\n盘后自动采集:%s\n"+
			"影响:体感练习/次日复盘/学习日报/扣费超额基准 全部停在该日,界面不会报错只是没有新日子\n"+
			"修复:开启自动采集 + 跑一次全市场回补(约 1 小时)",
		have, want, lag, collect)
	log.Warn("数据新鲜度告警:%s", msg)
	if a.pushService != nil {
		// StockCode 借用一个固定串当防重键(PushService 按 股票+类型 24h 去重),
		// 免得每天两次自检把同一条告警反复轰炸。
		a.pushService.Push(models.PushSignal{
			StockCode: "data-health",
			StockName: "⚠️日K数据未更新",
			Type:      models.PushTypeEnvChange,
			Level:     "timeSensitive",
			Message:   msg,
		})
	}
	return lag
}

// CheckDataFreshness 手动触发一次自检(RPC),返回人话结论。
func (a *App) CheckDataFreshness() string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	have := a.historyService.LatestTradeDate()
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	want := a.lastSettledTradeDate(now)
	if lag := a.checkDataFreshness(); lag > 0 {
		return fmt.Sprintf("⚠️日K库落后 %d 个交易日(库内最新 %s,应有 %s),已推送告警", lag, have, want)
	}
	return fmt.Sprintf("✅ 日K数据新鲜(库内最新 %s,应有 %s)", have, want)
}
