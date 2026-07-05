import { isWailsGoReady } from '../utils/wailsEnv';

export interface AccountHolding {
  symbol: string;
  name: string;
  entryDate: string;
  entryPrice: number;
  currentPrice: number;
  holdDays: number;
  unrealizedPct: number;
  value: number;
}
export interface AccountTrade {
  symbol: string;
  name: string;
  entryDate: string;
  exitDate: string;
  entryPrice: number;
  exitPrice: number;
  holdDays: number;
  returnPct: number;
  exitReason: string;
}
export interface AccountEquityPoint {
  date: string;
  value: number;
}
export interface StrategyAccountResult {
  strategy: string;
  capital: number;
  startDate: string;
  endDate: string;
  finalEquity: number;
  returnPct: number;
  maxDrawdown: number;
  benchmark: number;
  excess: number;
  cash: number;
  closedTrades: number;
  winRate: number;
  expectancy: number;
  payoffRatio: number;
  profitFactor: number;
  avgHoldDays: number;
  holdings: AccountHolding[];
  trades: AccountTrade[];
  equity: AccountEquityPoint[];
  warning: string;
}

type Bridge = {
  RunStrategyAccount?: (strategy: string, days: number) => Promise<StrategyAccountResult>;
  RunStrategyAccountRisk?: (strategy: string, days: number) => Promise<StrategyAccountResult>;
  RunPaperStrategyAccount?: (source: string) => Promise<StrategyAccountResult>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

export const runStrategyAccount = async (strategy: string, days = 250, useRisk = false): Promise<StrategyAccountResult | null> => {
  if (!isWailsGoReady()) return null;
  try {
    if (useRisk && b().RunStrategyAccountRisk) return await b().RunStrategyAccountRisk!(strategy, days);
    if (!b().RunStrategyAccount) return null;
    return await b().RunStrategyAccount!(strategy, days);
  } catch { return null; }
};

// 实盘跟踪账户：由我加进模拟持仓的票按策略(source)分组驱动
export const runPaperStrategyAccount = async (source: string): Promise<StrategyAccountResult | null> => {
  if (!isWailsGoReady() || !b().RunPaperStrategyAccount) return null;
  try { return await b().RunPaperStrategyAccount!(source); } catch { return null; }
};

export const EXIT_REASON_CN = (r?: string): string => {
  if (!r) return '';
  const half = r.startsWith('half_');
  const key = half ? r.slice(5) : r;
  const m: Record<string, string> = { stop_loss: '止损-5%', ma10: '破10线', turnover: '换手>12%', time_stop: '5日<3%', take_profit: '止盈+15%', window_end: '到期', manual: '手动平仓' };
  const base = m[key] || key;
  return half ? `减半·${base}` : base;
};
