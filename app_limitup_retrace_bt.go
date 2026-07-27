package main

// 涨停回踩12 历史回测(2026-07-26 建,07-27 重写取数结构)。
//
// ⚠️为什么不用 SQL 复刻规则回测:那是对 Go 评估器的二次实现,两边一有出入结论就是错的
// (已踩:我写的 SQL 六种放宽方案全返回 0,而 Go 漏斗显示候选能走到第 15 道门,说明 SQL 自己有 bug)。
// 所以直接调用生产用的 limitupRetraceDaily 逐日重放,零复刻零漂移。
//
// ⚠️取数结构踩过的坑:第一版按「逐日 × 逐股」查K线 → 500交易日 × 每日上千只 = 五十万次查询,
// 冷盘上跑 50 分钟没跑完。改成**每只股票整段序列只读一次,然后在内存里逐日向前走**:
// 查询数从 50 万降到 5 千,次日行情直接取序列下一根(连查都不用查)。
//
// 产出:①门级漏斗(哪道门最卡)②信号明细 ③按用户成文纪律结算 + 扣费超额(与项目其它策略可比)。

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/services"
)

type lrSignal struct {
	Date, Code, Name string
	Score            float64
	Entry            float64 // 信号日收盘(= 用户成文的尾盘介入价)
	NextO, NextH     float64
	NextL, NextC     float64
	Bench            float64
	HasBench         bool
}

// BacktestLimitupRetrace 用生产评估器回测 [start,end]。
func (a *App) BacktestLimitupRetrace(start, end string) string {
	if a == nil || a.historyService == nil {
		return "历史服务未就绪"
	}
	if start == "" {
		start = "2024-07-01" // 全市场完整覆盖从 2024-07 起(更早只有零星几十只)
	}
	if end == "" {
		end = a.historyService.LatestTradeDate()
	}
	t0 := time.Now()

	codes := a.historyService.AllUniverseCodes()
	if len(codes) == 0 {
		return "取不到股票池"
	}
	industryMap := buildIndustryMapFromEmbedded()

	lrFunnelMu.Lock()
	lrFunnelOn, lrFunnelCnt = true, map[string]int{}
	lrFunnelMu.Unlock()

	// 窗口长度:回测区间交易日 + 120 根预热(评估器要求 ≥120)
	span := len(a.historyService.TradeDatesSince(start, end))
	if span == 0 {
		return "区间内没有交易日"
	}
	need := span + 140

	var (
		mu       sync.Mutex
		signals  []lrSignal
		scanned  int
		loadFail int
	)
	sem := make(chan struct{}, 6) // NAS 只有 4 核 3.7G,并发别开大
	var wg sync.WaitGroup
	for _, code := range codes {
		if !isMainBoard10cm(code, "") { // 名字里的 ST 稍后用序列带回的名字再判
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(code string) {
			defer wg.Done()
			defer func() { <-sem }()
			daily, err := a.historyService.LoadKLineDataUntil(code, end, need)
			if err != nil || len(daily) < 130 {
				mu.Lock()
				loadFail++
				mu.Unlock()
				return
			}
			local := []lrSignal{}
			cnt := 0
			for i := 120; i < len(daily); i++ {
				d := daily[i]
				if len(d.Time) < 10 {
					continue
				}
				day := d.Time[:10]
				if day < start || day > end {
					continue
				}
				prev := daily[i-1]
				if prev.Close <= 0 || d.Close <= 0 {
					continue
				}
				pct := (d.Close/prev.Close - 1) * 100
				if pct > -1.0 || pct < -6.5 { // 与线上同款粗筛:信号日是缩量回踩阴线
					continue
				}
				cnt++
				// 合成快照行:门判定只用到 OHLCV 与价格,其余字段仅供展示
				row := services.ScanSnapshotRow{
					Symbol: code, Name: "", Price: d.Close, ChangePercent: pct, Amount: d.Amount,
				}
				item, ok := limitupRetraceDaily(row, industryMap[code], daily[:i+1], day)
				if !ok {
					continue
				}
				sig := lrSignal{Date: day, Code: code, Score: item.Score, Entry: d.Close}
				if i+1 < len(daily) { // 次日行情就在序列里,不用再查库
					nb := daily[i+1]
					sig.NextO, sig.NextH, sig.NextL, sig.NextC = nb.Open, nb.High, nb.Low, nb.Close
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

	lrFunnelMu.Lock()
	funnel := map[string]int{}
	for k, v := range lrFunnelCnt {
		funnel[k] = v
	}
	lrFunnelOn = false
	lrFunnelMu.Unlock()

	sort.Slice(signals, func(i, j int) bool { return signals[i].Date < signals[j].Date })
	// 基准按日缓存(每个信号日只查一次全市场中位)
	benchCache := map[string]float64{}
	for i := range signals {
		nd := a.historyService.NextTradeDateAfter(signals[i].Date)
		if nd == "" {
			continue
		}
		v, ok := benchCache[nd]
		if !ok {
			if m, good := a.historyService.MarketMedianPctPublic(nd); good {
				v = m
				benchCache[nd] = m
				ok = true
			}
		}
		if ok {
			signals[i].Bench, signals[i].HasBench = v, true
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "【涨停回踩12 历史回测】%s ~ %s\n", start, end)
	fmt.Fprintf(&b, "股票池 %d,读取失败 %d,进入评估 %d 股日,命中 %d 个信号,耗时 %.0fs\n\n",
		len(codes), loadFail, scanned, len(signals), time.Since(t0).Seconds())

	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(funnel))
	for k, v := range funnel {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	b.WriteString("门级漏斗(淘汰量倒序,最前面的就是最卡的门):\n")
	for i, e := range list {
		if i >= 14 {
			break
		}
		fmt.Fprintf(&b, "  %-30s %8d\n", e.k, e.v)
	}

	if len(signals) == 0 {
		b.WriteString("\n⚠️两年零信号:这套规则无法被验证,必须放宽后重测。\n")
		return b.String()
	}

	var sumClose, sumHigh, sumRule, sumEx float64
	winClose, winRule, winEx, nEx, nOK := 0, 0, 0, 0, 0
	byYear := map[string][]float64{}
	b.WriteString("\n信号明细(入场=信号日收盘):\n")
	for _, s := range signals {
		if s.Entry <= 0 || s.NextC <= 0 {
			continue
		}
		nOK++
		closeRet := (s.NextC/s.Entry - 1) * 100
		highRet := (s.NextH/s.Entry - 1) * 100
		lowRet := (s.NextL/s.Entry - 1) * 100
		ruleRet := closeRet
		switch { // 悲观口径:同日既触止损又冲高,算止损
		case lowRet <= -4.5:
			ruleRet = -4.5
		case highRet >= 7:
			ruleRet = 7
		}
		ruleRet -= 0.2
		sumClose += closeRet
		sumHigh += highRet
		sumRule += ruleRet
		if closeRet > 0 {
			winClose++
		}
		if ruleRet > 0 {
			winRule++
		}
		exTxt := "基准不可得"
		if s.HasBench {
			ex := closeRet - s.Bench - 0.2
			sumEx += ex
			nEx++
			if ex > 0 {
				winEx++
			}
			exTxt = fmt.Sprintf("超额%+.2f%%", ex)
			byYear[s.Date[:4]] = append(byYear[s.Date[:4]], ex)
		}
		fmt.Fprintf(&b, "  %s %s 评分%.0f 入%.2f → 次日 开%+.1f%% 高%+.1f%% 低%+.1f%% 收%+.2f%% | 纪律%+.2f%% | %s\n",
			s.Date, s.Code, s.Score, s.Entry, (s.NextO/s.Entry-1)*100, highRet, lowRet, closeRet, ruleRet, exTxt)
	}
	if nOK == 0 {
		b.WriteString("\n信号都缺次日行情,无法结算。\n")
		return b.String()
	}
	n := float64(nOK)
	fmt.Fprintf(&b, "\n汇总(可结算 n=%d):\n", nOK)
	fmt.Fprintf(&b, "  次日收盘裸收益   均值%+.2f%%  红盘率%.0f%%\n", sumClose/n, float64(winClose)/n*100)
	fmt.Fprintf(&b, "  次日最高(天花板) 均值%+.2f%%\n", sumHigh/n)
	fmt.Fprintf(&b, "  按成文纪律结算   均值%+.2f%%  胜率%.0f%%(已扣0.2%%成本)\n", sumRule/n, float64(winRule)/n*100)
	if nEx > 0 {
		fmt.Fprintf(&b, "  扣费超额(对全市场中位) 均值%+.2f%%  胜率%.0f%%  n=%d\n",
			sumEx/float64(nEx), float64(winEx)/float64(nEx)*100, nEx)
	}
	if len(byYear) > 1 {
		b.WriteString("  分年份扣费超额(regime 检验):\n")
		ys := make([]string, 0, len(byYear))
		for y := range byYear {
			ys = append(ys, y)
		}
		sort.Strings(ys)
		for _, y := range ys {
			v := byYear[y]
			s, w := 0.0, 0
			for _, x := range v {
				s += x
				if x > 0 {
					w++
				}
			}
			fmt.Fprintf(&b, "    %s: n=%d 均值%+.2f%% 胜率%.0f%%\n", y, len(v), s/float64(len(v)), float64(w)/float64(len(v))*100)
		}
	}
	b.WriteString("\n⚠️n<30 的结论一律当噪声;分时承接那20分回测里不参与(历史分时覆盖不足),故此处评分只反映日线部分。\n")
	return b.String()
}

var _ = models.KLineData{}
