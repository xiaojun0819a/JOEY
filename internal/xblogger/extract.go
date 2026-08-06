package xblogger

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Pick 从某一段里认出的一只票。
type Pick struct {
	Symbol   string   `json:"symbol"` // sh600519
	Name     string   `json:"name"`
	Kind     SegKind  `json:"kind"`   // 来自买入段还是持仓/卖出段
	Source   string   `json:"source"` // 哪个抽取器认出来的,便于事后查错
	RawLine  string   `json:"rawLine"`
	Warnings []string `json:"warnings,omitempty"`
}

var (
	// 名称(代码) —— 云舒交易日记「1: 创新医疗(002173)」,中英文括号都收
	reNameParenCode = regexp.MustCompile(`([\p{Han}][\p{Han}A-Za-z0-9]{1,7})\s*[(（]\s*(\d{6})\s*[)）]`)
	// 【名称】 —— 山野寻龙A股「明日建仓计划【紫金矿业】【中电鑫龙】」
	reBracketName = regexp.MustCompile(`[【\[]\s*([\p{Han}][\p{Han}A-Za-z0-9]{1,7})\s*[】\]]`)
	// 代码 名称 —— A股趋势捕手「603556 海兴电力」
	reCodeThenName = regexp.MustCompile(`(\d{6})\s+([\p{Han}][\p{Han}A-Za-z0-9]{1,7})`)
	// 极大数字串:防止从时间戳/指标数值里截出假代码(见 app_tip_picks.go 里同款注释)
	reDigitRun = regexp.MustCompile(`[0-9]+`)
)

// extractor 一个抽取器。按精度从高到低试,**第一个出结果的赢**——
// 精度高的写法自带名称+代码可以交叉校验,退到"纯名字包含"是最后手段。
type extractor struct {
	name string
	run  func(text string, cat *Catalog) []Pick
}

var extractors = []extractor{
	{"名称(代码)", extractNameParenCode},
	{"【名称】", extractBracketName},
	{"代码+名称", extractCodeThenName},
	{"裸代码", extractBareCode},
	{"名录匹配", extractByName},
}

// ExtractPicks 从一段文本里抽票。返回的 Pick 已去重(按代码),保持出现顺序。
func ExtractPicks(text string, cat *Catalog) []Pick {
	for _, ex := range extractors {
		if picks := ex.run(text, cat); len(picks) > 0 {
			return dedupPicks(picks)
		}
	}
	return nil
}

func dedupPicks(in []Pick) []Pick {
	seen := map[string]bool{}
	out := make([]Pick, 0, len(in))
	for _, p := range in {
		if p.Symbol == "" || seen[p.Symbol] {
			continue
		}
		seen[p.Symbol] = true
		out = append(out, p)
	}
	return out
}

// resolveNameAndCode 名称与代码的交叉校验 —— **名称优先**。
//
// 起因是真事:云舒交易日记 2026-07-29 写「顺钠股份(000523)」,而 000523 是红棉股份,
// 顺钠股份实为 000533(她自己贴的行情截图就是 000533)。信代码就买错票。
// 名字是人写给人看的、错了自己会发现;六位代码错一位没人看得出来。
func resolveNameAndCode(name, code6 string, cat *Catalog) (symbol, finalName string, warns []string) {
	name = strings.ReplaceAll(strings.TrimSpace(name), " ", "")
	symFromName, okName := cat.SymbolOf(name)
	nameFromCode, okCode := cat.NameOf(code6)

	switch {
	case okName && okCode:
		if symFromName != NormalizeCode(code6) {
			warns = append(warns, fmt.Sprintf("名称与代码不符:「%s」应为 %s,但原文写的 %s 是「%s」——已按名称建仓",
				name, symFromName, code6, nameFromCode))
		}
		return symFromName, name, warns
	case okName:
		if code6 != "" {
			warns = append(warns, fmt.Sprintf("原文代码 %s 不在名录里(可能已退市/写错),已按名称「%s」认定", code6, name))
		}
		return symFromName, name, warns
	case okCode:
		warns = append(warns, fmt.Sprintf("名称「%s」不在名录里,已按代码 %s 认定为「%s」", name, code6, nameFromCode))
		return NormalizeCode(code6), nameFromCode, warns
	}
	return "", name, append(warns, fmt.Sprintf("「%s」(%s)名称和代码都查不到,已跳过", name, code6))
}

func extractNameParenCode(text string, cat *Catalog) []Pick {
	out := []Pick{}
	for _, m := range reNameParenCode.FindAllStringSubmatch(text, 20) {
		sym, nm, warns := resolveNameAndCode(m[1], m[2], cat)
		if sym == "" {
			continue
		}
		out = append(out, Pick{Symbol: sym, Name: nm, Source: "名称(代码)", RawLine: m[0], Warnings: warns})
	}
	return out
}

func extractCodeThenName(text string, cat *Catalog) []Pick {
	out := []Pick{}
	for _, m := range reCodeThenName.FindAllStringSubmatch(text, 20) {
		sym, nm, warns := resolveNameAndCode(m[2], m[1], cat)
		if sym == "" {
			continue
		}
		out = append(out, Pick{Symbol: sym, Name: nm, Source: "代码+名称", RawLine: m[0], Warnings: warns})
	}
	return out
}

func extractBracketName(text string, cat *Catalog) []Pick {
	out := []Pick{}
	for _, m := range reBracketName.FindAllStringSubmatch(text, 20) {
		name := strings.TrimSpace(m[1])
		sym, ok := cat.SymbolOf(name)
		if !ok {
			continue // 【】里也可能是板块名/口号,查不到就不是票
		}
		out = append(out, Pick{Symbol: sym, Name: name, Source: "【名称】", RawLine: m[0]})
	}
	return out
}

func extractBareCode(text string, cat *Catalog) []Pick {
	out := []Pick{}
	for _, run := range reDigitRun.FindAllString(text, -1) {
		if len(run) != 6 {
			continue
		}
		name, ok := cat.NameOf(run)
		if !ok {
			continue
		}
		out = append(out, Pick{Symbol: NormalizeCode(run), Name: name, Source: "裸代码", RawLine: run})
	}
	return out
}

// extractByName 纯名字博主(走上大A巅峰/老林A股/A股老枪)的兜底:全市场名录 Contains。
//
// 两个必须的防护:
//   - 名录**长名在前**(见 Catalog),且命中后把该名字从文本里抹掉,
//     否则一个长名字里嵌着的短名字会被第二只票重复认领。
//   - 只认 ≥3 字的名字。两字名(如"金字")在中文里太容易撞进日常用语,
//     而这里的文本是自然语言散文,不是结构化字段。
func extractByName(text string, cat *Catalog) []Pick {
	if cat == nil {
		return nil
	}
	work := strings.ReplaceAll(text, " ", "")
	type hit struct {
		at   int
		pick Pick
	}
	hits := []hit{}
	for _, ns := range cat.sorted {
		if len([]rune(ns.Name)) < 3 {
			continue
		}
		idx := strings.Index(work, ns.Name)
		if idx < 0 {
			continue
		}
		hits = append(hits, hit{idx, Pick{Symbol: ns.Symbol, Name: ns.Name, Source: "名录匹配", RawLine: ns.Name}})
		// 抹掉已命中的名字,避免"长名内嵌短名"被重复认领。
		// 填充等长的 NUL 而不是删除:后面的名字要靠字节位置排序,长度一变位置就全错。
		work = strings.ReplaceAll(work, ns.Name, strings.Repeat("\x00", len(ns.Name)))
		if len(hits) >= 20 {
			break
		}
	}
	// 名录是按"名称长度降序"遍历的,命中顺序跟原文顺序无关。
	// 推送和建仓都要按博主写的顺序来(第一只往往是他的主推),所以这里按出现位置还原。
	sort.Slice(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	out := make([]Pick, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.pick)
	}
	return out
}
