import { GetStrategies, GetActiveStrategyID, SetActiveStrategy, AddStrategy, UpdateStrategy, DeleteStrategy, GenerateStrategy, EnhancePrompt, GetAgentConfigs, AddAgentConfig, UpdateAgentConfig, DeleteAgentConfig } from '../../wailsjs/go/main/App';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

// 策略专属专家配置
export interface StrategyAgent {
  id: string;
  name: string;
  role: string;
  avatar: string;
  color: string;
  instruction: string;
  tools: string[];
  mcpServers: string[];
  enabled: boolean;
  aiConfigId: string;
}

export interface Strategy {
  id: string;
  name: string;
  description: string;
  color: string;
  agents: StrategyAgent[];
  isBuiltin: boolean;
  source: string;
  sourceMeta: string;
  createdAt: number;
}

export interface GenerateStrategyRequest {
  prompt: string;
}

export interface GenerateStrategyResponse {
  success: boolean;
  error?: string;
  strategy?: Strategy;
  reasoning?: string;
}

// 获取所有策略
export const getStrategies = async (): Promise<Strategy[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取策略', 'go');
    return [];
  }
  return await GetStrategies();
};

// 获取当前激活策略ID
export const getActiveStrategyID = async (): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取激活策略', 'go');
    return '';
  }
  return await GetActiveStrategyID();
};

// 设置当前激活策略
export const setActiveStrategy = async (id: string): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('切换策略', 'go');
    return 'browser-mode:no-op';
  }
  return await SetActiveStrategy(id);
};

// 添加策略
export const addStrategy = async (strategy: Strategy): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('新增策略', 'go');
    return 'browser-mode:no-op';
  }
  return await AddStrategy(strategy as any);
};

// 更新策略
export const updateStrategy = async (strategy: Strategy): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('更新策略', 'go');
    return 'browser-mode:no-op';
  }
  return await UpdateStrategy(strategy as any);
};

// 删除策略
export const deleteStrategy = async (id: string): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('删除策略', 'go');
    return 'browser-mode:no-op';
  }
  return await DeleteStrategy(id);
};

// AI生成策略
export const generateStrategy = async (prompt: string): Promise<GenerateStrategyResponse> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('策略生成', 'go');
    return { success: false, error: '浏览器预览模式暂不支持策略生成' };
  }
  return await GenerateStrategy({ prompt });
};

// 提示词增强请求
export interface EnhancePromptRequest {
  originalPrompt: string;
  agentRole: string;
  agentName: string;
}

// 提示词增强响应
export interface EnhancePromptResponse {
  success: boolean;
  enhancedPrompt?: string;
  error?: string;
}

// 增强Agent提示词
export const enhancePrompt = async (req: EnhancePromptRequest): Promise<EnhancePromptResponse> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('提示词增强', 'go');
    return { success: false, error: '浏览器预览模式暂不支持提示词增强' };
  }
  return await EnhancePrompt(req);
};

// ========== Agent Config API ==========

export interface AgentConfig {
  id: string;
  name: string;
  role: string;
  avatar: string;
  color: string;
  instruction: string;
  tools: string[];
  mcpServers: string[];
  enabled: boolean;
  aiConfigId: string;
}

// 获取所有已启用的Agent配置
export const getAgentConfigs = async (): Promise<AgentConfig[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取专家配置', 'go');
    return [];
  }
  return await GetAgentConfigs();
};

// 添加Agent配置
export const addAgentConfig = async (config: AgentConfig): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('新增专家配置', 'go');
    return 'browser-mode:no-op';
  }
  return await AddAgentConfig(config);
};

// 更新Agent配置
export const updateAgentConfig = async (config: AgentConfig): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('更新专家配置', 'go');
    return 'browser-mode:no-op';
  }
  return await UpdateAgentConfig(config);
};

// 删除Agent配置
export const deleteAgentConfig = async (id: string): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('删除专家配置', 'go');
    return 'browser-mode:no-op';
  }
  return await DeleteAgentConfig(id);
};
