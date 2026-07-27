package services

// 超跌起爆11·自学习库:每日把该策略的历史入选(实盘留痕+重算)与"次日结果"配对成样本,
// 按特征分桶统计胜负差异,自动生成学习日报与调优建议。
// 纪律(用户项目史的血泪约定,见 jcp-alpha-honesty / jcp-shortterm-falsified):
//   ① 硬门槛(回撤/爆量/大阳/共振)永不自动改——规则=成文,天天变的规则无法被验证;
//   ② 自动调整只允许作用于「评分权重」,且样本≥100、每次幅度≤±20%、间隔≥7天、全程版本留痕;
//   ③ 报告里每个结论必须带样本量,n<20 的差异一律标注"噪声级,仅观察";
//   ④ 统计口径=扣费超额(2026-07-24 起):次日收益 − 同日全市场中位涨幅(等权基准) − 往返成本,
//      裸红盘率只作对照展示——牛市里闭眼买红盘率也高,跑赢中位数并覆盖费用才算真赢。

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// OversoldLearnSample 一条学习样本:信号日特征 + 次日结果。
type OversoldLearnSample struct {
	SignalDate   string  `json:"signalDate"`
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Score        float64 `json:"score"`
	Pct          float64 `json:"pct"`          // 信号日涨幅
	VolMult      float64 `json:"volMult"`      // 爆量倍数
	VolDry       float64 `json:"volDry"`       // 地量度(沉寂量/年均量)
	Drawdown     float64 `json:"drawdown"`     // 250日回撤%
	RiseFromBase float64 `json:"riseFromBase"` // 距底%
	Lamps        int     `json:"lamps"`        // 五灯红数
	Kongpan      float64 `json:"kongpan"`      // 控盘度
	StrongCount  int     `json:"strongCount"`  // 短线能量强势次数
	UpperShadow  float64 `json:"upperShadow"`  // 上影%
	LimitUp      int     `json:"limitUp"`      // 是否涨停级(≥9.5%)
	Turnover     float64 `json:"turnover"`     // 信号日换手%
	MA5Dist      float64 `json:"ma5Dist"`      // 收盘距MA5%
	MA10Dist     float64 `json:"ma10Dist"`     // 收盘距MA10%
	// —— 系统信息流因子(2026-07-22 用户要求:后台推的信息匹配当日入选当因子) ——
	CrossHits    int     `json:"crossHits"` // 同日被其他策略选中的家数(-1=不可得)
	Lhb          int     `json:"lhb"`       // 信号日龙虎榜:1上榜/0未上/-1不可得(lhb_daily覆盖期外)
	LhbNet       float64 `json:"lhbNet"`    // 龙虎榜净买(元)
	NewsCount    int     `json:"newsCount"` // 信号日匹配快讯条数(-1=不可得,历史日无快讯存档)
	MainPct      float64 `json:"mainPct"`   // 主力净占比%(-999=缺失)
	BenchRet     float64 `json:"benchRet"`  // 次日基准:全市场中位涨幅%(等权,-999=待回填/不可得)
	NextOpenRet  float64 `json:"nextOpenRet"`
	NextCloseRet float64 `json:"nextCloseRet"`
	NextHighRet  float64 `json:"nextHighRet"`
}

// oversoldCostPct 往返成本(%):佣金万2.5双边≈0.05 + 印花税卖出0.05 + 过户费≈0.002 + 小票滑点≈0.1 ≈ 0.2。
// 扣费超额 = next_close_ret − bench_ret − 本常量;学习日报与调优依据全按此口径。
const oversoldCostPct = 0.2

func (s *HistoryService) initOversoldLearnSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS oversold_learn_samples (
			signal_date TEXT NOT NULL,
			symbol TEXT NOT NULL,
			name TEXT,
			score REAL, pct REAL, vol_mult REAL, vol_dry REAL,
			drawdown REAL, rise_from_base REAL, lamps INTEGER, kongpan REAL,
			strong_count INTEGER, upper_shadow REAL, limit_up INTEGER,
			turnover REAL, ma5_dist REAL, ma10_dist REAL,
			next_open_ret REAL, next_close_ret REAL, next_high_ret REAL,
			created_at TEXT,
			PRIMARY KEY(signal_date, symbol)
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}
	// 因子列迁移(2026-07-22 系统信息流因子;2026-07-24 bench_ret 等权基准)
	for _, col := range []string{"cross_hits INTEGER DEFAULT -1", "lhb INTEGER DEFAULT -1", "lhb_net REAL DEFAULT 0", "news_count INTEGER DEFAULT -1", "main_pct REAL DEFAULT -999", "bench_ret REAL DEFAULT -999"} {
		if _, err := s.db.Exec("ALTER TABLE oversold_learn_samples ADD COLUMN " + col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				historyLog.Warn("学习库加列失败 %s: %v", col, err)
			}
		}
	}
	return nil
}

// RefreshOversoldFactors 用库内数据刷新全部样本的系统信息流因子(共振/龙虎榜/主力,可全历史重算;
// 快讯只能前向攒,不在此刷)。表小(数百行),每次学习后全量刷保持最新。
func (s *HistoryService) RefreshOversoldFactors() {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE oversold_learn_samples SET cross_hits=(
		SELECT COUNT(DISTINCT p.strategy_id) FROM strategy_scan_picks p
		WHERE p.signal_date=oversold_learn_samples.signal_date AND p.stock_code=oversold_learn_samples.symbol
		  AND p.strategy_id<>'oversold-ignite-v1')`)
	var lhbMin, lhbMax sql.NullString
	_ = s.db.QueryRow(`SELECT MIN(trade_date), MAX(trade_date) FROM lhb_daily`).Scan(&lhbMin, &lhbMax)
	if lhbMin.Valid && lhbMax.Valid {
		_, _ = s.db.Exec(`UPDATE oversold_learn_samples SET
			lhb=CASE WHEN EXISTS(SELECT 1 FROM lhb_daily d WHERE d.trade_date=signal_date AND d.code=symbol) THEN 1 ELSE 0 END,
			lhb_net=COALESCE((SELECT d.net_buy FROM lhb_daily d WHERE d.trade_date=signal_date AND d.code=symbol),0)
			WHERE signal_date BETWEEN ? AND ?`, lhbMin.String, lhbMax.String)
		_, _ = s.db.Exec(`UPDATE oversold_learn_samples SET lhb=-1, lhb_net=0 WHERE signal_date NOT BETWEEN ? AND ?`, lhbMin.String, lhbMax.String)
	}
	_, _ = s.db.Exec(`UPDATE oversold_learn_samples SET main_pct=COALESCE(
		(SELECT d.main_pct FROM stock_daily d WHERE d.stock_code=symbol AND d.trade_date=signal_date AND d.main_pct IS NOT NULL), -999)`)
}

// MarketMedianPctPublic 对外暴露等权基准(回测用)。
func (s *HistoryService) MarketMedianPctPublic(date string) (float64, bool) {
	return s.marketMedianPct(date)
}

// marketMedianPct 某交易日全市场个股涨跌幅中位数(%),等权基准。
// 不用指数:本地 stock_daily 只存个股,而 GetKLineData("sh000001") 已被证实是坏的(见 strategy_review.go);
// 等权中位数本地全历史可算,且正好匹配选股策略的机会集——跑赢当日中位数才叫会选股。
// 行数<1000 视为该日采集不全,不给基准(宁缺勿假)。
func (s *HistoryService) marketMedianPct(date string) (float64, bool) {
	rows, err := s.db.Query(`SELECT pct_change FROM stock_daily WHERE trade_date=? AND pct_change IS NOT NULL`, date)
	if err != nil {
		return 0, false
	}
	defer rows.Close()
	vals := make([]float64, 0, 6000)
	for rows.Next() {
		var v sql.NullFloat64
		if rows.Scan(&v) == nil && v.Valid {
			vals = append(vals, v.Float64)
		}
	}
	if len(vals) < 1000 {
		return 0, false
	}
	sort.Float64s(vals)
	return vals[len(vals)/2], true
}

// RefreshOversoldBenchmarks 给缺基准的样本回填 bench_ret。
// 样本的"次日"=该股信号日后首个有行情日(与 NextDayBar 同口径);停牌跨多日的收益对上复牌日单日基准,
// 属已知近似(样本占比极小)。按日缓存中位数,表小+trade_date有索引,一轮几十毫秒级。
func (s *HistoryService) RefreshOversoldBenchmarks() {
	if s == nil || s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT signal_date, symbol FROM oversold_learn_samples WHERE bench_ret<=-900`)
	if err != nil {
		return
	}
	type sk struct{ d, sym string }
	pend := []sk{}
	for rows.Next() {
		var r sk
		if rows.Scan(&r.d, &r.sym) == nil {
			pend = append(pend, r)
		}
	}
	rows.Close()
	if len(pend) == 0 {
		return
	}
	medCache := map[string]float64{}
	filled := 0
	for _, p := range pend {
		var nd sql.NullString
		_ = s.db.QueryRow(`SELECT MIN(trade_date) FROM stock_daily WHERE stock_code=? AND trade_date>? AND close_price>0`, p.sym, p.d).Scan(&nd)
		if !nd.Valid || nd.String == "" {
			continue
		}
		med, ok := medCache[nd.String]
		if !ok {
			m, good := s.marketMedianPct(nd.String)
			if !good {
				continue
			}
			med = m
			medCache[nd.String] = m
		}
		if _, err := s.db.Exec(`UPDATE oversold_learn_samples SET bench_ret=? WHERE signal_date=? AND symbol=?`, med, p.d, p.sym); err == nil {
			filled++
		}
	}
	if filled > 0 {
		historyLog.Info("学习库基准回填 %d 笔(全市场中位涨幅,等权)", filled)
	}
}

// SetOversoldNewsCount 写入某样本的快讯匹配数(每日学习时对"最新交易日"的入选现场匹配)。
func (s *HistoryService) SetOversoldNewsCount(signalDate, symbol string, n int) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE oversold_learn_samples SET news_count=? WHERE signal_date=? AND symbol=?`, n, signalDate, symbol)
}

// OversoldPendingPicks 返回已有留痕但尚未入学习库、且"次日行情已定型"的 (signal_date, symbol) 列表。
func (s *HistoryService) OversoldPendingPicks() []struct{ SignalDate, Symbol, Name string } {
	out := []struct{ SignalDate, Symbol, Name string }{}
	if s == nil || s.db == nil {
		return out
	}
	if err := s.initOversoldLearnSchema(); err != nil {
		return out
	}
	// 次日已定型 = 库里存在 signal_date 之后的交易日行情(用该股自己的行是否存在判定,停牌自然跳过)
	rows, err := s.db.Query(`
		SELECT p.signal_date, p.stock_code, COALESCE(p.stock_name,'')
		FROM strategy_scan_picks p
		LEFT JOIN oversold_learn_samples l ON l.signal_date=p.signal_date AND l.symbol=p.stock_code
		WHERE p.strategy_id='oversold-ignite-v1' AND l.symbol IS NULL
		  AND EXISTS (SELECT 1 FROM stock_daily d WHERE d.stock_code=p.stock_code AND d.trade_date>p.signal_date AND d.close_price>0)
		ORDER BY p.signal_date`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r struct{ SignalDate, Symbol, Name string }
		if rows.Scan(&r.SignalDate, &r.Symbol, &r.Name) == nil {
			out = append(out, r)
		}
	}
	return out
}

// NextDayBar 返回某股 signalDate 之后第一个有行情交易日的 开/高/收 与信号日收盘。
func (s *HistoryService) NextDayBar(symbol, signalDate string) (sigClose, nOpen, nHigh, nClose float64, ok bool) {
	if s == nil || s.db == nil {
		return
	}
	_ = s.db.QueryRow(`SELECT close_price FROM stock_daily WHERE stock_code=? AND trade_date=? AND close_price>0`, symbol, signalDate).Scan(&sigClose)
	if sigClose <= 0 {
		return
	}
	var o, h, c sql.NullFloat64
	err := s.db.QueryRow(`SELECT open_price, high_price, close_price FROM stock_daily
		WHERE stock_code=? AND trade_date>? AND close_price>0 ORDER BY trade_date LIMIT 1`, symbol, signalDate).Scan(&o, &h, &c)
	if err != nil || !c.Valid || c.Float64 <= 0 {
		return
	}
	nClose = c.Float64
	nOpen = o.Float64
	nHigh = h.Float64
	if nOpen <= 0 {
		nOpen = nClose
	}
	if nHigh <= 0 {
		nHigh = nClose
	}
	ok = true
	return
}

// InsertOversoldSample 落一条学习样本(幂等)。
func (s *HistoryService) InsertOversoldSample(m OversoldLearnSample) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("db not ready")
	}
	if err := s.initOversoldLearnSchema(); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO oversold_learn_samples
		(signal_date,symbol,name,score,pct,vol_mult,vol_dry,drawdown,rise_from_base,lamps,kongpan,strong_count,upper_shadow,limit_up,turnover,ma5_dist,ma10_dist,cross_hits,lhb,lhb_net,news_count,main_pct,bench_ret,next_open_ret,next_close_ret,next_high_ret,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.SignalDate, m.Symbol, m.Name, m.Score, m.Pct, m.VolMult, m.VolDry, m.Drawdown, m.RiseFromBase,
		m.Lamps, m.Kongpan, m.StrongCount, m.UpperShadow, m.LimitUp, m.Turnover, m.MA5Dist, m.MA10Dist,
		m.CrossHits, m.Lhb, m.LhbNet, m.NewsCount, m.MainPct, m.BenchRet,
		m.NextOpenRet, m.NextCloseRet, m.NextHighRet, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

type oversoldBucketStat struct {
	Label   string
	N       int
	ExWin   float64 // 扣费超额胜率:(次日收盘−基准−成本)>0 占比
	ExAvg   float64 // 扣费超额均值
	RawWin  float64 // 裸红盘率(对照,未扣)
	AvgHigh float64 // 次日高点均值(未扣,看冲高卖空间)
}

// TurnoverOn 某股某日换手率(缺失返回0)。
func (s *HistoryService) TurnoverOn(symbol, date string) float64 {
	if s == nil || s.db == nil {
		return 0
	}
	var v sql.NullFloat64
	_ = s.db.QueryRow(`SELECT turnover FROM stock_daily WHERE stock_code=? AND trade_date=?`, symbol, date).Scan(&v)
	return v.Float64
}

// MarkPicksReplayedPublic 导出版重算标记(学习回放批量补留痕时用)。
func (s *HistoryService) MarkPicksReplayedPublic(strategyID, signalDate string) {
	s.markPicksReplayed(strategyID, signalDate)
}

// BuildOversoldLearnReport 全样本聚合出学习日报(文本),并返回样本量与整体扣费超额胜率。
func (s *HistoryService) BuildOversoldLearnReport() (string, int, float64) {
	if s == nil || s.db == nil {
		return "学习库未就绪", 0, 0
	}
	if err := s.initOversoldLearnSchema(); err != nil {
		return "学习库未就绪: " + err.Error(), 0, 0
	}
	var total int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM oversold_learn_samples`).Scan(&total)
	if total == 0 {
		return "学习库暂无样本(等每日收盘后自动入库,或先跑历史回放攒样本)", 0, 0
	}
	// 扣费超额口径:超额 = 次日收盘收益 − 同日全市场中位涨幅 − 往返成本。只统计基准可得的样本。
	exExpr := fmt.Sprintf("(next_close_ret-bench_ret-%.2f)", oversoldCostPct)
	var nb int
	var exWin, exAvg, rawWin, rawAvg, avgHigh sql.NullFloat64
	_ = s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*),
		AVG(CASE WHEN %s>0 THEN 100.0 ELSE 0 END), AVG(%s),
		AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END), AVG(next_close_ret), AVG(next_high_ret)
		FROM oversold_learn_samples WHERE bench_ret>-900`, exExpr, exExpr)).
		Scan(&nb, &exWin, &exAvg, &rawWin, &rawAvg, &avgHigh)

	dims := []struct {
		title string
		expr  string // CASE 分桶表达式
	}{
		{"五灯红数", `CASE WHEN lamps>=5 THEN '5红' WHEN lamps=4 THEN '4红' ELSE '≤3红' END`},
		{"爆量倍数", `CASE WHEN vol_mult>=8 THEN '≥8倍' WHEN vol_mult>=5 THEN '5-8倍' ELSE '3-5倍' END`},
		{"信号日涨幅", `CASE WHEN limit_up=1 THEN '涨停级' ELSE '6-9.5%' END`},
		{"距底位置", `CASE WHEN rise_from_base<=15 THEN '≤15%' WHEN rise_from_base<=28 THEN '15-28%' ELSE '>28%' END`},
		{"回撤深度", `CASE WHEN drawdown>=55 THEN '≥55%' WHEN drawdown>=45 THEN '45-55%' ELSE '38-45%' END`},
		{"地量度", `CASE WHEN vol_dry<=0.5 THEN '≤0.5干' WHEN vol_dry<=0.8 THEN '0.5-0.8' ELSE '>0.8' END`},
		{"上影线", `CASE WHEN upper_shadow<=1 THEN '≤1%' WHEN upper_shadow<=4 THEN '1-4%' ELSE '>4%' END`},
		{"换手率", `CASE WHEN turnover>=15 THEN '≥15%' WHEN turnover>=8 THEN '8-15%' WHEN turnover>0 THEN '<8%' ELSE '缺失' END`},
		{"控盘度", `CASE WHEN kongpan>=60 THEN '≥60' WHEN kongpan>=45 THEN '45-60' ELSE '<45' END`},
		{"短线能量", `CASE WHEN strong_count>=2 THEN '≥2次' WHEN strong_count=1 THEN '1次' ELSE '0次' END`},
		{"距MA5", `CASE WHEN ma5_dist>=15 THEN '≥15%' WHEN ma5_dist>=8 THEN '8-15%' ELSE '<8%' END`},
		{"其他策略共振", `CASE WHEN cross_hits<0 THEN '不可得' WHEN cross_hits=0 THEN '独家' WHEN cross_hits=1 THEN '1策略共振' ELSE '≥2策略共振' END`},
		{"龙虎榜", `CASE WHEN lhb=1 AND lhb_net>0 THEN '上榜净买' WHEN lhb=1 THEN '上榜净卖' WHEN lhb=0 THEN '未上榜' ELSE '不可得' END`},
		{"主力资金", `CASE WHEN main_pct<=-900 THEN '缺失' WHEN main_pct>0 THEN '净流入' ELSE '净流出' END`},
		{"消息面", `CASE WHEN news_count<0 THEN '不可得' WHEN news_count=0 THEN '无快讯' ELSE '有快讯' END`},
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【超跌起爆11·学习日报】样本 %d 笔(基准可得 %d)| 扣费超额胜率 %.1f%% | 超额均值 %+.2f%%\n",
		total, nb, exWin.Float64, exAvg.Float64)
	fmt.Fprintf(&b, "  对照(未扣费):裸红盘率 %.1f%% | 收盘均值 %+.2f%% | 高点均值 %+.2f%%\n",
		rawWin.Float64, rawAvg.Float64, avgHigh.Float64)
	fmt.Fprintf(&b, "(超额=次日收盘收益−同日全市场中位涨幅(等权基准,本地日线算)−往返成本%.1f%%;跑赢中位数并覆盖费用才算真赢。样本=历史留痕/重算入选×次日真实行情;结论只在两侧样本都≥20时才算数,更小的标注为噪声)\n\n", oversoldCostPct)
	if nb == 0 {
		b.WriteString("★ 基准尚未回填(每日自学习会自动补全 bench_ret;回填完成前不出超额分桶结论,不拿裸红盘率下判断)。\n")
		return b.String(), total, 0
	}

	type finding struct {
		text string
		gap  float64
	}
	findings := []finding{}
	for _, d := range dims {
		rows, err := s.db.Query(fmt.Sprintf(`SELECT %s b, COUNT(*),
			AVG(CASE WHEN %s>0 THEN 100.0 ELSE 0 END), AVG(%s),
			AVG(CASE WHEN next_close_ret>0 THEN 100.0 ELSE 0 END), AVG(next_high_ret)
			FROM oversold_learn_samples WHERE bench_ret>-900 GROUP BY b ORDER BY b`, d.expr, exExpr, exExpr))
		if err != nil {
			continue
		}
		stats := []oversoldBucketStat{}
		for rows.Next() {
			var st oversoldBucketStat
			if rows.Scan(&st.Label, &st.N, &st.ExWin, &st.ExAvg, &st.RawWin, &st.AvgHigh) == nil {
				stats = append(stats, st)
			}
		}
		rows.Close()
		if len(stats) < 2 {
			continue
		}
		fmt.Fprintf(&b, "◆ %s:", d.title)
		for _, st := range stats {
			fmt.Fprintf(&b, "  [%s] n=%d 超额胜率%.0f%% 超额均值%+.2f%%(裸红盘%.0f%%/高点%+.2f%%);", st.Label, st.N, st.ExWin, st.ExAvg, st.RawWin, st.AvgHigh)
		}
		b.WriteString("\n")
		// 提炼:最好桶 vs 最差桶(两侧 n≥20 才算发现),口径=扣费超额胜率。
		// 缺失/不可得桶只展示不参与对比——那是数据质量问题,不是因子信号(否则"有数据 vs 没数据"会冒充达标发现)。
		cmp := make([]oversoldBucketStat, 0, len(stats))
		for _, st := range stats {
			if st.Label != "缺失" && st.Label != "不可得" {
				cmp = append(cmp, st)
			}
		}
		if len(cmp) < 2 {
			continue
		}
		sort.Slice(cmp, func(i, j int) bool { return cmp[i].ExWin > cmp[j].ExWin })
		best, worst := cmp[0], cmp[len(cmp)-1]
		gap := best.ExWin - worst.ExWin
		if best.N >= 20 && worst.N >= 20 && gap >= 15 {
			findings = append(findings, finding{
				text: fmt.Sprintf("%s:[%s]超额胜率 %.0f%%(n=%d) vs [%s] %.0f%%(n=%d),差 %.0f 个点",
					d.title, best.Label, best.ExWin, best.N, worst.Label, worst.ExWin, worst.N, gap),
				gap: gap,
			})
		} else if gap >= 15 {
			fmt.Fprintf(&b, "   (差异 %.0f 点但样本不足20/侧,噪声级仅观察)\n", gap)
		}
	}
	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool { return findings[i].gap > findings[j].gap })
		b.WriteString("\n★ 达标发现(扣费超额口径,两侧n≥20且差≥15点,可作为调优依据):\n")
		for i, f := range findings {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, f.text)
		}
		b.WriteString("→ 调优纪律:以上只影响「评分权重」建议;硬门槛不自动改。自 2026-07-24 起调优依据一律为扣费超额,历史上按裸红盘率定的调整(如换手加分v1.1)待超额口径重新验证。样本≥100后可开自动权重微调(幅度≤±20%,间隔≥7天,版本留痕)。\n")
	} else {
		b.WriteString("\n★ 暂无达标发现(要求两侧样本≥20且扣费超额胜率差≥15点)——继续攒样本,别急着下结论。\n")
	}
	return b.String(), total, exWin.Float64
}
