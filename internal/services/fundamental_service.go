package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
)

// 东方财富"业绩报表"批量接口：一次返回全市场单季财务（ROE/营收增速/净利增速/毛利/每股现金流…）
const emPerfReportName = "RPT_LICO_FN_CPD"

type emPerfRow struct {
	SecurityCode  string   `json:"SECURITY_CODE"`
	Name          string   `json:"SECURITY_NAME_ABBR"`
	ROE           *float64 `json:"WEIGHTAVG_ROE"`
	RevYoY        *float64 `json:"YSTZ"`
	ProfitYoY     *float64 `json:"SJLTZ"`
	GrossMargin   *float64 `json:"XSMLL"`
	CFPS          *float64 `json:"MGJYXJJE"`
	EPS           *float64 `json:"BASIC_EPS"`
	ReportDate    string   `json:"REPORTDATE"`
	NoticeDate    string   `json:"NOTICE_DATE"`
}

type emPerfResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Result  struct {
		Pages int         `json:"pages"`
		Data  []emPerfRow `json:"data"`
	} `json:"result"`
}

// FetchAndStoreFundamentals 批量拉取某报告期(如 2025-03-31)全市场业绩报表，入库 stock_fundamentals。
// 返回入库条数与告警。仅保留沪深主板/创业板/科创板(60/00/30/68)，剔除北交/新三板。
func (s *HistoryService) FetchAndStoreFundamentals(reportDate string) (int, string) {
	if s == nil || s.db == nil {
		return 0, "历史库未就绪"
	}
	reportDate = strings.TrimSpace(reportDate)
	if reportDate == "" {
		return 0, "报告期为空"
	}
	client := &http.Client{Timeout: 25 * time.Second}
	cols := "SECURITY_CODE,SECURITY_NAME_ABBR,WEIGHTAVG_ROE,YSTZ,SJLTZ,XSMLL,MGJYXJJE,BASIC_EPS,REPORTDATE,NOTICE_DATE"
	const pageSize = 500
	saved := 0
	now := time.Now().Format("2006-01-02 15:04:05")

	tx, err := s.db.Begin()
	if err != nil {
		return 0, "开启事务失败：" + err.Error()
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO stock_fundamentals
		(stock_code, report_date, stock_name, roe, rev_yoy, profit_yoy, gross_margin, cfps, eps, notice_date, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, "预编译失败：" + err.Error()
	}

	for page := 1; page <= 30; page++ {
		api := fmt.Sprintf("https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=%s&columns=%s&pageSize=%d&pageNumber=%d&sortColumns=NOTICE_DATE&sortTypes=-1&source=WEB&client=WEB&filter=%s",
			emPerfReportName, cols, pageSize, page, url.QueryEscape(fmt.Sprintf("(REPORTDATE='%s')", reportDate)))
		req, _ := http.NewRequest("GET", api, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Referer", "https://data.eastmoney.com/")
		resp, err := client.Do(req)
		if err != nil {
			_ = tx.Rollback()
			return saved, "请求失败：" + err.Error()
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var pr emPerfResp
		if err := json.Unmarshal(body, &pr); err != nil {
			_ = tx.Rollback()
			return saved, "解析失败：" + err.Error()
		}
		if !pr.Success || len(pr.Result.Data) == 0 {
			break
		}
		for _, r := range pr.Result.Data {
			code := normalizeFundamentalCode(r.SecurityCode)
			if code == "" {
				continue
			}
			rd := r.ReportDate
			if len(rd) >= 10 {
				rd = rd[:10]
			}
			if _, err := stmt.Exec(code, rd, r.Name,
				nf(r.ROE), nf(r.RevYoY), nf(r.ProfitYoY), nf(r.GrossMargin), nf(r.CFPS), nf(r.EPS),
				datePartShort(r.NoticeDate), now); err == nil {
				saved++
			}
		}
		if page >= pr.Result.Pages {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return saved, "提交失败：" + err.Error()
	}
	return saved, ""
}

// fundamentalRuleSet 基本面硬规则（仅含可批量取得的量化指标；市占率/壁垒/政策等定性留给步骤③人工）。
type fundamentalRuleSet struct {
	preset, label, rulesText            string
	excludeST                           bool
	minAnnROE                           float64 // 年化ROE下限(0=不限)
	profitYoYMin, profitYoYMax          float64 // 净利同比区间(Max=0 不设上限)
	revYoYMin                           float64 // 营收同比下限
	minGrossMargin                      float64 // 毛利率下限
	requirePositiveCFPS                 bool    // 每股经营现金流>0
	requirePositiveEPS                  bool    // 不亏损
	minAmountYuan                       float64 // 当日成交额下限(元)
	maxDebtRatio                        float64 // 资产负债率上限(%, 0=不限)
	maxValuationPctile                  float64 // 估值历史分位上限(%, 0=不限)
	maxGoodwillRatio                    float64 // 商誉占净资产上限(%, 0=不限)
}

func fundamentalPreset(preset string) fundamentalRuleSet {
	switch preset {
	case "boom": // 模板B 中线景气趋势
		return fundamentalRuleSet{
			preset: "boom", label: "中线景气(模板B)",
			rulesText: "排除ST/亏损/流动性差(日均<5000万)；入选：净利增速>20%、营收增速>15%、不亏损。注：行业景气/产业链环节/机构持仓需人工复核(步骤③)。",
			excludeST: true, requirePositiveEPS: true,
			profitYoYMin: 20, revYoYMin: 15, minAmountYuan: 5e7,
		}
	default: // 模板A 长线价值
		return fundamentalRuleSet{
			preset: "value", label: "长线价值(模板A)",
			rulesText: "排除ST、资产负债率>70%、商誉占净资产>30%；入选：年化ROE≥16%、净利增速10-30%、营收增速>0、毛利率≥20%、每股经营现金流>0、估值历史分位<40%(近250日市值分位代理PE分位)。注：分红率(派现/净利)、市占率、壁垒为步骤③人工复核。",
			excludeST: true, minAnnROE: 16, profitYoYMin: 10, profitYoYMax: 30,
			revYoYMin: 0, minGrossMargin: 20, requirePositiveCFPS: true,
			maxDebtRatio: 70, maxValuationPctile: 40, maxGoodwillRatio: 30,
		}
	}
}

// roeAnnualMult 把单季/累计ROE年化的倍数（按报告期月份）。
func roeAnnualMult(reportDate string) float64 {
	if len(reportDate) < 7 {
		return 1
	}
	switch reportDate[5:7] {
	case "03":
		return 4
	case "06":
		return 2
	case "09":
		return 4.0 / 3
	default: // 12 年报
		return 1
	}
}

// RunFundamentalScan 基本面初筛(步骤①②)：按模板A/B 规则筛全市场最新财务 + 关联市值/流动性，打分排序。
func (s *HistoryService) RunFundamentalScan(preset string) models.FundamentalScanResult {
	rs := fundamentalPreset(preset)
	res := models.FundamentalScanResult{Preset: rs.preset, PresetLabel: rs.label, RulesText: rs.rulesText}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	var reportDate string
	if err := s.db.QueryRow(`SELECT MAX(report_date) FROM stock_fundamentals`).Scan(&reportDate); err != nil || reportDate == "" {
		res.Warning = "尚无财务数据，请先刷新基本面"
		return res
	}
	res.ReportDate = reportDate
	mult := roeAnnualMult(reportDate)

	var tradeDate string
	_ = s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily`).Scan(&tradeDate)

	rows, err := s.db.Query(`SELECT f.stock_code, f.stock_name, f.roe, f.rev_yoy, f.profit_yoy, f.gross_margin, f.cfps, f.eps, f.debt_ratio, f.goodwill_ratio, f.dividend_yield,
			d.close_price, d.total_market_cap, d.amount
		FROM stock_fundamentals f
		LEFT JOIN stock_daily d ON d.stock_code=f.stock_code AND d.trade_date=?
		WHERE f.report_date=?`, tradeDate, reportDate)
	if err != nil {
		res.Warning = "查询失败：" + err.Error()
		return res
	}
	defer rows.Close()

	for rows.Next() {
		var code, name string
		var roe, revYoY, profitYoY, gm, cfps, eps, debt, goodwill, divYield, close, cap, amount sql.NullFloat64
		var nameN sql.NullString
		if err := rows.Scan(&code, &nameN, &roe, &revYoY, &profitYoY, &gm, &cfps, &eps, &debt, &goodwill, &divYield, &close, &cap, &amount); err != nil {
			continue
		}
		name = nameN.String
		res.UniverseCount++

		// 硬排除
		if rs.excludeST && strings.Contains(strings.ToUpper(name), "ST") {
			continue
		}
		if rs.requirePositiveEPS && eps.Float64 <= 0 {
			continue
		}
		if rs.requirePositiveCFPS && cfps.Float64 <= 0 {
			continue
		}
		if rs.minAmountYuan > 0 && amount.Float64 < rs.minAmountYuan {
			continue
		}
		if rs.maxDebtRatio > 0 && debt.Valid && debt.Float64 > rs.maxDebtRatio {
			continue
		}
		if rs.maxGoodwillRatio > 0 && goodwill.Valid && goodwill.Float64 > rs.maxGoodwillRatio {
			continue
		}
		annROE := roe.Float64 * mult
		if rs.minAnnROE > 0 && annROE < rs.minAnnROE {
			continue
		}
		if rs.profitYoYMin != 0 && profitYoY.Float64 < rs.profitYoYMin {
			continue
		}
		if rs.profitYoYMax > 0 && profitYoY.Float64 > rs.profitYoYMax {
			continue
		}
		// 低基数噪音过滤：净利同比>500% 多为去年基数极小的扭亏，不是可持续景气
		if rs.preset == "boom" && profitYoY.Float64 > 500 {
			continue
		}
		// ROE年化>150% 基本是单季一次性损益噪音，剔除
		if annROE > 150 {
			continue
		}
		if profitYoY.Float64 == 0 && !profitYoY.Valid {
			continue
		}
		if revYoY.Float64 < rs.revYoYMin {
			continue
		}
		if rs.minGrossMargin > 0 && gm.Float64 < rs.minGrossMargin {
			continue
		}

		passed := []string{}
		if rs.minAnnROE > 0 {
			passed = append(passed, fmt.Sprintf("ROE年化%.1f%%", annROE))
		}
		passed = append(passed, fmt.Sprintf("净利+%.1f%%", profitYoY.Float64), fmt.Sprintf("营收+%.1f%%", revYoY.Float64))
		if rs.requirePositiveCFPS {
			passed = append(passed, "现金流正")
		}

		// 打分：增速封顶(避免低基数霸榜)，ROE/营收/毛利综合
		capG := func(v float64) float64 {
			if v > 100 {
				return 100
			}
			return v
		}
		score := annROE + capG(profitYoY.Float64)*0.3 + capG(revYoY.Float64)*0.2 + gm.Float64*0.1
		if cfps.Float64 > 0 {
			score += 5
		}
		res.Candidates = append(res.Candidates, models.FundamentalCandidate{
			Symbol: code, Name: name, Price: round2(close.Float64), MarketCapYi: round2(cap.Float64 / 1e8),
			AnnROE: round2(annROE), RevYoY: round2(revYoY.Float64), ProfitYoY: round2(profitYoY.Float64),
			GrossMargin: round2(gm.Float64), CFPS: round2(cfps.Float64), EPS: round2(eps.Float64),
			AmountYi: round2(amount.Float64 / 1e8), DebtRatio: round2(debt.Float64),
			GoodwillRatio: round2(goodwill.Float64), DividendYield: round2(divYield.Float64),
			Score: round2(score), Passed: passed,
		})
	}
	rows.Close()

	// 估值历史分位过滤(近250日市值分位代理PE分位)——在候选已较少时逐只算
	if rs.maxValuationPctile > 0 {
		dates, _ := s.recentTradeDates(250)
		since := ""
		if len(dates) > 0 {
			since = dates[0]
		}
		kept := res.Candidates[:0]
		for _, c := range res.Candidates {
			pct := s.marketCapPercentile(c.Symbol, c.MarketCapYi*1e8, since)
			c.ValPctile = round2(pct)
			if pct > rs.maxValuationPctile {
				continue
			}
			if pct >= 0 {
				c.Passed = append(c.Passed, fmt.Sprintf("估值分位%.0f%%", pct))
			}
			kept = append(kept, c)
		}
		res.Candidates = kept
	}

	sort.Slice(res.Candidates, func(i, j int) bool { return res.Candidates[i].Score > res.Candidates[j].Score })
	if len(res.Candidates) > 100 {
		res.Candidates = res.Candidates[:100]
	}
	return res
}

type emBalanceRow struct {
	SecurityCode  string   `json:"SECURITY_CODE"`
	DebtRatio     *float64 `json:"DEBT_ASSET_RATIO"`
	Equity        *float64 `json:"TOTAL_EQUITY"`
}
type emBalanceResp struct {
	Success bool `json:"success"`
	Result  struct {
		Pages int            `json:"pages"`
		Data  []emBalanceRow `json:"data"`
	} `json:"result"`
}

// FetchAndStoreBalance 批量拉资产负债表，回填 stock_fundamentals 的 debt_ratio/equity（资产负债率/净资产）。
func (s *HistoryService) FetchAndStoreBalance(reportDate string) (int, string) {
	if s == nil || s.db == nil {
		return 0, "历史库未就绪"
	}
	client := &http.Client{Timeout: 25 * time.Second}
	const pageSize = 500
	updated := 0
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "开启事务失败：" + err.Error()
	}
	stmt, err := tx.Prepare(`UPDATE stock_fundamentals SET debt_ratio=?, equity=? WHERE stock_code=? AND report_date=?`)
	if err != nil {
		_ = tx.Rollback()
		return 0, "预编译失败：" + err.Error()
	}
	for page := 1; page <= 30; page++ {
		api := fmt.Sprintf("https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=RPT_DMSK_FN_BALANCE&columns=SECURITY_CODE,DEBT_ASSET_RATIO,TOTAL_EQUITY&pageSize=%d&pageNumber=%d&sortColumns=NOTICE_DATE&sortTypes=-1&source=WEB&client=WEB&filter=%s",
			pageSize, page, url.QueryEscape(fmt.Sprintf("(REPORT_DATE='%s')", reportDate)))
		req, _ := http.NewRequest("GET", api, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Referer", "https://data.eastmoney.com/")
		resp, err := client.Do(req)
		if err != nil {
			_ = tx.Rollback()
			return updated, "请求失败：" + err.Error()
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var pr emBalanceResp
		if err := json.Unmarshal(body, &pr); err != nil {
			_ = tx.Rollback()
			return updated, "解析失败：" + err.Error()
		}
		if !pr.Success || len(pr.Result.Data) == 0 {
			break
		}
		for _, r := range pr.Result.Data {
			code := normalizeFundamentalCode(r.SecurityCode)
			if code == "" {
				continue
			}
			if res, err := stmt.Exec(nf(r.DebtRatio), nf(r.Equity), code, reportDate); err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					updated++
				}
			}
		}
		if page >= pr.Result.Pages {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return updated, "提交失败：" + err.Error()
	}
	return updated, ""
}

// screenFundamentalAt 时点选股(无未来函数)：用 reportDate 那期年报 + asOfDate 当时价格/市值，按 preset 规则筛 topN。
func (s *HistoryService) screenFundamentalAt(rs fundamentalRuleSet, reportDate, asOfDate string, topN int) []string {
	tradeDate, err := s.ResolveTradeDateAtOrBefore(asOfDate)
	if err != nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT f.stock_code, f.stock_name, f.roe, f.rev_yoy, f.profit_yoy, f.gross_margin, f.cfps, f.eps, d.total_market_cap, d.amount
		FROM stock_fundamentals f LEFT JOIN stock_daily d ON d.stock_code=f.stock_code AND d.trade_date=?
		WHERE f.report_date=?`, tradeDate, reportDate)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type cc struct {
		code string
		cap  float64
		sc   float64
	}
	var cands []cc
	for rows.Next() {
		var code string
		var name sql.NullString
		var roe, revYoY, profitYoY, gm, cfps, eps, cap, amount sql.NullFloat64
		if rows.Scan(&code, &name, &roe, &revYoY, &profitYoY, &gm, &cfps, &eps, &cap, &amount) != nil {
			continue
		}
		if rs.excludeST && strings.Contains(strings.ToUpper(name.String), "ST") {
			continue
		}
		if rs.requirePositiveEPS && eps.Float64 <= 0 {
			continue
		}
		if rs.requirePositiveCFPS && cfps.Float64 <= 0 {
			continue
		}
		if rs.minAmountYuan > 0 && amount.Float64 < rs.minAmountYuan {
			continue
		}
		annROE := roe.Float64 // 年报ROE即全年，不年化
		if rs.minAnnROE > 0 && annROE < rs.minAnnROE {
			continue
		}
		if rs.profitYoYMin != 0 && profitYoY.Float64 < rs.profitYoYMin {
			continue
		}
		if rs.profitYoYMax > 0 && profitYoY.Float64 > rs.profitYoYMax {
			continue
		}
		if rs.preset == "boom" && profitYoY.Float64 > 500 {
			continue
		}
		if annROE > 150 || revYoY.Float64 < rs.revYoYMin {
			continue
		}
		if rs.minGrossMargin > 0 && gm.Float64 < rs.minGrossMargin {
			continue
		}
		capG := func(v float64) float64 {
			if v > 100 {
				return 100
			}
			return v
		}
		sc := annROE + capG(profitYoY.Float64)*0.3 + capG(revYoY.Float64)*0.2 + gm.Float64*0.1
		cands = append(cands, cc{code, cap.Float64, sc})
	}
	// 估值分位过滤(用 asOfDate 前的市值序列)
	if rs.maxValuationPctile > 0 {
		since := yearBefore(tradeDate)
		out := cands[:0]
		for _, c := range cands {
			if p := s.marketCapPercentile(c.code, c.cap, since); p < 0 || p <= rs.maxValuationPctile {
				out = append(out, c)
			}
		}
		cands = out
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].sc > cands[j].sc })
	if len(cands) > topN {
		cands = cands[:topN]
	}
	codes := make([]string, 0, len(cands))
	for _, c := range cands {
		codes = append(codes, c.code)
	}
	return codes
}

func yearBefore(d string) string {
	if len(d) < 10 {
		return d
	}
	t, err := time.Parse("2006-01-02", d[:10])
	if err != nil {
		return d
	}
	return t.AddDate(-1, 0, 0).Format("2006-01-02")
}

// basketReturn 等权篮子从 start 到 end 的净收益(扣双边成本)，以及有效只数。
func (s *HistoryService) basketReturn(codes []string, start, end string) (float64, int) {
	sd, _ := s.ResolveTradeDateAtOrBefore(start)
	ed, _ := s.ResolveTradeDateAtOrBefore(end)
	const buy, sell = 0.00125, 0.00225
	sum, n := 0.0, 0
	for _, code := range codes {
		var c0, c1 sql.NullFloat64
		s.db.QueryRow(`SELECT close_price FROM stock_daily WHERE stock_code=? AND trade_date>=? ORDER BY trade_date LIMIT 1`, code, sd).Scan(&c0)
		s.db.QueryRow(`SELECT close_price FROM stock_daily WHERE stock_code=? AND trade_date<=? ORDER BY trade_date DESC LIMIT 1`, code, ed).Scan(&c1)
		if c0.Float64 > 0 && c1.Float64 > 0 {
			sum += (c1.Float64*(1-sell)/(c0.Float64*(1+buy)) - 1) * 100
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// RunFundamentalRollingBacktest 基本面滚动逐年回测：每年用上年年报筛篮子、持有1年，看篮子超额是否穿越年份。
func (s *HistoryService) RunFundamentalRollingBacktest(preset string) models.FundamentalRollingResult {
	rs := fundamentalPreset(preset)
	res := models.FundamentalRollingResult{Preset: rs.preset, PresetLabel: rs.label}
	if s == nil || s.db == nil {
		res.Warning = "历史库未就绪"
		return res
	}
	var latest string
	s.db.QueryRow(`SELECT MAX(trade_date) FROM stock_daily`).Scan(&latest)
	windows := []struct{ label, report, start, end string }{
		{"2024年度(用2023年报)", "2023-12-31", "2024-05-06", "2025-05-06"},
		{"2025年度(用2024年报)", "2024-12-31", "2025-05-06", "2026-05-06"},
		{"2026至今(用2025年报)", "2025-12-31", "2026-05-06", latest},
	}
	for _, w := range windows {
		codes := s.screenFundamentalAt(rs, w.report, w.start, 20)
		if len(codes) == 0 {
			res.Periods = append(res.Periods, models.FundamentalRollingPeriod{Label: w.label, ReportDate: w.report, StartDate: w.start, EndDate: w.end})
			continue
		}
		bret, n := s.basketReturn(codes, w.start, w.end)
		mlevel, mdates, _ := s.EqualWeightIndexBetween(w.start, w.end)
		mret := 0.0
		if len(mdates) > 1 {
			mret = (mlevel[mdates[len(mdates)-1]]/mlevel[mdates[0]] - 1) * 100
		}
		names := codes
		if len(names) > 6 {
			names = names[:6]
		}
		res.Periods = append(res.Periods, models.FundamentalRollingPeriod{
			Label: w.label, ReportDate: w.report, StartDate: w.start, EndDate: w.end,
			BasketCount: n, BasketReturn: round2(bret), MarketReturn: round2(mret),
			Alpha: round2(bret - mret), TopNames: names,
		})
	}
	return res
}

// marketCapPercentile 当前市值在该股近窗口市值序列中的分位(%)。低=相对自身历史便宜，代理PE历史分位。
// 数据不足(<60个交易日)返回 -1。
func (s *HistoryService) marketCapPercentile(code string, currentCap float64, since string) float64 {
	if currentCap <= 0 {
		return -1
	}
	var total, below int
	row := s.db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN total_market_cap < ? THEN 1 ELSE 0 END)
		FROM stock_daily WHERE stock_code=? AND trade_date>=? AND total_market_cap>0`, currentCap, code, since)
	var belowN interface{}
	if err := row.Scan(&total, &belowN); err != nil || total < 60 {
		return -1
	}
	if v, ok := belowN.(int64); ok {
		below = int(v)
	}
	return float64(below) / float64(total) * 100
}

// FetchAndStoreGoodwill 批量拉商誉报表，回填 goodwill_ratio(商誉占净资产%)到最新报告期行(按 code 更新)。
func (s *HistoryService) FetchAndStoreGoodwill(reportDate string) (int, string) {
	return s.fetchBulkUpdate("RPT_GOODWILL_STOCKDETAILS",
		"SECURITY_CODE,GOODWILL,SUMSHEQUITY", reportDate,
		`UPDATE stock_fundamentals SET goodwill_ratio=? WHERE stock_code=? AND report_date=(SELECT MAX(report_date) FROM stock_fundamentals)`,
		func(m map[string]any) (string, []interface{}, bool) {
			code := normalizeFundamentalCode(asString(m["SECURITY_CODE"]))
			gw, eq := asFloat(m["GOODWILL"]), asFloat(m["SUMSHEQUITY"])
			if code == "" {
				return "", nil, false
			}
			ratio := 0.0
			if eq > 0 {
				ratio = gw / eq * 100
			}
			return code, []interface{}{ratio, code}, true
		})
}

// FetchAndStoreDividend 批量拉分红报表，回填 dividend_yield(股息率%)。同股多条取最新一条。
func (s *HistoryService) FetchAndStoreDividend(reportDate string) (int, string) {
	seen := map[string]bool{}
	return s.fetchBulkUpdate("RPT_SHAREBONUS_DET",
		"SECURITY_CODE,DIVIDENT_RATIO,NOTICE_DATE", reportDate,
		`UPDATE stock_fundamentals SET dividend_yield=? WHERE stock_code=? AND report_date=(SELECT MAX(report_date) FROM stock_fundamentals)`,
		func(m map[string]any) (string, []interface{}, bool) {
			code := normalizeFundamentalCode(asString(m["SECURITY_CODE"]))
			if code == "" || seen[code] {
				return "", nil, false
			}
			seen[code] = true
			return code, []interface{}{asFloat(m["DIVIDENT_RATIO"]), code}, true
		})
}

// fetchBulkUpdate 通用：分页拉某 eastmoney 报表，逐行经 build 生成 UPDATE 参数执行。
func (s *HistoryService) fetchBulkUpdate(reportName, cols, reportDate, updateSQL string,
	build func(map[string]any) (string, []interface{}, bool)) (int, string) {
	if s == nil || s.db == nil {
		return 0, "历史库未就绪"
	}
	client := &http.Client{Timeout: 25 * time.Second}
	const pageSize = 500
	updated := 0
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "开启事务失败：" + err.Error()
	}
	stmt, err := tx.Prepare(updateSQL)
	if err != nil {
		_ = tx.Rollback()
		return 0, "预编译失败：" + err.Error()
	}
	for page := 1; page <= 30; page++ {
		api := fmt.Sprintf("https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=%s&columns=%s&pageSize=%d&pageNumber=%d&sortColumns=NOTICE_DATE&sortTypes=-1&source=WEB&client=WEB&filter=%s",
			reportName, cols, pageSize, page, url.QueryEscape(fmt.Sprintf("(REPORT_DATE='%s')", reportDate)))
		req, _ := http.NewRequest("GET", api, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Referer", "https://data.eastmoney.com/")
		resp, err := client.Do(req)
		if err != nil {
			_ = tx.Rollback()
			return updated, "请求失败：" + err.Error()
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var pr struct {
			Success bool `json:"success"`
			Result  struct {
				Pages int              `json:"pages"`
				Data  []map[string]any `json:"data"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &pr); err != nil {
			_ = tx.Rollback()
			return updated, "解析失败：" + err.Error()
		}
		if !pr.Success || len(pr.Result.Data) == 0 {
			break
		}
		for _, m := range pr.Result.Data {
			if _, args, ok := build(m); ok {
				if res, err := stmt.Exec(args...); err == nil {
					if n, _ := res.RowsAffected(); n > 0 {
						updated++
					}
				}
			}
		}
		if page >= pr.Result.Pages {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return updated, "提交失败：" + err.Error()
	}
	return updated, ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func asFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func normalizeFundamentalCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return ""
	}
	switch {
	case strings.HasPrefix(code, "60"), strings.HasPrefix(code, "68"): // 沪市主板/科创板
		return "sh" + code
	case strings.HasPrefix(code, "00"), strings.HasPrefix(code, "30"): // 深市主板/创业板
		return "sz" + code
	default: // 北交(8)/新三板(4,9) 等剔除
		return ""
	}
}

func nf(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func datePartShort(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
