package services

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/run-bigpig/jcp/internal/embed"
	"github.com/run-bigpig/jcp/internal/models"
)

const (
	waveScanPreheatDays = 240
	waveScanHistoryBars = 270
	waveRecentKDays     = 260
)

// loadIndustryMap 从内置 stock_basic.json 取 代码(带前缀)->行业。
func loadIndustryMap() map[string]string {
	var raw struct {
		Data struct {
			Fields []string        `json:"fields"`
			Items  [][]interface{} `json:"items"`
		} `json:"data"`
	}
	out := map[string]string{}
	if json.Unmarshal(embed.StockBasicJSON, &raw) != nil {
		return out
	}
	ti, ii := -1, -1
	for i, f := range raw.Data.Fields {
		if f == "ts_code" {
			ti = i
		}
		if f == "industry" {
			ii = i
		}
	}
	if ti < 0 || ii < 0 {
		return out
	}
	for _, it := range raw.Data.Items {
		ts, _ := it[ti].(string) // 000001.SZ
		ind, _ := it[ii].(string)
		parts := strings.Split(ts, ".")
		if len(parts) != 2 || ind == "" {
			continue
		}
		out[strings.ToLower(parts[1])+parts[0]] = ind
	}
	return out
}

// ===== 通达信函数等价实现（数组版） =====

func waEMA(x []float64, n int) []float64 {
	out := make([]float64, len(x))
	if len(x) == 0 {
		return out
	}
	a := 2.0 / (float64(n) + 1)
	out[0] = x[0]
	for i := 1; i < len(x); i++ {
		out[i] = x[i]*a + out[i-1]*(1-a)
	}
	return out
}

// waSMA 通达信 SMA(X,N,M)
func waSMA(x []float64, n, m int) []float64 {
	out := make([]float64, len(x))
	if len(x) == 0 {
		return out
	}
	out[0] = x[0]
	for i := 1; i < len(x); i++ {
		out[i] = (x[i]*float64(m) + out[i-1]*float64(n-m)) / float64(n)
	}
	return out
}

func waMA(x []float64, n int) []float64 {
	out := make([]float64, len(x))
	var sum float64
	for i := range x {
		sum += x[i]
		if i >= n {
			sum -= x[i-n]
		}
		denom := n
		if i+1 < n {
			denom = i + 1
		}
		out[i] = sum / float64(denom)
	}
	return out
}

func waREF(x []float64, k int) []float64 {
	out := make([]float64, len(x))
	for i := range x {
		if i >= k {
			out[i] = x[i-k]
		} else {
			out[i] = x[i]
		}
	}
	return out
}

func waHHV(x []float64, n int) []float64 {
	out := make([]float64, len(x))
	for i := range x {
		hi := x[i]
		for j := i; j > i-n && j >= 0; j-- {
			if x[j] > hi {
				hi = x[j]
			}
		}
		out[i] = hi
	}
	return out
}

func waLLV(x []float64, n int) []float64 {
	out := make([]float64, len(x))
	for i := range x {
		lo := x[i]
		for j := i; j > i-n && j >= 0; j-- {
			if x[j] < lo {
				lo = x[j]
			}
		}
		out[i] = lo
	}
	return out
}

func waAVEDEV(x []float64, n int) []float64 {
	out := make([]float64, len(x))
	ma := waMA(x, n)
	for i := range x {
		var sum float64
		cnt := 0
		for j := i; j > i-n && j >= 0; j-- {
			sum += math.Abs(x[j] - ma[i])
			cnt++
		}
		if cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

func waCross(a, b []float64, i int) bool {
	if i < 1 {
		return false
	}
	return a[i] > b[i] && a[i-1] <= b[i-1]
}

func waCrossDown(a, b []float64, i int) bool {
	if i < 1 {
		return false
	}
	return a[i] < b[i] && a[i-1] >= b[i-1]
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// computeFishV2Signals 实现"吃鱼身V2"人工框架的可回测部分。
// 入场: 鱼身≥3条 且 (放量突破 或 缩量回踩不破) 且 不追高(离MA5≤10%)。
// 离场: 跌破10日线 或 远离MA5超12%止盈。排序按相对强度。
func computeFishV2Signals(dates []string, o, h, l, c, v, amount []float64, mret3 map[string]float64, full bool) waveSignals {
	n := len(c)
	w := waveSignals{dates: dates, open: o, high: h, low: l, close: c, amount: amount}
	entry := make([]bool, n)
	exit := make([]bool, n)
	rank := make([]float64, n)
	w.entry, w.exit, w.rank = entry, exit, rank
	if n < 60 {
		return w
	}
	ma5 := waMA(c, 5)
	ma10 := waMA(c, 10)
	ma20 := waMA(c, 20)
	vol5 := waMA(v, 5)
	hh5 := waHHV(h, 5)
	hh20 := waHHV(h, 20)
	hh30 := waHHV(h, 30)
	hh60 := waHHV(h, 60)
	ll10 := waLLV(l, 10)

	for i := 3; i < n; i++ {
		// 鱼身4条
		cond1 := c[i] > ma5[i] && c[i] > ma10[i] && c[i] > ma20[i] // 站上短中期
		cond2 := ma5[i] > ma10[i] && ma10[i] > ma20[i]             // 多头排列
		cond3 := hh5[i] >= hh30[i]*0.98 && c[i] > ma20[i]          // 近期创新高 且 回调不破中期
		ret3 := 0.0
		if c[i-3] > 0 {
			ret3 = (c[i]/c[i-3] - 1) * 100
		}
		mr3 := mret3[dates[i]]
		cond4 := ret3 > mr3 && ret3 > 0 // 强于大盘
		score := 0
		for _, b := range []bool{cond1, cond2, cond3, cond4} {
			if b {
				score++
			}
		}

		// 买点结构
		platHigh := hh20[i-1] // 突破前的20日平台高
		breakout := c[i] > platHigh && v[i] > vol5[i]*1.5
		pullback := l[i] <= ma10[i]*1.02 && c[i] >= ma10[i]*0.99 && c[i] >= ma5[i]*0.98 && v[i] < vol5[i]*0.85
		buyTrigger := breakout || pullback
		notChase := c[i] <= ma5[i]*1.10 // 离5日线不超10%

		// 风险收益比≥2:1（止损=10日低点，目标=前高或+测算空间）
		rrOK := true
		if full {
			stop := ll10[i]
			risk := c[i] - stop
			upside := hh60[i] - c[i]
			if upside < c[i]*0.08 {
				upside = c[i] * 0.08 // 创新高时给测算空间
			}
			rrOK = risk > 0 && upside/risk >= 2.0
		}

		entry[i] = score >= 3 && buyTrigger && notChase && rrOK
		exit[i] = c[i] < ma10[i] || c[i] > ma5[i]*1.12 // 跌破10日线 或 远离止盈
		rank[i] = ret3 - mr3                           // 相对强度排序
	}
	return w
}

// computeDriverSignals 复现 app 驾驶舱引擎 calculateTradingSignals 的 coreBuy(入场) 与 sellClear/止盈/减仓(离场)。
func computeDriverSignals(dates []string, o, h, l, c, v, amount []float64) waveSignals {
	n := len(c)
	w := waveSignals{dates: dates, open: o, high: h, low: l, close: c}
	entry := make([]bool, n)
	exit := make([]bool, n)
	rank := make([]float64, n)
	w.entry, w.exit, w.rank = entry, exit, rank
	if n < 70 {
		return w
	}
	ma5 := waMA(c, 5)
	ma10 := waMA(c, 10)
	ma20 := waMA(c, 20)
	ma60 := waMA(c, 60)
	e12 := waEMA(c, 12)
	e13 := waEMA(c, 13)
	e26 := waEMA(c, 26)
	e34 := waEMA(c, 34)
	e55 := waEMA(c, 55)
	dif := make([]float64, n)
	for i := range c {
		dif[i] = e12[i] - e26[i]
	}
	dea := waEMA(dif, 9)
	vol5 := waMA(v, 5)
	vol20 := waMA(v, 20)
	amt20 := waMA(amount, 20)
	closePos := make([]float64, n)
	energy := make([]float64, n)
	for i := range c {
		rng := math.Max(h[i]-l[i], 0.01)
		closePos[i] = (c[i] - l[i]) / rng
		energy[i] = v[i] * (closePos[i] - 0.5)
	}
	high20 := waHHV(h, 20)
	low20 := waLLV(l, 20)
	trendWeakArr := make([]bool, n)
	trendCashArr := make([]bool, n)
	lastEatFish := -1
	for i := 1; i < n; i++ {
		pct := 0.0
		if c[i-1] > 0 {
			pct = (c[i] - c[i-1]) / c[i-1]
		}
		trendStrong := e13[i] > e34[i] && e34[i] > e55[i] && c[i] > ma20[i]
		trendWeak := c[i] < ma20[i] || e13[i] < e34[i]
		trendHold := ma5[i] > ma10[i] && c[i] > ma20[i]
		trendCash := ma5[i] < ma10[i] || c[i] < ma20[i]
		trendWeakArr[i], trendCashArr[i] = trendWeak, trendCash

		eatFishStart := waCross(e13, e55, i) && e13[i] > e34[i] && c[i] > e13[i]
		eatFishContinue := lastEatFish >= 0 && (i-lastEatFish) <= 30 && e13[i] > e34[i] && c[i] > ma10[i] && closePos[i] > 0.45 && pct > -0.025
		eatFish := eatFishStart || eatFishContinue
		if eatFish {
			lastEatFish = i
		}
		prevHigh20 := 0.0
		for j := i - 1; j > i-21 && j >= 0; j-- {
			if c[j] > prevHigh20 {
				prevHigh20 = c[j]
			}
		}
		volumeExpansion := v[i] > math.Max(v[i-1]*1.2, vol5[i]*1.05)
		amountOk := amount[i] > 5e7 || amount[i] > amt20[i]*1.25
		moneyFire := c[i] > prevHigh20 && volumeExpansion && amountOk && c[i] > o[i] && closePos[i] > 0.6
		energyPrev := energy[i-1]
		shortBull := ma5[i] > ma10[i] && c[i] > ma10[i]
		midBull := ma10[i] > ma20[i] && ma20[i] >= ma60[i]*0.98
		gz := trendHold && energy[i] > 0 && energy[i] >= energyPrev*0.8 && shortBull && midBull
		entryScore := b2f(eatFish)*30 + b2f(moneyFire)*25 + b2f(gz)*20 + b2f(trendStrong)*15 + b2f(trendHold)*10

		streak := 0
		for j := i; j >= 0 && j > i-5; j-- {
			if c[j] > e55[j]*1.08 {
				streak++
			} else {
				break
			}
		}
		highZoneDev := 1.0
		if high20[i] > 0 {
			highZoneDev = (high20[i] - c[i]) / high20[i]
		}
		highZoneBlocked := streak >= 5 && highZoneDev < 0.05
		coreBuy := entryScore >= 70 && !highZoneBlocked

		macdDead := waCrossDown(dif, dea, i)
		nearUpperRange := high20[i] > low20[i] && c[i] > low20[i]+(high20[i]-low20[i])*0.82
		recentEatFish := lastEatFish >= 0 && (i-lastEatFish) <= 30
		prevTrendWeak := i >= 2 && trendWeakArr[i-1]
		prevTrendCash := i >= 2 && trendCashArr[i-1]
		energyTurnNeg := energy[i] < 0 && energyPrev >= 0
		takeProfit := recentEatFish && ((nearUpperRange && pct < 0 && v[i] > vol5[i]*1.15) || macdDead)
		sellReduce := macdDead || (trendWeak && !prevTrendWeak) || (trendCash && !prevTrendCash) || (recentEatFish && energyTurnNeg)
		sellClear := c[i] < ma20[i] && v[i] > vol20[i]*1.1 && pct < -0.025

		entry[i] = coreBuy
		exit[i] = sellClear || takeProfit || sellReduce
		rank[i] = entryScore
	}
	return w
}

// ===== 吃鱼身/短买/止盈/资金点火/控盘度 信号计算 =====

type waveSignals struct {
	dates                      []string
	open, high, low, close     []float64
	shortBuy, takeProfit, weak []bool    // 短买开仓 / 及时止盈 / 弱势(彩带翻绿)
	eatFishSig                 []bool    // 吃鱼身
	ignite, kongpan            []bool    // 资金点火(森68) / 高控盘
	kongpanVal                 []float64 // 控盘度
	entry, exit                []bool    // 统一入场/离场信号(按模式生成)
	rank                       []float64 // 排序分
	amount                     []float64 // 成交额(龙头排名用)
}

// computeWaveSignals 复现通达信"开仓吃鱼"主图 + 资金点火 + 五维控盘度。
func computeWaveSignals(dates []string, o, h, l, c, v, amount []float64, kongpanMin float64, ride bool) waveSignals {
	n := len(c)
	w := waveSignals{dates: dates, open: o, high: h, low: l, close: c}
	if n < 70 {
		w.entry, w.exit, w.rank = make([]bool, n), make([]bool, n), make([]float64, n)
		return w
	}

	// --- 吃鱼身核心 ---
	ema1 := waEMA(c, 2)
	ema2 := waEMA(ema1, 2)
	ema3 := waEMA(ema2, 2)
	fast := waEMA(ema3, 2)           // 快慢线
	slow := waEMA(waREF(fast, 1), 2) // 慢快线
	abc1 := make([]float64, n)
	for i := 0; i < n; i++ {
		abc1[i] = (c[i] + h[i] + l[i]) / 3
	}
	life := waEMA(waEMA(c, 8), 13) // 生命价线
	abc2 := waEMA(abc1, 14)
	abc4 := waEMA(abc1, 5)

	eatFish := make([]bool, n)
	for i := 0; i < n; i++ {
		crossUp := waCross(abc4, life, i)
		justAbove := abc4[i] > life[i] && i >= 1 && abc4[i-1] < life[i-1]*1.02
		eatFish[i] = (crossUp || justAbove) && fast[i] > slow[i] && c[i] > fast[i]
	}

	// 彩带 强势/弱势 + 持股
	strong := make([]bool, n)
	weak := make([]bool, n)
	jj := abc1
	aEMA := waEMA(jj, 13)
	holdTrend := make([]bool, n) // 趋势持股 A>REF(A,1)
	for i := 0; i < n; i++ {
		strong[i] = life[i] < abc2[i]
		weak[i] = life[i] > abc2[i]
		if i >= 1 {
			holdTrend[i] = aEMA[i] > aEMA[i-1]
		}
	}

	// 吃鱼后 = 最近60根内出现过吃鱼身
	eatFishWithin := make([]bool, n)
	for i := 0; i < n; i++ {
		for j := i; j > i-60 && j >= 0; j-- {
			if eatFish[j] {
				eatFishWithin[i] = true
				break
			}
		}
	}
	allow := make([]bool, n)
	for i := 0; i < n; i++ {
		allow[i] = strong[i] && holdTrend[i] && eatFishWithin[i]
	}

	// --- 持股/持币 锯齿腿 + 拐点 VAR19(短买)/VAR1A(止盈) ---
	hold := legMembership(c, true)   // 持股腿(VAR1..VARC)
	cashL := legMembership(c, false) // 持币腿(VARD..VAR18)
	var1 := make([]bool, n)          // 持股起始(C>ref1 && C>ref2)
	vard := make([]bool, n)          // 持币起始(C<ref1 && C<ref2)
	for i := 2; i < n; i++ {
		var1[i] = c[i] > c[i-1] && c[i] > c[i-2]
		vard[i] = c[i] < c[i-1] && c[i] < c[i-2]
	}
	shortBuy := make([]bool, n)
	takeProfit := make([]bool, n)
	for i := 1; i < n; i++ {
		var19 := cashL[i-1] && var1[i] // 从持币腿拐头向上
		var1a := hold[i-1] && vard[i]  // 从持股腿拐头向下
		shortBuy[i] = var19 && allow[i]
		takeProfit[i] = var1a && allow[i]
	}

	// --- 资金点火 森68 ---
	ignite := computeIgnite(o, h, l, c, v, amount)

	// --- 控盘度 ---
	aaa := make([]float64, n)
	for i := 0; i < n; i++ {
		aaa[i] = (3*c[i] + o[i] + h[i] + l[i]) / 6
	}
	e12 := waEMA(aaa, 12)
	e36 := waEMA(aaa, 36)
	kpVal := make([]float64, n)
	kp := make([]bool, n)
	for i := 1; i < n; i++ {
		if e36[i-1] != 0 {
			kpVal[i] = (e12[i]-e36[i-1])/e36[i-1]*100 + 50
		}
		kp[i] = kpVal[i] >= kongpanMin
	}

	w.shortBuy, w.takeProfit, w.weak = shortBuy, takeProfit, weak
	w.eatFishSig = eatFish
	w.ignite, w.kongpan, w.kongpanVal = ignite, kp, kpVal

	// 统一入场/离场(按 ride 模式)
	entry := make([]bool, n)
	exit := make([]bool, n)
	for i := 0; i < n; i++ {
		if ride {
			entry[i] = (eatFish[i] || ignite[i]) && kp[i]
			exit[i] = weak[i]
		} else {
			entry[i] = shortBuy[i] && (ignite[i] || kp[i])
			exit[i] = takeProfit[i] || weak[i]
		}
	}
	w.entry, w.exit, w.rank = entry, exit, kpVal
	return w
}

// legMembership 复现 VAR1..VARC(up=true 持股) / VARD..VAR18(up=false 持币) 的锯齿腿成员。
func legMembership(c []float64, up bool) []bool {
	n := len(c)
	const levels = 12
	cur := make([]bool, n) // 当前层
	member := make([]bool, n)
	// level 1
	for i := 2; i < n; i++ {
		if up {
			cur[i] = c[i] > c[i-1] && c[i] > c[i-2]
		} else {
			cur[i] = c[i] < c[i-1] && c[i] < c[i-2]
		}
	}
	for i := range cur {
		member[i] = cur[i]
	}
	prev := cur
	for k := 2; k <= levels; k++ {
		next := make([]bool, n)
		// 偶数层与奇数层交替；持股与持币方向相反
		even := k%2 == 0
		for i := 2; i < n; i++ {
			var cond bool
			// 持股: 偶层 C<=ref1 && C>=ref2 ; 奇层 C>=ref1 && C<=ref2
			// 持币: 相反
			a := c[i] <= c[i-1] && c[i] >= c[i-2]
			b := c[i] >= c[i-1] && c[i] <= c[i-2]
			if up {
				if even {
					cond = a
				} else {
					cond = b
				}
			} else {
				if even {
					cond = b
				} else {
					cond = a
				}
			}
			next[i] = prev[i-1] && cond
		}
		for i := range next {
			if next[i] {
				member[i] = true
			}
		}
		prev = next
	}
	return member
}

// computeIgnite 复现"开仓吃鱼"里的 森68 异动起爆(资金点火)。
func computeIgnite(o, h, l, c, v, amount []float64) []bool {
	n := len(c)
	ig := make([]bool, n)
	if n < 240 {
		return ig
	}
	e14 := waEMA(c, 20) // 森14=EMA20
	e30 := waEMA(c, 30)
	e35 := waEMA(c, 35)
	e40 := waEMA(c, 40)
	e45 := waEMA(c, 45)
	e90 := waEMA(c, 90)
	e98 := waEMA(c, 98)
	e106 := waEMA(c, 106)
	e114 := waEMA(c, 114)
	e140 := waEMA(c, 140)
	e148 := waEMA(c, 148)
	e156 := waEMA(c, 156)
	e164 := waEMA(c, 164)
	max4 := func(a, b, cc, d float64) float64 { return math.Max(math.Max(a, b), math.Max(cc, d)) }
	min4 := func(a, b, cc, d float64) float64 { return math.Min(math.Min(a, b), math.Min(cc, d)) }
	e5 := waEMA(c, 5)
	e10 := waEMA(c, 10)
	e20 := waEMA(c, 20)
	ma30 := waMA(c, 30)
	ma60 := waMA(c, 60)
	ma90 := waMA(c, 90)
	ma240 := waMA(c, 240)
	typ := make([]float64, n)
	for i := 0; i < n; i++ {
		typ[i] = (h[i] + l[i] + c[i]) / 3
	}
	ma81 := waMA(typ, 81)
	ad81 := waAVEDEV(typ, 81)

	for i := 1; i < n; i++ {
		s27 := max4(e30[i], e35[i], e40[i], e45[i])
		s29 := max4(e90[i], e98[i], e106[i], e114[i])
		s31 := max4(e140[i], e148[i], e156[i], e164[i])
		s28 := min4(e30[i], e35[i], e40[i], e45[i])
		s30 := min4(e90[i], e98[i], e106[i], e114[i])
		s32 := min4(e140[i], e148[i], e156[i], e164[i])
		s33 := math.Max(math.Max(s27, s29), s31)
		s34 := math.Min(math.Min(s28, s30), s32)
		if s34 == 0 {
			continue
		}
		s35 := s33 / s34
		s36 := l[i] / s27
		s36p := l[i-1] / max4(e30[i-1], e35[i-1], e40[i-1], e45[i-1])
		s37 := s35 < 1.3 && s36p < 1.05 && c[i] > e14[i] && c[i] > s33 && s36 < 1.08
		amt := amount[i] / 10000 // 万元
		amtp := amount[i-1] / 10000
		s39 := math.Min(amt, amtp) > 2000 && amt > 8000
		s40 := c[i] / c[i-1]
		s44 := math.Max(math.Max(e5[i], e10[i]), e20[i])
		s45 := math.Min(math.Min(e5[i], e10[i]), e20[i])
		volUp := v[i] > v[i-1]*1.2
		s46 := l[i] < s45 && c[i] > s44 && volUp && s39 && s40 > 1.029 && s37
		s48 := 0.0
		if ad81[i] != 0 {
			s48 = (typ[i] - ma81[i]) * 1000 / (15 * ad81[i])
		}
		s48p := 0.0
		if ad81[i-1] != 0 {
			s48p = (typ[i-1] - ma81[i-1]) * 1000 / (15 * ad81[i-1])
		}
		s49 := (s48p <= 100 && s48 > 100) && volUp && s39 && s40 > 1.029 && s37
		// 森64 均线收敛突破
		s54 := math.Abs(ma30[i]/ma60[i] - 1)
		s55 := math.Abs(ma60[i]/ma90[i] - 1)
		s56 := math.Abs(ma30[i]/ma90[i] - 1)
		s58 := s40 - 1
		s59 := (ma30[i] + ma60[i] + ma90[i]) / 3
		s60 := c[i] > s59*1.04 && c[i] < s59*1.15
		s62 := math.Abs(ma240[i]/ma240[i-20*boolToInt(i >= 20)] - 1)
		s63 := s62 < 0.04
		s64 := s54 < 0.04 && s55 < 0.04 && s56 < 0.04 && s58 > 0.04 && s60 && s63 && s59 > ma240[i]
		s65 := s64 && volUp && s39 && s37
		s66 := s35 < 1.15 && s36p < 1.04 && c[i] > e14[i] && c[i] > s33 && s36 < 1.08 && s40 > 1.04 && volUp && s39 && s37
		s67 := l[i] < s34 && c[i] > s33 && s40 > 1.05 && v[i] > v[i-1]*1.2
		ig[i] = s46 || s49 || s65 || s66 || s67
	}
	return ig
}

// computeIgniteRelaxed 复现用户给出的第一段"异动现主力进"宽口径版本。
// 它降低成交额、放量、均线收敛门槛，适合把"资金点火前后"作为波段候选维度。
func computeIgniteRelaxed(o, h, l, c, v, amount []float64) []bool {
	n := len(c)
	ig := make([]bool, n)
	if n < 240 {
		return ig
	}
	e14 := waEMA(c, 20)
	e30 := waEMA(c, 30)
	e35 := waEMA(c, 35)
	e40 := waEMA(c, 40)
	e45 := waEMA(c, 45)
	e90 := waEMA(c, 90)
	e98 := waEMA(c, 98)
	e106 := waEMA(c, 106)
	e114 := waEMA(c, 114)
	e140 := waEMA(c, 140)
	e148 := waEMA(c, 148)
	e156 := waEMA(c, 156)
	e164 := waEMA(c, 164)
	max4 := func(a, b, cc, d float64) float64 { return math.Max(math.Max(a, b), math.Max(cc, d)) }
	min4 := func(a, b, cc, d float64) float64 { return math.Min(math.Min(a, b), math.Min(cc, d)) }
	e5 := waEMA(c, 5)
	e10 := waEMA(c, 10)
	e20 := waEMA(c, 20)
	ma30 := waMA(c, 30)
	ma60 := waMA(c, 60)
	ma90 := waMA(c, 90)
	ma240 := waMA(c, 240)
	typ := make([]float64, n)
	for i := 0; i < n; i++ {
		typ[i] = (h[i] + l[i] + c[i]) / 3
	}
	ma81 := waMA(typ, 81)
	ad81 := waAVEDEV(typ, 81)

	for i := 1; i < n; i++ {
		s27 := max4(e30[i], e35[i], e40[i], e45[i])
		s29 := max4(e90[i], e98[i], e106[i], e114[i])
		s31 := max4(e140[i], e148[i], e156[i], e164[i])
		s28 := min4(e30[i], e35[i], e40[i], e45[i])
		s30 := min4(e90[i], e98[i], e106[i], e114[i])
		s32 := min4(e140[i], e148[i], e156[i], e164[i])
		s33 := math.Max(math.Max(s27, s29), s31)
		s34 := math.Min(math.Min(s28, s30), s32)
		if s27 == 0 || s34 == 0 || c[i-1] == 0 {
			continue
		}
		s35 := s33 / s34
		s36 := l[i] / s27
		prevS27 := max4(e30[i-1], e35[i-1], e40[i-1], e45[i-1])
		if prevS27 == 0 {
			continue
		}
		s36p := l[i-1] / prevS27
		s37 := s35 < 1.5 && s36p < 1.08 && c[i] > e14[i] && c[i] > s33 && s36 < 1.1
		amt := amount[i] / 10000 // 万元
		amtp := amount[i-1] / 10000
		s39 := math.Min(amt, amtp) > 600 && amt > 1500
		s40 := c[i] / c[i-1]
		s44 := math.Max(math.Max(e5[i], e10[i]), e20[i])
		s45 := math.Min(math.Min(e5[i], e10[i]), e20[i])
		volUp := v[i] > v[i-1]*1.05
		s46 := l[i] < s45 && c[i] > s44 && volUp && s39 && s40 > 1.01 && s37
		s48 := 0.0
		if ad81[i] != 0 {
			s48 = (typ[i] - ma81[i]) * 1000 / (15 * ad81[i])
		}
		s48p := 0.0
		if ad81[i-1] != 0 {
			s48p = (typ[i-1] - ma81[i-1]) * 1000 / (15 * ad81[i-1])
		}
		s49 := (s48p <= 100 && s48 > 100) && volUp && s39 && s40 > 1.01 && s37
		s54 := math.Abs(ma30[i]/ma60[i] - 1)
		s55 := math.Abs(ma60[i]/ma90[i] - 1)
		s56 := math.Abs(ma30[i]/ma90[i] - 1)
		s58 := s40 - 1
		s59 := (ma30[i] + ma60[i] + ma90[i]) / 3
		s60 := c[i] > s59*1.01 && c[i] < s59*1.25
		refIdx := i
		if i >= 20 {
			refIdx = i - 20
		}
		s62 := 0.0
		if ma240[refIdx] != 0 {
			s62 = math.Abs(ma240[i]/ma240[refIdx] - 1)
		}
		s63 := s62 < 0.08
		s64 := s54 < 0.08 && s55 < 0.08 && s56 < 0.08 && s58 > 0.02 && s60 && s63 && s59 > ma240[i]
		s65 := s64 && volUp && s39 && s37
		s66 := s35 < 1.35 && s36p < 1.10 && c[i] > e14[i] && c[i] > s33 && s36 < 1.15 && s40 > 1.02 && volUp && s39 && s37
		s67 := l[i] < s34 && c[i] > s33 && s40 > 1.02 && volUp
		ig[i] = s46 || s49 || s65 || s66 || s67
	}
	return ig
}

// computeIgniteStrictV1 复现用户第二段"森舟实战·短线能量"里的森舟68强口径。
// 注意：该段森舟19是 EMA(CLOSE,9)，和旧 computeIgnite 的 EMA90 不同，所以波段1.0单独使用这个版本。
func computeIgniteStrictV1(o, h, l, c, v, amount []float64) []bool {
	n := len(c)
	ig := make([]bool, n)
	if n < 240 {
		return ig
	}
	e14 := waEMA(c, 20)
	e30 := waEMA(c, 30)
	e35 := waEMA(c, 35)
	e40 := waEMA(c, 40)
	e45 := waEMA(c, 45)
	e9 := waEMA(c, 9)
	e98 := waEMA(c, 98)
	e106 := waEMA(c, 106)
	e114 := waEMA(c, 114)
	e140 := waEMA(c, 140)
	e148 := waEMA(c, 148)
	e156 := waEMA(c, 156)
	e164 := waEMA(c, 164)
	max4 := func(a, b, cc, d float64) float64 { return math.Max(math.Max(a, b), math.Max(cc, d)) }
	min4 := func(a, b, cc, d float64) float64 { return math.Min(math.Min(a, b), math.Min(cc, d)) }
	e5 := waEMA(c, 5)
	e10 := waEMA(c, 10)
	e20 := waEMA(c, 20)
	ma30 := waMA(c, 30)
	ma60 := waMA(c, 60)
	ma90 := waMA(c, 90)
	ma240 := waMA(c, 240)
	typ := make([]float64, n)
	for i := 0; i < n; i++ {
		typ[i] = (h[i] + l[i] + c[i]) / 3
	}
	ma81 := waMA(typ, 81)
	ad81 := waAVEDEV(typ, 81)

	for i := 1; i < n; i++ {
		s27 := max4(e30[i], e35[i], e40[i], e45[i])
		s29 := max4(e9[i], e98[i], e106[i], e114[i])
		s31 := max4(e140[i], e148[i], e156[i], e164[i])
		s28 := min4(e30[i], e35[i], e40[i], e45[i])
		s30 := min4(e9[i], e98[i], e106[i], e114[i])
		s32 := min4(e140[i], e148[i], e156[i], e164[i])
		s33 := math.Max(math.Max(s27, s29), s31)
		s34 := math.Min(math.Min(math.Min(s28, s30), s32), s28)
		if s27 == 0 || s34 == 0 || c[i-1] == 0 {
			continue
		}
		s35 := s33 / s34
		s36 := l[i] / s27
		prevS27 := max4(e30[i-1], e35[i-1], e40[i-1], e45[i-1])
		if prevS27 == 0 {
			continue
		}
		s36p := l[i-1] / prevS27
		s37 := s35 < 1.3 && s36p < 1.05 && c[i] > e14[i] && c[i] > s33 && s36 < 1.08
		amt := amount[i] / 10000
		amtp := amount[i-1] / 10000
		s39 := math.Min(amt, amtp) > 2000 && amt > 8000
		s40 := c[i] / c[i-1]
		s44 := math.Max(math.Max(e5[i], e10[i]), e20[i])
		s45 := math.Min(math.Min(e5[i], e10[i]), e20[i])
		volUp := v[i] > v[i-1]*1.2
		s46 := l[i] < s45 && c[i] > s44 && volUp && s39 && s40 > 1.029 && s37
		s48 := 0.0
		if ad81[i] != 0 {
			s48 = (typ[i] - ma81[i]) * 1000 / (15 * ad81[i])
		}
		s48p := 0.0
		if ad81[i-1] != 0 {
			s48p = (typ[i-1] - ma81[i-1]) * 1000 / (15 * ad81[i-1])
		}
		s49 := (s48p <= 100 && s48 > 100) && volUp && s39 && s40 > 1.029 && s37
		s54 := math.Abs(ma30[i]/ma60[i] - 1)
		s55 := math.Abs(ma60[i]/ma90[i] - 1)
		s56 := math.Abs(ma30[i]/ma90[i] - 1)
		s58 := s40 - 1
		s59 := (ma30[i] + ma60[i] + ma90[i]) / 3
		s60 := c[i] > s59*1.04 && c[i] < s59*1.15
		refIdx := i
		if i >= 20 {
			refIdx = i - 20
		}
		s62 := 0.0
		if ma240[refIdx] != 0 {
			s62 = math.Abs(ma240[i]/ma240[refIdx] - 1)
		}
		s63 := s62 < 0.04
		s64 := s54 < 0.04 && s55 < 0.04 && s56 < 0.04 && s58 > 0.04 && s60 && s63 && s59 > ma240[i]
		s65 := s64 && volUp && s39 && s37
		s66 := s35 < 1.15 && s36p < 1.04 && c[i] > e14[i] && c[i] > s33 && s36 < 1.08 && s40 > 1.04 && volUp && s39 && s37
		s67 := l[i] < s34 && c[i] > s33 && s40 > 1.05 && volUp
		ig[i] = s46 || s49 || s65 || s66 || s67
	}
	return ig
}

type waveV1Signals struct {
	entry, eatFish, relaxedIgnite, strictIgnite []bool
	recentRelaxed, recentStrict                 []bool
	mainOpenFish, timelyTakeProfit              []bool
	breakTakeProfit                             []bool
	strongSignal, mainRise                      []bool
	strongCount                                 []int
	mainControlStart, mainControlReduce         []bool
	buyState, trendBull, energyBull             []bool
	midBull, shortBull, gz                      []bool
	kongpanVal, score                           []float64
	level, phase                                []string
}

// computeWaveV1Signals 合并三段通达信公式：吃鱼身/异动点火、短线能量、买卖红绿灯。
func computeWaveV1Signals(dates []string, o, h, l, c, v, amount []float64) waveV1Signals {
	n := len(c)
	out := waveV1Signals{
		entry: make([]bool, n), eatFish: make([]bool, n), relaxedIgnite: make([]bool, n), strictIgnite: make([]bool, n),
		recentRelaxed: make([]bool, n), recentStrict: make([]bool, n), strongSignal: make([]bool, n), mainRise: make([]bool, n),
		mainOpenFish: make([]bool, n), timelyTakeProfit: make([]bool, n), breakTakeProfit: make([]bool, n),
		strongCount: make([]int, n), mainControlStart: make([]bool, n), mainControlReduce: make([]bool, n),
		buyState: make([]bool, n), trendBull: make([]bool, n), energyBull: make([]bool, n),
		midBull: make([]bool, n), shortBull: make([]bool, n), gz: make([]bool, n),
		kongpanVal: make([]float64, n), score: make([]float64, n), level: make([]string, n), phase: make([]string, n),
	}
	if n < 240 {
		return out
	}

	// 第一维：吃鱼身。
	ema1 := waEMA(c, 2)
	ema2 := waEMA(ema1, 2)
	ema3 := waEMA(ema2, 2)
	fast := waEMA(ema3, 2)
	slow := waEMA(waREF(fast, 1), 2)
	abc1 := make([]float64, n)
	for i := range c {
		abc1[i] = (c[i] + h[i] + l[i]) / 3
	}
	life := waEMA(waEMA(c, 8), 13)
	abc2 := waEMA(abc1, 14)
	abc4 := waEMA(abc1, 5)
	for i := range c {
		crossUp := waCross(abc4, life, i)
		justAbove := i >= 1 && abc4[i] > life[i] && abc4[i-1] < life[i-1]*1.02
		out.eatFish[i] = (crossUp || justAbove) && fast[i] > slow[i] && c[i] > fast[i]
	}
	out.relaxedIgnite = computeIgniteRelaxed(o, h, l, c, v, amount)
	out.strictIgnite = computeIgniteStrictV1(o, h, l, c, v, amount)
	for i := range c {
		out.recentRelaxed[i] = recentTrue(out.relaxedIgnite, i, 10)
		out.recentStrict[i] = recentTrue(out.strictIgnite, i, 10)
	}

	// 主图日K：开仓吃鱼 / 及时止盈 / 放量破位止盈。
	ma5Main := waMA(c, 5)
	ma10Main := waMA(c, 10)
	vol5Main := waMA(v, 5)
	aTrend := waEMA(abc1, 13)
	holdTrend := make([]bool, n)
	strongRibbon := make([]bool, n)
	eatFishAfter := make([]bool, n)
	holdLegMain := legMembershipMain(c, true)
	cashLegMain := legMembershipMain(c, false)
	vardMain := make([]bool, n)
	for i := 0; i < n; i++ {
		if i >= 1 {
			holdTrend[i] = aTrend[i] > aTrend[i-1]
		}
		strongRibbon[i] = holdTrend[i] && life[i] < abc2[i]
		eatFishAfter[i] = recentTrue(out.eatFish, i, 60)
		if i >= 2 {
			vardMain[i] = c[i] < c[i-1] && c[i] < c[i-2]
		}
	}
	for i := 1; i < n; i++ {
		var19 := cashLegMain[i-1] && c[i] > c[i-1]
		var1a := holdLegMain[i-1] && vardMain[i]
		allowShortBuy := strongRibbon[i] && holdTrend[i] && eatFishAfter[i]
		out.mainOpenFish[i] = var19 && allowShortBuy
		out.breakTakeProfit[i] = ma10Main[i] > c[i] && ma10Main[i-1] <= c[i-1] && c[i-1] > ma5Main[i-1] && v[i] > vol5Main[i]*1.2
		out.timelyTakeProfit[i] = var1a || out.breakTakeProfit[i]
	}

	// 第二维：严格异动后的转强/主升/超强计数。
	lastStrict := -1
	countStrong := 0
	for i := 1; i < n; i++ {
		if out.strictIgnite[i] {
			lastStrict = i
			countStrong = 0
		}
		if lastStrict >= 0 {
			bars := i - lastStrict
			if bars >= 1 && bars <= 50 && c[i-1] > 0 && c[i]/c[i-1] > 1.05 {
				out.strongSignal[i] = true
				countStrong++
			}
			out.strongCount[i] = countStrong
			out.mainRise[i] = out.strongSignal[i] && countStrong >= 2
		}
	}

	// 第二维补充：主力控盘拉升/减仓。
	controlBase := waEMA(waEMA(c, 9), 9)
	control := make([]float64, n)
	shipping := make([]bool, n)
	for i := 1; i < n; i++ {
		if controlBase[i-1] != 0 {
			control[i] = (controlBase[i] - controlBase[i-1]) / controlBase[i-1] * 1000
		}
		out.mainControlStart[i] = control[i] > 0 && control[i-1] <= 0
		shipping[i] = control[i] < control[i-1] && control[i] > 0
		out.mainControlReduce[i] = shipping[i] && !shipping[i-1]
	}

	// 第三维：控盘度 + 买卖红绿灯。
	aaa := make([]float64, n)
	for i := range c {
		aaa[i] = (3*c[i] + o[i] + h[i] + l[i]) / 6
	}
	aaa12 := waEMA(aaa, 12)
	aaa36 := waEMA(aaa, 36)
	for i := 1; i < n; i++ {
		if aaa36[i-1] != 0 {
			out.kongpanVal[i] = (aaa12[i]-aaa36[i-1])/aaa36[i-1]*100 + 50
		}
	}

	diff := make([]float64, n)
	e12 := waEMA(c, 12)
	e26 := waEMA(c, 26)
	for i := range c {
		diff[i] = e12[i] - e26[i]
	}
	dea := waEMA(diff, 9)
	rsv55 := stockPercent(c, h, l, 55)
	k := waSMA(rsv55, 13, 1)
	d := waSMA(k, 8, 1)
	lwrRaw := negStockPercent(c, h, l, 21)
	lwr1 := waSMA(lwrRaw, 13, 1)
	lwr2 := waSMA(lwr1, 17, 1)
	mav := make([]float64, n)
	for i := range c {
		mav[i] = (c[i]*2 + h[i] + l[i]) / 4
	}
	sk := make([]float64, n)
	em13 := waEMA(mav, 13)
	em55 := waEMA(mav, 55)
	for i := range c {
		sk[i] = em13[i] - em55[i]
	}
	sd := waEMA(sk, 7)
	ma5 := waMA(c, 5)
	gu3 := make([]float64, n)
	for i := range c {
		gu3[i] = (2*c[i] + h[i] + l[i]) / 4
	}
	ll34 := waLLV(l, 34)
	hh34 := waHHV(h, 34)
	mainRaw := make([]float64, n)
	for i := range c {
		if hh34[i] != ll34[i] {
			mainRaw[i] = (gu3[i] - ll34[i]) / (hh34[i] - ll34[i]) * 100
		}
	}
	mainLine := waEMA(mainRaw, 13)
	retailSeed := make([]float64, n)
	for i := range c {
		prev := mainLine[i]
		if i >= 1 {
			prev = mainLine[i-1]
		}
		retailSeed[i] = 0.667*prev + 0.333*mainLine[i]
	}
	retailLine := waEMA(retailSeed, 2)

	x3 := make([]float64, n)
	for i := range c {
		x3[i] = (c[i] + l[i] + h[i]) / 3
	}
	x4 := waEMA(x3, 6)
	x5 := waEMA(x4, 5)

	energy := make([]float64, n)
	for i := range c {
		mid := (h[i] + l[i]) / 2
		if mid != 0 && v[i] > 0 {
			energy[i] = math.Sqrt(v[i]) * ((c[i] - mid) / mid)
		}
	}
	energySmooth := waEMA(energy, 10)
	energyInertia := waEMA(energySmooth, 10)

	midRaw := stockPercent(c, h, l, 21)
	midLong := waSMA(midRaw, 5, 1)
	midShort := waSMA(midLong, 10, 1)
	shortRaw := stockPercent(c, h, l, 10)
	shortLong := waSMA(shortRaw, 5, 1)
	shortShort := waSMA(shortLong, 5, 1)

	for i := 1; i < n; i++ {
		longForce := (2 * (sk[i] - sd[i])) * 3.8
		shortForce := (-2 * (sk[i] - sd[i])) * 3.8
		out.buyState[i] = (longForce > shortForce && ma5[i-1] <= ma5[i] && (mainLine[i] > retailLine[i] || diff[i] > dea[i])) ||
			(diff[i] > dea[i] && k[i] > d[i] && lwr1[i] > lwr2[i])
		out.trendBull[i] = x4[i] >= x5[i]
		out.energyBull[i] = energyInertia[i] > 0
		out.midBull[i] = midLong[i] > midShort[i]
		out.shortBull[i] = shortLong[i] > shortShort[i]
		out.gz[i] = out.buyState[i] && out.trendBull[i] && out.energyBull[i] && out.midBull[i] && out.shortBull[i]

		score := 24.0

		// 第一维：鱼身/开仓/宽口径点火，取最强形态，避免同源信号重复堆满分。
		firstScore := 0.0
		switch {
		case out.mainOpenFish[i]:
			firstScore = 18
		case out.eatFish[i]:
			firstScore = 14
		case out.relaxedIgnite[i]:
			firstScore = 10
		case out.recentRelaxed[i]:
			firstScore = 5
		}

		// 第二维：起爆强度、转强次数、控盘斜率，单维最高22分。
		secondScore := 0.0
		if out.strictIgnite[i] {
			secondScore += 8
		} else if out.recentStrict[i] {
			secondScore += 4
		}
		if out.strongSignal[i] {
			switch {
			case out.strongCount[i] >= 3:
				secondScore += 10
			case out.strongCount[i] == 2:
				secondScore += 7
			default:
				secondScore += 4
			}
		}
		if out.mainControlStart[i] {
			secondScore += 4
		}
		if out.mainRise[i] {
			secondScore += 3
		}
		secondScore = clampF(secondScore, 0, 22)

		// 第三维：买卖/趋势/量能/中期/短期五灯。入选多为五灯共振，因此只给稳定底分。
		redLights := countTrue(out.buyState[i], out.trendBull[i], out.energyBull[i], out.midBull[i], out.shortBull[i])
		lampScore := float64(redLights) * 3
		if out.gz[i] {
			lampScore += 5
		}
		lampScore = clampF(lampScore, 0, 20)

		// 控盘度改为连续分，而不是60/80两档跳变；80分控盘也只是加到约14分。
		kongpanScore := clampF((out.kongpanVal[i]-50)*0.45, 0, 18)

		score += firstScore + secondScore + lampScore + kongpanScore
		if out.mainControlReduce[i] {
			score -= 12
		}
		if out.timelyTakeProfit[i] {
			score -= 18
		}
		if out.breakTakeProfit[i] {
			score -= 16
		}
		if !out.energyBull[i] {
			score -= 4
		}
		if !out.trendBull[i] {
			score -= 4
		}
		out.score[i] = math.Round(clampF(score, 0, 100)*10) / 10
		out.level[i] = waveV1Level(out, i)
		out.phase[i] = waveV1Phase(out, i)

		firstDim := out.mainOpenFish[i] || out.eatFish[i] || out.relaxedIgnite[i] || out.recentRelaxed[i]
		secondDim := out.mainOpenFish[i] || out.strictIgnite[i] || out.strongSignal[i] || out.mainControlStart[i] || (out.recentStrict[i] && c[i] >= c[i-1])
		thirdDim := out.gz[i]
		out.entry[i] = !out.timelyTakeProfit[i] && !out.mainControlReduce[i] && firstDim && thirdDim &&
			(secondDim || (out.kongpanVal[i] >= 60 && (out.eatFish[i] || out.relaxedIgnite[i] || out.strictIgnite[i])))
	}
	return out
}

// legMembershipMain 复现主图"持股/持币趋势点"。主图 VAR1 只要求 C>REF(C,1)，
// 与旧锯齿版本不同，因此单独保留，避免影响已有回测。
func legMembershipMain(c []float64, up bool) []bool {
	n := len(c)
	const levels = 12
	cur := make([]bool, n)
	member := make([]bool, n)
	for i := 2; i < n; i++ {
		if up {
			cur[i] = c[i] > c[i-1]
		} else {
			cur[i] = c[i] < c[i-1] && c[i] < c[i-2]
		}
	}
	for i := range cur {
		member[i] = cur[i]
	}
	prev := cur
	for k := 2; k <= levels; k++ {
		next := make([]bool, n)
		even := k%2 == 0
		for i := 2; i < n; i++ {
			a := c[i] <= c[i-1] && c[i] >= c[i-2]
			b := c[i] >= c[i-1] && c[i] <= c[i-2]
			var cond bool
			if up {
				if even {
					cond = a
				} else {
					cond = b
				}
			} else {
				if even {
					cond = b
				} else {
					cond = a
				}
			}
			next[i] = prev[i-1] && cond
		}
		for i := range next {
			if next[i] {
				member[i] = true
			}
		}
		prev = next
	}
	return member
}

func stockPercent(c, h, l []float64, n int) []float64 {
	out := make([]float64, len(c))
	hh := waHHV(h, n)
	ll := waLLV(l, n)
	for i := range c {
		if hh[i] != ll[i] {
			out[i] = (c[i] - ll[i]) / (hh[i] - ll[i]) * 100
		}
	}
	return out
}

func negStockPercent(c, h, l []float64, n int) []float64 {
	out := make([]float64, len(c))
	hh := waHHV(h, n)
	ll := waLLV(l, n)
	for i := range c {
		if hh[i] != ll[i] {
			out[i] = -(hh[i] - c[i]) / (hh[i] - ll[i]) * 100
		}
	}
	return out
}

func recentTrue(values []bool, i, bars int) bool {
	for j := i; j >= 0 && j > i-bars; j-- {
		if values[j] {
			return true
		}
	}
	return false
}

func countTrue(values ...bool) int {
	n := 0
	for _, v := range values {
		if v {
			n++
		}
	}
	return n
}

func waveV1Level(s waveV1Signals, i int) string {
	switch {
	case s.mainOpenFish[i]:
		return "开仓吃鱼"
	case s.strongSignal[i] && s.strongCount[i] >= 3:
		return "超强"
	case s.strongSignal[i] && s.strongCount[i] == 2:
		return "主升"
	case s.strongSignal[i]:
		return "转强"
	case s.strictIgnite[i]:
		return "起爆"
	case s.eatFish[i]:
		return "吃鱼身"
	case s.relaxedIgnite[i]:
		return "异动"
	case s.mainControlStart[i]:
		return "主力拉升"
	default:
		return "观察"
	}
}

func waveV1Phase(s waveV1Signals, i int) string {
	if s.timelyTakeProfit[i] {
		return "及时止盈"
	}
	if s.mainControlReduce[i] {
		return "减仓风险"
	}
	if s.gz[i] && s.mainRise[i] {
		return "五灯共振·主升"
	}
	if s.gz[i] && s.eatFish[i] {
		return "五灯共振·鱼身"
	}
	if s.gz[i] {
		return "五灯共振"
	}
	return fmt.Sprintf("%d/5红灯", countTrue(s.buyState[i], s.trendBull[i], s.energyBull[i], s.midBull[i], s.shortBull[i]))
}

func buildWaveV1Candidate(code, name string, price float64, date string, s waveV1Signals, i int) models.WaveCandidate {
	reasons := make([]string, 0, 8)
	risks := make([]string, 0, 4)
	if s.eatFish[i] {
		reasons = append(reasons, "吃鱼身：快慢线向上且站上生命价线")
	}
	if s.mainOpenFish[i] {
		reasons = append(reasons, "主图开仓吃鱼：持币腿拐头向上，且强势/趋势持股/60日内吃鱼身同时满足")
	}
	if s.relaxedIgnite[i] {
		reasons = append(reasons, "异动现主力进：宽口径资金点火")
	} else if s.recentRelaxed[i] {
		reasons = append(reasons, "近10日出现宽口径资金点火")
	}
	if s.strictIgnite[i] {
		reasons = append(reasons, "异动起爆：强口径短线能量触发")
	} else if s.recentStrict[i] {
		reasons = append(reasons, "近10日出现强口径异动起爆")
	}
	if s.strongSignal[i] {
		switch {
		case s.strongCount[i] >= 3:
			reasons = append(reasons, "超强：起爆后第3次以上强势日")
		case s.strongCount[i] == 2:
			reasons = append(reasons, "主升：起爆后第2次强势日")
		default:
			reasons = append(reasons, "转强：起爆后首次强势日")
		}
	}
	if s.mainControlStart[i] {
		reasons = append(reasons, "主力拉升：控盘斜率上穿0")
	}
	if s.gz[i] {
		reasons = append(reasons, "买卖状态、趋势、量能、中期、短期五灯共振")
	}
	if s.kongpanVal[i] >= 80 {
		reasons = append(reasons, "高控盘度>=80")
	} else if s.kongpanVal[i] >= 60 {
		reasons = append(reasons, "中控盘度>=60")
	}
	if s.mainControlReduce[i] {
		risks = append(risks, "控盘斜率回落，出现减仓提示")
	}
	if s.timelyTakeProfit[i] {
		risks = append(risks, "主图及时止盈触发")
	}
	if s.breakTakeProfit[i] {
		risks = append(risks, "放量跌破10日线，触发破位止盈")
	}
	if !s.trendBull[i] {
		risks = append(risks, "趋势灯未转红")
	}
	if !s.energyBull[i] {
		risks = append(risks, "量能惯性未转红")
	}
	return models.WaveCandidate{
		Code: code, Name: name, Price: round2(price), Date: date,
		Kongpan: round2(s.kongpanVal[i]), Ignite: s.relaxedIgnite[i] || s.strictIgnite[i],
		Score: round2(s.score[i]), Level: s.level[i], Phase: s.phase[i],
		EatFish: s.eatFish[i], RelaxedIgnite: s.relaxedIgnite[i], StrictIgnite: s.strictIgnite[i],
		MainOpenFish: s.mainOpenFish[i], TimelyTakeProfit: s.timelyTakeProfit[i], BreakTakeProfit: s.breakTakeProfit[i],
		RecentIgnite: s.recentRelaxed[i] || s.recentStrict[i], StrongSignal: s.strongSignal[i],
		StrongCount: s.strongCount[i], MainRise: s.mainRise[i],
		MainControlStart: s.mainControlStart[i], MainControlReduce: s.mainControlReduce[i],
		BuyState: s.buyState[i], TrendBull: s.trendBull[i], EnergyBull: s.energyBull[i],
		MidBull: s.midBull[i], ShortBull: s.shortBull[i], GZ: s.gz[i],
		Reasons: reasons, Risks: risks,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ===== 吃鱼身组合回测 =====

// RunWaveBacktest 龙头吃鱼身策略组合回测（池子 300/688）。
// 进场: 短买开仓 且 (资金点火 或 高控盘)；离场: 及时止盈 或 彩带翻绿；次日开盘成交。
func (s *HistoryService) RunWaveBacktest(req models.BacktestRequest) models.BacktestResult {
	res := models.BacktestResult{ByReason: map[string]int{}, Status: "running"}
	if s == nil || s.db == nil {
		res.Status = "failed"
		res.Message = "history db not ready"
		return res
	}
	days := req.Days
	if days <= 0 || days > 520 {
		days = 500
	}
	maxPos := req.MaxPositions
	if maxPos <= 0 {
		maxPos = 5
	}
	kongpanMin := req.TakeProfitPct // 复用字段传"高控盘阈值"，<=0 默认 60
	if kongpanMin <= 0 {
		kongpanMin = 60
	}
	stopPct := req.StopLossPct     // <0 启用硬止损
	ride := req.SellRule == "ride" // 骑趋势：吃鱼身/点火进，仅彩带翻绿出
	costMul := 1.0 - req.CostPct/100

	// 1) 交易窗口
	dates, err := s.recentTradeDates(days)
	if err != nil || len(dates) < 30 {
		res.Status = "failed"
		res.Message = "交易日不足"
		return res
	}
	res.StartDate, res.EndDate, res.TradingDays = dates[0], dates[len(dates)-1], len(dates)
	winStart := dates[0]
	dateIdx := make(map[string]int, len(dates))
	for i, d := range dates {
		dateIdx[d] = i
	}

	// 大盘闸门(可选)：龙头趋势票躲系统性崩盘
	var states map[string]*marketState
	if req.GateMode == "smart" {
		states = s.loadMarketStates(winStart)
	}
	var mret3 map[string]float64
	if req.Engine == "fishv2" || req.Engine == "combo" {
		mret3 = s.loadMarketRet3()
	}

	// 2) 取 300/688 全历史(含窗口前做指标预热)，逐只算信号
	type sig struct {
		w   waveSignals
		pos map[string]int // date -> 该股序列下标
	}
	sigs := make(map[string]*sig)
	names := make(map[string]string)
	codes := s.waveUniverseCodes(req.Universe == "all")
	for _, code := range codes {
		o, hi, lo, cl, vol, amt, ds, name := s.loadWaveSeries(code)
		if len(cl) < 250 {
			continue
		}
		var ws waveSignals
		switch req.Engine {
		case "driver":
			ws = computeDriverSignals(ds, o, hi, lo, cl, vol, amt)
		case "fishv2":
			ws = computeFishV2Signals(ds, o, hi, lo, cl, vol, amt, mret3, req.SellRule == "full")
		case "combo":
			// 波段驾驶舱 = 通达信锯齿 + 吃鱼身V2骨架 结合
			wsW := computeWaveSignals(ds, o, hi, lo, cl, vol, amt, kongpanMin, false)
			wsF := computeFishV2Signals(ds, o, hi, lo, cl, vol, amt, mret3, false)
			nn := len(cl)
			ws = waveSignals{dates: ds, open: o, high: hi, low: lo, close: cl, amount: amt}
			ws.entry, ws.exit, ws.rank = make([]bool, nn), make([]bool, nn), make([]float64, nn)
			andMode := req.SellRule != "or" // 默认 AND
			for i := 0; i < nn; i++ {
				if andMode {
					ws.entry[i] = wsW.entry[i] && wsF.entry[i]
				} else {
					ws.entry[i] = wsW.entry[i] || wsF.entry[i]
				}
				ws.exit[i] = wsW.exit[i] || wsF.exit[i] // 任一离场即走
				ws.rank[i] = wsW.rank[i]                // 控盘度排序
			}
		default:
			ws = computeWaveSignals(ds, o, hi, lo, cl, vol, amt, kongpanMin, ride)
		}
		idx := make(map[string]int, len(ds))
		for i, d := range ds {
			idx[d] = i
		}
		sigs[code] = &sig{w: ws, pos: idx}
		names[code] = name
	}

	// 吃鱼身V2完整版：主线行业(每日按成员3日均涨幅取前8) + 龙头(成交额排序)
	fishFull := req.Engine == "fishv2" && req.SellRule == "full"
	industryOf := map[string]string{}
	mainline := map[string]map[string]bool{}
	if fishFull {
		industryOf = loadIndustryMap()
		for _, d := range dates {
			sum := map[string]float64{}
			cnt := map[string]int{}
			for code, sg := range sigs {
				ind := industryOf[code]
				if ind == "" {
					continue
				}
				i, ok := sg.pos[d]
				if !ok || i < 3 || sg.w.close[i-3] <= 0 {
					continue
				}
				sum[ind] += (sg.w.close[i]/sg.w.close[i-3] - 1) * 100
				cnt[ind]++
			}
			type kv struct {
				ind string
				avg float64
			}
			arr := make([]kv, 0, len(sum))
			for ind := range sum {
				if cnt[ind] >= 3 {
					arr = append(arr, kv{ind, sum[ind] / float64(cnt[ind])})
				}
			}
			sort.Slice(arr, func(a, b int) bool { return arr[a].avg > arr[b].avg })
			set := map[string]bool{}
			for k := 0; k < 8 && k < len(arr); k++ {
				set[arr[k].ind] = true
			}
			mainline[d] = set
		}
	}

	// 3) 组合模拟
	type wpos struct {
		invest, entryPrice float64
		entryDate          string
		hold               int
	}
	cash := 1.0
	positions := make(map[string]*wpos)
	var pending []string
	equity := make([]float64, len(dates))
	trades := make([]models.BacktestTrade, 0, 256)

	priceAt := func(code, date string, open bool) (float64, bool) {
		sg := sigs[code]
		if sg == nil {
			return 0, false
		}
		i, ok := sg.pos[date]
		if !ok {
			return 0, false
		}
		if open {
			return sg.w.open[i], true
		}
		return sg.w.close[i], true
	}
	markEquity := func(date string) float64 {
		e := cash
		for code, p := range positions {
			if px, ok := priceAt(code, date, false); ok && p.entryPrice > 0 {
				e += p.invest * px / p.entryPrice
			} else {
				e += p.invest
			}
		}
		return e
	}

	for di, date := range dates {
		// 入场挂单
		for _, code := range pending {
			if len(positions) >= maxPos {
				break
			}
			if _, held := positions[code]; held {
				continue
			}
			px, ok := priceAt(code, date, true)
			if !ok || px <= 0 {
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
			positions[code] = &wpos{invest: invest, entryPrice: px, entryDate: date}
		}
		pending = nil

		// 离场判定(及时止盈 或 彩带翻绿)
		for code, p := range positions {
			if p.entryDate == date {
				continue
			}
			sg := sigs[code]
			i, ok := sg.pos[date]
			if !ok {
				continue
			}
			p.hold++
			exit := ""
			px := sg.w.close[i]
			// 硬止损（盘中低点触发，优先）
			if stopPct < 0 && sg.w.low[i] > 0 && sg.w.low[i] <= p.entryPrice*(1+stopPct/100) {
				exit = "stop_loss"
				px = p.entryPrice * (1 + stopPct/100)
			} else if sg.w.exit[i] {
				exit = "signal_exit"
			}
			if exit != "" {
				cash += p.invest * px / p.entryPrice * costMul
				trades = append(trades, models.BacktestTrade{
					Code: code, Name: names[code], EntryDate: p.entryDate, EntryPrice: round2(p.entryPrice),
					ExitDate: date, ExitPrice: round2(px), HoldDays: p.hold,
					ReturnPct: round2((px/p.entryPrice*costMul - 1) * 100), ExitReason: exit, Source: "wave",
				})
				delete(positions, code)
			}
		}

		equity[di] = markEquity(date)

		// 出信号挂下一日
		gateOK := states == nil || gatePassSmart(states[date])
		if di < len(dates)-1 && len(positions) < maxPos && gateOK {
			type c2 struct {
				code string
				kp   float64
			}
			cands := make([]c2, 0, 64)
			for code, sg := range sigs {
				if _, held := positions[code]; held {
					continue
				}
				i, ok := sg.pos[date]
				if !ok || date < winStart {
					continue
				}
				if sg.w.entry[i] {
					if fishFull {
						ind := industryOf[code]
						if ind == "" || !mainline[date][ind] {
							continue // 非主线行业不做
						}
						cands = append(cands, c2{code, sg.w.amount[i]}) // 龙头：成交额优先
					} else {
						cands = append(cands, c2{code, sg.w.rank[i]})
					}
				}
			}
			sort.Slice(cands, func(a, b int) bool {
				if cands[a].kp != cands[b].kp {
					return cands[a].kp > cands[b].kp
				}
				return cands[a].code < cands[b].code
			})
			slots := maxPos - len(positions)
			for _, c := range cands {
				pending = append(pending, c.code)
				if len(pending) >= slots {
					break
				}
			}
		}
	}

	// 收尾平仓
	last := dates[len(dates)-1]
	for code, p := range positions {
		if px, ok := priceAt(code, last, false); ok {
			cash += p.invest * px / p.entryPrice * costMul
			trades = append(trades, models.BacktestTrade{
				Code: code, Name: names[code], EntryDate: p.entryDate, EntryPrice: round2(p.entryPrice),
				ExitDate: last, ExitPrice: round2(px), HoldDays: p.hold,
				ReturnPct: round2((px/p.entryPrice*costMul - 1) * 100), ExitReason: "window_end", Source: "wave",
			})
		}
	}

	s.aggregate(&res, trades, nil) // 波段用独立数据结构，暂不算等权全A基准
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
	res.Message = fmt.Sprintf("吃鱼身组合：%d笔，胜率%.1f%%，累计%.1f%%，回撤%.1f%%", res.TotalTrades, res.WinRate, res.TotalReturn, res.MaxDrawdown)
	return res
}

// loadMarketStates 从全市场截面按日聚合大盘环境(成交额/涨跌停/小盘breadth)。
func (s *HistoryService) loadMarketStates(startDate string) map[string]*marketState {
	out := make(map[string]*marketState)
	rows, err := s.db.Query(`SELECT trade_date,
		SUM(amount),
		SUM(CASE WHEN pct_change>=9.8 THEN 1 ELSE 0 END),
		SUM(CASE WHEN pct_change<=-9.8 THEN 1 ELSE 0 END),
		SUM(CASE WHEN total_market_cap>0 AND total_market_cap<=1e10 AND close_price>ma20 THEN 1 ELSE 0 END),
		SUM(CASE WHEN total_market_cap>0 AND total_market_cap<=1e10 THEN 1 ELSE 0 END)
		FROM stock_daily WHERE trade_date>=? GROUP BY trade_date`, startDate)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var amt float64
		var up, down, above, total int
		if rows.Scan(&d, &amt, &up, &down, &above, &total) != nil {
			continue
		}
		st := &marketState{amount: amt, limitUp: up, limitDown: down}
		if total > 0 {
			st.breadth = float64(above) / float64(total) * 100
		}
		out[d] = st
	}
	return out
}

// isSTOrDelisted 判定 ST/*ST/退市股(名称含 ST 或"退"),波段选股统一剔除。
func isSTOrDelisted(name string) bool {
	up := strings.ToUpper(name)
	return strings.Contains(up, "ST") || strings.Contains(name, "退")
}

// strictKeyPatternAtLast 判定"五维全红买(gz) + 均线多头(MA5>MA10>MA20)"在最后一根是否成立。
// 供「重点布局」三周期(日K/30分/60分)共振复用——同一套判定喂不同周期的K线即可。
func strictKeyPatternAtLast(ws waveV1Signals, c []float64) bool {
	n := len(c)
	if n < 240 {
		return false
	}
	i := n - 1
	if !ws.gz[i] { // gz = buyState&&trendBull&&energyBull&&midBull&&shortBull(五维擒龙全红买)
		return false
	}
	ma5 := waMA(c, 5)
	ma10 := waMA(c, 10)
	ma20 := waMA(c, 20)
	return ma5[i] > ma10[i] && ma10[i] > ma20[i] // 均线多头排列
}

// tfKeyStrict 拉某周期(30m/60m)K线,判定该周期是否"五维全红买+多头"。
// 返回 (ok=形态成立, fetched=取到足够数据)。区分两者才能"只对取数失败重试,不把没形态误判成失败"。
// asOf 非空时把K线截断到该日(含)收盘,用于历史复盘补算;行情源只带约40个交易日的分钟线,
// 截断后不足240根(约选股日早于10~20个交易日前)则 fetched=false 诚实降级不硬编。
func (s *HistoryService) tfKeyStrict(code, period, asOf string) (bool, bool) {
	if s == nil || s.marketService == nil {
		return false, false
	}
	ks, err := s.marketService.GetKLineData(code, period, 320)
	if err != nil {
		return false, false
	}
	if asOf != "" {
		cut := make([]models.KLineData, 0, len(ks))
		for _, k := range ks {
			if len(k.Time) >= 10 && k.Time[:10] <= asOf {
				cut = append(cut, k)
			}
		}
		ks = cut
	}
	if len(ks) < 240 {
		return false, false
	}
	n := len(ks)
	o := make([]float64, n)
	h := make([]float64, n)
	l := make([]float64, n)
	c := make([]float64, n)
	v := make([]float64, n)
	amt := make([]float64, n)
	ds := make([]string, n)
	for i, k := range ks {
		o[i], h[i], l[i], c[i] = k.Open, k.High, k.Low, k.Close
		v[i] = float64(k.Volume)
		amt[i] = k.Amount
		ds[i] = k.Time
	}
	ws := computeWaveV1Signals(ds, o, h, l, c, v, amt)
	return strictKeyPatternAtLast(ws, c), true
}

// tfKeyStrictTimeout 给单次周期确认加硬超时:行情源半死(TDX 断连要等 30s+ 才报错)时,
// 没有 per-call 上限会把整个扫描拖到分钟级。超时视为取数失败 (false,false)。
func (s *HistoryService) tfKeyStrictTimeout(code, period, asOf string, budget time.Duration) (bool, bool) {
	type res struct{ ok, fetched bool }
	done := make(chan res, 1)
	go func() {
		ok, fetched := s.tfKeyStrict(code, period, asOf)
		done <- res{ok, fetched}
	}()
	select {
	case r := <-done:
		return r.ok, r.fetched
	case <-time.After(budget):
		return false, false
	}
}

// tfKeyStrictRetry 只对"取数失败"重试一次(形态不成立不重试),消除行情源瞬时抖动带来的随机漏判。
func (s *HistoryService) tfKeyStrictRetry(code, period, asOf string, budget time.Duration) bool {
	ok, fetched := s.tfKeyStrictTimeout(code, period, asOf, budget)
	if fetched {
		return ok
	}
	time.Sleep(800 * time.Millisecond)
	ok, _ = s.tfKeyStrictTimeout(code, period, asOf, budget)
	return ok
}

// confirmKeyLayout 对日线严格命中者拉 30/60 分确认三周期共振。返回(30与60都过, 已确认周期列表)。
// 30/60 分并行取且各限 12s;取数失败自动重试一次,行情源慢/断连时单只最多等 ~25s 而不是无限。
func (s *HistoryService) confirmKeyLayout(code string) (bool, []string) {
	var ok30, ok60 bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); ok30 = s.tfKeyStrictRetry(code, "30m", "", 12*time.Second) }()
	go func() { defer wg.Done(); ok60 = s.tfKeyStrictRetry(code, "60m", "", 12*time.Second) }()
	wg.Wait()
	tfs := []string{"日K"}
	if ok30 {
		tfs = append(tfs, "30分")
	}
	if ok60 {
		tfs = append(tfs, "60分")
	}
	return ok30 && ok60, tfs
}

// BackfillKeyLayoutMarks 历史复盘补算「重点布局」:旧留痕没带★标时,用"截至选股日"的数据重判并打标。
// 日K从本地历史截断;30/60分从行情源取最近320根截到选股日(源约带40个交易日分钟线,更早的补不了,跳过)。
// 就地修改 picks 的 Triggers。返回补上的数量。
func (s *HistoryService) BackfillKeyLayoutMarks(signalDate string, picks []models.StrategyPickSnapshot) int {
	if s == nil || len(picks) == 0 {
		return 0
	}
	for i := range picks { // 已带标(新扫描存的)就不用补
		for _, t := range picks[i].Triggers {
			if strings.Contains(t, "重点布局") {
				return 0
			}
		}
	}
	added := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	deadline := time.Now().Add(40 * time.Second)
	for i := range picks {
		if time.Now().After(deadline) {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if time.Now().After(deadline) {
				return
			}
			code := strings.ToLower(strings.TrimSpace(picks[i].Symbol))
			o, h, l, c, v, amt, ds, _ := s.loadWaveSeriesRecentUntil(code, signalDate, waveScanHistoryBars)
			n := len(c)
			if n < 240 || ds[n-1] != signalDate {
				return
			}
			ws := computeWaveV1Signals(ds, o, h, l, c, v, amt)
			if !strictKeyPatternAtLast(ws, c) {
				return
			}
			if !s.tfKeyStrictRetry(code, "30m", signalDate, 12*time.Second) {
				return
			}
			if !s.tfKeyStrictRetry(code, "60m", signalDate, 12*time.Second) {
				return
			}
			mu.Lock()
			picks[i].Triggers = append([]string{"★重点布局(补算)"}, picks[i].Triggers...)
			added++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return added
}

// ScanWaveCandidates 实盘波段策略1.0扫描：三段通达信公式合并，独立于低吸策略。
func (s *HistoryService) ScanWaveCandidates(topN int, useGate bool) models.WaveScanResult {
	res := models.WaveScanResult{
		GatePassed:  true,
		PreheatDays: waveScanPreheatDays,
		DataSource:  "本地历史库",
	}
	if s == nil || s.db == nil {
		res.Message = "history db not ready"
		return res
	}
	if topN <= 0 {
		topN = 10
	}

	latestLocal, latestErr := s.latestCompleteTradeDate(1000)
	var snapshots []ScanSnapshotRow
	snapshotMap := map[string]ScanSnapshotRow{}
	if s.marketService != nil {
		// 取全A快照加 90 秒硬超时:行情源半死时这一步会无限期挂住(东财不通→回退腾讯 87 批),
		// 而扫描锁一直被持有,前端只看到"扫描中"永不结束、再点又提示"已有一次正在进行"(僵尸锁)。
		// 超时就降级为纯本地历史库扫描(数据口径会在结果里注明),保证扫描总能返回。
		tSnap := time.Now()
		type snapRes struct {
			rows []ScanSnapshotRow
			err  error
		}
		ch := make(chan snapRes, 1)
		go func() {
			rows, err := s.marketService.GetAllAStockSnapshot(false)
			ch <- snapRes{rows, err}
		}()
		select {
		case r := <-ch:
			if r.err == nil && len(r.rows) > 0 {
				snapshots = r.rows
				snapshotMap = buildWaveSnapshotMap(r.rows)
				res.SnapshotAsOf = waveSnapshotAsOf(r.rows)
				res.AsOf = res.SnapshotAsOf
				res.DataSource = "实时全A快照 + 最近日K + 本地历史库"
			} else if r.err != nil {
				historyLog.Warn("波段扫描: 全A快照失败(%.1fs),降级纯本地库: %v", time.Since(tSnap).Seconds(), r.err)
			}
			historyLog.Info("波段扫描: 取全A快照 %.1fs (%d只)", time.Since(tSnap).Seconds(), len(snapshots))
		case <-time.After(90 * time.Second):
			historyLog.Warn("波段扫描: 全A快照超时90s,降级为纯本地历史库扫描(行情源异常)")
			res.DataSource = "本地历史库(实时快照超时,降级)"
		}
	}
	if latestErr != nil || latestLocal == "" {
		if len(snapshotMap) == 0 {
			res.Message = "无历史数据"
			return res
		}
	}
	if res.AsOf == "" {
		res.AsOf = latestLocal
	}

	if useGate {
		if len(snapshots) > 0 {
			res.GatePassed = gatePassSmart(waveMarketStateFromSnapshots(snapshots))
		} else {
			st := s.loadMarketStates(latestLocal)
			res.GatePassed = gatePassSmart(st[latestLocal])
		}
		if !res.GatePassed {
			res.Message = fmt.Sprintf("大盘闸门未通过，波段暂不出票；数据口：%s，%d日预热", res.DataSource, res.PreheatDays)
			return res
		}
	}

	codes := make([]string, 0, len(snapshotMap))
	if len(snapshotMap) > 0 {
		for _, row := range snapshots {
			if row.Symbol == "" || row.Price <= 0 {
				continue
			}
			codes = append(codes, strings.ToLower(row.Symbol))
		}
		codes = dedupWaveCodes(codes)
	} else {
		codes = s.waveUniverseCodes(true)
	}
	res.UniverseCount = len(codes)
	tScan0 := time.Now()
	historyLog.Info("波段扫描: 全市场 %d 只,开始逐只预热(每只 %d 根)…", len(codes), waveScanHistoryBars)

	type kc struct {
		c           models.WaveCandidate
		dailyStrict bool // 日线已"五维全红买+多头",够格去拉30/60分确认重点布局
	}
	var hits []kc
	// 全市场逐只预热(每只一次日线库查询 + 240日指标计算)。这里是整个扫描最重的一段:
	// 5000+ 只串行要跑十几分钟(实测),而后面的复核/重点布局都有并行+预算保护,唯独这里没有。
	// 改 worker 池并发:SQL 是 IO 等待、指标是纯计算,并发能把两者重叠起来。
	// 固定 worker 数(而非每只一个 goroutine)是为了压住内存——历史上这个扫描 OOM 过。
	// 结果顺序不受并发影响:hits 后面按 (分数, 代码) 排序,有确定的 tie-break。
	{
		var (
			mu       sync.Mutex
			scanned  int64
			patched  int64
			codesCh  = make(chan string, len(codes))
			workerWG sync.WaitGroup
		)
		for _, code := range codes {
			codesCh <- code
		}
		close(codesCh)
		workers := runtime.NumCPU()
		if workers > 6 {
			workers = 6 // NAS 是低功耗 4 核,再高只会互相抢
		}
		if workers < 2 {
			workers = 2
		}
		for w := 0; w < workers; w++ {
			workerWG.Add(1)
			go func() {
				defer workerWG.Done()
				for code := range codesCh {
					o, h, l, c, v, amt, ds, name := s.loadWaveSeriesRecent(code, waveScanHistoryBars)
					if len(c) == 0 {
						continue
					}
					if row, ok := snapshotMap[strings.ToLower(code)]; ok {
						var isPatched bool
						o, h, l, c, v, amt, ds, name, isPatched = mergeWaveSeriesWithSnapshot(o, h, l, c, v, amt, ds, name, row, res.AsOf)
						if isPatched {
							atomic.AddInt64(&patched, 1)
						}
					}
					if isSTOrDelisted(name) {
						continue // 剔除 ST/*ST/退市:风险票不进波段候选
					}
					if len(c) < waveScanPreheatDays {
						continue
					}
					li := len(c) - 1
					if len(snapshotMap) == 0 && ds[li] != latestLocal {
						continue // 本地模式下仍要求该股最新数据是完整交易日
					}
					if len(snapshotMap) > 0 && res.AsOf != "" && ds[li] < res.AsOf {
						continue
					}
					atomic.AddInt64(&scanned, 1)
					ws := computeWaveV1Signals(ds, o, h, l, c, v, amt)
					if ws.entry[li] {
						cand := kc{c: buildWaveV1Candidate(code, name, c[li], ds[li], ws, li), dailyStrict: strictKeyPatternAtLast(ws, c)}
						mu.Lock()
						hits = append(hits, cand)
						mu.Unlock()
					}
				}
			}()
		}
		workerWG.Wait()
		res.ScannedCount += int(scanned)
		res.PatchedCount += int(patched)
		historyLog.Info("波段扫描: 预热并发 %d 路", workers)
	}

	historyLog.Info("波段扫描: 预热+选股完成 耗时%.1fs (扫描%d只/命中%d只),进入日K复核…",
		time.Since(tScan0).Seconds(), res.ScannedCount, len(hits))
	tRefine0 := time.Now()

	// 最近日K复核:行情源前复权全字段是权威口径(本地 stock_daily 不复权+缺开高低,除权股会失真)。
	// 取数失败重试2次;仍失败的票【剔除】而非带着可疑的本地口径混进榜——这是"每次扫描结果不一致"的根源:
	// 源抖动时每次成功复核的票集合不同,榜单在两个口径间随机混合。宁缺毋混,复核通过的集合本身是确定的。
	uncheckedDropped := 0
	vetoCount := 0
	if len(hits) > 0 && s.marketService != nil {
		type refineOut struct {
			hit     kc
			checked bool
			veto    bool
		}
		// 并行(6路)+120秒总预算:行情源半死时串行重试会把整个扫描拖到十几分钟,
		// 甚至因内存堆积被 OOM。预算耗尽的票按"未复核"处理(剔除,宁缺毋混)。
		outs := make([]refineOut, len(hits))
		refineDeadline := time.Now().Add(120 * time.Second)
		var rwg sync.WaitGroup
		rsem := make(chan struct{}, 6)
		for i := range hits {
			outs[i] = refineOut{hit: hits[i]}
			rwg.Add(1)
			go func(i int) {
				defer rwg.Done()
				rsem <- struct{}{}
				defer func() { <-rsem }()
				if time.Now().After(refineDeadline) {
					return
				}
				var cand models.WaveCandidate
				var checked, veto bool
				for attempt := 0; attempt < 3; attempt++ {
					cand, checked, veto = s.refineWaveCandidateWithRecentKLine(outs[i].hit.c)
					if checked || time.Now().After(refineDeadline) {
						break
					}
					time.Sleep(time.Duration(attempt+1) * 600 * time.Millisecond)
				}
				if checked {
					outs[i].hit.c = cand
				}
				outs[i].checked = checked
				outs[i].veto = veto
			}(i)
		}
		rwg.Wait()
		for i := range outs {
			if outs[i].checked {
				res.RecentKCount++
			}
		}
		allUnchecked := true
		for _, o := range outs {
			if o.checked {
				allUnchecked = false
				break
			}
		}
		refined := hits[:0]
		for _, o := range outs {
			if o.checked && o.veto {
				vetoCount++
				continue // 源口径否决(除权失真等),踢出
			}
			if !o.checked {
				if !allUnchecked {
					uncheckedDropped++
					continue // 个别取数失败:剔除,不拿可疑本地口径混榜
				}
				// 行情源整体不可用:全部保留并在消息里明确警示,好过整榜消失
			}
			refined = append(refined, o.hit)
		}
		hits = refined
		if allUnchecked && len(hits) > 0 {
			res.DataSource += "(⚠️行情源整体不可用,本榜未经复核,本地口径除权股可能失真,仅供参考)"
		}
	}
	// 评分优先，其次阶段强弱、控盘度。
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].c.Score != hits[j].c.Score {
			return hits[i].c.Score > hits[j].c.Score
		}
		if hits[i].c.StrongCount != hits[j].c.StrongCount {
			return hits[i].c.StrongCount > hits[j].c.StrongCount
		}
		if hits[i].c.GZ != hits[j].c.GZ {
			return hits[i].c.GZ
		}
		if hits[i].c.Kongpan != hits[j].c.Kongpan {
			return hits[i].c.Kongpan > hits[j].c.Kongpan
		}
		return hits[i].c.Code < hits[j].c.Code
	})

	historyLog.Info("波段扫描: 日K复核完成 耗时%.1fs (校验%d只/剔除%d只/否决%d只),进入重点布局确认…",
		time.Since(tRefine0).Seconds(), res.RecentKCount, uncheckedDropped, vetoCount)
	tKey0 := time.Now()

	// 重点布局:仅对日线严格命中者(五维全红买+多头)拉30/60分确认三周期共振,命中则置顶。
	// 并行(6路)+45秒总预算:盘中主行情源断连时逐只串行会卡死整个扫描(每只都等TDX失败再走慢新浪),
	// 预算耗尽就放弃剩余确认(诚实降级:未确认的不打重点布局标),保证扫描总能按时返回。
	keyCount := 0
	if s.marketService != nil {
		const maxKeyProbe = 24
		deadline := time.Now().Add(45 * time.Second)
		idxs := make([]int, 0, maxKeyProbe)
		for i := range hits {
			if hits[i].dailyStrict && len(idxs) < maxKeyProbe {
				idxs = append(idxs, i)
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		for _, i := range idxs {
			if time.Now().After(deadline) {
				log.Warn("重点布局确认超预算,放弃剩余候选(行情源慢)")
				break
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if time.Now().After(deadline) {
					return
				}
				if ok, tfs := s.confirmKeyLayout(hits[i].c.Code); ok {
					mu.Lock()
					hits[i].c.KeyLayout = true
					hits[i].c.KeyLayoutTF = tfs
					hits[i].c.Reasons = append(hits[i].c.Reasons, "日K+30分+60分三周期五维全红买+多头共振")
					keyCount++
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
		if keyCount > 0 {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].c.KeyLayout && !hits[j].c.KeyLayout })
		}
	}

	for i := 0; i < topN && i < len(hits); i++ {
		res.Items = append(res.Items, hits[i].c)
	}
	res.Count = len(res.Items)
	droppedNote := ""
	if uncheckedDropped > 0 {
		droppedNote = fmt.Sprintf("，另有%d只因行情源取数失败未复核已剔除(宁缺毋混)", uncheckedDropped)
	}
	if vetoCount > 0 {
		droppedNote += fmt.Sprintf("，复核否决%d只", vetoCount) // 治本(前复权对齐)后应≈0,持续>10说明本地口径又漂了
	}
	historyLog.Info("波段扫描: 重点布局确认完成 耗时%.1fs(%d只)。全程总耗时 %.1fs", time.Since(tKey0).Seconds(), keyCount, time.Since(tScan0).Seconds())
	res.Message = fmt.Sprintf("波段策略1.0扫描完成，命中 %d 只（重点布局%d只）%s；数据口：%s，%d日预热，快照补丁%d只，最近日K校验%d只；三维为吃鱼身/异动点火、短线能量、五灯共振", len(hits), keyCount, droppedNote, res.DataSource, res.PreheatDays, res.PatchedCount, res.RecentKCount)
	return res
}

// ScanWaveCandidatesOnDate 用本地 stock_daily 在指定交易日重算波段候选。
// 主要用于次日复盘缺少扫描留痕时兜底补算，不接入实时快照，也不做最近日K二次否决。
func (s *HistoryService) ScanWaveCandidatesOnDate(date string, topN int, useGate bool) models.WaveScanResult {
	date = normalizeReviewDate(date, "")
	res := models.WaveScanResult{
		AsOf:        date,
		GatePassed:  true,
		PreheatDays: waveScanPreheatDays,
		DataSource:  "本地历史库指定日补算",
	}
	if s == nil || s.db == nil {
		res.Message = "history db not ready"
		return res
	}
	if date == "" {
		res.Message = "未指定补算交易日"
		return res
	}
	if topN <= 0 {
		topN = 10
	}

	if useGate {
		states := s.loadMarketStates(date)
		res.GatePassed = gatePassSmart(states[date])
		if !res.GatePassed {
			res.Message = fmt.Sprintf("大盘闸门未通过，波段暂不出票；补算日：%s", date)
			return res
		}
	}

	codes := s.waveUniverseCodes(true)
	res.UniverseCount = len(codes)

	type kc struct {
		c models.WaveCandidate
	}
	var hits []kc
	for _, code := range codes {
		o, h, l, c, v, amt, ds, name := s.loadWaveSeriesRecentUntil(code, date, waveScanHistoryBars)
		if isSTOrDelisted(name) {
			continue // 剔除 ST/*ST/退市,与实盘扫描口径一致
		}
		if len(c) < waveScanPreheatDays {
			continue
		}
		li := len(c) - 1
		if ds[li] != date {
			continue
		}
		res.ScannedCount++
		ws := computeWaveV1Signals(ds, o, h, l, c, v, amt)
		if ws.entry[li] {
			hits = append(hits, kc{buildWaveV1Candidate(code, name, c[li], ds[li], ws, li)})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].c.Score != hits[j].c.Score {
			return hits[i].c.Score > hits[j].c.Score
		}
		if hits[i].c.StrongCount != hits[j].c.StrongCount {
			return hits[i].c.StrongCount > hits[j].c.StrongCount
		}
		if hits[i].c.GZ != hits[j].c.GZ {
			return hits[i].c.GZ
		}
		if hits[i].c.Kongpan != hits[j].c.Kongpan {
			return hits[i].c.Kongpan > hits[j].c.Kongpan
		}
		return hits[i].c.Code < hits[j].c.Code
	})
	for i := 0; i < topN && i < len(hits); i++ {
		res.Items = append(res.Items, hits[i].c)
	}
	res.Count = len(res.Items)
	res.Message = fmt.Sprintf("波段策略1.0指定日补算完成，命中 %d 只；补算日：%s，数据口：%s", len(hits), date, res.DataSource)
	return res
}

func buildWaveSnapshotMap(rows []ScanSnapshotRow) map[string]ScanSnapshotRow {
	out := make(map[string]ScanSnapshotRow, len(rows))
	for _, row := range rows {
		code := strings.ToLower(strings.TrimSpace(row.Symbol))
		if code == "" || row.Price <= 0 {
			continue
		}
		out[code] = row
	}
	return out
}

func dedupWaveCodes(codes []string) []string {
	seen := make(map[string]bool, len(codes))
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func waveSnapshotAsOf(rows []ScanSnapshotRow) string {
	latest := ""
	for _, row := range rows {
		d := waveDateFromText(row.UpdateTime)
		if d > latest {
			latest = d
		}
	}
	if latest == "" {
		latest = waveTodayDate()
	}
	return latest
}

func waveDateFromText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
		return text[:10]
	}
	if len(text) >= 8 && isDigits(text[:8]) {
		return text[:4] + "-" + text[4:6] + "-" + text[6:8]
	}
	return ""
}

func waveTodayDate() string {
	loc := time.FixedZone("CST", 8*60*60)
	return time.Now().In(loc).Format("2006-01-02")
}

func waveMarketStateFromSnapshots(rows []ScanSnapshotRow) *marketState {
	st := &marketState{}
	for _, row := range rows {
		if row.Price <= 0 {
			continue
		}
		if isFinitePositive(row.Amount) {
			st.amount += row.Amount
		}
		if row.ChangePercent >= 9.8 {
			st.limitUp++
		} else if row.ChangePercent <= -9.8 {
			st.limitDown++
		}
	}
	return st
}

func mergeWaveSeriesWithSnapshot(o, h, l, c, v, amount []float64, dates []string, name string, row ScanSnapshotRow, asOf string) ([]float64, []float64, []float64, []float64, []float64, []float64, []string, string, bool) {
	price := row.Price
	if price <= 0 {
		return o, h, l, c, v, amount, dates, name, false
	}
	patchDate := asOf
	if patchDate == "" {
		patchDate = waveDateFromText(row.UpdateTime)
	}
	if patchDate == "" {
		patchDate = waveTodayDate()
	}
	if row.Name != "" {
		name = row.Name
	}

	amt := 0.0
	if isFinitePositive(row.Amount) {
		amt = row.Amount
	}
	vol := waveSnapshotVolume(row)

	if len(c) > 0 {
		li := len(c) - 1
		if dates[li] > patchDate {
			return o, h, l, c, v, amount, dates, name, false
		}
		if dates[li] == patchDate {
			op := orVal(o[li], price)
			hp := math.Max(orVal(h[li], price), price)
			lp := math.Min(orVal(l[li], price), price)
			o[li], h[li], l[li], c[li] = op, math.Max(hp, op), math.Min(lp, op), price
			if amt > 0 {
				amount[li] = amt
			}
			if vol > 0 {
				v[li] = vol
			}
			return o, h, l, c, v, amount, dates, name, true
		}
	}

	op := price
	if implied := wavePrevCloseFromSnapshot(row); implied > 0 {
		op = implied
	} else if len(c) > 0 && c[len(c)-1] > 0 {
		op = c[len(c)-1]
	}
	hp := math.Max(op, price)
	lp := math.Min(op, price)
	o = append(o, op)
	h = append(h, hp)
	l = append(l, lp)
	c = append(c, price)
	v = append(v, vol)
	amount = append(amount, amt)
	dates = append(dates, patchDate)
	return o, h, l, c, v, amount, dates, name, true
}

func waveSnapshotVolume(row ScanSnapshotRow) float64 {
	if isFinitePositive(row.Amount) && row.Price > 0 {
		return row.Amount / row.Price
	}
	return 0
}

func wavePrevCloseFromSnapshot(row ScanSnapshotRow) float64 {
	if row.Price <= 0 || !isFinite(row.ChangePercent) {
		return 0
	}
	base := 1 + row.ChangePercent/100
	if base <= 0 {
		return 0
	}
	return row.Price / base
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func isFinitePositive(v float64) bool {
	return v > 0 && isFinite(v)
}

func (s *HistoryService) refineWaveCandidateWithRecentKLine(cand models.WaveCandidate) (models.WaveCandidate, bool, bool) {
	if s == nil || s.marketService == nil || cand.Code == "" {
		return cand, false, false
	}
	klines, err := s.marketService.GetKLineData(cand.Code, "1d", waveRecentKDays)
	if err != nil || len(klines) < waveScanPreheatDays {
		return cand, false, false
	}
	o, h, l, c, v, amt, ds := waveSeriesFromKLines(klines)
	if len(c) < waveScanPreheatDays {
		return cand, false, false
	}
	li := len(c) - 1
	if cand.Date != "" && ds[li] < cand.Date {
		return cand, false, false
	}
	ws := computeWaveV1Signals(ds, o, h, l, c, v, amt)
	if !ws.entry[li] {
		return cand, true, true
	}
	refined := buildWaveV1Candidate(cand.Code, cand.Name, c[li], ds[li], ws, li)
	if refined.Name == "" {
		refined.Name = cand.Name
	}
	return refined, true, false
}

func waveSeriesFromKLines(klines []models.KLineData) (o, h, l, c, v, amount []float64, dates []string) {
	type row struct {
		date                           string
		open, high, low, close, amount float64
		volume                         float64
	}
	rows := make([]row, 0, len(klines))
	for _, k := range klines {
		d := waveDateFromText(k.Time)
		if d == "" || k.Close <= 0 {
			continue
		}
		rows = append(rows, row{
			date:   d,
			open:   orVal(k.Open, k.Close),
			high:   orVal(k.High, k.Close),
			low:    orVal(k.Low, k.Close),
			close:  k.Close,
			amount: k.Amount,
			volume: float64(k.Volume),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].date < rows[j].date })
	for _, r := range rows {
		dates = append(dates, r.date)
		o = append(o, r.open)
		h = append(h, math.Max(r.high, r.close))
		l = append(l, math.Min(r.low, r.close))
		c = append(c, r.close)
		v = append(v, r.volume)
		amount = append(amount, r.amount)
	}
	return
}

// loadMarketRet3 全市场每日平均涨幅的3日累计(作"大盘"基准)。
func (s *HistoryService) loadMarketRet3() map[string]float64 {
	rows, err := s.db.Query(`SELECT trade_date, AVG(pct_change) FROM stock_daily WHERE pct_change IS NOT NULL GROUP BY trade_date ORDER BY trade_date`)
	if err != nil {
		return map[string]float64{}
	}
	defer rows.Close()
	type dp struct {
		d   string
		avg float64
	}
	var seq []dp
	for rows.Next() {
		var d string
		var a float64
		if rows.Scan(&d, &a) == nil {
			seq = append(seq, dp{d, a})
		}
	}
	out := make(map[string]float64, len(seq))
	for i := range seq {
		s3 := seq[i].avg
		if i >= 1 {
			s3 += seq[i-1].avg
		}
		if i >= 2 {
			s3 += seq[i-2].avg
		}
		out[seq[i].d] = s3
	}
	return out
}

// waveUniverseCodes 取吃鱼身池子代码（all=全A，否则 300/301/688/689）。
// WaveResonance 波段鱼身引擎在"最后一根K"上的共振快照(供其他策略复用同源信号,如超跌起爆11)。
type WaveResonance struct {
	EatFish       bool    // 吃鱼身
	MainOpenFish  bool    // 开仓吃鱼(主图短买)
	StrictIgnite  bool    // 异动点火·强口径
	RelaxedIgnite bool    // 异动点火·宽口径
	MainRise      bool    // 主力拉升(控盘斜率上穿0)
	StrongCount   int     // 强势次数(1转强/2主升/≥3超强)
	Lamps         int     // 五灯红数(买/趋势/量能/中期/短期)
	AllLamps      bool    // 五灯全红
	Kongpan       float64 // 控盘度
	Level         string  // 引擎分级文案
}

// WaveResonanceAtLast 用与波段1.0完全同一套 computeWaveV1Signals 计算序列,取最后一根的共振状态。
func WaveResonanceAtLast(dates []string, o, h, l, c, v, amount []float64) WaveResonance {
	ws := computeWaveV1Signals(dates, o, h, l, c, v, amount)
	li := len(c) - 1
	res := WaveResonance{}
	if li < 0 || len(ws.eatFish) <= li {
		return res
	}
	res.EatFish = ws.eatFish[li]
	res.MainOpenFish = ws.mainOpenFish[li]
	res.StrictIgnite = ws.strictIgnite[li]
	res.RelaxedIgnite = ws.relaxedIgnite[li]
	res.MainRise = ws.mainRise[li]
	res.StrongCount = ws.strongCount[li]
	lamps := 0
	for _, b := range []bool{ws.buyState[li], ws.trendBull[li], ws.energyBull[li], ws.midBull[li], ws.shortBull[li]} {
		if b {
			lamps++
		}
	}
	res.Lamps = lamps
	res.AllLamps = lamps == 5
	res.Kongpan = ws.kongpanVal[li]
	if len(ws.level) > li {
		res.Level = ws.level[li]
	}
	return res
}

// AllUniverseCodes 本地历史库全部股票代码(供策略历史重算遍历,与波段补算同一取数口)。
func (s *HistoryService) AllUniverseCodes() []string { return s.waveUniverseCodes(true) }

// SeriesRecentUntil 某股截至 endDate 的最近 limit 根日K(升序,前复权口径),返回开高低收/量/额/日期/名称。
func (s *HistoryService) SeriesRecentUntil(code string, endDate string, limit int) (o, h, l, c, v, amount []float64, dates []string, name string) {
	return s.loadWaveSeriesRecentUntil(code, endDate, limit)
}

// IsSTOrDelistedName 名称是否 ST/退市(导出供 app 层重算过滤,与波段口径一致)。
func IsSTOrDelistedName(name string) bool { return isSTOrDelisted(name) }

func (s *HistoryService) waveUniverseCodes(all bool) []string {
	q := `SELECT DISTINCT stock_code FROM stock_daily WHERE substr(stock_code,3,3) IN ('300','301','688','689')`
	if all {
		q = `SELECT DISTINCT stock_code FROM stock_daily`
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]string, 0, 2000)
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil && c != "" {
			out = append(out, c)
		}
	}
	return out
}

// loadWaveSeriesRecent 取最近 N 根 OHLCV+额（升序），保留足够预热但避免全A扫描加载多年历史。
func (s *HistoryService) loadWaveSeriesRecent(code string, limit int) (o, h, l, c, v, amount []float64, dates []string, name string) {
	if limit <= 0 {
		return s.loadWaveSeries(code)
	}
	rows, err := s.db.Query(`SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, stock_name
		FROM (
			SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, stock_name
			FROM stock_daily
			WHERE stock_code=? AND close_price>0
			ORDER BY trade_date DESC
			LIMIT ?
		) ORDER BY trade_date`, code, limit)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var op, hp, lp, cp, vol, amt, nameNull interface{}
		if err := rows.Scan(&d, &op, &hp, &lp, &cp, &vol, &amt, &nameNull); err != nil {
			continue
		}
		cf := toF(cp)
		if cf <= 0 {
			continue
		}
		dates = append(dates, d)
		o = append(o, orVal(toF(op), cf))
		h = append(h, orVal(toF(hp), cf))
		l = append(l, orVal(toF(lp), cf))
		c = append(c, cf)
		v = append(v, toF(vol))
		amount = append(amount, toF(amt))
		if ns, ok := nameNull.(string); ok && ns != "" {
			name = ns
		}
	}
	return
}

func (s *HistoryService) loadWaveSeriesRecentUntil(code string, endDate string, limit int) (o, h, l, c, v, amount []float64, dates []string, name string) {
	if limit <= 0 {
		limit = waveScanHistoryBars
	}
	rows, err := s.db.Query(`SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, stock_name
		FROM (
			SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, stock_name
			FROM stock_daily
			WHERE stock_code=? AND trade_date<=? AND close_price>0
			ORDER BY trade_date DESC
			LIMIT ?
		) ORDER BY trade_date`, code, endDate, limit)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var op, hp, lp, cp, vol, amt, nameNull interface{}
		if err := rows.Scan(&d, &op, &hp, &lp, &cp, &vol, &amt, &nameNull); err != nil {
			continue
		}
		cf := toF(cp)
		if cf <= 0 {
			continue
		}
		dates = append(dates, d)
		o = append(o, orVal(toF(op), cf))
		h = append(h, orVal(toF(hp), cf))
		l = append(l, orVal(toF(lp), cf))
		c = append(c, cf)
		v = append(v, toF(vol))
		amount = append(amount, toF(amt))
		if ns, ok := nameNull.(string); ok && ns != "" {
			name = ns
		}
	}
	return
}

// loadWaveSeries 取单只全历史 OHLCV+额（升序）。
func (s *HistoryService) loadWaveSeries(code string) (o, h, l, c, v, amount []float64, dates []string, name string) {
	rows, err := s.db.Query(`SELECT trade_date, open_price, high_price, low_price, close_price, volume, amount, stock_name
		FROM stock_daily WHERE stock_code=? AND close_price>0 ORDER BY trade_date`, code)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var op, hp, lp, cp, vol, amt, nameNull interface{}
		if err := rows.Scan(&d, &op, &hp, &lp, &cp, &vol, &amt, &nameNull); err != nil {
			continue
		}
		cf := toF(cp)
		if cf <= 0 {
			continue
		}
		dates = append(dates, d)
		o = append(o, orVal(toF(op), cf))
		h = append(h, orVal(toF(hp), cf))
		l = append(l, orVal(toF(lp), cf))
		c = append(c, cf)
		v = append(v, toF(vol))
		amount = append(amount, toF(amt))
		if ns, ok := nameNull.(string); ok && ns != "" {
			name = ns
		}
	}
	return
}

func toF(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	}
	return 0
}

func orVal(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

// ===== 波段执行时点对比回测(2026-07-20 用户要求) =====
// 对比两种执行方案(信号集合完全相同,只比买卖时点):
//   A「2:30扫描当天买」: 信号日D 以收盘价买入(近似14:30实价,见诚实声明) → D+1 开盘卖出
//   B「17:30扫描次日买」: D+1 开盘价买入 → D+2 开盘卖出
// 口径与 live 波段扫描严格一致:270根窗口逐日重算 computeWaveV1Signals、同评分同排序取每日前N、
// 大盘闸门(gatePassSmart,不过=当日空仓)、剔ST。扣双边成本(PaperCostRates)。
// 诚实声明(写进结果):①历史无14:30盘中数据,A的买价用当日收盘近似,且2:30盘中信号与收盘信号
// 会有出入(尾盘半小时变化),回测统一用收盘信号——对A偏乐观;②涨停可成交过滤:A剔信号日收盘涨停
// (封死买不进),B剔D+1开盘即涨停(一字买不进);③跌停卖不出未建模。

type WaveTimingTradeStat struct {
	Trades     int     `json:"trades"`
	Wins       int     `json:"wins"`
	WinRate    float64 `json:"winRate"`
	AvgNet     float64 `json:"avgNet"`   // 单笔净收益均值%
	SumNet     float64 `json:"sumNet"`   // 逐笔净收益求和%
	Compound   float64 `json:"compound"` // 每日等权组合复利累计%
	MaxDD      float64 `json:"maxDd"`    // 组合净值最大回撤%
	WorstOne   float64 `json:"worstOne"` // 单笔最大亏%
	BestOne    float64 `json:"bestOne"`
	Skipped    int     `json:"skipped"`    // 因涨停买不进被剔的笔数
	AvgHold    float64 `json:"avgHold"`    // 平均持有交易日数
	Unresolved int     `json:"unresolved"` // 数据末端仍未触发离场的笔数(按最后收盘价截断结算)
}

type WaveTimingSchemeMonth struct {
	Month  string  `json:"month"`
	Trades int     `json:"trades"`
	Win    float64 `json:"win"`
	Avg    float64 `json:"avg"`
}

type WaveTimingScheme struct {
	Name    string                  `json:"name"`
	Stat    WaveTimingTradeStat     `json:"stat"`
	Monthly []WaveTimingSchemeMonth `json:"monthly"`
}

type WaveTimingBacktestResult struct {
	StartDate  string             `json:"startDate"`
	EndDate    string             `json:"endDate"`
	TopN       int                `json:"topN"`
	SignalDays int                `json:"signalDays"` // 有信号的交易日数
	GateDays   int                `json:"gateDays"`   // 闸门通过的交易日数
	TotalDays  int                `json:"totalDays"`
	Schemes    []WaveTimingScheme `json:"schemes"`
	Notes      []string           `json:"notes"`
	ElapsedSec float64            `json:"elapsedSec"`
}

func waveTimingLimitRatio(code string) float64 {
	c := strings.ToLower(code)
	if strings.HasPrefix(c, "sz300") || strings.HasPrefix(c, "sz301") || strings.HasPrefix(c, "sh688") || strings.HasPrefix(c, "sh689") {
		return 1.20
	}
	if strings.HasPrefix(c, "bj") {
		return 1.30
	}
	return 1.10
}

// RunWaveTimingBacktest 近 months 个月、每日前 topN 的执行时点对比回测。
func (s *HistoryService) RunWaveTimingBacktest(months, topN int) WaveTimingBacktestResult {
	t0 := time.Now()
	res := WaveTimingBacktestResult{TopN: topN}
	if s == nil || s.db == nil {
		res.Notes = append(res.Notes, "history db not ready")
		return res
	}
	if months <= 0 {
		months = 6
	}
	if topN <= 0 {
		topN = 5
	}

	// 交易日轴(全市场口径)
	var allDays []string
	rows, err := s.db.Query(`SELECT DISTINCT trade_date FROM stock_daily ORDER BY trade_date`)
	if err != nil {
		res.Notes = append(res.Notes, "交易日查询失败: "+err.Error())
		return res
	}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			allDays = append(allDays, d)
		}
	}
	rows.Close()
	if len(allDays) < waveScanPreheatDays+10 {
		res.Notes = append(res.Notes, "本地日线深度不足")
		return res
	}
	endSignalIdx := len(allDays) - 3 // B 需要 D+2,信号日最多到倒数第3个交易日
	startWant := time.Now().AddDate(0, -months, 0).Format("2006-01-02")
	startIdx := 0
	for i, d := range allDays {
		if d >= startWant {
			startIdx = i
			break
		}
	}
	if startIdx > endSignalIdx {
		startIdx = endSignalIdx
	}
	signalDays := allDays[startIdx : endSignalIdx+1]
	res.StartDate, res.EndDate = signalDays[0], signalDays[len(signalDays)-1]
	res.TotalDays = len(signalDays)

	// 大盘闸门逐日
	states := s.loadMarketStates(allDays[maxInt(0, startIdx-2)])
	gateOK := map[string]bool{}
	for _, d := range signalDays {
		if gatePassSmart(states[d]) {
			gateOK[d] = true
			res.GateDays++
		}
	}

	// 批量加载:预热270根 + 回测期
	loadStart := allDays[maxInt(0, startIdx-(waveScanHistoryBars+8))]
	series, idxMap, err := s.LoadAllKLinesAscending(loadStart, allDays[len(allDays)-1])
	if err != nil {
		res.Notes = append(res.Notes, "批量加载失败: "+err.Error())
		return res
	}
	// 股票名(剔ST用,取最新名)
	names := map[string]string{}
	if nr, err := s.db.Query(`SELECT stock_code, stock_name FROM stock_daily WHERE trade_date=?`, allDays[len(allDays)-1]); err == nil {
		for nr.Next() {
			var c, n string
			if nr.Scan(&c, &n) == nil {
				names[c] = n
			}
		}
		nr.Close()
	}

	// 逐股并行:窗口滑动逐信号日算 live 同款信号
	type hitRec struct {
		day  string
		cand models.WaveCandidate
		idx  int
		code string
	}
	var mu sync.Mutex
	byDay := map[string][]hitRec{}
	codes := make([]string, 0, len(series))
	for c := range series {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			name := names[code]
			if isSTOrDelisted(name) {
				return
			}
			arr := series[code]
			pos := idxMap[code]
			n := len(arr)
			if n < waveScanPreheatDays {
				return
			}
			// 整段转列一次,窗口用切片(零拷贝),与 live 的270根窗口逐日重算完全同口径
			o := make([]float64, n)
			h := make([]float64, n)
			l := make([]float64, n)
			c := make([]float64, n)
			v := make([]float64, n)
			amt := make([]float64, n)
			ds := make([]string, n)
			for i, k := range arr {
				o[i], h[i], l[i], c[i], v[i], amt[i], ds[i] = k.Open, k.High, k.Low, k.Close, float64(k.Volume), k.Amount, k.Time
			}
			for _, day := range signalDays {
				if !gateOK[day] {
					continue
				}
				li, ok := pos[day]
				if !ok {
					continue // 该股当日停牌/无数据
				}
				lo := li - waveScanHistoryBars + 1
				if lo < 0 {
					lo = 0
				}
				if li-lo+1 < waveScanPreheatDays {
					continue
				}
				ws := computeWaveV1Signals(ds[lo:li+1], o[lo:li+1], h[lo:li+1], l[lo:li+1], c[lo:li+1], v[lo:li+1], amt[lo:li+1])
				wi := li - lo
				if !ws.entry[wi] {
					continue
				}
				cand := buildWaveV1Candidate(code, name, c[li], day, ws, wi)
				mu.Lock()
				byDay[day] = append(byDay[day], hitRec{day: day, cand: cand, idx: li, code: code})
				mu.Unlock()
			}
		}(code)
	}
	wg.Wait()

	// 逐日取前N + 多方案结算(信号只算一遍,结算便宜)
	buyC, sellC := PaperCostRates()
	netOf := func(buy, sell float64) float64 {
		outlay := buy * (1 + buyC)
		return (sell*(1-sellC) - outlay) / outlay * 100
	}
	type acc struct {
		stat   WaveTimingTradeStat
		daily  map[string][]float64 // 信号日→当日各笔净收益(等权日收益用)
		months map[string][]float64
		holds  []int
	}
	newAcc := func() *acc { return &acc{daily: map[string][]float64{}, months: map[string][]float64{}} }
	// 六方案:A/B 各配 开盘卖(原口径) 与 冲高+3%/+5%止盈(未及目标当日收盘了结)
	schemeNames := []string{
		"A·2:30买→次日开盘卖",
		"A·2:30买→次日冲高+3%止盈(否则收盘走)",
		"A·2:30买→次日冲高+5%止盈(否则收盘走)",
		"B·次日开盘买→第三日开盘卖",
		"B·次日开盘买→第三日冲高+3%止盈(否则收盘走)",
		"B·次日开盘买→第三日冲高+5%止盈(否则收盘走)",
		"C·次日开盘买→波段纪律持有",
	}
	accs := map[string]*acc{}
	for _, nm := range schemeNames {
		accs[nm] = newAcc()
	}
	recordH := func(a *acc, day string, net float64, hold int) {
		a.stat.Trades++
		a.stat.SumNet += net
		if net > 0 {
			a.stat.Wins++
		}
		if net < a.stat.WorstOne {
			a.stat.WorstOne = net
		}
		if net > a.stat.BestOne {
			a.stat.BestOne = net
		}
		a.months[day[:7]] = append(a.months[day[:7]], net)
		a.daily[day] = append(a.daily[day], net)
		a.holds = append(a.holds, hold)
	}
	record := func(a *acc, day string, net float64) { recordH(a, day, net, 1) }
	// tpSell: 持有日bar上的止盈结算——跳空开在目标上按开盘卖(更优),盘中冲到按目标价卖(挂单成交),
	// 都没有则收盘了结(乏力就走)。日K无法分辨盘中高低先后,但本口径只有止盈单无盘中止损,无歧义。
	tpSell := func(buy float64, bar models.KLineData, tgtPct float64) float64 {
		tgt := buy * (1 + tgtPct/100)
		if bar.Open >= tgt {
			return bar.Open
		}
		if bar.High >= tgt {
			return tgt
		}
		return bar.Close
	}
	for _, day := range signalDays {
		hits := byDay[day]
		if len(hits) == 0 {
			continue
		}
		res.SignalDays++
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].cand.Score != hits[j].cand.Score {
				return hits[i].cand.Score > hits[j].cand.Score
			}
			if hits[i].cand.StrongCount != hits[j].cand.StrongCount {
				return hits[i].cand.StrongCount > hits[j].cand.StrongCount
			}
			if hits[i].cand.GZ != hits[j].cand.GZ {
				return hits[i].cand.GZ
			}
			if hits[i].cand.Kongpan != hits[j].cand.Kongpan {
				return hits[i].cand.Kongpan > hits[j].cand.Kongpan
			}
			return hits[i].cand.Code < hits[j].cand.Code
		})
		if len(hits) > topN {
			hits = hits[:topN]
		}
		for _, hrec := range hits {
			arr := series[hrec.code]
			li := hrec.idx
			if li+2 >= len(arr) {
				continue
			}
			d0, d1, d2 := arr[li], arr[li+1], arr[li+2]
			ratio := waveTimingLimitRatio(hrec.code)
			prevClose := 0.0
			if li > 0 {
				prevClose = arr[li-1].Close
			}
			// A 系:信号日收盘买(近似2:30);信号日收盘涨停=封死买不进
			limit0 := round2(prevClose * ratio)
			if prevClose > 0 && d0.Close >= limit0-0.001 {
				for _, nm := range schemeNames[:3] {
					accs[nm].stat.Skipped++
				}
			} else if d0.Close > 0 && d1.Open > 0 {
				record(accs[schemeNames[0]], day, netOf(d0.Close, d1.Open))
				record(accs[schemeNames[1]], day, netOf(d0.Close, tpSell(d0.Close, d1, 3)))
				record(accs[schemeNames[2]], day, netOf(d0.Close, tpSell(d0.Close, d1, 5)))
			}
			// B 系:D+1 开盘买;开盘即涨停(一字近似)=买不进;T+1 制度当日不能卖,持有到 D+2
			limit1 := round2(d0.Close * ratio)
			if d1.Open >= limit1-0.001 {
				for _, nm := range schemeNames[3:] {
					accs[nm].stat.Skipped++
				}
			} else if d1.Open > 0 && d2.Open > 0 {
				record(accs[schemeNames[3]], day, netOf(d1.Open, d2.Open))
				record(accs[schemeNames[4]], day, netOf(d1.Open, tpSell(d1.Open, d2, 3)))
				record(accs[schemeNames[5]], day, netOf(d1.Open, tpSell(d1.Open, d2, 5)))

				// C: D+1 开盘买,按波段成文纪律逐日推演离场(riskWave:-8%止损/+8%保本/+12%移动回8%/
				// +30%封顶/破确认前低(收盘)/10日不涨<3%清)。与 live 引擎同序判定;比引擎更诚实一处:
				// 跳空穿越止损线按当日开盘价成交(现实滑点),不是理论线价。
				cost := d1.Open
				entryIdx := li + 1
				w0 := entryIdx - 130
				if w0 < 0 {
					w0 = 0
				}
				pivots := confirmedPivotLows(arr[w0:])
				prof := riskWave
				peak := d1.High
				exited := false
				for j := entryIdx + 1; j < len(arr); j++ {
					b := arr[j]
					hold := j - entryIdx
					if b.High > peak {
						peak = b.High
					}
					line, _ := prof.RiskStopLine(cost, peak)
					sellPx, done := 0.0, false
					switch {
					case b.Open > 0 && b.Open <= line: // 跳空开在线下:按开盘成交
						sellPx, done = b.Open, true
					case b.Low > 0 && b.Low <= line: // 盘中触线:按线价成交
						sellPx, done = line, true
					}
					if !done && prof.StructStop {
						if pv := pivots[j-w0]; pv > 0 && b.Close < pv {
							sellPx, done = b.Close, true // 结构止损:收盘口径
						}
					}
					if !done {
						tp := cost * (1 + prof.TPPct/100)
						if b.Open >= tp {
							sellPx, done = b.Open, true // 跳空开在封顶上:按开盘(更优)
						} else if b.High >= tp {
							sellPx, done = tp, true
						}
					}
					if !done && prof.TimeStopDays > 0 && hold >= prof.TimeStopDays && (b.Close-cost)/cost*100 < prof.TimeStopGain {
						sellPx, done = b.Close, true
					}
					if done {
						recordH(accs[schemeNames[6]], day, netOf(cost, sellPx), hold)
						exited = true
						break
					}
				}
				if !exited { // 数据末端仍持有:按最后收盘截断结算并计数
					last := arr[len(arr)-1]
					recordH(accs[schemeNames[6]], day, netOf(cost, last.Close), len(arr)-1-entryIdx)
					accs[schemeNames[6]].stat.Unresolved++
				}
			}
		}
	}

	for _, nm := range schemeNames {
		a := accs[nm]
		st := a.stat
		if st.Trades > 0 {
			st.WinRate = round2(float64(st.Wins) * 100 / float64(st.Trades))
			st.AvgNet = round2(st.SumNet / float64(st.Trades))
			st.SumNet = round2(st.SumNet)
		}
		var dayKeys []string
		for d := range a.daily {
			dayKeys = append(dayKeys, d)
		}
		sort.Strings(dayKeys)
		nav, peak, maxdd := 1.0, 1.0, 0.0
		for _, d := range dayKeys {
			list := a.daily[d]
			sum := 0.0
			for _, x := range list {
				sum += x
			}
			nav *= 1 + (sum/float64(len(list)))/100
			if nav > peak {
				peak = nav
			}
			if dd := (nav/peak - 1) * 100; dd < maxdd {
				maxdd = dd
			}
		}
		st.Compound = round2((nav - 1) * 100)
		st.MaxDD = round2(maxdd)
		st.WorstOne = round2(st.WorstOne)
		st.BestOne = round2(st.BestOne)
		if len(a.holds) > 0 {
			sumH := 0
			for _, x := range a.holds {
				sumH += x
			}
			st.AvgHold = round2(float64(sumH) / float64(len(a.holds)))
		}
		var monthKeys []string
		for m := range a.months {
			monthKeys = append(monthKeys, m)
		}
		sort.Strings(monthKeys)
		var monthly []WaveTimingSchemeMonth
		for _, m := range monthKeys {
			list := a.months[m]
			w, sum := 0, 0.0
			for _, x := range list {
				sum += x
				if x > 0 {
					w++
				}
			}
			monthly = append(monthly, WaveTimingSchemeMonth{Month: m, Trades: len(list),
				Win: round2(float64(w) * 100 / float64(len(list))), Avg: round2(sum / float64(len(list)))})
		}
		res.Schemes = append(res.Schemes, WaveTimingScheme{Name: nm, Stat: st, Monthly: monthly})
	}
	res.Notes = append(res.Notes,
		"A系买价用信号日收盘近似2:30实价;2:30盘中信号与收盘信号有出入,统一用收盘信号——对A系偏乐观",
		"止盈口径:目标价挂单,跳空开在目标上按开盘卖,盘中冲到按目标价成交,均未及则当日收盘了结(乏力就走);无盘中止损,无高低先后歧义",
		"可成交过滤:A系剔信号日收盘涨停(封死),B系剔D+1开盘即达涨停(一字近似);跌停卖不出未建模",
		"信号口径=live波段1.0(270根窗口/240日预热/大盘闸门/剔ST/同评分排序),扣双边成本≈0.35%;B/C系遵守T+1(买入日不卖)",
		"C口径=波段成文纪律逐日推演(riskWave):跳空穿线按开盘成交(比理论线价诚实);组合日收益按信号日归组,C持有多日会有重叠持仓,复利/回撤为近似口径,单笔均值与胜率不受影响",
	)
	res.ElapsedSec = round2(time.Since(t0).Seconds())
	return res
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
