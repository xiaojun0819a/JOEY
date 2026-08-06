package xblogger

import (
	"fmt"
	"strings"
	"time"
)

// Config 一个博主的解析配置。
type Config struct {
	Key     string // 内部键,也用作模拟持仓的分组名
	Handle  string // @句柄(不带 @)
	Display string // 中文昵称

	// WholePostFallback 切不出买入段时,是否可以把「中性段」(即没有任何持仓/卖出标题的部分)整段当买入。
	//
	// ⚠️只给"整贴就是推票、从不在同一条里写持仓和止损"的博主开。
	// 对会混写的博主(老林/老枪/走上大A巅峰)必须关掉:切不出买入段时宁可 0 只 + 告警,
	// 也不能退回整贴猜——那正是会把「❌止损离场:华天科技」买进来的路径。
	// 注意即使开着,也只回退到中性段,持仓段/卖出段永远排除在外。
	WholePostFallback bool

	// HasTradePlan 帖子自带入场区间/止盈/止损(目前只有云舒交易日记)。
	HasTradePlan bool

	// MaxPicks 一条推文最多认几只。超出即视为解析跑偏,全部作废等人工看。
	MaxPicks int

	// AutoBuy 灰度开关:false = 只推送不建仓。新接入的博主一律先 false 跑两周。
	AutoBuy bool

	// Aliases 手动录入时可以用的简称(除 Key/Handle/Display 外)。
	// 手打全名太累,而"老枪""云舒"这类简称是用户自己嘴里的叫法。
	Aliases []string

	Note string
}

// Configs 六个博主的配置。Key 一旦用于建仓分组就不可改(它是数据里的分组名)。
//
// 每个人的"必须排除段"不写在这里 —— segMarkers 是全局共用的,谁写了持仓/止损标题谁就被切开。
// 这样某个博主哪天新增一种段落写法,六个人一起受益,不用逐个改配置。
var Configs = []Config{
	{
		Key: "x-dianfeng", Aliases: []string{"巅峰", "走上大A", "大A巅峰"}, Handle: "GusQuijasTJ", Display: "走上大A巅峰",
		MaxPicks: 6, Note: "明日提前预告/明日参考 是买入段;持仓观点段必须排除(实测一条里 5 只只有 2 只是买)",
	},
	{
		Key: "x-qushi", Aliases: []string{"趋势捕手", "捕手", "趋势"}, Handle: "Aw3ff_", Display: "A股趋势捕手",
		WholePostFallback: true, MaxPicks: 4,
		Note: "写法「603556 海兴电力」代码+名,整贴即推票,未见持仓/止损段",
	},
	{
		Key: "x-laolin", Aliases: []string{"老林", "老林A股"}, Handle: "Ferhat31162", Display: "老林A股-寻找主线",
		MaxPicks: 6, Note: "周X建仓密码 是买入段;新建仓/止损离场 段必须排除(华天科技就是从止损段里混出来的)",
	},
	{
		Key: "x-shanye", Aliases: []string{"山野寻龙", "寻龙", "山野"}, Handle: "ComMurtadha", Display: "山野寻龙A股",
		MaxPicks: 4, Note: "【名称】写法,票直接写在「明日建仓计划」标题行上",
	},
	{
		Key: "x-laoqiang", Aliases: []string{"老枪", "A股老枪"}, Handle: "shachoo_king", Display: "A股老枪",
		MaxPicks: 6, Note: "建仓目标 是买入段;新上车/今日离场 必须排除。标题星期需与发帖星期比对(周二发「周二建仓安排」是复盘)",
	},
	{
		Key: "x-yunshu", Aliases: []string{"云舒", "云舒日记"}, Handle: "naixiaiwangu", Display: "云舒交易日记",
		WholePostFallback: true, HasTradePlan: true, MaxPicks: 4,
		Note: "「N: 名称(代码)」+ 完整入场区间/止盈/止损。⚠️她写过错代码(顺钠股份写成 000523),靠名称纠正",
	},
}

// ConfigByHandle 按 @句柄取配置。
func ConfigByHandle(handle string) (Config, bool) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	for _, c := range Configs {
		if strings.ToLower(c.Handle) == h {
			return c, true
		}
	}
	return Config{}, false
}

// ConfigByKey 按内部键取配置。
func ConfigByKey(key string) (Config, bool) {
	for _, c := range Configs {
		if c.Key == key {
			return c, true
		}
	}
	return Config{}, false
}

// Parsed 一条推文的解析结果。
type Parsed struct {
	Blogger  string     `json:"blogger"` // Config.Key
	Handle   string     `json:"handle"`
	Display  string     `json:"display"`
	PostedAt string     `json:"postedAt"`
	Target   TargetDate `json:"target"`

	Buys  []Pick `json:"buys"`  // 明日建仓 —— 只有这些会进模拟盘
	Holds []Pick `json:"holds"` // 他已经持有的,记录用
	Exits []Pick `json:"exits"` // 他卖掉的,记录用(也用来给之前建的仓打上"作者已离场")

	Warnings    []string `json:"warnings,omitempty"`
	NeedsReview bool     `json:"needsReview"` // 有告警/数量异常/日期靠猜 → 人工看一眼再放行
	Segments    int      `json:"segments"`
	Plans       []Plan   `json:"plans,omitempty"` // 帖子自带的交易计划(目前只有云舒)
}

// Parse 解析一条推文。postedAt 必须是**东八区**的发帖时间(日期判定全靠它)。
func Parse(cfg Config, text string, postedAt time.Time, cat *Catalog) Parsed {
	out := Parsed{
		Blogger:  cfg.Key,
		Handle:   cfg.Handle,
		Display:  cfg.Display,
		PostedAt: postedAt.Format("2006-01-02 15:04"),
	}

	segs := SplitSegments(text)
	out.Segments = len(segs)

	var buyParts, holdParts, exitParts, neutralParts, headers []string
	for _, s := range segs {
		switch s.Kind {
		case SegBuy:
			buyParts = append(buyParts, s.Text())
			headers = append(headers, s.Header)
		case SegHold:
			holdParts = append(holdParts, s.Text())
		case SegExit:
			exitParts = append(exitParts, s.Text())
		default:
			neutralParts = append(neutralParts, s.Text())
		}
	}

	buyText := strings.Join(buyParts, "\n")
	if strings.TrimSpace(buyText) == "" {
		if cfg.WholePostFallback {
			// 只回退到中性段:持仓段和卖出段无论如何都不能进买入池
			buyText = strings.Join(neutralParts, "\n")
		} else if len(holdParts)+len(exitParts) > 0 {
			out.Warnings = append(out.Warnings,
				"没切出买入段,但有持仓/卖出段 —— 这条多半只是复盘,已按 0 只处理(该博主不允许整贴兜底)")
		} else {
			out.Warnings = append(out.Warnings, "没切出买入段,也没有可辨认的段标题 —— 需人工确认格式是否变了")
		}
	}

	// 句子级复查:买入池里凡是含"卖出/清仓/止损/离场"的句子,整句挪进卖出池。
	// 段落切分只认短标题行,拦不住藏在大白话长句里的卖出(见 SplitSellSentences 的实例)。
	buyText, sellSentences := SplitSellSentences(buyText)
	if strings.TrimSpace(sellSentences) != "" {
		exitParts = append(exitParts, sellSentences)
	}

	out.Buys = tag(ExtractPicks(buyText, cat), SegBuy)
	out.Holds = tag(ExtractPicks(strings.Join(holdParts, "\n"), cat), SegHold)
	out.Exits = tag(ExtractPicks(strings.Join(exitParts, "\n"), cat), SegExit)

	// 同一只票既在买入池又在卖出池:**按抽取精度判谁说了算**。
	//
	// 一刀切"以卖出为准"看着保险,实测会误杀:云舒交易日记的推票帖里,
	// 票写在编号行「2:顺钠股份(000523)」上(高精度、名代码齐全),
	// 而正文另一句复盘顺带提了它一嘴带"止盈"字样 —— 结果她当天的正经推荐被判成卖出。
	//
	// 分界线是抽取器:名称(代码)/【名称】/代码+名称 这三种是**博主的结构化推荐格式**,
	// 只有他真要推一只票才会这么写;而"名录匹配"是从散文里捞出来的名字,分量轻得多。
	// 所以高精度格式里的买入压得住散文里的卖出;散文对散文,还是卖出优先(买错比漏买贵)。
	exitSet := map[string]bool{}
	for _, p := range out.Exits {
		exitSet[p.Symbol] = true
	}
	kept := out.Buys[:0]
	for _, p := range out.Buys {
		if exitSet[p.Symbol] {
			if p.Source == "名录匹配" {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("「%s」在散文里同时被提到买和卖,已按卖出处理不建仓", p.Name))
				continue
			}
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("「%s」写在正式推荐格式里(%s),但正文别处也提到卖出 —— 已按买入处理,建议人工看一眼",
					p.Name, p.Source))
		}
		kept = append(kept, p)
	}
	out.Buys = kept

	// 日期线索:只取开头两行 + 买入段标题。
	// 绝不能拿全文 —— 云舒的技术面正文里有「截至 7 月 29 日收盘」,会把目标日拽回昨天。
	hintLines := firstNonEmptyLines(text, 2)
	out.Target = ResolveTargetDate(strings.Join(append(hintLines, headers...), "\n"), postedAt)

	if cfg.HasTradePlan {
		out.Plans = ExtractPlans(text, out.Buys)
	}

	if out.Target.SameDay {
		out.Warnings = append(out.Warnings,
			"目标日=发帖当天 → 这是当日复盘不是明日预告,不建仓("+out.Target.Basis+")")
		out.Buys = nil
	}
	if out.Target.Guessed && len(out.Buys) > 0 {
		out.Warnings = append(out.Warnings, "原文没写目标日期,按次日推定 —— 需人工确认")
	}
	if cfg.MaxPicks > 0 && len(out.Buys) > cfg.MaxPicks {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("认出 %d 只,超过该博主常态上限 %d —— 判为解析跑偏,本条全部作废", len(out.Buys), cfg.MaxPicks))
		out.Buys = nil
	}
	for _, p := range out.Buys {
		out.Warnings = append(out.Warnings, p.Warnings...)
	}
	out.NeedsReview = len(out.Warnings) > 0
	return out
}

func tag(in []Pick, k SegKind) []Pick {
	for i := range in {
		in[i].Kind = k
	}
	return in
}

func firstNonEmptyLines(text string, n int) []string {
	out := []string{}
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
		if len(out) >= n {
			break
		}
	}
	return out
}

// ConfigByAlias 按任意叫法找博主:内部键 / @句柄 / 中文全名 / 简称,都认。
// 手动录入时用户不会打全名,「老枪」「云舒」才是他嘴里的叫法。
func ConfigByAlias(s string) (Config, bool) {
	k := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@")))
	if k == "" {
		return Config{}, false
	}
	for _, c := range Configs {
		if strings.ToLower(c.Key) == k || strings.ToLower(c.Handle) == k || strings.ToLower(c.Display) == k {
			return c, true
		}
		for _, a := range c.Aliases {
			if strings.ToLower(a) == k {
				return c, true
			}
		}
	}
	return Config{}, false
}

// AliasHint 给用户看的"可以怎么写"提示。
func AliasHint() string {
	var b strings.Builder
	for _, c := range Configs {
		b.WriteString(c.Display)
		if len(c.Aliases) > 0 {
			b.WriteString("(" + strings.Join(c.Aliases, "/") + ")")
		}
		b.WriteString("  ")
	}
	return strings.TrimSpace(b.String())
}
