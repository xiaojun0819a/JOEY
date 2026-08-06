package main

// 外部荐股跟单验证(2026-07-27 建,用户定名「天鼎早盘推荐」)。
//
// 用途:用户订阅的荐股服务每天 09:29 群发 5-6 只票,他想按「开盘买入 → 次日冲高卖出」跟单,
// 并用模拟持仓累积**真实前向样本**——因为卖方给的战绩截图是他们自己挑的一周,证明不了任何事。
//
// 三条定死的纪律(都是为了让样本可信,别为了好看去动):
//
// ① **记账价 = 提交时刻的实时价,不是当日开盘价。**
//    用户口头说"按开盘价或31分实时价",但如果 09:31 提交却按 09:30 开盘价记账,
//    等于白捡了这一分钟的涨跌——这正是本项目栽过的前视偏差(见 canAutoBuyNow 注释里的
//    哈药股份实例:凌晨按昨收建仓、白捡次日跳空,胜率系统性虚高)。
//    结果里另外回传当日开盘价 DayOpen 供对照,让用户自己看见"想买的价"和"真能买的价"差多少。
//
// ② **只在交易时段记账(canAutoBuyNow:交易日 09:25-15:00)。** 收盘后"按收盘价买入"现实不可成交,
//    且是拿已知结果当成交价。要补录特殊情况,走原有的「新增手动持仓」。
//
// ③ **涨停封死不记(isBuyableNow)。** 盘口无卖盘还记一笔,等于自欺。
//
// 另:本来源建仓即置 auto_exit_off=1,**风控引擎一律不碰**。
// 因为这次实验要测的是用户自己"冲高就卖"的手感——引擎替他卖了,测的就不是他的手了。

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// tipPickSource 来源 ID。ID 是数据唯一键不可改;界面名在前端 STRATEGY_SOURCE_LABELS 里。
const tipPickSource = "tip-tianding"

// 从任意文本里抠 6 位股票代码:短信/微信原文会带名字、日期、广告语,全部忽略只认数字。
//
// 取**极大数字串**再判长度==6,而不是直接匹配 `\d{6}`:
//   - 直接 `\d{6}` 会从 "20260727093022" 这种时间戳里截出假代码;
//   - 用 `(^|\D)(\d{6})(\D|$)` 夹住看似能防,但边界会被前一个匹配吃掉——
//     "600584,002167,002879" 只认得出第 1、3 个,中间那个的前导逗号已被消费(实测踩到)。
//
// 极大串写法两个问题都没有:14 位时间戳是一整串(长度≠6 被弃),逗号分隔的三个各自成串。
var tipDigitRunRe = regexp.MustCompile(`[0-9]+`)

// TipPickRow 单只票的处理结果。没记进去的 Skipped 非空,如实回传原因。
type TipPickRow struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`   // 实际记账价 = 提交时刻实时价
	DayOpen float64 `json:"dayOpen"` // 当日开盘价,仅供对照,不参与记账
	Shares  int64   `json:"shares"`
	Amount  float64 `json:"amount"` // 实际占用金额(整手取整后)
	Skipped string  `json:"skipped"`
}

type TipPickResult struct {
	Date    string       `json:"date"`
	At      string       `json:"at"` // 提交时刻,记账价就是这一刻的价
	Source  string       `json:"source"`
	Note    string       `json:"note"`
	Rows    []TipPickRow `json:"rows"`
	Added   int          `json:"added"`
	Skipped int          `json:"skipped"`
	Warning string       `json:"warning"`
}

// parseTipCodes 从原文里提取去重后的规范代码,保持出现顺序。
func parseTipCodes(raw string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, run := range tipDigitRunRe.FindAllString(raw, -1) {
		if len(run) != 6 {
			continue
		}
		sym := normalizeJournalStockSymbol(run)
		if len(sym) != 8 || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, sym)
	}
	return out
}

// AddTipPicks 把一批荐股按每只固定金额建进模拟持仓。
// raw=短信/消息原文(只认里面的 6 位代码);amountPerStock=每只投入金额(0 则默认 3 万);note=备注(哪家推的)。
func (a *App) AddTipPicks(raw string, amountPerStock float64, note string) (*TipPickResult, error) {
	if a.paperService == nil || a.marketService == nil {
		return nil, fmt.Errorf("服务未初始化")
	}
	if amountPerStock <= 0 {
		amountPerStock = 30000
	}
	codes := parseTipCodes(raw)
	if len(codes) == 0 {
		return nil, fmt.Errorf("没从文本里认出任何 6 位股票代码")
	}
	if len(codes) > 20 {
		return nil, fmt.Errorf("一次最多 20 只,认出了 %d 只,请检查文本里是否混进了别的数字", len(codes))
	}
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	if !a.canAutoBuyNow() {
		return nil, fmt.Errorf("当前不在可成交时段(需交易日 09:25-15:00)。现在记账要么拿昨收当买价、要么拿已知收盘价当买价,都是自欺;补录请走「新增手动持仓」")
	}

	res := &TipPickResult{
		Date:   now.Format("2006-01-02"),
		At:     now.Format("2006-01-02 15:04:05"),
		Source: tipPickSource,
		Note:   strings.TrimSpace(note),
	}

	rt, err := a.marketService.GetStockRealTimeData(codes...)
	if err != nil {
		return nil, fmt.Errorf("取实时行情失败:%w", err)
	}
	quote := map[string]int{}
	for i, s := range rt {
		quote[strings.ToLower(strings.TrimSpace(s.Symbol))] = i
	}

	// 已在手的同源持仓:同一天重复提交不重复建仓(手滑点两次不该变成双倍仓位)
	held := map[string]bool{}
	for _, p := range a.paperService.OpenPositions() {
		if p.Source == tipPickSource {
			held[strings.ToLower(strings.TrimSpace(p.Symbol))] = true
		}
	}

	for _, sym := range codes {
		row := TipPickRow{Symbol: sym}
		idx, ok := quote[sym]
		if !ok {
			row.Skipped = "取不到行情(代码可能不存在)"
			res.Rows = append(res.Rows, row)
			res.Skipped++
			continue
		}
		st := rt[idx]
		row.Name, row.Price, row.DayOpen = st.Name, st.Price, st.Open
		switch {
		case held[sym]:
			row.Skipped = "该源已有未平仓持仓,不重复建仓"
		case st.Price <= 0:
			row.Skipped = "实时价为 0,无法记账"
		default:
			if ok, why := a.isBuyableNow(sym, st.Price, st.ChangePercent); !ok {
				row.Skipped = "买不进:" + why
				break
			}
			shares := int64(amountPerStock/(st.Price*100)) * 100
			if shares <= 0 {
				row.Skipped = fmt.Sprintf("%.0f 元买不起 1 手(现价 %.2f)", amountPerStock, st.Price)
				break
			}
			id, err := a.paperService.Add(sym, st.Name, tipPickSource, st.Price, shares, "manual")
			if err != nil {
				row.Skipped = "落库失败:" + err.Error()
				break
			}
			// 纪律③:本来源永不由引擎平仓,建仓即接管
			_ = a.paperService.SetAutoExitOff(id, true)
			row.Shares, row.Amount = shares, float64(shares)*st.Price
			res.Added++
		}
		if row.Skipped != "" {
			res.Skipped++
		}
		res.Rows = append(res.Rows, row)
	}

	if res.Added > 0 {
		res.Warning = "记账价=提交时刻实时价(非开盘价),卖出请自行操作——风控引擎对本来源一律不接管"
	}
	log.Info("荐股建仓[%s] 记入%d 跳过%d @%s", res.Note, res.Added, res.Skipped, res.At)
	return res, nil
}

// ===== 买点信号推送扫描(2026-07-28 用户定:波段/三倍量/超跌起爆,14:30 与 14:45 各一次) =====
//
// 背景:此前**只有波段(手动跑)和低吸选股1(已下架)会推买点**,14:50 那轮全量扫描只建仓不推送,
// 所以摘掉低吸选项1 之后自动买点推送归零。用户要这三个策略在尾盘两个时点推给他。
//
// 纪律:
//   ① 只推送不建仓——建仓统一在 14:50 那轮,否则同票会在三个时点各建一次,仓位翻三倍。
//   ② 单飞锁:两个时点若因重启等原因叠到一起,后一轮直接跳过,不叠跑(NAS 是 J4125,
//      全量扫描叠跑过 OOM,见 tailScanAllMu 的注释)。
//   ③ 推送带【策略名】,几条并排能看出是谁选的。
//   ④ 涨停封死买不进的票不推(isPriceAtLimitUp,在 pushScannerSignals 里已拦)。

var signalPushScanMu sync.Mutex

// signalPushDeadlineMin 推送硬截止(14:57)。过了这个点还没算完就**放弃推送**:
// 手机收到时已接近/越过收盘,票根本买不进,推出去只是噪声,还会污染"我看到信号了"的判断。
// 2026-07-30 实录:波段那轮拖到 14:58:59 才出结果、推送 15:00 才到手机,用户明确要求 14:58 前推完。
const signalPushDeadlineMin = 14*60 + 57

// tooLateToPush 现在推还来得及吗(非交易日/收盘后一律来不及)。
func (a *App) tooLateToPush() bool {
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	return now.Hour()*60+now.Minute() >= signalPushDeadlineMin
}

// runSignalPushScan 买点信号推送扫描。part: "light"=三倍量+超跌起爆(约15秒) / "wave"=波段(约4分钟)。
// 拆开跑是因为两者耗时差一个数量级,混在一起会被波段拖到收盘后(见 monitor 侧时间表注释)。
func (a *App) runSignalPushScan(part string) {
	if a == nil || a.pushService == nil {
		return
	}
	if !signalPushScanMu.TryLock() {
		log.Info("买点信号扫描[%s]:上一轮还没跑完,本轮跳过(不叠跑)", part)
		return
	}
	defer signalPushScanMu.Unlock()

	// 让路:14:52 那次会紧跟在 14:50 自动建仓那轮(RunTailForwardScanAll)之后。
	// 这里**只探测不占用** tailScanAllMu——一旦真把锁拿在手里,建仓那轮的 TryLock 会失败、整轮被跳过,
	// 那比叠跑还糟。最多等 2 分钟:等太久会撞上推送截止,不如放弃这一轮。
	for i := 0; i < 24; i++ {
		if tailScanAllMu.TryLock() {
			tailScanAllMu.Unlock()
			break
		}
		if i == 0 {
			log.Info("买点信号扫描[%s]:全量扫描进行中,等它跑完再开始(避免叠跑打爆内存)", part)
		}
		time.Sleep(5 * time.Second)
	}
	if a.tooLateToPush() {
		log.Info("买点信号扫描[%s]:已过 14:57 推送截止,放弃本轮(推了也买不进)", part)
		return
	}

	// part=="full":波段先跑(最慢),**跑完立刻接着跑轻的两个**,不再等下一个定时窗口。
	// 用户 2026-07-30 定的顺序。好处是三个策略在同一轮里连着推完(约 14:40→14:45),
	// 不会出现"波段推了、另两个还要再等十几分钟"的割裂,也不用为轻任务单开一个时点。
	if part == "full" || part == "wave" {
		// ⚠️RunWaveScanner 内部**已经调用 pushWaveSignals**,这里绝不能再推一次(会重复轰炸);
		// 它也自带 waveScanMu 单飞锁,与手动扫描叠跑时会自己让路。
		// 用带闸门的口径(大盘环境不通过就不出票),与手动跑一致——推送不该比手动更宽松。
		res := a.RunWaveScanner()
		log.Info("买点信号扫描[波段]:选出 %d 只(闸门%v)", len(res.Items), res.GatePassed)
		// 放进跨账号共享池,署名「系统自动扫描」。不做这一步的话,用户打开波段弹窗
		// 看到的仍是上一次**手动**扫描的旧结果——后台刚扫过一轮界面上完全看不出来。
		publishSharedScan("RunWaveScanner", res, sharedScanAutoUser)
		if part == "wave" {
			return
		}
		if a.tooLateToPush() { // 波段若异常慢(降级态可到 8 分钟),后面的就别推了
			log.Info("买点信号扫描:波段跑完已过 14:57 截止,后续策略不再推送")
			return
		}
	}

	for _, s := range []struct {
		label  string
		method string // 共享池按 RPC 方法名索引,要和前端取数用的名字一致
		run    func(models.LowBuyScannerRequest) models.LowBuyScannerResult
	}{
		{"三倍量", "RunTripleVolumeScannerV5", a.RunTripleVolumeScannerV5},
		{"超跌起爆", "RunOversoldIgniteScanner", a.RunOversoldIgniteScanner},
	} {
		res := s.run(models.LowBuyScannerRequest{Limit: 10})
		// 先发布再判截止:结果本身值得看(界面上能看到后台扫过什么),
		// 只有"推送到手机"才受 14:57 截止约束——推了买不进才是噪声,看一眼不是。
		publishSharedScan(s.method, res, sharedScanAutoUser)
		if a.tooLateToPush() { // 每只算完再看一次表:降级态下单个扫描也可能拖很久
			log.Info("买点信号扫描[%s]:算完已过截止,不再推送", s.label)
			return
		}
		log.Info("买点信号扫描[%s]:选出 %d 只", s.label, len(res.Items))
		a.pushScannerSignals(res.Items, s.label)
	}
}

// ReportFrontendError 前端渲染崩溃上报(2026-07-30 建)。
//
// 起因:模拟持仓弹窗被 SafeBoundary 接住报"页面出错",但 SafeBoundary 只 console.error,
// 而 Wails 产品版打不开 devtools —— 错误信息完全取不到,只能靠猜。这类崩溃项目里已发生多次
// (体感练习白屏、模拟持仓 toFixed 崩),每次都要重新摸。
//
// 装上这个后,前端崩溃会落进 server.log,ssh 上去 grep「前端渲染崩溃」即可看到组件栈。
// 刻意不返回错误、不抛异常:上报失败绝不能再引发一次崩溃。
func (a *App) ReportFrontendError(where, message, stack string) string {
	trim := func(s string, n int) string {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) > n {
			return string(r[:n]) + "…"
		}
		return s
	}
	log.Warn("前端渲染崩溃 [%s] %s\n组件栈:%s", trim(where, 80), trim(message, 500), trim(stack, 1500))
	return "ok"
}
