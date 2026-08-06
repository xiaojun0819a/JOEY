package xblogger

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reExplicitDate = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*日?`)
	reNextWeekDay  = regexp.MustCompile(`下周([一二三四五])`)
	// 「下周」但没跟星期几。必须排在 reNextWeekDay 之后判,否则「下周一」会被它先吃掉。
	reNextWeekPlain = regexp.MustCompile(`下周`)
	reThisWeekDay   = regexp.MustCompile(`周([一二三四五])`)
	// 刻意不含「盘前」:博主结尾的套话「盘前继续分享交易思路」跟目标日期无关,会误触发。
	reTomorrow = regexp.MustCompile(`明天|明日|次日`)
)

var cnWeekday = map[string]int{"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6, "日": 7, "天": 7}

// isoWeekday 周一=1 … 周日=7。
func isoWeekday(t time.Time) int {
	if w := int(t.Weekday()); w == 0 {
		return 7
	} else {
		return w
	}
}

// TargetDate 目标交易日的判定结果。
type TargetDate struct {
	Date    string `json:"date"`    // YYYY-MM-DD(自然日,还没对齐交易日)
	Basis   string `json:"basis"`   // 判定依据,人话
	SameDay bool   `json:"sameDay"` // 标题指的就是发帖当天 → 复盘,不是预告
	Guessed bool   `json:"guessed"` // 文里没给任何时间线索,按"下一天"猜的
}

// ResolveTargetDate 从标题/段标题里判定"这批票是哪天买"。
//
// 顺序即优先级:显式日期 > 下周X > 明天 > 周X > 猜。
//
// **「周X」必须跟发帖日的星期比**,这是本函数存在的主要理由:
// A股老枪 2026-07-28(周二)发「🔥周二建仓安排:」,那是当天收盘后的复盘——
// 当成明日预告去建仓,等于拿已经知道的当日涨跌当买点(本项目栽过的前视偏差)。
// 同一个人 2026-07-26(周日)发「周一建仓:」才是真预告。两条推文长得一模一样,
// 只有"标题星期 vs 发帖星期"能分开。
func ResolveTargetDate(hint string, postedAt time.Time) TargetDate {
	hint = strings.TrimSpace(hint)

	if m := reExplicitDate.FindStringSubmatch(hint); m != nil {
		mon, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		if mon >= 1 && mon <= 12 && day >= 1 && day <= 31 {
			d := time.Date(postedAt.Year(), time.Month(mon), day, 0, 0, 0, 0, postedAt.Location())
			// 跨年:12 月底发帖写「1月2日」,年份要 +1(否则会算成 11 个月前)
			if d.Before(postedAt.AddDate(0, 0, -180)) {
				d = d.AddDate(1, 0, 0)
			}
			return TargetDate{
				Date:    d.Format("2006-01-02"),
				Basis:   fmt.Sprintf("原文写明 %d月%d日", mon, day),
				SameDay: d.Format("2006-01-02") == postedAt.Format("2006-01-02"),
			}
		}
	}

	if m := reNextWeekDay.FindStringSubmatch(hint); m != nil {
		tw := cnWeekday[m[1]]
		// 下周一 = 发帖日之后最近的那个周一,再按 tw 顺延
		nextMon := postedAt.AddDate(0, 0, 8-isoWeekday(postedAt))
		d := nextMon.AddDate(0, 0, tw-1)
		return TargetDate{Date: d.Format("2006-01-02"), Basis: fmt.Sprintf("原文写「下周%s」", m[1])}
	}

	if reTomorrow.MatchString(hint) {
		d := postedAt.AddDate(0, 0, 1)
		return TargetDate{Date: d.Format("2006-01-02"), Basis: "原文写「明日/次日」"}
	}

	// 「下周」没写星期几 —— 老林A股 2026-07-31(周五)发「下周建仓提前公开!下周建仓密码」。
	// 不单独处理的话会掉进最后的"按次日推定",算成周六,离他的意思差了两天。
	if reNextWeekPlain.MatchString(hint) {
		d := postedAt.AddDate(0, 0, 8-isoWeekday(postedAt)) // 下周一
		return TargetDate{Date: d.Format("2006-01-02"), Basis: "原文写「下周」(没写星期几,按下周一)"}
	}

	if m := reThisWeekDay.FindStringSubmatch(hint); m != nil {
		tw, pw := cnWeekday[m[1]], isoWeekday(postedAt)
		switch delta := tw - pw; {
		case delta == 0:
			return TargetDate{
				Date:    postedAt.Format("2006-01-02"),
				Basis:   fmt.Sprintf("原文写「周%s」,发帖当天正是周%s → 这是当日复盘不是预告", m[1], m[1]),
				SameDay: true,
			}
		case delta > 0:
			d := postedAt.AddDate(0, 0, delta)
			return TargetDate{Date: d.Format("2006-01-02"), Basis: fmt.Sprintf("原文写「周%s」(本周内)", m[1])}
		default:
			d := postedAt.AddDate(0, 0, delta+7)
			return TargetDate{Date: d.Format("2006-01-02"), Basis: fmt.Sprintf("原文写「周%s」(已过,顺延到下周)", m[1])}
		}
	}

	d := postedAt.AddDate(0, 0, 1)
	return TargetDate{Date: d.Format("2006-01-02"), Basis: "原文没写时间,按发帖次日推定", Guessed: true}
}
