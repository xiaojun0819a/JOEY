package main

// X 荐股博主 —— 手动补录历史(2026-08-01 建)。
//
// 为什么要有这条路:自动回溯被 X 限流卡得很死 —— 六个博主连着跑,五个只翻到两周就断,
// 改成一人一跑、中间歇十分钟又要大半天。而用户自己翻手机看一眼就知道某人某天推了什么,
// 让他直接贴比等机器爬快一个数量级。
//
// 录入格式一行一条,**第二列是发帖日**(不是买入日):
//
//	老枪 07-30 神州信息 武商集团
//	云舒 2026-07-15 创新医疗、顺钠股份
//	趋势捕手 07-22 603556
//
// 目标买入日 = 发帖日之后的**下一个交易日**。这跟这六个人的实际节奏一致(晚上发、次日买),
// 也和自动解析那条路的默认推定一致,两边的数据才能混在一起统计。
// 少数"下周一建仓"这种跨周的,手动录时直接把发帖日写成买入日的前一个交易日即可。
//
// 幂等:tweet_id 用 manual-{博主}-{日期} 合成,同一天重复贴不会重复入库(会覆盖当天那批)。

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/xblogger"
)

// XManualRow 一行录入的结果。
type XManualRow struct {
	Line    string   `json:"line"`
	Blogger string   `json:"blogger"`
	Display string   `json:"display"`
	Posted  string   `json:"posted"`
	Target  string   `json:"target"`
	Symbols []string `json:"symbols"`
	Names   []string `json:"names"`
	Error   string   `json:"error,omitempty"`
}

type XManualResult struct {
	Rows    []XManualRow `json:"rows"`
	Added   int          `json:"added"`
	Signals int          `json:"signals"`
	Failed  int          `json:"failed"`
}

// 日期:2026-07-15 / 07-15 / 7.15 / 7月15日 都收 —— 手打时没人会统一格式。
var xManualDateRe = regexp.MustCompile(`^(?:(\d{4})[-/.年])?(\d{1,2})[-/.月](\d{1,2})日?$`)

// AddXSignalsManual 手动补录历史推荐。raw 一行一条。
func (a *App) AddXSignalsManual(raw string) (*XManualResult, error) {
	db, err := openXblogDB()
	if err != nil {
		return nil, err
	}
	cat := xCatalog()
	cst := time.FixedZone("CST", 8*60*60)
	now := time.Now().In(cst)
	res := &XManualResult{}

	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		row := XManualRow{Line: line}
		// 统一分隔符再切:用户会混用空格、逗号、顿号
		norm := strings.NewReplacer("，", " ", ",", " ", "、", " ", "\t", " ", "　", " ").Replace(line)
		fields := strings.Fields(norm)
		if len(fields) < 3 {
			row.Error = "至少要有 博主 日期 股票 三段"
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}

		cfg, ok := xblogger.ConfigByAlias(fields[0])
		if !ok {
			row.Error = fmt.Sprintf("不认识博主「%s」,可用:%s", fields[0], xblogger.AliasHint())
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}
		row.Blogger, row.Display = cfg.Key, cfg.Display

		m := xManualDateRe.FindStringSubmatch(fields[1])
		if m == nil {
			row.Error = fmt.Sprintf("看不懂日期「%s」(可写 2026-07-15 / 07-15 / 7月15日)", fields[1])
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}
		year := now.Year()
		if m[1] != "" {
			fmt.Sscanf(m[1], "%d", &year)
		}
		var mon, day int
		fmt.Sscanf(m[2], "%d", &mon)
		fmt.Sscanf(m[3], "%d", &day)
		posted := time.Date(year, time.Month(mon), day, 20, 0, 0, 0, cst)
		// 没写年份又算到了未来 → 是去年的(12 月底补录 1 月的帖子会踩到)
		if m[1] == "" && posted.After(now.AddDate(0, 0, 1)) {
			posted = posted.AddDate(-1, 0, 0)
		}
		row.Posted = posted.Format("2006-01-02")
		row.Target = a.nextTradingDayOnOrAfter(posted.AddDate(0, 0, 1).Format("2006-01-02"))

		picks := xblogger.ExtractPicks(strings.Join(fields[2:], " "), cat)
		if len(picks) == 0 {
			row.Error = "这几段里认不出股票:" + strings.Join(fields[2:], " ")
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}
		if cfg.MaxPicks > 0 && len(picks) > cfg.MaxPicks {
			row.Error = fmt.Sprintf("认出 %d 只,超过该博主常态上限 %d —— 这行怕是串了", len(picks), cfg.MaxPicks)
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}

		tweetID := fmt.Sprintf("manual-%s-%s", cfg.Key, row.Posted)
		// 同一天重复贴按覆盖处理:补录难免贴错再贴一次,留着旧的会把统计弄脏
		_, _ = db.Exec(`DELETE FROM x_signal WHERE tweet_id=?`, tweetID)
		_, _ = db.Exec(`DELETE FROM x_post WHERE tweet_id=?`, tweetID)
		if _, err := db.Exec(`INSERT INTO x_post
			(tweet_id, blogger, handle, posted_at, fetched_at, url, text, parsed, target_date, buy_count, needs_review)
			VALUES (?,?,?,?,?,?,?,?,?,?,0)`,
			tweetID, cfg.Key, cfg.Handle, posted.Format("2006-01-02 15:04:05"),
			now.Format("2006-01-02 15:04:05"), "", "[手动补录] "+line, "", row.Target, len(picks)); err != nil {
			row.Error = "落库失败:" + err.Error()
			res.Rows, res.Failed = append(res.Rows, row), res.Failed+1
			continue
		}
		for _, pk := range picks {
			_, _ = db.Exec(`INSERT OR IGNORE INTO x_signal
				(tweet_id, blogger, symbol, name, kind, target_date, created_at)
				VALUES (?,?,?,?,'buy',?,?)`,
				tweetID, cfg.Key, pk.Symbol, pk.Name, row.Target, now.Format("2006-01-02 15:04:05"))
			row.Symbols = append(row.Symbols, pk.Symbol)
			row.Names = append(row.Names, pk.Name)
			res.Signals++
		}
		res.Added++
		res.Rows = append(res.Rows, row)
	}
	log.Info("X荐股手动补录:%d 行 %d 只,失败 %d 行", res.Added, res.Signals, res.Failed)
	return res, nil
}
