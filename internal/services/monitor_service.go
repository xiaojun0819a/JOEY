package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"
)

var monitorLog = logger.New("monitor")

// 信号阈值——与低吸扫描器内置的买卖纪律(SellPointHint/StopLossHint)严格一致：
//
//	止损：跌破买入价 -5% 无条件止损；跌破10日线次日不收回离场
//	止盈：3日累计 +15% 减半；单日换手 >12% 先走
//	时间止损：持仓5日涨幅 <3% 清仓
const (
	stopLossPct        = -5.0 // 止损线
	takeProfitPct      = 15.0 // 止盈线（短线累计涨幅）
	turnoverExitPct    = 12.0 // 换手过高疑似出货
	timeStopHoldDays   = 5    // 时间止损：持仓交易日
	timeStopMinGainPct = 3.0  // 时间止损：涨幅低于此值则换股
)

// MonitorService 盘中信号监控：按低吸纪律盯持仓，命中即推送。
type MonitorService struct {
	sessionService *SessionService
	marketService  *MarketService
	historyService *HistoryService
	pushService    *PushService
	configService  *ConfigService

	buyScan     func(part string) // 买点信号推送回调(由 App 注入)。part: light=三倍量+超跌起爆 / wave=波段
	waveScan    func()            // 盘后波段扫描回调（由 App 注入，全A锯齿，17:30）
	paperExit   func()            // 模拟持仓风控巡检（由 App 注入，盘中每5分钟+尾盘窗口）
	boardReseal func()            // 游资炸板回封状态机（由 App 注入，盘中每分钟,名单空时是空转)
	dataHealth  func()            // 日K数据新鲜度自检(由 App 注入,每交易日 09:10 与 17:20)
	xOpenBuy    func()            // X 荐股博主推荐的次日开盘建仓(由 App 注入,每交易日 09:30)

	mu            sync.Mutex
	cancel        context.CancelFunc
	lastHardStop  time.Time         // 上次 -5% 硬止损快循环时间
	lastEnvCheck  time.Time         // 上次大盘环境检查时间
	lastPaperExit time.Time         // 上次模拟持仓风控巡检时间
	lastGate      *bool             // 上次大盘闸门状态(nil=未建立基线)
	dailyDone     map[string]string // 固定时点任务 taskKey -> 已执行日期
	running       bool
}

func NewMonitorService(sessionService *SessionService, marketService *MarketService, historyService *HistoryService, pushService *PushService, configService *ConfigService) *MonitorService {
	return &MonitorService{
		sessionService: sessionService,
		marketService:  marketService,
		historyService: historyService,
		pushService:    pushService,
		configService:  configService,
		dailyDone:      make(map[string]string),
	}
}

// SetBuyScanFunc 注入尾盘买点扫描回调。
func (m *MonitorService) SetBuyScanFunc(fn func(part string)) {
	if m == nil {
		return
	}
	m.buyScan = fn
}

// SetWaveScanFunc 注入盘后波段扫描回调（全A锯齿，17:30，盘后历史更新完再跑）。
func (m *MonitorService) SetWaveScanFunc(fn func()) {
	if m == nil {
		return
	}
	m.waveScan = fn
}

// SetPaperExitFunc 注入模拟持仓风控巡检(盘中止损/止盈实时触线+尾盘收盘口径判定)。
func (m *MonitorService) SetPaperExitFunc(fn func()) {
	if m == nil {
		return
	}
	m.paperExit = fn
}

// SetDataHealthFunc 注入日K数据新鲜度自检(见 app_data_health.go)。
func (m *MonitorService) SetDataHealthFunc(fn func()) {
	if m == nil {
		return
	}
	m.dataHealth = fn
}

// SetXOpenBuyFunc 注入 X 荐股博主的开盘建仓(见 app_xblogger_buy.go)。
func (m *MonitorService) SetXOpenBuyFunc(fn func()) {
	if m == nil {
		return
	}
	m.xOpenBuy = fn
}

// SetBoardResealFunc 注入游资炸板回封状态机(封死→炸板→回封买入,成文规则v1)。
func (m *MonitorService) SetBoardResealFunc(fn func()) {
	if m == nil {
		return
	}
	m.boardReseal = fn
}

func (m *MonitorService) cstNow() time.Time {
	return time.Now().In(time.FixedZone("CST", 8*60*60))
}

func (m *MonitorService) getConfig() models.MonitorConfig {
	cfg := models.MonitorConfig{Enabled: false, IntervalMinutes: 15, AfterMarketCheck: true}
	if m == nil || m.configService == nil {
		return cfg
	}
	appCfg := m.configService.GetConfig()
	if appCfg == nil {
		return cfg
	}
	mc := appCfg.Push.Monitor
	if mc.IntervalMinutes <= 0 {
		mc.IntervalMinutes = 15
	}
	return mc
}

// Start 启动盘中调度（1 分钟节拍，按市场状态与时钟分发）。
func (m *MonitorService) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		m.tick()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				m.tick()
			}
		}
	}()
}

func (m *MonitorService) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// tick 每分钟调度一次。按"定时+贴收盘"分层派发，全部以真实市场状态为网关：
//   - 盘中每 IntervalMinutes：仅 -5% 硬止损快循环（轻量，只读持仓实时价）
//   - 14:00：尾盘买点扫描（低吸尾盘买）
//   - 14:30 / 14:55：完整体检（MA10/+15%/换手>12%，贴收盘判断减少噪音）
//   - 17:00：盘后时间止损
func (m *MonitorService) tick() {
	cfg := m.getConfig()
	if m.marketService == nil {
		return
	}
	status := m.marketService.GetMarketStatus()
	if !status.IsTradeDay {
		return
	}
	now := m.cstNow()
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()

	// 模拟持仓风控巡检：独立于推送监控开关(纪律执行是核心功能,不是可选推送)。
	// 盘中每5分钟触线判定;尾盘窗口(14:45-15:00)自然覆盖收盘口径条款的当天执行。
	// 炸板回封状态机:每分钟必须看一眼(名单为空时开销≈0);独立于推送开关,与风控巡检同理
	if status.Status == "trading" && m.boardReseal != nil {
		m.runGuarded(m.boardReseal)
	}
	if status.Status == "trading" && m.paperExit != nil {
		m.mu.Lock()
		pdue := m.lastPaperExit.IsZero() || now.Sub(m.lastPaperExit) >= 5*time.Minute
		if pdue {
			m.lastPaperExit = now
		}
		m.mu.Unlock()
		if pdue {
			m.runGuarded(m.paperExit)
		}
	}
	if !cfg.Enabled {
		return
	}

	// 盘中 -5% 硬止损快循环（灾难线，唯一值得盘中紧盯）
	if status.Status == "trading" {
		interval := cfg.IntervalMinutes
		if interval <= 0 {
			interval = 15
		}
		m.mu.Lock()
		due := m.lastHardStop.IsZero() || now.Sub(m.lastHardStop) >= time.Duration(interval)*time.Minute
		if due && !m.running {
			m.lastHardStop = now
		}
		m.mu.Unlock()
		if due {
			m.runGuarded(func() { m.CheckHardStop() })
		}

		// 大盘环境检查：每 15 分钟，仅在闸门翻转时推送
		m.mu.Lock()
		envDue := m.lastEnvCheck.IsZero() || now.Sub(m.lastEnvCheck) >= 15*time.Minute
		if envDue {
			m.lastEnvCheck = now
		}
		m.mu.Unlock()
		if envDue {
			m.runGuarded(func() { m.CheckMarketEnv() })
		}
	}

	// 固定时点任务（贴收盘 / 盘后），各自每交易日仅一次
	// 买点信号扫描(2026-07-30 用户定;全部 detached,不占 monitor 单槽)。
	//
	// 硬要求:**14:58 前必须推完**,否则手机收到时已收盘、票买不进,推了等于噪声。
	// 盘中实测耗时(收盘后测的数据严重低估,别再用):
	//   三倍量 + 超跌起爆 ≈ 15 秒(轻)
	//   波段(全A锯齿)   ≈ 3分43秒(重)
	//
	// **每交易日只此一轮(14:40,用户 2026-07-30 定)**:波段先跑(最慢),
	// 跑完立刻接着跑轻的两个 → 约 14:45 全部推完。
	// 串成一轮而不是各排各的时点,是因为波段耗时波动大(降级态可到 8 分钟),
	// 固定时点排在它后面要么撞车、要么留太多空档;跑完即接最省时也最紧凑。
	// 即使波段拖到 8 分钟,14:48 也还在截止线内;真超了 App 侧会放弃推送。
	// (曾另排过 14:30 的轻量预警轮,用户觉得多余,已摘。)
	//
	// **只推送不建仓**:建仓统一在 14:50 那轮,否则同票会在多个时点各建一次,仓位翻倍。
	if m.buyScan != nil {
		m.fireDailyWindowDetached("signalscan1440", today, nowMin, 14*60+40, func() { m.buyScan("full") })
	}
	// 日K数据新鲜度自检:两个时点各一次(2026-07-31 加)。
	//   09:10 开盘前 —— 此刻应有"上一交易日"数据,让你在下单前就知道地基是不是旧的
	//   17:20 采集窗口(16:00-17:00)之后 —— 此刻应有"今天"的数据,当天就抓到采集失败
	// 起因:采集开关被关掉后 stock_daily 连断 4 个交易日无人察觉(体感练习/复盘/学习日报/
	// 扣费超额基准全部静默停在那天,界面不报错),最后靠用户提问才发现。静默失效必须主动探。
	if m.dataHealth != nil {
		m.fireDailyWindow("datahealth0910", today, nowMin, 9*60+10, m.dataHealth)
		m.fireDailyWindow("datahealth1720", today, nowMin, 17*60+20, m.dataHealth)
	}
	// X 荐股博主的推荐在 09:30 开盘建仓。用**执行时刻的实时价**记账,不是当日开盘价——
	// 若因重启拖到 09:50 才跑,那就按 09:50 的价买,记录里的 day_open 会把这个差异显出来。
	// 拿已经走完的开盘价回头入账才是前视偏差。
	if m.xOpenBuy != nil {
		m.fireDailyWindowDetached("xopenbuy0930", today, nowMin, 9*60+30, m.xOpenBuy)
	}
	m.fireDailyWindow("tail1430", today, nowMin, 14*60+30, func() { m.MonitorPositionsIntraday() })
	m.fireDailyWindow("tail1455", today, nowMin, 14*60+55, func() { m.MonitorPositionsIntraday() })
	if cfg.AfterMarketCheck {
		m.fireDailyWindow("timestop", today, nowMin, 17*60, func() { m.RunAfterMarketCheck() })
	}
	// 盘后波段(锯齿)扫描：等历史数据更新完再跑，独立于时间止损开关
	m.fireDailyWindow("wavescan", today, nowMin, 17*60+30, func() {
		if m.waveScan != nil {
			m.waveScan()
		}
	})
}

// fireDailyWindow 在 [targetMin, targetMin+30) 窗口内、每交易日首个 tick 执行一次 fn。
// 30 分钟窗口避免 app 在目标时间之后启动时误触发陈旧任务。
func (m *MonitorService) fireDailyWindow(taskKey, today string, nowMin, targetMin int, fn func()) {
	if nowMin < targetMin || nowMin >= targetMin+30 {
		return
	}
	m.mu.Lock()
	done := m.dailyDone[taskKey] == today
	if !done && !m.running {
		m.dailyDone[taskKey] = today
	}
	m.mu.Unlock()
	if !done {
		m.runGuarded(fn)
	}
}

// fireDailyWindowDetached 与 fireDailyWindow 同样的"每交易日仅一次"窗口语义,但**不占用 monitor
// 的单槽执行位**(runGuarded),而是起独立 goroutine 跑。给「自带并发控制且耗时长」的任务用。
//
// ⚠️2026-07-30 血的教训:买点信号扫描原来走 runGuarded,而盘中它要跑 8 分半(波段一家 3分43秒,
// 我此前测的 55s 是收盘后测的,严重低估)。后果两头堵:
//
//	① 它占着那把锁的 14:50-14:59,炸板回封(每分钟)和模拟持仓风控巡检(每5分钟)全被卡死;
//	② 它自己也被别的任务挡着——当天 14:30 的窗口一路被挡到 14:50:27 才开始,
//	   导致推送落到 15:00 收盘后才到手机,票根本来不及买。
//
// 所以:长任务必须脱离这把全局锁,靠自己的 mutex 防重入。
func (m *MonitorService) fireDailyWindowDetached(taskKey, today string, nowMin, targetMin int, fn func()) {
	if nowMin < targetMin || nowMin >= targetMin+30 {
		return
	}
	m.mu.Lock()
	done := m.dailyDone[taskKey] == today
	if !done {
		m.dailyDone[taskKey] = today // 不看 m.running:本任务不排这个队
	}
	m.mu.Unlock()
	if !done {
		go fn()
	}
}

func (m *MonitorService) runGuarded(fn func()) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()
	fn()
}

// MonitorPositionsIntraday 盘中遍历持仓，按优先级判定单条信号并推送。
// 返回触发的信号数（便于手动调用/测试）。
func (m *MonitorService) MonitorPositionsIntraday() int {
	if m == nil || m.sessionService == nil || m.pushService == nil {
		return 0
	}
	positions := m.sessionService.ListPositions()
	if len(positions) == 0 {
		return 0
	}
	snap := m.snapshotMap()
	today := m.cstNow().Format("2006-01-02")
	fired := 0
	for _, held := range positions {
		row, ok := snap[held.StockCode]
		if !ok || row.Price <= 0 || held.Position.CostPrice <= 0 {
			continue
		}
		price := row.Price
		turnover := row.TurnoverRate
		ma10 := m.ma10(held.StockCode)
		cost := held.Position.CostPrice
		pnl := (price - cost) / cost * 100

		// ⚠️A股 T+1:当日买入当日不可卖。下面 1/2/3 类告警都是"现在就走"的指令,
		// 对当日新仓根本执行不了——2026-07-27 实例:14:10 推荐买入、14:44 就推"换手>12% 建议先走",
		// 而那批仓位买入日正是当天。
		// 这里**不静默丢弃**:跌到 -5% 你仍然该知道,只是不能假装你现在能卖。
		// 所以照推、但把 T+1 事实钉在文案里,并降一档紧急度(不必立刻掏手机)。
		// 注:第 4 类「跌破10日线」文案本来就写"明日不收回则离场",指向次日,不受影响。
		// (模拟持仓引擎 app.go ApplyPaperExitRules 早在 2026-07-21 就加了同款守卫,
		//  推送这条路一直漏着,同一个坑只补了一半。)
		newToday := strings.TrimSpace(held.Position.BuyDate) == today
		t1Note := ""
		level := "timeSensitive"
		if newToday {
			t1Note = "｜⚠️当日买入,T+1 次日才可卖,今日无法执行"
			level = "active"
		}

		// 1) 止损 -5%（无条件，最高优先级）
		if pnl <= stopLossPct {
			m.push(held, models.PushTypeStopLoss, level,
				fmt.Sprintf("现价 %.2f，成本 %.2f，浮亏 %.1f%%，触发 -5%% 止损线，无条件离场%s", price, cost, pnl, t1Note))
			fired++
			continue
		}

		// 2) 止盈 +15%（短线累计涨幅，减半）
		if pnl >= takeProfitPct {
			m.push(held, models.PushTypeTakeProfit, level,
				fmt.Sprintf("现价 %.2f，累计涨 %.1f%%，达 +15%% 止盈线，建议减半仓锁利%s", price, pnl, t1Note))
			fired++
			continue
		}

		// 3) 换手 >12%（疑似主力出货）
		if turnover > turnoverExitPct {
			m.push(held, models.PushTypeTakeProfit, level,
				fmt.Sprintf("换手率 %.1f%%（>12%%），主力疑似出货，浮盈 %.1f%%，建议先走%s", turnover, pnl, t1Note))
			fired++
			continue
		}

		// 4) 跌破 10 日线（次日不收回离场）
		if ma10 > 0 && price < ma10 {
			m.push(held, models.PushTypeStopLoss, "active",
				fmt.Sprintf("现价 %.2f 跌破 10 日线(%.2f)，浮动 %.1f%%，明日不收回则离场", price, ma10, pnl))
			fired++
			continue
		}
	}
	if fired > 0 {
		monitorLog.Info("盘中监控：持仓 %d 只，触发 %d 条信号", len(positions), fired)
	}
	return fired
}

// CheckHardStop 仅检查 -5% 硬止损（轻量：批量读持仓实时价，不拉全A快照），用于盘中快循环。
func (m *MonitorService) CheckHardStop() int {
	if m == nil || m.sessionService == nil || m.pushService == nil || m.marketService == nil {
		return 0
	}
	positions := m.sessionService.ListPositions()
	if len(positions) == 0 {
		return 0
	}
	codes := make([]string, 0, len(positions))
	for _, h := range positions {
		codes = append(codes, h.StockCode)
	}
	stocks, err := m.marketService.GetStockRealTimeData(codes...)
	if err != nil {
		monitorLog.Warn("硬止损取实时价失败: %v", err)
		return 0
	}
	priceMap := make(map[string]float64, len(stocks))
	for _, s := range stocks {
		priceMap[s.Symbol] = s.Price
	}
	fired := 0
	for _, h := range positions {
		price := priceMap[h.StockCode]
		cost := h.Position.CostPrice
		if price <= 0 || cost <= 0 {
			continue
		}
		pnl := (price - cost) / cost * 100
		if pnl <= stopLossPct {
			m.push(h, models.PushTypeStopLoss, "timeSensitive",
				fmt.Sprintf("现价 %.2f，成本 %.2f，浮亏 %.1f%%，触发 -5%% 止损线，无条件离场", price, cost, pnl))
			fired++
		}
	}
	if fired > 0 {
		monitorLog.Info("盘中硬止损：触发 %d 条", fired)
	}
	return fired
}

// RunAfterMarketCheck 盘后时间止损：持仓>=5个交易日且涨幅<3% 建议清仓换股。
func (m *MonitorService) RunAfterMarketCheck() int {
	if m == nil || m.sessionService == nil || m.pushService == nil {
		return 0
	}
	positions := m.sessionService.ListPositions()
	if len(positions) == 0 {
		return 0
	}
	snap := m.snapshotMap()
	fired := 0
	for _, held := range positions {
		buyDate := strings.TrimSpace(held.Position.BuyDate)
		if buyDate == "" || m.historyService == nil {
			continue // 没有买入日期无法计算持仓天数
		}
		holdDays := m.historyService.CountTradingDaysSince(buyDate)
		if holdDays < timeStopHoldDays {
			continue
		}
		row, ok := snap[held.StockCode]
		if !ok || row.Price <= 0 || held.Position.CostPrice <= 0 {
			continue
		}
		pnl := (row.Price - held.Position.CostPrice) / held.Position.CostPrice * 100
		if pnl < timeStopMinGainPct {
			m.push(held, models.PushTypeTimeStop, "active",
				fmt.Sprintf("持仓 %d 个交易日，涨幅仅 %.1f%%（<3%%），效率低，建议明日清仓换股", holdDays, pnl))
			fired++
		}
	}
	if fired > 0 {
		monitorLog.Info("盘后时间止损：触发 %d 条", fired)
	}
	return fired
}

// 大盘闸门阈值（与低吸扫描器一致，按最近半年真实分布校准）
const (
	gateLimitUpMin   = 60
	gateLimitDownMax = 50
	gateAmountMin    = 2e12 // 2.0万亿
)

// CheckMarketEnv 评估大盘闸门，仅在 ✅↔❌ 翻转时推送一次。
func (m *MonitorService) CheckMarketEnv() {
	if m == nil || m.marketService == nil || m.pushService == nil {
		return
	}
	snap, err := m.marketService.BuildScanMarketSnapshot()
	if err != nil {
		return
	}
	passed := evalMarketGate(snap)

	m.mu.Lock()
	prev := m.lastGate
	m.lastGate = &passed
	m.mu.Unlock()

	if prev == nil || *prev == passed {
		return // 基线建立 或 状态未变
	}
	if passed {
		m.pushService.Push(models.PushSignal{
			StockCode: "MARKET_up", StockName: "大盘", Type: models.PushTypeEnvChange, Level: "active",
			Message: fmt.Sprintf("大盘过滤 ❌→✅，结构转好，低吸观察池可激活（上证%.0f / 涨停%d / 跌停%d）", snap.ShPrice, snap.LimitUpCount, snap.LimitDownCount),
		})
	} else {
		m.pushService.Push(models.PushSignal{
			StockCode: "MARKET_down", StockName: "大盘", Type: models.PushTypeEnvChange, Level: "timeSensitive",
			Message: fmt.Sprintf("大盘过滤 ✅→❌，结构转弱，谨慎操作/降仓（上证%.0f / 涨停%d / 跌停%d）", snap.ShPrice, snap.LimitUpCount, snap.LimitDownCount),
		})
	}
	monitorLog.Info("大盘闸门翻转: %v -> %v", *prev, passed)
}

// evalMarketGate 镜像低吸扫描器的大盘闸门：4个子条件按有效数动态判定。
func evalMarketGate(s ScanMarketSnapshot) bool {
	plausible := func(p float64) bool { return p > 100 && p < 100000 }
	gateScore, validGateCount := 0, 4
	shOk := plausible(s.ShPrice) && plausible(s.ShMA20)
	if !shOk {
		validGateCount--
	} else if s.ShPrice > s.ShMA20 {
		gateScore++
	}
	if s.LimitUpCount > gateLimitUpMin {
		gateScore++
	}
	if s.LimitDownCount < gateLimitDownMax {
		gateScore++
	}
	if s.TotalAmount > gateAmountMin {
		gateScore++
	} else if s.TotalAmount <= 0 {
		validGateCount--
	}
	if validGateCount < 2 {
		validGateCount = 2
	}
	required := 3
	if validGateCount <= 3 {
		required = 2
	}
	return gateScore >= required
}

// snapshotMap 取一次全A快照（盘中含实时价/换手），按代码建索引供本轮复用。
func (m *MonitorService) snapshotMap() map[string]ScanSnapshotRow {
	if m.marketService == nil {
		return nil
	}
	rows, err := m.marketService.GetAllAStockSnapshot(false)
	if err != nil {
		monitorLog.Warn("监控取全A快照失败: %v", err)
		return nil
	}
	mp := make(map[string]ScanSnapshotRow, len(rows))
	for _, r := range rows {
		mp[r.Symbol] = r
	}
	return mp
}

// ma10 取单只 10 日线（来自日K，最后一根的 MA10）。
func (m *MonitorService) ma10(code string) float64 {
	if m.marketService == nil {
		return 0
	}
	if klines, err := m.marketService.GetKLineData(code, "1d", 20); err == nil && len(klines) > 0 {
		return klines[len(klines)-1].MA10
	}
	return 0
}

func (m *MonitorService) push(held models.HeldPosition, signalType, level, message string) {
	m.pushService.Push(models.PushSignal{
		StockCode: held.StockCode,
		StockName: held.StockName,
		Type:      signalType,
		Message:   message,
		Level:     level,
	})
}
