package main

// 外部荐股跟单验证(2026-07-27 建,用户定名「天鼎早盘推荐」)。
//
// 用途:用户订阅的荐股服务每天 09:29 群发 5-6 只票,他想按「开盘买入 → 次日冲高卖出」跟单,
// 并用模拟持仓累积**真实前向样本**——因为卖方给的战绩截图是他们自己挑的一周,证明不了任何事。
//
// 三条定死的纪律(都是为了让样本可信,别为了好看去动):
//
// ① **记账价 = 提交时刻的实时价,不是当日开盘价。**
//    用户口头说"按开盘价或31分实时价",但如果 09:31 提交却按 09:30 开盘价记账,
//    等于白捡了这一分钟的涨跌——这正是本项目栽过的前视偏差(见 canAutoBuyNow 注释里的
//    哈药股份实例:凌晨按昨收建仓、白捡次日跳空,胜率系统性虚高)。
//    结果里另外回传当日开盘价 DayOpen 供对照,让用户自己看见"想买的价"和"真能买的价"差多少。
//
// ② **只在交易时段记账(canAutoBuyNow:交易日 09:25-15:00)。** 收盘后"按收盘价买入"现实不可成交,
//    且是拿已知结果当成交价。要补录特殊情况,走原有的「新增手动持仓」。
//
// ③ **涨停封死不记(isBuyableNow)。** 盘口无卖盘还记一笔,等于自欺。
//
// 另:本来源建仓即置 auto_exit_off=1,**风控引擎一律不碰**。
// 因为这次实验要测的是用户自己"冲高就卖"的手感——引擎替他卖了,测的就不是他的手了。

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// tipPickSource 来源 ID。ID 是数据唯一键不可改;界面名在前端 STRATEGY_SOURCE_LABELS 里。
const tipPickSource = "tip-tianding"

// 从任意文本里抠 6 位股票代码:短信/微信原文会带名字、日期、广告语,全部忽略只认数字。
//
// 取**极大数字串**再判长度==6,而不是直接匹配 `\d{6}`:
//   - 直接 `\d{6}` 会从 "20260727093022" 这种时间戳里截出假代码;
//   - 用 `(^|\D)(\d{6})(\D|$)` 夹住看似能防,但边界会被前一个匹配吃掉——
//     "600584,002167,002879" 只认得出第 1、3 个,中间那个的前导逗号已被消费(实测踩到)。
//
// 极大串写法两个问题都没有:14 位时间戳是一整串(长度≠6 被弃),逗号分隔的三个各自成串。
var tipDigitRunRe = regexp.MustCompile(`[0-9]+`)

// TipPickRow 单只票的处理结果。没记进去的 Skipped 非空,如实回传原因。
type TipPickRow struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`   // 实际记账价 = 提交时刻实时价
	DayOpen float64 `json:"dayOpen"` // 当日开盘价,仅供对照,不参与记账
	Shares  int64   `json:"shares"`
	Amount  float64 `json:"amount"` // 实际占用金额(整手取整后)
	Skipped string  `json:"skipped"`
}

type TipPickResult struct {
	Date    string       `json:"date"`
	At      string       `json:"at"` // 提交时刻,记账价就是这一刻的价
	Source  string       `json:"source"`
	Note    string       `json:"note"`
	Rows    []TipPickRow `json:"rows"`
	Added   int          `json:"added"`
	Skipped int          `json:"skipped"`
	Warning string       `json:"warning"`
}

// parseTipCodes 从原文里提取去重后的规范代码,保持出现顺序。
func parseTipCodes(raw string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, run := range tipDigitRunRe.FindAllString(raw, -1) {
		if len(run) != 6 {
			continue
		}
		sym := normalizeJournalStockSymbol(run)
		if len(sym) != 8 || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, sym)
	}
	return out
}

// AddTipPicks 把一批荐股按每只固定金额建进模拟持仓。
// raw=短信/消息原文(只认里面的 6 位代码);amountPerStock=每只投入金额(0 则默认 3 万);note=备注(哪家推的)。
func (a *App) AddTipPicks(raw string, amountPerStock float64, note string) (*TipPickResult, error) {
	if a.paperService == nil || a.marketService == nil {
		return nil, fmt.Errorf("服务未初始化")
	}
	if amountPerStock <= 0 {
		amountPerStock = 30000
	}
	codes := parseTipCodes(raw)
	if len(codes) == 0 {
		return nil, fmt.Errorf("没从文本里认出任何 6 位股票代码")
	}
	if len(codes) > 20 {
		return nil, fmt.Errorf("一次最多 20 只,认出了 %d 只,请检查文本里是否混进了别的数字", len(codes))
	}
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	if !a.canAutoBuyNow() {
		return nil, fmt.Errorf("当前不在可成交时段(需交易日 09:25-15:00)。现在记账要么拿昨收当买价、要么拿已知收盘价当买价,都是自欺;补录请走「新增手动持仓」")
	}

	res := &TipPickResult{
		Date:   now.Format("2006-01-02"),
		At:     now.Format("2006-01-02 15:04:05"),
		Source: tipPickSource,
		Note:   strings.TrimSpace(note),
	}

	rt, err := a.marketService.GetStockRealTimeData(codes...)
	if err != nil {
		return nil, fmt.Errorf("取实时行情失败:%w", err)
	}
	quote := map[string]int{}
	for i, s := range rt {
		quote[strings.ToLower(strings.TrimSpace(s.Symbol))] = i
	}

	// 已在手的同源持仓:同一天重复提交不重复建仓(手滑点两次不该变成双倍仓位)
	held := map[string]bool{}
	for _, p := range a.paperService.OpenPositions() {
		if p.Source == tipPickSource {
			held[strings.ToLower(strings.TrimSpace(p.Symbol))] = true
		}
	}

	for _, sym := range codes {
		row := TipPickRow{Symbol: sym}
		idx, ok := quote[sym]
		if !ok {
			row.Skipped = "取不到行情(代码可能不存在)"
			res.Rows = append(res.Rows, row)
			res.Skipped++
			continue
		}
		st := rt[idx]
		row.Name, row.Price, row.DayOpen = st.Name, st.Price, st.Open
		switch {
		case held[sym]:
			row.Skipped = "该源已有未平仓持仓,不重复建仓"
		case st.Price <= 0:
			row.Skipped = "实时价为 0,无法记账"
		default:
			if ok, why := a.isBuyableNow(sym, st.Price, st.ChangePercent); !ok {
				row.Skipped = "买不进:" + why
				break
			}
			shares := int64(amountPerStock/(st.Price*100)) * 100
			if shares <= 0 {
				row.Skipped = fmt.Sprintf("%.0f 元买不起 1 手(现价 %.2f)", amountPerStock, st.Price)
				break
			}
			id, err := a.paperService.Add(sym, st.Name, tipPickSource, st.Price, shares, "manual")
			if err != nil {
				row.Skipped = "落库失败:" + err.Error()
				break
			}
			// 纪律③:本来源永不由引擎平仓,建仓即接管
			_ = a.paperService.SetAutoExitOff(id, true)
			row.Shares, row.Amount = shares, float64(shares)*st.Price
			res.Added++
		}
		if row.Skipped != "" {
			res.Skipped++
		}
		res.Rows = append(res.Rows, row)
	}

	if res.Added > 0 {
		res.Warning = "记账价=提交时刻实时价(非开盘价),卖出请自行操作——风控引擎对本来源一律不接管"
	}
	log.Info("荐股建仓[%s] 记入%d 跳过%d @%s", res.Note, res.Added, res.Skipped, res.At)
	return res, nil
}
