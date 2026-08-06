package main

// X 荐股博主 —— 次日开盘建仓。
//
// 时点定在 **09:30:00**,用那一刻的实时价记账。
// 不用当日开盘价:若 09:35 才跑却按 09:30 开盘价入账,等于白捡这五分钟的涨跌
// (本项目在低吸策略上栽过同类前视偏差,见 canAutoBuyNow 注释)。开盘价另存一列纯做对照,
// 让用户自己看见"想买的价"和"真能买到的价"差多少。
//
// 买不进的票**照样落一行 skipped 并写明原因**,不是悄悄丢掉:
// 一字涨停买不进本身就是这条推荐的真实结果,把它抹掉会让博主的胜率虚高。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/xblogger"
)

var xOpenBuyMu sync.Mutex

// xDueSignal 今天该建仓的一条买入信号。
type xDueSignal struct {
	ID        int64
	TweetID   string
	Blogger   string
	Symbol    string
	Name      string
	EntryHigh float64
}

// RunXOpenBuy 把今天到期的博主推荐建进模拟持仓。09:30 由 monitor 触发,也可手动调。
func (a *App) RunXOpenBuy() string {
	if !xOpenBuyMu.TryLock() {
		return "上一轮还在跑,本轮跳过"
	}
	defer xOpenBuyMu.Unlock()

	if a == nil || a.paperService == nil || a.marketService == nil {
		return "服务未初始化"
	}
	db, err := openXblogDB()
	if err != nil {
		return "打不开 xblog.db:" + err.Error()
	}
	cst := time.FixedZone("CST", 8*60*60)
	now := time.Now().In(cst)
	today := now.Format("2006-01-02")

	// 先把过期未建的收尾:目标日已过还是 pending,说明那天没跑成(重启/停机)。
	// 标成 expired 而不是留着,否则会在某个不相干的日子被突然买进去。
	if r, err := db.Exec(`UPDATE x_signal SET buy_status='expired',
		buy_reason='目标日已过仍未建仓(当天未执行)' WHERE kind='buy' AND buy_status='pending' AND target_date<?`, today); err == nil {
		if n, _ := r.RowsAffected(); n > 0 {
			log.Warn("X荐股:%d 条过期未建仓的信号已标记 expired", n)
		}
	}

	if !a.canAutoBuyNow() {
		return "当前不在可成交时段(需交易日 09:25-15:00),本轮不建仓"
	}

	rows, err := db.Query(`SELECT id, tweet_id, blogger, symbol, name, entry_high
		FROM x_signal WHERE kind='buy' AND target_date=? AND buy_status='pending' ORDER BY id`, today)
	if err != nil {
		return "查询待建仓失败:" + err.Error()
	}
	due := []xDueSignal{}
	for rows.Next() {
		var s xDueSignal
		if rows.Scan(&s.ID, &s.TweetID, &s.Blogger, &s.Symbol, &s.Name, &s.EntryHigh) == nil {
			due = append(due, s)
		}
	}
	rows.Close()
	if len(due) == 0 {
		return "今天没有到期的博主推荐"
	}

	// 关着自动建仓的博主:落一行 off,保留信号本身(验证模块照样能用历史价算它的表现)
	kept := due[:0]
	offBy := map[string]int{}
	for _, s := range due {
		if a.xAutoBuyOn(s.Blogger) {
			kept = append(kept, s)
			continue
		}
		_, _ = db.Exec(`UPDATE x_signal SET buy_status='off', buy_reason='该博主自动建仓未开启(灰度期只推送)' WHERE id=?`, s.ID)
		offBy[s.Blogger]++
	}
	due = kept

	var b strings.Builder
	added, skipped := 0, 0
	if len(due) > 0 {
		symbols := make([]string, 0, len(due))
		for _, s := range due {
			symbols = append(symbols, s.Symbol)
		}
		quotes, err := a.marketService.GetStockRealTimeData(symbols...)
		if err != nil {
			return "取实时行情失败:" + err.Error()
		}
		idx := map[string]int{}
		for i, q := range quotes {
			idx[strings.ToLower(strings.TrimSpace(q.Symbol))] = i
		}
		// 同源已持有的不重复建仓(同一只票被同一个博主连推两天很常见)
		held := map[string]bool{}
		for _, p := range a.paperService.OpenPositions() {
			held[strings.ToLower(p.Source)+"|"+strings.ToLower(strings.TrimSpace(p.Symbol))] = true
		}

		for _, s := range due {
			reason, price, dayOpen, outside, posID := "", 0.0, 0.0, 0, int64(0)
			qi, ok := idx[strings.ToLower(s.Symbol)]
			switch {
			case !ok:
				reason = "取不到行情(代码可能已退市)"
			case held[strings.ToLower(s.Blogger)+"|"+strings.ToLower(s.Symbol)]:
				reason = "该博主分组下已有未平仓持仓,不重复建仓"
			default:
				q := quotes[qi]
				price, dayOpen = q.Price, q.Open
				if q.Price <= 0 {
					reason = "实时价为 0"
					break
				}
				if ok, why := a.isBuyableNow(s.Symbol, q.Price, q.ChangePercent); !ok {
					reason = "买不进:" + why
					break
				}
				shares := int64(xBuyAmountPerStock/(q.Price*100)) * 100
				if shares <= 0 {
					reason = fmt.Sprintf("%.0f 元买不起 1 手(现价 %.2f)", xBuyAmountPerStock, q.Price)
					break
				}
				id, err := a.paperService.Add(s.Symbol, s.Name, s.Blogger, q.Price, shares, "auto")
				if err != nil {
					reason = "落库失败:" + err.Error()
					break
				}
				posID = id
				added++
				// 博主自己写了低吸区间、而开盘就买在区间上沿之上:按他的规矩这单本不该开。
				// 这里**照买不误**(六个人要用同一套规则才能横向比),只打标记,
				// 让对照分析能算出"只买区间内"和"全买"两种口径的差别。
				if s.EntryHigh > 0 && q.Price > s.EntryHigh {
					outside = 1
				}
			}
			if reason != "" {
				skipped++
				_, _ = db.Exec(`UPDATE x_signal SET buy_status='skipped', buy_reason=?, day_open=? WHERE id=?`,
					reason, dayOpen, s.ID)
				fmt.Fprintf(&b, "✗ %s %s\n", s.Name, reason)
				continue
			}
			_, _ = db.Exec(`UPDATE x_signal SET buy_status='bought', buy_price=?, day_open=?,
				outside_plan=?, position_id=?, buy_reason='' WHERE id=?`, price, dayOpen, outside, posID, s.ID)
			disp := s.Blogger
			if cfg, ok := xblogger.ConfigByKey(s.Blogger); ok {
				disp = cfg.Display
			}
			fmt.Fprintf(&b, "· [%s] %s %.2f", disp, s.Name, price)
			if dayOpen > 0 {
				fmt.Fprintf(&b, "(较开盘%+.2f%%)", (price/dayOpen-1)*100)
			}
			if outside == 1 {
				b.WriteString(" ⚠️高于博主低吸上沿")
			}
			b.WriteString("\n")
		}
	}

	summary := fmt.Sprintf("X荐股开盘建仓:记入 %d 只,跳过 %d 只", added, skipped)
	if len(offBy) > 0 {
		parts := []string{}
		for k, n := range offBy {
			disp := k
			if cfg, ok := xblogger.ConfigByKey(k); ok {
				disp = cfg.Display
			}
			parts = append(parts, fmt.Sprintf("%s×%d", disp, n))
		}
		summary += ",未开自动建仓 " + strings.Join(parts, "/")
	}
	log.Info("%s", summary)

	if (added > 0 || skipped > 0) && a.pushService != nil {
		a.pushService.Push(models.PushSignal{
			Type:    models.PushTypeBuyPoint,
			Message: "【X荐股跟单】" + summary + "\n" + strings.TrimRight(b.String(), "\n"),
			Level:   "active",
		})
	}
	return summary + "\n" + b.String()
}
