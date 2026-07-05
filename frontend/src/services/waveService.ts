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

type GoBridge = {
  RunWaveScanner?: () => Promise<WaveScanResult>;
  RunWaveScannerWithGate?: (useGate: boolean) => Promise<WaveScanResult>;
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
