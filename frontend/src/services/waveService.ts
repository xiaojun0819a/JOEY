import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type WaveCandidate = {
  code: string;
  name: string;
  price: number;
  kongpan: number; // 控盘度
  ignite: boolean; // 资金点火
  date: string;
  score: number;
  level: string;
  phase: string;
  eatFish: boolean;
  relaxedIgnite: boolean;
  strictIgnite: boolean;
  recentIgnite: boolean;
  mainOpenFish: boolean;
  timelyTakeProfit: boolean;
  breakTakeProfit: boolean;
  strongSignal: boolean;
  strongCount: number;
  mainRise: boolean;
  mainControlStart: boolean;
  mainControlReduce: boolean;
  buyState: boolean;
  trendBull: boolean;
  energyBull: boolean;
  midBull: boolean;
  shortBull: boolean;
  gz: boolean;
  keyLayout?: boolean;        // 重点布局:日K+30分+60分三周期共振
  keyLayoutTF?: string[] | null; // 已确认共振周期
  reasons?: string[] | null;
  risks?: string[] | null;
};

export type WaveScanResult = {
  asOf: string;
  snapshotAsOf?: string;
  dataSource?: string;
  universeCount?: number;
  scannedCount?: number;
  preheatDays?: number;
  patchedCount?: number;
  recentKCount?: number;
  gatePassed: boolean;
  gateBypassed?: boolean;
  count: number;
  items: WaveCandidate[] | null;
  message: string;
};

export type WaveScanStatus = {
  running: boolean;
  ageSec: number; // 最近一次完成距今秒数,-1=从未完成
  result?: WaveScanResult | null;
};

type GoBridge = {
  RunWaveScanner?: () => Promise<WaveScanResult>;
  RunWaveScannerWithGate?: (useGate: boolean) => Promise<WaveScanResult>;
  GetWaveScanStatus?: () => Promise<WaveScanStatus>;
};

const getGoBridge = (): GoBridge => {
  const win = window as unknown as { go?: { main?: { App?: GoBridge } } };
  return win.go?.main?.App || {};
};

export const runWaveScanner = async (): Promise<WaveScanResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('波段选股扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunWaveScanner) {
    throw new Error('当前版本未暴露 RunWaveScanner 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunWaveScanner();
};

export const runWaveScannerWithGate = async (useGate: boolean): Promise<WaveScanResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('波段选股扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (bridge.RunWaveScannerWithGate) {
    return await bridge.RunWaveScannerWithGate(useGate);
  }
  if (useGate && bridge.RunWaveScanner) {
    return await bridge.RunWaveScanner();
  }
  throw new Error('当前版本未暴露临时闸门接口，请重启 Wails 开发服务');
};

// 查询扫描状态(轻量):断线找回用——长扫描请求被隧道掐断后,服务端仍会完成并缓存结果
export const getWaveScanStatus = async (): Promise<WaveScanStatus | null> => {
  if (!isWailsGoReady()) return null;
  const bridge = getGoBridge();
  if (!bridge.GetWaveScanStatus) return null;
  try { return await bridge.GetWaveScanStatus(); } catch { return null; }
};
