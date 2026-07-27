import { isWailsGoReady } from '../utils/wailsEnv';
import { exitReasonLabel } from '../utils/exitReason';

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

// 策略账户成交记录的离场原因。这里空 = 无(手动平仓在库里是字面量 "manual",见 app.go 的
// chooseFirstNonEmpty(p.ExitReason, "manual")),所以不套模拟持仓那套"空=手动"的默认值。
export const EXIT_REASON_CN = (r?: string): string => (r ? exitReasonLabel(r) : '');
