// 推送服务 - 调用后端 API
import { TestPush, PushSignal, RunPositionMonitorOnce } from '@wailsjs/go/main/App';
import type { models } from '@wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type PushResult = models.PushResult;
export type PushSignalInput = models.PushSignal;

// 发送测试推送，验证各渠道配置
export const testPush = async (): Promise<PushResult> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('测试推送', 'go');
    return { sent: false, skipped: false, channels: {}, message: '浏览器预览模式暂不支持测试推送' } as PushResult;
  }
  return await TestPush();
};

// 发送一条信号推送
export const pushSignal = async (signal: PushSignalInput): Promise<PushResult> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('信号推送', 'go');
    return { sent: false, skipped: false, channels: {}, message: '浏览器预览模式暂不支持推送' } as PushResult;
  }
  return await PushSignal(signal);
};

// 立即跑一次盘中持仓监控，返回触发信号数
export const runPositionMonitorOnce = async (): Promise<number> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('持仓监控', 'go');
    return 0;
  }
  return await RunPositionMonitorOnce();
};
