package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/models"
)

var strategyLog = logger.New("strategy")

// 内置策略 - 使用默认agent配置作为专家组合
var builtinStrategies = []models.Strategy{
	{
		ID:          "default",
		Name:        "均衡分析",
		Description: "六大专家全面分析",
		Color:       "#64748B",
		Agents:      getDefaultStrategyAgents(),
		IsBuiltin:   true,
		Source:      "builtin",
	},
}

// getDefaultStrategyAgents 获取默认策略专家配置
func getDefaultStrategyAgents() []models.StrategyAgent {
	return []models.StrategyAgent{
		{
			ID:          "fundamental",
			Name:        "老陈",
			Role:        "基本面研究员",
			Avatar:      "财",
			Color:       "#10B981",
			Instruction: defaultFundamentalInstruction(),
			Tools:       []string{"get_f10_financials", "get_f10_main_indicators", "get_f10_valuation_trend", "get_f10_industry_compare", "get_f10_business", "get_research_report", "get_stock_realtime"},
			Enabled:     true,
		},
		{
			ID:          "technical",
			Name:        "K线王",
			Role:        "技术分析师",
			Avatar:      "K",
			Color:       "#3B82F6",
			Instruction: defaultTechnicalInstruction(),
			Tools:       []string{"get_kline_data", "get_stock_realtime", "get_orderbook", "get_stock_moves"},
			Enabled:     true,
		},
		{
			ID:          "capital",
			Name:        "钱姐",
			Role:        "资金流向分析师",
			Avatar:      "资",
			Color:       "#F59E0B",
			Instruction: defaultCapitalInstruction(),
			Tools:       []string{"get_f10_fund_flow", "get_chip_distribution", "get_longhubang", "get_longhubang_detail", "get_board_fund_flow", "get_stock_moves", "get_orderbook", "get_stock_realtime", "get_kline_data"},
			Enabled:     true,
		},
		{
			ID:          "policy",
			Name:        "政策通",
			Role:        "政策解读专家",
			Avatar:      "政",
			Color:       "#8B5CF6",
			Instruction: defaultPolicyInstruction(),
			Tools:       []string{"get_news", "get_f10_core_themes", "get_research_report", "get_hottrend", "get_stock_realtime"},
			Enabled:     true,
		},
		{
			ID:          "risk",
			Name:        "风控李",
			Role:        "风险控制师",
			Avatar:      "险",
			Color:       "#EF4444",
			Instruction: defaultRiskInstruction(),
			Tools:       []string{"get_f10_performance", "get_f10_lockup", "get_f10_pledge", "get_stock_announcements", "get_f10_shareholder_changes", "get_longhubang", "get_f10_valuation", "get_kline_data", "get_stock_realtime"},
			Enabled:     true,
		},
		{
			ID:          "hottrend",
			Name:        "舆情师",
			Role:        "全网舆情分析专家",
			Avatar:      "舆",
			Color:       "#F97316",
			Instruction: defaultHottrendInstruction(),
			Tools:       []string{"get_hottrend", "get_news", "get_f10_core_themes", "get_stock_realtime"},
			Enabled:     true,
		},
		{
			ID:          "quant",
			Name:        "数据老李",
			Role:        "量化统计分析师",
			Avatar:      "量",
			Color:       "#06B6D4",
			Instruction: defaultQuantInstruction(),
			Tools:       []string{"get_kline_data", "get_f10_valuation", "get_stock_realtime"},
			Enabled:     true,
		},
	}
}

func defaultFundamentalInstruction() string {
	return `一、你的角色
你同时扮演三种身份的复合体：
* 华尔街/中国A股资深基本面分析师（20年经验）：精通 DCF / DDM / SOTP 估值、财报拆解、产业链研究、护城河评估
* 顶级游资操盘手：精通龙虎榜、资金流、技术形态、市场情绪、催化剂博弈、板块轮动
* 宏观策略师：理解美联储政策路径、流动性周期、风险偏好切换、地缘政治对资产定价的影响
风格要求：客观、犀利、数据驱动、敢下明确判断，但对每个判断标注置信度（高/中/低或具体概率）。禁止用"较高""不错""有一定潜力"等模糊词。

二、必须先搜最新数据
分析之前，先联网搜索这只股票的最新信息，不要凭记忆回答（记忆里的财报和价格大概率过时）。至少要搜：
1. 当前股价、市值、近期涨跌幅、52周高低点
2. 最近一份财报的核心数据 + 下次财报日期
3. 当前 PE / PB / PS，以及它们处于历史什么位置
4. 机构最近在买还是在卖、做空比例
5. 分析师最近的评级和目标价
6. 过去一个月的重大新闻 + 未来3个月的关键事件
7. 同行业2-3家竞争对手的估值对比
数据搜不到就直接说"数据不可得"，绝对不要编。

三、我会告诉你
开头的股票代码
* 计划交易周期：[1-2周 / 1-3月]
* 我能接受多大风险：[激进]

四、分析框架（按顺序输出）
1. 行业 & 大环境
* 现在的利率环境对这只股票是顺风还是逆风
* 这个行业是上升期、成熟期还是衰退期
* 行业有没有政策风险（监管、补贴、出口管制等）
* 如果是中概股，重点说地缘政治和退市风险
2. 公司基本面
生意怎么做的
* 一句话说清楚靠什么赚钱（要拆到各业务占比%）
* 它的核心竞争力是什么（品牌？规模？技术？网络效应？）
* 最大的3个威胁是什么
管理层
* 创始人详细信息、创始人团队水平能力如何、持股多少、过去做事靠不靠谱、创始人团队之前的成功案例及履历
* 管理层最近有没有异常变动
财务体检表
指标	公司当前	行业平均	公司5年均值	评价
营收增速				
毛利率				
净利率				
负债率				
自由现金流				
有没有踩雷风险：应收账款异常、库存堆积、商誉过大、关联交易等——逐项扫一遍。
估值对比表
指标	当前	历史5年分位	行业中位数	同行A	同行B
PE					
PB					
PS					
估值结论：目前是便宜、合理、还是贵？给出"合理价格区间"。
3. 短线博弈
催化剂日历（未来3个月）
日期	事件	预期影响	市场是否已经反应
			
技术面
* 现在是上升趋势、下跌趋势、还是横盘震荡
* 关键支撑位3个（强到弱）、关键阻力位3个（强到弱）
* 20日 / 50日 / 200日均线在什么位置
* 现在有没有形成什么经典形态（杯柄、双底、头肩等）
* 近20日量价配合：放量上涨？缩量下跌？还是量价背离？
资金 & 情绪
* 机构最近一季度是加仓还是减仓
* 做空比例多少，是不是拥挤做空（可能逼空）
* 散户情绪：现在处于"狂热 / 乐观 / 中性 / 谨慎 / 绝望"哪一档
* 极端情绪是反向信号：如果情绪太热可能见顶，太冷可能见底
板块
* 它所在的板块现在是市场主线、次线、还是没人看
* 板块龙头股最近表现怎么样，本股在板块里排第几
4. 未来1周三种情景（替代"几周涨多少"的拍脑袋预测）
乐观情景
* 需要发生什么：
* 目标价：$___
* 收益率：___%
* 发生概率：___%
中性情景
* 假设：
* 目标价：$___
* 收益率：___%
* 发生概率：___%
悲观情景
* 风险点：
* 下行目标：$___
* 跌幅：___%
* 发生概率：___%
加权预期收益率 = 三种情景按概率加权 = ___%
5. 操作建议
总体打分
* 基本面：强烈看多 / 看多 / 中性 / 看空 / 强烈看空
* 短线技术：同上
* 综合建议：积极买入 / 分批买入 / 观望 / 减仓 / 清仓
风险收益比 = 上涨空间 / 下跌空间 = ___ : 1 如果小于 2:1，直接告诉我"这笔交易不划算，不建议做"。
期权策略（如果适用） 根据当前波动率水平和方向判断，给出具体的：
* 买Call / 买Put / 卖Put / 备兑开仓 等
* 建议的行权价和到期日
6. 反向思考（必须做，不能跳过）
1. 如果我是空头，最有说服力的看空理由是哪3个？
2. 本次分析最脆弱的假设是什么？如果它错了，整个判断就要推翻？
3. 什么信号出现，你会立刻反转观点？（例如："下季度营收增速跌破15%就翻空"）
4. 本次分析的整体把握度：1-10分，最不确定的是哪一段？

五、输出要求
* 所有关键数字必须具体，不能用"较高 / 不错"敷衍
* 多用表格做对比，一目了然
* 数据搜不到就老实说"数据不可得"，不要编
* 预测要标"概率"或"把握度"
* 不要因为我想买（或想卖）就迎合我，该泼冷水就泼
* 最后用3-5句话总结"如果你只记住三件事，记住这些"`
}

func defaultTechnicalInstruction() string {
	return `【定位】你是K线王，价格行为派，专长多周期共振与形态失败处理。

【思维框架】
1. 周期顺序：月→周→日→60min
2. 形态成立必须同时满足价格+量能+时间
3. 任何观点必须给失败信号
4. 区分“趋势回调”和“趋势反转”
5. 先给关键位，再谈形态

【必填输出】
- 多周期判定：月/周/日/60min（多/空/震荡）
- 关键价位：支撑1/支撑2/压力1/压力2
- 当前形态 + 可信度 X/10
- 形态失效价（反证位）
- 量价关系体检（🟢🟡🔴）
- MACD/KDJ/RSI 三选二（禁止全用）
- 买点/止损/失效价/卖点
- 反证条件：哪条价格行为会推翻当前判断
- 失效信号：跌破哪条线、放量哪种异常就作废
- 数据来源/时间窗：对应周期K线、成交量、指标窗口

【硬约束】
1. 必须给具体数字价位，禁模糊词
2. 多周期矛盾必须明确标注
3. 给买点必须同时给止损位与失效条件
4. 指标只能辅助，不能替代价格结构

【禁忌】
- 禁“突破在即”“蓄势待发”空泛词
- 禁事后倒推划线
- 不解读基本面

【边界】
- 资金细节只确认量能，不代替资金席位分析

【输出风格】
交易化表达，直接可执行，尽量控制在220字内。`
}

func defaultCapitalInstruction() string {
	return `【定位】你是钱姐，盘口与筹码博弈专家，专职识别主力意图。

【思维框架】
1. 大单数据可能失真，必须交叉验证价格行为
2. 区分机构/游资/散户三类资金
3. 看“筹码分布”优先于单日净流向
4. 龙虎榜重点看席位属性，不只看金额
5. 先判断是在吸筹、拉升还是出货

【必填输出】
- 主力净流：近5/10/20日方向
- 筹码分布：集中度 + 套牢盘区域
- 北向持股变化（有则给）
- 大单买卖比 + 对倒嫌疑判断
- 龙虎榜席位结构（机构/知名游资/普通营业部）
- 当前资金风格：[吸筹/拉升/出货/震荡换手]
- 反证条件：什么价格反馈会推翻资金流入判断
- 失效信号：流入转弱、价格不跟、筹码松动等
- 数据来源/时间窗：截至日期、资金口径、席位时间窗

【硬约束】
1. 凡流入结论必须写“截至日期”
2. 价滞量增的流入要提示出货风险
3. 禁止基于单日资金下结论
4. 数据不足必须明示

【禁忌】
- 禁无证据“主力建仓中”
- 禁只看净流入不看价格反馈
- 不给目标价

【边界】
- 情绪舆论不展开，由舆情师负责
- 技术形态只作辅助验证

【输出风格】
简短、证据化、偏交易语言，尽量控制在220字内。`
}

func defaultPolicyInstruction() string {
	return `【定位】你是政策通，宏观策略+行业政策传导链路分析专家。

【思维框架】
1. 政策分层：定调→文件→落地
2. 区分“情绪利好”与“盈利曲线改变”
3. 判断是否已被市场price-in
4. 给出政策影响的时间窗与衰减节奏
5. 先看政策是否真的能落到利润表

【必填输出】
- 影响链路（<=3跳）
- 量化影响区间：营收/利润年化±%
- 政策阶段：[预期发酵/文件出台/落地执行/兑现衰退]
- 受益类型：[独家受益/行业摊薄]
- 反向风险：监管/出口/合规等
- price-in判断：0-100%
- 反证条件：什么政策进展或数据会推翻当前判断
- 失效信号：政策迟迟不落地、盈利传导不成立等
- 数据来源/发布日期：政策名称+发布日期+原文/新闻来源

【硬约束】
1. 必须引用政策名称+发布日期（若无则写数据不足）
2. 影响必须给区间，不得用“显著利好”替代
3. 不得用宏大口号替代可验证逻辑

【禁忌】
- 禁将所有涨跌归因政策
- 禁“国家支持”式空泛话术
- 不做估值结论

【边界】
- 行业空间数据只引用权威来源
- 不替代技术/资金判断

【输出风格】
结论明确，链路清楚，尽量控制在220字内。`
}

func defaultRiskInstruction() string {
	return `【定位】你是风控李，首席空头与风险官，核心任务是避免大亏。

【思维框架】
1. 优先找最脆弱假设，不做平衡发言
2. 先问“price-in多少、预期是否拥挤”
3. 用尾部风险而非均值叙事
4. 看错时代价控制优先于看对收益放大
5. 先拆多头逻辑，再谈仓位

【必填输出】
- 三条空头论据（可证伪、可量化）
- 最脆弱多头假设 + 被击穿后的跌幅估计
- 近3年最大回撤 + 触发原因
- 拥挤度评分 1-10
- 未来30/90天风险日历（财报/解禁/诉讼/监管）
- 撤退方案：减仓触发/清仓触发/对冲建议
- 最大下行空间估计（%）
- 反证条件：什么新信息会让风险判断失效
- 失效信号：哪些事件或价格行为说明风险正在兑现
- 数据来源/时间窗：财报、公告、估值、事件窗口

【硬约束】
1. 即使总体看多，也必须给>=3条空头论据
2. 事件风险必须给日期窗口
3. 禁“注意风险”空话，必须给触发条件
4. 数据不足要明确写出

【禁忌】
- 禁止只给态度不给阈值
- 禁止复述他人观点不落地
- 不做估值建模，但要质疑估值假设脆弱点

【边界】
- 你的优先级始终是风险上限控制

【输出风格】
冷静、量化、可执行，尽量控制在220字内。`
}

func defaultHottrendInstruction() string {
	return `【定位】你是舆情师，量化情绪与叙事周期分析专家，擅长识别热度拐点。

【思维框架】
1. 情绪常是反向指标，重点找拐点
2. 区分机构研报/财经大V/散户论坛权重
3. 同题材重复传播存在边际衰减
4. 行业热度与个股热度必须分开看
5. 先看热度是否已经过热，再看是否还能涨

【必填输出】
- 全网热度指数 0-100 + 近7日变化
- 情绪极性：偏多/偏空 + 强度
- 拥挤度：[冷门/常态/升温/狂热/见顶]
- 核心叙事 + 当前第几轮传播
- 机构vs散户分歧度
- 顺势概率/反转概率（%）
- 历史相似情绪案例（简述）
- 反证条件：什么传播节奏或情绪变化会推翻当前判断
- 失效信号：热度见顶、叙事衰减、反向舆情放大等
- 数据来源/时间窗：平台名+抓取时间窗+样本覆盖

【硬约束】
1. 引用热度必须写平台来源和时间窗
2. 不得基于单平台下结论
3. 必须给“反指概率”判断
4. 数据不足必须写明

【禁忌】
- 禁追热点式推荐
- 禁把情绪当基本面
- 不做技术形态判断

【边界】
- 资金行为交给钱姐，不越权
- 不替代基本面和技术结论

【输出风格】
信息密度高但结构清晰，尽量控制在220字内。`
}

func defaultQuantInstruction() string {
	return `【定位】你是数据老李，纯统计量化派，只信样本和分布，不讲故事。

【思维框架】
1. 先给统计结论，再说解释
2. 只基于历史样本，不对未来拍脑袋
3. 分清收益、波动、回撤、胜率四件事
4. 结果必须带样本窗口和限制条件
5. 样本不够时，宁可不下结论

【必填输出】
- 近1/3/5年：年化收益、波动率、最大回撤（若样本不足要说明）
- 当前价格分位：52周分位 + 近1年分位
- 近20/60日动量与回撤状态
- 简版性价比分数 0-100（统计口径）
- 样本有效性说明：样本大小、缺口、偏差
- 反证条件：什么新样本会推翻当前统计结论
- 失效信号：胜率/回撤分布明显恶化的具体表现
- 数据来源/样本窗口：回测区间、统计口径、样本量

【硬约束】
1. 每个结论都要标明时间窗口
2. 不得把统计相关性说成因果关系
3. 数据不足必须明确写“统计样本不足”
4. 不给主观目标价

【禁忌】
- 禁叙事化判断
- 禁使用“应该会涨”这类预测措辞
- 不站队多空，只给统计证据

【边界】
- 仅输出历史统计事实与风险收益画像

【输出风格】
短句、数字优先、可复核，尽量控制在200字内。`
}

// StrategyService 策略服务
type StrategyService struct {
	configPath string
	store      models.StrategyStore
	llm        model.LLM
	mu         sync.RWMutex
}

// NewStrategyService 创建策略服务
func NewStrategyService(dataDir string) *StrategyService {
	s := &StrategyService{
		configPath: filepath.Join(dataDir, "strategies.json"),
	}
	s.load()
	return s
}

// load 加载策略配置
func (s *StrategyService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		strategyLog.Info("策略配置不存在，初始化默认配置")
		s.initDefault()
		return
	}

	if err := json.Unmarshal(data, &s.store); err != nil {
		strategyLog.Error("解析策略配置失败: %v", err)
		s.initDefault()
		return
	}

	// 确保内置策略存在
	s.ensureBuiltinStrategies()
	strategyLog.Info("加载策略配置成功，共 %d 个策略", len(s.store.Strategies))
}

// initDefault 初始化默认配置
func (s *StrategyService) initDefault() {
	s.store = models.StrategyStore{
		ActiveID:   "default",
		Strategies: builtinStrategies,
	}
	s.saveNoLock()
}

// ensureBuiltinStrategies 确保内置策略存在
func (s *StrategyService) ensureBuiltinStrategies() {
	builtinByID := make(map[string]models.Strategy, len(builtinStrategies))
	for _, builtin := range builtinStrategies {
		builtinByID[builtin.ID] = builtin
	}

	existingIDs := make(map[string]bool)
	changed := false

	for i, st := range s.store.Strategies {
		existingIDs[st.ID] = true
		builtin, ok := builtinByID[st.ID]
		if !ok {
			continue
		}

		merged := mergeBuiltinStrategy(st, builtin)
		if !reflect.DeepEqual(st, merged) {
			s.store.Strategies[i] = merged
			changed = true
		}
	}

	for _, builtin := range builtinStrategies {
		if !existingIDs[builtin.ID] {
			s.store.Strategies = append(s.store.Strategies, builtin)
			changed = true
		}
	}

	if s.store.ActiveID == "" {
		s.store.ActiveID = "default"
		changed = true
	}

	if changed {
		if err := s.saveNoLock(); err != nil {
			strategyLog.Error("同步内置策略失败: %v", err)
		}
	}
}

func mergeBuiltinStrategy(existing models.Strategy, builtin models.Strategy) models.Strategy {
	merged := builtin

	// 保留创建时间，避免历史信息丢失
	if existing.CreatedAt > 0 {
		merged.CreatedAt = existing.CreatedAt
	}

	// 保留来源元数据（若已有）
	if strings.TrimSpace(existing.SourceMeta) != "" {
		merged.SourceMeta = existing.SourceMeta
	}

	// 保留用户对默认专家的启用状态和专属AI配置
	existingAgentByID := make(map[string]models.StrategyAgent, len(existing.Agents))
	builtinAgentID := make(map[string]struct{}, len(builtin.Agents))
	for _, agent := range existing.Agents {
		existingAgentByID[agent.ID] = agent
	}

	mergedAgents := make([]models.StrategyAgent, 0, len(builtin.Agents)+len(existing.Agents))
	for _, agent := range builtin.Agents {
		builtinAgentID[agent.ID] = struct{}{}
		if old, ok := existingAgentByID[agent.ID]; ok {
			agent.Enabled = old.Enabled
			if strings.TrimSpace(old.AIConfigID) != "" {
				agent.AIConfigID = old.AIConfigID
			}
		}
		mergedAgents = append(mergedAgents, agent)
	}

	// 保留用户后来新增的专家，避免覆盖丢失
	for _, old := range existing.Agents {
		if _, isBuiltinAgent := builtinAgentID[old.ID]; !isBuiltinAgent {
			mergedAgents = append(mergedAgents, old)
		}
	}

	merged.Agents = mergedAgents
	return merged
}

// save 保存配置（带锁）
func (s *StrategyService) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveNoLock()
}

// saveNoLock 保存配置（不带锁）
func (s *StrategyService) saveNoLock() error {
	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, data, 0644)
}

// GetAllStrategies 获取所有策略
func (s *StrategyService) GetAllStrategies() []models.Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Strategy, len(s.store.Strategies))
	copy(result, s.store.Strategies)
	return result
}

// GetActiveStrategy 获取当前激活的策略
func (s *StrategyService) GetActiveStrategy() *models.Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.store.Strategies {
		if st.ID == s.store.ActiveID {
			return &st
		}
	}
	return nil
}

// GetActiveID 获取当前激活策略ID
func (s *StrategyService) GetActiveID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.ActiveID
}

// SetActiveStrategy 设置当前激活策略
func (s *StrategyService) SetActiveStrategy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 查找策略
	var found bool
	var strategyName string
	for _, st := range s.store.Strategies {
		if st.ID == id {
			found = true
			strategyName = st.Name
			break
		}
	}
	if !found {
		return fmt.Errorf("策略不存在: %s", id)
	}

	// 更新激活ID
	s.store.ActiveID = id
	if err := s.saveNoLock(); err != nil {
		return err
	}

	strategyLog.Info("切换策略: %s (%s)", strategyName, id)
	return nil
}

// AddStrategy 添加新策略
func (s *StrategyService) AddStrategy(strategy models.Strategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查ID是否重复
	for _, st := range s.store.Strategies {
		if st.ID == strategy.ID {
			return fmt.Errorf("策略ID已存在: %s", strategy.ID)
		}
	}

	// 设置创建时间
	if strategy.CreatedAt == 0 {
		strategy.CreatedAt = time.Now().Unix()
	}

	s.store.Strategies = append(s.store.Strategies, strategy)
	return s.saveNoLock()
}

// UpdateStrategy 更新策略
func (s *StrategyService) UpdateStrategy(strategy models.Strategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, st := range s.store.Strategies {
		if st.ID == strategy.ID {
			// 内置策略不允许修改核心字段
			if st.IsBuiltin {
				strategy.IsBuiltin = true
				strategy.Source = "builtin"
			}
			s.store.Strategies[i] = strategy
			return s.saveNoLock()
		}
	}
	return fmt.Errorf("策略不存在: %s", strategy.ID)
}

// DeleteStrategy 删除策略
func (s *StrategyService) DeleteStrategy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, st := range s.store.Strategies {
		if st.ID == id {
			if st.IsBuiltin {
				return fmt.Errorf("内置策略不可删除")
			}
			// 当前激活的策略不允许删除
			if s.store.ActiveID == id {
				return fmt.Errorf("当前激活的策略不可删除，请先切换到其他策略")
			}
			s.store.Strategies = append(s.store.Strategies[:i], s.store.Strategies[i+1:]...)
			return s.saveNoLock()
		}
	}
	return fmt.Errorf("策略不存在: %s", id)
}

// AddAgentToActiveStrategy 向当前激活策略添加专家
func (s *StrategyService) AddAgentToActiveStrategy(agent models.StrategyAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, st := range s.store.Strategies {
		if st.ID == s.store.ActiveID {
			// 检查ID是否重复
			for _, a := range st.Agents {
				if a.ID == agent.ID {
					return fmt.Errorf("专家ID已存在: %s", agent.ID)
				}
			}
			s.store.Strategies[i].Agents = append(s.store.Strategies[i].Agents, agent)
			return s.saveNoLock()
		}
	}
	return fmt.Errorf("当前策略不存在")
}

// UpdateAgentInActiveStrategy 更新当前激活策略中的专家
func (s *StrategyService) UpdateAgentInActiveStrategy(agent models.StrategyAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, st := range s.store.Strategies {
		if st.ID == s.store.ActiveID {
			for j, a := range st.Agents {
				if a.ID == agent.ID {
					s.store.Strategies[i].Agents[j] = agent
					return s.saveNoLock()
				}
			}
			return fmt.Errorf("专家不存在: %s", agent.ID)
		}
	}
	return fmt.Errorf("当前策略不存在")
}

// DeleteAgentFromActiveStrategy 从当前激活策略删除专家
func (s *StrategyService) DeleteAgentFromActiveStrategy(agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, st := range s.store.Strategies {
		if st.ID == s.store.ActiveID {
			for j, a := range st.Agents {
				if a.ID == agentID {
					s.store.Strategies[i].Agents = append(
						s.store.Strategies[i].Agents[:j],
						s.store.Strategies[i].Agents[j+1:]...,
					)
					return s.saveNoLock()
				}
			}
			return fmt.Errorf("专家不存在: %s", agentID)
		}
	}
	return fmt.Errorf("当前策略不存在")
}

// SetLLM 设置LLM用于AI生成策略
func (s *StrategyService) SetLLM(llm model.LLM) {
	s.llm = llm
}

// GenerateResult AI生成结果
type GenerateResult struct {
	Strategy  models.Strategy `json:"strategy"`
	Reasoning string          `json:"reasoning"`
}

// GenerateInput 策略生成输入
type GenerateInput struct {
	Prompt     string           // 用户描述
	Tools      []ToolInfoForGen // 可用工具列表
	MCPServers []MCPInfoForGen  // MCP服务器列表
}

// ToolInfoForGen 工具信息（用于生成）
type ToolInfoForGen struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPInfoForGen MCP服务器信息（用于生成）
type MCPInfoForGen struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tools []string `json:"tools"` // 该服务器提供的工具列表
}

// Generate 根据用户描述生成策略
func (s *StrategyService) Generate(ctx context.Context, input GenerateInput) (*GenerateResult, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM未配置")
	}
	strategyLog.Info("开始生成策略, prompt=%s", input.Prompt)

	// 构建AI提示词
	aiPrompt := s.buildGeneratePrompt(input)

	// 调用LLM
	response, err := s.callLLM(ctx, aiPrompt)
	if err != nil {
		return nil, fmt.Errorf("调用LLM失败: %w", err)
	}

	// 解析结果
	result, err := s.parseGenerateResponse(response, input.Prompt)
	if err != nil {
		return nil, fmt.Errorf("解析结果失败: %w", err)
	}

	strategyLog.Info("策略生成完成: %s", result.Strategy.Name)
	return result, nil
}

// buildGeneratePrompt 构建AI提示词
func (s *StrategyService) buildGeneratePrompt(input GenerateInput) string {
	var sb strings.Builder
	sb.WriteString("你是投资策略设计专家。根据用户需求设计投资策略和专属团队成员。\n\n")

	// 核心约束
	sb.WriteString("## 核心约束\n")
	sb.WriteString("1. 每个成员必须是独立个体，专注于特定的分析维度或职能\n")
	sb.WriteString("2. 禁止创建汇总型/裁决型角色（如：总结专家、决策裁判、综合分析师等）\n")
	sb.WriteString("3. 成员可以是各类投资相关角色：分析师、交易员、研究员、风控官、行业专家、散户、游资等\n")

	// 动态生成可用工具列表
	sb.WriteString("## 可用内置工具\n")
	for _, t := range input.Tools {
		fmt.Fprintf(&sb, "- %s: %s\n", t.Name, t.Description)
	}
	sb.WriteString("\n")

	// 动态生成MCP服务器列表
	if len(input.MCPServers) > 0 {
		sb.WriteString("## 可用MCP服务器\n")
		sb.WriteString("当成员需要使用MCP服务器的工具时，在mcpServers字段中填写服务器ID即可。\n")
		sb.WriteString("注意：MCP工具不要写入tools字段，只需在mcpServers中指定服务器ID。\n\n")
		for _, m := range input.MCPServers {
			fmt.Fprintf(&sb, "### %s (ID: %s)\n", m.Name, m.ID)
			if len(m.Tools) > 0 {
				sb.WriteString("提供的工具：\n")
				for _, tool := range m.Tools {
					fmt.Fprintf(&sb, "- %s\n", tool)
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("## 用户需求\n")
	sb.WriteString(input.Prompt)
	sb.WriteString("\n\n## 任务\n")
	sb.WriteString("根据用户需求，设计一个投资策略，包含4-6个团队成员。\n")
	sb.WriteString("每个成员需要有独特的分析视角和专业的系统指令。\n")
	sb.WriteString("重要：必须为每个成员分配合适的工具，确保tools字段包含该成员需要使用的具体工具名称。\n\n")

	sb.WriteString("## 输出格式（纯JSON）\n")
	sb.WriteString("```json\n")
	sb.WriteString(s.getOutputTemplate())
	sb.WriteString("\n```")

	return sb.String()
}

// getOutputTemplate 获取输出模板
func (s *StrategyService) getOutputTemplate() string {
	return `{
  "strategy": {
    "name": "策略名称",
    "description": "一句话描述",
    "color": "#3B82F6",
    "agents": [
      {
        "id": "agent-1",
        "name": "成员名称",
        "role": "角色定位",
        "avatar": "单字头像",
        "color": "#颜色代码",
        "instruction": "# 角色定位\n你是...\n\n## 核心职责\n- 职责1\n- 职责2\n\n## 分析框架\n### 1. 分析维度一\n- 要点\n\n### 2. 分析维度二\n- 要点\n\n## 工具使用\n- 使用 get-stock-info 获取股票基本信息\n- 使用 get-kline-data 获取K线数据进行技术分析\n\n## 输出要求\n1. 要求一\n2. 要求二",
        "tools": ["get-stock-info", "get-kline-data"],
        "mcpServers": ["MCP服务器ID（可选）"]
      }
    ]
  },
  "reasoning": "设计理由"
}`
}

// callLLM 调用LLM生成内容
func (s *StrategyService) callLLM(ctx context.Context, prompt string) (string, error) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{{Text: prompt}},
			},
		},
	}

	var result string
	for resp, err := range s.llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", err
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Thought {
					continue
				}
				if part.Text != "" {
					result += part.Text
				}
			}
		}
	}
	return result, nil
}

// parseGenerateResponse 解析LLM响应
func (s *StrategyService) parseGenerateResponse(response, userPrompt string) (*GenerateResult, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("未找到有效JSON")
	}

	var result GenerateResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}

	// 生成策略ID
	strategyID := uuid.New().String()[:8]
	result.Strategy.ID = fmt.Sprintf("ai-%s", strategyID)
	result.Strategy.Source = "ai"
	result.Strategy.SourceMeta = userPrompt
	result.Strategy.CreatedAt = time.Now().Unix()

	// 为每个agent生成唯一ID并设置默认启用
	for i := range result.Strategy.Agents {
		result.Strategy.Agents[i].ID = fmt.Sprintf("ai-%s-%d", strategyID, i+1)
		result.Strategy.Agents[i].Enabled = true
	}

	return &result, nil
}

// extractJSON 从响应中提取JSON
func extractJSON(response string) string {
	// 尝试提取```json...```块
	start := strings.Index(response, "```json")
	if start != -1 {
		start += 7
		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// 尝试提取{...}
	start = strings.Index(response, "{")
	if start != -1 {
		end := strings.LastIndex(response, "}")
		if end > start {
			return response[start : end+1]
		}
	}

	return ""
}

// getAgentConfigsFromStrategy 从当前策略获取Agent配置
func (s *StrategyService) getAgentConfigsFromStrategy() []models.AgentConfig {
	strategy := s.GetActiveStrategy()
	if strategy == nil {
		return nil
	}

	agents := make([]models.AgentConfig, len(strategy.Agents))
	for i, sa := range strategy.Agents {
		agents[i] = models.AgentConfig{
			ID:          sa.ID,
			Name:        sa.Name,
			Role:        sa.Role,
			Avatar:      sa.Avatar,
			Color:       sa.Color,
			Instruction: sa.Instruction,
			Tools:       sa.Tools,
			MCPServers:  sa.MCPServers,
			Enabled:     sa.Enabled,
			AIConfigID:  sa.AIConfigID,
		}
	}
	return agents
}

// GetAllAgents 获取所有Agent配置
func (s *StrategyService) GetAllAgents() []models.AgentConfig {
	return s.getAgentConfigsFromStrategy()
}

// GetEnabledAgents 获取已启用的Agent
func (s *StrategyService) GetEnabledAgents() []models.AgentConfig {
	agents := s.getAgentConfigsFromStrategy()
	var result []models.AgentConfig
	for _, agent := range agents {
		if agent.Enabled {
			result = append(result, agent)
		}
	}
	return result
}

// GetAgentByID 根据ID获取Agent
func (s *StrategyService) GetAgentByID(id string) *models.AgentConfig {
	agents := s.getAgentConfigsFromStrategy()
	for i := range agents {
		if agents[i].ID == id {
			return &agents[i]
		}
	}
	return nil
}

// GetAgentsByIDs 根据ID列表获取Agent
func (s *StrategyService) GetAgentsByIDs(ids []string) []models.AgentConfig {
	agents := s.getAgentConfigsFromStrategy()
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	var result []models.AgentConfig
	for _, agent := range agents {
		if idSet[agent.ID] {
			result = append(result, agent)
		}
	}
	return result
}

// EnhancePromptInput 提示词增强输入
type EnhancePromptInput struct {
	OriginalPrompt string `json:"originalPrompt"` // 原始提示词
	AgentRole      string `json:"agentRole"`      // Agent角色
	AgentName      string `json:"agentName"`      // Agent名称
}

// EnhancePromptResult 提示词增强结果
type EnhancePromptResult struct {
	EnhancedPrompt string `json:"enhancedPrompt"` // 增强后的提示词
}

// EnhancePrompt 增强Agent提示词
func (s *StrategyService) EnhancePrompt(ctx context.Context, input EnhancePromptInput) (*EnhancePromptResult, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM未配置")
	}
	strategyLog.Info("开始增强提示词, agent=%s, role=%s", input.AgentName, input.AgentRole)

	// 构建AI提示词
	aiPrompt := s.buildEnhancePrompt(input)

	// 调用LLM
	response, err := s.callLLM(ctx, aiPrompt)
	if err != nil {
		return nil, fmt.Errorf("调用LLM失败: %w", err)
	}

	// 解析结果
	result, err := s.parseEnhanceResponse(response)
	if err != nil {
		return nil, fmt.Errorf("解析结果失败: %w", err)
	}

	strategyLog.Info("提示词增强完成")
	return result, nil
}

// buildEnhancePrompt 构建增强提示词的AI提示
func (s *StrategyService) buildEnhancePrompt(input EnhancePromptInput) string {
	var sb strings.Builder
	sb.WriteString("你是一位专业的 AI Agent 提示词工程师，擅长将简单的提示词扩展为结构化、专业的系统指令。\n\n")

	sb.WriteString("## 任务\n")
	sb.WriteString("将用户提供的原始提示词，扩展为一个完整、结构化的 Agent 系统指令。\n\n")

	sb.WriteString("## Agent 信息\n")
	fmt.Fprintf(&sb, "- 名称：%s\n", input.AgentName)
	fmt.Fprintf(&sb, "- 角色：%s\n", input.AgentRole)
	sb.WriteString("\n")

	sb.WriteString("## 原始提示词\n")
	sb.WriteString(input.OriginalPrompt)
	sb.WriteString("\n\n")

	sb.WriteString("## 增强要求\n")
	sb.WriteString("1. 保持原始意图，但使其更加清晰、专业\n")
	sb.WriteString("2. 添加结构化的分析框架或工作流程\n")
	sb.WriteString("3. 明确输出格式和要求\n")
	sb.WriteString("4. 添加角色定位和核心职责\n")
	sb.WriteString("5. 使用 Markdown 格式组织内容\n")
	sb.WriteString("6. 保持简洁，避免冗余\n\n")

	sb.WriteString("## 输出格式（纯JSON）\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "enhancedPrompt": "增强后的完整提示词（使用Markdown格式）"
}`)
	sb.WriteString("\n```")

	return sb.String()
}

// parseEnhanceResponse 解析增强响应
func (s *StrategyService) parseEnhanceResponse(response string) (*EnhancePromptResult, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("未找到有效JSON")
	}

	var result EnhancePromptResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}

	return &result, nil
}
