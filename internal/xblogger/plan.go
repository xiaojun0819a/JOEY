package xblogger

import (
	"regexp"
	"strconv"
	"strings"
)

// Plan 帖子自带的交易计划。目前只有云舒交易日记会写全,其余博主只给票不给价。
//
// 为什么单独存而不直接拿去当风控参数:六个博主里只有一个给计划,
// 如果各用各的出场规则,最后比出来的是**出场规则的差异**,不是选股能力的差异。
// 所以建仓统一用一套固定出场,她这套参数另开一条对照线跑,正好能测出"她的止盈止损值不值钱"。
type Plan struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	EntryLow  float64 `json:"entryLow"`  // 低吸区间下沿
	EntryHigh float64 `json:"entryHigh"` // 低吸区间上沿 —— 开盘价高于它,按她自己的规矩就该观望
	TP1       float64 `json:"tp1"`
	TP2       float64 `json:"tp2"`
	Stop      float64 `json:"stop"`
	Raw       string  `json:"raw"`
}

var (
	// 「1: 创新医疗(002173)」——按编号切块,每只票各自的计划才不会串
	reItemSplit = regexp.MustCompile(`(?m)^\s*(\d{1,2})\s*[:：、.]\s*`)
	// 「19.75—20.20 元区间分批低吸」:各种破折号都收。
	// 后面必须跟低吸/回踩类的词 —— 否则技术面里的价格区间也会被当成入场区间。
	reEntryRange = regexp.MustCompile(`(\d+\.\d{2})\s*[—–\-~－至到]\s*(\d+\.\d{2})\s*元[^\n]{0,16}?(低吸|回踩|布局|建仓|介入|进场)`)

	// 价格一律要求**两位小数 + 后缀元**。两个坑都是实测出来的:
	//   ① 「有效跌破 10 日均线 18.99 元」里的 10 是均线周期,不是价格;
	//   ② 成交量「118.048 万手」写成三位小数,只匹配 \d+\.\d{2} 会截出假价格 118.04。
	// 要求紧跟「元」两个问题一起解决。
	reTP1 = regexp.MustCompile(`第一止盈[^0-9\n]{0,12}(\d+\.\d{2})`)
	reTP2 = regexp.MustCompile(`第二止盈[^0-9\n]{0,12}(\d+\.\d{2})`)
	// 止损锚在「跌破」而不是「止损」二字:她有时写「硬性止损点位:有效跌破…」,
	// 有时写「有效跌破 12.80 元…无条件短线止损离场」(价格在"止损"之前)。
	// 而以「止损」起锚会先撞上规划操作里的「回避冲高回落的止损风险」,把第一止盈价当成止损价。
	reStop = regexp.MustCompile(`跌破[^\n]{0,24}?(\d+\.\d{2})\s*元`)
)

// ExtractPlans 从原文里给每只买入票配上它自己的交易计划。
// 按编号块切分后,块里出现哪只票,计划就归谁;切不出编号块就整段找一次。
func ExtractPlans(text string, buys []Pick) []Plan {
	if len(buys) == 0 {
		return nil
	}
	blocks := splitNumbered(text)
	out := []Plan{}
	for _, b := range buys {
		blk := text
		for _, cand := range blocks {
			if strings.Contains(strings.ReplaceAll(cand, " ", ""), b.Name) {
				blk = cand
				break
			}
		}
		p := Plan{Symbol: b.Symbol, Name: b.Name}
		if m := reEntryRange.FindStringSubmatch(blk); m != nil {
			p.EntryLow, _ = strconv.ParseFloat(m[1], 64)
			p.EntryHigh, _ = strconv.ParseFloat(m[2], 64)
		}
		if m := reTP1.FindStringSubmatch(blk); m != nil {
			p.TP1, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reTP2.FindStringSubmatch(blk); m != nil {
			p.TP2, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reStop.FindStringSubmatch(blk); m != nil {
			p.Stop, _ = strconv.ParseFloat(m[1], 64)
		}
		if p.EntryLow == 0 && p.TP1 == 0 && p.Stop == 0 {
			continue // 一个数都没抠到就别记,免得下游拿 0 当价格
		}
		p.Raw = trimRunes(blk, 400)
		out = append(out, p)
	}
	return out
}

func splitNumbered(text string) []string {
	locs := reItemSplit.FindAllStringIndex(text, -1)
	if len(locs) < 2 {
		return nil
	}
	out := make([]string, 0, len(locs))
	for i, l := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, text[l[0]:end])
	}
	return out
}

func trimRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
