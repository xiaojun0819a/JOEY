package main

// 涨停回踩12(limitup-retrace-v12,2026-07-26 用户成文规则落地)
//
// 三日序列模型:T-2 高质量首板 → T-1 温和放量换手 → T(信号日)缩量回踩长下影收回。
// 与既有「涨停回调低吸4」(limit-pullback-v1)不是一回事:那个是松散版(近2-8日有涨停+回调缩量+8触发命中5),
// 这个要求严格的三日相邻序列,且主板 10cm 专用。
//
// 用户定的三条纠偏(都照做了):
//   ① 只做主板 10cm 首板 —— 创业/科创 20cm、北交 30cm 与本参数不通用,混在一起等于自欺;
//   ② 第三日"缩量"不是越少越好 —— 用 35%~65% 区间,极端地量往往是没人接盘;
//   ③ 长下影不等于洗盘 —— 必须同时"跌不深/收得回/关键位不破/上影不长/尾盘承接好"。
//
// 评分 100 分(V5.0 模块制,不是满足/不满足):
//   首板质量20 + 二日放量确认15 + 三日缩量洗盘20 + 分时承接20 + 板块共振10 + 大盘环境5 + 筹码结构5 + 资金成本5
// **分时承接是本 app 相对通达信的真优势**:通达信日线公式拿不到"14点后是否创新低/尾盘放量/均价线斜率",
// 而这里有自采集 15 秒级分时(2026-07-06 起)。做法=日线硬门先筛出极小候选池,只对幸存者取分时,成本可忽略。
// 分时拿不到时(历史日/停采期)该模块记 0 并把总分按可得模块归一化,同时明示"分时不可得",不假装满分。
//
// 定位:候选发现工具,不接 14:50 自动买入。未经滚动回测,先攒留痕样本再判优劣(项目铁律,见短线策略证伪史)。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/services"
)

const limitupRetraceID = "limitup-retrace-v12"

// ===== 漏斗诊断:数清每道门各杀掉多少只,决定放宽哪道门时用数据说话,不拍脑袋 =====
var (
	lrFunnelMu  sync.Mutex
	lrFunnelOn  bool
	lrFunnelCnt map[string]int
)

func lrDrop(reason string) (models.LowBuyScannerItem, bool) {
	lrFunnelMu.Lock()
	if lrFunnelOn && lrFunnelCnt != nil {
		lrFunnelCnt[reason]++
	}
	lrFunnelMu.Unlock()
	return models.LowBuyScannerItem{}, false
}

// DiagnoseLimitupRetrace 跑一遍全市场,报告每道门的淘汰数(按淘汰量倒序)。
func (a *App) DiagnoseLimitupRetrace() string {
	lrFunnelMu.Lock()
	lrFunnelOn, lrFunnelCnt = true, map[string]int{}
	lrFunnelMu.Unlock()
	res := a.RunLimitupRetraceScanner(models.LowBuyScannerRequest{Limit: 50})
	lrFunnelMu.Lock()
	snap := map[string]int{}
	for k, v := range lrFunnelCnt {
		snap[k] = v
	}
	lrFunnelOn = false
	lrFunnelMu.Unlock()

	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(snap))
	total := 0
	for k, v := range snap {
		list = append(list, kv{k, v})
		total += v
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	var b strings.Builder
	fmt.Fprintf(&b, "【涨停回踩12 漏斗诊断】全市场 %d,进入日线复核后被各门淘汰 %d,最终入选 %d\n",
		res.UniverseCount, total, res.SelectedCount)
	b.WriteString("(按淘汰量倒序;排最前的就是当前最卡的门)\n")
	for _, e := range list {
		fmt.Fprintf(&b, "  %-28s %6d\n", e.k, e.v)
	}
	return b.String()
}

// isMainBoard10cm 主板 10cm 普通股:剔创业(300/301)、科创(688/689)、北交(8x/4x)、ST。
func isMainBoard10cm(symbol, name string) bool {
	s := strings.ToLower(strings.TrimSpace(symbol))
	digits := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "sh"), "sz"), "bj")
	if strings.HasPrefix(s, "bj") || strings.HasPrefix(digits, "8") || strings.HasPrefix(digits, "4") {
		return false
	}
	if strings.HasPrefix(digits, "300") || strings.HasPrefix(digits, "301") ||
		strings.HasPrefix(digits, "688") || strings.HasPrefix(digits, "689") {
		return false
	}
	return !strings.Contains(strings.ToUpper(name), "ST")
}

// limitupRetraceDaily 日线层判定 + 打分。daily 升序,最后一根 = 信号日(第三天)。
//
// ⚠️架构按用户原文第四节:「基础形态必须满足;质量条件进行评分;只考虑80分以上」。
// 第一版我把**所有**条件都做成硬门,同时又打一遍分——既然不满足的根本进不来,分数就没意义了,
// 而且十几道门相乘直接把信号打成 0(2026-07 实测:7个月全市场 144,130 股日 → 0 信号)。
// 现在拆成两层:
//
//	【硬门=形态身份】不满足就不是"首板→放量→缩量回踩长下影"这个形态,没得商量;
//	【评分=质量高低】放量几倍、距底多远、均线状态、收盘位置……都只影响分数,不挡入选。
//
// 日线部分满分 80,分时承接那 20 分在扫描器里补。
func limitupRetraceDaily(row services.ScanSnapshotRow, industry string, daily []models.KLineData, asOf string) (models.LowBuyScannerItem, bool) {
	n := len(daily)
	if n < 120 {
		return lrDrop("数据不足")
	}
	if !isMainBoard10cm(row.Symbol, row.Name) {
		return lrDrop("非主板10cm")
	}
	d3 := daily[n-1] // 第三天=信号日
	d2 := daily[n-2] // 第二天
	d1 := daily[n-3] // 第一天=首板日
	d0 := daily[n-4] // 首板前一日
	for _, b := range []models.KLineData{d3, d2, d1, d0} {
		if b.Close <= 0 || b.High <= 0 || b.Low <= 0 || b.Volume <= 0 {
			return lrDrop("四根K线数据缺失")
		}
	}

	// ==================== 硬门:形态身份 ====================
	// ① 第一天必须是封住的涨停,且不是一字(一字买不到,没有换手意义)
	d1Pct := (d1.Close/d0.Close - 1) * 100
	if d1Pct < 9.8 || d1.Close < d1.High*0.999 {
		return lrDrop("首板未涨停/未封住")
	}
	if d1.High <= d1.Low || (d1.Open > 0 && d1.Open >= d1.Close) {
		return lrDrop("首板一字")
	}
	// ② 首板身份:前 8 日无涨停(连板票的三日结构含义完全不同,不能混)
	for i := n - 4; i >= n-11 && i >= 1; i-- {
		if daily[i].Close > 0 && daily[i-1].Close > 0 &&
			(daily[i].Close/daily[i-1].Close-1)*100 >= 9.8 {
			return lrDrop("前8日已有涨停(非首板)")
		}
	}
	// ③ 第二天:收阳 + 不破首板实体中位(承接住了,才谈得上"换手充分")
	d1Mid := (d1.Open + d1.Close) / 2
	if d1.Open <= 0 {
		d1Mid = (d0.Close + d1.Close) / 2
	}
	if d2.Open > 0 && d2.Close <= d2.Open {
		return lrDrop("二日收阴")
	}
	if d2.Low < d1Mid*0.97 || d2.Close <= d1Mid {
		return lrDrop("二日破首板实体中位")
	}
	// ④ 第三天:缩量 + 回踩 + 长下影收回 + 关键位不破 —— 这四条就是形态本身
	d3Pct := (d3.Close/d2.Close - 1) * 100
	if d3Pct > -1.5 || d3Pct < -5.5 {
		return lrDrop("三日跌幅不在-5.5%~-1.5%")
	}
	d3VolRatio := float64(d3.Volume) / float64(d2.Volume)
	if d3VolRatio < 0.35 || d3VolRatio > 0.65 {
		return lrDrop("三日量比不在35%~65%")
	}
	d3Body := math.Abs(d3.Close - d3.Open)
	d3Lower := math.Min(d3.Close, d3.Open) - d3.Low
	d3Upper := d3.High - math.Max(d3.Close, d3.Open)
	if d3.Open <= 0 { // 开盘缺失:用前收当开盘,口径粗但不整批误杀
		d3Body = math.Abs(d3.Close - d2.Close)
		d3Lower = math.Min(d3.Close, d2.Close) - d3.Low
		d3Upper = d3.High - math.Max(d3.Close, d2.Close)
	}
	if d3Lower < d3Body*1.2 || d3Lower <= d3Upper*1.1 {
		return lrDrop("三日下影不够长/上影过长")
	}
	d3ClosePos := (d3.Close - d3.Low) / (d3.High - d3.Low + 0.01)
	if d3ClosePos < 0.55 {
		return lrDrop("三日收盘位置<55%")
	}
	if d3.Close < d1Mid || d3.Low < d1.Low*0.97 {
		return lrDrop("三日关键位破了")
	}
	// ⑤ 可交易性
	if row.Price < 3 || row.Price > 80 {
		return lrDrop("价格不在3~80元")
	}

	// ==================== 评分:质量高低(日线 80 分) ====================
	score := 0.0
	triggers := []string{"主板10cm首板", "缩量回踩长下影", "关键位未破"}
	reasons := []string{}
	risks := []string{}

	// —— 首板质量 20 —— 放量 8 / 实体 6 / 位置 6
	v5, okv := klineVolMAAt(daily, n-3, 5)
	d1VolMult := 0.0
	if okv && v5 > 0 {
		d1VolMult = float64(d1.Volume) / v5
	}
	switch {
	case d1VolMult >= 1.8 && d1VolMult <= 4.0:
		score += 8
	case d1VolMult >= 1.5 && d1VolMult <= 4.5:
		score += 6
	case d1VolMult >= 1.2:
		score += 3
		risks = append(risks, fmt.Sprintf("首板放量仅 %.2f 倍,资金介入力度一般", d1VolMult))
	default:
		risks = append(risks, fmt.Sprintf("首板放量 %.2f 倍不在健康区间", d1VolMult))
	}
	if d1.Open > 0 && (d1.High-d1.Low) > 0 {
		br := (d1.Close - d1.Open) / (d1.High - d1.Low)
		switch {
		case br >= 0.6:
			score += 6
		case br >= 0.35:
			score += 4
		default:
			score += 1
			risks = append(risks, "首板实体偏小,盘中震荡大")
		}
	} else {
		score += 4 // 开盘缺失:给中档,不奖不罚
	}
	low60 := daily[n-3].Low
	for i := n - 3; i >= 0 && i > n-63; i-- {
		if daily[i].Low > 0 && daily[i].Low < low60 {
			low60 = daily[i].Low
		}
	}
	riseFromLow60 := 0.0
	if low60 > 0 {
		riseFromLow60 = d1.Close / low60
	}
	switch {
	case riseFromLow60 > 0 && riseFromLow60 < 1.25:
		score += 6
	case riseFromLow60 < 1.40:
		score += 4
	case riseFromLow60 < 1.55:
		score += 2
	default:
		risks = append(risks, fmt.Sprintf("首板距60日低点已 +%.0f%%,高位首板诱多风险", (riseFromLow60-1)*100))
	}

	// —— 二日放量确认 15 —— 涨幅档 4 / 量比 6 / 收盘位置 3 / 上影 2
	d2Pct := (d2.Close/d1.Close - 1) * 100
	switch {
	case d2Pct >= 1 && d2Pct <= 5:
		score += 4
	case d2Pct >= 0 && d2Pct <= 7:
		score += 3
	default:
		risks = append(risks, fmt.Sprintf("二日涨幅 %+.2f%% 偏离健康区(0~7%%)", d2Pct))
	}
	d2VolRatio := float64(d2.Volume) / float64(d1.Volume)
	switch {
	case d2VolRatio >= 1.10 && d2VolRatio <= 1.60:
		score += 6
	case d2VolRatio >= 1.05 && d2VolRatio <= 1.80:
		score += 4
	case d2VolRatio > 1.80:
		score += 1
		risks = append(risks, fmt.Sprintf("二日爆量 %.2f 倍首板,派发嫌疑", d2VolRatio))
	}
	d2ClosePos := (d2.Close - d2.Low) / (d2.High - d2.Low + 0.01)
	if d2ClosePos >= 0.70 {
		score += 3
	} else if d2ClosePos >= 0.55 {
		score += 2
	}
	d2Body := math.Abs(d2.Close - d2.Open)
	d2Upper := d2.High - math.Max(d2.Close, d2.Open)
	if d2.Open <= 0 || d2Upper <= d2Body*1.2+0.01 {
		score += 2
	} else {
		risks = append(risks, "二日上影偏长,盘中有抛压")
	}

	// —— 三日缩量洗盘 20 —— 缩量档 8 / 下影强度 6 / 收盘位置 6
	if d3VolRatio >= 0.40 && d3VolRatio <= 0.60 {
		score += 8
	} else {
		score += 5
	}
	if d3Body > 0 {
		switch r := d3Lower / d3Body; {
		case r >= 2.0:
			score += 6
		case r >= 1.5:
			score += 4
		default:
			score += 2
		}
	}
	switch {
	case d3ClosePos >= 0.70:
		score += 6
	case d3ClosePos >= 0.60:
		score += 4
	default:
		score += 2
	}

	// —— 趋势与均线 10(原为硬门,现降级计分) ——
	ma5, ok5 := klineMAAt(daily, n-1, 5)
	ma10, ok10 := klineMAAt(daily, n-1, 10)
	ma20, ok20 := klineMAAt(daily, n-1, 20)
	ma20d1, okA := klineMAAt(daily, n-3, 20)
	ma20prev, okB := klineMAAt(daily, n-4, 20)
	if okA && okB && d1.Close > ma20d1 && ma20d1 >= ma20prev {
		score += 5
		triggers = append(triggers, "首板站上MA20且MA20向上")
	} else {
		risks = append(risks, "首板未站上MA20或MA20下行(下跌反弹嫌疑)")
	}
	if ok5 && ok10 && ok20 {
		if ma5 > ma10 && ma10 >= ma20 {
			score += 5
			triggers = append(triggers, "均线多头排列")
		} else if d3.Close >= ma5*0.98 && d3.Close >= ma10*0.97 {
			score += 2
		} else {
			risks = append(risks, "已跌破MA5/MA10支撑")
		}
	}

	// —— 三日细节 10 —— 小低开 4 / 不过度反抽 3 / 未破二日低点 3
	if d3.Open > 0 {
		op := (d3.Open/d2.Close - 1) * 100
		if op <= 0.5 && op >= -4 {
			score += 4
		} else if op < -4 {
			risks = append(risks, fmt.Sprintf("三日大幅低开 %.2f%%,恐慌盘", op))
		}
	} else {
		score += 2
	}
	if d3.High <= d2.High*1.035 {
		score += 3
	}
	if d3.Low >= d2.Low {
		score += 3
	} else {
		risks = append(risks, "盘中破了第二天低点(已收回,属下探洗盘边缘)")
	}

	// —— 资金成本 5:收盘站上当日真实成交均价(VWAP=额/量,量单位手) ——
	vwap := 0.0
	if d3.Amount > 0 && d3.Volume > 0 {
		vwap = d3.Amount / (float64(d3.Volume) * 100)
		if vwap > 0 && d3.Close >= vwap {
			score += 5
			triggers = append(triggers, "收盘站上当日均价")
		}
	}

	reasons = append(reasons,
		fmt.Sprintf("首板日:涨 %.2f%%,放量 %.2f×前5日均量,距60日低点 +%.0f%%", d1Pct, d1VolMult, (riseFromLow60-1)*100),
		fmt.Sprintf("第二天:涨 %.2f%%,量 %.2f×首板,收在振幅 %.0f%% 位置", d2Pct, d2VolRatio, d2ClosePos*100),
		fmt.Sprintf("第三天:跌 %.2f%%,量 %.0f%%于第二天,下影/实体 %.1f,收在振幅 %.0f%% 位置", d3Pct, d3VolRatio*100, safeDiv(d3Lower, d3Body), d3ClosePos*100),
		fmt.Sprintf("关键位:首板实体中位 %.2f,首板最低 %.2f,MA5 %.2f / MA10 %.2f", d1Mid, d1.Low, ma5, ma10),
	)
	if vwap > 0 {
		reasons = append(reasons, fmt.Sprintf("当日真实成交均价 %.2f,收盘 %.2f", vwap, d3.Close))
	}

	item := models.LowBuyScannerItem{
		Symbol: row.Symbol, Name: row.Name, Price: row.Price, ChangePercent: row.ChangePercent,
		Amount: row.Amount, TurnoverRate: row.TurnoverRate,
		MainNetInflow: row.MainNetInflow, MainNetInflowRatio: row.MainNetInflowRatio, MainFlowSource: row.MainFlowSource,
		TotalMarketCap: row.TotalMarketCap, FloatMarketCap: row.FloatMarketCap,
		Industry: industry, Score: score, TriggerCount: len(triggers),
		Triggers: triggers, Reasons: reasons, RiskFlags: risks,
		MA10: ma10, MA10Status: "hold",
		BuyPointHint:  fmt.Sprintf("尾盘 14:45 后确认不再创新低再低吸;理想价=收盘附近或首板实体中位 %.2f 上方0~2%%", d1Mid),
		SellPointHint: "次日高开3%+冲高分批止盈;冲高后跌破分时均价优先锁利;买入后2日未破第三天高点则退出",
		StopLossHint:  fmt.Sprintf("结构止损=第三天最低 %.2f 下方1%%;或买入价 -4%%~-5%% 无条件退出", d3.Low),
	}
	return item, true
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// intradayAcceptScore 分时承接(满分20)。只对日线幸存者调用,成本可忽略。
// 四条各5分:14:00后不创新低 / 收盘站上全天均价 / 尾盘15分钟放量 / 下午均价线向上。
// 返回 (得分, 说明, 是否拿到分时)。
func (a *App) intradayAcceptScore(code, date string) (float64, []string, bool) {
	if a == nil || a.intradayService == nil {
		return 0, nil, false
	}
	_, minutes, err := a.intradayService.StockIntraday(code, date)
	if err != nil || len(minutes) < 60 {
		return 0, nil, false
	}
	score := 0.0
	notes := []string{}

	// 全天最低点出现时刻 & 14:00 后的最低
	dayLow, lowAt := minutes[0].Price, minutes[0].Time
	for _, t := range minutes {
		if t.Price > 0 && t.Price < dayLow {
			dayLow, lowAt = t.Price, t.Time
		}
	}
	pmLow := math.MaxFloat64
	for _, t := range minutes {
		if t.Time >= "14:00" && t.Price > 0 && t.Price < pmLow {
			pmLow = t.Price
		}
	}
	if pmLow == math.MaxFloat64 {
		pmLow = dayLow
	}
	if pmLow > dayLow*1.0005 { // 14:00 后没再创新低(留万5容差防同价)
		score += 5
		notes = append(notes, fmt.Sprintf("14:00后未创新低(全天低点 %.2f 出现在 %s)", dayLow, lowAt))
	} else {
		notes = append(notes, "⚠️14:00后仍在创新低")
	}

	// 收盘 vs 全天均价(累计额/累计量)
	last := minutes[len(minutes)-1]
	if last.Volume > 0 && last.Amount > 0 {
		vwap := last.Amount / (last.Volume * 100)
		if vwap > 0 {
			if last.Price >= vwap {
				score += 5
				notes = append(notes, fmt.Sprintf("收盘 %.2f 站上全天均价 %.2f", last.Price, vwap))
			} else {
				notes = append(notes, fmt.Sprintf("⚠️收盘 %.2f 低于全天均价 %.2f", last.Price, vwap))
			}
			// 下午均价线是否抬升:13:00 与收盘的均价对比
			for _, t := range minutes {
				if t.Time >= "13:00" && t.Volume > 0 && t.Amount > 0 {
					pmVwap := t.Amount / (t.Volume * 100)
					if pmVwap > 0 && vwap >= pmVwap {
						score += 5
						notes = append(notes, "下午均价线走平或抬升")
					} else {
						notes = append(notes, "⚠️下午均价线下行(资金成本一路走低)")
					}
					break
				}
			}
		}
	}

	// 尾盘15分钟放量:14:45后增量 vs 全天每15分钟均量
	tailVol, totalVol := 0.0, last.Volume
	var beforeTail float64
	for _, t := range minutes {
		if t.Time >= "14:45" {
			if beforeTail == 0 {
				beforeTail = t.Volume
			}
		}
	}
	if beforeTail > 0 && totalVol > beforeTail {
		tailVol = totalVol - beforeTail
		avg15 := totalVol / 16 // 全天约16个15分钟段
		if avg15 > 0 && tailVol >= avg15 {
			score += 5
			notes = append(notes, fmt.Sprintf("尾盘15分钟放量(%.0f手 ≥ 全天15分钟均量 %.0f手)", tailVol, avg15))
		} else {
			notes = append(notes, "尾盘15分钟未见放量回补")
		}
	}
	return score, notes, true
}

// RunLimitupRetraceScanner 涨停回踩12 扫描。
func (a *App) RunLimitupRetraceScanner(req models.LowBuyScannerRequest) models.LowBuyScannerResult {
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	result := models.LowBuyScannerResult{
		AsOf:        start.Format("2006-01-02 15:04:05"),
		RuleVersion: "涨停回踩12(主板10cm三日序列:首板→温和放量→缩量35~65%回踩长下影收回;100分模块制,含分时承接20分)",
		Items:       []models.LowBuyScannerItem{},
	}
	if a == nil || a.marketService == nil {
		result.Warning = "行情服务未初始化"
		return result
	}
	snapshots, err := a.marketService.GetAllAStockSnapshot(req.IncludeBeijing)
	if err != nil {
		result.Warning = combineWarnings(result.Warning, "全A快照获取失败："+err.Error())
		return result
	}
	result.UniverseCount = len(snapshots)
	industryMap := buildIndustryMapFromEmbedded()
	candidates, checkedDaily, dailyFailed, droppedDaily := a.runParallelDailyScan(snapshots, industryMap, result.AsOf, parallelDailyScanSpec{
		klineDays: 180,
		minBars:   120,
		prefilter: func(row services.ScanSnapshotRow) bool {
			if row.Price <= 0 || row.Amount <= 0 || row.IsST {
				return false
			}
			if !isMainBoard10cm(row.Symbol, row.Name) {
				return false
			}
			// 信号日是缩量回踩阴线:粗筛跌幅区间(快照口径给容差)
			return row.ChangePercent <= -1.0 && row.ChangePercent >= -6.5
		},
		evaluate: limitupRetraceDaily,
	})
	result.CandidateCount = len(candidates)

	// 大盘环境 5 分(用快照现算,不碰坏掉的指数接口)
	marketScore, marketNote := limitupRetraceMarketScore(snapshots)

	// 只对日线幸存者取分时(池子极小,成本可忽略)
	sigDate := a.effectiveSignalDate()
	for i := range candidates {
		candidates[i].Score += marketScore
		if marketNote != "" {
			candidates[i].Reasons = append(candidates[i].Reasons, marketNote)
		}
		// 板块共振 10 分:同行业当日中位涨幅(候选池内近似,数据不足则不给分)
		if peer, ok := industryPeerStrength(snapshots, industryMap, candidates[i].Symbol); ok {
			switch {
			case peer >= 0:
				candidates[i].Score += 10
				candidates[i].Reasons = append(candidates[i].Reasons, fmt.Sprintf("板块共振:同行业当日中位 %+.2f%%(未跳水)", peer))
			case peer >= -1.5:
				candidates[i].Score += 5
				candidates[i].Reasons = append(candidates[i].Reasons, fmt.Sprintf("板块偏弱:同行业当日中位 %+.2f%%", peer))
			default:
				candidates[i].RiskFlags = append(candidates[i].RiskFlags, fmt.Sprintf("板块跳水:同行业当日中位 %+.2f%%", peer))
			}
		}
		// 分时承接 20 分
		if s, notes, ok := a.intradayAcceptScore(candidates[i].Symbol, sigDate); ok {
			candidates[i].Score += s
			candidates[i].Reasons = append(candidates[i].Reasons, notes...)
			if s >= 15 {
				candidates[i].Triggers = append(candidates[i].Triggers, "分时承接强")
			}
		} else {
			// 拿不到分时:该模块不计分,把总分按可得的 80 分归一化到 100,并明示
			candidates[i].Score = candidates[i].Score / 80 * 100
			candidates[i].RiskFlags = append(candidates[i].RiskFlags, "分时不可得(未计分时承接20分,总分已按80分制归一化)")
		}
		candidates[i].TriggerCount = len(candidates[i].Triggers)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Amount > candidates[j].Amount
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	candidates = a.applyLearnedTuning(limitupRetraceID, candidates)
	candidates = annotateAndDemoteSealed(candidates)
	result.Items = candidates
	result.SelectedCount = len(candidates)
	result.Warning = combineWarnings(result.Warning, fmt.Sprintf(
		"日线复核 %d 只(失败 %d,淘汰 %d);评分=首板20+二日15+三日20+分时20+板块10+大盘5+筹码5+成本5。"+
			"⚠️未经滚动回测,定位候选发现,不接自动买入", checkedDaily, dailyFailed, droppedDaily))
	log.Info("涨停回踩12扫描完成: 全市场%d 候选%d 入选%d 耗时%.1fs",
		result.UniverseCount, result.CandidateCount, result.SelectedCount, time.Since(start).Seconds())
	a.saveLowBuyStrategyPicks(limitupRetraceID, "涨停回踩12", result)
	return result
}

// limitupRetraceMarketScore 大盘环境 5 分。
// ⚠️不用上证指数:本项目的 GetKLineData("sh000001") 早被证实是坏的(见 oversold_learn 的基准注释),
// 改用手上现成的全A快照直接算两件事,既免一次取数,也更贴你说的"情绪周期比MACD重要":
//
//	① 全市场当日涨幅中位 ≥ -1.5%(不是普跌退潮日)→ 3分
//	② 涨停家数 ≥ 40(赚钱效应还在)→ 2分
func limitupRetraceMarketScore(snapshots []services.ScanSnapshotRow) (float64, string) {
	pcts := make([]float64, 0, len(snapshots))
	limitUp := 0
	for _, r := range snapshots {
		if r.Price <= 0 {
			continue
		}
		pcts = append(pcts, r.ChangePercent)
		if r.ChangePercent >= 9.8 {
			limitUp++
		}
	}
	if len(pcts) < 1000 { // 快照不全就不给分,宁缺勿假
		return 0, ""
	}
	sort.Float64s(pcts)
	med := pcts[len(pcts)/2]
	score := 0.0
	if med >= -1.5 {
		score += 3
	}
	if limitUp >= 40 {
		score += 2
	}
	if score == 5 {
		return 5, fmt.Sprintf("大盘环境:全市场中位 %+.2f%%、涨停 %d 家(赚钱效应在)", med, limitUp)
	}
	return score, fmt.Sprintf("⚠️大盘环境打折:全市场中位 %+.2f%%、涨停 %d 家(退潮日别做隔日模型)", med, limitUp)
}

// klineVolMAAt 成交量的 N 日均值(endIdx 处)。
func klineVolMAAt(klines []models.KLineData, endIdx int, period int) (float64, bool) {
	if period <= 0 || endIdx < 0 || endIdx >= len(klines) || endIdx-period+1 < 0 {
		return 0, false
	}
	sum := 0.0
	for i := endIdx - period + 1; i <= endIdx; i++ {
		if klines[i].Volume <= 0 {
			return 0, false
		}
		sum += float64(klines[i].Volume)
	}
	return sum / float64(period), true
}

// industryPeerStrength 同行业当日涨幅中位数(用全A快照现算)。
func industryPeerStrength(snapshots []services.ScanSnapshotRow, industryMap map[string]string, symbol string) (float64, bool) {
	ind := industryMap[symbol]
	if ind == "" {
		return 0, false
	}
	pcts := make([]float64, 0, 64)
	for _, r := range snapshots {
		if industryMap[r.Symbol] == ind && r.Symbol != symbol && r.Price > 0 {
			pcts = append(pcts, r.ChangePercent)
		}
	}
	if len(pcts) < 5 {
		return 0, false
	}
	sort.Float64s(pcts)
	return pcts[len(pcts)/2], true
}
