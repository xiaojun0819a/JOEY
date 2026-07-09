package main

// 访客数据隔离(headless 多用户):每个访客账号一个 App 分身。
// 共享内核:行情/历史/档案/新闻/工具/MCP/策略与专家配置/AI key(全部指向主人实例的同一份服务);
// 每用户私有:自选+分组(ConfigService 访客视图)、圆桌会话、交易台账、模拟盘(含看板/投研报告缓存)、
// 会议记忆、投研报告文件——全部落在 dataDir/users/<用户名>/ 下,互相不可见。
// 主人(JCP_TOKEN)继续用原 App 实例,数据留在原 dataDir,零迁移。

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/run-bigpig/jcp/internal/memory"
	"github.com/run-bigpig/jcp/internal/meeting"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/services"
)

// sanitizeUserDirName 把用户名转成安全的目录名(保留中英文数字与 _-,其余替换为 _)。
func sanitizeUserDirName(username string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, username)
}

// ForUser 基于主人实例创建某访客的分身 App。只应由 headless 的 RPC 分发器调用(每用户一次,缓存复用)。
func (a *App) ForUser(username string) (*App, error) {
	userDir := filepath.Join(paths.GetDataDir(), "users", sanitizeUserDirName(username))

	guestConfig, err := services.NewGuestConfigService(a.configService, userDir)
	if err != nil {
		return nil, err
	}
	sessionService := services.NewSessionService(userDir)
	journalService, err := services.NewJournalService(userDir)
	if err != nil {
		return nil, err
	}
	paperService, err := services.NewPaperService(userDir)
	if err != nil {
		return nil, err
	}

	// 会议服务按用户实例化:它持有记忆管理器,共享会让会议记忆串用户。
	// 工具/MCP/专家配置(strategyService)与 AI key(经 config 委托)仍是主人那份。
	meetingService := meeting.NewServiceFull(a.toolRegistry, a.mcpManager)
	cfg := a.configService.GetConfig()
	var memoryManager *memory.Manager
	if cfg.Memory.Enabled {
		memoryManager = memory.NewManagerWithConfig(userDir, memory.Config{
			MaxRecentRounds:   cfg.Memory.MaxRecentRounds,
			MaxKeyFacts:       cfg.Memory.MaxKeyFacts,
			MaxSummaryLength:  cfg.Memory.MaxSummaryLength,
			CompressThreshold: cfg.Memory.CompressThreshold,
		})
		meetingService.SetMemoryManager(memoryManager)
		if cfg.Memory.AIConfigID != "" {
			for i := range cfg.AIConfigs {
				if cfg.AIConfigs[i].ID == cfg.Memory.AIConfigID {
					meetingService.SetMemoryAIConfig(&cfg.AIConfigs[i])
					break
				}
			}
		}
	}
	if cfg.ModeratorAIID != "" {
		for i := range cfg.AIConfigs {
			if cfg.AIConfigs[i].ID == cfg.ModeratorAIID {
				meetingService.SetModeratorAIConfig(&cfg.AIConfigs[i])
				break
			}
		}
	} else {
		meetingService.SetModeratorAIConfig(nil)
	}
	meetingService.SetRetryCount(cfg.AIRetryCount)
	meetingService.SetVerboseAgentIO(cfg.VerboseAgentIO)
	meetingService.SetAgentSelectionStyle(cfg.AgentSelectionStyle)
	meetingService.SetEnableSecondReview(cfg.EnableSecondReview)

	return &App{
		ctx:           a.ctx,
		guestUsername: username,
		guestDataDir:  userDir,

		// 每用户私有
		configService:  guestConfig,
		sessionService: sessionService,
		journalService: journalService,
		paperService:   paperService,
		meetingService: meetingService,
		memoryManager:  memoryManager,

		// 共享内核(主人实例的服务指针)
		marketService:     a.marketService,
		newsService:       a.newsService,
		f10Service:        a.f10Service,
		historyService:    a.historyService,
		archiveService:    a.archiveService,
		pushService:       a.pushService,
		monitorService:    a.monitorService,
		hotTrendService:   a.hotTrendService,
		longHuBangService: a.longHuBangService,
		marketPusher:      a.marketPusher,
		strategyService:   a.strategyService,
		agentContainer:    a.agentContainer,
		toolRegistry:      a.toolRegistry,
		mcpManager:        a.mcpManager,
		updateService:     a.updateService,
		openClawServer:    a.openClawServer,

		meetingCancels:      make(map[string]context.CancelFunc),
		coreContextCacheTTL: defaultCoreContextCacheTTL,
		coreContextCache:    make(map[string]coreContextCacheEntry),
	}, nil
}
