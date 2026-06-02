package meeting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/adk"
	"github.com/run-bigpig/jcp/internal/adk/mcp"
	"github.com/run-bigpig/jcp/internal/adk/openai"
	"github.com/run-bigpig/jcp/internal/adk/tools"
	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/memory"
	"github.com/run-bigpig/jcp/internal/models"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// 日志实例
var log = logger.New("Meeting")

// 超时配置常量
const (
	MeetingTimeout       = 10 * time.Minute // 整个会议的最大时长
	AgentTimeout         = 3 * time.Minute  // 单个专家发言的最大时长
	ModeratorTimeout     = 2 * time.Minute  // 小韭菜分析/总结的最大时长
	ModelCreationTimeout = 15 * time.Second // 模型创建的最大时长
)

// 重试配置常量
const (
	DefaultAIRetryCount = 2
	MaxAgentRetries     = 2                // 单个专家最大重试次数
	RetryBaseDelay      = 2 * time.Second  // 指数退避基础延迟
	RetryMaxDelay       = 15 * time.Second // 指数退避最大延迟
)

// 错误定义
var (
	ErrMeetingTimeout   = errors.New("会议超时，已返回部分结果")
	ErrModeratorTimeout = errors.New("小韭菜响应超时")
	ErrNoAIConfig       = errors.New("未配置 AI 服务")
	ErrNoAgents         = errors.New("没有可用的专家")
	ErrEmptyAgentReply  = errors.New("模型返回空内容")
)

// isRetryableError 判断错误是否可重试
// 超时、主动取消、配置错误不重试；网络错误、API 临时错误可重试
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	msg := err.Error()
	// 配置类错误不重试
	if strings.Contains(msg, "config") || strings.Contains(msg, "not found") {
		return false
	}
	return true
}

// retryRun 带指数退避的重试包装
// 在父 ctx 未取消的前提下，最多重试 maxRetries 次
func retryRun(ctx context.Context, maxRetries int, fn func() (string, error)) (string, error) {
	result, err := fn()
	if err == nil || !isRetryableError(err) {
		return result, err
	}

	var lastErr error = err
	for i := 1; i <= maxRetries; i++ {
		// 指数退避：baseDelay * 2^(i-1)，上限 RetryMaxDelay
		delay := RetryBaseDelay * time.Duration(1<<(i-1))
		if delay > RetryMaxDelay {
			delay = RetryMaxDelay
		}
		log.Warn("retry %d/%d after %v, last error: %v", i, maxRetries, delay, lastErr)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}

		result, err = fn()
		if err == nil {
			log.Info("retry %d/%d succeeded", i, maxRetries)
			return result, nil
		}
		lastErr = err
		if !isRetryableError(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("重试 %d 次后仍失败: %w", maxRetries, lastErr)
}

// AIConfigResolver AI配置解析器函数类型
// 根据 AIConfigID 返回对应的 AI 配置，如果 ID 为空或找不到则返回默认配置
type AIConfigResolver func(aiConfigID string) *models.AIConfig

// MeetingState 中断的会议状态缓存（用于失败后恢复继续执行）
type MeetingState struct {
	AIConfig       *models.AIConfig
	Stock          models.Stock
	Query          string
	Position       *models.StockPosition
	SelectedAgents []models.AgentConfig // 全部选中的专家
	History        []DiscussionEntry    // 已完成的讨论历史
	Responses      []ChatResponse       // 已完成的响应
	FailedIndex    int                  // 失败的专家在 selectedAgents 中的索引
	MemoryContext  string               // 记忆上下文
	StockMemory    *memory.StockMemory  // 股票记忆引用
	Moderator      *Moderator           // 主持人引用（用于最终总结）
	CreatedAt      time.Time            // 创建时间（用于 TTL 清理）
}

// MeetingStateTTL 中断状态缓存过期时间
const MeetingStateTTL = 10 * time.Minute

// Service 会议室服务，编排多专家并行分析
type Service struct {
	modelFactory      *adk.ModelFactory
	toolRegistry      *tools.Registry
	mcpManager        *mcp.Manager
	memoryManager     *memory.Manager
	memoryAIConfig    *models.AIConfig // 记忆管理使用的 LLM 配置
	moderatorAIConfig *models.AIConfig // 意图分析(小韭菜)使用的 LLM 配置
	aiConfigResolver  AIConfigResolver // AI配置解析器
	retryCount        int
	verboseAgentIO    bool
	selectionStyle    models.AgentSelectionStyle
	enableSecondRound bool
	meetingStates     map[string]*MeetingState // 中断的会议状态缓存，key: stockCode
	meetingStatesMu   sync.RWMutex
}

// NewServiceFull 创建完整配置的会议室服务
func NewServiceFull(registry *tools.Registry, mcpMgr *mcp.Manager) *Service {
	return &Service{
		modelFactory:      adk.NewModelFactory(),
		toolRegistry:      registry,
		mcpManager:        mcpMgr,
		retryCount:        DefaultAIRetryCount,
		verboseAgentIO:    false,
		selectionStyle:    models.AgentSelectionBalanced,
		enableSecondRound: false,
		meetingStates:     make(map[string]*MeetingState),
	}
}

// SetMemoryManager 设置记忆管理器
func (s *Service) SetMemoryManager(memMgr *memory.Manager) {
	s.memoryManager = memMgr
}

// SetMemoryAIConfig 设置记忆管理使用的 LLM 配置
func (s *Service) SetMemoryAIConfig(aiConfig *models.AIConfig) {
	s.memoryAIConfig = aiConfig
}

// SetModeratorAIConfig 设置意图分析(小韭菜)使用的 LLM 配置
func (s *Service) SetModeratorAIConfig(aiConfig *models.AIConfig) {
	s.moderatorAIConfig = aiConfig
}

// SetAIConfigResolver 设置 AI 配置解析器
func (s *Service) SetAIConfigResolver(resolver AIConfigResolver) {
	s.aiConfigResolver = resolver
}

// SetRetryCount 设置 AI 请求重试次数（1-5，超出范围自动收敛）
func (s *Service) SetRetryCount(count int) {
	if count < 1 {
		count = DefaultAIRetryCount
	}
	if count > 5 {
		count = 5
	}
	s.retryCount = count
}

// SetVerboseAgentIO 设置是否输出完整 Agent 输入输出日志
func (s *Service) SetVerboseAgentIO(enabled bool) {
	s.verboseAgentIO = enabled
}

// SetAgentSelectionStyle 设置小韭菜选人风格
func (s *Service) SetAgentSelectionStyle(style models.AgentSelectionStyle) {
	switch style {
	case models.AgentSelectionConservative, models.AgentSelectionAggressive, models.AgentSelectionBalanced:
		s.selectionStyle = style
	default:
		s.selectionStyle = models.AgentSelectionBalanced
	}
}

// SetEnableSecondReview 设置是否启用二轮复议
func (s *Service) SetEnableSecondReview(enabled bool) {
	s.enableSecondRound = enabled
}

// ChatRequest 聊天请求
type ChatRequest struct {
	StockCode    string                `json:"stockCode"` // 股票代码（用于状态缓存 key）
	Stock        models.Stock          `json:"stock"`
	KLineData    []models.KLineData    `json:"klineData"`
	Agents       []models.AgentConfig  `json:"agents"`
	Query        string                `json:"query"`
	ReplyContent string                `json:"replyContent"`
	CoreContext  string                `json:"coreContext"`
	AllAgents    []models.AgentConfig  `json:"allAgents"` // 所有可用专家（智能模式用）
	Position     *models.StockPosition `json:"position"`  // 用户持仓信息
}

// 会议模式常量
const (
	MeetingModeSmart  = "smart"  // 串行智能模式（小韭菜编排）
	MeetingModeDirect = "direct" // 独立模式（@ 指定专家）
)

const (
	ModeratorAgentID = "moderator"
	ModeratorName    = "老板娘"
	ModeratorRole    = "会议主持"
)

// ChatResponse 聊天响应
type ChatResponse struct {
	AgentID     string `json:"agentId"`
	AgentName   string `json:"agentName"`
	Role        string `json:"role"`
	Content     string `json:"content"`
	Round       int    `json:"round"`
	MsgType     string `json:"msgType"`               // opening/opinion/summary
	Error       string `json:"error,omitempty"`       // 失败时的错误信息，前端据此显示重试按钮
	MeetingMode string `json:"meetingMode,omitempty"` // smart=串行, direct=独立
}

type DebateChallenge struct {
	ChallengerID   string
	ChallengerName string
	TargetID       string
	TargetName     string
	Question       string
}

// ResponseCallback 响应回调函数类型
// 每当有新的发言产生时调用，用于实时推送到前端
type ResponseCallback func(resp ChatResponse)

// ProgressEvent 进度事件（细粒度实时反馈）
type ProgressEvent struct {
	Type      string `json:"type"`      // thinking/tool_call/tool_result/streaming/agent_start/agent_done
	AgentID   string `json:"agentId"`   // 当前专家 ID
	AgentName string `json:"agentName"` // 当前专家名称
	Detail    string `json:"detail"`    // 工具名称或阶段描述
	Content   string `json:"content"`   // 流式文本片段或工具结果摘要
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(event ProgressEvent)

// emitProgress 安全地发送进度事件（nil 安全）
func emitProgress(cb ProgressCallback, event ProgressEvent) {
	if cb != nil {
		cb(event)
	}
}

func finalizeAgentContent(partialText string, finalText string, sawPartial bool) (string, error) {
	partialText = openai.FilterVendorToolCallMarkers(partialText)
	finalText = openai.FilterVendorToolCallMarkers(finalText)
	if sawPartial && strings.TrimSpace(partialText) != "" {
		return partialText, nil
	}
	if strings.TrimSpace(finalText) == "" {
		return "", ErrEmptyAgentReply
	}
	return finalText, nil
}

// SendMessage 发送会议消息，生成多专家回复（并行执行）
func (s *Service) SendMessage(ctx context.Context, aiConfig *models.AIConfig, req ChatRequest) ([]ChatResponse, error) {
	llm, err := s.modelFactory.CreateModel(ctx, aiConfig)
	if err != nil {
		log.Error("CreateModel error: %v", err)
		return nil, err
	}
	log.Info("model created successfully")

	return s.runAgentsParallel(ctx, llm, aiConfig, req)
}

// RunSmartMeeting 智能会议模式（小韭菜编排）
// 专家按顺序串行发言，后一个专家可以参考前面的发言内容
func (s *Service) RunSmartMeeting(ctx context.Context, aiConfig *models.AIConfig, req ChatRequest) ([]ChatResponse, error) {
	return s.RunSmartMeetingWithCallback(ctx, aiConfig, req, nil, nil)
}

// RunSmartMeetingSync OpenClaw 专用：串行分析，只返回最终总结结果
// 不使用流式回调，不缓存中断状态，专家失败时跳过继续
func (s *Service) RunSmartMeetingSync(ctx context.Context, aiConfig *models.AIConfig, req ChatRequest) (string, error) {
	if aiConfig == nil {
		return "", ErrNoAIConfig
	}
	if len(req.AllAgents) == 0 {
		return "", ErrNoAgents
	}

	// 设置整个会议的超时上下文
	meetingCtx, meetingCancel := context.WithTimeout(ctx, MeetingTimeout)
	defer meetingCancel()

	// 创建模型
	modelCtx, modelCancel := context.WithTimeout(meetingCtx, ModelCreationTimeout)
	llm, err := s.modelFactory.CreateModel(modelCtx, aiConfig)
	modelCancel()
	if err != nil {
		return "", fmt.Errorf("create model error: %w", err)
	}

	// 创建 Moderator LLM
	var moderatorLLM model.LLM
	if s.moderatorAIConfig != nil {
		moderatorLLM, err = s.modelFactory.CreateModel(meetingCtx, s.moderatorAIConfig)
		if err != nil {
			log.Warn("create moderator LLM error, fallback to default: %v", err)
			moderatorLLM = llm
		}
	} else {
		moderatorLLM = llm
	}
	moderator := NewModerator(moderatorLLM)
	moderator.SetSelectionStyle(s.selectionStyle)

	// 设置记忆 LLM
	if s.memoryManager != nil {
		if s.memoryAIConfig != nil {
			memoryLLM, err := s.modelFactory.CreateModel(meetingCtx, s.memoryAIConfig)
			if err == nil {
				s.memoryManager.SetLLM(memoryLLM)
			} else {
				s.memoryManager.SetLLM(llm)
			}
		} else {
			s.memoryManager.SetLLM(llm)
		}
	}

	// 加载股票记忆
	var stockMemory *memory.StockMemory
	var memoryContext string
	if s.memoryManager != nil {
		stockMemory, _ = s.memoryManager.GetOrCreate(req.Stock.Symbol, req.Stock.Name)
		memoryContext = s.memoryManager.BuildContext(stockMemory, req.Query)
	}

	log.Info("[OpenClaw] stock: %s, query: %s, agents: %d", req.Stock.Symbol, req.Query, len(req.AllAgents))

	// 第0轮：小韭菜分析意图并选择专家
	moderatorCtx, moderatorCancel := context.WithTimeout(meetingCtx, ModeratorTimeout)
	decision, err := moderator.Analyze(moderatorCtx, &req.Stock, req.Query, req.AllAgents)
	moderatorCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: 小韭菜分析超时", ErrModeratorTimeout)
		}
		return "", fmt.Errorf("moderator analyze error: %w", err)
	}

	log.Debug("[OpenClaw] decision: selected=%v, topic=%s", decision.Selected, decision.Topic)

	selectedAgents := s.filterAgentsOrdered(req.AllAgents, decision.Selected)
	if len(selectedAgents) == 0 {
		selectedAgents = s.fallbackAgents(req.AllAgents, 2)
		if len(selectedAgents) == 0 {
			return "", fmt.Errorf("小韭菜未选中任何有效专家")
		}
	}

	// 第1轮：专家串行发言，失败时跳过继续
	var history []DiscussionEntry
	for i, agentCfg := range selectedAgents {
		if meetingCtx.Err() != nil {
			log.Warn("[OpenClaw] meeting timeout, got %d/%d agents", i, len(selectedAgents))
			break
		}

		log.Debug("[OpenClaw] agent %d/%d: %s starting", i+1, len(selectedAgents), agentCfg.Name)

		agentAIConfig := s.resolveAgentAIConfig(&agentCfg, aiConfig)
		agentLLM, err := s.modelFactory.CreateModel(meetingCtx, agentAIConfig)
		if err != nil {
			log.Error("[OpenClaw] create agent LLM error, skip %s: %v", agentCfg.ID, err)
			continue
		}
		builder := s.createBuilder(agentLLM, agentAIConfig)

		previousContext := s.buildPreviousContext(history)
		if memoryContext != "" {
			previousContext = memoryContext + "\n" + previousContext
		}

		agentQuery := req.Query
		if decision.Tasks != nil {
			if task, ok := decision.Tasks[agentCfg.ID]; ok && task != "" {
				agentQuery = task
			}
		}

		content, err := retryRun(meetingCtx, s.retryCount, func() (string, error) {
			agentCtx, agentCancel := context.WithTimeout(meetingCtx, AgentTimeout)
			defer agentCancel()
			return s.runSingleAgent(agentCtx, builder, &agentCfg, &req.Stock, agentQuery, previousContext, req.CoreContext, nil, req.Position)
		})

		if err != nil {
			log.Error("[OpenClaw] agent %s failed, skip: %v", agentCfg.ID, err)
			continue
		}

		history = append(history, DiscussionEntry{
			Round: 1, AgentID: agentCfg.ID, AgentName: agentCfg.Name,
			Role: agentCfg.Role, Content: content,
		})
		log.Debug("[OpenClaw] agent %s done, content len: %d", agentCfg.ID, len(content))
	}

	if len(history) == 0 {
		return "", fmt.Errorf("所有专家均分析失败")
	}

	if s.enableSecondRound {
		reviewResponses := s.runSecondReviewRound(meetingCtx, aiConfig, &req.Stock, req.Query, selectedAgents, history, req.Position, nil, MeetingModeSmart, 4)
		for _, resp := range reviewResponses {
			history = append(history, DiscussionEntry{
				Round: resp.Round, AgentID: resp.AgentID, AgentName: resp.AgentName, Role: resp.Role, Content: resp.Content,
			})
		}
	}

	// 最终轮：小韭菜总结
	summaryCtx, summaryCancel := context.WithTimeout(meetingCtx, ModeratorTimeout)
	summary, err := moderator.SummarizeWithContext(summaryCtx, &req.Stock, req.Query, history, buildSummaryContext(req.CoreContext, &req.Stock, req.Position))
	summaryCancel()
	if err != nil {
		return "", fmt.Errorf("总结生成失败: %w", err)
	}

	// 异步保存记忆
	if s.memoryManager != nil && stockMemory != nil && summary != "" {
		go func() {
			bgCtx := context.Background()
			keyPoints := s.extractKeyPointsFromHistory(bgCtx, history)
			if err := s.memoryManager.AddRound(bgCtx, stockMemory, req.Query, summary, keyPoints); err != nil {
				log.Error("[OpenClaw] save memory error: %v", err)
			}
		}()
	}

	log.Info("[OpenClaw] meeting done for %s, summary len: %d", req.Stock.Symbol, len(summary))
	return summary, nil
}

// RunSmartMeetingWithCallback 智能会议模式（带实时回调）
// respCallback 在每个发言完成后调用
// progressCallback 在工具调用、流式输出等细粒度事件时调用
func (s *Service) RunSmartMeetingWithCallback(ctx context.Context, aiConfig *models.AIConfig, req ChatRequest, respCallback ResponseCallback, progressCallback ProgressCallback) ([]ChatResponse, error) {
	if aiConfig == nil {
		return nil, ErrNoAIConfig
	}
	if len(req.AllAgents) == 0 {
		return nil, ErrNoAgents
	}

	// 设置整个会议的超时上下文
	meetingCtx, meetingCancel := context.WithTimeout(ctx, MeetingTimeout)
	defer meetingCancel()

	// 创建模型（带超时）
	modelCtx, modelCancel := context.WithTimeout(meetingCtx, ModelCreationTimeout)
	llm, err := s.modelFactory.CreateModel(modelCtx, aiConfig)
	modelCancel()
	if err != nil {
		return nil, fmt.Errorf("create model error: %w", err)
	}

	var responses []ChatResponse

	// 创建 Moderator LLM（优先使用独立配置）
	var moderatorLLM model.LLM
	if s.moderatorAIConfig != nil {
		moderatorLLM, err = s.modelFactory.CreateModel(meetingCtx, s.moderatorAIConfig)
		if err != nil {
			log.Warn("create moderator LLM error, fallback to default: %v", err)
			moderatorLLM = llm
		} else {
			log.Debug("using dedicated moderator LLM: %s", s.moderatorAIConfig.ModelName)
		}
	} else {
		moderatorLLM = llm
	}
	moderator := NewModerator(moderatorLLM)
	moderator.SetSelectionStyle(s.selectionStyle)

	// 设置 LLM 到记忆管理器（启用摘要功能）
	if s.memoryManager != nil {
		// 优先使用配置的记忆 LLM，否则使用会议 LLM
		if s.memoryAIConfig != nil {
			memoryLLM, err := s.modelFactory.CreateModel(meetingCtx, s.memoryAIConfig)
			if err == nil {
				s.memoryManager.SetLLM(memoryLLM)
				log.Debug("using dedicated memory LLM: %s", s.memoryAIConfig.ModelName)
			} else {
				log.Warn("create memory LLM error, fallback to meeting LLM: %v", err)
				s.memoryManager.SetLLM(llm)
			}
		} else {
			s.memoryManager.SetLLM(llm)
		}
	}

	// 加载股票记忆（如果启用了记忆管理）
	var stockMemory *memory.StockMemory
	var memoryContext string
	if s.memoryManager != nil {
		stockMemory, _ = s.memoryManager.GetOrCreate(req.Stock.Symbol, req.Stock.Name)
		memoryContext = s.memoryManager.BuildContext(stockMemory, req.Query)
		if memoryContext != "" {
			log.Debug("loaded memory context for %s, len: %d", req.Stock.Symbol, len(memoryContext))
		}
	}

	log.Info("stock: %s, query: %s, agents: %d", req.Stock.Symbol, req.Query, len(req.AllAgents))

	// 第0轮：小韭菜分析意图并选择专家（带超时）
	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_start", AgentID: ModeratorAgentID, AgentName: ModeratorName, Detail: "分析问题意图",
	})

	moderatorCtx, moderatorCancel := context.WithTimeout(meetingCtx, ModeratorTimeout)
	decision, err := moderator.Analyze(moderatorCtx, &req.Stock, req.Query, req.AllAgents)
	moderatorCancel()

	if err != nil {
		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_done", AgentID: ModeratorAgentID, AgentName: ModeratorName,
		})
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: 小韭菜分析超时", ErrModeratorTimeout)
		}
		return nil, fmt.Errorf("moderator analyze error: %w", err)
	}

	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_done", AgentID: ModeratorAgentID, AgentName: ModeratorName,
	})

	log.Debug("decision: selected=%v, topic=%s", decision.Selected, decision.Topic)

	// 添加开场白并立即回调
	openingResp := ChatResponse{
		AgentID:     ModeratorAgentID,
		AgentName:   ModeratorName,
		Role:        ModeratorRole,
		Content:     decision.Opening,
		Round:       0,
		MsgType:     "opening",
		MeetingMode: MeetingModeSmart,
	}
	responses = append(responses, openingResp)
	if respCallback != nil {
		respCallback(openingResp)
	}

	// 筛选被选中的专家（按小韭菜选择的顺序）
	selectedAgents := s.filterAgentsOrdered(req.AllAgents, decision.Selected)
	if len(selectedAgents) == 0 {
		selectedAgents = s.fallbackAgents(req.AllAgents, 2)
		if len(selectedAgents) == 0 {
			return responses, nil
		}
	}

	// 三轮讨论：
	// R1 独立陈述 -> R2 强制交锋 -> R3 修正终判
	var history []DiscussionEntry
	var roundOneHistory []DiscussionEntry
	var roundTwoHistory []DiscussionEntry

	// ---------- R1: 独立陈述 ----------
	for i, agentCfg := range selectedAgents {
		select {
		case <-meetingCtx.Done():
			log.Warn("meeting timeout, got %d responses", len(responses))
			return responses, ErrMeetingTimeout
		default:
		}

		log.Debug("round1 agent %d/%d: %s starting", i+1, len(selectedAgents), agentCfg.Name)
		agentAIConfig := s.resolveAgentAIConfig(&agentCfg, aiConfig)
		agentLLM, err := s.modelFactory.CreateModel(meetingCtx, agentAIConfig)
		if err != nil {
			log.Error("create agent LLM error: %v", err)
			continue
		}
		builder := s.createBuilder(agentLLM, agentAIConfig)

		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_start", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: "第一轮·独立陈述",
		})

		agentQuery := req.Query
		if decision.Tasks != nil {
			if task, ok := decision.Tasks[agentCfg.ID]; ok && task != "" {
				agentQuery = task
			}
		}
		roundPrompt := buildRoundOnePrompt(agentQuery)

		roundCoreContext := req.CoreContext
		if memoryContext != "" {
			roundCoreContext = strings.TrimSpace(memoryContext + "\n\n" + roundCoreContext)
		}

		content, err := retryRun(meetingCtx, s.retryCount, func() (string, error) {
			agentCtx, agentCancel := context.WithTimeout(meetingCtx, AgentTimeout)
			defer agentCancel()
			return s.runSingleAgent(agentCtx, builder, &agentCfg, &req.Stock, roundPrompt, "", roundCoreContext, progressCallback, req.Position)
		})

		if err != nil {
			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_error", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: err.Error(),
			})
			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
			})
			log.Error("round1 agent %s failed after retries: %v", agentCfg.ID, err)

			failedResp := ChatResponse{
				AgentID:     agentCfg.ID,
				AgentName:   agentCfg.Name,
				Role:        agentCfg.Role,
				Content:     "",
				Round:       1,
				MsgType:     "opinion",
				Error:       err.Error(),
				MeetingMode: MeetingModeSmart,
			}
			responses = append(responses, failedResp)
			if respCallback != nil {
				respCallback(failedResp)
			}

			if req.StockCode != "" {
				s.cacheMeetingState(req.StockCode, &MeetingState{
					AIConfig:       aiConfig,
					Stock:          req.Stock,
					Query:          req.Query,
					Position:       req.Position,
					SelectedAgents: selectedAgents,
					History:        history,
					Responses:      responses,
					FailedIndex:    i,
					MemoryContext:  memoryContext,
					StockMemory:    stockMemory,
					Moderator:      moderator,
					CreatedAt:      time.Now(),
				})

				remainingIDs := make([]string, 0, len(selectedAgents)-i-1)
				for _, ra := range selectedAgents[i+1:] {
					remainingIDs = append(remainingIDs, ra.ID)
				}
				emitProgress(progressCallback, ProgressEvent{
					Type: "meeting_interrupted", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
					Detail: err.Error(), Content: strings.Join(remainingIDs, ","),
				})
			}
			break
		}

		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
		})

		resp := ChatResponse{
			AgentID:     agentCfg.ID,
			AgentName:   agentCfg.Name,
			Role:        agentCfg.Role,
			Content:     content,
			Round:       1,
			MsgType:     "opinion",
			MeetingMode: MeetingModeSmart,
		}
		responses = append(responses, resp)
		if respCallback != nil {
			respCallback(resp)
		}

		entry := DiscussionEntry{
			Round:     1,
			AgentID:   agentCfg.ID,
			AgentName: agentCfg.Name,
			Role:      agentCfg.Role,
			Content:   content,
		}
		history = append(history, entry)
		roundOneHistory = append(roundOneHistory, entry)

		log.Debug("round1 agent %s done, content len: %d", agentCfg.ID, len(content))
	}

	// 检查是否被中断（有缓存状态说明中断了，跳过总结）
	if req.StockCode != "" {
		s.meetingStatesMu.RLock()
		_, interrupted := s.meetingStates[req.StockCode]
		s.meetingStatesMu.RUnlock()
		if interrupted {
			log.Info("meeting interrupted for %s, skipping summary", req.StockCode)
			return responses, nil
		}
	}

	// ---------- R2: 强制交锋（每位专家至少质疑一位） ----------
	if len(roundOneHistory) > 1 {
		for _, challenger := range selectedAgents {
			select {
			case <-meetingCtx.Done():
				log.Warn("meeting timeout during round2, got %d responses", len(responses))
				return responses, ErrMeetingTimeout
			default:
			}

			target := pickDebateTarget(challenger, selectedAgents)
			if target.ID == "" {
				continue
			}

			challenge := DebateChallenge{
				ChallengerID:   challenger.ID,
				ChallengerName: challenger.Name,
				TargetID:       target.ID,
				TargetName:     target.Name,
				Question:       buildDebateQuestion(challenger, target),
			}

			agentAIConfig := s.resolveAgentAIConfig(&target, aiConfig)
			agentLLM, err := s.modelFactory.CreateModel(meetingCtx, agentAIConfig)
			if err != nil {
				log.Error("round2 create agent LLM error: %v", err)
				continue
			}
			builder := s.createBuilder(agentLLM, agentAIConfig)

			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_start", AgentID: target.ID, AgentName: target.Name, Detail: "第二轮·交叉质疑",
			})

			roundPrompt := buildChallengePrompt(challenge, req.Query, roundOneHistory)
			roundCoreContext := req.CoreContext
			if memoryContext != "" {
				roundCoreContext = strings.TrimSpace(memoryContext + "\n\n" + roundCoreContext)
			}

			content, err := retryRun(meetingCtx, s.retryCount, func() (string, error) {
				agentCtx, agentCancel := context.WithTimeout(meetingCtx, AgentTimeout)
				defer agentCancel()
				return s.runSingleAgent(agentCtx, builder, &target, &req.Stock, roundPrompt, "", roundCoreContext, progressCallback, req.Position)
			})

			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_done", AgentID: target.ID, AgentName: target.Name,
			})
			if err != nil || strings.TrimSpace(content) == "" {
				log.Warn("round2 target %s skipped: %v", target.ID, err)
				continue
			}

			contentWithTag := fmt.Sprintf("【交锋】%s -> %s：%s\n%s", challenge.ChallengerName, challenge.TargetName, challenge.Question, content)
			resp := ChatResponse{
				AgentID:     target.ID,
				AgentName:   target.Name,
				Role:        target.Role,
				Content:     contentWithTag,
				Round:       2,
				MsgType:     "opinion",
				MeetingMode: MeetingModeSmart,
			}
			responses = append(responses, resp)
			if respCallback != nil {
				respCallback(resp)
			}

			entry := DiscussionEntry{
				Round:     2,
				AgentID:   target.ID,
				AgentName: target.Name,
				Role:      target.Role,
				Content:   contentWithTag,
			}
			history = append(history, entry)
			roundTwoHistory = append(roundTwoHistory, entry)
		}
	}

	// ---------- R3: 修正终判 ----------
	if len(roundOneHistory) > 0 {
		for _, agentCfg := range selectedAgents {
			select {
			case <-meetingCtx.Done():
				log.Warn("meeting timeout during round3, got %d responses", len(responses))
				return responses, ErrMeetingTimeout
			default:
			}

			agentAIConfig := s.resolveAgentAIConfig(&agentCfg, aiConfig)
			agentLLM, err := s.modelFactory.CreateModel(meetingCtx, agentAIConfig)
			if err != nil {
				log.Error("round3 create agent LLM error: %v", err)
				continue
			}
			builder := s.createBuilder(agentLLM, agentAIConfig)

			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_start", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: "第三轮·修正终判",
			})

			roundPrompt := buildFinalRevisionPrompt(req.Query, roundOneHistory, roundTwoHistory)
			roundCoreContext := req.CoreContext
			if memoryContext != "" {
				roundCoreContext = strings.TrimSpace(memoryContext + "\n\n" + roundCoreContext)
			}

			content, err := retryRun(meetingCtx, s.retryCount, func() (string, error) {
				agentCtx, agentCancel := context.WithTimeout(meetingCtx, AgentTimeout)
				defer agentCancel()
				return s.runSingleAgent(agentCtx, builder, &agentCfg, &req.Stock, roundPrompt, "", roundCoreContext, progressCallback, req.Position)
			})

			emitProgress(progressCallback, ProgressEvent{
				Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
			})
			if err != nil || strings.TrimSpace(content) == "" {
				log.Warn("round3 agent %s skipped: %v", agentCfg.ID, err)
				continue
			}

			resp := ChatResponse{
				AgentID:     agentCfg.ID,
				AgentName:   agentCfg.Name,
				Role:        agentCfg.Role,
				Content:     content,
				Round:       3,
				MsgType:     "opinion",
				MeetingMode: MeetingModeSmart,
			}
			responses = append(responses, resp)
			if respCallback != nil {
				respCallback(resp)
			}

			history = append(history, DiscussionEntry{
				Round:     3,
				AgentID:   agentCfg.ID,
				AgentName: agentCfg.Name,
				Role:      agentCfg.Role,
				Content:   content,
			})
		}
	}

	// ---------- 可选 R4: 复议 ----------
	if s.enableSecondRound && len(history) > 0 {
		reviewResponses := s.runSecondReviewRound(meetingCtx, aiConfig, &req.Stock, req.Query, selectedAgents, history, req.Position, progressCallback, MeetingModeSmart, 4)
		for _, resp := range reviewResponses {
			responses = append(responses, resp)
			if respCallback != nil {
				respCallback(resp)
			}
			history = append(history, DiscussionEntry{
				Round: resp.Round, AgentID: resp.AgentID, AgentName: resp.AgentName, Role: resp.Role, Content: resp.Content,
			})
		}
	}

	// 最终轮：小韭菜总结（带超时）
	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_start", AgentID: ModeratorAgentID, AgentName: ModeratorName, Detail: "总结讨论",
	})

	summaryCtx, summaryCancel := context.WithTimeout(meetingCtx, ModeratorTimeout)
	summary, err := moderator.Summarize(summaryCtx, &req.Stock, req.Query, history)
	summaryCancel()

	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_done", AgentID: ModeratorAgentID, AgentName: ModeratorName,
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn("summary timeout, returning partial results")
		} else {
			log.Error("summary error: %v", err)
		}
		// 总结失败不影响返回已有结果
		return responses, nil
	}

	if summary != "" {
		summaryResp := ChatResponse{
			AgentID:     ModeratorAgentID,
			AgentName:   ModeratorName,
			Role:        ModeratorRole,
			Content:     summary,
			Round:       summaryRound(history),
			MsgType:     "summary",
			MeetingMode: MeetingModeSmart,
		}
		responses = append(responses, summaryResp)
		if respCallback != nil {
			respCallback(summaryResp)
		}
	}

	// 保存记忆（如果启用了记忆管理）
	if s.memoryManager != nil && stockMemory != nil && summary != "" {
		// 异步保存记忆，不阻塞返回
		go func() {
			// 使用独立 context，因为会议 ctx 可能已取消
			bgCtx := context.Background()
			keyPoints := s.extractKeyPointsFromHistory(bgCtx, history)
			if err := s.memoryManager.AddRound(bgCtx, stockMemory, req.Query, summary, keyPoints); err != nil {
				log.Error("save memory error: %v", err)
			} else {
				log.Debug("saved memory for %s", req.Stock.Symbol)
			}
		}()
	}

	return responses, nil
}

// runAgentsParallel 并行运行多个 Agent（带超时控制）
func (s *Service) runAgentsParallel(ctx context.Context, defaultLLM model.LLM, defaultAIConfig *models.AIConfig, req ChatRequest) ([]ChatResponse, error) {
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		responses []ChatResponse
	)

	// 设置整体超时
	parallelCtx, cancel := context.WithTimeout(ctx, MeetingTimeout)
	defer cancel()

	log.Debug("running %d agents in parallel", len(req.Agents))

	for _, agentConfig := range req.Agents {
		wg.Add(1)
		go func(cfg models.AgentConfig) {
			defer wg.Done()

			// 获取该专家的 AI 配置
			agentAIConfig := s.resolveAgentAIConfig(&cfg, defaultAIConfig)

			// 为该专家创建 LLM
			var agentLLM model.LLM
			var err error
			if agentAIConfig == defaultAIConfig {
				agentLLM = defaultLLM
			} else {
				agentLLM, err = s.modelFactory.CreateModel(parallelCtx, agentAIConfig)
				if err != nil {
					log.Error("create agent LLM error: %v", err)
					return
				}
			}
			builder := s.createBuilder(agentLLM, agentAIConfig)

			// 单个 Agent 带指数退避重试
			content, err := retryRun(parallelCtx, s.retryCount, func() (string, error) {
				agentCtx, agentCancel := context.WithTimeout(parallelCtx, AgentTimeout)
				defer agentCancel()
				return s.runSingleAgent(agentCtx, builder, &cfg, &req.Stock, req.Query, req.ReplyContent, req.CoreContext, nil, req.Position)
			})
			if err != nil {
				log.Error("agent %s failed after retries: %v", cfg.ID, err)
				mu.Lock()
				responses = append(responses, ChatResponse{
					AgentID:     cfg.ID,
					AgentName:   cfg.Name,
					Role:        cfg.Role,
					MsgType:     "opinion",
					Error:       err.Error(),
					MeetingMode: MeetingModeDirect,
				})
				mu.Unlock()
				return
			}

			mu.Lock()
			responses = append(responses, ChatResponse{
				AgentID:     cfg.ID,
				AgentName:   cfg.Name,
				Role:        cfg.Role,
				Content:     content,
				MeetingMode: MeetingModeDirect,
			})
			mu.Unlock()
			log.Debug("agent %s done, content len: %d", cfg.ID, len(content))
		}(agentConfig)
	}

	wg.Wait()
	log.Info("all agents done, got %d responses", len(responses))
	return responses, nil
}

// runSingleAgent 运行单个 Agent（统一入口）
// progressCallback 为 nil 时不发送进度事件，也不启用 streaming 模式
func (s *Service) runSingleAgent(
	ctx context.Context,
	builder *adk.ExpertAgentBuilder,
	cfg *models.AgentConfig,
	stock *models.Stock,
	query string,
	replyContent string,
	coreContext string,
	progressCallback ProgressCallback,
	position *models.StockPosition,
) (string, error) {
	agentInstance, err := builder.BuildAgentWithContext(cfg, stock, query, replyContent, coreContext, position)
	if err != nil {
		return "", err
	}

	sessionService := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:        "jcp",
		Agent:          agentInstance,
		SessionService: sessionService,
	})
	if err != nil {
		return "", err
	}

	sessionID := fmt.Sprintf("session-%s-%d", cfg.ID, time.Now().UnixNano())
	if _, err = sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "jcp",
		UserID:    "user",
		SessionID: sessionID,
	}); err != nil {
		return "", fmt.Errorf("create session error: %w", err)
	}

	userMsg := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText(query)},
	}

	// 有 progressCallback 时启用 streaming，否则普通模式
	runCfg := agent.RunConfig{}
	if progressCallback != nil {
		runCfg.StreamingMode = agent.StreamingModeSSE
	}

	var partialText strings.Builder
	var finalText strings.Builder
	sawPartial := false
	for event, err := range r.Run(ctx, "user", sessionID, userMsg, runCfg) {
		if err != nil {
			return "", err
		}
		if event == nil || event.LLMResponse.Content == nil {
			continue
		}
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Thought {
				continue
			}
			if part.FunctionCall != nil && progressCallback != nil {
				progressCallback(ProgressEvent{
					Type: "tool_call", AgentID: cfg.ID, AgentName: cfg.Name,
					Detail: part.FunctionCall.Name,
				})
			}
			if part.FunctionCall != nil && s.verboseAgentIO {
				log.Info("agent %s tool_call: %s", cfg.ID, part.FunctionCall.Name)
			}
			if part.FunctionResponse != nil && progressCallback != nil {
				progressCallback(ProgressEvent{
					Type: "tool_result", AgentID: cfg.ID, AgentName: cfg.Name,
					Detail: part.FunctionResponse.Name,
				})
			}
			if part.FunctionResponse != nil && s.verboseAgentIO {
				log.Info("agent %s tool_result: %s", cfg.ID, part.FunctionResponse.Name)
			}
			if part.Text != "" {
				// streaming 模式下只累积 Partial 片段，避免重复
				if progressCallback != nil {
					if event.LLMResponse.Partial {
						sawPartial = true
						partialText.WriteString(part.Text)
						progressCallback(ProgressEvent{
							Type: "streaming", AgentID: cfg.ID, AgentName: cfg.Name,
							Content: part.Text,
						})
					} else if !sawPartial {
						finalText.WriteString(part.Text)
					}
				} else {
					finalText.WriteString(part.Text)
				}
			}
		}
	}

	content, err := finalizeAgentContent(partialText.String(), finalText.String(), sawPartial)
	if err != nil {
		if s.verboseAgentIO {
			log.Warn("agent %s empty output treated as retryable failure", cfg.ID)
		}
		return "", err
	}
	if s.verboseAgentIO {
		log.Info("agent %s output: %s", cfg.ID, truncateString(content, 160))
	}
	return content, nil
}

// filterAgentsOrdered 按指定顺序筛选专家（保持小韭菜选择的顺序）
func (s *Service) filterAgentsOrdered(all []models.AgentConfig, ids []string) []models.AgentConfig {
	agentMap := make(map[string]models.AgentConfig)
	for _, a := range all {
		agentMap[a.ID] = a
	}
	var result []models.AgentConfig
	for _, id := range ids {
		if agent, ok := agentMap[id]; ok {
			result = append(result, agent)
		}
	}
	return result
}

// buildPreviousContext 构建前面专家发言的上下文
func (s *Service) buildPreviousContext(history []DiscussionEntry) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【前面专家的发言】\n")
	for _, entry := range history {
		fmt.Fprintf(&sb, "- %s（%s）：%s\n\n", entry.AgentName, entry.Role, entry.Content)
	}
	return sb.String()
}

func buildRoundContext(title string, entries []DiscussionEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【")
	sb.WriteString(title)
	sb.WriteString("】\n")
	for _, entry := range entries {
		fmt.Fprintf(&sb, "- %s（%s）：%s\n\n", entry.AgentName, entry.Role, entry.Content)
	}
	return sb.String()
}

func buildRoundOnePrompt(baseQuery string) string {
	return "【第一轮：独立陈述】\n" +
		"请独立给出你的初判，不要被其他专家预设立场影响。\n" +
		"要求：必须包含【结论】【核心证据】【买点/卖点或观望条件】【止损或失效条件】【仓位建议】。\n" +
		"若数据不足，明确写“数据不足”。\n\n用户问题：" + baseQuery
}

func buildChallengePrompt(challenge DebateChallenge, baseQuery string, roundOneHistory []DiscussionEntry) string {
	var sb strings.Builder
	sb.WriteString("【第二轮：交叉质疑（强制交锋）】\n")
	sb.WriteString("你不是原发言人，请站在你的专业视角回应下面这条质疑。\n")
	sb.WriteString("要求：先回答质疑，再给出你认可/不认可的理由，最后给可执行修正建议。\n")
	sb.WriteString("若不同意，必须给可证伪条件；若同意，必须给新增约束。\n\n")
	sb.WriteString(fmt.Sprintf("用户原问题：%s\n\n", baseQuery))
	sb.WriteString("【原始观点摘要】\n")
	for _, entry := range roundOneHistory {
		fmt.Fprintf(&sb, "- %s：%s\n", entry.AgentName, entry.Content)
	}
	sb.WriteString("\n")
	sb.WriteString("【当前质疑】\n")
	sb.WriteString(fmt.Sprintf("%s -> %s：%s\n", challenge.ChallengerName, challenge.TargetName, challenge.Question))
	sb.WriteString("\n请在180字内完成回应。")
	return sb.String()
}

func buildFinalRevisionPrompt(baseQuery string, roundOneHistory []DiscussionEntry, roundTwoResponses []DiscussionEntry) string {
	var sb strings.Builder
	sb.WriteString("【第三轮：修正终判】\n")
	sb.WriteString("请基于第一轮观点与第二轮交叉质疑，给出你的最终修正版结论。\n")
	sb.WriteString("必须输出：\n")
	sb.WriteString("1) 最终立场（看多/中性/看空）+ 置信度\n")
	sb.WriteString("2) 相比第一轮，你修正了什么\n")
	sb.WriteString("3) 触发失效的关键条件\n")
	sb.WriteString("4) 可执行动作（买/卖/观望 + 仓位）\n")
	sb.WriteString("若你坚持原观点，也要说明为何不修正。\n\n")
	sb.WriteString(fmt.Sprintf("用户问题：%s\n\n", baseQuery))
	sb.WriteString(buildRoundContext("第一轮观点", roundOneHistory))
	sb.WriteString(buildRoundContext("第二轮交叉质疑回应", roundTwoResponses))
	sb.WriteString("请在180字内回答。")
	return sb.String()
}

func pickDebateTarget(self models.AgentConfig, selectedAgents []models.AgentConfig) models.AgentConfig {
	if len(selectedAgents) <= 1 {
		return models.AgentConfig{}
	}
	selfRisk := strings.Contains(self.ID, "risk")
	selfTech := strings.Contains(self.ID, "technical")
	selfCapital := strings.Contains(self.ID, "capital")
	selfFundamental := strings.Contains(self.ID, "fundamental")

	chooseByID := func(preferred []string) models.AgentConfig {
		for _, pid := range preferred {
			for _, candidate := range selectedAgents {
				if candidate.ID == self.ID {
					continue
				}
				if candidate.ID == pid {
					return candidate
				}
			}
		}
		return models.AgentConfig{}
	}

	if selfRisk {
		if t := chooseByID([]string{"fundamental", "technical", "capital", "policy", "hottrend", "quant"}); t.ID != "" {
			return t
		}
	}
	if selfFundamental {
		if t := chooseByID([]string{"risk", "policy", "quant", "capital", "technical", "hottrend"}); t.ID != "" {
			return t
		}
	}
	if selfTech {
		if t := chooseByID([]string{"risk", "capital", "quant", "fundamental", "policy", "hottrend"}); t.ID != "" {
			return t
		}
	}
	if selfCapital {
		if t := chooseByID([]string{"technical", "risk", "quant", "hottrend", "fundamental", "policy"}); t.ID != "" {
			return t
		}
	}
	if strings.Contains(self.ID, "policy") {
		if t := chooseByID([]string{"fundamental", "risk", "quant", "technical", "capital", "hottrend"}); t.ID != "" {
			return t
		}
	}
	if strings.Contains(self.ID, "hottrend") {
		if t := chooseByID([]string{"capital", "risk", "technical", "quant", "fundamental", "policy"}); t.ID != "" {
			return t
		}
	}
	if strings.Contains(self.ID, "quant") {
		if t := chooseByID([]string{"technical", "capital", "fundamental", "risk", "policy", "hottrend"}); t.ID != "" {
			return t
		}
	}

	for _, candidate := range selectedAgents {
		if candidate.ID != self.ID {
			return candidate
		}
	}
	return models.AgentConfig{}
}

func buildDebateQuestion(challenger models.AgentConfig, target models.AgentConfig) string {
	if target.ID == "" || challenger.ID == "" {
		return "你的结论在什么条件下会失效？请给出可量化触发条件。"
	}

	switch challenger.ID {
	case "fundamental":
		return fmt.Sprintf("你给出的交易结论如何映射到盈利与估值分位？若EPS不达预期，你的结论如何修正？")
	case "technical":
		return fmt.Sprintf("你的判断对应的关键价位与失效位是什么？如果放量不持续，你是否撤回观点？")
	case "capital":
		return fmt.Sprintf("你的逻辑如何被资金行为验证？若出现价滞量增，你的结论会否反转？")
	case "policy":
		return fmt.Sprintf("你的结论里有哪些政策因素已price-in？若政策落地弱于预期，影响有多大？")
	case "risk":
		return fmt.Sprintf("你的观点最脆弱的假设是什么？给出最大下行空间和触发条件。")
	case "hottrend":
		return fmt.Sprintf("你的结论是否受情绪拥挤影响？若热度转折，观点要怎么调整？")
	case "quant":
		return fmt.Sprintf("你的结论在历史样本中的胜率和回撤分布是什么？统计上是否显著？")
	default:
		return fmt.Sprintf("请给出你观点的反证条件：什么情况下你会承认当前结论失效？")
	}
}

// extractKeyPointsFromHistory 从讨论历史中提取关键点
func (s *Service) extractKeyPointsFromHistory(ctx context.Context, history []DiscussionEntry) []string {
	// 如果有记忆管理器，使用 LLM 智能提取
	if s.memoryManager != nil {
		discussions := make([]memory.DiscussionInput, 0, len(history))
		for _, entry := range history {
			discussions = append(discussions, memory.DiscussionInput{
				AgentName: entry.AgentName,
				Role:      entry.Role,
				Content:   entry.Content,
			})
		}
		keyPoints, err := s.memoryManager.ExtractKeyPoints(ctx, discussions)
		if err != nil {
			log.Warn("LLM extract key points error, fallback: %v", err)
		} else {
			return keyPoints
		}
	}

	// 降级：简单截取
	keyPoints := make([]string, 0, len(history))
	for _, entry := range history {
		runes := []rune(entry.Content)
		content := entry.Content
		if len(runes) > 80 {
			content = string(runes[:80]) + "..."
		}
		keyPoints = append(keyPoints, fmt.Sprintf("%s: %s", entry.AgentName, content))
	}
	return keyPoints
}

// resolveAgentAIConfig 解析专家的 AI 配置（优先使用专家自定义配置，否则降级为默认配置）
func (s *Service) resolveAgentAIConfig(agentCfg *models.AgentConfig, defaultConfig *models.AIConfig) *models.AIConfig {
	if s.aiConfigResolver != nil && agentCfg.AIConfigID != "" {
		if resolved := s.aiConfigResolver(agentCfg.AIConfigID); resolved != nil {
			log.Debug("agent %s using custom AI: %s", agentCfg.ID, resolved.ModelName)
			return resolved
		}
	}
	return defaultConfig
}

// createBuilder 创建 ExpertAgentBuilder
func (s *Service) createBuilder(llm model.LLM, aiConfig *models.AIConfig) *adk.ExpertAgentBuilder {
	if s.mcpManager != nil {
		return adk.NewExpertAgentBuilderFull(llm, aiConfig, s.toolRegistry, s.mcpManager)
	}
	if s.toolRegistry != nil {
		return adk.NewExpertAgentBuilderWithTools(llm, aiConfig, s.toolRegistry)
	}
	return adk.NewExpertAgentBuilder(llm, aiConfig)
}

// RetrySingleAgent 重试单个失败的专家（前端手动重试调用）
func (s *Service) RetrySingleAgent(
	ctx context.Context,
	aiConfig *models.AIConfig,
	agentCfg *models.AgentConfig,
	stock *models.Stock,
	query string,
	progressCallback ProgressCallback,
	position *models.StockPosition,
) (ChatResponse, error) {
	// 获取该专家的 AI 配置
	agentAIConfig := s.resolveAgentAIConfig(agentCfg, aiConfig)

	agentLLM, err := s.modelFactory.CreateModel(ctx, agentAIConfig)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create model error: %w", err)
	}
	builder := s.createBuilder(agentLLM, agentAIConfig)

	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_start", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: agentCfg.Role,
	})

	// 带指数退避重试
	content, err := retryRun(ctx, s.retryCount, func() (string, error) {
		agentCtx, cancel := context.WithTimeout(ctx, AgentTimeout)
		defer cancel()
		return s.runSingleAgent(agentCtx, builder, agentCfg, stock, query, "", "", progressCallback, position)
	})

	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
	})

	if err != nil {
		return ChatResponse{
			AgentID:     agentCfg.ID,
			AgentName:   agentCfg.Name,
			Role:        agentCfg.Role,
			MsgType:     "opinion",
			Error:       err.Error(),
			MeetingMode: MeetingModeDirect,
		}, err
	}

	return ChatResponse{
		AgentID:     agentCfg.ID,
		AgentName:   agentCfg.Name,
		Role:        agentCfg.Role,
		Content:     content,
		Round:       1,
		MsgType:     "opinion",
		MeetingMode: MeetingModeDirect,
	}, nil
}

// cacheMeetingState 缓存中断的会议状态
func (s *Service) cacheMeetingState(stockCode string, state *MeetingState) {
	s.meetingStatesMu.Lock()
	defer s.meetingStatesMu.Unlock()
	s.meetingStates[stockCode] = state
	log.Info("cached meeting state for %s, failedIndex=%d", stockCode, state.FailedIndex)
}

// CancelInterruptedMeeting 取消中断的会议（用户放弃重试时调用）
func (s *Service) CancelInterruptedMeeting(stockCode string) {
	s.meetingStatesMu.Lock()
	defer s.meetingStatesMu.Unlock()
	delete(s.meetingStates, stockCode)
	log.Info("cancelled interrupted meeting for %s", stockCode)
}

// HasInterruptedMeeting 检查是否有中断的会议
func (s *Service) HasInterruptedMeeting(stockCode string) bool {
	s.meetingStatesMu.RLock()
	defer s.meetingStatesMu.RUnlock()
	state, ok := s.meetingStates[stockCode]
	if !ok {
		return false
	}
	// 检查 TTL
	if time.Since(state.CreatedAt) > MeetingStateTTL {
		return false
	}
	return true
}

// ContinueMeeting 恢复中断的会议：重试失败专家 + 继续剩余专家 + 总结
func (s *Service) ContinueMeeting(
	ctx context.Context,
	stockCode string,
	respCallback ResponseCallback,
	progressCallback ProgressCallback,
) ([]ChatResponse, error) {
	// 取出缓存状态
	s.meetingStatesMu.Lock()
	state, ok := s.meetingStates[stockCode]
	if ok {
		delete(s.meetingStates, stockCode)
	}
	s.meetingStatesMu.Unlock()

	if !ok || time.Since(state.CreatedAt) > MeetingStateTTL {
		return nil, fmt.Errorf("没有可恢复的会议状态")
	}

	log.Info("continuing meeting for %s, failedIndex=%d, total=%d",
		stockCode, state.FailedIndex, len(state.SelectedAgents))

	// 设置会议超时
	meetingCtx, meetingCancel := context.WithTimeout(ctx, MeetingTimeout)
	defer meetingCancel()

	responses := state.Responses
	history := state.History

	// 从失败的专家开始，依次执行
	startIndex := state.FailedIndex
	for i := startIndex; i < len(state.SelectedAgents); i++ {
		select {
		case <-meetingCtx.Done():
			log.Warn("continue meeting timeout, got %d responses", len(responses))
			return responses, ErrMeetingTimeout
		default:
		}

		agentCfg := state.SelectedAgents[i]
		log.Debug("continue: agent %d/%d: %s", i+1, len(state.SelectedAgents), agentCfg.Name)

		// 获取该专家的 AI 配置
		agentAIConfig := s.resolveAgentAIConfig(&agentCfg, state.AIConfig)

		agentLLM, err := s.modelFactory.CreateModel(meetingCtx, agentAIConfig)
		if err != nil {
			log.Error("continue: create agent LLM error: %v", err)
			continue
		}
		builder := s.createBuilder(agentLLM, agentAIConfig)

		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_start", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: agentCfg.Role,
		})

		previousContext := s.buildPreviousContext(history)
		if state.MemoryContext != "" {
			previousContext = state.MemoryContext + "\n" + previousContext
		}

		content, err := retryRun(meetingCtx, s.retryCount, func() (string, error) {
			agentCtx, agentCancel := context.WithTimeout(meetingCtx, AgentTimeout)
			defer agentCancel()
			return s.runSingleAgent(agentCtx, builder, &agentCfg, &state.Stock, state.Query, previousContext, "", progressCallback, state.Position)
		})

		if err != nil {
			emitProgress(progressCallback, ProgressEvent{Type: "agent_error", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: err.Error()})
			emitProgress(progressCallback, ProgressEvent{Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name})
			log.Error("continue: agent %s failed: %v", agentCfg.ID, err)

			failedResp := ChatResponse{
				AgentID: agentCfg.ID, AgentName: agentCfg.Name, Role: agentCfg.Role,
				Round: 1, MsgType: "opinion", Error: err.Error(), MeetingMode: MeetingModeSmart,
			}
			responses = append(responses, failedResp)
			if respCallback != nil {
				respCallback(failedResp)
			}

			// 再次缓存，允许用户继续重试
			s.cacheMeetingState(stockCode, &MeetingState{
				AIConfig:       state.AIConfig,
				Stock:          state.Stock,
				Query:          state.Query,
				Position:       state.Position,
				SelectedAgents: state.SelectedAgents,
				History:        history,
				Responses:      responses,
				FailedIndex:    i,
				MemoryContext:  state.MemoryContext,
				StockMemory:    state.StockMemory,
				Moderator:      state.Moderator,
				CreatedAt:      time.Now(),
			})

			remainingIDs := make([]string, 0, len(state.SelectedAgents)-i-1)
			for _, ra := range state.SelectedAgents[i+1:] {
				remainingIDs = append(remainingIDs, ra.ID)
			}
			emitProgress(progressCallback, ProgressEvent{
				Type: "meeting_interrupted", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
				Detail: err.Error(), Content: strings.Join(remainingIDs, ","),
			})
			break
		}

		emitProgress(progressCallback, ProgressEvent{Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name})

		resp := ChatResponse{
			AgentID: agentCfg.ID, AgentName: agentCfg.Name, Role: agentCfg.Role,
			Content: content, Round: 1, MsgType: "opinion", MeetingMode: MeetingModeSmart,
		}
		responses = append(responses, resp)
		if respCallback != nil {
			respCallback(resp)
		}

		history = append(history, DiscussionEntry{
			Round: 1, AgentID: agentCfg.ID, AgentName: agentCfg.Name,
			Role: agentCfg.Role, Content: content,
		})
	}

	// 检查是否再次中断
	s.meetingStatesMu.RLock()
	_, stillInterrupted := s.meetingStates[stockCode]
	s.meetingStatesMu.RUnlock()
	if stillInterrupted {
		return responses, nil
	}

	// 全部完成，执行小韭菜总结
	return s.runMeetingSummary(meetingCtx, state, history, responses, respCallback, progressCallback)
}

// runMeetingSummary 执行小韭菜总结（ContinueMeeting 专用）
func (s *Service) runMeetingSummary(
	ctx context.Context,
	state *MeetingState,
	history []DiscussionEntry,
	responses []ChatResponse,
	respCallback ResponseCallback,
	progressCallback ProgressCallback,
) ([]ChatResponse, error) {
	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_start", AgentID: ModeratorAgentID, AgentName: ModeratorName, Detail: "总结讨论",
	})

	summaryCtx, summaryCancel := context.WithTimeout(ctx, ModeratorTimeout)
	summary, err := state.Moderator.SummarizeWithContext(summaryCtx, &state.Stock, state.Query, history, buildSummaryContext("", &state.Stock, state.Position))
	summaryCancel()

	emitProgress(progressCallback, ProgressEvent{
		Type: "agent_done", AgentID: ModeratorAgentID, AgentName: ModeratorName,
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn("continue summary timeout")
		} else {
			log.Error("continue summary error: %v", err)
		}
		return responses, nil
	}

	if summary != "" {
		summaryResp := ChatResponse{
			AgentID: ModeratorAgentID, AgentName: ModeratorName,
			Role: ModeratorRole, Content: summary,
			Round: summaryRound(history), MsgType: "summary", MeetingMode: MeetingModeSmart,
		}
		responses = append(responses, summaryResp)
		if respCallback != nil {
			respCallback(summaryResp)
		}
	}

	// 异步保存记忆
	if s.memoryManager != nil && state.StockMemory != nil && summary != "" {
		go func() {
			bgCtx := context.Background()
			keyPoints := s.extractKeyPointsFromHistory(bgCtx, history)
			if err := s.memoryManager.AddRound(bgCtx, state.StockMemory, state.Query, summary, keyPoints); err != nil {
				log.Error("save memory error: %v", err)
			}
		}()
	}

	return responses, nil
}

func buildSummaryContext(coreContext string, stock *models.Stock, position *models.StockPosition) string {
	var sb strings.Builder
	if position != nil && position.Shares > 0 {
		currentPrice := 0.0
		symbol := ""
		name := ""
		if stock != nil {
			currentPrice = stock.Price
			symbol = strings.TrimSpace(stock.Symbol)
			name = strings.TrimSpace(stock.Name)
		}
		stockLabel := strings.TrimSpace(strings.TrimSpace(name + " " + symbol))
		if stockLabel == "" {
			stockLabel = "当前标的"
		}
		pnlText := "暂无盈亏数据"
		if currentPrice > 0 {
			pnlPerShare := currentPrice - position.CostPrice
			pnlTotal := pnlPerShare * float64(position.Shares)
			pnlRatio := 0.0
			if position.CostPrice > 0 {
				pnlRatio = pnlPerShare / position.CostPrice * 100
			}
			pnlText = fmt.Sprintf("浮盈亏 %.2f（%.2f%%）", pnlTotal, pnlRatio)
		}
		sb.WriteString("【用户持仓】\n")
		sb.WriteString(fmt.Sprintf("%s：持有 %d 股，成本价 %.2f，现价 %.2f，%s。\n", stockLabel, position.Shares, position.CostPrice, currentPrice, pnlText))
	}
	if strings.TrimSpace(coreContext) != "" {
		sb.WriteString("【核心数据包】\n")
		sb.WriteString(coreContext)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func (s *Service) fallbackAgents(all []models.AgentConfig, limit int) []models.AgentConfig {
	if limit <= 0 {
		limit = 1
	}

	result := make([]models.AgentConfig, 0, limit)
	for _, agent := range all {
		if !agent.Enabled {
			continue
		}
		result = append(result, agent)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func (s *Service) runSecondReviewRound(
	ctx context.Context,
	defaultAIConfig *models.AIConfig,
	stock *models.Stock,
	query string,
	selectedAgents []models.AgentConfig,
	history []DiscussionEntry,
	position *models.StockPosition,
	progressCallback ProgressCallback,
	meetingMode string,
	round int,
) []ChatResponse {
	if len(history) == 0 || len(selectedAgents) == 0 {
		return nil
	}
	if round <= 0 {
		round = 2
	}

	reviewQuery := buildSecondReviewQuery(query)
	responses := make([]ChatResponse, 0, len(selectedAgents))
	for _, agentCfg := range selectedAgents {
		if ctx.Err() != nil {
			break
		}

		agentAIConfig := s.resolveAgentAIConfig(&agentCfg, defaultAIConfig)
		agentLLM, err := s.modelFactory.CreateModel(ctx, agentAIConfig)
		if err != nil {
			log.Error("create revision agent LLM error: %v", err)
			continue
		}
		builder := s.createBuilder(agentLLM, agentAIConfig)

		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_start", AgentID: agentCfg.ID, AgentName: agentCfg.Name, Detail: "二轮复议",
		})
		previousContext := s.buildPreviousContext(history)
		content, err := retryRun(ctx, s.retryCount, func() (string, error) {
			agentCtx, cancel := context.WithTimeout(ctx, AgentTimeout)
			defer cancel()
			return s.runSingleAgent(agentCtx, builder, &agentCfg, stock, reviewQuery, previousContext, "", progressCallback, position)
		})
		emitProgress(progressCallback, ProgressEvent{
			Type: "agent_done", AgentID: agentCfg.ID, AgentName: agentCfg.Name,
		})
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}

		responses = append(responses, ChatResponse{
			AgentID:     agentCfg.ID,
			AgentName:   agentCfg.Name,
			Role:        agentCfg.Role,
			Content:     content,
			Round:       round,
			MsgType:     "opinion",
			MeetingMode: meetingMode,
		})
	}
	return responses
}

func buildSecondReviewQuery(query string) string {
	return "请基于用户原始问题和前面专家的观点进行一次简短复议。\n" +
		"如果你认为原判断成立，请明确说明并补充最关键的前提；如果需要修正，请直接给出修正后的结论和原因。\n" +
		"避免重复第一轮内容，控制在120字以内。\n\n用户问题：" + query
}

func summaryRound(history []DiscussionEntry) int {
	if len(history) == 0 {
		return 2
	}
	if history[len(history)-1].Round >= 2 {
		return history[len(history)-1].Round + 1
	}
	return 2
}
