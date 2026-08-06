// Package xblogger 解析 X(推特)荐股博主的动态原文,抽出「明天要买的票」。
//
// 为什么不能把整条推文当一坨文本抽代码/名字(这是本包存在的唯一理由):
//
//	实测六个博主,**四个的推文里同时躺着"要买的"和"绝不能买的"**。最狠的一例是
//	老林A股 2026-07-29 那条:「📢周四建仓密码:✅岩山科技 ✅徐工机械」后面紧跟着
//	「❌止损离场:✅华天科技」——无脑抽会把他当天刚割肉的华天科技买进模拟盘。
//	A股老枪同理,一条推文里 7 只票只有 3 只是明日建仓,另外 4 只是已持仓和已卖出。
//
// 所以流程固定为:切段 → 给每段贴标签(买入/持仓/卖出)→ **只从买入段抽票**。
//
// 三条从实测里长出来的硬规则,改动前先看清代价:
//   - ✅/❌ 这类符号**不能当判据**。老枪的 ✅ 既用在"计划建仓"也用在"今日新上车",
//     老林在「止损离场」标题下面跟的也是 ✅。只有段标题算数。
//   - 名称与代码冲突时**以名称为准**。云舒交易日记 2026-07-29 把顺钠股份写成 (000523),
//     而 000523 是红棉股份、顺钠股份实为 000533(她自己配的截图就是 000533)。
//     照代码买就买错票。
//   - 标题里的星期要**跟发帖日的星期比**。老枪 07-28(周二)发「周二建仓安排」是当日复盘,
//     07-26(周日)发「周一建仓」才是明日预告。前者建仓 = 拿已知涨跌当买点。
package xblogger

import (
	"sort"
	"strings"
)

// NameSym 一条(名称,带前缀代码)。
type NameSym struct {
	Name   string // 已去空格
	Symbol string // sh600519 / sz000001 / bj430047
}

// Catalog 全市场名录。名称匹配靠 Contains,故 sorted 必须**长名在前**——
// 否则「深科技」会先于某个包含它的更长名字命中,把票认错。
type Catalog struct {
	byName map[string]string
	byCode map[string]string // 6 位数字 → 名称
	sorted []NameSym
}

// NewCatalog 建名录。pairs 顺序无所谓,内部会按名称长度降序重排。
func NewCatalog(pairs []NameSym) *Catalog {
	c := &Catalog{
		byName: make(map[string]string, len(pairs)),
		byCode: make(map[string]string, len(pairs)),
		sorted: make([]NameSym, 0, len(pairs)),
	}
	for _, p := range pairs {
		name := strings.ReplaceAll(strings.TrimSpace(p.Name), " ", "")
		sym := strings.ToLower(strings.TrimSpace(p.Symbol))
		if name == "" || sym == "" {
			continue
		}
		c.byName[name] = sym
		c.byCode[digitsOf(sym)] = name
		c.sorted = append(c.sorted, NameSym{Name: name, Symbol: sym})
	}
	sort.Slice(c.sorted, func(i, j int) bool {
		li, lj := len([]rune(c.sorted[i].Name)), len([]rune(c.sorted[j].Name))
		if li != lj {
			return li > lj
		}
		return c.sorted[i].Symbol < c.sorted[j].Symbol
	})
	return c
}

// SymbolOf 名称 → 带前缀代码。
func (c *Catalog) SymbolOf(name string) (string, bool) {
	if c == nil {
		return "", false
	}
	s, ok := c.byName[strings.ReplaceAll(strings.TrimSpace(name), " ", "")]
	return s, ok
}

// NameOf 6 位代码 → 名称。
func (c *Catalog) NameOf(code6 string) (string, bool) {
	if c == nil {
		return "", false
	}
	n, ok := c.byCode[strings.TrimSpace(code6)]
	return n, ok
}

// digitsOf 取出代码里的 6 位数字部分。
func digitsOf(sym string) string {
	var b strings.Builder
	for _, r := range sym {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeCode 6 位裸代码 → 带交易所前缀。
// 60/68/50/51/58/59/9 → sh;4/8 → bj(北交所);其余 → sz。
func NormalizeCode(code6 string) string {
	code6 = strings.TrimSpace(code6)
	if len(code6) != 6 {
		return ""
	}
	switch code6[0] {
	case '6', '5', '9':
		return "sh" + code6
	case '4', '8':
		return "bj" + code6
	default:
		return "sz" + code6
	}
}
