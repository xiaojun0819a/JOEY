package services

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/embed"
	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var log = logger.New("market")

// 预编译正则表达式，避免重复编译
var (
	sinaStockRegex = regexp.MustCompile(`var hq_str_(\w+)="([^"]*)"`)
	sinaIndexRegex = regexp.MustCompile(`var hq_str_s_(\w+)="([^"]*)"`)
)

const (
	sinaStockURL       = "http://hq.sinajs.cn/rn=%d&list=%s"
	sinaKLineURL       = "http://quotes.sina.cn/cn/api/json_v2.php/CN_MarketDataService.getKLineData?symbol=%s&scale=%s&ma=5,10,20&datalen=%d"
	emBoardFundFlowURL = "https://push2.eastmoney.com/api/qt/clist/get"
	emFundFlowKLineURL = "https://push2.eastmoney.com/api/qt/stock/fflow/kline/get"
	emAnnouncementURL  = "https://np-anotice-stock.eastmoney.com/api/security/ann"
	qqFundFlowURL      = "https://proxy.finance.qq.com/cgi/cgi-bin/fundflow/hsfundtab"
)

const (
	klineCacheTTLIntraday = 2 * time.Second
	klineCacheTTLDefault  = 30 * time.Second
)

// 默认大盘指数代码
var defaultIndexCodes = []string{
	"s_sh000001", // 上证指数
	"s_sz399001", // 深证成指
	"s_sz399006", // 创业板指
}

// StockWithOrderBook 包含盘口数据的股票信息
type StockWithOrderBook struct {
	models.Stock
	OrderBook models.OrderBook `json:"orderBook"`
}

// stockCache 股票数据缓存
type stockCache struct {
	data      []StockWithOrderBook
	timestamp time.Time
}

// klineCache K线数据缓存
type klineCache struct {
	data      []models.KLineData
	timestamp time.Time
	ttl       time.Duration
}

// MarketStatus 市场交易状态
type MarketStatus struct {
	Status      string `json:"status"`      // trading, closed, pre_market, lunch_break
	StatusText  string `json:"statusText"`  // 中文状态描述
	IsTradeDay  bool   `json:"isTradeDay"`  // 是否交易日
	HolidayName string `json:"holidayName"` // 节假日名称（如有）
}

// TradingPeriod 交易时段
type TradingPeriod struct {
	Status    string `json:"status"`    // 状态标识
	Text      string `json:"text"`      // 中文描述
	StartTime string `json:"startTime"` // 开始时间 HH:MM
	EndTime   string `json:"endTime"`   // 结束时间 HH:MM
}

// TradingSchedule 交易时间表
type TradingSchedule struct {
	IsTradeDay  bool            `json:"isTradeDay"`  // 今天是否交易日
	HolidayName string          `json:"holidayName"` // 节假日名称
	Periods     []TradingPeriod `json:"periods"`     // 时段列表
}

// MarketService 市场数据服务
type MarketService struct {
	client           *http.Client
	primaryProvider  marketProvider
	fallbackProvider marketProvider

	// 股票数据缓存
	cache    map[string]*stockCache
	cacheMu  sync.RWMutex
	cacheTTL time.Duration

	// K线数据缓存
	klineCache    map[string]*klineCache
	klineCacheMu  sync.RWMutex
	klineCacheTTL time.Duration
}

// NewMarketService 创建市场数据服务
func NewMarketService() *MarketService {
	ms := newMarketService()
	ms.primaryProvider = newTDXMarketProvider()
	ms.fallbackProvider = newSinaMarketProvider(ms)
	return ms
}

// cleanCacheLoop 定期清理过期缓存，防止内存泄漏
func (ms *MarketService) cleanCacheLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ms.cleanExpiredCache()
	}
}

// cleanExpiredCache 清理过期缓存
func (ms *MarketService) cleanExpiredCache() {
	now := time.Now()

	// 清理股票缓存
	ms.cacheMu.Lock()
	for key, cached := range ms.cache {
		if now.Sub(cached.timestamp) > 10*time.Second {
			delete(ms.cache, key)
		}
	}
	ms.cacheMu.Unlock()

	// 清理K线缓存
	ms.klineCacheMu.Lock()
	for key, cached := range ms.klineCache {
		ttl := cached.ttl
		if ttl <= 0 {
			ttl = ms.klineCacheTTL
		}
		// 使用 3 倍 TTL 做内存回收，避免活跃缓存被过早清理
		if now.Sub(cached.timestamp) > ttl*3 {
			delete(ms.klineCache, key)
		}
	}
	ms.klineCacheMu.Unlock()
}

// getKLineCacheTTL 返回不同周期的缓存策略
func (ms *MarketService) getKLineCacheTTL(period string) time.Duration {
	// 分时需要高时效，避免增量推送读取到过旧缓存
	if period == "1m" {
		return klineCacheTTLIntraday
	}
	return ms.klineCacheTTL
}

// GetStockDataWithOrderBook 获取股票实时数据（含真实盘口），带缓存
func (ms *MarketService) GetStockDataWithOrderBook(codes ...string) ([]StockWithOrderBook, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 排序codes保证缓存key一致性
	sortedCodes := make([]string, len(codes))
	copy(sortedCodes, codes)
	sort.Strings(sortedCodes)
	cacheKey := strings.Join(sortedCodes, ",")

	// 检查缓存
	ms.cacheMu.RLock()
	if cached, ok := ms.cache[cacheKey]; ok {
		if time.Since(cached.timestamp) < ms.cacheTTL {
			ms.cacheMu.RUnlock()
			return cached.data, nil
		}
	}
	ms.cacheMu.RUnlock()

	// 从API获取数据
	data, err := ms.fetchStockDataWithFallback(codes...)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	ms.cacheMu.Lock()
	ms.cache[cacheKey] = &stockCache{
		data:      data,
		timestamp: time.Now(),
	}
	ms.cacheMu.Unlock()

	return data, nil
}

// fetchStockDataWithOrderBook 从API获取股票数据（含盘口）
func (ms *MarketService) fetchStockDataWithOrderBookFromSina(codes ...string) ([]StockWithOrderBook, error) {
	codeList := strings.Join(codes, ",")
	url := fmt.Sprintf(sinaStockURL, time.Now().UnixNano(), codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://finance.sina.com.cn")

	resp, err := ms.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return ms.parseSinaStockDataWithOrderBook(string(body))
}

// parseSinaStockDataWithOrderBook 解析新浪股票数据（含盘口）
func (ms *MarketService) parseSinaStockDataWithOrderBook(data string) ([]StockWithOrderBook, error) {
	var stocks []StockWithOrderBook
	matches := sinaStockRegex.FindAllStringSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 3 || match[2] == "" {
			continue
		}
		parts := strings.Split(match[2], ",")
		if len(parts) < 32 {
			continue
		}
		stock := ms.parseStockWithOrderBook(match[1], parts)
		stocks = append(stocks, stock)
	}
	return stocks, nil
}

// GetStockRealTimeData 获取股票实时数据
func (ms *MarketService) GetStockRealTimeData(codes ...string) ([]models.Stock, error) {
	data, err := ms.GetStockDataWithOrderBook(codes...)
	if err != nil {
		return nil, err
	}

	stocks := make([]models.Stock, 0, len(data))
	for _, item := range data {
		stocks = append(stocks, item.Stock)
	}
	return stocks, nil
}

// parseSinaStockData 解析新浪股票数据
func (ms *MarketService) parseSinaStockData(data string, codes []string) ([]models.Stock, error) {
	var stocks []models.Stock
	matches := sinaStockRegex.FindAllStringSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 3 || match[2] == "" {
			continue
		}
		parts := strings.Split(match[2], ",")
		if len(parts) < 32 {
			continue
		}

		stock := ms.parseStockFields(match[1], parts)
		stocks = append(stocks, stock)
	}
	return stocks, nil
}

// parseStockFields 解析股票字段
func (ms *MarketService) parseStockFields(code string, parts []string) models.Stock {
	price, _ := strconv.ParseFloat(parts[3], 64)
	open, _ := strconv.ParseFloat(parts[1], 64)
	high, _ := strconv.ParseFloat(parts[4], 64)
	low, _ := strconv.ParseFloat(parts[5], 64)
	preClose, _ := strconv.ParseFloat(parts[2], 64)
	volume, _ := strconv.ParseInt(parts[8], 10, 64)
	amount, _ := strconv.ParseFloat(parts[9], 64)

	change := price - preClose
	changePercent := 0.0
	if preClose > 0 {
		changePercent = (change / preClose) * 100
	}

	return models.Stock{
		Symbol:        code,
		Name:          parts[0],
		Price:         price,
		Open:          open,
		High:          high,
		Low:           low,
		PreClose:      preClose,
		Change:        change,
		ChangePercent: changePercent,
		Volume:        volume,
		Amount:        amount,
	}
}

// parseStockWithOrderBook 解析股票字段和真实盘口数据
// 新浪API返回数据格式: 名称,今开,昨收,当前价,最高,最低,买一价,卖一价,成交量,成交额,
// 买一量,买一价,买二量,买二价,买三量,买三价,买四量,买四价,买五量,买五价,
// 卖一量,卖一价,卖二量,卖二价,卖三量,卖三价,卖四量,卖四价,卖五量,卖五价,日期,时间
func (ms *MarketService) parseStockWithOrderBook(code string, parts []string) StockWithOrderBook {
	stock := ms.parseStockFields(code, parts)

	// 解析真实五档盘口数据
	var bids, asks []models.OrderBookItem

	// 买盘数据 (索引 10-19: 买一量,买一价,买二量,买二价...)
	if len(parts) >= 20 {
		for i := 0; i < 5; i++ {
			volIdx := 10 + i*2
			priceIdx := 11 + i*2
			if priceIdx < len(parts) {
				bidVol, _ := strconv.ParseInt(parts[volIdx], 10, 64)
				bidPrice, _ := strconv.ParseFloat(parts[priceIdx], 64)
				if bidPrice > 0 {
					bids = append(bids, models.OrderBookItem{
						Price: bidPrice,
						Size:  bidVol / 100, // 转换为手
					})
				}
			}
		}
	}

	// 卖盘数据 (索引 20-29: 卖一量,卖一价,卖二量,卖二价...)
	if len(parts) >= 30 {
		for i := 0; i < 5; i++ {
			volIdx := 20 + i*2
			priceIdx := 21 + i*2
			if priceIdx < len(parts) {
				askVol, _ := strconv.ParseInt(parts[volIdx], 10, 64)
				askPrice, _ := strconv.ParseFloat(parts[priceIdx], 64)
				if askPrice > 0 {
					asks = append(asks, models.OrderBookItem{
						Price: askPrice,
						Size:  askVol / 100, // 转换为手
					})
				}
			}
		}
	}

	// 计算累计量和占比
	calculateOrderBookTotals(bids)
	calculateOrderBookTotals(asks)

	return StockWithOrderBook{
		Stock:     stock,
		OrderBook: models.OrderBook{Bids: bids, Asks: asks},
	}
}

// calculateOrderBookTotals 计算盘口累计量和占比
func calculateOrderBookTotals(items []models.OrderBookItem) {
	if len(items) == 0 {
		return
	}

	var total int64
	var maxSize int64
	for _, item := range items {
		if item.Size > maxSize {
			maxSize = item.Size
		}
	}

	for i := range items {
		total += items[i].Size
		items[i].Total = total
		if maxSize > 0 {
			items[i].Percent = float64(items[i].Size) / float64(maxSize)
		}
	}
}

// GetKLineData 获取K线数据（带缓存）
func (ms *MarketService) GetKLineData(code string, period string, days int) ([]models.KLineData, error) {
	cacheKey := fmt.Sprintf("%s:%s:%d", code, period, days)
	ttl := ms.getKLineCacheTTL(period)

	// 检查缓存
	ms.klineCacheMu.RLock()
	if cached, ok := ms.klineCache[cacheKey]; ok {
		cachedTTL := cached.ttl
		if cachedTTL <= 0 {
			cachedTTL = ttl
		}
		if time.Since(cached.timestamp) < cachedTTL {
			ms.klineCacheMu.RUnlock()
			return cached.data, nil
		}
	}
	ms.klineCacheMu.RUnlock()

	// 从API获取数据
	klines, err := ms.fetchKLineDataWithFallback(code, period, days)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	ms.klineCacheMu.Lock()
	ms.klineCache[cacheKey] = &klineCache{
		data:      klines,
		timestamp: time.Now(),
		ttl:       ttl,
	}
	ms.klineCacheMu.Unlock()

	return klines, nil
}

// fetchKLineData 从API获取K线数据
func (ms *MarketService) fetchKLineDataFromSina(code string, period string, days int) ([]models.KLineData, error) {
	scale := ms.periodToScale(period)
	url := fmt.Sprintf(sinaKLineURL, code, scale, days)

	resp, err := ms.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	klines, err := ms.parseKLineData(string(body))
	if err != nil {
		return nil, err
	}

	// 分时模式下只返回当天的数据，并计算均价线
	if period == "1m" {
		klines = filterTodayKLines(klines)
		klines = calculateAvgLine(klines)
	}

	return klines, nil
}

// periodToScale 周期转换为新浪API的scale参数
func (ms *MarketService) periodToScale(period string) string {
	switch period {
	case "1m":
		return "1" // 1分钟线（分时图）
	case "1d":
		return "240" // 日线
	case "1w":
		return "1680" // 周线
	case "1mo":
		return "7200" // 月线
	default:
		return "240"
	}
}

// filterTodayKLines 过滤只返回当天的K线数据
func filterTodayKLines(klines []models.KLineData) []models.KLineData {
	if len(klines) == 0 {
		return klines
	}

	today := time.Now().Format("2006-01-02")
	result := make([]models.KLineData, 0)

	for _, k := range klines {
		// 时间格式为 "2006-01-02 15:04:05"，取日期部分比较
		if len(k.Time) >= 10 && k.Time[:10] == today {
			result = append(result, k)
		}
	}

	// 如果当天没有数据（非交易日），返回最后一天的数据
	if len(result) == 0 && len(klines) > 0 {
		lastDay := klines[len(klines)-1].Time[:10]
		for _, k := range klines {
			if len(k.Time) >= 10 && k.Time[:10] == lastDay {
				result = append(result, k)
			}
		}
	}

	return result
}

// calculateAvgLine 计算分时均价线 (VWAP = 累计成交额 / 累计成交量)
func calculateAvgLine(klines []models.KLineData) []models.KLineData {
	if len(klines) == 0 {
		return klines
	}

	var totalAmount float64
	var totalVolume int64

	for i := range klines {
		totalAmount += klines[i].Amount
		totalVolume += klines[i].Volume

		if totalVolume > 0 {
			klines[i].Avg = totalAmount / float64(totalVolume)
		}
	}

	return klines
}

// parseKLineData 解析K线数据 - 使用标准JSON解析
func (ms *MarketService) parseKLineData(data string) ([]models.KLineData, error) {
	// 新浪API返回的K线数据结构（含均线和成交额）
	type sinaKLine struct {
		Day       string  `json:"day"`
		Open      string  `json:"open"`
		High      string  `json:"high"`
		Low       string  `json:"low"`
		Close     string  `json:"close"`
		Volume    string  `json:"volume"`
		Amount    string  `json:"amount"`
		MAPrice5  float64 `json:"ma_price5"`
		MAPrice10 float64 `json:"ma_price10"`
		MAPrice20 float64 `json:"ma_price20"`
	}

	var sinaData []sinaKLine
	if err := json.Unmarshal([]byte(data), &sinaData); err != nil {
		return nil, err
	}

	klines := make([]models.KLineData, 0, len(sinaData))
	for _, item := range sinaData {
		open, _ := strconv.ParseFloat(item.Open, 64)
		high, _ := strconv.ParseFloat(item.High, 64)
		low, _ := strconv.ParseFloat(item.Low, 64)
		closePrice, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseInt(item.Volume, 10, 64)
		amount, _ := strconv.ParseFloat(item.Amount, 64)

		klines = append(klines, models.KLineData{
			Time:   item.Day,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: volume,
			Amount: amount,
			MA5:    item.MAPrice5,
			MA10:   item.MAPrice10,
			MA20:   item.MAPrice20,
		})
	}
	return klines, nil
}

// GetRealOrderBook 获取真实盘口数据
func (ms *MarketService) GetRealOrderBook(code string) (models.OrderBook, error) {
	data, err := ms.GetStockDataWithOrderBook(code)
	if err != nil || len(data) == 0 {
		return models.OrderBook{}, err
	}
	return data[0].OrderBook, nil
}

// GenerateOrderBook 生成盘口数据（保留兼容，建议使用 GetRealOrderBook）
func (ms *MarketService) GenerateOrderBook(price float64) models.OrderBook {
	var bids, asks []models.OrderBookItem

	for i := 0; i < 5; i++ {
		bidPrice := price - float64(i+1)*0.01
		askPrice := price + float64(i+1)*0.01

		bids = append(bids, models.OrderBookItem{
			Price:   bidPrice,
			Size:    int64(100 + i*50),
			Total:   int64((100 + i*50) * (i + 1)),
			Percent: float64(100-i*15) / 100,
		})
		asks = append(asks, models.OrderBookItem{
			Price:   askPrice,
			Size:    int64(100 + i*50),
			Total:   int64((100 + i*50) * (i + 1)),
			Percent: float64(100-i*15) / 100,
		})
	}

	return models.OrderBook{Bids: bids, Asks: asks}
}

// GetMarketStatus 获取当前市场交易状态
func (ms *MarketService) GetMarketStatus() MarketStatus {
	now := time.Now()
	// 使用固定时区 UTC+8，避免 Windows 缺少时区数据库的问题
	loc := time.FixedZone("CST", 8*60*60)
	now = now.In(loc)
	// 检查是否为交易日
	isTradeDay, holidayName := ms.isTradeDay(now)
	if !isTradeDay {
		statusText := "休市"
		if holidayName != "" {
			statusText = holidayName + "休市"
		} else if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
			statusText = "周末休市"
		}
		result := MarketStatus{
			Status:      "closed",
			StatusText:  statusText,
			IsTradeDay:  false,
			HolidayName: holidayName,
		}
		return result
	}

	// 交易日，判断当前时间段
	hour, minute := now.Hour(), now.Minute()
	currentMinutes := hour*60 + minute

	// A股交易时间: 9:30-11:30, 13:00-15:00
	var result MarketStatus
	switch {
	case currentMinutes < 9*60+15:
		result = MarketStatus{Status: "pre_market", StatusText: "盘前", IsTradeDay: true}
	case currentMinutes < 9*60+30:
		result = MarketStatus{Status: "pre_market", StatusText: "集合竞价", IsTradeDay: true}
	case currentMinutes < 11*60+30:
		result = MarketStatus{Status: "trading", StatusText: "交易中", IsTradeDay: true}
	case currentMinutes < 13*60:
		result = MarketStatus{Status: "lunch_break", StatusText: "午间休市", IsTradeDay: true}
	case currentMinutes < 15*60:
		result = MarketStatus{Status: "trading", StatusText: "交易中", IsTradeDay: true}
	default:
		result = MarketStatus{Status: "closed", StatusText: "已收盘", IsTradeDay: true}
	}
	return result
}

// GetTradingSchedule 获取交易时间表（供前端判断市场状态）
func (ms *MarketService) GetTradingSchedule() TradingSchedule {
	now := time.Now()
	loc := time.FixedZone("CST", 8*60*60)
	now = now.In(loc)

	isTradeDay, holidayName := ms.isTradeDay(now)

	// A股交易时段配置
	periods := []TradingPeriod{
		{Status: "pre_market", Text: "盘前", StartTime: "00:00", EndTime: "09:15"},
		{Status: "pre_market", Text: "集合竞价", StartTime: "09:15", EndTime: "09:30"},
		{Status: "trading", Text: "交易中", StartTime: "09:30", EndTime: "11:30"},
		{Status: "lunch_break", Text: "午间休市", StartTime: "11:30", EndTime: "13:00"},
		{Status: "trading", Text: "交易中", StartTime: "13:00", EndTime: "15:00"},
		{Status: "closed", Text: "已收盘", StartTime: "15:00", EndTime: "24:00"},
	}

	return TradingSchedule{
		IsTradeDay:  isTradeDay,
		HolidayName: holidayName,
		Periods:     periods,
	}
}

// isTradeDay 判断指定日期是否为交易日
// A股交易日判定：非周末 且 非节假日（调休上班也不算交易日）
func (ms *MarketService) isTradeDay(date time.Time) (bool, string) {

	// 周末一律不是交易日
	weekday := date.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false, "周末"
	}

	// 工作日：检查是否为节假日
	isOffDay, inList, note := ms.getHolidayStatus(date)
	if inList && isOffDay {
		return false, note
	}

	return true, ""
}

// getHolidayStatus 获取指定日期的节假日状态
// 返回: isOffDay=true表示休息日, inList=是否在节假日列表中, note为节假日名称
func (ms *MarketService) getHolidayStatus(date time.Time) (isOffDay bool, inList bool, note string) {
	dateStr := date.Format("2006-01-02")
	year := date.Year()

	// 加载该年份的节假日数据
	yearData, err := ms.loadHolidayData(year)
	if err != nil {
		log.Warn("加载 %d 年节假日数据失败: %v", year, err)
		return false, false, ""
	}

	// 查找该日期
	if isOff, exists := yearData[dateStr]; exists {
		noteName := ms.getHolidayNote(year, dateStr)
		return isOff, true, noteName
	}

	// 不在节假日列表中
	return false, false, ""
}

// getHolidayNote 获取节假日名称
func (ms *MarketService) getHolidayNote(year int, dateStr string) string {
	cacheFile := getHolidayCacheFile(year)
	fileData, err := os.ReadFile(cacheFile)
	if err != nil {
		return ""
	}

	var hd holidayData
	if json.Unmarshal(fileData, &hd) != nil {
		return ""
	}

	for _, day := range hd.Days {
		if day.Date == dateStr {
			return day.Name
		}
	}
	return ""
}

// tradeDatesCache 交易日缓存文件结构
type tradeDatesCache struct {
	TradeDates []string  `json:"tradeDates"` // 交易日列表
	UpdatedAt  time.Time `json:"updatedAt"`  // 更新时间
}

// holidayData 节假日数据结构
type holidayData struct {
	Year int          `json:"year"`
	Days []holidayDay `json:"days"`
}

type holidayDay struct {
	Name     string `json:"name"`
	Date     string `json:"date"`
	IsOffDay bool   `json:"isOffDay"`
}

// holidayCache 节假日缓存（按年份）
var (
	holidayCacheMu   sync.RWMutex
	holidayCacheData = make(map[int]map[string]bool) // year -> date -> isOffDay
)

const holidayCDNURL = "https://cdn.jsdelivr.net/gh/NateScarlet/holiday-cn@master/%d.json"

// getHolidayCacheFile 获取节假日缓存文件路径
func getHolidayCacheFile(year int) string {
	return filepath.Join(paths.EnsureCacheDir("holiday"), fmt.Sprintf("%d.json", year))
}

// loadHolidayData 加载指定年份的节假日数据
func (ms *MarketService) loadHolidayData(year int) (map[string]bool, error) {
	// 先检查内存缓存
	holidayCacheMu.RLock()
	if data, ok := holidayCacheData[year]; ok {
		holidayCacheMu.RUnlock()
		return data, nil
	}
	holidayCacheMu.RUnlock()

	// 尝试从文件缓存加载
	cacheFile := getHolidayCacheFile(year)
	if fileData, err := os.ReadFile(cacheFile); err == nil {
		var hd holidayData
		if json.Unmarshal(fileData, &hd) == nil {
			data := ms.parseHolidayData(&hd)
			holidayCacheMu.Lock()
			holidayCacheData[year] = data
			holidayCacheMu.Unlock()
			return data, nil
		}
	}

	// 从CDN获取
	return ms.fetchHolidayData(year)
}

// fetchHolidayData 从CDN获取节假日数据
func (ms *MarketService) fetchHolidayData(year int) (map[string]bool, error) {
	url := fmt.Sprintf(holidayCDNURL, year)
	resp, err := ms.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("获取节假日数据失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var hd holidayData
	if err := json.Unmarshal(body, &hd); err != nil {
		return nil, err
	}

	// 保存到文件缓存
	cacheFile := getHolidayCacheFile(year)
	os.WriteFile(cacheFile, body, 0644)

	// 解析并缓存到内存
	data := ms.parseHolidayData(&hd)
	holidayCacheMu.Lock()
	holidayCacheData[year] = data
	holidayCacheMu.Unlock()

	log.Info("加载 %d 年节假日数据，共 %d 条", year, len(hd.Days))
	return data, nil
}

// parseHolidayData 解析节假日数据为 map
func (ms *MarketService) parseHolidayData(hd *holidayData) map[string]bool {
	data := make(map[string]bool)
	for _, day := range hd.Days {
		data[day.Date] = day.IsOffDay
	}
	return data
}

// isTradeDate 判断指定日期是否为交易日
// A股交易日 = 非周末 且 非节假日（调休上班也不算交易日）
func (ms *MarketService) isTradeDate(date time.Time) bool {
	isTradeDay, _ := ms.isTradeDay(date)
	return isTradeDay
}

// getTradeDatesCacheFile 获取交易日缓存文件路径
func getTradeDatesCacheFile() string {
	return filepath.Join(paths.EnsureCacheDir(""), "trade_dates.json")
}

// GetTradeDates 获取指定天数内的交易日列表（从今天往前推）
func (ms *MarketService) GetTradeDates(days int) ([]string, error) {
	// 先尝试从文件缓存加载
	cached, err := ms.loadTradeDatesCache()
	if err == nil && len(cached.TradeDates) > 0 {
		// 检查缓存是否过期（每天更新一次）
		if time.Since(cached.UpdatedAt) < 24*time.Hour {
			log.Debug("使用交易日缓存，共 %d 天", len(cached.TradeDates))
			return ms.filterTradeDates(cached.TradeDates, days), nil
		}
	}

	// 缓存不存在或过期，重新获取
	log.Info("开始获取交易日列表")
	tradeDates, err := ms.fetchTradeDates(90) // 获取90天的数据
	if err != nil {
		// 如果获取失败但有旧缓存，使用旧缓存
		if cached != nil && len(cached.TradeDates) > 0 {
			log.Warn("获取交易日失败，使用旧缓存: %v", err)
			return ms.filterTradeDates(cached.TradeDates, days), nil
		}
		return nil, err
	}

	// 保存到文件缓存
	if err := ms.saveTradeDatesCache(tradeDates); err != nil {
		log.Warn("保存交易日缓存失败: %v", err)
	}

	return ms.filterTradeDates(tradeDates, days), nil
}

// filterTradeDates 过滤交易日列表，只返回指定天数
func (ms *MarketService) filterTradeDates(dates []string, days int) []string {
	if len(dates) <= days {
		return dates
	}
	return dates[:days]
}

// loadTradeDatesCache 从文件加载交易日缓存
func (ms *MarketService) loadTradeDatesCache() (*tradeDatesCache, error) {
	data, err := os.ReadFile(getTradeDatesCacheFile())
	if err != nil {
		return nil, err
	}
	var cache tradeDatesCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

// saveTradeDatesCache 保存交易日缓存到文件
func (ms *MarketService) saveTradeDatesCache(dates []string) error {
	cache := tradeDatesCache{
		TradeDates: dates,
		UpdatedAt:  time.Now(),
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getTradeDatesCacheFile(), data, 0644)
}

// fetchTradeDates 获取交易日列表
func (ms *MarketService) fetchTradeDates(days int) ([]string, error) {
	var tradeDates []string
	today := time.Now()

	// 预加载需要的年份节假日数据
	yearsNeeded := make(map[int]bool)
	for i := 0; i < days; i++ {
		yearsNeeded[today.AddDate(0, 0, -i).Year()] = true
	}
	for year := range yearsNeeded {
		if _, err := ms.loadHolidayData(year); err != nil {
			log.Warn("加载 %d 年节假日数据失败: %v", year, err)
		}
	}

	for i := 0; i < days; i++ {
		date := today.AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")

		if ms.isTradeDate(date) {
			tradeDates = append(tradeDates, dateStr)
		}
	}

	log.Info("获取到 %d 个交易日", len(tradeDates))
	return tradeDates, nil
}

// GetMarketIndices 获取大盘指数数据
func (ms *MarketService) GetMarketIndices() ([]models.MarketIndex, error) {
	return ms.fetchMarketIndicesWithFallback()
}

// SearchStocks 搜索股票
func (ms *MarketService) SearchStocks(keyword string, limit int) []StockSearchResult {
	return ms.searchStocksWithFallback(keyword, limit)
}

// ScanSnapshotRow 全A扫描的单只股票快照
type ScanSnapshotRow struct {
	Symbol             string
	Name               string
	Price              float64
	ChangePercent      float64
	Amount             float64
	TurnoverRate       float64
	TotalMarketCap     float64
	FloatMarketCap     float64
	Industry           string
	IsST               bool
	MainNetInflow      float64
	MainNetInflowRatio float64
	MainFlowSource     string
	UpdateTime         string
}

// ScanMarketSnapshot 全A扫描的大盘快照
type ScanMarketSnapshot struct {
	ShPrice        float64
	ShMA20         float64
	LimitUpCount   int
	LimitDownCount int
	TotalAmount    float64
}

// GetAllAStockSnapshot 拉取全A（沪深，按需含北交所）扫描快照
func (ms *MarketService) GetAllAStockSnapshot(includeBeijing bool) ([]ScanSnapshotRow, error) {
	items, err := ms.getAllAStockSnapshotFromEastmoney(includeBeijing)
	if err == nil && len(items) > 0 {
		return items, nil
	}
	if err != nil {
		log.Warn("东财全A快照失败，切换腾讯源: %v", err)
	}
	fallbackItems, fallbackErr := ms.getAllAStockSnapshotFromTencent(includeBeijing)
	if fallbackErr == nil && len(fallbackItems) > 0 {
		ms.enrichSnapshotMainFlowWithQQFund(fallbackItems)
		return fallbackItems, nil
	}
	if err != nil && fallbackErr != nil {
		return nil, fmt.Errorf("东财源失败: %v；腾讯源失败: %v", err, fallbackErr)
	}
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	return nil, err
}

func (ms *MarketService) getAllAStockSnapshotFromEastmoney(includeBeijing bool) ([]ScanSnapshotRow, error) {
	fs := "m:0 t:6,m:0 t:80,m:1 t:2,m:1 t:23"
	if includeBeijing {
		fs += ",m:0 t:81 s:2048"
	}

	page := 1
	pageSize := 200
	maxPages := 30
	total := int64(0)
	items := make([]ScanSnapshotRow, 0, 4000)

	for page <= maxPages {
		params := url.Values{}
		params.Set("np", "1")
		params.Set("fltt", "2")
		params.Set("invt", "2")
		params.Set("fid", "f3")
		params.Set("po", "1")
		params.Set("pn", strconv.Itoa(page))
		params.Set("pz", strconv.Itoa(pageSize))
		params.Set("fs", fs)
		params.Set("fields", "f12,f14,f2,f3,f6,f8,f20,f21,f62,f184,f124,f13")
		params.Set("ut", "8dec03ba335b81bf4ebdf7b29ec27d15")

		raw, err := ms.fetchMarketJSON(emBoardFundFlowURL+"?"+params.Encode(), map[string]string{
			"Referer":    "https://quote.eastmoney.com/",
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		})
		if err != nil {
			return nil, err
		}

		data, ok := raw["data"].(map[string]any)
		if !ok || data == nil {
			return nil, fmt.Errorf("全A扫描响应缺少data")
		}
		if page == 1 {
			total = int64(toInt64Any(data["total"]))
		}

		rows := toMapSliceLocal(toSliceAnyLocal(data["diff"]))
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			code := strings.TrimSpace(toStringLocal(row["f12"]))
			if code == "" {
				continue
			}
			marketID := toInt64Any(row["f13"])
			symbol := formatSymbolByMarket(code, marketID)
			if symbol == "" {
				continue
			}

			name := strings.TrimSpace(toStringLocal(row["f14"]))
			items = append(items, ScanSnapshotRow{
				Symbol:             symbol,
				Name:               name,
				Price:              toFloat64Any(row["f2"]),
				ChangePercent:      toFloat64Any(row["f3"]),
				Amount:             toFloat64Any(row["f6"]),
				TurnoverRate:       toFloat64Any(row["f8"]),
				TotalMarketCap:     toFloat64Any(row["f20"]),
				FloatMarketCap:     toFloat64Any(row["f21"]),
				Industry:           "",
				IsST:               isSTName(name),
				MainNetInflow:      toFloat64Any(row["f62"]),
				MainNetInflowRatio: toFloat64Any(row["f184"]),
				MainFlowSource:     "eastmoney",
				UpdateTime:         formatEastmoneyTimestamp(toInt64Any(row["f124"])),
			})
		}

		if int64(len(items)) >= total || len(rows) < pageSize {
			break
		}
		page++
	}

	return items, nil
}

func (ms *MarketService) getAllAStockSnapshotFromTencent(includeBeijing bool) ([]ScanSnapshotRow, error) {
	catalog := loadEmbeddedStockCatalog(includeBeijing)
	if len(catalog) == 0 {
		return nil, fmt.Errorf("腾讯快照回退失败：本地股票目录为空")
	}

	// 腾讯接口单次 URL 过长会失败，这里做小批量。
	const batchSize = 60
	items := make([]ScanSnapshotRow, 0, len(catalog))
	nowText := time.Now().Format("2006-01-02 15:04:05")

	for i := 0; i < len(catalog); i += batchSize {
		end := i + batchSize
		if end > len(catalog) {
			end = len(catalog)
		}
		chunk := catalog[i:end]
		symbols := make([]string, 0, len(chunk))
		meta := make(map[string]embeddedStockMeta, len(chunk))
		for _, item := range chunk {
			symbols = append(symbols, item.Symbol)
			meta[item.Symbol] = item
		}

		qURL := "https://qt.gtimg.cn/q=" + strings.Join(symbols, ",")
		req, reqErr := http.NewRequest("GET", qURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Referer", "https://gu.qq.com/")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

		resp, doErr := ms.client.Do(req)
		if doErr != nil {
			return nil, doErr
		}
		reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
		body, readErr := io.ReadAll(reader)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		// 同批次追加腾讯盘口买卖比数据（作为主力代理分数）
		pkURL := "https://qt.gtimg.cn/q=" + buildTencentPKSymbols(symbols)
		pkReq, pkReqErr := http.NewRequest("GET", pkURL, nil)
		if pkReqErr != nil {
			return nil, pkReqErr
		}
		pkReq.Header.Set("Referer", "https://gu.qq.com/")
		pkReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
		pkResp, pkDoErr := ms.client.Do(pkReq)
		if pkDoErr != nil {
			return nil, pkDoErr
		}
		pkReader := transform.NewReader(pkResp.Body, simplifiedchinese.GBK.NewDecoder())
		pkBody, pkReadErr := io.ReadAll(pkReader)
		pkResp.Body.Close()
		if pkReadErr != nil {
			return nil, pkReadErr
		}
		pkScore := parseTencentPKScoreMap(string(pkBody))

		lines := strings.Split(string(body), ";")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "v_") || !strings.Contains(line, "=\"") {
				continue
			}
			lhsRhs := strings.SplitN(line, "=\"", 2)
			if len(lhsRhs) != 2 {
				continue
			}
			symbol := strings.TrimPrefix(lhsRhs[0], "v_")
			payload := strings.TrimSuffix(lhsRhs[1], "\"")
			if payload == "" {
				continue
			}
			fields := strings.Split(payload, "~")
			// 关键字段按公开索引：3现价, 32涨跌幅, 37成交额(万), 38换手率, 44/45市值
			if len(fields) < 46 {
				continue
			}

			price := parseFloat64Safe(fields[3])
			changePct := parseFloat64Safe(fields[32])
			amountWan := parseFloat64Safe(fields[37])
			turnover := parseFloat64Safe(fields[38])
			mcap44 := parseFloat64Safe(fields[44])
			mcap45 := parseFloat64Safe(fields[45])

			// 兼容索引口径差异：总市值一般 >= 流通市值
			totalCapYi := math.Max(mcap44, mcap45)
			floatCapYi := math.Min(mcap44, mcap45)
			if totalCapYi <= 0 {
				continue
			}
			if floatCapYi <= 0 {
				floatCapYi = totalCapYi
			}

			m := meta[symbol]
			name := fields[1]
			if strings.TrimSpace(name) == "" {
				name = m.Name
			}
			mainNetProxy := math.NaN()
			mainRatioProxy := math.NaN()
			mainFlowSource := ""
			if score, ok := pkScore[symbol]; ok {
				mainRatioProxy = score
				// 映射到近似“主力净流入额”代理量，便于前端展示非空
				mainNetProxy = (score / 100.0) * amountWan * 10000
				mainFlowSource = "tencent-pk-proxy"
			}

			items = append(items, ScanSnapshotRow{
				Symbol:             symbol,
				Name:               name,
				Price:              price,
				ChangePercent:      changePct,
				Amount:             amountWan * 10000, // 万元 -> 元
				TurnoverRate:       turnover,
				TotalMarketCap:     totalCapYi * 1e8, // 亿 -> 元
				FloatMarketCap:     floatCapYi * 1e8, // 亿 -> 元
				Industry:           m.Industry,
				IsST:               isSTName(name),
				MainNetInflow:      mainNetProxy,
				MainNetInflowRatio: mainRatioProxy,
				MainFlowSource:     mainFlowSource,
				UpdateTime:         nowText,
			})
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("腾讯快照回退失败：返回为空")
	}
	return items, nil
}

func buildTencentPKSymbols(symbols []string) string {
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		if len(sym) < 3 {
			continue
		}
		out = append(out, "s_pk"+sym)
	}
	return strings.Join(out, ",")
}

func parseTencentPKScoreMap(text string) map[string]float64 {
	result := make(map[string]float64)
	lines := strings.Split(text, ";")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "v_s_pk") || !strings.Contains(line, "=\"") {
			continue
		}
		lhsRhs := strings.SplitN(line, "=\"", 2)
		if len(lhsRhs) != 2 {
			continue
		}
		key := strings.TrimPrefix(lhsRhs[0], "v_s_pk")
		payload := strings.TrimSuffix(lhsRhs[1], "\"")
		if payload == "" {
			continue
		}
		parts := strings.Split(payload, "~")
		if len(parts) < 4 {
			continue
		}
		// s_pk 返回四项，分别对应不同周期买卖盘强弱，这里取短中期均值作为主力代理
		v1 := parseFloat64Safe(parts[0])
		v2 := parseFloat64Safe(parts[1])
		v3 := parseFloat64Safe(parts[2])
		v4 := parseFloat64Safe(parts[3])
		score := ((v1+v2+v3+v4)/4.0 - 0.25) * 100.0
		result[key] = score
	}
	return result
}

type qqTodayFundFlow struct {
	MainNetIn   float64
	MainInRate  float64
	MainOutRate float64
	MainInflow  float64
	MainOutflow float64
	HasValue    bool
}

// MainFlowHistoryPoint 主力历史净流入点（按交易日）
type MainFlowHistoryPoint struct {
	Date          string
	MainNetInflow float64
	Price         float64
}

func (ms *MarketService) enrichSnapshotMainFlowWithQQFund(items []ScanSnapshotRow) {
	if len(items) == 0 {
		return
	}
	workerCount := 6
	if workerCount > len(items) {
		workerCount = len(items)
	}
	indices := make(chan int, len(items))
	for i := range items {
		indices <- i
	}
	close(indices)

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for idx := range indices {
				row := &items[idx]
				// 已有东财主力数据时不覆盖
				if row.MainFlowSource == "eastmoney" && !math.IsNaN(row.MainNetInflow) && !math.IsNaN(row.MainNetInflowRatio) {
					continue
				}
				ff, err := ms.fetchQQTodayFundFlow(row.Symbol)
				if err != nil || !ff.HasValue {
					continue
				}
				row.MainNetInflow = ff.MainNetIn
				ratio := ff.MainInRate - ff.MainOutRate
				if math.Abs(ratio) < 0.0001 && row.FloatMarketCap > 0 {
					// 腾讯 summary 里的 mcRatio 对应主力净流入占流通市值比，转成百分比
					ratio = (ff.MainNetIn / row.FloatMarketCap) * 100
				}
				row.MainNetInflowRatio = ratio
				row.MainFlowSource = "tencent-fundflow"
			}
		}()
	}
	wg.Wait()
}

func (ms *MarketService) fetchQQTodayFundFlow(symbol string) (qqTodayFundFlow, error) {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) < 8 {
		return qqTodayFundFlow{}, fmt.Errorf("symbol invalid: %s", symbol)
	}
	if !strings.HasPrefix(code, "sh") && !strings.HasPrefix(code, "sz") {
		return qqTodayFundFlow{}, fmt.Errorf("qq fundflow unsupported market: %s", symbol)
	}
	params := url.Values{}
	params.Set("code", code)
	params.Set("type", "todayFundFlow")
	params.Set("klineNeedDay", "5")

	req, err := http.NewRequest("GET", qqFundFlowURL+"?"+params.Encode(), nil)
	if err != nil {
		return qqTodayFundFlow{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://gu.qq.com/")

	resp, err := ms.client.Do(req)
	if err != nil {
		return qqTodayFundFlow{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return qqTodayFundFlow{}, err
	}
	if isHTMLBody(body) {
		return qqTodayFundFlow{}, fmt.Errorf("qq fundflow returns html")
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return qqTodayFundFlow{}, err
	}
	if toInt64Any(raw["code"]) != 0 {
		return qqTodayFundFlow{}, fmt.Errorf("qq fundflow api code=%v msg=%v", raw["code"], raw["msg"])
	}
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		return qqTodayFundFlow{}, fmt.Errorf("qq fundflow empty data")
	}
	today, _ := data["todayFundFlow"].(map[string]any)
	if today == nil {
		return qqTodayFundFlow{}, nil
	}
	mainNet := parseFloat64Safe(toStringLocal(today["mainNetIn"]))
	mainInRate := parseFloat64Safe(toStringLocal(today["mainInRate"]))
	mainOutRate := parseFloat64Safe(toStringLocal(today["mainOutRate"]))
	mainInflow := parseFloat64Safe(toStringLocal(today["mainIn"]))
	mainOutflow := parseFloat64Safe(toStringLocal(today["mainOut"]))

	return qqTodayFundFlow{
		MainNetIn:   mainNet,
		MainInRate:  mainInRate,
		MainOutRate: mainOutRate,
		MainInflow:  mainInflow,
		MainOutflow: mainOutflow,
		HasValue:    !(mainNet == 0 && mainInflow == 0 && mainOutflow == 0 && mainInRate == 0 && mainOutRate == 0),
	}, nil
}

// GetQQMainFlowHistory 获取腾讯主力历史净流入（默认按日期升序）
func (ms *MarketService) GetQQMainFlowHistory(symbol string, days int) ([]MainFlowHistoryPoint, error) {
	return ms.fetchQQHistoryFundFlow(symbol, days)
}

func (ms *MarketService) fetchQQHistoryFundFlow(symbol string, days int) ([]MainFlowHistoryPoint, error) {
	code := strings.ToLower(strings.TrimSpace(symbol))
	if len(code) < 8 {
		return nil, fmt.Errorf("symbol invalid: %s", symbol)
	}
	if !strings.HasPrefix(code, "sh") && !strings.HasPrefix(code, "sz") {
		return nil, fmt.Errorf("qq fundflow unsupported market: %s", symbol)
	}
	if days <= 0 {
		days = 5
	}
	if days > 120 {
		days = 120
	}

	params := url.Values{}
	params.Set("code", code)
	params.Set("type", "historyFundFlow")
	params.Set("klineNeedDay", strconv.Itoa(days))

	req, err := http.NewRequest("GET", qqFundFlowURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://gu.qq.com/")

	resp, err := ms.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if isHTMLBody(body) {
		return nil, fmt.Errorf("qq history fundflow returns html")
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if toInt64Any(raw["code"]) != 0 {
		return nil, fmt.Errorf("qq history fundflow api code=%v msg=%v", raw["code"], raw["msg"])
	}
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		return nil, fmt.Errorf("qq history fundflow empty data")
	}
	history, _ := data["historyFundFlow"].(map[string]any)
	if history == nil {
		return nil, fmt.Errorf("qq history fundflow empty history")
	}

	rows := toMapSliceLocal(toSliceAnyLocal(history["oneDayKlineList"]))
	points := make([]MainFlowHistoryPoint, 0, len(rows))
	for _, row := range rows {
		date := strings.TrimSpace(toStringLocal(row["date"]))
		if date == "" {
			continue
		}
		points = append(points, MainFlowHistoryPoint{
			Date:          date,
			MainNetInflow: parseFloat64Safe(toStringLocal(row["mainNetIn"])),
			Price:         parseFloat64Safe(toStringLocal(row["price"])),
		})
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("qq history fundflow list empty")
	}

	sort.SliceStable(points, func(i, j int) bool {
		return points[i].Date < points[j].Date
	})
	if len(points) > days {
		points = points[len(points)-days:]
	}
	return points, nil
}

type embeddedStockMeta struct {
	Symbol   string
	Name     string
	Industry string
}

func loadEmbeddedStockCatalog(includeBeijing bool) []embeddedStockMeta {
	type stockBasicData struct {
		Data struct {
			Fields []string        `json:"fields"`
			Items  [][]interface{} `json:"items"`
		} `json:"data"`
	}
	var basic stockBasicData
	if err := json.Unmarshal(embed.StockBasicJSON, &basic); err != nil {
		return nil
	}

	symbolIdx, nameIdx, tsCodeIdx, industryIdx := -1, -1, -1, -1
	for i, field := range basic.Data.Fields {
		switch field {
		case "symbol":
			symbolIdx = i
		case "name":
			nameIdx = i
		case "ts_code":
			tsCodeIdx = i
		case "industry":
			industryIdx = i
		}
	}
	if symbolIdx < 0 || nameIdx < 0 {
		return nil
	}

	out := make([]embeddedStockMeta, 0, len(basic.Data.Items))
	for _, row := range basic.Data.Items {
		if symbolIdx >= len(row) || nameIdx >= len(row) {
			continue
		}
		code, _ := row[symbolIdx].(string)
		name, _ := row[nameIdx].(string)
		if code == "" {
			continue
		}

		prefix := "sz"
		if tsCodeIdx >= 0 && tsCodeIdx < len(row) {
			tsCode, _ := row[tsCodeIdx].(string)
			switch {
			case strings.HasSuffix(strings.ToUpper(tsCode), ".SH"):
				prefix = "sh"
			case strings.HasSuffix(strings.ToUpper(tsCode), ".BJ"):
				if !includeBeijing {
					continue
				}
				prefix = "bj"
			case strings.HasSuffix(strings.ToUpper(tsCode), ".SZ"):
				prefix = "sz"
			}
		} else {
			switch code[0] {
			case '6', '9', '5':
				prefix = "sh"
			case '8', '4':
				if !includeBeijing {
					continue
				}
				prefix = "bj"
			default:
				prefix = "sz"
			}
		}

		industry := ""
		if industryIdx >= 0 && industryIdx < len(row) {
			industry, _ = row[industryIdx].(string)
		}
		out = append(out, embeddedStockMeta{
			Symbol:   prefix + code,
			Name:     name,
			Industry: industry,
		})
	}
	return out
}

// BuildScanMarketSnapshot 构建扫描时的大盘快照
func (ms *MarketService) BuildScanMarketSnapshot() (ScanMarketSnapshot, error) {
	result := ScanMarketSnapshot{}

	// 上证当前点位优先走指数接口（更稳）
	if indices, idxErr := ms.GetMarketIndices(); idxErr == nil {
		for _, idx := range indices {
			if strings.EqualFold(idx.Code, "sh000001") {
				result.ShPrice = idx.Price
				break
			}
		}
	}

	// 上证 MA20：优先使用新浪日K（避免主源偶发异常值）
	if shDaily, err := ms.fetchKLineDataFromSina("sh000001", "1d", 40); err == nil {
		if ma20, closePrice, ok := extractIndexSnapshot(shDaily); ok {
			result.ShMA20 = ma20
			if !isValidIndexPoint(result.ShPrice) {
				result.ShPrice = closePrice
			}
		}
	}
	// 兜底：若仍不可用，再尝试统一 K 线接口
	if !isValidIndexPoint(result.ShPrice) || !isValidIndexPoint(result.ShMA20) {
		if shDaily, err := ms.GetKLineData("sh000001", "1d", 40); err == nil {
			if ma20, closePrice, ok := extractIndexSnapshot(shDaily); ok {
				result.ShMA20 = ma20
				if !isValidIndexPoint(result.ShPrice) {
					result.ShPrice = closePrice
				}
			}
		}
	}

	// 涨停/跌停计数 + 两市成交额（分页聚合，避免单次大包返回空）
	page := 1
	pageSize := 200
	maxPages := 30
	total := int64(0)
	processed := 0

	for page <= maxPages {
		params := url.Values{}
		params.Set("np", "1")
		params.Set("fltt", "2")
		params.Set("invt", "2")
		params.Set("fid", "f3")
		params.Set("po", "1")
		params.Set("pn", strconv.Itoa(page))
		params.Set("pz", strconv.Itoa(pageSize))
		params.Set("fs", "m:0 t:6,m:0 t:80,m:1 t:2,m:1 t:23")
		params.Set("fields", "f3,f6")
		params.Set("ut", "8dec03ba335b81bf4ebdf7b29ec27d15")

		raw, err := ms.fetchMarketJSON(emBoardFundFlowURL+"?"+params.Encode(), map[string]string{
			"Referer":    "https://quote.eastmoney.com/",
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		})
		if err != nil {
			return result, err
		}
		data, ok := raw["data"].(map[string]any)
		if !ok || data == nil {
			return result, fmt.Errorf("大盘快照响应缺少data")
		}
		if page == 1 {
			total = int64(toInt64Any(data["total"]))
		}

		rows := toMapSliceLocal(toSliceAnyLocal(data["diff"]))
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			pct := toFloat64Any(row["f3"])
			if pct >= 9.8 {
				result.LimitUpCount++
			}
			if pct <= -9.8 {
				result.LimitDownCount++
			}
			result.TotalAmount += toFloat64Any(row["f6"])
			processed++
		}

		if int64(processed) >= total || len(rows) < pageSize {
			break
		}
		page++
	}
	if processed == 0 {
		return result, fmt.Errorf("大盘快照返回空列表")
	}

	return result, nil
}

func isValidIndexPoint(v float64) bool {
	return v > 10 && v < 30000
}

func extractIndexSnapshot(klines []models.KLineData) (ma20 float64, closePrice float64, ok bool) {
	if len(klines) < 20 {
		return 0, 0, false
	}
	last := klines[len(klines)-1]
	closePrice = last.Close
	if !isValidIndexPoint(closePrice) {
		return 0, 0, false
	}
	sum := 0.0
	count := 0
	for i := len(klines) - 20; i < len(klines); i++ {
		if i < 0 || i >= len(klines) {
			continue
		}
		c := klines[i].Close
		if c <= 0 {
			return 0, 0, false
		}
		sum += c
		count++
	}
	if count < 20 {
		return 0, 0, false
	}
	ma20 = sum / float64(count)
	if !isValidIndexPoint(ma20) {
		return 0, 0, false
	}
	return ma20, closePrice, true
}

func (ms *MarketService) fetchMarketIndicesFromSina() ([]models.MarketIndex, error) {
	codeList := strings.Join(defaultIndexCodes, ",")
	url := fmt.Sprintf(sinaStockURL, time.Now().UnixNano(), codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://finance.sina.com.cn")

	resp, err := ms.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return ms.parseMarketIndices(string(body))
}

// parseMarketIndices 解析大盘指数数据
// 新浪简化指数数据格式: var hq_str_s_sh000001="上证指数,3094.668,-128.073,-3.97,436653,5458126"
// 字段: 名称,当前点位,涨跌点数,涨跌幅(%),成交量(手),成交额(万元)
func (ms *MarketService) parseMarketIndices(data string) ([]models.MarketIndex, error) {
	var indices []models.MarketIndex
	matches := sinaIndexRegex.FindAllStringSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 3 || match[2] == "" {
			continue
		}
		parts := strings.Split(match[2], ",")
		if len(parts) < 6 {
			continue
		}

		price, _ := strconv.ParseFloat(parts[1], 64)
		change, _ := strconv.ParseFloat(parts[2], 64)
		changePercent, _ := strconv.ParseFloat(parts[3], 64)
		volume, _ := strconv.ParseInt(parts[4], 10, 64)
		amount, _ := strconv.ParseFloat(parts[5], 64)

		indices = append(indices, models.MarketIndex{
			Code:          match[1],
			Name:          parts[0],
			Price:         price,
			Change:        change,
			ChangePercent: changePercent,
			Volume:        volume,
			Amount:        amount,
		})
	}
	return indices, nil
}

// GetBoardFundFlowList 获取板块资金流列表（行业/概念/地域）
func (ms *MarketService) GetBoardFundFlowList(category string, page int, size int) (models.BoardFundFlowList, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}

	normalizedCategory := normalizeBoardCategory(category)

	params := url.Values{}
	params.Set("np", "1")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("po", "1")
	params.Set("fid", "f62")
	params.Set("stat", "1")
	params.Set("fields", "f12,f14,f2,f3,f62,f184,f66,f69,f72,f75,f78,f81,f84,f87,f124")
	params.Set("ut", "8dec03ba335b81bf4ebdf7b29ec27d15")
	params.Set("pn", strconv.Itoa(page))
	params.Set("pz", strconv.Itoa(size))
	params.Set("fs", boardFundFlowFS(normalizedCategory))

	raw, err := ms.fetchMarketJSON(emBoardFundFlowURL+"?"+params.Encode(), map[string]string{
		"Referer":    "https://data.eastmoney.com/",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	})
	if err != nil {
		return models.BoardFundFlowList{}, err
	}

	data, ok := raw["data"].(map[string]any)
	if !ok || data == nil {
		return models.BoardFundFlowList{}, fmt.Errorf("板块资金流响应缺少data")
	}

	diffRows := toMapSliceLocal(toSliceAnyLocal(data["diff"]))
	items := make([]models.BoardFundFlowItem, 0, len(diffRows))
	var updateTime string
	for _, row := range diffRows {
		item := models.BoardFundFlowItem{
			Code:                 strings.TrimSpace(toStringLocal(row["f12"])),
			Name:                 strings.TrimSpace(toStringLocal(row["f14"])),
			Price:                toFloat64Any(row["f2"]),
			ChangePercent:        toFloat64Any(row["f3"]),
			MainNetInflow:        toFloat64Any(row["f62"]),
			MainNetInflowRatio:   toFloat64Any(row["f184"]),
			SuperNetInflow:       toFloat64Any(row["f66"]),
			SuperNetInflowRatio:  toFloat64Any(row["f69"]),
			LargeNetInflow:       toFloat64Any(row["f72"]),
			LargeNetInflowRatio:  toFloat64Any(row["f75"]),
			MediumNetInflow:      toFloat64Any(row["f78"]),
			MediumNetInflowRatio: toFloat64Any(row["f81"]),
			SmallNetInflow:       toFloat64Any(row["f84"]),
			SmallNetInflowRatio:  toFloat64Any(row["f87"]),
		}
		if ts := toInt64Any(row["f124"]); ts > 0 {
			item.UpdateTime = formatEastmoneyTimestamp(ts)
			if updateTime == "" {
				updateTime = item.UpdateTime
			}
		}
		items = append(items, item)
	}

	return models.BoardFundFlowList{
		Category:   normalizedCategory,
		Items:      items,
		Total:      toInt64Any(data["total"]),
		UpdateTime: updateTime,
	}, nil
}

// GetStockMovesList 获取盘口异动列表
func (ms *MarketService) GetStockMovesList(moveType string, page int, size int) (models.StockMoveList, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 30
	}
	if size > 200 {
		size = 200
	}

	normalizedMoveType := normalizeStockMoveType(moveType)
	fid, po := stockMoveSort(normalizedMoveType)

	params := url.Values{}
	params.Set("np", "1")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("fid", fid)
	params.Set("po", po)
	params.Set("pn", strconv.Itoa(page))
	params.Set("pz", strconv.Itoa(size))
	params.Set("fs", "m:0 t:6,m:0 t:80,m:1 t:2,m:1 t:23")
	params.Set("fields", "f12,f14,f2,f3,f22,f8,f5,f6,f62,f184,f15,f16,f17,f18,f124")
	params.Set("ut", "8dec03ba335b81bf4ebdf7b29ec27d15")

	raw, err := ms.fetchMarketJSON(emBoardFundFlowURL+"?"+params.Encode(), map[string]string{
		"Referer":         "https://quote.eastmoney.com/",
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Accept":          "*/*",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Connection":      "keep-alive",
	})
	if err != nil {
		return models.StockMoveList{}, err
	}

	data, ok := raw["data"].(map[string]any)
	if !ok || data == nil {
		return models.StockMoveList{}, fmt.Errorf("盘口异动响应缺少data")
	}

	diffRows := toMapSliceLocal(toSliceAnyLocal(data["diff"]))
	items := make([]models.StockMoveItem, 0, len(diffRows))
	var updateTime string
	for idx, row := range diffRows {
		item := models.StockMoveItem{
			Rank:               (page-1)*size + idx + 1,
			Code:               strings.TrimSpace(toStringLocal(row["f12"])),
			Name:               strings.TrimSpace(toStringLocal(row["f14"])),
			Price:              toFloat64Any(row["f2"]),
			ChangePercent:      toFloat64Any(row["f3"]),
			Speed:              toFloat64Any(row["f22"]),
			TurnoverRate:       toFloat64Any(row["f8"]),
			Volume:             toInt64Any(row["f5"]),
			Amount:             toFloat64Any(row["f6"]),
			MainNetInflow:      toFloat64Any(row["f62"]),
			MainNetInflowRatio: toFloat64Any(row["f184"]),
			High:               toFloat64Any(row["f15"]),
			Low:                toFloat64Any(row["f16"]),
			Open:               toFloat64Any(row["f17"]),
			PreClose:           toFloat64Any(row["f18"]),
		}
		if ts := toInt64Any(row["f124"]); ts > 0 {
			item.UpdateTime = formatEastmoneyTimestamp(ts)
			if updateTime == "" {
				updateTime = item.UpdateTime
			}
		}
		items = append(items, item)
	}

	return models.StockMoveList{
		MoveType:   normalizedMoveType,
		Items:      items,
		Total:      toInt64Any(data["total"]),
		UpdateTime: updateTime,
	}, nil
}

// GetBoardLeaders 获取板块龙头候选
func (ms *MarketService) GetBoardLeaders(boardCode string, limit int) (models.BoardLeaderList, error) {
	normalizedBoard := normalizeBoardCode(boardCode)
	if normalizedBoard == "" {
		return models.BoardLeaderList{}, fmt.Errorf("无效板块代码: %s", boardCode)
	}
	if limit <= 0 {
		limit = 6
	}
	if limit > 30 {
		limit = 30
	}

	fetchSize := limit * 6
	if fetchSize < 30 {
		fetchSize = 30
	}
	if fetchSize > 200 {
		fetchSize = 200
	}

	params := url.Values{}
	params.Set("np", "1")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("po", "1")
	params.Set("fid", "f3")
	params.Set("fields", "f12,f14,f2,f3,f8,f62,f184,f124")
	params.Set("ut", "8dec03ba335b81bf4ebdf7b29ec27d15")
	params.Set("pn", "1")
	params.Set("pz", strconv.Itoa(fetchSize))
	params.Set("fs", "b:"+normalizedBoard)

	raw, err := ms.fetchMarketJSON(emBoardFundFlowURL+"?"+params.Encode(), map[string]string{
		"Referer":    "https://data.eastmoney.com/",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	})
	if err != nil {
		return models.BoardLeaderList{}, err
	}

	data, ok := raw["data"].(map[string]any)
	if !ok || data == nil {
		return models.BoardLeaderList{}, fmt.Errorf("板块龙头响应缺少data")
	}

	diffRows := toMapSliceLocal(toSliceAnyLocal(data["diff"]))
	items := make([]models.BoardLeaderItem, 0, len(diffRows))
	var updateTime string
	for _, row := range diffRows {
		item := models.BoardLeaderItem{
			Code:               strings.TrimSpace(toStringLocal(row["f12"])),
			Name:               strings.TrimSpace(toStringLocal(row["f14"])),
			Price:              toFloat64Any(row["f2"]),
			ChangePercent:      toFloat64Any(row["f3"]),
			TurnoverRate:       toFloat64Any(row["f8"]),
			MainNetInflow:      toFloat64Any(row["f62"]),
			MainNetInflowRatio: toFloat64Any(row["f184"]),
		}
		item.Score = calculateBoardLeaderScore(item.ChangePercent, item.MainNetInflow, item.MainNetInflowRatio)
		if ts := toInt64Any(row["f124"]); ts > 0 {
			item.UpdateTime = formatEastmoneyTimestamp(ts)
			if updateTime == "" {
				updateTime = item.UpdateTime
			}
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			if items[i].ChangePercent == items[j].ChangePercent {
				return items[i].MainNetInflow > items[j].MainNetInflow
			}
			return items[i].ChangePercent > items[j].ChangePercent
		}
		return items[i].Score > items[j].Score
	})

	if len(items) > limit {
		items = items[:limit]
	}
	for i := range items {
		items[i].Rank = i + 1
	}

	return models.BoardLeaderList{
		BoardCode:  normalizedBoard,
		Items:      items,
		UpdateTime: updateTime,
	}, nil
}

// GetIndexFundFlowSeries 获取指数资金流曲线
func (ms *MarketService) GetIndexFundFlowSeries(code string, interval string, limit int) (models.FundFlowKLineSeries, error) {
	if strings.TrimSpace(code) == "" {
		return models.FundFlowKLineSeries{}, fmt.Errorf("未提供指数代码")
	}
	if limit < 0 {
		limit = 0
	}

	params := url.Values{}
	params.Set("lmt", "0")
	params.Set("klt", normalizeFundFlowInterval(interval))
	params.Set("fields1", "f1,f2,f3,f7")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61,f62,f63,f64,f65")
	params.Set("ut", "b2884a393a59ad64002292a3e90d46a5")
	params.Set("secid", indexSecID(code))

	raw, err := ms.fetchMarketJSON(emFundFlowKLineURL+"?"+params.Encode(), map[string]string{
		"Referer":    "https://quote.eastmoney.com/",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	})
	if err != nil {
		return models.FundFlowKLineSeries{}, err
	}

	data, ok := raw["data"].(map[string]any)
	if !ok || data == nil {
		return models.FundFlowKLineSeries{}, fmt.Errorf("资金流曲线响应缺少data")
	}

	series := models.FundFlowKLineSeries{
		Code:   toStringLocal(data["code"]),
		Name:   toStringLocal(data["name"]),
		Market: int(toInt64Any(data["market"])),
	}
	series.TradePeriods = parseTradePeriods(data["tradePeriods"])

	points := make([]models.FundFlowKLine, 0)
	for _, line := range toStringSlice(data["klines"]) {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		points = append(points, models.FundFlowKLine{
			Time:            strings.TrimSpace(parts[0]),
			MainNetInflow:   parseFloat64Safe(parts[1]),
			SuperNetInflow:  parseFloat64Safe(parts[2]),
			LargeNetInflow:  parseFloat64Safe(parts[3]),
			MediumNetInflow: parseFloat64Safe(parts[4]),
			SmallNetInflow:  parseFloat64Safe(parts[5]),
		})
	}
	if limit > 0 && len(points) > limit {
		points = points[len(points)-limit:]
	}
	series.KLines = points
	return series, nil
}

// GetStockAnnouncements 获取个股公告摘要
func (ms *MarketService) GetStockAnnouncements(code string, page int, size int) (models.StockAnnouncements, error) {
	normalized := normalizeStockListCode(code)
	if normalized == "" {
		return models.StockAnnouncements{}, fmt.Errorf("未提供股票代码")
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}

	params := url.Values{}
	params.Set("ann_type", "A")
	params.Set("client_source", "web")
	params.Set("stock_list", normalized)
	params.Set("page_index", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(size))

	raw, err := ms.fetchMarketJSON(emAnnouncementURL+"?"+params.Encode(), map[string]string{
		"Referer":    "https://notice.eastmoney.com/",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	})
	if err != nil {
		return models.StockAnnouncements{}, err
	}

	data, ok := raw["data"].(map[string]any)
	if !ok || data == nil {
		return models.StockAnnouncements{}, fmt.Errorf("公告响应缺少data")
	}

	rows := toMapSliceLocal(toSliceAnyLocal(data["list"]))
	items := make([]models.StockAnnouncement, 0, len(rows))
	for _, row := range rows {
		items = append(items, models.StockAnnouncement{
			Title:      strings.TrimSpace(toStringLocal(row["title"])),
			NoticeDate: strings.TrimSpace(toStringLocal(row["notice_date"])),
			Type:       strings.TrimSpace(toStringLocal(row["ann_type"])),
			Columns:    strings.TrimSpace(toStringLocal(row["columns"])),
			ArtCode:    strings.TrimSpace(toStringLocal(row["art_code"])),
		})
	}

	return models.StockAnnouncements{
		Code:  normalized,
		Items: items,
		Total: toInt64Any(data["total"]),
	}, nil
}

func normalizeBoardCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "industry", "hy", "行业":
		return "industry"
	case "concept", "gn", "概念", "题材":
		return "concept"
	case "region", "dy", "地区", "地域":
		return "region"
	default:
		return "industry"
	}
}

func normalizeStockMoveType(moveType string) string {
	switch strings.ToLower(strings.TrimSpace(moveType)) {
	case "surge", "speed_up", "speedup", "rapid_up", "up":
		return "surge"
	case "drop", "speed_down", "speeddown", "rapid_down", "down":
		return "drop"
	case "change_up", "rise", "up_change":
		return "change_up"
	case "change_down", "fall", "down_change":
		return "change_down"
	case "mainflow", "fund", "capital":
		return "mainflow"
	case "turnover", "activity", "active":
		return "turnover"
	default:
		return "surge"
	}
}

func stockMoveSort(moveType string) (string, string) {
	switch moveType {
	case "drop":
		return "f22", "0"
	case "change_up":
		return "f3", "1"
	case "change_down":
		return "f3", "0"
	case "mainflow":
		return "f62", "1"
	case "turnover":
		return "f8", "1"
	default:
		return "f22", "1"
	}
}

func normalizeBoardCode(boardCode string) string {
	candidate := strings.ToUpper(strings.TrimSpace(boardCode))
	candidate = strings.TrimPrefix(candidate, "B:")
	if strings.HasPrefix(candidate, "BI") && len(candidate) == 6 {
		candidate = "BK" + candidate[2:]
	}
	if strings.HasPrefix(candidate, "BK") && len(candidate) == 6 {
		return candidate
	}
	if len(candidate) == 4 && isDigits(candidate) {
		return "BK" + candidate
	}
	return ""
}

func formatSymbolByMarket(code string, marketID int64) string {
	if !isDigits(code) {
		return ""
	}
	switch marketID {
	case 1:
		return "sh" + code
	case 0:
		return "sz" + code
	case 2:
		return "bj" + code
	default:
		// 兜底按首位推断
		switch code[0] {
		case '6', '9', '5':
			return "sh" + code
		case '8', '4':
			return "bj" + code
		default:
			return "sz" + code
		}
	}
}

func isSTName(name string) bool {
	n := strings.ToUpper(strings.TrimSpace(name))
	return strings.Contains(n, "ST") || strings.Contains(n, "*ST")
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func calculateBoardLeaderScore(changePercent, mainNetInflow, mainNetInflowRatio float64) float64 {
	flowScore := 0.0
	if mainNetInflow != 0 {
		flowScore = math.Log10(math.Abs(mainNetInflow)/1e6 + 1)
		if mainNetInflow < 0 {
			flowScore = -flowScore
		}
	}
	score := changePercent*1.8 + mainNetInflowRatio*0.8 + flowScore*3.0
	return math.Round(score*100) / 100
}

func boardFundFlowFS(category string) string {
	switch category {
	case "concept":
		return "m:90 t:3"
	case "region":
		return "m:90 t:1"
	default:
		return "m:90 s:4"
	}
}

func normalizeFundFlowInterval(interval string) string {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1", "1m", "1min", "min":
		return "1"
	case "5", "5m":
		return "5"
	case "15", "15m":
		return "15"
	case "30", "30m":
		return "30"
	case "60", "60m":
		return "60"
	case "101", "1d", "day":
		return "101"
	default:
		return "1"
	}
}

func normalizeStockListCode(code string) string {
	lower := normalizeMarketCode(code)
	if strings.HasPrefix(lower, "sh") || strings.HasPrefix(lower, "sz") || strings.HasPrefix(lower, "bj") {
		return lower[2:]
	}
	return lower
}

func normalizeMarketCode(code string) string {
	lower := strings.ToLower(strings.TrimSpace(code))
	if lower == "" {
		return ""
	}
	if strings.HasPrefix(lower, "sh") || strings.HasPrefix(lower, "sz") || strings.HasPrefix(lower, "bj") {
		return lower
	}
	if len(lower) == 6 && isDigits(lower) {
		switch lower[0] {
		case '6':
			return "sh" + lower
		case '0', '3':
			return "sz" + lower
		case '4', '8':
			return "bj" + lower
		}
	}
	return lower
}

func indexSecID(code string) string {
	normalized := normalizeMarketCode(code)
	switch normalized {
	case "sh000001":
		return "1.000001"
	case "sz399001":
		return "0.399001"
	case "sz399006":
		return "0.399006"
	default:
		if strings.HasPrefix(normalized, "sh") {
			return "1." + normalized[2:]
		}
		return "0." + strings.TrimPrefix(normalized, normalized[:2])
	}
}

func formatEastmoneyTimestamp(ts int64) string {
	if ts <= 0 {
		return ""
	}
	text := strconv.FormatInt(ts, 10)
	cst := time.FixedZone("CST", 8*60*60)
	switch len(text) {
	case 10:
		return time.Unix(ts, 0).In(cst).Format("2006-01-02 15:04:05")
	case 13:
		return time.UnixMilli(ts).In(cst).Format("2006-01-02 15:04:05")
	case 12:
		if t, err := time.Parse("200601021504", text); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	case 14:
		if t, err := time.Parse("20060102150405", text); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	case 8:
		if t, err := time.Parse("20060102", text); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return text
}

func parseTradePeriods(value any) models.TradePeriods {
	raw, ok := value.(map[string]any)
	if !ok || raw == nil {
		return models.TradePeriods{}
	}

	result := models.TradePeriods{
		Pre:   parseTradePeriod(raw["pre"]),
		After: parseTradePeriod(raw["after"]),
	}
	for _, item := range toSliceAnyLocal(raw["periods"]) {
		if period := parseTradePeriod(item); period != nil {
			result.Periods = append(result.Periods, *period)
		}
	}
	return result
}

func parseTradePeriod(value any) *models.TradePeriod {
	raw, ok := value.(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	begin := toInt64Any(raw["b"])
	end := toInt64Any(raw["e"])
	if begin == 0 && end == 0 {
		return nil
	}
	return &models.TradePeriod{Begin: begin, End: end}
}

func toSliceAnyLocal(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func toMapSliceLocal(items []any) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch row := item.(type) {
		case map[string]any:
			result = append(result, row)
		case []any:
			for _, nested := range row {
				if nestedRow, ok := nested.(map[string]any); ok {
					result = append(result, nestedRow)
				}
			}
		}
	}
	return result
}

func toStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func parseFloat64Safe(value string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return f
}

func toStringLocal(value any) string {
	if value == nil {
		return ""
	}
	if v, ok := value.(string); ok {
		return v
	}
	return fmt.Sprintf("%v", value)
}

func toFloat64Any(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		return parseFloat64Safe(v)
	default:
		return parseFloat64Safe(toStringLocal(value))
	}
}

func toInt64Any(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i
	default:
		i, _ := strconv.ParseInt(strings.TrimSpace(toStringLocal(value)), 10, 64)
		return i
	}
}

func (ms *MarketService) fetchMarketJSON(urlStr string, headers map[string]string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := ms.fetchMarketJSONOnce(urlStr, headers)
		if err == nil {
			return result, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
	}
	return nil, lastErr
}

func (ms *MarketService) fetchMarketJSONOnce(urlStr string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := ms.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if isHTMLBody(body) {
		return nil, fmt.Errorf("上游返回HTML响应，可能被拦截或接口变更")
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}
