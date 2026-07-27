package main

// 留痕信号日的正确算法(2026-07-26 查出的错标 bug)。
//
// 病症:某条三倍量留痕写着 signal_date=2026-06-11、收11.24/+4.95%,但那根 K 线其实是 06-10 的;
//      06-11 当天该股是跌 4.27%。查下来 scanned_at=2026-06-11 04:06 —— 凌晨四点跑的扫描。
// 根因:留痕的信号日直接取 result.AsOf,而 AsOf = time.Now()(扫描时的墙上时钟)。
//      盘前(凌晨)或非交易日(周末)跑扫描时,能拿到的日K只到上一交易日,
//      日期却被盖成"运行当天",于是留痕、次日复盘、自学习样本、体感练习全部错位一天。
// 全库体检:约 325 条留痕中招(占 13%),扫描时刻集中在 00:09~05:19,外加几条周末跑的。
//
// 正确规则:信号日 = 这批数据实际对应的交易日
//   ① 交易日且已开盘(≥09:30):扫描用的是当天实时行情 → 今天
//   ② 其余(盘前 / 周末 / 节假日):数据只能到库里最新交易日 → 取 MAX(trade_date)

import "time"

func (a *App) effectiveSignalDate() string {
	now := time.Now()
	today := now.Format("2006-01-02")
	if a == nil {
		return today
	}
	if a.marketService != nil && a.marketService.IsTradingDay(now) &&
		now.Hour()*60+now.Minute() >= 9*60+30 {
		return today // 开盘后的实时扫描,数据就是今天的
	}
	if a.historyService != nil {
		if d := a.historyService.LatestTradeDate(); d != "" {
			return d
		}
	}
	return today // 库里一根K线都没有:退化成今天,总比空着强
}
