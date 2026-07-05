import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';
import type {
  HistoryAutoCollectRequest,
  HistoryAutoCollectStatus,
  HistoryCollectRequest,
  HistoryCollectResult,
  LowBuyScannerRequest,
  LowBuyScannerResult,
} from '../components/LowBuyScannerDialog';
import type {
  LateDayChaseScannerRequest,
  LateDayChaseScannerResult,
} from '../components/LateDayChaseScannerDialog';

type GoBridge = {
  RunLowBuyScannerV1?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunLimitPullbackScanner?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunTripleVolumeScannerV5?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunTailBuyScannerV6?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunHotMoneyBreakoutScannerV7?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunDipEntryScannerV8?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunMonsterScannerV9?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunMonsterScannerV10?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunTailLazyScannerV2?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunCaoYuanStandardScanner4A?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunCaoYuanZhuangScanner4B?: (req: LowBuyScannerRequest) => Promise<LowBuyScannerResult>;
  RunTailLazyReplayOnDate?: (date: string, limit: number) => Promise<LowBuyScannerResult>;
  RunLowBuyReplayOnDate?: (date: string, limit: number, maxChangePct: number) => Promise<LowBuyScannerResult>;
  RunTailLazyBatchReplay?: (start: string, end: string) => Promise<TailLazyBatchResult>;
  RunLowBuyBatchReplay?: (start: string, end: string, topN: number, maxChangePct: number) => Promise<LowBuyBatchResult>;
  RunLateDayChaseScanner?: (req: LateDayChaseScannerRequest) => Promise<LateDayChaseScannerResult>;
  CollectDailyHistory?: (req: HistoryCollectRequest) => Promise<HistoryCollectResult>;
  GetHistoryAutoCollectStatus?: () => Promise<HistoryAutoCollectStatus>;
  UpdateHistoryAutoCollect?: (req: HistoryAutoCollectRequest) => Promise<HistoryAutoCollectStatus>;
};

export interface TailLazyBatchRow {
  label: string;
  samples: number;
  hit3Rate: number;
  hit5Rate: number;
  avgHigh: number;
  avgOpen: number;
  avgClose: number;
  tpWinRate: number;
  tpExpectancy: number;
}
export interface TailLazyBatchResult {
  start: string;
  end: string;
  rows: TailLazyBatchRow[];
  warning: string;
}

export interface LowBuyBatchRow {
  label: string;
  trades: number;
  winRate: number;
  expectancy: number;
  payoffRatio: number;
  profitFactor: number;
  maxLoss: number;
  benchmark: number;
  excess: number;
  avgHold: number;
}
export interface LowBuyBatchResult {
  start: string;
  end: string;
  topN: number;
  rows: LowBuyBatchRow[];
  warning: string;
}

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

// 涨停回调低吸：近期涨停强启动后，等待缩量回踩和均线承接。
export const runLimitPullbackScanner = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('涨停回调低吸扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunLimitPullbackScanner) {
    throw new Error('当前版本未暴露 RunLimitPullbackScanner 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunLimitPullbackScanner(req);
};

// 三倍量策略5：未涨停阳线 + 成交量>=前一日3倍 + 一阳穿MA5/10/20/30。
export const runTripleVolumeScannerV5 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('三倍量策略5扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunTripleVolumeScannerV5) {
    throw new Error('当前版本未暴露 RunTripleVolumeScannerV5 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunTripleVolumeScannerV5(req);
};

export const runTailBuyScannerV6 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('尾盘买入策略6扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunTailBuyScannerV6) {
    throw new Error('当前版本未暴露 RunTailBuyScannerV6 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunTailBuyScannerV6(req);
};

export const runHotMoneyBreakoutScannerV7 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('游资突破策略7扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunHotMoneyBreakoutScannerV7) {
    throw new Error('当前版本未暴露 RunHotMoneyBreakoutScannerV7 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunHotMoneyBreakoutScannerV7(req);
};

export const runDipEntryScannerV8 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('低吸入场策略8扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunDipEntryScannerV8) {
    throw new Error('当前版本未暴露 RunDipEntryScannerV8 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunDipEntryScannerV8(req);
};

export const runMonsterScannerV9 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('捉妖策略9扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunMonsterScannerV9) {
    throw new Error('当前版本未暴露 RunMonsterScannerV9 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunMonsterScannerV9(req);
};

export const runMonsterScannerV10 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('捉妖策略10扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunMonsterScannerV10) {
    throw new Error('当前版本未暴露 RunMonsterScannerV10 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunMonsterScannerV10(req);
};

// 尾盘懒人策略2（打强：量比1-2.5 / 涨幅3-6% / 换手5-10% / 多头排列 / 新高 / 形态）
export const runTailLazyScannerV2 = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('尾盘懒人扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunTailLazyScannerV2) {
    throw new Error('当前版本未暴露 RunTailLazyScannerV2 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunTailLazyScannerV2(req);
};

// 草元标准4A（normal反推：深度超跌 + 贴近地板 + 当日止跌）
export const runCaoYuanStandardScanner4A = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('草元标准4A扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunCaoYuanStandardScanner4A) {
    throw new Error('当前版本未暴露 RunCaoYuanStandardScanner4A 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunCaoYuanStandardScanner4A(req);
};

// 草元抓庄4B（ZZ反推：90日涨停记忆 + 深跌企稳 + 高控盘代理）
export const runCaoYuanZhuangScanner4B = async (req: LowBuyScannerRequest): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('草元抓庄4B扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunCaoYuanZhuangScanner4B) {
    throw new Error('当前版本未暴露 RunCaoYuanZhuangScanner4B 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunCaoYuanZhuangScanner4B(req);
};

// 尾盘懒人 · 历史复盘：指定交易日筛选 + 次日表现
export const runTailLazyReplayOnDate = async (date: string, limit = 30): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('尾盘懒人历史复盘', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunTailLazyReplayOnDate) {
    throw new Error('当前版本未暴露 RunTailLazyReplayOnDate 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunTailLazyReplayOnDate(date, limit);
};

// 低吸 · 单日历史复盘：指定日Top + 机械纪律持有结果
export const runLowBuyReplayOnDate = async (date: string, limit = 30, maxChangePct = 1.5): Promise<LowBuyScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('低吸单日复盘', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunLowBuyReplayOnDate) {
    throw new Error('当前版本未暴露 RunLowBuyReplayOnDate 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunLowBuyReplayOnDate(date, limit, maxChangePct);
};

// 尾盘懒人 · 批量历史复盘：区间整体真实命中率/期望
export const runTailLazyBatchReplay = async (start: string, end: string): Promise<TailLazyBatchResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('尾盘懒人批量复盘', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunTailLazyBatchReplay) {
    throw new Error('当前版本未暴露 RunTailLazyBatchReplay 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunTailLazyBatchReplay(start, end);
};

// 低吸 · 批量历史复盘：区间整体胜率/期望/赔率/超额alpha
export const runLowBuyBatchReplay = async (start: string, end: string, topN = 3, maxChangePct = 1.5): Promise<LowBuyBatchResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('低吸批量复盘', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunLowBuyBatchReplay) {
    throw new Error('当前版本未暴露 RunLowBuyBatchReplay 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunLowBuyBatchReplay(start, end, topN, maxChangePct);
};

export const runLateDayChaseScanner = async (req: LateDayChaseScannerRequest): Promise<LateDayChaseScannerResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('尾盘强势股扫描', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.RunLateDayChaseScanner) {
    throw new Error('当前版本未暴露 RunLateDayChaseScanner 接口，请重启 Wails 开发服务');
  }
  return await bridge.RunLateDayChaseScanner(req);
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
