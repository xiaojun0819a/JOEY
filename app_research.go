package main

// 投研报告(机构级深度诊断报告V5.0)：用户提供的旗舰版提示词驱动，异步生成(15-25分钟)，
// 产出 Markdown 正文 + Word(.doc) 文件。任务态在内存，成品落库(board_reports 表, period="research-v5")
// + 文件落盘(<dataDir>/reports/)，重启后仍可回显与下载。

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/adk/openai"
	"github.com/run-bigpig/jcp/internal/meeting"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/services"
)

const researchReportTimeout = 25 * time.Minute // 整体参考值(前端提示用)
const researchPartTimeout = 12 * time.Minute   // 单段生成超时;报告分两段(1-8章/9-16章)生成再拼接——模型单次稳定输出上限约6-8千字,一口气1.2万字必被压缩成摘要
const researchReportPeriodKey = "research-v5" // 落库用的 period 标识

// ResearchReportStatus 投研报告任务状态(供前端轮询)
type ResearchReportStatus struct {
	Status      string `json:"status"` // idle | running | done | failed
	StockCode   string `json:"stockCode"`
	StockName   string `json:"stockName"`
	Report      string `json:"report"`
	FileName    string `json:"fileName"` // Word 文件名(下载用)
	FilePath    string `json:"filePath"` // 服务端绝对路径(本地模式打开用)
	Error       string `json:"error"`
	ModelName   string `json:"modelName"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	ElapsedSec  int    `json:"elapsedSec"`
}

type researchJob struct {
	mu     sync.Mutex
	status ResearchReportStatus
	start  time.Time
}

var (
	researchJobsMu sync.Mutex
	researchJobs   = map[string]*researchJob{}
)

func researchReportsDir() string {
	dir := filepath.Join(paths.GetDataDir(), "reports")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func researchReportFileName(stockName, stockCode string) string {
	name := strings.TrimSpace(stockName)
	if name == "" {
		name = stockCode
	}
	// Word 能直接打开 HTML 内容的 .doc；文件名保持用户要求的口径
	return fmt.Sprintf("%s%s_机构级深度诊断报告V5.0_旗舰增强版.doc", name, stockCode)
}

// StartResearchReport 启动异步生成。已在跑则直接返回当前进度(幂等)。
func (a *App) StartResearchReport(stockCode, stockName string) ResearchReportStatus {
	stockCode = strings.TrimSpace(stockCode)
	if stockCode == "" {
		return ResearchReportStatus{Status: "failed", Error: "stockCode 不能为空"}
	}

	researchJobsMu.Lock()
	if job, ok := researchJobs[stockCode]; ok {
		job.mu.Lock()
		st := job.status
		job.mu.Unlock()
		if st.Status == "running" {
			researchJobsMu.Unlock()
			st.ElapsedSec = int(time.Since(job.start).Seconds())
			return st
		}
	}
	job := &researchJob{start: time.Now()}
	job.status = ResearchReportStatus{
		Status:    "running",
		StockCode: stockCode,
		StockName: stockName,
		StartedAt: job.start.Format("2006-01-02 15:04:05"),
	}
	researchJobs[stockCode] = job
	researchJobsMu.Unlock()

	go a.runResearchReport(job, stockCode, stockName)

	st := job.status
	return st
}

// GetResearchReport 查询任务状态；无内存任务时回落到落库缓存(重启后回显)。
func (a *App) GetResearchReport(stockCode string) ResearchReportStatus {
	stockCode = strings.TrimSpace(stockCode)
	researchJobsMu.Lock()
	job, ok := researchJobs[stockCode]
	researchJobsMu.Unlock()
	if ok {
		job.mu.Lock()
		st := job.status
		job.mu.Unlock()
		if st.Status == "running" {
			st.ElapsedSec = int(time.Since(job.start).Seconds())
		}
		return st
	}

	// 回落：查落库缓存
	if a.paperService != nil {
		if rec, err := a.paperService.GetBoardReport(stockCode, researchReportPeriodKey); err == nil && rec != nil && rec.Report != "" {
			fileName := researchReportFileName(rec.StockName, stockCode)
			filePath := filepath.Join(researchReportsDir(), fileName)
			if _, err := os.Stat(filePath); err != nil {
				// 文件丢了就按 md 重建一份
				_ = os.WriteFile(filePath, []byte(markdownToWordHTML(strings.TrimSuffix(fileName, ".doc"), rec.Report)), 0644)
			}
			return ResearchReportStatus{
				Status: "done", StockCode: stockCode, StockName: rec.StockName,
				Report: rec.Report, FileName: fileName, FilePath: filePath,
				ModelName: rec.ModelName, FinishedAt: rec.GeneratedAt,
			}
		}
	}
	return ResearchReportStatus{Status: "idle", StockCode: stockCode}
}

// GetResearchReportHTML 返回报告的完整 HTML(与 Word 文件同源)。
// 前端用 iframe 展示——流式 markdown 渲染器(markstream)对 4 万字大文档会渲染空白,iframe+成品 HTML 是确定性方案。
func (a *App) GetResearchReportHTML(stockCode string) string {
	st := a.GetResearchReport(stockCode)
	if st.Status != "done" || st.Report == "" {
		return ""
	}
	title := strings.TrimSuffix(st.FileName, ".doc")
	return markdownToWordHTML(title, st.Report)
}

func (job *researchJob) set(update func(*ResearchReportStatus)) {
	job.mu.Lock()
	update(&job.status)
	job.mu.Unlock()
}

func (a *App) runResearchReport(job *researchJob, stockCode, stockName string) {
	fail := func(msg string) {
		job.set(func(s *ResearchReportStatus) {
			s.Status = "failed"
			s.Error = msg
			s.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
			s.ElapsedSec = int(time.Since(job.start).Seconds())
		})
		rt_logf("投研报告失败 %s: %s", stockCode, msg)
	}

	config := a.configService.GetConfig()
	aiConfig := a.getDefaultAIConfig(config)
	if aiConfig == nil {
		fail("未配置AI服务")
		return
	}
	// 不抬 maxTokens：网关按"输入+max_tokens≤窗口"校验，抬高输出上限反而必撞
	// "input exceeds context window"(16384/12288 两档实测都炸)；看板报告用默认 8192 能产出 1.28 万字，够用。
	boosted := *aiConfig

	stocks, _ := a.marketService.GetStockRealTimeData(stockCode)
	var stock models.Stock
	if len(stocks) > 0 {
		stock = stocks[0]
	}
	if stock.Symbol == "" {
		stock.Symbol = stockCode
	}
	if strings.TrimSpace(stock.Name) == "" {
		stock.Name = stockName
	}
	if strings.TrimSpace(stockName) == "" {
		stockName = stock.Name
	}
	job.set(func(s *ResearchReportStatus) { s.StockName = stockName })

	agentCfg, ok := a.findBoardReportAgent()
	if !ok {
		fail("未找到老陈基本面分析师配置")
		return
	}
	agentCfg.AIConfigID = "" // 强制走 boosted 配置(抬高了 maxTokens)
	// 关键：把 V5.0 人设放进 system(替换老陈人设)，避免老陈"精炼输出"的风格把深度报告带成几百字摘要
	agentCfg.Name = "投研组"
	agentCfg.Role = "机构级深度报告小组"
	agentCfg.Instruction = researchReportPersona

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	position := a.sessionService.GetPosition(stockCode)
	coreContext := a.buildCoreContext(stockCode, stock, position)

	genPart := func(part int) (string, error) {
		ctx, cancel := context.WithTimeout(baseCtx, researchPartTimeout)
		defer cancel()
		// 每段只挂该段需要的工具:20个工具的返回全堆进上下文会超模型窗口(实测 input exceeds context window)
		partCfg := agentCfg
		partCfg.Tools = researchPartTools(part)
		chatReq := meeting.ChatRequest{
			StockCode:    stockCode,
			Stock:        stock,
			Agents:       []models.AgentConfig{partCfg},
			Query:        buildResearchReportQuery(stock, part),
			CoreContext:  coreContext,
			Position:     position,
			AgentTimeout: researchPartTimeout,
			// 长输出必须流式，否则撞 AI 网关约5分钟的非流式响应硬超时
			Progress: func(meeting.ProgressEvent) {},
		}
		responses, err := a.meetingService.SendMessage(ctx, &boosted, chatReq)
		if err != nil {
			return "", err
		}
		for _, resp := range responses {
			if strings.TrimSpace(resp.Content) != "" {
				return strings.TrimSpace(openai.FilterVendorToolCallMarkers(resp.Content)), nil
			}
			if strings.TrimSpace(resp.Error) != "" {
				return "", fmt.Errorf("%s", resp.Error)
			}
		}
		return "", fmt.Errorf("模型未返回内容")
	}

	var parts [2]string
	for i := 1; i <= 2; i++ {
		content, err := genPart(i)
		if err == nil && len([]rune(content)) < 1500 {
			rt_logf("投研报告第%d部分过短(%d字),重试一次", i, len([]rune(content)))
			if c2, e2 := genPart(i); e2 == nil && len([]rune(c2)) > len([]rune(content)) {
				content = c2
			}
		}
		if err != nil {
			fail(fmt.Sprintf("第%d部分生成失败: %v", i, err))
			return
		}
		if len([]rune(content)) < 1500 {
			fail(fmt.Sprintf("第%d部分仍过短(%d字),模型未按要求输出完整章节", i, len([]rune(content))))
			return
		}
		parts[i-1] = content
		rt_logf("投研报告 %s 第%d/2部分完成: %d 字", stockCode, i, len([]rune(content)))
	}
	report := strings.TrimSpace(parts[0] + "\n\n" + parts[1])

	// 产出 Word 文件(HTML-in-.doc，Word/WPS 可直接打开)
	fileName := researchReportFileName(stockName, stockCode)
	filePath := filepath.Join(researchReportsDir(), fileName)
	title := strings.TrimSuffix(fileName, ".doc")
	if err := os.WriteFile(filePath, []byte(markdownToWordHTML(title, report)), 0644); err != nil {
		fail("写Word文件失败: " + err.Error())
		return
	}

	finishedAt := time.Now().Format("2006-01-02 15:04:05")
	// 落库缓存(与看板报告同表，period 区分)，重启后可回显
	if a.paperService != nil {
		if err := a.paperService.SaveBoardReport(services.BoardReportRecord{
			StockCode:   stockCode,
			Period:      researchReportPeriodKey,
			StockName:   stockName,
			Report:      report,
			AgentID:     agentCfg.ID,
			AgentName:   agentCfg.Name,
			ModelName:   boosted.ModelName,
			GeneratedAt: finishedAt,
		}); err != nil {
			rt_logf("缓存投研报告失败 %s: %v", stockCode, err)
		}
	}

	job.set(func(s *ResearchReportStatus) {
		s.Status = "done"
		s.Report = report
		s.FileName = fileName
		s.FilePath = filePath
		s.ModelName = boosted.ModelName
		s.FinishedAt = finishedAt
		s.ElapsedSec = int(time.Since(job.start).Seconds())
	})
	rt_logf("投研报告完成 %s(%s): %d 字, 耗时 %ds", stockName, stockCode, len([]rune(report)), int(time.Since(job.start).Seconds()))
}

func rt_logf(format string, args ...interface{}) { log.Info(format, args...) }

// researchReportPersona 放进 agent system 提示词(替换老陈人设)。
// 教训：把整套 V5.0 塞进用户消息时，老陈 system 里的"精炼输出"风格会赢——68秒吐264字摘要收工。
// 人设+硬性篇幅纪律必须占住 system 位。
const researchReportPersona = `你是一个"买方机构研究员 + 游资情绪交易员 + 量化策略分析师"三合一的深度研报小组，专职产出机构级深度诊断报告(V5.0 旗舰增强版)。

你的输出纪律(最高优先级，覆盖一切默认行为)：
1. 你产出的是完整长篇研报，不是摘要、不是提纲、不是快评。目标篇幅不少于 12000 字，必须写满全部 16 章直到"第十六章"结束才允许停笔。
2. 严禁"已完成工具检索。结论：…"这类几百字收工的行为——那是不合格交付。
3. 动笔前先调用挂载的工具取数(每个工具最多调用一次，节约上下文)，基于真实返回写作；工具取不到的数据明确写"公开资料未充分披露"，不许编。
4. 每一章遵循：数据事实 → 逻辑分析 → 未来推演 → 风险提示 → 投研结论；关键数据标注来源(工具/口径/截至时间)。
5. 输出纯 Markdown(# 一级标题、## 二级标题、Markdown 表格)；不要输出任何关于文件生成/下载的说明。
6. 语言中文，机构研报风格但普通投资者能看懂；敢给结论但不假装确定；所有交易建议带风险前提。
7. 严禁：只有框架、只有结论、"值得关注"式模糊收尾、忽略风险、虚构数据、凑字数空话、把概念当业绩、把短线涨停当长期逻辑、把可能性写成已兑现。`

// researchPartTools 每段的工具集(各8个精选)。实测13个/段仍爆窗:模型会把每个工具都调一遍,
// 5年估值趋势/10篇研报/全量热榜这类大payload叠加后超过通道上下文。剔除大数据量工具,只留写作必需。
func researchPartTools(part int) []string {
	if part == 1 {
		return []string{
			"get_stock_realtime", "get_f10_business", "get_f10_financials", "get_f10_main_indicators",
			"get_f10_valuation", "get_f10_industry_compare", "get_research_report", "get_news",
		}
	}
	return []string{
		"get_stock_realtime", "get_kline_data", "get_f10_fund_flow", "get_chip_distribution",
		"get_longhubang", "get_f10_shareholder_numbers", "get_f10_shareholder_changes", "get_f10_valuation",
	}
}

// buildResearchReportQuery 下达具体任务(章节结构)。人设与纪律在 system(researchReportPersona)。
// part=1 交付第一~七章,part=2 交付第八~十六章——模型单次稳定输出上限约6-8千字,分段才能拿到完整深度;
// 分界放在第八章(资金博弈)是因为它开始需要龙虎榜/筹码/资金流工具,与后面的技术面同一工具组。
func buildResearchReportQuery(stock models.Stock, part int) string {
	name := strings.TrimSpace(stock.Name)
	code := strings.TrimSpace(stock.Symbol)
	var sb strings.Builder
	// 注意措辞必须含 expert_builder.isFullReportTask 的暗号词("完整报告"/"不要压缩"/"不受150字限制"),
	// 否则查询会被包上"控制在150字以内"的尾巴(实测模型直接拒写长文)
	sb.WriteString(fmt.Sprintf("任务：为【%s】【%s】生成《%s%s_机构级深度诊断报告V5.0_旗舰增强版》——这是完整报告任务，不要压缩，不受150字限制。\n", name, code, name, code))
	if part == 1 {
		sb.WriteString("本报告分两次交付，本次交付【前半部分：第一章到第七章】。先调用挂载的工具取最新数据(每个工具最多一次；get_research_report 的 pageSize 传 3，get_news 的 limit 传 5，控制数据量)，然后连续输出第一章到第七章(本部分不少于6000字)，写到第七章结束即停，不要写第八章之后的内容。\n\n")
	} else {
		sb.WriteString("本报告分两次交付，前半部分(第一~七章)已完成。本次交付【后半部分：第八章到第十六章】。先调用挂载的工具取数(每个工具最多一次；get_kline_data 取日K days=120 即可，控制数据量)，然后直接从「# 第八章」开始连续输出到第十六章(本部分不少于6000字)，不要重复前七章内容。\n\n")
	}
	sb.WriteString(`一、报告总要求
1. 必须先调用工具获取最新数据(行情、K线、财务、估值趋势、主营业务、同行对比、研报、资金流、龙虎榜、筹码、公告、新闻、热度)，再写作；禁止凭记忆。
2. 必须使用真实数据，不得虚构。
3. 工具取不到、无法确认的数据，必须明确写“公开资料未充分披露”或“暂未查到权威来源”，不能硬编。
4. 报告正文篇幅要求充实(目标不少于12000字)，每一章必须有足够分析密度，不能只有标题和一两句话。
5. 每一章遵循：数据事实 → 逻辑分析 → 未来推演 → 风险提示 → 投研结论。
6. 关键数据必须标注来源(哪个工具/什么口径/截至时间)。
7. 输出格式为纯 Markdown：# 一级标题、## 二级标题、Markdown 表格；不要输出任何关于文件生成/下载的说明(Word 排版由系统完成)。
8. 语言中文，风格偏机构研报，但要让普通投资者也能看懂。
9. 最后必须给出明确操作结论：重点出击 / 重点关注 / 观察池 / 谨慎 / 放弃。

二、报告结构(严格按以下章节，标题编号保持一致)

# 第一章：投资摘要
股票名称/代码/行业/核心概念、市值与估值、最新财务表现、核心投资逻辑、最大预期差、最大风险点、鱼身指数、主升浪概率、综合评级、游资一句话结论。
必须给出总览表：
| 项目 | 结论 |
|---|---|
| 股票名称 | |
| 股票代码 | |
| 核心主线 | |
| 当前阶段 | 鱼头/鱼身/鱼尾 |
| 鱼身指数 | |
| 主升浪概率 | |
| 风险等级 | 低/中/高 |
| 操作评级 | 重点出击/重点关注/观察池/谨慎/放弃 |

# 第二章：公司全景画像
发展历史、主营结构、控股股东与实控人、核心子公司、核心客户、产品与应用场景、转型路径、市场主流认知、真实基本面与认知的预期差。必须判断：这是传统制造股、周期股、成长股、题材股，还是正在主业重估的转型股？

# 第三章：产业链全景分析
拆上游(原材料/设备/供应链约束/卡脖子)、中游(公司环节/核心产品/技术壁垒/产能/认证周期/客户导入难度)、下游(应用场景/客户景气度/是否受益AI、算力、半导体、新能源、军工、机器人等主线)。
必须回答：行业是不是未来方向？公司是不是核心受益者？龙头/二线/跟风？行业空间能否支撑市值扩张？

# 第四章：行业竞争格局与同行对比
至少选3-5家同行，对比市值/营收/净利/毛利率/净利率/ROE/估值/壁垒/客户/成长性/资金关注度/题材纯度。
必须生成同行对比表：
| 公司 | 主营方向 | 市值 | 营收 | 净利润 | 毛利率 | PE | 核心优势 | 核心短板 |
|---|---|--:|--:|--:|--:|--:|---|---|
最后给出：行业位置、是否细分龙头、有无估值重估空间、和最强同行差在哪、和普通同行强在哪。

# 第五章：财务三年深度拆解
覆盖近三年+最新一期：营收、归母净利、扣非、毛利率、净利率、ROE、负债率、经营现金流、存货、应收、合同负债、资本开支、研发/销售/管理费用。
必须生成财务趋势表：
| 年份 | 营收 | 同比 | 归母净利润 | 同比 | 毛利率 | 净利率 | 经营现金流 |
|---|--:|--:|--:|--:|--:|--:|--:|
重点分析：收入是真增长还是并表/一次性？利润是主营改善还是非经常性？现金流配不配利润？应收/存货风险？毛利率改善？业绩拐点？最新一期是否验证趋势？

# 第六章：成长性与业绩拐点分析
增长来自哪里？老业务修复还是新业务放量？新业务有没有收入利润贡献？有没有订单/产能/客户/合同支撑？0到1、1到10还是10到100？利润弹性？第二增长曲线？
三情景推演表：
| 情景 | 假设 | 营收变化 | 利润变化 | 估值影响 |
|---|---|--:|--:|---|

# 第七章：主线题材热度系统
分析公司涉及的全部题材(AI算力/半导体/消费电子/先进封装/军工/机器人/新能源/数据中心/液冷/国产替代/新材料等)。不能沾边就给高分。
判断：核心还是边缘？业绩题材还是概念题材？已兑现还是预期？主线还是支线？有无政策/产业新闻/研报催化？游资炒作空间？
题材热度评分表：
| 题材 | 相关度 | 兑现程度 | 市场热度 | 评分 |
|---|---|---|---|--:|

# 第八章：机构与资金博弈分析
龙虎榜、游资/机构席位、主力资金5/10/20日、融资融券、基金持仓、前十大股东、股东户数、筹码集中度、成交额、换手率、是否短线过热。
判断：机构趋势资金还是游资短炒？主升初期放量还是尾部放量？资金是否持续？一致性过强风险？

# 第九章：技术面与鱼身定位
周线/日线/月线趋势、均线系统、MACD、量能、箱体突破、前高压力、下方支撑、筹码峰、缩量回踩/放量突破/高位加速。
按鱼身模型判断(鱼头=刚启动逻辑未确认高风险高赔率；鱼身=趋势确认资金持续业绩题材共振最适合参与；鱼尾=连续加速全民讨论放量滞涨风险大于收益)。
必须输出：
| 阶段 | 判断 |
|---|---|
| 当前阶段 | 鱼头/鱼身/鱼尾 |
| 所处位置 | 前段/中段/后段 |
| 参与价值 | 高/中/低 |
| 追高风险 | 高/中/低 |

# 第十章：V5.0鱼身量化评分模型
按权重打分(总分100)：产业逻辑15、财务质量15、成长性15、题材热度20、资金认可20、技术趋势10、鱼身位置5。
评级标准：≥90重点出击；80-89重点关注；70-79观察池；60-69谨慎；<60放弃。
必须逐项解释为什么给这个分数，不能只给分。

# 第十一章：AI概率预测模块
| 周期 | 上涨概率 | 震荡概率 | 下跌概率 | 核心依据 |
|---|--:|--:|--:|---|
| 1个月 | | | | |
| 3个月 | | | | |
| 6个月 | | | | |
概率是基于当前公开数据的主观量化结果，不是确定性预测；必须提示不确定性；必须结合基本面/资金面/技术面/情绪。

# 第十二章：交易计划卡
三套方案(激进/平衡/保守)，每套含首仓条件、加仓条件、止损条件、止盈条件、仓位上限、不参与条件：
| 类型 | 首仓 | 加仓 | 止损 | 止盈 | 适合人群 |
|---|---|---|---|---|---|
必须强调：不追高，不满仓，不赌单日涨跌，以“回踩低吸+突破确认+板块共振”为核心。

# 第十三章：风险矩阵
至少8项(行业/政策/业绩不及预期/估值过高/资金退潮/客户集中/应收/存货/新业务/技术路线/减持/系统性)：
| 风险 | 等级 | 触发条件 | 应对策略 |
|---|---|---|---|

# 第十四章：估值与目标区间推演
至少两种估值方法(PE/PS/PEG/分部/可比)，按公司类型选择：
| 情景 | 核心假设 | 合理估值区间 | 对应逻辑 |
|---|---|---|---|
不给绝对保证式目标价；必须写“估值区间仅供研究，不构成投资建议”。

# 第十五章：最终结论
值不值得重点跟踪？短线交易还是中线趋势？最大看点？最大风险？适合追涨、低吸还是等待？一句话总结这家公司是什么？
必须输出三条一句话结论：
游资一句话结论：“……”
机构一句话结论：“……”
普通投资者一句话结论：“……”

# 第十六章：参考资料与数据来源
列出所有关键数据来源(哪个工具、什么口径、截至时间)。

三、写作风格要求
不要AI摘要腔；每章分析密度要够；多用表格；多用“核心判断/风险点/后续验证指标”；像买方研究员但不过度晦涩；敢给结论但不假装确定；所有交易建议带风险前提。

四、特别禁止
不要简版；不要只有框架；不要只有结论；不要“值得关注”式模糊收尾；不要忽略风险；不要虚构数据；不要凑字数空话；不要把概念当业绩；不要把短线涨停当长期逻辑；不要把可能性写成已兑现。

`)
	q := sb.String()
	// 按 part 裁掉不属于本次交付的章节说明
	if part == 1 {
		if i := strings.Index(q, "# 第八章"); i >= 0 {
			if j := strings.Index(q, "三、写作风格要求"); j > i {
				q = q[:i] + q[j:]
			}
		}
		q += "\n最后再次强调：这是完整报告任务，不要压缩，不受150字限制。现在从「# 第一章：投资摘要」连续写到「# 第七章：主线题材热度系统」结束，本部分不少于6000字。几百字的摘要视为任务失败。\n"
	} else {
		if i := strings.Index(q, "# 第一章"); i >= 0 {
			if j := strings.Index(q, "# 第八章"); j > i {
				q = q[:i] + q[j:]
			}
		}
		q += "\n最后再次强调：这是完整报告任务，不要压缩，不受150字限制。现在直接从「# 第八章：机构与资金博弈分析」连续写到「# 第十六章：参考资料与数据来源」结束，本部分不少于6000字。几百字的摘要视为任务失败。\n"
	}
	return q
}

// ---------- Markdown → Word(HTML .doc) ----------

var (
	mdBoldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalicRe = regexp.MustCompile(`\*([^*]+)\*`)
	mdCodeRe   = regexp.MustCompile("`([^`]+)`")
)

func mdInline(s string) string {
	s = html.EscapeString(s)
	s = mdBoldRe.ReplaceAllString(s, "<b>$1</b>")
	s = mdItalicRe.ReplaceAllString(s, "<i>$1</i>")
	s = mdCodeRe.ReplaceAllString(s, "<code>$1</code>")
	return s
}

func isTableSepLine(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	t = strings.Trim(t, "|")
	for _, cell := range strings.Split(t, "|") {
		c := strings.TrimSpace(cell)
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func splitTableRow(s string) []string {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	cells := strings.Split(t, "|")
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.TrimSpace(c)
	}
	return out
}

// markdownToWordHTML 把报告 Markdown 渲染成 Word 可直接打开的 HTML(.doc)。
// 覆盖研报所需子集：标题/表格/列表/引用/分隔线/加粗斜体行内代码/代码块/段落。
func markdownToWordHTML(title, md string) string {
	var b strings.Builder
	b.WriteString("<html xmlns:o='urn:schemas-microsoft-com:office:office' xmlns:w='urn:schemas-microsoft-com:office:word'><head><meta charset='utf-8'><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title><style>
body{font-family:"DengXian","Microsoft YaHei",SimSun,sans-serif;font-size:11pt;line-height:1.7;color:#1a1a1a;margin:2.2cm;}
h1{font-size:20pt;color:#0b3a6b;border-bottom:2px solid #0b3a6b;padding-bottom:6px;margin-top:28px;}
h2{font-size:15pt;color:#0b3a6b;margin-top:22px;}
h3{font-size:12.5pt;color:#14508f;margin-top:16px;}
table{border-collapse:collapse;width:100%;margin:10px 0;font-size:10pt;}
th,td{border:1px solid #999;padding:5px 8px;text-align:left;}
th{background:#eaf1f9;font-weight:bold;}
blockquote{border-left:4px solid #b7cbe3;margin:8px 0;padding:4px 12px;color:#555;background:#f5f8fc;}
pre{background:#f4f4f4;border:1px solid #ddd;padding:8px;font-size:9.5pt;white-space:pre-wrap;}
hr{border:none;border-top:1px solid #bbb;margin:16px 0;}
.cover{text-align:center;margin-top:180px;}
.cover h1{border:none;font-size:26pt;}
.cover .meta{margin-top:30px;color:#555;font-size:12pt;}
.pagebreak{page-break-before:always;}
</style></head><body>`)

	// 封面页
	b.WriteString("<div class='cover'><h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1><div class='meta'>机构级深度诊断报告 V5.0 · 旗舰增强版<br>生成时间：")
	b.WriteString(time.Now().Format("2006-01-02 15:04"))
	b.WriteString("<br>本报告由 AI 基于公开数据生成，仅供研究参考，不构成投资建议</div></div><div class='pagebreak'></div>\n")

	lines := strings.Split(md, "\n")
	i := 0
	inList := false
	inOList := false
	closeLists := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
		if inOList {
			b.WriteString("</ol>\n")
			inOList = false
		}
	}
	for i < len(lines) {
		line := lines[i]
		t := strings.TrimSpace(line)

		// 代码块
		if strings.HasPrefix(t, "```") {
			closeLists()
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			i++ // 跳过结尾 ```
			b.WriteString("<pre>" + html.EscapeString(strings.Join(code, "\n")) + "</pre>\n")
			continue
		}
		// 表格
		if strings.HasPrefix(t, "|") && i+1 < len(lines) && isTableSepLine(lines[i+1]) {
			closeLists()
			header := splitTableRow(t)
			b.WriteString("<table><tr>")
			for _, h := range header {
				b.WriteString("<th>" + mdInline(h) + "</th>")
			}
			b.WriteString("</tr>\n")
			i += 2
			for i < len(lines) {
				rt := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(rt, "|") {
					break
				}
				b.WriteString("<tr>")
				for _, c := range splitTableRow(rt) {
					b.WriteString("<td>" + mdInline(c) + "</td>")
				}
				b.WriteString("</tr>\n")
				i++
			}
			b.WriteString("</table>\n")
			continue
		}
		switch {
		case t == "":
			closeLists()
		case strings.HasPrefix(t, "#### "):
			closeLists()
			b.WriteString("<h3>" + mdInline(t[5:]) + "</h3>\n")
		case strings.HasPrefix(t, "### "):
			closeLists()
			b.WriteString("<h3>" + mdInline(t[4:]) + "</h3>\n")
		case strings.HasPrefix(t, "## "):
			closeLists()
			b.WriteString("<h2>" + mdInline(t[3:]) + "</h2>\n")
		case strings.HasPrefix(t, "# "):
			closeLists()
			b.WriteString("<h1>" + mdInline(t[2:]) + "</h1>\n")
		case t == "---" || t == "***" || t == "___":
			closeLists()
			b.WriteString("<hr>\n")
		case strings.HasPrefix(t, "> "):
			closeLists()
			b.WriteString("<blockquote>" + mdInline(t[2:]) + "</blockquote>\n")
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			if inOList {
				b.WriteString("</ol>\n")
				inOList = false
			}
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>" + mdInline(t[2:]) + "</li>\n")
		case regexp.MustCompile(`^\d+[.、]\s*`).MatchString(t):
			if inList {
				b.WriteString("</ul>\n")
				inList = false
			}
			if !inOList {
				b.WriteString("<ol>\n")
				inOList = true
			}
			b.WriteString("<li>" + mdInline(regexp.MustCompile(`^\d+[.、]\s*`).ReplaceAllString(t, "")) + "</li>\n")
		default:
			closeLists()
			b.WriteString("<p>" + mdInline(t) + "</p>\n")
		}
		i++
	}
	closeLists()
	b.WriteString("</body></html>")
	return b.String()
}
