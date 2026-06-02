import { CheckForUpdate, DoUpdate, RestartApp, GetCurrentVersion } from '../../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';
import { isWailsBridgeReady, isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export interface UpdateInfo {
  hasUpdate: boolean;
  latestVersion: string;
  currentVersion: string;
  releaseUrl: string;
  releaseNotes: string;
  error?: string;
}

export interface UpdateProgress {
  status: 'checking' | 'downloading' | 'installing' | 'completed' | 'error';
  message: string;
  percent: number;
}

export async function checkForUpdate(): Promise<UpdateInfo> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('检查更新', 'go');
    return {
      hasUpdate: false,
      latestVersion: '',
      currentVersion: 'browser',
      releaseUrl: '',
      releaseNotes: '',
      error: '浏览器预览模式暂不支持更新',
    };
  }
  return await CheckForUpdate();
}

export async function doUpdate(): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('执行更新', 'go');
    return 'browser-mode:no-op';
  }
  return await DoUpdate();
}

export async function restartApp(): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('重启应用', 'go');
    return 'browser-mode:no-op';
  }
  return await RestartApp();
}

export async function getCurrentVersion(): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取版本', 'go');
    return 'browser';
  }
  return await GetCurrentVersion();
}

export function onUpdateProgress(callback: (progress: UpdateProgress) => void): () => void {
  if (!isWailsBridgeReady()) {
    warnWailsUnavailable('更新进度订阅', 'both');
    return () => {};
  }
  EventsOn('update:progress', callback);
  return () => EventsOff('update:progress');
}
