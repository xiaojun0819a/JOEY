package services

import (
	"encoding/json"
	"strings"
	"sync"

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
