package xblogger

import (
	"regexp"
	"strings"
)

// SegKind 段落语义。
type SegKind string

const (
	SegNeutral SegKind = ""     // 未分类正文(行情点评、口号、免责声明)
	SegBuy     SegKind = "buy"  // 明日建仓段 —— 只有这里的票才建仓
	SegHold    SegKind = "hold" // 已持仓段(新上车/新进/今日操作)—— 记录,不建仓
	SegExit    SegKind = "exit" // 卖出段(止损离场/清仓)—— 记录为退出信号
)

// segMarker 一个段标题标记。
type segMarker struct {
	re   *regexp.Regexp
	kind SegKind
}

// 段标记表。**长匹配优先**,这是防串味的关键:
// 「新建仓」含「建仓」、「止损离场」含「离场」,若按先后顺序匹配会把持仓段/止损段误判成买入段。
// 实现上对每行取所有命中里最长的那个,所以这里的先后顺序不影响结果。
var segMarkers = []segMarker{
	// —— 卖出 ——
	{regexp.MustCompile(`止损离场`), SegExit},
	{regexp.MustCompile(`今日离场`), SegExit},
	{regexp.MustCompile(`清仓离场`), SegExit},
	{regexp.MustCompile(`离场`), SegExit},
	{regexp.MustCompile(`清仓`), SegExit},
	{regexp.MustCompile(`减仓`), SegExit},
	{regexp.MustCompile(`兑现`), SegExit},
	{regexp.MustCompile(`卖出`), SegExit},

	// —— 已持仓(最容易被误当成买点的一类)——
	{regexp.MustCompile(`持仓同步更新`), SegHold},
	{regexp.MustCompile(`周[一二三四五六日]新上车`), SegHold},
	{regexp.MustCompile(`新上车`), SegHold},
	{regexp.MustCompile(`新建仓`), SegHold},
	{regexp.MustCompile(`今日持仓`), SegHold},
	{regexp.MustCompile(`持仓观点`), SegHold},
	{regexp.MustCompile(`持仓维持`), SegHold},
	{regexp.MustCompile(`今日操作`), SegHold},
	{regexp.MustCompile(`新进`), SegHold},

	// —— 明日建仓 ——
	{regexp.MustCompile(`建仓目标`), SegBuy},
	{regexp.MustCompile(`建仓计划`), SegBuy},
	{regexp.MustCompile(`建仓密码`), SegBuy},
	{regexp.MustCompile(`建仓安排`), SegBuy},
	{regexp.MustCompile(`明日提前预告`), SegBuy},
	{regexp.MustCompile(`明日参考`), SegBuy},
	{regexp.MustCompile(`明日重点`), SegBuy},
	{regexp.MustCompile(`潜力票`), SegBuy},
	{regexp.MustCompile(`(明天|明日|次日)[（(]?[^）)\n]{0,8}[）)]?建仓`), SegBuy},
	{regexp.MustCompile(`下?周[一二三四五]建仓`), SegBuy},
	{regexp.MustCompile(`布局提前公开`), SegBuy},
	{regexp.MustCompile(`重点关注`), SegBuy},
}

// headerMaxRunes 无冒号结尾时,一行最多多少字才可能是段标题。
//
// 存在的意义:云舒交易日记的正文里有「第二止盈目标 23.80 元,抵达前期震荡高位压力位后全部清仓离场」
// 这样的散文句——它含「清仓」「离场」两个卖出标记。若不限长度,这一句会被当成卖出段标题,
// 把它后面所有内容都染成"卖出",整条推文报废。真正的段标题都很短(「🎯建仓目标:」6 字)。
const headerMaxRunes = 20

// lineSegKind 判定一行是不是段标题;返回它开启的段类型。不是标题则返回 SegNeutral,false。
func lineSegKind(line string) (SegKind, bool) {
	s := strings.TrimSpace(line)
	if s == "" {
		return SegNeutral, false
	}
	trimmed := strings.TrimRight(s, " ")
	endsWithColon := strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：")
	if !endsWithColon && len([]rune(s)) > headerMaxRunes {
		return SegNeutral, false
	}
	best, bestLen := SegNeutral, 0
	for _, m := range segMarkers {
		loc := m.re.FindString(s)
		if loc == "" {
			continue
		}
		if n := len([]rune(loc)); n > bestLen {
			best, bestLen = m.kind, n
		}
	}
	if bestLen == 0 {
		return SegNeutral, false
	}
	return best, true
}

// 句子级的卖出语义词。与段标记表分开维护:段标记要求出现在**短标题行**上,
// 这些则用在散文句子里。
var sellWordsRe = regexp.MustCompile(`卖出|清仓|止损|离场|减仓|兑现|割肉|出局|已出|清掉`)

// 句子切分:中文标点 + 换行。分号也算 —— 博主爱用分号串一长串操作。
var sentenceSplitRe = regexp.MustCompile(`[。！？!?\n；;]+`)

// SplitSellSentences 把含卖出语义的句子从买入池里摘出来。
//
// 为什么段落切分还不够:段标题必须是短行(见 headerMaxRunes),
// 而博主经常在**大白话长句**里说卖出 —— 实测云舒交易日记 2026-07-30:
//
//	「深科技昨天我就通知卖出了,后面还有喷子在我评论区下面说什么今天必涨…」
//
// 这句 30 多字、没冒号,当然不是段标题;而她那条推文又没有任何建仓段,
// 于是整贴兜底把它当成了买入池 —— **把她昨天就喊卖的票买进来**。
// 段落管结构,这里管句子,两层都要有。
func SplitSellSentences(text string) (keep, sell string) {
	var kb, sb []string
	for _, s := range sentenceSplitRe.Split(text, -1) {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if sellWordsRe.MatchString(s) {
			sb = append(sb, s)
			continue
		}
		kb = append(kb, s)
	}
	return strings.Join(kb, "\n"), strings.Join(sb, "\n")
}

// Segment 一段连续文本及其语义。
type Segment struct {
	Kind   SegKind
	Header string   // 段标题原文(可能为空 = 开头的无标题部分)
	Lines  []string // 含标题行本身 —— 山野寻龙把票直接写在标题行上(明日建仓计划【紫金矿业】【中电鑫龙】)
}

// Text 段落全文。
func (s Segment) Text() string { return strings.Join(s.Lines, "\n") }

// SplitSegments 按段标题把推文切开。
// 标题行本身归入它开启的那一段(必须如此:山野寻龙的票就写在标题行里)。
func SplitSegments(text string) []Segment {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	segs := []Segment{}
	cur := Segment{Kind: SegNeutral}
	for _, ln := range lines {
		if kind, ok := lineSegKind(ln); ok {
			if len(cur.Lines) > 0 {
				segs = append(segs, cur)
			}
			cur = Segment{Kind: kind, Header: strings.TrimSpace(ln), Lines: []string{ln}}
			continue
		}
		cur.Lines = append(cur.Lines, ln)
	}
	if len(cur.Lines) > 0 {
		segs = append(segs, cur)
	}
	return segs
}
