package main

// 「二波启动」形态回测(2026-07-31 建)。
//
// 来源:用户拿来一张教学图(易直涨,赤脚底/烟斗底/水下多方炮)。三个"形态"其实是**同一个骨架**:
//     第一波上涨(资金进场证据) → 洗盘回调 → 缩量见底 → 放量突破平台
// 三者只在"洗盘段长什么样"上有别(V型/U型/大阳夹小K),买点、共同规律完全一致。
// 所以这里不去分辨形状(那部分主观且不可证伪),只把**四段结构里能机械判定的五条**写死:
//     ① 有过第一波上涨   ② 回调幅度在区间内   ③ 洗盘缩量
//     ④ 未跌回第一波起点且站上 MA20   ⑤ 放量突破洗盘平台高点
// 图里另外三条(热点/龙头/情绪周期)本轮不做:前两条要板块数据、第三条没有客观定义。
//
// ⚠️为什么值得测:那张图的"实盘案例"和"符合标准股票"全是事后挑的,一个失败案例都没有,
// 也不给基础胜率。而它列的票里有 5 只(立新能源/长城军工/昭衍新药/赤天化/中电鑫龙)
// 正是 7 月被反复推荐的热门股——股票涨完之后总能在K线上套出一个形态。
// 本回测的核心产出因此不是"胜率多高",而是**符合形态却失败的比例**——那正是图上缺的一半。
//
// 口径与既有回测一致:扣 0.2% 成本、对全市场等权中位算超额、分年度看 regime 依赖。

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// swParams 形态参数。全部显式化,便于做敏感性——写死的阈值最容易变成过拟合。
type swParams struct {
	FirstWaveMin float64 // 第一波最小涨幅%
	FirstWaveMax float64 // 第一波最大涨幅%(位置不能太高)
	DrawdownMin  float64 // 回调最小幅度%
	DrawdownMax  float64 // 回调最大幅度%
	ShrinkMax    float64 // 洗盘段均量/上涨段均量 上限(缩量)
	VolMult      float64 // 突破日放量倍数(对前5日均量)
	MinWashDays  int     // 洗盘段最少天数
	MaxWashDays  int     // 洗盘段最多天数
}

func defaultSWParams() swParams {
	return swParams{
		FirstWaveMin: 20, FirstWaveMax: 80,
		DrawdownMin: 10, DrawdownMax: 25, // 图里明写"调整10%-25%最佳"
		ShrinkMax: 0.7, VolMult: 2.0,
		MinWashDays: 5, MaxWashDays: 40,
	}
}

type swSignal struct {
	Date, Code string
	Entry      float64 // 突破日收盘价=入场价
	FirstWave  float64 // 第一波涨幅%
	Drawdown   float64 // 回调幅度%
	Shrink     float64 // 缩量比
	VolX       float64 // 突破放量倍数
	WashDays   float64 // 洗盘天数
	Fwd        map[int]float64
	Bench      map[int]float64
	HasBench   map[int]bool
}

// detectSecondWave 在以 i 为突破日的序列上判定形态。c/h/l/v 为升序序列。
// 返回命中与各项特征值。判定顺序即漏斗顺序,便于定位哪一条卡掉最多。
func detectSecondWave(c, h, l, v []float64, i int, p swParams) (swSignal, bool) {
	var out swSignal
	if i < 100 || i >= len(c) {
		return out, false
	}
	// ① 找第一波:近 90 日内的最高收盘作为波峰
	peak := i - 1
	for j := i - 90; j < i; j++ {
		if j >= 0 && c[j] > c[peak] {
			peak = j
		}
	}
	washDays := i - peak - 1
	if washDays < p.MinWashDays || washDays > p.MaxWashDays {
		return out, false
	}
	// 波峰之前 90 日内的最低收盘 = 第一波起点
	base := peak
	for j := peak - 90; j < peak; j++ {
		if j >= 0 && c[j] < c[base] {
			base = j
		}
	}
	if base >= peak || c[base] <= 0 {
		return out, false
	}
	firstWave := (c[peak]/c[base] - 1) * 100
	if firstWave < p.FirstWaveMin || firstWave > p.FirstWaveMax {
		return out, false
	}
	// ② 洗盘段最低收盘 → 回调幅度
	trough := peak + 1
	for j := peak + 1; j < i; j++ {
		if c[j] < c[trough] {
			trough = j
		}
	}
	dd := (c[peak] - c[trough]) / c[peak] * 100
	if dd < p.DrawdownMin || dd > p.DrawdownMax {
		return out, false
	}
	// ③ 未跌回第一波起点(跌回去就不是洗盘,是趋势结束)
	if c[trough] <= c[base] {
		return out, false
	}
	// ④ 缩量:洗盘段均量 / 上涨段均量
	// ⚠️两个均值函数必须分开:曾把 MA20 也用 avgVol 算,结果比较的是"收盘价 vs 成交量均值"
	// (十几块 vs 几百上千),条件永远不成立、规则被这一行挡死、回测返回零命中——
	// 而零命中极易被误读成"这个形态不存在"。单元测试才逼出来。
	mean := func(arr []float64, from, to int) float64 {
		if from < 0 || to <= from || to > len(arr) {
			return 0
		}
		s := 0.0
		for j := from; j < to; j++ {
			s += arr[j]
		}
		return s / float64(to-from)
	}
	avg := func(from, to int) float64 { return mean(v, from, to) } // 成交量均值
	upVol, washVol := avg(base, peak+1), avg(peak+1, i)
	if upVol <= 0 || washVol <= 0 {
		return out, false
	}
	shrink := washVol / upVol
	if shrink > p.ShrinkMax {
		return out, false
	}
	// ⑤ 突破日:放量 + 阳线 + 收盘突破洗盘段最高收盘 + 站上 MA20
	recent := avg(i-5, i)
	if recent <= 0 || v[i] < recent*p.VolMult {
		return out, false
	}
	if c[i] <= c[i-1] { // 必须是上涨日
		return out, false
	}
	// 「突破平台」= 突破**回调见底后的横盘区**高点,不是整个洗盘段的最高点。
	// ⚠️最初写成后者,结果零命中:洗盘段是从波峰往下走的,它的最高收盘就在波峰附近,
	// 等于要求突破日一根收复整个回调(15% 的回调要单日涨 15%),现实中不可能出现。
	// 取最近 20 日(不足则取整个洗盘段)的最高收盘作为平台高——这才是图里画的那条平台线。
	plat := 20
	if washDays < plat {
		plat = washDays
	}
	washHigh := c[i-plat]
	for j := i - plat; j < i; j++ {
		if c[j] > washHigh {
			washHigh = c[j]
		}
	}
	if c[i] <= washHigh {
		return out, false
	}
	ma20 := mean(c, i-19, i+1) // 收盘价的 20 日均线
	if ma20 <= 0 || c[i] < ma20 {
		return out, false
	}
	out.Entry = c[i]
	out.FirstWave, out.Drawdown, out.Shrink, out.VolX = firstWave, dd, shrink, v[i]/recent
	out.WashDays = float64(washDays)
	return out, true
}

// swScanStat 一次扫描的规模统计,供输出交代覆盖面。
type swScanStat struct{ Pool, LoadFail, Scanned int }

// backtestSWSignals 扫全市场取信号并算好基准。回测与样本外验证共用这一份,
// 保证两边的口径逐字节一致——两套实现最容易在细节上悄悄分叉。
func (a *App) backtestSWSignals(start, end string) ([]swSignal, swScanStat) {
	return a.backtestSWSignalsWith(start, end, defaultSWParams())
}

// backtestSWSignalsWith 同上,但可指定参数——敏感性扫描用最宽松的参数扫一次,
// 之后所有变体在内存里过滤,避免每个变体重扫一次全市场。
func (a *App) backtestSWSignalsWith(start, end string, p swParams) ([]swSignal, swScanStat) {
	var stat swScanStat
	if a == nil || a.historyService == nil {
		return nil, stat
	}
	codes := a.historyService.AllUniverseCodes()
	if len(codes) == 0 {
		return nil, stat
	}
	span := len(a.historyService.TradeDatesSince(start, end))
	if span == 0 {
		return nil, stat
	}
	stat.Pool = len(codes)
	need := span + 200 // 形态要看 190 根历史

	holds := []int{1, 3, 5, 10}
	var (
		mu       sync.Mutex
		signals  []swSignal
		scanned  int
		loadFail int
	)
	sem := make(chan struct{}, 6) // NAS 4核,并发别开大
	var wg sync.WaitGroup
	for _, code := range codes {
		if !isMainBoard10cm(code, "") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(code string) {
			defer wg.Done()
			defer func() { <-sem }()
			_, hh, ll, cc, vv, _, dates, name := a.historyService.SeriesRecentUntil(code, end, need)
			if len(cc) < 210 || strings.Contains(name, "ST") || strings.Contains(name, "退") {
				mu.Lock()
				loadFail++
				mu.Unlock()
				return
			}
			local := []swSignal{}
			cnt := 0
			for i := 100; i < len(cc); i++ {
				day := dates[i]
				if day < start || day > end {
					continue
				}
				cnt++
				sig, ok := detectSecondWave(cc, hh, ll, vv, i, p)
				if !ok {
					continue
				}
				sig.Date, sig.Code = day, code
				sig.Fwd = map[int]float64{}
				for _, hd := range holds { // 前向收益就在同一条序列里,不再查库
					if i+hd < len(cc) && cc[i] > 0 {
						sig.Fwd[hd] = (cc[i+hd]/cc[i] - 1) * 100
					}
				}
				local = append(local, sig)
			}
			mu.Lock()
			scanned += cnt
			signals = append(signals, local...)
			mu.Unlock()
		}(code)
	}
	wg.Wait()
	sort.Slice(signals, func(i, j int) bool { return signals[i].Date < signals[j].Date })

	// 基准:逐日全市场等权中位,按持有期累加(近似——严格口径应是"组合累计收益的中位数",
	// 逐日中位相加会略微低估波动,但足以判断有没有跑赢市场漂移。已在输出里标明是近似)。
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
	for i := range signals {
		signals[i].Bench = map[int]float64{}
		signals[i].HasBench = map[int]bool{}
		d := signals[i].Date
		acc, okAll := 0.0, true
		for hd := 1; hd <= 10; hd++ {
			d = a.historyService.NextTradeDateAfter(d)
			if d == "" {
				okAll = false
			}
			if okAll {
				if m, good := dailyMed(d); good {
					acc += m
				} else {
					okAll = false
				}
			}
			for _, want := range holds {
				if hd == want {
					signals[i].Bench[want], signals[i].HasBench[want] = acc, okAll
				}
			}
		}
	}

	stat.LoadFail, stat.Scanned = loadFail, scanned
	return signals, stat
}

// BacktestSecondWave 二波启动形态历史回测。start/end 为空则用默认区间。
func (a *App) BacktestSecondWave(start, end string) string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	if start == "" {
		start = "2024-07-01"
	}
	if end == "" {
		end = a.historyService.LatestTradeDate()
	}
	p := defaultSWParams()
	t0 := time.Now()
	holds := []int{1, 3, 5, 10}
	signals, stat := a.backtestSWSignals(start, end)

	var b strings.Builder
	fmt.Fprintf(&b, "【二波启动形态 历史回测】%s ~ %s\n", start, end)
	fmt.Fprintf(&b, "规则:第一波+%.0f~%.0f%% → 回调%.0f~%.0f%% → 洗盘%d~%d日且缩量≤%.1f → 放量%.1f倍突破平台且站上MA20\n",
		p.FirstWaveMin, p.FirstWaveMax, p.DrawdownMin, p.DrawdownMax, p.MinWashDays, p.MaxWashDays, p.ShrinkMax, p.VolMult)
	fmt.Fprintf(&b, "股票池 %d(主板10cm、剔ST/退),读取失败 %d,评估 %d 股日,**命中 %d 个信号**,耗时 %.0fs\n\n",
		stat.Pool, stat.LoadFail, stat.Scanned, len(signals), time.Since(t0).Seconds())
	if len(signals) == 0 {
		b.WriteString("零命中——条件过严或该形态在此区间不成立。\n")
		return b.String()
	}

	fmt.Fprintf(&b, "%-6s %6s %8s %8s %8s %8s %8s\n", "持有", "样本", "均值", "中位", "胜率", "扣费超额", "跑赢率")
	for _, hd := range holds {
		var rets, exs []float64
		for _, s := range signals {
			r, ok := s.Fwd[hd]
			if !ok {
				continue
			}
			rets = append(rets, r)
			if s.HasBench[hd] {
				exs = append(exs, r-s.Bench[hd]-0.2) // 扣 0.2% 双边成本
			}
		}
		if len(rets) == 0 {
			continue
		}
		fmt.Fprintf(&b, "T+%-4d %6d %+7.2f%% %+7.2f%% %7.0f%% %+7.2f%% %7.0f%%\n",
			hd, len(rets), swMean(rets), swMedian(rets), swWinRate(rets),
			swMean(exs), swWinRate(exs))
	}

	// ★ 这张图缺的那一半:失败分布
	b.WriteString("\n【失败分布 —— 教学图里没有的那一半】\n")
	for _, hd := range holds {
		var rets []float64
		for _, s := range signals {
			if r, ok := s.Fwd[hd]; ok {
				rets = append(rets, r)
			}
		}
		if len(rets) == 0 {
			continue
		}
		n := float64(len(rets))
		var d5, d10, u10 int
		for _, r := range rets {
			if r <= -5 {
				d5++
			}
			if r <= -10 {
				d10++
			}
			if r >= 10 {
				u10++
			}
		}
		fmt.Fprintf(&b, "  T+%-3d 跌超5%%:%4.0f%%   跌超10%%:%4.0f%%   涨超10%%:%4.0f%%\n",
			hd, float64(d5)/n*100, float64(d10)/n*100, float64(u10)/n*100)
	}

	// 分年度:看是不是只在某个 regime 里成立
	byYear := map[string][]float64{}
	for _, s := range signals {
		if s.HasBench[5] {
			if r, ok := s.Fwd[5]; ok {
				byYear[s.Date[:4]] = append(byYear[s.Date[:4]], r-s.Bench[5]-0.2)
			}
		}
	}
	if len(byYear) > 0 {
		b.WriteString("\n【分年度 T+5 扣费超额 —— 看是不是只在某种行情里成立】\n")
		years := make([]string, 0, len(byYear))
		for y := range byYear {
			years = append(years, y)
		}
		sort.Strings(years)
		for _, y := range years {
			v := byYear[y]
			fmt.Fprintf(&b, "  %s  n=%-5d 超额均值%+.2f%%  跑赢率%.0f%%\n", y, len(v), swMean(v), swWinRate(v))
		}
	}
	// ★ 单因子分桶:先看每个特征本身有没有分离力,有了再谈组合评分。
	// 直接手搓一个加权评分公式是过拟合的标准做法——433 个样本能被任何公式"解释"得很好。
	//
	// 两个指标分开看,因为这是右偏尾生意:
	//   中位超额 = 这个因子能不能改善**普通那一单**
	//   涨超10%率 = 这个因子能不能预测**少数大赢家**
	// 两者未必同向:能筛出大赢家的因子可能同时放大亏损,那属于"加杠杆"不是"提高胜率"。
	b.WriteString("\n【单因子分桶(按四分位)—— T+5 扣费超额中位 / 涨超10%率】\n")
	b.WriteString("  判读门槛:两端桶样本各≥50 且中位差≥1.0pp 才算有分离力,否则按噪声看待。\n")
	for _, f := range []struct {
		name string
		get  func(swSignal) float64
	}{
		{"第一波涨幅%", func(s swSignal) float64 { return s.FirstWave }},
		{"回调幅度%", func(s swSignal) float64 { return s.Drawdown }},
		{"缩量比(越小越缩)", func(s swSignal) float64 { return s.Shrink }},
		{"突破放量倍数", func(s swSignal) float64 { return s.VolX }},
		{"洗盘天数", func(s swSignal) float64 { return s.WashDays }},
	} {
		type row struct{ key, ex, fwd float64 }
		var rows []row
		for _, s := range signals {
			r, ok := s.Fwd[5]
			if !ok || !s.HasBench[5] {
				continue
			}
			rows = append(rows, row{f.get(s), r - s.Bench[5] - 0.2, r})
		}
		if len(rows) < 40 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
		q := len(rows) / 4
		fmt.Fprintf(&b, "\n  ◆ %s\n", f.name)
		var firstMed, lastMed float64
		for k := 0; k < 4; k++ {
			lo, hi := k*q, (k+1)*q
			if k == 3 {
				hi = len(rows)
			}
			seg := rows[lo:hi]
			exs := make([]float64, len(seg))
			big := 0
			for j, r := range seg {
				exs[j] = r.ex
				if r.fwd >= 10 {
					big++
				}
			}
			med := swMedian(exs)
			if k == 0 {
				firstMed = med
			}
			if k == 3 {
				lastMed = med
			}
			fmt.Fprintf(&b, "    Q%d [%6.2f~%6.2f] n=%-4d 中位超额%+6.2f%%  涨超10%%:%3.0f%%\n",
				k+1, seg[0].key, seg[len(seg)-1].key, len(seg), med, float64(big)/float64(len(seg))*100)
		}
		diff := lastMed - firstMed
		verdict := "无分离力(按噪声看待)"
		if q >= 50 && (diff >= 1.0 || diff <= -1.0) {
			verdict = fmt.Sprintf("**有分离力**:Q4−Q1 = %+.2fpp", diff)
		}
		fmt.Fprintf(&b, "    → %s\n", verdict)
	}

	b.WriteString("\n注:基准=逐日全市场等权中位按持有期累加(近似口径);成本按双边 0.2% 扣。\n")
	b.WriteString("注:分桶只做单因子观察,不构成评分公式——多因子组合需另跑样本外验证,否则就是过拟合。\n")
	return b.String()
}

func swMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func swMedian(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64{}, v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func swWinRate(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	w := 0
	for _, x := range v {
		if x > 0 {
			w++
		}
	}
	return float64(w) / float64(len(v)) * 100
}

// ===== 样本外验证(2026-07-31)=====
//
// 全样本分桶发现:五个因子里只有「回调幅度」有分离力(Q4−Q1=+1.53pp,且四桶单调、
// 中位超额与涨超10%率同向)。但那是**在同一批数据里挑出来的最好一档**——四分位切法
// 本身就保证会有一个"最好的桶",分不清是真信号还是我在 433 个点上找出来的花样。
//
// 所以做这一步:**切分点只用样本内数据算,原封不动套到样本外**。
//   样本内 2024-07 ~ 2025-12:算出"深回调"的阈值(样本内回调幅度的中位数)
//   样本外 2026-01 ~ 至今  :用同一个阈值分组,看深回调是否仍然占优
// 方向还在 → 大概率是真的;方向没了或反转 → 就是过拟合,到此为止。
//
// ⚠️阈值必须来自样本内。若用全样本的四分位边界(19.79%),样本外数据参与了定阈值,
// 那就是泄题,验证结果一文不值。

// swSplitStats 一组信号的统计。
type swSplitStats struct {
	N       int
	MedEx   float64 // 中位扣费超额
	MeanEx  float64
	WinRate float64 // 跑赢率
	BigRate float64 // 涨超10%比例
}

func swStatsOf(sigs []swSignal, hold int) swSplitStats {
	var exs, rets []float64
	for _, s := range sigs {
		r, ok := s.Fwd[hold]
		if !ok || !s.HasBench[hold] {
			continue
		}
		exs = append(exs, r-s.Bench[hold]-0.2)
		rets = append(rets, r)
	}
	if len(exs) == 0 {
		return swSplitStats{}
	}
	big := 0
	for _, r := range rets {
		if r >= 10 {
			big++
		}
	}
	return swSplitStats{
		N: len(exs), MedEx: swMedian(exs), MeanEx: swMean(exs),
		WinRate: swWinRate(exs), BigRate: float64(big) / float64(len(rets)) * 100,
	}
}

// ValidateSecondWaveOOS 回调深度这个发现的样本外验证。
// split 为空则默认 2026-01-01(样本内 1.5 年、样本外 7 个月)。
func (a *App) ValidateSecondWaveOOS(start, split, end string) string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	if start == "" {
		start = "2024-07-01"
	}
	if split == "" {
		split = "2026-01-01"
	}
	if end == "" {
		end = a.historyService.LatestTradeDate()
	}
	// 复用回测的取信号逻辑:直接跑一次全区间,再按日期切
	full, _ := a.backtestSWSignals(start, end)
	if len(full) == 0 {
		return "区间内零信号,无法验证"
	}
	var in, out []swSignal
	for _, s := range full {
		if s.Date < split {
			in = append(in, s)
		} else {
			out = append(out, s)
		}
	}
	if len(in) < 60 || len(out) < 40 {
		return fmt.Sprintf("样本不足:样本内 %d / 样本外 %d(要求 ≥60 / ≥40),换个切分点或拉长区间", len(in), len(out))
	}

	// ★ 阈值只用样本内算:样本内回调幅度的中位数
	dds := make([]float64, 0, len(in))
	for _, s := range in {
		dds = append(dds, s.Drawdown)
	}
	thr := swMedian(dds)

	var b strings.Builder
	fmt.Fprintf(&b, "【二波启动·回调深度发现 · 样本外验证】\n")
	fmt.Fprintf(&b, "样本内 %s ~ %s(n=%d) → 算出阈值\n", start, split, len(in))
	fmt.Fprintf(&b, "样本外 %s ~ %s(n=%d) → 原封不动套用\n", split, end, len(out))
	fmt.Fprintf(&b, "**阈值(样本内回调幅度中位数)= %.2f%%**,以此分「深回调 / 浅回调」两组\n\n", thr)

	for _, hold := range []int{3, 5, 10} {
		fmt.Fprintf(&b, "── 持有 T+%d ──\n", hold)
		fmt.Fprintf(&b, "  %-8s %-8s %5s %10s %10s %8s %9s\n", "区间", "组", "n", "中位超额", "均值超额", "跑赢率", "涨超10%")
		var inDiff, outDiff float64
		for _, seg := range []struct {
			label string
			sigs  []swSignal
		}{{"样本内", in}, {"样本外", out}} {
			var deep, shallow []swSignal
			for _, s := range seg.sigs {
				if s.Drawdown >= thr {
					deep = append(deep, s)
				} else {
					shallow = append(shallow, s)
				}
			}
			ds, ss := swStatsOf(deep, hold), swStatsOf(shallow, hold)
			fmt.Fprintf(&b, "  %-8s %-8s %5d %9.2f%% %9.2f%% %7.0f%% %8.0f%%\n",
				seg.label, "深回调", ds.N, ds.MedEx, ds.MeanEx, ds.WinRate, ds.BigRate)
			fmt.Fprintf(&b, "  %-8s %-8s %5d %9.2f%% %9.2f%% %7.0f%% %8.0f%%\n",
				"", "浅回调", ss.N, ss.MedEx, ss.MeanEx, ss.WinRate, ss.BigRate)
			d := ds.MedEx - ss.MedEx
			fmt.Fprintf(&b, "  %-8s %-8s %5s %+9.2fpp(深−浅)\n", "", "→差", "", d)
			if seg.label == "样本内" {
				inDiff = d
			} else {
				outDiff = d
			}
		}
		verdict := "❌ 样本外方向反转或消失 —— 判定为过拟合"
		switch {
		case inDiff > 0 && outDiff > 0 && outDiff >= inDiff*0.5:
			verdict = "✅ 样本外保住了至少一半强度 —— 支持是真信号"
		case inDiff > 0 && outDiff > 0:
			verdict = "⚠️ 样本外同向但明显衰减 —— 弱证据,别当核心依据"
		}
		fmt.Fprintf(&b, "  %s\n\n", verdict)
	}
	b.WriteString("注:阈值仅由样本内数据决定,样本外未参与任何调参。\n")
	b.WriteString("注:样本外只有 7 个月,方向一致也只是「没被证伪」,不等于已验证。\n")
	return b.String()
}

// ===== 参数敏感性扫描(2026-07-31)=====
//
// 抗过拟合的另一半:样本外验证回答"换时间还成不成立",敏感性回答"换参数还成不成立"。
// 若超额只在某一组参数上冒出来、稍微一动就没了,那是曲线拟合;若在一片区间上都稳,
// 才像是抓到了真东西。**高原比尖峰可信。**
//
// 效率关键:**只扫一次全市场**,用最宽松的参数收信号(把特征值都记下来),
// 之后所有参数变体都在内存里过滤。否则每个变体重扫一次全市场 = 10 分钟 × N,
// NAS 上根本跑不完(而且并发重扫过会 OOM)。

func looseSWParams() swParams {
	return swParams{
		FirstWaveMin: 10, FirstWaveMax: 150,
		DrawdownMin: 5, DrawdownMax: 40,
		ShrinkMax: 1.0, VolMult: 1.2,
		MinWashDays: 3, MaxWashDays: 60,
	}
}

// swPass 判定一个已收集的信号是否满足给定参数(纯内存过滤,不重扫)。
func swPass(s swSignal, p swParams) bool {
	return s.FirstWave >= p.FirstWaveMin && s.FirstWave <= p.FirstWaveMax &&
		s.Drawdown >= p.DrawdownMin && s.Drawdown <= p.DrawdownMax &&
		s.Shrink <= p.ShrinkMax && s.VolX >= p.VolMult &&
		s.WashDays >= float64(p.MinWashDays) && s.WashDays <= float64(p.MaxWashDays)
}

// SweepSecondWaveParams 参数敏感性扫描。逐个参数在基准值附近变动,其余固定为默认。
func (a *App) SweepSecondWaveParams(start, end string) string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	if start == "" {
		start = "2024-07-01"
	}
	if end == "" {
		end = a.historyService.LatestTradeDate()
	}
	t0 := time.Now()
	// 一次宽松扫描,之后全在内存里过滤
	all, stat := a.backtestSWSignalsWith(start, end, looseSWParams())
	if len(all) == 0 {
		return "宽松参数下也零命中,检查规则实现"
	}
	base := defaultSWParams()

	var b strings.Builder
	fmt.Fprintf(&b, "【二波启动 · 参数敏感性扫描】%s ~ %s\n", start, end)
	fmt.Fprintf(&b, "宽松池 n=%d(评估 %d 股日,耗时 %.0fs);基准参数下 n=%d\n",
		len(all), stat.Scanned, time.Since(t0).Seconds(), len(swFilter(all, base)))
	b.WriteString("判读:超额在一片区间上都为正 = 高原(可信);只在某点冒尖 = 曲线拟合。\n")

	type variant struct {
		label string
		mut   func(*swParams)
	}
	groups := []struct {
		name string
		vs   []variant
	}{
		{"回调下限%", []variant{
			{"≥5", func(p *swParams) { p.DrawdownMin = 5 }},
			{"≥10(基准)", func(p *swParams) { p.DrawdownMin = 10 }},
			{"≥15", func(p *swParams) { p.DrawdownMin = 15 }},
			{"≥18", func(p *swParams) { p.DrawdownMin = 18 }},
			{"≥20", func(p *swParams) { p.DrawdownMin = 20 }},
		}},
		{"缩量比上限", []variant{
			{"≤0.5", func(p *swParams) { p.ShrinkMax = 0.5 }},
			{"≤0.6", func(p *swParams) { p.ShrinkMax = 0.6 }},
			{"≤0.7(基准)", func(p *swParams) { p.ShrinkMax = 0.7 }},
			{"≤0.85", func(p *swParams) { p.ShrinkMax = 0.85 }},
			{"≤1.0(不限)", func(p *swParams) { p.ShrinkMax = 1.0 }},
		}},
		{"突破放量倍数", []variant{
			{"≥1.5", func(p *swParams) { p.VolMult = 1.5 }},
			{"≥2.0(基准)", func(p *swParams) { p.VolMult = 2.0 }},
			{"≥2.5", func(p *swParams) { p.VolMult = 2.5 }},
			{"≥3.0", func(p *swParams) { p.VolMult = 3.0 }},
		}},
		{"第一波涨幅下限%", []variant{
			{"≥10", func(p *swParams) { p.FirstWaveMin = 10 }},
			{"≥20(基准)", func(p *swParams) { p.FirstWaveMin = 20 }},
			{"≥30", func(p *swParams) { p.FirstWaveMin = 30 }},
			{"≥40", func(p *swParams) { p.FirstWaveMin = 40 }},
		}},
	}
	for _, g := range groups {
		fmt.Fprintf(&b, "\n◆ %s(其余参数固定为基准)\n", g.name)
		fmt.Fprintf(&b, "  %-14s %6s %12s %12s %9s\n", "取值", "n", "T+5中位超额", "T+5均值超额", "涨超10%")
		for _, v := range g.vs {
			p := base
			v.mut(&p)
			sub := swFilter(all, p)
			st := swStatsOf(sub, 5)
			if st.N < 30 {
				fmt.Fprintf(&b, "  %-14s %6d  (样本<30,不判读)\n", v.label, st.N)
				continue
			}
			fmt.Fprintf(&b, "  %-14s %6d %11.2f%% %11.2f%% %8.0f%%\n",
				v.label, st.N, st.MedEx, st.MeanEx, st.BigRate)
		}
	}
	b.WriteString("\n注:同一批信号池内存过滤,各行之间只有参数差异,数据与基准口径完全一致。\n")
	return b.String()
}

func swFilter(all []swSignal, p swParams) []swSignal {
	out := make([]swSignal, 0, len(all))
	for _, s := range all {
		if swPass(s, p) {
			out = append(out, s)
		}
	}
	return out
}
