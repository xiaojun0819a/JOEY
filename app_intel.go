package main

// 交易情报库(第二大脑)V1。核心哲学:不预测、不喊单,只做两件事——
//   1) 把散信息接起来:手记/快讯入库,按持仓/自选归档;
//   2) 主动送反证:按需生成一份"晨报",指出哪些新信息在打脸你的持仓、该核实什么。
// 每个结论必须可追溯到原文(引用具体笔记/快讯),不能溯源的一律不算数——这是防"AI算命"的硬约束。
// 数据存 dataDir/intel.db(逐户:访客各自的 guestDataDir)。AI 用默认 config,在本机/服务端跑。

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/run-bigpig/jcp/internal/adk"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/services"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// IntelNote 一条情报/笔记
type IntelNote struct {
	ID        int64    `json:"id"`
	CreatedAt string   `json:"createdAt"`
	Text      string   `json:"text"`
	Source    string   `json:"source"` // manual(手记) | news(快讯) | url(链接)
	Codes     []string `json:"codes"`  // 关联股票代码(可空)
}

var (
	intelDBsMu sync.Mutex
	intelDBs   = map[string]*sql.DB{}
)

func (a *App) intelDBPath() string {
	base := paths.GetDataDir()
	if a.guestDataDir != "" {
		base = a.guestDataDir
	}
	return filepath.Join(base, "intel.db")
}

func (a *App) intelDB() (*sql.DB, error) {
	p := a.intelDBPath()
	intelDBsMu.Lock()
	defer intelDBsMu.Unlock()
	if db, ok := intelDBs[p]; ok {
		return db, nil
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS intel_note (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			text TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			codes TEXT NOT NULL DEFAULT ''  -- 逗号分隔的股票代码
		);
		CREATE INDEX IF NOT EXISTS idx_intel_ts ON intel_note(ts);
	`); err != nil {
		_ = db.Close()
		return nil, err
	}
	intelDBs[p] = db
	return db, nil
}

func codesToStr(codes []string) string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}

func strToCodes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AddIntelNote 入库一条情报。source 空则默认 manual。
func (a *App) AddIntelNote(text string, codes []string, source string) (*IntelNote, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("内容不能为空")
	}
	if source = strings.TrimSpace(source); source == "" {
		source = "manual"
	}
	db, err := a.intelDB()
	if err != nil {
		return nil, err
	}
	// 链接抓取:文本里的 URL 自动抓标题+正文摘要拼进笔记(晨报才有内容可引;抓不到则原样保留网址)
	text = enrichIntelLinks(text)
	// 自动关联:从正文里识别股票代码(sh600519/600519)和股票名称,合并进手填的 codes
	codes = mergeIntelCodes(codes, detectIntelCodes(text))
	ts := time.Now().Format("2006-01-02 15:04:05")
	codeStr := codesToStr(codes)
	res, err := db.Exec("INSERT INTO intel_note(ts, text, source, codes) VALUES(?,?,?,?)", ts, text, source, codeStr)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &IntelNote{ID: id, CreatedAt: ts, Text: text, Source: source, Codes: strToCodes(codeStr)}, nil
}

// ListIntelNotes 列出情报(新→旧)。codeFilter 非空则只返回关联该代码的。
func (a *App) ListIntelNotes(codeFilter string, limit int) ([]IntelNote, error) {
	db, err := a.intelDB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := "SELECT id, ts, text, source, codes FROM intel_note"
	args := []any{}
	if cf := strings.TrimSpace(codeFilter); cf != "" {
		q += " WHERE codes LIKE ?"
		args = append(args, "%"+cf+"%")
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelNote{}
	for rows.Next() {
		var n IntelNote
		var codes string
		if err := rows.Scan(&n.ID, &n.CreatedAt, &n.Text, &n.Source, &codes); err != nil {
			return nil, err
		}
		n.Codes = strToCodes(codes)
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteIntelNote 删除一条。
func (a *App) DeleteIntelNote(id int64) string {
	db, err := a.intelDB()
	if err != nil {
		return err.Error()
	}
	if _, err := db.Exec("DELETE FROM intel_note WHERE id=?", id); err != nil {
		return err.Error()
	}
	return "success"
}

// IntelDigestResponse 情报晨报响应
type IntelDigestResponse struct {
	Success     bool   `json:"success"`
	Digest      string `json:"digest"`      // markdown 正文
	NoteCount   int    `json:"noteCount"`   // 参与分析的笔记数
	HoldCount   int    `json:"holdCount"`   // 持仓数
	GeneratedAt string `json:"generatedAt"`
	ModelName   string `json:"modelName,omitempty"`
	Error       string `json:"error,omitempty"`
}

// GenerateIntelDigest 生成"反证检查"晨报:拿你的持仓 + 相关情报 + 近期快讯,
// 让 AI 只做三件事——①哪些新信息在打脸你的持仓(引原文)②该核实的问题③不给买卖建议/不预测。
// positions 由前端传入(远程模式下持仓在 NAS,前端取好传进来);为空则回落取本机持仓。
func (a *App) GenerateIntelDigest(positions []models.HeldPosition) IntelDigestResponse {
	now := time.Now().Format("2006-01-02 15:04:05")
	cfg := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(cfg)
	if aiConfig == nil {
		return IntelDigestResponse{Success: false, Error: "未配置AI服务", GeneratedAt: now}
	}

	if len(positions) == 0 {
		positions = a.GetHeldPositions()
	}
	notes, _ := a.ListIntelNotes("", 200)
	news := a.GetTelegraphList()
	if len(news) > 40 {
		news = news[:40]
	}

	if len(notes) == 0 && len(positions) == 0 {
		return IntelDigestResponse{Success: false, Error: "情报库和持仓都是空的,先记几条笔记或建个持仓再生成", GeneratedAt: now}
	}

	prompt := buildIntelDigestPrompt(positions, notes, news)

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, 4*time.Minute)
	defer cancel()

	factory := adk.NewModelFactory()
	llm, err := factory.CreateModel(ctx, aiConfig)
	if err != nil {
		return IntelDigestResponse{Success: false, Error: err.Error(), GeneratedAt: now}
	}
	reqLLM := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(prompt)}}},
	}
	var sb strings.Builder
	for resp, genErr := range llm.GenerateContent(ctx, reqLLM, false) {
		if genErr != nil {
			return IntelDigestResponse{Success: false, Error: genErr.Error(), GeneratedAt: now}
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Thought || part.Text == "" {
					continue
				}
				sb.WriteString(part.Text)
			}
		}
	}
	digest := strings.TrimSpace(sb.String())
	if digest == "" {
		return IntelDigestResponse{Success: false, Error: "AI 未返回内容,请重试", GeneratedAt: now}
	}
	return IntelDigestResponse{
		Success: true, Digest: digest, NoteCount: len(notes), HoldCount: len(positions),
		GeneratedAt: now, ModelName: aiConfig.ModelName,
	}
}

// buildIntelDigestPrompt 组装晨报提示词。刻意约束:反证优先、必引原文、禁买卖建议/禁预测。
func buildIntelDigestPrompt(positions []models.HeldPosition, notes []IntelNote, news []services.Telegraph) string {
	var b strings.Builder
	b.WriteString("你是交易者的\"第二大脑\"晨报助手。你的唯一职责是把用户脑子里散落的信息接起来,并主动送上反证。\n")
	b.WriteString("【铁律】\n")
	b.WriteString("1. 不预测涨跌、不给买入/卖出/加减仓建议。违反即失败。\n")
	b.WriteString("2. 每一条结论必须引用下面提供的原文(标注[笔记#id]或[快讯])。凭空推断、无出处的话一律不要写。\n")
	b.WriteString("3. 重点是\"打脸\":找出与'用户持有该股的理由/常识预期'相矛盾的新信息。没有矛盾就如实说\"未发现明显反证\"。\n")
	b.WriteString("4. 语言极简、只讲信息本身,不煽动、不迎合。\n\n")

	b.WriteString("【用户当前持仓】\n")
	if len(positions) == 0 {
		b.WriteString("(无持仓)\n")
	} else {
		for _, p := range positions {
			b.WriteString(fmt.Sprintf("- %s(%s) 成本%.2f 买入日%s\n", p.StockName, p.StockCode, p.Position.CostPrice, p.Position.BuyDate))
		}
	}

	b.WriteString("\n【情报库笔记(用户自己沉淀的)】\n")
	if len(notes) == 0 {
		b.WriteString("(空)\n")
	} else {
		for _, n := range notes {
			codes := ""
			if len(n.Codes) > 0 {
				codes = " {" + strings.Join(n.Codes, ",") + "}"
			}
			b.WriteString(fmt.Sprintf("[笔记#%d %s]%s %s\n", n.ID, n.CreatedAt[:10], codes, oneLine(n.Text, 300)))
		}
	}

	b.WriteString("\n【近期市场快讯】\n")
	if len(news) == 0 {
		b.WriteString("(无)\n")
	} else {
		for _, t := range news {
			b.WriteString(fmt.Sprintf("[快讯 %s] %s\n", t.Time, oneLine(t.Content, 200)))
		}
	}

	b.WriteString("\n【输出格式(markdown)】\n")
	b.WriteString("## 🔴 正在打脸你的持仓\n")
	b.WriteString("(逐条:哪只票 · 矛盾在哪 · 引用[出处]。没有就写\"未发现明显反证\")\n")
	b.WriteString("## 🔥 本期最强叙事\n")
	b.WriteString("(只根据上面材料里**反复出现/信息在增多**的主题来判断,按强弱排 1-3 条;每条标出是哪些[出处]在支撑,并说这叙事和你持仓的关系。**不许凭常识补充材料里没有的叙事,不许预测它会不会涨。**)\n")
	b.WriteString("## 📌 今天该核实的问题\n")
	b.WriteString("(3条以内,针对上面的矛盾/存疑点,给出具体要去查什么,而不是买卖动作)\n")
	b.WriteString("## 🧵 本期信息串联\n")
	b.WriteString("(把零散笔记/快讯里指向同一件事的连起来,标出各自出处;没有可略)\n")
	return b.String()
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// ===== 链接抓取 + 股票自动关联 =====

var intelURLRe = regexp.MustCompile(`https?://[^\s\x{3000}-\x{303F}\x{FF00}-\x{FFEF}"'<>()]+`)
var intelCodeRe = regexp.MustCompile(`(?i)\b(?:(sh|sz|bj)\s*)?([0-9]{6})\b`)

// enrichIntelLinks 抓取文本里的链接(最多2条),把「标题+正文摘要」追加进笔记。抓取失败原样保留。
func enrichIntelLinks(text string) string {
	urls := intelURLRe.FindAllString(text, 2)
	if len(urls) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range urls {
		title, body := fetchIntelPage(client, u)
		if title == "" && body == "" {
			b.WriteString("\n\n【链接未能抓取,仅存原地址】" + u)
			continue
		}
		b.WriteString("\n\n【链接抓取】" + title + "\n")
		if body != "" {
			b.WriteString(body)
		}
		b.WriteString("\n(来源: " + u + ")")
	}
	return b.String()
}

// fetchIntelPage 抓一页,粗提 <title> 和可见正文前约800字。只处理 text/html。
func fetchIntelPage(client *http.Client, url string) (title, body string) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", ""
	}
	html := string(raw)
	if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(html); len(m) > 1 {
		title = strings.TrimSpace(m[1])
	}
	// 去 script/style 再剥标签,压空白
	html = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`).ReplaceAllString(html, " ")
	html = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, " ")
	html = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(html)
	fields := strings.Fields(html)
	text := strings.Join(fields, " ")
	rs := []rune(text)
	if len(rs) > 800 {
		rs = rs[:800]
		text = string(rs) + "…"
	} else {
		text = string(rs)
	}
	return title, strings.TrimSpace(text)
}

// detectIntelCodes 从正文识别关联股票:带/不带前缀的6位代码 + 全市场股票名称精确包含。
func detectIntelCodes(text string) []string {
	found := make([]string, 0, 8)
	seen := map[string]bool{}
	add := func(sym string) {
		sym = strings.ToLower(strings.TrimSpace(sym))
		if sym == "" || seen[sym] || len(found) >= 10 {
			return
		}
		seen[sym] = true
		found = append(found, sym)
	}
	// ① 代码:sh600519 / SZ000725 / 裸六位(按首位归交易所)
	for _, m := range intelCodeRe.FindAllStringSubmatch(text, 12) {
		prefix, num := strings.ToLower(m[1]), m[2]
		if prefix == "" {
			switch {
			case strings.HasPrefix(num, "6") || strings.HasPrefix(num, "5") || strings.HasPrefix(num, "9"):
				prefix = "sh"
			case strings.HasPrefix(num, "8") || strings.HasPrefix(num, "4"):
				prefix = "bj"
			default:
				prefix = "sz"
			}
		}
		add(prefix + num)
	}
	// ② 名称:全市场目录精确包含(名称已去空格;≥3字才匹配,避免两字名误伤日常用语)
	compact := strings.ReplaceAll(text, " ", "")
	for _, ns := range services.AllStockNameSymbols() {
		if len([]rune(ns.Name)) < 3 {
			continue
		}
		if strings.Contains(compact, ns.Name) {
			add(ns.Symbol)
			if len(found) >= 10 {
				break
			}
		}
	}
	return found
}

// mergeIntelCodes 手填优先,自动识别的补在后面,去重。
func mergeIntelCodes(manual, detected []string) []string {
	out := make([]string, 0, len(manual)+len(detected))
	seen := map[string]bool{}
	for _, c := range append(append([]string{}, manual...), detected...) {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}
