package main

// 选股体感练习 RPC(2026-07-25)。用户要的流程:
//   ① 只看某日 14:50 真实选出的那批票(信号日证据,不给次日结果)→ ② 自己盲选一只
//   → ③ 用次日真实分时逐笔回放 → ④ 自己按下卖出 → ⑤ 结算并与"持到收盘/当日最高/开盘就卖"对照。
// 本文件只负责 ①:发信号日证据 + 告诉前端次日是哪天。分时磁带走既有 GetStockIntraday。

import (
	"fmt"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// GetDrillSession 取某策略某信号日的练习题面。signalDate 留空则用最近一个可练习的日子。
func (a *App) GetDrillSession(strategyID, signalDate string) models.DrillSession {
	res := models.DrillSession{
		StrategyID:   strategyID,
		StrategyName: strategyReviewName(strategyID),
		SignalDate:   signalDate,
		Picks:        []models.DrillPick{},
	}
	if a == nil || a.historyService == nil {
		res.Warning = "历史服务未就绪"
		return res
	}
	if strategyID == "" {
		res.Warning = "缺少策略标识"
		return res
	}
	pairs := a.historyService.DrillDatesFor(strategyID, 200)
	// 只留"次日拿得到分时磁带"的日子,否则点进去必然撞墙。两个磁带源都认:
	//   ① minute_ticks:自采集 15 秒级,2026-07-06 起全市场(按 trade_date 聚合很快,PK 最左就是它);
	//   ② drill_tape_days:体感练习按需回补(通达信一分钟线)后的登记表——
	//      ⚠️不能直接问 minute_history "有哪些日子",它 PK 是 stock_code 打头,按日聚合=扫全表。
	res.AvailableDates = make([]string, 0, len(pairs))
	if a.intradayService != nil {
		tLo, tHi := a.intradayService.MinuteTickDateRange()
		marked := a.intradayService.DrillTapeDays()
		for _, p := range pairs {
			live := tLo != "" && p.NextDate >= tLo && p.NextDate <= tHi
			if live || marked[p.NextDate] {
				res.AvailableDates = append(res.AvailableDates, p.Date)
			}
		}
	} else {
		for _, p := range pairs {
			res.AvailableDates = append(res.AvailableDates, p.Date)
		}
	}
	if len(res.AvailableDates) == 0 {
		res.Warning = "该策略还没有可练习的日子(需要:当日有真实选股留痕,且次日有分时存档——自采集从 2026-07-06 起)"
		return res
	}
	if res.SignalDate == "" {
		res.SignalDate = res.AvailableDates[0]
	}
	picks, err := a.historyService.LoadDrillPicks(strategyID, res.SignalDate)
	if err != nil {
		res.Warning = "读取留痕失败:" + err.Error()
		return res
	}
	res.Picks = picks
	res.NextDate = a.historyService.NextTradeDateAfter(res.SignalDate)
	if len(picks) == 0 {
		res.Warning = fmt.Sprintf("%s 无该策略的真实选股留痕(手工加进模拟仓的倒灌行已排除,不作练习题)", res.SignalDate)
		return res
	}
	if res.NextDate == "" {
		res.Warning = fmt.Sprintf("%s 之后还没有交易日行情——练习要用次日分时,请选更早的日子", res.SignalDate)
	}
	return res
}

// StartDrillTapeBackfill 按需补齐体感练习要用的历史分时磁带。
// 只补「留痕票的次日」这一小撮(2026-07-06 之前全部留痕也才千余股日),不做全市场爆破。
// 走既有 BackfillMinuteHistory:通达信源、40ms 节流、断点续跑(已有的股日自动跳过)。
func (a *App) StartDrillTapeBackfill(start, end string) string {
	if a.intradayService == nil || a.historyService == nil {
		return "服务未就绪"
	}
	if start == "" {
		start = "2026-01-01"
	}
	if end == "" {
		end = time.Now().Format("2006-01-02")
	}
	plan := a.historyService.DrillTapePlan(start, end)
	total := 0
	for _, d := range plan {
		total += len(d.Codes)
	}
	if total == 0 {
		return fmt.Sprintf("%s~%s 区间内没有需要补的留痕票", start, end)
	}
	go func() {
		if err := a.intradayService.BackfillMinuteHistory(plan); err != nil {
			log.Warn("体感练习磁带回补: %v", err)
			return
		}
		// 登记这批日子已补:之后练习日期过滤查这张小表,绝不去 minute_history 按日聚合
		a.intradayService.MarkDrillTapeDays(plan)
		log.Info("体感练习磁带回补完成并登记 %d 个交易日", len(plan))
	}()
	return fmt.Sprintf("已启动:%d 个交易日、共 %d 股日(已有的会跳过,约 %.0f 分钟),进度查 GetHistoryMinuteBackfillStatus",
		len(plan), total, float64(total)*0.09/60)
}
