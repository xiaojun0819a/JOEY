package main

// 竞价/分时数据查询 API(数据由 IntradayService 在 NAS 上实时采集自建)。

import (
	"sort"
	"strings"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/services"
)

// GetAuctionFinal 某日竞价定型榜(9:25 撮合结果,按竞价金额降序)。date 形如 2026-07-06。
func (a *App) GetAuctionFinal(date string, limit int) []services.AuctionFinalRow {
	if a.intradayService == nil {
		return nil
	}
	rows, err := a.intradayService.AuctionFinal(date, limit)
	if err != nil {
		log.Warn("竞价定型查询失败 %s: %v", date, err)
		return nil
	}
	// 补齐"昨日成交量"(手):从日K库取 date 前一交易日全市场量。
	if a.historyService != nil {
		if prevVols, _, e := a.historyService.DailyVolumesBefore(date); e == nil {
			for i := range rows {
				if v, ok := prevVols[strings.ToLower(rows[i].StockCode)]; ok {
					rows[i].PrevVolume = v / 100 // 股→手
				}
			}
		}
	}
	return rows
}

// GetAuctionPicksG 全市场评估「集合竞价稳健版选股公式 V2.1」(G),返回命中个股(竞价额降序)。
// 因 G 的候选(温和高开)通常不在竞价额 Top100 内,必须扫全市场;用一次批量日K载入避免逐股查库。
func (a *App) GetAuctionPicksG(date string) []services.AuctionFinalRow {
	if a.intradayService == nil || a.historyService == nil {
		return nil
	}
	rows, err := a.intradayService.AuctionFinal(date, 6000) // 全市场竞价
	if err != nil {
		log.Warn("竞价选股 G 查询失败 %s: %v", date, err)
		return nil
	}
	prevVols, prevDate, e := a.historyService.DailyVolumesBefore(date)
	if e != nil || prevDate == "" {
		return nil
	}
	// 先用"竞价侧闸门"(不需日K)把全市场几千只筛到个位/几十只,再对幸存者逐股载日K评估技术面,
	// 避免每次都批量载入整市 160 日日K(~75万行/次)。
	out := make([]services.AuctionFinalRow, 0, 16)
	for _, r := range rows {
		if v, ok := prevVols[strings.ToLower(r.StockCode)]; ok {
			r.PrevVolume = v / 100
		}
		if !passesAuctionGatesG(r) {
			continue
		}
		bars, be := a.historyService.LoadKLineDataUntil(r.StockCode, prevDate, 70)
		if be != nil || len(bars) < 60 {
			continue
		}
		if matchAuctionFormulaG(r, bars) {
			r.MatchG = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out
}

// passesAuctionGatesG 只判 G 里不依赖日K的竞价侧闸门,用作全市场廉价预筛。
func passesAuctionGatesG(r services.AuctionFinalRow) bool {
	昨收 := r.Price / (1 + r.Pct/100)
	if 昨收 <= 0 || r.Price <= 0 || r.PrevVolume <= 0 {
		return false
	}
	name := r.Name
	if strings.Contains(name, "ST") || strings.Contains(name, "*") || strings.Contains(name, "退") {
		return false
	}
	竞价量 := r.Amount / r.Price / 100
	竞昨量比 := 竞价量 / r.PrevVolume * 100
	return r.Pct >= 1 && r.Pct <= 4 && // 适度高开
		r.Amount >= 15000000 && // 竞价金额达标
		竞昨量比 >= 4 && 竞昨量比 <= 15 && // 竞价放量
		r.Price >= 3 && r.Price <= 80 // 价格适中
}

// GetAuctionPicksC 全市场评估「集合竞价·温和放量高开选股 v2.0」(C),返回命中个股(竞价额降序)。
func (a *App) GetAuctionPicksC(date string) []services.AuctionFinalRow {
	if a.intradayService == nil || a.historyService == nil {
		return nil
	}
	rows, err := a.intradayService.AuctionFinal(date, 6000)
	if err != nil {
		log.Warn("竞价选股 C 查询失败 %s: %v", date, err)
		return nil
	}
	prevVols, prevDate, e := a.historyService.DailyVolumesBefore(date)
	if e != nil || prevDate == "" {
		return nil
	}
	out := make([]services.AuctionFinalRow, 0, 32)
	for _, r := range rows {
		if v, ok := prevVols[strings.ToLower(r.StockCode)]; ok {
			r.PrevVolume = v / 100
		}
		if !passesAuctionGatesC(r) {
			continue
		}
		bars, be := a.historyService.LoadKLineDataUntil(r.StockCode, prevDate, 70)
		if be != nil || len(bars) <= 60 {
			continue
		}
		if matchAuctionFormulaC(r, bars) {
			r.MatchC = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out
}

// passesAuctionGatesC 只判 C 里不依赖日K的竞价侧闸门(高开幅度/竞价额/换手/名称/代码),廉价预筛。
func passesAuctionGatesC(r services.AuctionFinalRow) bool {
	if r.Price <= 0 || r.FloatMcap <= 0 {
		return false
	}
	// 基础过滤:非 ST/*ST、剔除北交所/老三板(代码 4/8/92 开头)
	if strings.Contains(r.Name, "ST") || strings.Contains(r.Name, "*") {
		return false
	}
	num := auctionCodeNum(r.StockCode)
	if strings.HasPrefix(num, "4") || strings.HasPrefix(num, "8") || strings.HasPrefix(num, "92") {
		return false
	}
	gkf := r.Pct                        // 高开幅度% =(竞价价/昨收-1)*100
	jje := r.Amount / 10000             // 竞价额(万元)
	jhs := r.Amount / r.FloatMcap * 100 // 竞价换手率% = 竞价额/流通市值*100
	return gkf > 0.5 && gkf < 3.0 && jje > 500 && jhs > 0.15
}

// auctionCodeNum 去掉 sh/sz/bj 前缀,取纯数字代码(用于 CODELIKE 前缀判断)。
func auctionCodeNum(code string) string {
	c := strings.TrimSpace(code)
	for len(c) > 0 && (c[0] < '0' || c[0] > '9') {
		c = c[1:]
	}
	return c
}

// matchAuctionFormulaC 评估「集合竞价·温和放量高开选股 v2.0」(命名 C)。
// row: 当日竞价定型;bars: 截至前一交易日的日K(升序,末根=昨日=REF(...,1))。逐条对照用户公式。
func matchAuctionFormulaC(row services.AuctionFinalRow, bars []models.KLineData) bool {
	n := len(bars)
	if n <= 60 { // BARSCOUNT(C)>60 剔除次新(同时保证 REF(C,11)/MA20 可算)
		return false
	}
	if row.Price <= 0 || row.FloatMcap <= 0 {
		return false
	}
	num := auctionCodeNum(row.StockCode)
	if strings.HasPrefix(num, "4") || strings.HasPrefix(num, "8") || strings.HasPrefix(num, "92") {
		return false
	}
	if strings.Contains(row.Name, "ST") || strings.Contains(row.Name, "*") {
		return false
	}
	bk20 := strings.HasPrefix(num, "30") || strings.HasPrefix(num, "68") // 创业板/科创板 20cm

	c := func(k int) float64 { return bars[n-k].Close }
	vol := func(k int) float64 { return float64(bars[n-k].Volume) } // 股

	// 竞价派生量
	jjl := row.Amount / row.Price // 竞价量(股)= 竞价额/竞价价
	jje := row.Amount / 10000     // 竞价额(万元)
	gkf := row.Pct                // 高开幅度%

	// LBD:竞价量(手)/前5日均量(手)*100
	ma5v := (vol(1) + vol(2) + vol(3) + vol(4) + vol(5)) / 5
	if ma5v <= 0 {
		return false
	}
	lbd := (jjl / 100) / (ma5v / 100) * 100 // = jjl/ma5v*100

	// DLGL:昨日量非极端地量(> 20日均量*0.3)
	var sum20v float64
	for i := 0; i < 20; i++ {
		sum20v += vol(1 + i)
	}
	dlgl := vol(1) > (sum20v/20)*0.3

	// JHS:竞价换手率% = 竞价量(股)/流通股本(股)*100 = 竞价额/流通市值*100
	jhs := row.Amount / row.FloatMcap * 100

	// 昨日未涨停/未跌停(创业板科创板 ±20%,其余 ±10%,留余量 1.198/1.098 等)
	up, dn := 1.098, 0.902
	if bk20 {
		up, dn = 1.198, 0.802
	}
	fzt := c(1) < c(2)*up
	fdt := c(1) > c(2)*dn

	// 位置与趋势
	wgw := c(1)/c(11) < 1.25 // 10日涨幅<25%,避高位
	var sum20c float64
	for i := 0; i < 20; i++ {
		sum20c += c(1 + i)
	}
	qs := c(1) > sum20c/20 // 昨收站上20日线

	return lbd >= 8 && dlgl &&
		gkf > 0.5 && gkf < 3.0 &&
		jhs > 0.15 && jje > 500 &&
		fzt && fdt && wgw && qs
}

// matchAuctionFormulaG 评估「集合竞价稳健版选股公式 V2.1」(命名 G)。
// row: 当日竞价定型(竞价价/额/涨幅);bars: 截至前一交易日的日K(升序,末根=昨日=REF(...,1))。
// 逐条对照用户公式,不做任何规则改写。
func matchAuctionFormulaG(row services.AuctionFinalRow, bars []models.KLineData) bool {
	n := len(bars)
	if n < 60 {
		return false
	}
	// REF(x,k):k=1 为昨日(末根),k=2 为前日 ...
	c := func(k int) float64 { return bars[n-k].Close }
	h := func(k int) float64 { return bars[n-k].High }
	l := func(k int) float64 { return bars[n-k].Low }
	amt := func(k int) float64 { // 元;库中 amount 缺失时用 量(股)×收盘 估
		a := bars[n-k].Amount
		if a <= 0 {
			a = float64(bars[n-k].Volume) * bars[n-k].Close
		}
		return a
	}
	volShou := func(k int) float64 { return float64(bars[n-k].Volume) / 100 } // 股→手
	maC := func(period, k int) float64 {                                      // MA(C,period) 在 REF k 处
		end := n - k
		start := end - period + 1
		if start < 0 {
			return 0
		}
		s := 0.0
		for i := start; i <= end; i++ {
			s += bars[i].Close
		}
		return s / float64(period)
	}
	llvL := func(period, k int) float64 { // LLV(L,period) 在 REF k 处
		end := n - k
		start := end - period + 1
		if start < 0 {
			start = 0
		}
		m := bars[start].Low
		for i := start; i <= end; i++ {
			if bars[i].Low < m {
				m = bars[i].Low
			}
		}
		return m
	}

	// —— 一、基础竞价数据 ——
	昨收 := row.Price / (1 + row.Pct/100) // = DYNAINFO(3)
	竞价价 := row.Price                    // = DYNAINFO(4)
	竞价额 := row.Amount                   // = DYNAINFO(15)(元)
	if 昨收 <= 0 || 竞价价 <= 0 {
		return false
	}
	竞价涨幅 := row.Pct
	竞价量 := 竞价额 / 竞价价 / 100    // 手
	prevVolShou := volShou(1) // 昨日量(手)
	prevAmt := amt(1)         // 昨日额(元)
	if prevVolShou <= 0 || prevAmt <= 0 {
		return false
	}
	竞昨量比 := 竞价量 / prevVolShou * 100
	竞昨额比 := 竞价额 / prevAmt * 100

	// —— 二、状态过滤 ——
	正常交易 := 竞价量 > 0 && prevVolShou > 0 && prevAmt > 0 // DYNAINFO(8)>0 隐含(有竞价量即有效)
	name := row.Name
	非ST := !strings.Contains(name, "ST") && !strings.Contains(name, "*")
	非退市 := !strings.Contains(name, "退")

	// —— 三、竞价强度 ——
	适度高开 := 竞价涨幅 >= 1 && 竞价涨幅 <= 4
	竞价金额达标 := 竞价额 >= 15000000
	竞价放量 := 竞昨量比 >= 4 && 竞昨量比 <= 15
	竞价额强 := 竞昨额比 >= 3 && 竞昨额比 <= 12

	// —— 四、流动性 ——
	昨日成交活跃 := prevAmt >= 200000000

	// —— 五、趋势 ——
	站上二十日线 := c(1) > maC(20, 1)
	短期趋势向上 := maC(5, 1) >= maC(10, 1)
	二十日线向上 := maC(20, 1) >= maC(20, 2)
	趋势向上 := 站上二十日线 && 短期趋势向上 && 二十日线向上

	// —— 六、位置 ——
	非严重高位 := c(1)/llvL(20, 1) <= 1.30
	中期不过热 := c(1)/llvL(60, 1) <= 1.50
	近期涨幅适中 := c(1)/c(6) < 1.20

	// —— 七、昨日状态 ——
	昨日不过热 := c(1)/c(2) < 1.095
	昨日不弱 := c(1)/c(2) > 0.97
	昨日承接正常 := (c(1)-l(1))/(h(1)-l(1)+0.01) >= 0.45

	// —— 八、价格 ——
	价格适中 := 竞价价 >= 3 && 竞价价 <= 80

	// —— 九、最终选股 ——
	return 正常交易 && 非ST && 非退市 && 适度高开 && 竞价金额达标 && 竞价放量 &&
		竞价额强 && 昨日成交活跃 && 趋势向上 && 非严重高位 && 中期不过热 &&
		近期涨幅适中 && 昨日不过热 && 昨日不弱 && 昨日承接正常 && 价格适中
}

// StockIntradayResult 单股单日分时(含竞价过程)
type StockIntradayResult struct {
	Code    string                  `json:"code"`
	Date    string                  `json:"date"`
	Auction []services.IntradayTick `json:"auction"` // 9:15-9:25 竞价过程(30s粒度)
	Minutes []services.IntradayTick `json:"minutes"` // 9:30-15:00 分时(60s粒度,量额为累计)
}

// GetStockIntraday 某股某日的竞价过程+分时序列。
func (a *App) GetStockIntraday(code, date string) StockIntradayResult {
	res := StockIntradayResult{Code: code, Date: date}
	if a.intradayService == nil {
		return res
	}
	auction, minutes, err := a.intradayService.StockIntraday(code, date)
	if err != nil {
		log.Warn("分时查询失败 %s %s: %v", code, date, err)
		return res
	}
	res.Auction = auction
	res.Minutes = minutes
	return res
}

// GetStockFocusTicks 重点池 3 秒线(自选/持仓股)。
func (a *App) GetStockFocusTicks(code, date string) []services.IntradayTick {
	if a.intradayService == nil {
		return nil
	}
	ticks, err := a.intradayService.StockFocusTicks(code, date)
	if err != nil {
		return nil
	}
	return ticks
}

// intradayFocusPool 重点池:自选(全部分组去重)+ 模拟持仓在场股。
func (a *App) intradayFocusPool() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 128)
	add := func(sym string) {
		if sym == "" || seen[sym] {
			return
		}
		seen[sym] = true
		out = append(out, sym)
	}
	// ⚠️2026-07-28 修:这里原来是
	//     for _, codes := range a.GetStockGroups() { for _, c := range codes { add(c) } }
	// 而 GetStockGroups 返回的是 **股票代码 → 所属分组ID列表**(map[symbol][]groupID),
	// 遍历"值"拿到的是 g1784700493950713450 / trade-journal 这种**分组 ID**,被当成股票代码塞进了重点池。
	// 两个后果:
	//   ① 自选股**一条 3 秒线都没采到**(实测:只在分组里的 59 只票今日 focus_ticks 全为 0,
	//      而模拟持仓的票有 4857 条)——重点池等于只覆盖了持仓,自选那半边一直是空的;
	//   ② TDX 的 GetQuote 只要有一个代码不被 IsStock 认可就**整批报错**
	//      ("DefaultCodes未初始化"),于是每 3 秒刷一条 WARN、每次都白跑一次主源再回落兜底源。
	// 正解:取 **键**(股票代码)。另外直接用 configService,不走 a.GetStockGroups()——
	// 后者每次调用都会跑一遍 syncTradeJournalWatchGroup,3 秒一次没必要。
	if a.configService != nil {
		for _, s := range a.configService.GetWatchlist() {
			add(s.Symbol) // 自选(含未归组的)
		}
		for code := range a.configService.GetStockGroups() {
			add(code) // 分组成员;键才是代码
		}
	}
	if a.paperService != nil {
		for _, p := range a.paperService.OpenPositions() {
			add(p.Symbol)
		}
	}
	return out
}

// GetIntradayCoverage 采集覆盖统计(攒了几天/多少行)。
func (a *App) GetIntradayCoverage() services.IntradayCoverage {
	if a.intradayService == nil {
		return services.IntradayCoverage{}
	}
	return a.intradayService.Coverage()
}
