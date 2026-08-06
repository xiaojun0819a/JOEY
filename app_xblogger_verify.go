package main

// X 荐股博主 —— 历史回溯验证(模块②)。
//
// 回答一个问题:**这六个人到底行不行。**
//
// 口径与本项目其它回测(二波启动/涨停回踩/超跌起爆)**逐条对齐**,这样数字横向可比:
//   · 基准 = 逐日全市场等权中位,按持有期累加
//   · 成本 = 双边 0.2%
//   · 扣费超额 = 个股收益 − 基准 − 成本
//   · **必出中位数、跑赢率和失败分布** —— 均值会被右尾少数大赢家拉起来,
//     只看均值就会把"十次里九次小亏、一次暴赚"误读成好策略(这是本项目反复栽过的坑)。
//
// 两处专门为"跟单博主"设计的诚实处理:
//
// ① **入场用目标日开盘价。** 博主是前一晚发的票,你能做的最早动作就是次日开盘买。
//    用当日收盘价入场等于白捡了一整天的日内信息,胜率会系统性虚高。
//
// ② **一字涨停单独统计,不混进主口径。** 开盘就封死的票你根本买不进,
//    把它算进收益是自欺;但把它悄悄删掉又会低估博主的眼光(那往往是他最准的一次)。
//    所以主口径只算「可买样本」,另出一行「全样本」把买不进的按开盘价假设成交,两个数都摆出来。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/xblogger"
)

// xVerifyCostPct 双边成本,与既有回测一致(模拟盘实际按 0.35% 记,回测统一 0.2% 以便横向比)。
const xVerifyCostPct = 0.2

var xVerifyHolds = []int{1, 3, 5, 10}

type xVerifyOne struct {
	Blogger string
	Symbol  string
	Name    string
	Date    string  // 目标交易日
	Entry   float64 // 开盘价
	OpenPct float64 // 开盘相对昨收
	Buyable bool    // 开盘不是一字涨停
	Excess  map[int]float64
	HasEx   map[int]bool
}

type xVerifyStat struct {
	N       int
	Median  float64
	Mean    float64
	WinRate float64 // 扣费超额 > 0 的比例
	Down5   float64 // 原始收益跌超 5%
	Down10  float64
	Up10    float64
}

// VerifyXBloggers 回溯验证:把库里所有「明日买入」信号按目标日开盘价建仓,算 T+1/3/5/10 的扣费超额。
// start/end 空则不限。
func (a *App) VerifyXBloggers(start, end string) string {
	var b strings.Builder
	if a == nil || a.historyService == nil {
		return "历史库未就绪"
	}
	db, err := openXblogDB()
	if err != nil {
		return "打不开 xblog.db:" + err.Error()
	}
	t0 := time.Now()

	q := `SELECT blogger, symbol, name, target_date FROM x_signal WHERE kind='buy'`
	args := []any{}
	if start != "" {
		q += ` AND target_date>=?`
		args = append(args, start)
	}
	if end != "" {
		q += ` AND target_date<=?`
		args = append(args, end)
	}
	q += ` ORDER BY target_date`
	rows, err := db.Query(q, args...)
	if err != nil {
		return "查询信号失败:" + err.Error()
	}
	type raw struct{ blogger, symbol, name, date string }
	all := []raw{}
	for rows.Next() {
		var r raw
		if rows.Scan(&r.blogger, &r.symbol, &r.name, &r.date) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	if len(all) == 0 {
		return "库里还没有买入信号 —— 先在那台 Windows 上跑 3-backfill.bat 把历史推文灌进来"
	}

	// 每只票只读一次序列,按日期建索引
	bySymbol := map[string][]raw{}
	for _, r := range all {
		bySymbol[r.symbol] = append(bySymbol[r.symbol], r)
	}
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")

	// 基准缓存:逐日全市场等权中位
	medCache := map[string]float64{}
	dailyMed := func(d string) (float64, bool) {
		if v, ok := medCache[d]; ok {
			return v, v != -999
		}
		if m, good := a.historyService.MarketMedianPctPublic(d); good {
			medCache[d] = m
			return m, true
		}
		medCache[d] = -999
		return 0, false
	}

	out := []xVerifyOne{}
	noData, noBench := 0, 0
	for sym, list := range bySymbol {
		o, _, _, c, _, _, dates, _ := a.historyService.SeriesRecentUntil(sym, today, 320)
		if len(c) < 3 {
			noData += len(list)
			continue
		}
		idxOf := make(map[string]int, len(dates))
		for i, d := range dates {
			idxOf[d] = i
		}
		for _, r := range list {
			i, ok := idxOf[r.date]
			if !ok || i == 0 || o[i] <= 0 || c[i-1] <= 0 {
				noData++
				continue
			}
			one := xVerifyOne{
				Blogger: r.blogger, Symbol: sym, Name: r.name, Date: r.date,
				Entry: o[i], OpenPct: (o[i]/c[i-1] - 1) * 100,
				Excess: map[int]float64{}, HasEx: map[int]bool{},
			}
			// 一字涨停 = 开盘就顶格。用"开盘涨幅≥9.8%"近似(创业板/科创板已在池外)。
			one.Buyable = one.OpenPct < 9.8

			// 基准从**目标日当天**起累加。买在开盘、卖在收盘,持有区间含当日日内,
			// 而当日中位包含隔夜跳空(我们没吃到)——这会略微高估基准、低估我们的超额,
			// 偏保守,可接受。
			acc, okAll := 0.0, true
			d := r.date
			for hd := 0; hd <= 10; hd++ {
				if hd > 0 {
					d = a.historyService.NextTradeDateAfter(d)
					if d == "" {
						okAll = false
					}
				}
				if okAll {
					if m, good := dailyMed(d); good {
						acc += m
					} else {
						okAll = false
					}
				}
				for _, want := range xVerifyHolds {
					if hd != want {
						continue
					}
					if i+want < len(c) && okAll {
						ret := (c[i+want]/o[i] - 1) * 100
						one.Excess[want] = ret - acc - xVerifyCostPct
						one.HasEx[want] = true
					} else if i+want >= len(c) {
						// 还没走完持有期(最近几天的信号),不算不猜
					} else {
						noBench++
					}
				}
			}
			out = append(out, one)
		}
	}

	fmt.Fprintf(&b, "【X 荐股博主 回溯验证】信号 %d 条", len(all))
	if start != "" || end != "" {
		fmt.Fprintf(&b, "(%s ~ %s)", or(start, "不限"), or(end, "不限"))
	}
	fmt.Fprintf(&b, ",可评估 %d 条", len(out))
	if noData > 0 {
		fmt.Fprintf(&b, ",取不到行情 %d 条", noData)
	}
	b.WriteString("\n")
	b.WriteString("入场=目标日**开盘价**(博主前一晚发,最早只能次日开盘买);出场=T+N 收盘价\n")
	fmt.Fprintf(&b, "扣费超额 = 个股收益 − 全市场等权中位累加 − %.1f%% 双边成本\n\n", xVerifyCostPct)

	byBlogger := map[string][]xVerifyOne{}
	for _, r := range out {
		byBlogger[r.Blogger] = append(byBlogger[r.Blogger], r)
	}
	keys := make([]string, 0, len(byBlogger))
	for k := range byBlogger {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		list := byBlogger[k]
		disp := k
		if cfg, ok := xblogger.ConfigByKey(k); ok {
			disp = cfg.Display
		}
		buyable := []xVerifyOne{}
		for _, r := range list {
			if r.Buyable {
				buyable = append(buyable, r)
			}
		}
		fmt.Fprintf(&b, "◆ %s(%s)\n", disp, k)
		fmt.Fprintf(&b, "  推荐 %d 只,其中开盘一字涨停买不进 %d 只(%.0f%%)\n",
			len(list), len(list)-len(buyable), pct(len(list)-len(buyable), len(list)))
		if len(buyable) == 0 {
			b.WriteString("  没有可买样本\n\n")
			continue
		}
		b.WriteString("  持有    样本    中位超额    均值超额    跑赢率    跌超5%   跌超10%   涨超10%\n")
		for _, hd := range xVerifyHolds {
			s := statOf(buyable, hd)
			if s.N == 0 {
				continue
			}
			fmt.Fprintf(&b, "  T+%-3d %5d   %+7.2f%%   %+7.2f%%   %5.0f%%   %5.0f%%   %5.0f%%   %5.0f%%\n",
				hd, s.N, s.Median, s.Mean, s.WinRate, s.Down5, s.Down10, s.Up10)
		}
		// 全样本对照:把买不进的也按开盘价假设成交。两个数差得多,说明他的价值集中在你吃不到的那部分。
		if len(buyable) < len(list) {
			sa, sb := statOf(list, 5), statOf(buyable, 5)
			fmt.Fprintf(&b, "  [T+5 对照] 全样本(含买不进,假设开盘成交)中位 %+.2f%% vs 可买样本 %+.2f%%\n",
				sa.Median, sb.Median)
		}
		b.WriteString("\n")
	}

	// 汇总一行,便于横向排队
	b.WriteString("── 横向对比(T+5 可买样本)──\n")
	type rank struct {
		disp string
		s    xVerifyStat
	}
	rk := []rank{}
	for _, k := range keys {
		buyable := []xVerifyOne{}
		for _, r := range byBlogger[k] {
			if r.Buyable {
				buyable = append(buyable, r)
			}
		}
		disp := k
		if cfg, ok := xblogger.ConfigByKey(k); ok {
			disp = cfg.Display
		}
		if s := statOf(buyable, 5); s.N > 0 {
			rk = append(rk, rank{disp, s})
		}
	}
	sort.Slice(rk, func(i, j int) bool { return rk[i].s.Median > rk[j].s.Median })
	for _, r := range rk {
		fmt.Fprintf(&b, "  %-14s n=%-4d 中位 %+6.2f%%  跑赢率 %3.0f%%\n", r.disp, r.s.N, r.s.Median, r.s.WinRate)
	}

	b.WriteString("\n⚠️怎么读:\n")
	b.WriteString("  · **看中位数和跑赢率,别看均值。** 均值被少数暴涨股拉高,跑不赢市场的策略也能有正均值。\n")
	b.WriteString("  · 跑赢率长期不过半 = 没有 alpha,哪怕均值是正的。\n")
	b.WriteString("  · 样本 <30 条的不要下结论,尤其别因为某人前几条准就开他的自动建仓。\n")
	fmt.Fprintf(&b, "\n耗时 %.0fs\n", time.Since(t0).Seconds())
	return b.String()
}

func statOf(list []xVerifyOne, hd int) xVerifyStat {
	vals := []float64{}
	rets := []float64{}
	for _, r := range list {
		if !r.HasEx[hd] {
			continue
		}
		vals = append(vals, r.Excess[hd])
		rets = append(rets, r.Excess[hd]+xVerifyCostPct) // 近似原始超额,用于失败分布
	}
	s := xVerifyStat{N: len(vals)}
	if s.N == 0 {
		return s
	}
	sum, win, d5, d10, u10 := 0.0, 0, 0, 0, 0
	for i, v := range vals {
		sum += v
		if v > 0 {
			win++
		}
		switch {
		case rets[i] <= -10:
			d10++
			d5++
		case rets[i] <= -5:
			d5++
		}
		if rets[i] >= 10 {
			u10++
		}
	}
	s.Mean = sum / float64(s.N)
	sort.Float64s(vals)
	if s.N%2 == 1 {
		s.Median = vals[s.N/2]
	} else {
		s.Median = (vals[s.N/2-1] + vals[s.N/2]) / 2
	}
	s.WinRate = pct(win, s.N)
	s.Down5, s.Down10, s.Up10 = pct(d5, s.N), pct(d10, s.N), pct(u10, s.N)
	return s
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return math.Round(float64(a)/float64(b)*1000) / 10
}

func or(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}
