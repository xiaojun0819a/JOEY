import { GetOrCreateSession, GetSessionMessages, ClearSessionMessages, SendMeetingMessage, UpdateStockPosition, RetryAgent, RetryAgentAndContinue, CancelInterruptedMeeting } from '../../wailsjs/go/main/App';
import type { StockPosition } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export interface StockSession {
  id: string;
  stockCode: string;
  stockName: string;
  messages: ChatMessage[];
  position?: StockPosition; // 持仓信息
  createdAt: number;
  updatedAt: number;
}

export interface ChatMessage {
  id: string;
  agentId: string;
  agentName: string;
  role: string;
  content: string;
  timestamp: number;
  replyTo?: string;
  mentions?: string[];
  round?: number;
  msgType?: string;
  error?: string;  // 失败时的错误信息
  meetingMode?: string; // smart=串行, direct=独立
}

// 会议室消息请求
export interface MeetingMessageRequest {
  stockCode: string;
  content: string;
  mentionIds: string[];
  replyToId: string;
  replyContent: string;
}

// 获取或创建Session
export const getOrCreateSession = async (stockCode: string, stockName: string): Promise<StockSession> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('会话初始化', 'go');
    return {
      id: `browser:${stockCode}`,
      stockCode,
      stockName,
      messages: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
  }
  return await GetOrCreateSession(stockCode, stockName);
};

// 获取Session消息
export const getSessionMessages = async (stockCode: string): Promise<ChatMessage[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取会话消息', 'go');
    return [];
  }
  return await GetSessionMessages(stockCode);
};

// 清空Session消息
export const clearSessionMessages = async (stockCode: string): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('清空会话消息', 'go');
    return 'browser-mode:no-op';
  }
  return await ClearSessionMessages(stockCode);
};

// 发送会议室消息（@指定成员回复）
export const sendMeetingMessage = async (req: MeetingMessageRequest): Promise<ChatMessage[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('发起讨论', 'go');
    return [];
  }
  return await SendMeetingMessage(req);
};

// 更新股票持仓信息
export const updateStockPosition = async (stockCode: string, shares: number, costPrice: number): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('更新持仓', 'go');
    return 'browser-mode:no-op';
  }
  return await UpdateStockPosition(stockCode, shares, costPrice);
};

// 重试单个失败的专家
export const retryAgent = async (stockCode: string, agentId: string, query: string): Promise<ChatMessage> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('重试专家', 'go');
    return {
      id: `browser-retry-${Date.now()}`,
      agentId,
      agentName: '系统',
      role: '',
      content: '浏览器预览模式暂不支持重试专家',
      timestamp: Date.now(),
    };
  }
  return await RetryAgent(stockCode, agentId, query);
};

// 重试失败专家并继续执行剩余专家
export const retryAgentAndContinue = async (stockCode: string): Promise<ChatMessage[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('继续会议', 'go');
    return [];
  }
  return await RetryAgentAndContinue(stockCode);
};

// 取消中断的会议（用户放弃重试）
export const cancelInterruptedMeeting = async (stockCode: string): Promise<boolean> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('取消会议', 'go');
    return true;
  }
  return await CancelInterruptedMeeting(stockCode);
};
