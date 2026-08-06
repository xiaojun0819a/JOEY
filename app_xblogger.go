package main

// X(推特)荐股博主跟单验证。2026-07-31 建。
//
// 用户关注了六个在 X 上发"明日看好票"的博主,想要三件事:
//   ① 每隔几分钟刷新他们的动态,出现推票就推到手机;
//   ② 次日开盘自动买进模拟持仓,按博主分组,累积**前向样本**;
//   ③ 回溯半年历史推文,验证每个人到底行不行。
//
// 全部逻辑放 NAS 后端;抓取放用户那台常开的 Windows 笔记本(它挂着 VPN,NAS 直连不到 x.com),
// 抓到的**原始文本**POST 到本文件的 /x/ingest。抓取端只负责搬运,一行解析都不做——
// 解析规则会一直改,改在服务端才不用每次去动那台笔记本。
//
// 三条纪律(前两条跟天鼎荐股跟单一致,理由见 app_tip_picks.go 顶部):
//   ① **记账价 = 执行时刻的实时价**,不是当日开盘价。09:30 那一刻的实时价就是能成交的价;
//      若 09:35 才跑却按 09:30 开盘价记账,等于白捡这五分钟的信息。
//   ② **只在交易时段建仓**,涨停封死不建。
//   ③ **建仓即 auto_exit_off**……这一条**故意不照抄**:天鼎那个实验测的是用户自己的手感,
//      所以不让引擎接管;这里要测的是"博主选股行不行",必须有**统一的出场规则**才能横向比。
//      六人里只有云舒交易日记给了止盈止损,若各用各的,比出来的是出场规则差异不是选股能力差异。
//      所以这里交给风控引擎按统一 profile 管,她自带的计划另存 x_signal 里做对照分析。

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/services"
	"github.com/run-bigpig/jcp/internal/xblogger"
)

// xBuyAmountPerStock 每只票的建仓金额。定死不随博主变,否则仓位差会混进业绩比较里。
const xBuyAmountPerStock = 30000.0

var (
	xblogDBMu sync.Mutex
	xblogDB   *sql.DB
)

// xblogDBPathOverride 仅供测试改道到临时目录 —— 否则单测会往主人真实的 xblog.db 里写脏数据。
var xblogDBPathOverride string

// xblogDBPath 固定在主人数据目录:这是主人专属功能,不做访客隔离。
func xblogDBPath() string {
	if xblogDBPathOverride != "" {
		return xblogDBPathOverride
	}
	return filepath.Join(paths.GetDataDir(), "xblog.db")
}

func openXblogDB() (*sql.DB, error) {
	xblogDBMu.Lock()
	defer xblogDBMu.Unlock()
	if xblogDB != nil {
		return xblogDB, nil
	}
	db, err := sql.Open("sqlite", xblogDBPath()+"?_pragma=busy_timeout(8000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS x_post (
			tweet_id    TEXT PRIMARY KEY,       -- 幂等键:抓取端重复推同一条不会重复建仓
			blogger     TEXT NOT NULL,
			handle      TEXT NOT NULL,
			posted_at   TEXT NOT NULL,          -- 东八区 2006-01-02 15:04:05
			fetched_at  TEXT NOT NULL,
			url         TEXT NOT NULL DEFAULT '',
			text        TEXT NOT NULL,
			parsed      TEXT NOT NULL DEFAULT '',  -- Parsed 的 JSON 原样存档,便于日后换规则重跑
			target_date TEXT NOT NULL DEFAULT '',  -- 已对齐到交易日
			buy_count   INTEGER NOT NULL DEFAULT 0,
			needs_review INTEGER NOT NULL DEFAULT 0,
			pushed_at   TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_xpost_blogger ON x_post(blogger, posted_at);

		CREATE TABLE IF NOT EXISTS x_signal (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			tweet_id    TEXT NOT NULL,
			blogger     TEXT NOT NULL,
			symbol      TEXT NOT NULL,
			name        TEXT NOT NULL,
			kind        TEXT NOT NULL,          -- buy / hold / exit
			target_date TEXT NOT NULL,          -- 交易日
			entry_low   REAL NOT NULL DEFAULT 0,
			entry_high  REAL NOT NULL DEFAULT 0,
			tp1         REAL NOT NULL DEFAULT 0,
			tp2         REAL NOT NULL DEFAULT 0,
			stop        REAL NOT NULL DEFAULT 0,
			buy_status  TEXT NOT NULL DEFAULT 'pending', -- pending/bought/skipped/manual_off
			buy_reason  TEXT NOT NULL DEFAULT '',
			buy_price   REAL NOT NULL DEFAULT 0,
			day_open    REAL NOT NULL DEFAULT 0,   -- 仅对照:想买的价 vs 真能买的价
			outside_plan INTEGER NOT NULL DEFAULT 0, -- 成交价高于博主自己写的低吸区间上沿
			position_id INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL,
			UNIQUE(tweet_id, symbol, kind)
		);
		CREATE INDEX IF NOT EXISTS idx_xsig_due ON x_signal(kind, target_date, buy_status);

		CREATE TABLE IF NOT EXISTS x_blogger_state (
			blogger  TEXT PRIMARY KEY,
			auto_buy INTEGER NOT NULL DEFAULT 0,  -- 灰度期默认关:先只推送,人工核对两周再开
			paused   INTEGER NOT NULL DEFAULT 0
		);
	`); err != nil {
		_ = db.Close()
		return nil, err
	}
	xblogDB = db
	return db, nil
}

// xCatalog 全市场名录(实时库现名优先),按 TTL 自动刷新,见 services.AllStockNameSymbols。
func xCatalog() *xblogger.Catalog {
	src := services.AllStockNameSymbols()
	pairs := make([]xblogger.NameSym, 0, len(src))
	for _, ns := range src {
		pairs = append(pairs, xblogger.NameSym{Name: ns.Name, Symbol: ns.Symbol})
	}
	return xblogger.NewCatalog(pairs)
}

// ===== 接收 =====

// XIngestPost 抓取端推上来的一条原始推文。
type XIngestPost struct {
	ID       string `json:"id"`       // 推文 id(URL 末尾那串数字),幂等键
	PostedAt string `json:"postedAt"` // RFC3339 或 "2006-01-02 15:04:05";空则用当前时间
	Text     string `json:"text"`
	URL      string `json:"url"`
}

// XIngestRequest 一次上报(同一个博主的一批推文)。
type XIngestRequest struct {
	Handle string        `json:"handle"` // @句柄,带不带 @ 都行
	Posts  []XIngestPost `json:"posts"`

	// DryRun 只解析、不入库、不推送。给抓取端的 test 模式用:
	// 想看某条推文会被解析成什么,不该为此在数据库里留下痕迹,
	// 也不该因为这条已经存过就跳过解析(正常路径遇到重复会直接跳过)。
	DryRun bool `json:"dryRun"`
}

// XIngestResult 处理结果,回给抓取端记日志用。
type XIngestResult struct {
	Blogger  string `json:"blogger"`
	Display  string `json:"display"`
	Received int    `json:"received"`
	New      int    `json:"new"`
	Repeat   int    `json:"repeat"`
	// Updated 已存过、但这次抓到的正文更长,已覆盖重解析的条数。
	// 单独计数而不是从 Repeat 里减 —— 减出来会是负数,报告里出现负的"重复数"
	// 会让人怀疑整份输出,哪怕别的都对。
	Updated int               `json:"updated"`
	Parsed  []xblogger.Parsed `json:"parsed"`
	Errors  []string          `json:"errors,omitempty"`
}

// IngestXPosts 接收并解析一批推文;新出现的买入信号立即推手机。
func (a *App) IngestXPosts(req XIngestRequest) (*XIngestResult, error) {
	cfg, ok := xblogger.ConfigByHandle(req.Handle)
	if !ok {
		return nil, fmt.Errorf("未登记的博主 @%s —— 请先加进 xblogger.Configs(解析规则是按人配的,不能通配)", req.Handle)
	}
	db, err := openXblogDB()
	if err != nil {
		return nil, err
	}
	cat := xCatalog()
	cst := time.FixedZone("CST", 8*60*60)
	now := time.Now().In(cst)
	res := &XIngestResult{Blogger: cfg.Key, Display: cfg.Display, Received: len(req.Posts)}

	for _, p := range req.Posts {
		id := strings.TrimSpace(p.ID)
		if id == "" || strings.TrimSpace(p.Text) == "" {
			res.Errors = append(res.Errors, "跳过一条:id 或正文为空")
			continue
		}
		if !req.DryRun {
			var oldText string
			err := db.QueryRow(`SELECT text FROM x_post WHERE tweet_id=?`, id).Scan(&oldText)
			if err == nil {
				// 已经存过。**但如果这次拿到的正文更长,就得覆盖重解析。**
				//
				// 起因:X 折叠长推文,抓取端要逐条开单条页取全文,而那一步会被限流打挂
				// (实测连续 8 条 20s 超时)。失败的那些按截断版入库,里面的票就丢了——
				// 而按 id 去重意味着以后重跑也会跳过,**丢的永远补不回来**。
				// 允许"更长的正文覆盖旧的",重跑一次就能把这些缺口填上。
				if len([]rune(p.Text)) <= len([]rune(oldText)) {
					res.Repeat++
					continue
				}
				log.Info("X荐股[%s] %s 正文变长 %d→%d 字,覆盖重解析",
					cfg.Display, id, len([]rune(oldText)), len([]rune(p.Text)))
				_, _ = db.Exec(`DELETE FROM x_signal WHERE tweet_id=?`, id)
				_, _ = db.Exec(`DELETE FROM x_post WHERE tweet_id=?`, id)
				res.Updated++
			}
		}
		postedAt := parseXTime(p.PostedAt, now, cst)
		parsed := xblogger.Parse(cfg, p.Text, postedAt, cat)

		// 目标日对齐到真实交易日:博主写"明日"时不管周末和节假日。
		// 对齐后写回 parsed —— 否则 dryRun 的输出会显示"目标日 2026-08-01",
		// 而那天是周六,人看了会以为解析错了(实际入库存的是顺延后的 08-03)。
		target := a.nextTradingDayOnOrAfter(parsed.Target.Date)
		if target != parsed.Target.Date {
			parsed.Target.Basis += fmt.Sprintf(";%s 非交易日,顺延至 %s", parsed.Target.Date, target)
			parsed.Target.Date = target
		}
		blob, _ := json.Marshal(parsed)

		if req.DryRun {
			res.Parsed = append(res.Parsed, parsed)
			continue
		}

		if _, err := db.Exec(`INSERT INTO x_post
			(tweet_id, blogger, handle, posted_at, fetched_at, url, text, parsed, target_date, buy_count, needs_review)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, cfg.Key, cfg.Handle, postedAt.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"),
			p.URL, p.Text, string(blob), target, len(parsed.Buys), boolInt(parsed.NeedsReview)); err != nil {
			res.Errors = append(res.Errors, "落库失败:"+err.Error())
			continue
		}
		res.New++

		planOf := map[string]xblogger.Plan{}
		for _, pl := range parsed.Plans {
			planOf[pl.Symbol] = pl
		}
		for _, group := range [][]xblogger.Pick{parsed.Buys, parsed.Holds, parsed.Exits} {
			for _, pk := range group {
				pl := planOf[pk.Symbol]
				_, _ = db.Exec(`INSERT OR IGNORE INTO x_signal
					(tweet_id, blogger, symbol, name, kind, target_date, entry_low, entry_high, tp1, tp2, stop, created_at)
					VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
					id, cfg.Key, pk.Symbol, pk.Name, string(pk.Kind), target,
					pl.EntryLow, pl.EntryHigh, pl.TP1, pl.TP2, pl.Stop, now.Format("2006-01-02 15:04:05"))
			}
		}
		// 只要提到股票就推(买入/持仓/卖出都算),并附上原文全文 —— 用户要自己看博主原话。
		// 纯广告和进群通知由 pushXPost 内部过滤掉。
		if len(parsed.Buys)+len(parsed.Holds)+len(parsed.Exits) > 0 {
			a.pushXPost(cfg, parsed, target, p.Text)
			_, _ = db.Exec(`UPDATE x_post SET pushed_at=? WHERE tweet_id=?`, now.Format("2006-01-02 15:04:05"), id)
		}
		res.Parsed = append(res.Parsed, parsed)
		log.Info("X荐股[%s] 收 %s:买入%d 持仓%d 卖出%d 目标日%s 待复核=%v",
			cfg.Display, id, len(parsed.Buys), len(parsed.Holds), len(parsed.Exits), target, parsed.NeedsReview)
	}
	return res, nil
}

// parseXTime 尽量宽松地认发帖时间;认不出就按"现在",并让日期判定走"次日"兜底。
func parseXTime(s string, fallback time.Time, loc *time.Location) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.In(loc)
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc)
	}
	return fallback
}

// nextTradingDayOnOrAfter 把自然日对齐到当天或之后最近的交易日。
func (a *App) nextTradingDayOnOrAfter(date string) string {
	cst := time.FixedZone("CST", 8*60*60)
	d, err := time.ParseInLocation("2006-01-02", date, cst)
	if err != nil {
		return date
	}
	if a == nil || a.marketService == nil {
		return date
	}
	for i := 0; i < 12; i++ { // 最长春节假期也就十来天
		if a.marketService.IsTradingDay(d) {
			return d.Format("2006-01-02")
		}
		d = d.AddDate(0, 0, 1)
	}
	return date
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ===== 推送 =====

// xPushRawMaxRunes 推送里附带的原文上限。Telegram 单条 4096 字符,留出头部和告警的余量。
const xPushRawMaxRunes = 2600

// pushXPost 一条推文推一条消息,**带原文全文**。
//
// 2026-08-02 用户定:回溯验证告一段落后,他的用法从"看统计"转成"盯实时"——
// 光推解析出的票不够,他要自己看博主原话才能判断这次推荐靠不靠谱
// (比如云舒会在票暴跌后原样再推一遍,只看票名完全看不出来)。
//
// 刻意改成**一条推文一条消息**,不是一只票一条:
// 附了全文之后按只推会把同一段一千多字的原文重复发好几遍。
func (a *App) pushXPost(cfg xblogger.Config, parsed xblogger.Parsed, target, raw string) {
	if a == nil || a.pushService == nil {
		return
	}
	// 纯广告/公告(一只票都没提)不推,否则群推广、进群通知一天能刷十几条。
	if len(parsed.Buys)+len(parsed.Holds)+len(parsed.Exits) == 0 {
		return
	}
	autoBuy := a.xAutoBuyOn(cfg.Key)
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】%s", cfg.Display, parsed.PostedAt)
	if len(parsed.Buys) > 0 {
		names := make([]string, 0, len(parsed.Buys))
		for _, pk := range parsed.Buys {
			names = append(names, fmt.Sprintf("%s(%s)", pk.Name, pk.Symbol))
		}
		fmt.Fprintf(&b, "\n✅ %s 买入:%s", target, strings.Join(names, "、"))
		if !autoBuy {
			b.WriteString("\n(仅记录,自动建仓未开)")
		}
	}
	if len(parsed.Holds) > 0 {
		names := make([]string, 0, len(parsed.Holds))
		for _, pk := range parsed.Holds {
			names = append(names, pk.Name)
		}
		fmt.Fprintf(&b, "\n⚪️ 他已持仓:%s", strings.Join(names, "、"))
	}
	if len(parsed.Exits) > 0 {
		names := make([]string, 0, len(parsed.Exits))
		for _, pk := range parsed.Exits {
			names = append(names, pk.Name)
		}
		fmt.Fprintf(&b, "\n🔴 他已卖出:%s", strings.Join(names, "、"))
	}
	for _, pl := range parsed.Plans {
		bits := []string{}
		if pl.EntryLow > 0 {
			bits = append(bits, fmt.Sprintf("低吸 %.2f-%.2f", pl.EntryLow, pl.EntryHigh))
		}
		if pl.TP1 > 0 {
			bits = append(bits, fmt.Sprintf("止盈 %.2f/%.2f", pl.TP1, pl.TP2))
		}
		if pl.Stop > 0 {
			bits = append(bits, fmt.Sprintf("止损 %.2f", pl.Stop))
		}
		if len(bits) > 0 {
			fmt.Fprintf(&b, "\n📋 %s %s", pl.Name, strings.Join(bits, " · "))
		}
	}
	// 告警必须进推送正文:解析出错时要能在手机上一眼看见,而不是等他去翻日志
	for _, w := range parsed.Warnings {
		fmt.Fprintf(&b, "\n⚠️%s", w)
	}
	if s := strings.TrimSpace(raw); s != "" {
		b.WriteString("\n────────\n")
		if r := []rune(s); len(r) > xPushRawMaxRunes {
			b.WriteString(string(r[:xPushRawMaxRunes]) + fmt.Sprintf("…(原文共 %d 字,已截断)", len(r)))
		} else {
			b.WriteString(s)
		}
	}
	code, name := "", ""
	if len(parsed.Buys) > 0 {
		code, name = parsed.Buys[0].Symbol, parsed.Buys[0].Name
	}
	go a.pushService.Push(models.PushSignal{
		StockCode: code,
		StockName: name,
		Type:      models.PushTypeBuyPoint,
		Message:   b.String(),
		Level:     "active",
	})
}

// ===== 灰度开关 =====

func (a *App) xAutoBuyOn(blogger string) bool {
	db, err := openXblogDB()
	if err != nil {
		return false
	}
	var on int
	// 没有记录 = 未开通。新博主一律先只推送,人工核对两周再打开。
	_ = db.QueryRow(`SELECT auto_buy FROM x_blogger_state WHERE blogger=? AND paused=0`, blogger).Scan(&on)
	return on == 1
}

// SetXBloggerAutoBuy 打开/关闭某个博主的自动建仓(按人独立,谁准开谁)。
func (a *App) SetXBloggerAutoBuy(blogger string, on bool) (string, error) {
	if _, ok := xblogger.ConfigByKey(blogger); !ok {
		return "", fmt.Errorf("未知博主 %s", blogger)
	}
	db, err := openXblogDB()
	if err != nil {
		return "", err
	}
	if _, err := db.Exec(`INSERT INTO x_blogger_state(blogger, auto_buy) VALUES(?,?)
		ON CONFLICT(blogger) DO UPDATE SET auto_buy=excluded.auto_buy`, blogger, boolInt(on)); err != nil {
		return "", err
	}
	log.Info("X荐股:%s 自动建仓 → %v", blogger, on)
	if on {
		return "已开启自动建仓", nil
	}
	return "已关闭自动建仓(仍会推送和记录)", nil
}
