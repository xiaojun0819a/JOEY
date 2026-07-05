import { isWailsGoReady } from '../utils/wailsEnv';

export interface BacktestRequest {
  days?: number;
  topN?: number;
  entryRule?: string;   // next_open | close
  maxMarketCap?: number;
  sellRule?: string;    // fast | patient
  maxPositions?: number;
  takeProfitPct?: number;
  stopLossPct?: number;
  costPct?: number;     // 单笔往返成本%，0=用真实散户口径默认
  gateMode?: string;
  universe?: string;
  engine?: string;
  maxChangePct?: number; // 低吸当日涨幅上限%（与扫描器一致）
}

export interface BacktestResult {
  startDate: string;
  endDate: string;
  tradingDays: number;
  totalTrades: number;
  winTrades: number;
  winRate: number;
  avgReturn: number;     // 期望值/笔
  avgWin: number;
  avgLoss: number;
  profitFactor: number;
  payoffRatio: number;
  maxLossPct: number;
  benchmarkPct: number;
  excessPct: number;     // 超额 alpha
  maxDrawdown: number;
  avgHoldDays: number;
  totalReturn: number;
  byReason: Record<string, number>;
  status: string;
  message: string;
}

type Bridge = {
  RunBacktest?: (req: BacktestRequest) => Promise<BacktestResult>;
  RunPortfolioBacktest?: (req: BacktestRequest) => Promise<BacktestResult>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

// 单笔胜率口径回测（期望值/赔率/超额）
export const runBacktest = async (req: BacktestRequest): Promise<BacktestResult | null> => {
  if (!isWailsGoReady() || !b().RunBacktest) return null;
  try { return await b().RunBacktest!(req); } catch { return null; }
};

// 真实组合回测（固定资金/限仓位，出真实净值曲线+回撤）
export const runPortfolioBacktest = async (req: BacktestRequest): Promise<BacktestResult | null> => {
  if (!isWailsGoReady() || !b().RunPortfolioBacktest) return null;
  try { return await b().RunPortfolioBacktest!(req); } catch { return null; }
};
