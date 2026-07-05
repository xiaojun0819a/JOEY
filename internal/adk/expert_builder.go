package adk

import (
	"fmt"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/adk/mcp"
	"github.com/run-bigpig/jcp/internal/adk/tools"
	"github.com/run-bigpig/jcp/internal/models"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ExpertAgentBuilder 专家 Agent 构建器
type ExpertAgentBuilder struct {
	llm          model.LLM
	aiConfig     *models.AIConfig // AI 配置（包含 temperature、maxTokens）
	toolRegistry *tools.Registry
	mcpManager   *mcp.Manager
}

// NewExpertAgentBuilder 创建专家 Agent 构建器
func NewExpertAgentBuilder(llm model.LLM, aiConfig *models.AIConfig) *ExpertAgentBuilder {
	return &ExpertAgentBuilder{llm: llm, aiConfig: aiConfig}
}

// NewExpertAgentBuilderWithTools 创建带工具的专家 Agent 构建器
func NewExpertAgentBuilderWithTools(llm model.LLM, aiConfig *models.AIConfig, registry *tools.Registry) *ExpertAgentBuilder {
	return &ExpertAgentBuilder{llm: llm, aiConfig: aiConfig, toolRegistry: registry}
}

// NewExpertAgentBuilderFull 创建完整配置的专家 Agent 构建器
func NewExpertAgentBuilderFull(llm model.LLM, aiConfig *models.AIConfig, registry *tools.Registry, mcpMgr *mcp.Manager) *ExpertAgentBuilder {
	return &ExpertAgentBuilder{llm: llm, aiConfig: aiConfig, toolRegistry: registry, mcpManager: mcpMgr}
}

// BuildAgentWithContext 根据配置构建 LLM Agent（支持引用上下文）
func (b *ExpertAgentBuilder) BuildAgentWithContext(config *models.AgentConfig, stock *models.Stock, query string, replyContent string, coreContext string, position *models.StockPosition) (agent.Agent, error) {
	instruction := b.buildInstructionWithContext(config, stock, query, replyContent, coreContext, position)

	// 获取 Agent 配置的工具
	var agentTools []tool.Tool
	if b.toolRegistry != nil && len(config.Tools) > 0 {
		agentTools = b.toolRegistry.GetTools(config.Tools)
	}

	// 获取 MCP toolsets
	var toolsets []tool.Toolset
	if b.mcpManager != nil && len(config.MCPServers) > 0 {
		log.Info("Agent %s 请求 MCP servers: %v", config.ID, config.MCPServers)
		toolsets = b.mcpManager.GetToolsetsByIDs(config.MCPServers)
		log.Info("Agent %s 获取到 %d 个 toolsets", config.ID, len(toolsets))
		// 打印每个 toolset 的名称
		for i, ts := range toolsets {
			log.Info("Agent %s toolset[%d]: %s", config.ID, i, ts.Name())
		}
	}

	// 构建生成配置（应用 temperature 和 maxTokens）
	var generateConfig *genai.GenerateContentConfig
	if b.aiConfig != nil {
		temp := float32(b.aiConfig.Temperature)
		generateConfig = &genai.GenerateContentConfig{
			Temperature: &temp,
		}
		if b.aiConfig.MaxTokens > 0 {
			generateConfig.MaxOutputTokens = int32(b.aiConfig.MaxTokens)
		}
	}

	return llmagent.New(llmagent.Config{
		Name:                  config.ID,
		Model:                 b.llm,
		Description:           config.Role,
		Instruction:           instruction,
		Tools:                 agentTools,
		Toolsets:              toolsets,
		GenerateContentConfig: generateConfig,
	})
}

// buildInstructionWithContext 构建 Agent 指令（支持引用上下文）
func (b *ExpertAgentBuilder) buildInstructionWithContext(config *models.AgentConfig, stock *models.Stock, query string, replyContent string, coreContext string, position *models.StockPosition) string {
	baseInstruction := config.Instruction
	if baseInstruction == "" {
		baseInstruction = fmt.Sprintf("你是一位%s，名字是%s。", config.Role, config.Name)
	}

	// 构建可用工具说明
	toolsDescription := b.buildToolsDescription(config)

	// 获取当前时间和盘中状态
	now := time.Now()
	timeStr := now.Format("2006-01-02 15:04:05")
	weekday := now.Weekday()
	hour, minute := now.Hour(), now.Minute()
	currentMinutes := hour*60 + minute

	// 判断盘中状态（A股交易时间：9:30-11:30, 13:00-15:00，周一至周五）
	var marketStatus string
	if weekday == time.Saturday || weekday == time.Sunday {
		marketStatus = "休市（周末）"
	} else if currentMinutes >= 9*60+30 && currentMinutes <= 11*60+30 {
		marketStatus = "盘中（上午交易时段）"
	} else if currentMinutes >= 13*60 && currentMinutes <= 15*60 {
		marketStatus = "盘中（下午交易时段）"
	} else if currentMinutes < 9*60+30 {
		marketStatus = "盘前"
	} else if currentMinutes > 15*60 {
		marketStatus = "盘后"
	} else {
		marketStatus = "午间休市"
	}

	prompt := fmt.Sprintf(`%s
%s
当前时间: %s
市场状态: %s

## 工具调用规范
当你需要调用工具时，必须通过系统提供的标准 function call 机制进行调用。
**重要：需要调用工具时，不要在工具调用前输出任何思考过程或分析文字，直接发起工具调用。工具返回结果后，再基于结果组织你的回答。**
禁止在回复文本中输出任何自定义的工具调用标签，包括但不限于：
- <tool_call>、</tool_call>
- <tool_call_begin>、</tool_call_end>
- <invoke>、</invoke>
- <tool>、</tool>
- 任何类似 <xxx:tool_call> 格式的标签
直接使用 API 提供的 tool_calls 功能，不要在文本中模拟工具调用。

股票: %s (%s)
当前价格: %.2f
涨跌幅: %.2f%%
`, baseInstruction, toolsDescription, timeStr, marketStatus, stock.Symbol, stock.Name, stock.Price, stock.ChangePercent)

	// 如果有持仓信息，加入上下文
	if position != nil && position.Shares > 0 {
		marketValue := float64(position.Shares) * stock.Price
		costAmount := float64(position.Shares) * position.CostPrice
		profitLoss := marketValue - costAmount
		profitPercent := 0.0
		if costAmount > 0 {
			profitPercent = (profitLoss / costAmount) * 100
		}
		prompt += fmt.Sprintf(`
用户持仓: %d股，成本价 %.2f
持仓市值: %.2f，盈亏: %.2f (%.2f%%)
`, position.Shares, position.CostPrice, marketValue, profitLoss, profitPercent)
	}

	if strings.TrimSpace(coreContext) != "" {
		prompt += fmt.Sprintf(`
【核心数据包】
%s
`, coreContext)
	}

	// 如果有引用内容，加入上下文
	if replyContent != "" {
		prompt += fmt.Sprintf(`--- 引用的观点 ---
%s
---

你的分析任务: %s

请结合以上引用的观点，发表你的专业看法。可以赞同、补充或反驳。回复控制在150字以内。`, replyContent, query)
	} else if isFullReportTask(query) {
		prompt += fmt.Sprintf(`你的分析任务: %s

请按任务要求输出完整报告，不要压缩，不受150字限制；关键数字必须具体，数据不可得就明确写“数据不可得”。`, query)
	} else {
		prompt += fmt.Sprintf(`你的分析任务: %s

请用简洁专业的语言回答，控制在150字以内。`, query)
	}

	return prompt
}

func isFullReportTask(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	return strings.Contains(q, "完整版看板报告") ||
		strings.Contains(q, "完整报告") ||
		strings.Contains(q, "不受150字限制") ||
		strings.Contains(q, "不要压缩") ||
		strings.Contains(q, "完整框架输出")
}

// buildToolsDescription 构建可用工具说明
func (b *ExpertAgentBuilder) buildToolsDescription(config *models.AgentConfig) string {
	var searchTools []string // 搜索类工具
	var dataTools []string   // 数据查询工具
	var otherTools []string  // 其他工具

	// 搜索类工具关键词
	searchKeywords := []string{"search", "搜索", "web", "网页", "tavily", "google", "bing"}

	// 获取内置工具信息并分类
	if b.toolRegistry != nil && len(config.Tools) > 0 {
		toolInfos := b.toolRegistry.GetToolInfosByNames(config.Tools)
		for _, info := range toolInfos {
			desc := fmt.Sprintf("- %s: %s", info.Name, info.Description)
			if b.isSearchTool(info.Name, info.Description, searchKeywords) {
				searchTools = append(searchTools, desc)
			} else if b.isDataTool(info.Name) {
				dataTools = append(dataTools, desc)
			} else {
				otherTools = append(otherTools, desc)
			}
		}
	}

	// 获取 MCP 工具信息并分类
	if b.mcpManager != nil && len(config.MCPServers) > 0 {
		mcpTools := b.mcpManager.GetToolInfosByServerIDs(config.MCPServers)
		for _, info := range mcpTools {
			desc := fmt.Sprintf("- %s: %s (来自 %s)", info.Name, info.Description, info.ServerName)
			if b.isSearchTool(info.Name, info.Description, searchKeywords) {
				searchTools = append(searchTools, desc)
			} else if b.isDataTool(info.Name) {
				dataTools = append(dataTools, desc)
			} else {
				otherTools = append(otherTools, desc)
			}
		}
	}

	if len(searchTools) == 0 && len(dataTools) == 0 && len(otherTools) == 0 {
		return ""
	}

	return b.formatToolsInstruction(searchTools, dataTools, otherTools)
}

// isSearchTool 判断是否为搜索类工具
func (b *ExpertAgentBuilder) isSearchTool(name, description string, keywords []string) bool {
	nameLower := strings.ToLower(name)
	descLower := strings.ToLower(description)
	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) || strings.Contains(descLower, kw) {
			return true
		}
	}
	return false
}

// isDataTool 判断是否为数据查询工具
func (b *ExpertAgentBuilder) isDataTool(name string) bool {
	dataKeywords := []string{"kline", "k线", "realtime", "实时", "orderbook", "盘口", "news", "新闻"}
	nameLower := strings.ToLower(name)
	for _, kw := range dataKeywords {
		if strings.Contains(nameLower, kw) {
			return true
		}
	}
	return false
}

// formatToolsInstruction 格式化工具使用指导
func (b *ExpertAgentBuilder) formatToolsInstruction(searchTools, dataTools, otherTools []string) string {
	var result strings.Builder

	result.WriteString("\n## 工具使用规则（必须遵守）\n\n")

	// 搜索工具 - 强制使用
	if len(searchTools) > 0 {
		result.WriteString("### 搜索工具（遇到信息查询必须调用）\n")
		for _, t := range searchTools {
			result.WriteString(t + "\n")
		}
		result.WriteString("\n**重要**: 当用户询问新闻、事件、公告、研报、市场动态等信息时，")
		result.WriteString("你**必须先调用搜索工具**获取最新信息，**禁止凭记忆回答**。\n\n")
	}

	// 数据工具
	if len(dataTools) > 0 {
		result.WriteString("### 数据查询工具\n")
		for _, t := range dataTools {
			result.WriteString(t + "\n")
		}
		result.WriteString("\n")
	}

	// 其他工具
	if len(otherTools) > 0 {
		result.WriteString("### 其他工具\n")
		for _, t := range otherTools {
			result.WriteString(t + "\n")
		}
		result.WriteString("\n")
	}

	// 通用指导
	result.WriteString("### 工具调用原则\n")
	result.WriteString("1. 需要实时数据时，必须调用工具，不要编造数据\n")
	result.WriteString("2. 搜索类工具优先用于获取最新信息\n")
	result.WriteString("3. 工具返回结果后再组织回答\n")

	return result.String()
}
