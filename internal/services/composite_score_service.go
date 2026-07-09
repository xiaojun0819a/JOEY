package services

// 综合评分选股服务:
//   第〇层 一票否决 —— 基本面初筛已排除 ST/负债>70%/商誉>30%,本层再加 年线乖离>100%、流动性不足
//   第一层 质量分 0-50 —— 直接复用基本面扫描打分(封顶50)
//   第二层 结构分 0-30 —— 诚实面板事实:均线多头/位250区间/破前高未破前低/缩量回调
//   第三层 催化分 0-20 —— 60日涨停(回封打折)/近10日龙虎榜净买/最新竞价金额前200
//   状态门(不进分数) —— 当日破确认前低或破季线未修复 → 禁止执行
// 每次评分按交易日落快照,前向 30/60 交易日验证 Top10 等权扣费超额(对比全市场等权基准)。
// 权重只允许被验证结果做减法(删维度),不允许加法调参。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// CompositeKlineFn 由 App 注入,复用档案+实时衔接的日K管线
type CompositeKlineFn func(code string, period string, days int) []models.KLineData

type CompositeScoreService struct {
	history  *HistoryService
	intraday *IntradayService
	lhb      *LongHuBangService
	klineFn  CompositeKlineFn

	mu          sync.Mutex
	lhbSet      map[string]bool // 近10个交易日龙虎榜净买>0 的代码(纯数字)
	lhbCachedAt time.Time
}

func NewCompositeScoreService(h *HistoryService, i *IntradayService, l *LongHuBangService, k CompositeKlineFn) *CompositeScoreService {
	s := &CompositeScoreService{history: h, intraday: i, lhb: l, klineFn: k}
	s.initSchema()
	return s
}

// StartAutoRun 每周五收盘后(16:30)自动评分落快照,并把 Top10 中过状态门的等权开进模拟盘。
// 快照存的是纯 Top10(回算口径);模拟盘只开 GateOK 的(执行口径)——两者的差值本身就是"状态门是否有价值"的检验。
// 每只等权 ~1万元,来源标"综合评分"(套用 riskComposite 风控:价值宽线+结构止损),已持有的不重复开。
func (s *CompositeScoreService) StartAutoRun(paper *PaperService) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		lastDay := ""
		for range ticker.C {
			now := time.Now()
			today := now.Format("2006-01-02")
			if now.Weekday() != time.Friday || now.Hour() < 16 || (now.Hour() == 16 && now.Minute() < 30) || lastDay == today {
				continue
			}
			lastDay = today
			// 只在交易日跑:当日收盘采集(16:00)完成后,库里最新交易日应=今天
			dates, err := s.history.RecentTradeDates(1)
			if err != nil || len(dates) == 0 || dates[0] != today {
				log.Info("综合评分周任务:今日(%s)非交易日或数据未就绪,跳过", today)
				continue
			}
			res := s.Run("value")
			log.Info("综合评分周任务:%s 入榜%d 否决%d 快照=%v", res.RunDate, len(res.Rows), len(res.VetoedRows), res.SnapshotSaved)
			if paper == nil || len(res.Rows) == 0 {
				continue
			}
			held := map[string]bool{}
			for _, p := range paper.OpenPositions() {
				if p.Source == "综合评分" {
					held[strings.ToLower(p.Symbol)] = true
				}
			}
			opened := 0
			for i, r := range res.Rows {
				if i >= 10 {
					break
				}
				if !r.GateOK || r.Price <= 0 || held[strings.ToLower(r.Symbol)] {
					continue
				}
				shares := int64(10000/r.Price/100) * 100
				if shares < 100 {
					shares = 100
				}
				if _, err := paper.Add(r.Symbol, r.Name, "综合评分", r.Price, shares); err != nil {
					log.Warn("综合评分自动开仓失败 %s: %v", r.Symbol, err)
				} else {
					opened++
				}
			}
			log.Info("综合评分周任务:Top10 过门自动开仓 %d 笔(等权约1万/只)", opened)
		}
	}()
}

func (s *CompositeScoreService) initSchema() {
	if s.history == nil || s.history.db == nil {
		return
	}
	// 龙虎榜日缓存:评分从库读,缺哪天补哪天,不再评分现场打外部接口
	if _, err := s.history.db.Exec(`CREATE TABLE IF NOT EXISTS lhb_daily (
		trade_date TEXT NOT NULL,
		code TEXT NOT NULL,
		net_buy REAL,
		PRIMARY KEY (trade_date, code)
	)`); err != nil {
		log.Warn("lhb_daily 表初始化失败: %v", err)
	}
	_, err := s.history.db.Exec(`CREATE TABLE IF NOT EXISTS composite_score_snapshots (
		run_date TEXT NOT NULL,
		symbol TEXT NOT NULL,
		name TEXT,
		rank INTEGER,
		total REAL,
		quality REAL,
		structure REAL,
		catalyst REAL,
		price REAL,
		gate_ok INTEGER,
		preset TEXT,
		created_at TEXT,
		PRIMARY KEY (run_date, symbol)
	)`)
	if err != nil {
		log.Warn("综合评分快照表初始化失败: %v", err)
	}
}

func digitsOf(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// limitRatioOf 涨停幅度(基本面初筛已排除 ST 与北交所)
func limitRatioOf(code string) float64 {
	d := digitsOf(code)
	if strings.HasPrefix(d, "68") || strings.HasPrefix(d, "30") {
		return 0.2
	}
	return 0.1
}

func ma(vals []float64, n, i int) float64 {
	if n <= 0 || i+1 < n {
		return math.NaN()
	}
	sum := 0.0
	for j := i - n + 1; j <= i; j++ {
		sum += vals[j]
	}
	return sum / float64(n)
}

// structFacts 结构与催化的K线事实(k 按时间升序)
type klineFacts struct {
	ok            bool
	bull          bool    // C>MA20>MA60
	pos250        float64 // 现价在近250日高低区间百分位
	dev250        float64 // 年线乖离 %
	has250        bool
	breakHigh30   bool // 30日内收盘上穿确认前高
	breakLow30    bool // 30日内收盘下穿确认前低
	shrinkPull    bool // 缩量回调:量5<量20 且 距MA20<8%
	belowPivotLow bool // 当日收盘低于确认前低(状态门)
	belowMA60     bool // 当日收盘低于季线(状态门)
	boardIn60     bool // 60日内涨停
	boardSolid    bool // 至少一个非回封板
	avgAmt20      float64
}

func evalKlineFacts(code string, k []models.KLineData) klineFacts {
	f := klineFacts{}
	n := len(k)
	if n < 60 {
		return f
	}
	f.ok = true
	C := make([]float64, n)
	H := make([]float64, n)
	L := make([]float64, n)
	V := make([]float64, n)
	for i, b := range k {
		C[i], H[i], L[i], V[i] = b.Close, b.High, b.Low, float64(b.Volume)
	}
	i := n - 1
	ma20, ma60 := ma(C, 20, i), ma(C, 60, i)
	f.bull = C[i] > ma20 && ma20 > ma60
	f.belowMA60 = !math.IsNaN(ma60) && C[i] < ma60
	// 位250 与年线乖离
	win := 250
	if n < win {
		win = n
	}
	hh, ll := -math.MaxFloat64, math.MaxFloat64
	for j := n - win; j < n; j++ {
		if H[j] > hh {
			hh = H[j]
		}
		if L[j] < ll {
			ll = L[j]
		}
	}
	if hh > ll {
		f.pos250 = (C[i] - ll) / (hh - ll) * 100
	}
	if n >= 250 {
		f.has250 = true
		f.dev250 = (C[i]/ma(C, 250, i) - 1) * 100
	}
	// 确认前高/前低:5日枢轴,滞后5日生效(与诚实面板同口径,不回改)
	const M = 5
	confHigh := make([]float64, n)
	confLow := make([]float64, n)
	curH, curL := math.NaN(), math.NaN()
	for t := 0; t < n; t++ {
		p := t - M // 候选枢轴位
		if p >= M {
			isH, isL := true, true
			for j := p - M; j <= p+M && j < n; j++ {
				if H[j] > H[p] {
					isH = false
				}
				if L[j] < L[p] {
					isL = false
				}
			}
			if isH {
				curH = H[p]
			}
			if isL {
				curL = L[p]
			}
		}
		confHigh[t], confLow[t] = curH, curL
	}
	from := n - 30
	if from < 1 {
		from = 1
	}
	for t := from; t < n; t++ {
		if !math.IsNaN(confHigh[t]) && C[t] > confHigh[t] && C[t-1] <= confHigh[t-1] {
			f.breakHigh30 = true
		}
		if !math.IsNaN(confLow[t]) && C[t] < confLow[t] && C[t-1] >= confLow[t-1] {
			f.breakLow30 = true
		}
	}
	f.belowPivotLow = !math.IsNaN(confLow[i]) && C[i] < confLow[i]
	// 缩量回调
	vma5, vma20 := ma(V, 5, i), ma(V, 20, i)
	f.shrinkPull = vma5 < vma20 && !math.IsNaN(ma20) && math.Abs(C[i]/ma20-1) < 0.08
	// 60日涨停(按板块幅度;回封=盘中曾低于涨停价3%)
	r := limitRatioOf(code)
	from60 := n - 60
	if from60 < 1 {
		from60 = 1
	}
	for t := from60; t < n; t++ {
		zt := math.Round(C[t-1]*(1+r)*100) / 100
		if C[t] >= zt-0.01 && C[t] >= H[t]-0.001 {
			f.boardIn60 = true
			if L[t] >= zt*0.97 {
				f.boardSolid = true
			}
		}
	}
	// 近20日均成交额
	from20 := n - 20
	if from20 < 0 {
		from20 = 0
	}
	sum, cnt := 0.0, 0
	for t := from20; t < n; t++ {
		amt := k[t].Amount
		if amt <= 0 {
			amt = C[t] * V[t] // 部分数据源量单位为手,此处只做下限校验,宁可放行
		}
		sum += amt
		cnt++
	}
	if cnt > 0 {
		f.avgAmt20 = sum / float64(cnt)
	}
	return f
}

// lhbNetBuySet 近10个交易日龙虎榜净买>0 的代码集合。
// 库优先(lhb_daily),缺的交易日现场补拉并落库——首跑后每天只新增一天,评分不再被外部接口拖慢。
func (s *CompositeScoreService) lhbNetBuySet() (map[string]bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lhbSet != nil && time.Since(s.lhbCachedAt) < time.Hour {
		return s.lhbSet, ""
	}
	set := map[string]bool{}
	dates, err := s.history.RecentTradeDates(10)
	if err != nil || len(dates) == 0 {
		return set, "龙虎榜:无交易日数据"
	}
	db := s.history.db
	fetchFail := 0
	for _, d := range dates {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM lhb_daily WHERE trade_date=?`, d).Scan(&n)
		if n > 0 {
			continue // 已入库
		}
		res, err := s.lhb.GetLongHuBangList(500, 1, d)
		if err != nil || res == nil || len(res.Items) == 0 {
			fetchFail++ // 当日榜未出或接口失败:不落标记,下次再补
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			continue
		}
		for _, it := range res.Items {
			_, _ = tx.Exec(`INSERT OR REPLACE INTO lhb_daily (trade_date, code, net_buy) VALUES (?,?,?)`,
				d, digitsOf(it.Code), it.NetBuyAmt)
		}
		_ = tx.Commit()
	}
	rows, err := db.Query(`SELECT DISTINCT code FROM lhb_daily WHERE trade_date>=? AND trade_date<=? AND net_buy>0`,
		dates[0], dates[len(dates)-1])
	if err == nil {
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				set[c] = true
			}
		}
		rows.Close()
	}
	warn := ""
	if len(set) == 0 && fetchFail == len(dates) {
		warn = "龙虎榜数据不可用,催化分该项计0"
	} else {
		s.lhbSet = set
		s.lhbCachedAt = time.Now()
	}
	return set, warn
}

// auctionTopSet 最近有数据的一天,竞价金额前200的代码集合
func (s *CompositeScoreService) auctionTopSet() (map[string]bool, string) {
	set := map[string]bool{}
	if s.intraday == nil {
		return set, "竞价采集未启用,催化分该项计0"
	}
	dates, err := s.history.RecentTradeDates(3)
	if err != nil {
		return set, "竞价:无交易日数据"
	}
	for _, d := range dates {
		rows, err := s.intraday.AuctionFinal(d, 200)
		if err == nil && len(rows) > 0 {
			for _, r := range rows {
				set[digitsOf(r.StockCode)] = true
			}
			return set, ""
		}
	}
	return set, "近3日无竞价采集数据,催化分该项计0"
}

const compositeRulesText = "第〇层否决:ST/负债>70%/商誉>30%(基本面初筛)+年线乖离>100%+20日均额<5000万。" +
	"质量0-50:基本面扫描分封顶50。结构0-30:多头排列+10/位250∈[30,75]+8/30日破前高未破前低+7/缩量回调+5。" +
	"催化0-20:60日涨停+6(全回封×0.7)/近10日龙虎榜净买+6/最新竞价额前200+8。" +
	"状态门:当日破确认前低或破季线→禁止执行,不扣分。评分只排序,不择时;权重只做验证减法。"

// Run 执行综合评分并落快照
func (s *CompositeScoreService) Run(preset string) models.CompositeScoreResult {
	res := models.CompositeScoreResult{Preset: preset, RulesText: compositeRulesText}
	if s == nil || s.history == nil {
		res.Warning = "历史服务未就绪"
		return res
	}
	if preset == "" {
		preset = "value"
		res.Preset = preset
	}
	fscan := s.history.RunFundamentalScan(preset)
	res.PresetLabel = fscan.PresetLabel
	res.UniverseCount = len(fscan.Candidates)
	if fscan.Warning != "" {
		res.Warning = fscan.Warning
	}
	if len(fscan.Candidates) == 0 {
		return res
	}
	dates, _ := s.history.RecentTradeDates(1)
	if len(dates) > 0 {
		res.RunDate = dates[0]
	} else {
		res.RunDate = time.Now().Format("2006-01-02")
	}
	lhbSet, lhbWarn := s.lhbNetBuySet()
	aucSet, aucWarn := s.auctionTopSet()
	for _, w := range []string{lhbWarn, aucWarn} {
		if w != "" {
			if res.Warning != "" {
				res.Warning += ";"
			}
			res.Warning += w
		}
	}

	// K线并发预取(行情源,8路)。不读 stock_daily:每日快照只有收盘价,高低开为空,
	// 降级填充会让涨停/枢轴/位250 全部失真(2026-07-09 实测)——口径优先于速度。
	klines := make(map[string][]models.KLineData, len(fscan.Candidates))
	if s.klineFn != nil {
		var kmu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, c := range fscan.Candidates {
			wg.Add(1)
			go func(sym string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				k := s.klineFn(sym, "1d", 300)
				kmu.Lock()
				klines[sym] = k
				kmu.Unlock()
			}(c.Symbol)
		}
		wg.Wait()
	}

	for _, c := range fscan.Candidates {
		row := models.CompositeScoreRow{
			Symbol: c.Symbol, Name: c.Name, Price: c.Price,
			AnnROE: c.AnnROE, MarketCapYi: c.MarketCapYi,
			QualityFacts: c.Passed,
		}
		row.Quality = math.Min(50, c.Score)
		f := evalKlineFacts(c.Symbol, klines[c.Symbol])
		if !f.ok {
			row.Vetoed = true
			row.VetoReasons = append(row.VetoReasons, "K线不足60根,无法评估结构")
			res.VetoedRows = append(res.VetoedRows, row)
			continue
		}
		// 一票否决
		if f.has250 && f.dev250 > 100 {
			row.VetoReasons = append(row.VetoReasons, fmt.Sprintf("年线乖离%.0f%%>100%%", f.dev250))
		}
		if f.avgAmt20 > 0 && f.avgAmt20 < 5000_0000 {
			row.VetoReasons = append(row.VetoReasons, fmt.Sprintf("20日均额%.0f万<5000万", f.avgAmt20/1e4))
		}
		if len(row.VetoReasons) > 0 {
			row.Vetoed = true
			res.VetoedRows = append(res.VetoedRows, row)
			continue
		}
		// 结构分
		if f.bull {
			row.Structure += 10
			row.StructFacts = append(row.StructFacts, "均线多头排列+10")
		}
		if f.pos250 >= 30 && f.pos250 <= 75 {
			row.Structure += 8
			row.StructFacts = append(row.StructFacts, fmt.Sprintf("位250=%.0f∈[30,75]+8", f.pos250))
		} else {
			row.StructFacts = append(row.StructFacts, fmt.Sprintf("位250=%.0f", f.pos250))
		}
		if f.breakHigh30 && !f.breakLow30 {
			row.Structure += 7
			row.StructFacts = append(row.StructFacts, "30日破前高未破前低+7")
		}
		if f.shrinkPull {
			row.Structure += 5
			row.StructFacts = append(row.StructFacts, "缩量回调+5")
		}
		// 催化分
		if f.boardIn60 {
			pts := 6.0
			label := "60日内涨停+6"
			if !f.boardSolid {
				pts = 4
				label = "60日内涨停(全回封)+4"
			}
			row.Catalyst += pts
			row.CatalystFacts = append(row.CatalystFacts, label)
		}
		d := digitsOf(c.Symbol)
		if lhbSet[d] {
			row.Catalyst += 6
			row.CatalystFacts = append(row.CatalystFacts, "近10日龙虎榜净买+6")
		}
		if aucSet[d] {
			row.Catalyst += 8
			row.CatalystFacts = append(row.CatalystFacts, "竞价金额前200+8")
		}
		// 状态门
		if f.belowPivotLow {
			row.GateReasons = append(row.GateReasons, "破确认前低未修复")
		}
		if f.belowMA60 {
			row.GateReasons = append(row.GateReasons, "破季线未修复")
		}
		row.GateOK = len(row.GateReasons) == 0
		row.Total = math.Round((row.Quality+row.Structure+row.Catalyst)*10) / 10
		res.Rows = append(res.Rows, row)
	}
	sort.Slice(res.Rows, func(i, j int) bool { return res.Rows[i].Total > res.Rows[j].Total })
	res.SnapshotSaved = s.saveSnapshot(res)
	return res
}

func (s *CompositeScoreService) saveSnapshot(res models.CompositeScoreResult) bool {
	if s.history == nil || s.history.db == nil || len(res.Rows) == 0 || res.RunDate == "" {
		return false
	}
	tx, err := s.history.db.Begin()
	if err != nil {
		log.Warn("综合评分快照事务失败: %v", err)
		return false
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	limit := len(res.Rows)
	if limit > 50 {
		limit = 50
	}
	for i := 0; i < limit; i++ {
		r := res.Rows[i]
		if _, err := tx.Exec(`INSERT OR REPLACE INTO composite_score_snapshots
			(run_date,symbol,name,rank,total,quality,structure,catalyst,price,gate_ok,preset,created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			res.RunDate, r.Symbol, r.Name, i+1, r.Total, r.Quality, r.Structure, r.Catalyst,
			r.Price, compositeBool(r.GateOK), res.Preset, now); err != nil {
			log.Warn("综合评分快照写入失败 %s: %v", r.Symbol, err)
			_ = tx.Rollback()
			return false
		}
	}
	if err := tx.Commit(); err != nil {
		log.Warn("综合评分快照提交失败: %v", err)
		return false
	}
	return true
}

func compositeBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Validate 前向验证:每个快照日的 Top10 等权,30/60 交易日后的扣费超额
func (s *CompositeScoreService) Validate() models.CompositeValidationResult {
	const costPct = 0.3 // 双边成本 0.3%
	res := models.CompositeValidationResult{CostNote: fmt.Sprintf("组合收益已扣双边成本%.1f%%;基准=全市场等权指数(自建库)", costPct)}
	if s == nil || s.history == nil || s.history.db == nil {
		res.Warning = "历史服务未就绪"
		return res
	}
	db := s.history.db
	runRows, err := db.Query(`SELECT DISTINCT run_date FROM composite_score_snapshots ORDER BY run_date`)
	if err != nil {
		res.Warning = err.Error()
		return res
	}
	var runDates []string
	for runRows.Next() {
		var d string
		if runRows.Scan(&d) == nil {
			runDates = append(runDates, d)
		}
	}
	runRows.Close()
	if len(runDates) == 0 {
		res.Warning = "还没有快照;每次运行综合评分会自动落一份"
		return res
	}
	// 全部交易日(从最早快照起)
	dateRows, err := db.Query(`SELECT DISTINCT trade_date FROM stock_daily WHERE trade_date>=? ORDER BY trade_date`, runDates[0])
	if err != nil {
		res.Warning = err.Error()
		return res
	}
	var allDates []string
	idx := map[string]int{}
	for dateRows.Next() {
		var d string
		if dateRows.Scan(&d) == nil {
			idx[d] = len(allDates)
			allDates = append(allDates, d)
		}
	}
	dateRows.Close()

	closeOn := func(symbol, date string) float64 {
		var v float64
		err := db.QueryRow(`SELECT close_price FROM stock_daily WHERE stock_code=? AND trade_date=?`, symbol, date).Scan(&v)
		if err != nil {
			return 0
		}
		return v
	}

	for _, rd := range runDates {
		start, ok := idx[rd]
		if !ok {
			continue
		}
		type snap struct {
			symbol string
			price  float64
		}
		var top []snap
		q, err := db.Query(`SELECT symbol, price FROM composite_score_snapshots WHERE run_date=? ORDER BY rank LIMIT 10`, rd)
		if err != nil {
			continue
		}
		for q.Next() {
			var sn snap
			if q.Scan(&sn.symbol, &sn.price) == nil && sn.price > 0 {
				top = append(top, sn)
			}
		}
		q.Close()
		if len(top) == 0 {
			continue
		}
		elapsed := len(allDates) - 1 - start
		for _, h := range []int{30, 60} {
			row := models.CompositeValidationRow{RunDate: rd, HorizonDays: h, DaysElapsed: elapsed}
			if start+h >= len(allDates) {
				res.Rows = append(res.Rows, row) // 未成熟
				continue
			}
			target := allDates[start+h]
			sum, n := 0.0, 0
			for _, sn := range top {
				exit := closeOn(sn.symbol, target)
				if exit <= 0 {
					continue
				}
				sum += exit/sn.price - 1
				n++
			}
			if n == 0 {
				res.Rows = append(res.Rows, row)
				continue
			}
			row.Matured = true
			row.N = n
			row.PortRet = math.Round((sum/float64(n)*100-costPct)*100) / 100
			level, _, err := s.history.EqualWeightIndexBetween(rd, target)
			if err == nil && level[rd] > 0 && level[target] > 0 {
				row.BenchRet = math.Round((level[target]/level[rd]-1)*100*100) / 100
			}
			row.Excess = math.Round((row.PortRet-row.BenchRet)*100) / 100
			res.Rows = append(res.Rows, row)
		}
	}
	return res
}
