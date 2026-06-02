import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';
import type {
  HistoryAutoCollectRequest,
  HistoryAutoCollectStatus,
  HistoryCollectRequest,
  HistoryCollectResult,
  LowBuyScannerRequest,
  LowBuyScannerResult,
} from '../components/LowBuyScannerDialog';

type GoBridge = {
  RunLowBuyScannerV1?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  CollectDailyHistory?: (req: HistoryCollectRequest) => Promise<HistoryCollectResult>;
  GetHistoryAutoCollectStatus?: () => Promise<HistoryAutoCollectStatus>;
  UpdateHistoryAutoCollect?: (req: HistoryAutoCollectRequest) => Promise<HistoryAutoCollectStatus>;
};

const getGoBridge = (): GoBridge => {
  const win = window as unknown as { go?: { main?: { App?: GoBridge } } };
  return win.go?.main?.App || {};
};

export const runLowBuyScannerV1 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('低吸选股扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunLowBuyScannerV1) {
    throw new Error('当前版本未暴露 RunLowBuyScannerV1 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunLowBuyScannerV1(req);
};

export const collectDailyHistory = async (req: HistoryCollectRequest): Promise<HistoryCollectResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('历史数据采集', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.CollectDailyHistory) {
    throw new Error('当前版本未暴露 CollectDailyHistory 接口，请重启 Wails 开发服务');
  }
  return await bridge.CollectDailyHistory(req);
};

export const getHistoryAutoCollectStatus = async (): Promise<HistoryAutoCollectStatus | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('历史自动采集状态', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.GetHistoryAutoCollectStatus) {
    throw new Error('当前版本未暴露 GetHistoryAutoCollectStatus 接口，请重启 Wails 开发服务');
  }
  return await bridge.GetHistoryAutoCollectStatus();
};

export const updateHistoryAutoCollect = async (req: HistoryAutoCollectRequest): Promise<HistoryAutoCollectStatus | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('历史自动采集配置', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.UpdateHistoryAutoCollect) {
    throw new Error('当前版本未暴露 UpdateHistoryAutoCollect 接口，请重启 Wails 开发服务');
  }
  return await bridge.UpdateHistoryAutoCollect(req);
};
