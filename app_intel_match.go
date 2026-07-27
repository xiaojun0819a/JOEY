package main

// 情报库 × 策略打通(2026-07-27,第二大脑第二步)。
//
// 第一步只做到"资料进得来",但资料躺在库里是死的——要你主动去看晨报才想得起来。
// 这一步让它主动找你:**策略选中的票,如果近期在情报库里被提过,就在候选卡片和推送里标出来**。
//
// ⚠️前提改动:情报库原本是纯本地的(intel.db 在本机,方法在 remoteBridge 的 LOCAL_METHODS 白名单里),
// 而扫描与推送都跑在 NAS 上,**NAS 根本读不到本机的情报库**。所以本步把情报库迁到 NAS
// (同一份 Go 代码,数据目录随进程走;迁移前确认两边都是 0 条,零风险)。
// 代价:情报库不再是"本机私有",而是跟持仓/留痕一样以 NAS 为单一数据源——这也更合理,
// 否则换台电脑就看不到自己记的东西。
//
// 定位纪律:命中只是**标注**,不加分、不改排序、不影响选股。原因是情报是观点不是数据,
// 让它参与打分等于让一个未经校准的信息源直接左右仓位。先记录、后校准(第三步做)。

import (
	"fmt"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// intelHitDays 情报有效期:超过这个天数的旧笔记不再标注(观点有时效,一个月前的看法不该影响今天)。
const intelHitDays = 30

type intelHit struct {
	When    string // 记录日期 MM-DD
	Snippet string // 正文摘要
	Source  string
}

// lookupIntelHits 查这批代码在近 intelHitDays 天内的情报笔记(每只最多取最近 2 条)。
func (a *App) lookupIntelHits(codes []string) map[string][]intelHit {
	out := map[string][]intelHit{}
	if a == nil || len(codes) == 0 {
		return out
	}
	db, err := a.intelDB()
	if err != nil {
		return out
	}
	since := time.Now().AddDate(0, 0, -intelHitDays).Format("2006-01-02")
	rows, err := db.Query(`SELECT ts, text, source, codes FROM intel_note WHERE ts >= ? ORDER BY ts DESC LIMIT 500`, since)
	if err != nil {
		return out
	}
	defer rows.Close()
	want := map[string]bool{}
	for _, c := range codes {
		want[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for rows.Next() {
		var ts, text, source, codeStr string
		if rows.Scan(&ts, &text, &source, &codeStr) != nil {
			continue
		}
		for _, c := range strings.Split(codeStr, ",") {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "" || !want[c] || len(out[c]) >= 2 {
				continue
			}
			snippet := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
			if r := []rune(snippet); len(r) > 60 {
				snippet = string(r[:60]) + "…"
			}
			when := ts
			if len(when) >= 10 {
				when = when[5:10]
			}
			out[c] = append(out[c], intelHit{When: when, Snippet: snippet, Source: source})
		}
	}
	return out
}

// annotateIntelHits 给候选打上情报库命中标记。**只标注,不加分不改排序**(见文件头纪律)。
func (a *App) annotateIntelHits(items []models.LowBuyScannerItem) []models.LowBuyScannerItem {
	if a == nil || len(items) == 0 {
		return items
	}
	codes := make([]string, 0, len(items))
	for _, it := range items {
		codes = append(codes, it.Symbol)
	}
	hits := a.lookupIntelHits(codes)
	if len(hits) == 0 {
		return items
	}
	for i := range items {
		hs := hits[strings.ToLower(strings.TrimSpace(items[i].Symbol))]
		if len(hs) == 0 {
			continue
		}
		items[i].Triggers = append(items[i].Triggers, fmt.Sprintf("📌情报库命中×%d", len(hs)))
		items[i].TriggerCount = len(items[i].Triggers)
		for _, h := range hs {
			src := h.Source
			if strings.HasPrefix(src, "file:") {
				src = "文件·" + strings.TrimPrefix(src, "file:")
			}
			items[i].Reasons = append(items[i].Reasons,
				fmt.Sprintf("📌情报库(%s·%s):%s", h.When, src, h.Snippet))
		}
	}
	return items
}

// intelHitLine 供推送文案追加一行(没有命中返回空串)。
func (a *App) intelHitLine(symbol string) string {
	hs := a.lookupIntelHits([]string{symbol})[strings.ToLower(strings.TrimSpace(symbol))]
	if len(hs) == 0 {
		return ""
	}
	return fmt.Sprintf("\n📌情报库命中(%s):%s", hs[0].When, hs[0].Snippet)
}
