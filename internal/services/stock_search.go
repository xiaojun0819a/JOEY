package services

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/run-bigpig/jcp/internal/embed"
)

type stockBasicData struct {
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

type StockSearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Industry string `json:"industry"`
	Market   string `json:"market"`
}

func searchEmbeddedStocks(keyword string, limit int) []StockSearchResult {
	if keyword == "" {
		return nil
	}

	var basicData stockBasicData
	if err := json.Unmarshal(embed.StockBasicJSON, &basicData); err != nil {
		return nil
	}

	var symbolIdx, nameIdx, industryIdx, tsCodeIdx int = -1, -1, -1, -1
	for i, field := range basicData.Data.Fields {
		switch field {
		case "symbol":
			symbolIdx = i
		case "name":
			nameIdx = i
		case "industry":
			industryIdx = i
		case "ts_code":
			tsCodeIdx = i
		}
	}
	if symbolIdx < 0 || nameIdx < 0 {
		return nil
	}

	results := make([]StockSearchResult, 0, limit)
	upperKeyword := strings.ToUpper(keyword)
	for _, item := range basicData.Data.Items {
		if limit > 0 && len(results) >= limit {
			break
		}

		symbol, _ := item[symbolIdx].(string)
		name, _ := item[nameIdx].(string)
		if !matchStockKeyword(upperKeyword, symbol, name) {
			continue
		}

		industry := ""
		if industryIdx >= 0 && industryIdx < len(item) {
			industry, _ = item[industryIdx].(string)
		}

		market := ""
		fullSymbol := symbol
		if tsCodeIdx >= 0 && tsCodeIdx < len(item) {
			tsCode, _ := item[tsCodeIdx].(string)
			switch {
			case strings.HasSuffix(tsCode, ".SH"):
				market = "上海"
				fullSymbol = "sh" + symbol
			case strings.HasSuffix(tsCode, ".SZ"):
				market = "深圳"
				fullSymbol = "sz" + symbol
			case strings.HasSuffix(tsCode, ".BJ"):
				market = "北京"
				fullSymbol = "bj" + symbol
			}
		}

		results = append(results, StockSearchResult{
			Symbol:   fullSymbol,
			Name:     name,
			Industry: industry,
			Market:   market,
		})
	}
	return results
}

func filterStockCatalog(catalog []StockSearchResult, keyword string, limit int) []StockSearchResult {
	if keyword == "" {
		return nil
	}

	upperKeyword := strings.ToUpper(keyword)
	results := make([]StockSearchResult, 0, limit)
	for _, item := range catalog {
		if limit > 0 && len(results) >= limit {
			break
		}
		if matchStockKeyword(upperKeyword, item.Symbol, item.Name) {
			results = append(results, item)
		}
	}
	return results
}

func matchStockKeyword(keyword string, symbol string, name string) bool {
	upperSymbol := strings.ToUpper(symbol)
	upperName := strings.ToUpper(name)
	if strings.Contains(upperSymbol, keyword) || strings.Contains(upperName, keyword) {
		return true
	}
	// 拼音首字母:纯字母且≥2位的关键词才尝试(如 HYKJ→华依科技),前缀匹配
	if len(keyword) >= 2 && isAsciiLetters(keyword) {
		return strings.HasPrefix(nameInitials(name), keyword)
	}
	return false
}

func isAsciiLetters(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return len(s) > 0
}

// GB2312 一级字库按拼音排序的区位边界(经典首字母提取法,常用字全覆盖;
// 生僻字/多音字非常用读音会有偏差,对股票名足够)
var pyBounds = []struct {
	lo uint16
	c  byte
}{
	{0xB0A1, 'A'}, {0xB0C5, 'B'}, {0xB2C1, 'C'}, {0xB4EE, 'D'}, {0xB6EA, 'E'},
	{0xB7A2, 'F'}, {0xB8C1, 'G'}, {0xB9FE, 'H'}, {0xBBF7, 'J'}, {0xBFA6, 'K'},
	{0xC0AC, 'L'}, {0xC2E8, 'M'}, {0xC4C3, 'N'}, {0xC5B6, 'O'}, {0xC5BE, 'P'},
	{0xC6DA, 'Q'}, {0xC8BB, 'R'}, {0xC8F6, 'S'}, {0xCBFA, 'T'}, {0xCDDA, 'W'},
	{0xCEF4, 'X'}, {0xD1B9, 'Y'}, {0xD4D1, 'Z'},
}

// 首字母串缓存(全市场约5000个名称,首轮搜索后零开销)
var initialsCache sync.Map

// nameInitials 名称→拼音首字母串:汉字取拼音首字母,ASCII 字母数字原样大写,其余跳过。
// 如 华依科技→HYKJ、TCL科技→TCLKJ、*ST海投→STHT。
func nameInitials(name string) string {
	if v, ok := initialsCache.Load(name); ok {
		return v.(string)
	}
	enc := simplifiedchinese.GBK.NewEncoder()
	var sb strings.Builder
	for _, r := range name {
		if r < 128 {
			if r >= 'a' && r <= 'z' {
				sb.WriteByte(byte(r - 32))
			} else if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				sb.WriteByte(byte(r))
			}
			continue
		}
		gb, err := enc.Bytes([]byte(string(r)))
		if err != nil || len(gb) != 2 {
			continue
		}
		v := uint16(gb[0])<<8 | uint16(gb[1])
		if v < 0xB0A1 || v >= 0xD7FA {
			continue // GB2312 二级字库(生僻字)不按拼音排序,跳过
		}
		letter := byte(0)
		for _, b := range pyBounds {
			if v >= b.lo {
				letter = b.c
			}
		}
		if letter != 0 {
			sb.WriteByte(letter)
		}
	}
	out := sb.String()
	initialsCache.Store(name, out)
	return out
}

// ===== 全市场 名称→带前缀代码 目录(供情报文本自动关联股票) =====

type StockNameSymbol struct {
	Name   string
	Symbol string // 如 sh600519
	// Canonical 是不是现用名。false = 历史旧名别名(留着是为了认出博主写的旧称呼),
	// 反查"代码→显示名"时必须优先取 Canonical 的那个,否则界面上会冒出早已废弃的名字。
	Canonical bool
}

var (
	nameCatalogOnce sync.Once
	nameCatalog     []StockNameSymbol

	// 实时名录来源(由 HistoryService 注册)。嵌入表 stock_basic.json 是 2025-05-29 的快照,
	// 到 2026-07 已经**过期 14 个月**:实测 360 个名称已变(ST 戴帽/摘帽/改名)、85 个新代码根本不在表里。
	// 后果是"按名字认股票"会静默漏掉——「中利集团」(表里写旧名 ST中利)、「南网数字」(301638 新股)
	// 两例都是真实漏抓,发生在 X 博主推票解析上。
	// 修法:嵌入表只当兜底底座,最新交易日的 stock_daily.stock_name 覆盖在上面。
	liveNameMu       sync.RWMutex
	liveNameProvider func() map[string]string
	mergedCatalog    []StockNameSymbol
	mergedAt         time.Time
)

// liveNameCatalogTTL 合并结果的缓存时长。名字一天最多变一次(开盘前),半小时足够新,
// 又能挡住"每条推文解析都去扫一遍 5000 行"的重复查询。
const liveNameCatalogTTL = 30 * time.Minute

// SetLiveNameProvider 注册实时名录来源(代码带前缀小写 → 现用名)。传 nil 可注销。
func SetLiveNameProvider(f func() map[string]string) {
	liveNameMu.Lock()
	liveNameProvider, mergedCatalog, mergedAt = f, nil, time.Time{}
	liveNameMu.Unlock()
}

// AllStockNameSymbols 返回全市场(名称,带前缀代码)目录:实时库现名优先,嵌入表兜底。
//
// 排序:**名称长度降序**。名称匹配用 Contains,短名可能是长名的子串
// (如目录里同时有「深科技」和某个含它的四字名时,先匹配长的才不会张冠李戴),
// 调用方按此顺序逐个匹配并把命中的名字从原文里抹掉,即可避免一段文字被两只票同时认领。
func AllStockNameSymbols() []StockNameSymbol {
	base := embeddedNameSymbols()

	liveNameMu.RLock()
	provider, cached, at := liveNameProvider, mergedCatalog, mergedAt
	liveNameMu.RUnlock()
	if provider == nil {
		return base
	}
	if cached != nil && time.Since(at) < liveNameCatalogTTL {
		return cached
	}

	live := provider()
	if len(live) == 0 { // 库没起来/当天还没数据:退回嵌入表,别把目录清空
		return base
	}
	// 新旧名字**都留着**,一起指向同一个代码。
	//
	// 改名是双向的坑,两个方向都实测到过:
	//   · 只认旧名 → 「中利集团」(嵌入表里记的是旧名 ST中利)认不出
	//   · 只认新名 → 「重庆百货」认不出(它已改名重百集团,而老林A股写的还是旧名)
	// 博主写的是他习惯的那个名字,不会跟着交易所改。所以按别名收,谁写哪个都能认。
	// 冲突时以实时库的现名优先(先填 live,旧名只在该名字还没被占用时才补进来)。
	byName := make(map[string]string, len(base)+len(live))
	canon := make(map[string]bool, len(live))
	for code, name := range live {
		code = strings.ToLower(strings.TrimSpace(code))
		name = strings.ReplaceAll(strings.TrimSpace(name), " ", "")
		if code != "" && name != "" {
			byName[name] = code
			canon[name] = true
		}
	}
	for _, ns := range base {
		if _, taken := byName[ns.Name]; !taken {
			byName[ns.Name] = ns.Symbol
		}
	}
	out := make([]StockNameSymbol, 0, len(byName))
	for name, code := range byName {
		out = append(out, StockNameSymbol{Name: name, Symbol: code, Canonical: canon[name]})
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := len([]rune(out[i].Name)), len([]rune(out[j].Name))
		if li != lj {
			return li > lj // 长名在前
		}
		return out[i].Symbol < out[j].Symbol // 同长度按代码定序,保证可复现
	})

	liveNameMu.Lock()
	mergedCatalog, mergedAt = out, time.Now()
	liveNameMu.Unlock()
	return out
}

// embeddedNameSymbols 嵌入表底座(内容不变,解析一次即缓存)。
func embeddedNameSymbols() []StockNameSymbol {
	nameCatalogOnce.Do(func() {
		var basicData stockBasicData
		if err := json.Unmarshal(embed.StockBasicJSON, &basicData); err != nil {
			return
		}
		symbolIdx, nameIdx, tsCodeIdx := -1, -1, -1
		for i, f := range basicData.Data.Fields {
			switch f {
			case "symbol":
				symbolIdx = i
			case "name":
				nameIdx = i
			case "ts_code":
				tsCodeIdx = i
			}
		}
		if symbolIdx < 0 || nameIdx < 0 {
			return
		}
		out := make([]StockNameSymbol, 0, 6000)
		for _, item := range basicData.Data.Items {
			symbol, _ := item[symbolIdx].(string)
			name, _ := item[nameIdx].(string)
			if symbol == "" || name == "" {
				continue
			}
			full := symbol
			if tsCodeIdx >= 0 && tsCodeIdx < len(item) {
				tsCode, _ := item[tsCodeIdx].(string)
				switch {
				case strings.HasSuffix(tsCode, ".SH"):
					full = "sh" + symbol
				case strings.HasSuffix(tsCode, ".SZ"):
					full = "sz" + symbol
				case strings.HasSuffix(tsCode, ".BJ"):
					full = "bj" + symbol
				}
			}
			// 名称统一去空格(如"万 科A"历史格式),匹配时用 Contains
			out = append(out, StockNameSymbol{Name: strings.ReplaceAll(name, " ", ""), Symbol: strings.ToLower(full), Canonical: true})
		}
		nameCatalog = out
	})
	return nameCatalog
}
