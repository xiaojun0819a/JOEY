// 配置服务 - 调用后端API
import { GetConfig, UpdateConfig, GetAvailableTools, TestAIConnection } from '@wailsjs/go/main/App';
import type { models } from '@wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type AppConfig = models.AppConfig;

// 内置工具信息
export interface ToolInfo {
  name: string;
  description: string;
}

const DEFAULT_APP_CONFIG: AppConfig = {
  theme: 'military',
  candleColorMode: 'red-up',
  aiConfigs: [],
  defaultAiId: '',
  strategyAiId: '',
  moderatorAiId: '',
  aiRetryCount: 2,
  verboseAgentIO: false,
  agentSelectionStyle: 'balanced',
  enableSecondReview: false,
  mcpServers: [],
  memory: {
    enabled: true,
    aiConfigId: '',
    maxRecentRounds: 3,
    maxKeyFacts: 20,
    maxSummaryLength: 300,
    compressThreshold: 5,
  } as any,
  proxy: {
    mode: 'none',
    customUrl: '',
  } as any,
  layout: {
    leftPanelWidth: 280,
    rightPanelWidth: 384,
    bottomPanelHeight: 132,
    windowWidth: 0,
    windowHeight: 0,
  } as any,
  openClaw: {
    enabled: false,
    port: 51888,
    apiKey: '',
  } as any,
  indicators: {
    ma: { enabled: true, periods: [5, 10, 20] },
    ema: { enabled: false, periods: [12, 26] },
    boll: { enabled: false, period: 20, multiplier: 2 },
    macd: { enabled: true, fast: 12, slow: 26, signal: 9 },
    rsi: { enabled: false, period: 14 },
    kdj: { enabled: false, period: 9, k: 3, d: 3 },
  } as any,
} as unknown as AppConfig;

export const getConfig = async (): Promise<AppConfig> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取配置', 'go');
    return DEFAULT_APP_CONFIG;
  }
  return await GetConfig();
};

export const updateConfig = async (config: AppConfig): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('保存配置', 'go');
    return 'browser-mode:no-op';
  }
  return await UpdateConfig(config);
};

// 获取可用的内置工具列表
export const getAvailableTools = async (): Promise<ToolInfo[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('内置工具列表', 'go');
    return [];
  }
  return await GetAvailableTools();
};

// 测试 AI 配置连通性
export const testAIConnection = async (config: models.AIConfig): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('AI连通性测试', 'go');
    return '浏览器预览模式暂不支持测试连接';
  }
  return await TestAIConnection(config);
};
