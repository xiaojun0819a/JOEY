import { isWailsGoReady } from '../utils/wailsEnv';
import { STRATEGY_SOURCE_FILTERS, STRATEGY_SOURCE_LABELS, getStrategySourceLabel } from '../utils/strategySource';

export interface PaperPosition {
  id: number;
  symbol: string;
  name: string;
  source: string;
  costPrice: number;
  shares: number;
  openDate: string;
  openPrice: number;
  status: string; // open/closed
  closePrice: number;
  closeDate: string;
  exitReason?: string; // 自动平仓原因
  currentPrice?: number;
  profitPct?: number;
  profitAmount?: number;
  riskKind?: string;
  stopPrice?: number;
  tpPrice?: number;
}

export interface RiskConcentration { name: string; pct: number; }
export interface PaperRiskSummary {
  positionCount: number;
  totalCost: number;
  totalValue: number;
  profitPct: number;
  singleCap: number;
  sectorCap: number;
  drawdownAlertPct: number;
  maxSinglePct: number;
  singleOver: RiskConcentration[];
  sectorTop: RiskConcentration[];
  sectorOver: RiskConcentration[];
  peakValue: number;
  drawdownFromPeak: number;
  drawdownAlert: boolean;
  warnings: string[];
}

export interface PaperSourceStat {
  source: string;
  total: number;
  closed: number;
  win: number;
  winRate: number;
  avgReturn: number;
  totalReturn: number;
  avgWin: number;
  avgLoss: number;
  payoffRatio: number;
  profitFactor: number;
  maxLoss: number;
}

export interface PaperStats {
  openCount: number;
  closedCount: number;
  winRate: number;
  expectancy: number;
  payoffRatio: number;
  profitFactor: number;
  maxLoss: number;
  bySource: PaperSourceStat[];
}

type Bridge = {
  AddPaperPosition?: (symbol: string, name: string, source: string, costPrice: number, shares: number) => Promise<string>;
  ListPaperPositions?: () => Promise<PaperPosition[]>;
  UpdatePaperPosition?: (id: number, costPrice: number, shares: number) => Promise<string>;
  ClosePaperPosition?: (id: number, closePrice: number) => Promise<string>;
  ReopenPaperPosition?: (id: number) => Promise<string>;
  DeletePaperPosition?: (id: number) => Promise<string>;
  GetPaperStats?: () => Promise<PaperStats>;
  ApplyPaperExitRules?: () => Promise<number>;
  GetPaperRiskSummary?: () => Promise<PaperRiskSummary>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

export const addPaperPosition = async (symbol: string, name: string, source: string, costPrice: number, shares = 1000) => {
  if (!isWailsGoReady() || !b().AddPaperPosition) return;
  try { await b().AddPaperPosition!(symbol, name, source, costPrice, shares); } catch { /* ignore */ }
};

export const listPaperPositions = async (): Promise<PaperPosition[]> => {
  if (!isWailsGoReady() || !b().ListPaperPositions) return [];
  try { return (await b().ListPaperPositions!()) || []; } catch { return []; }
};

export const updatePaperPosition = async (id: number, costPrice: number, shares: number) => {
  if (!isWailsGoReady() || !b().UpdatePaperPosition) return;
  try { await b().UpdatePaperPosition!(id, costPrice, shares); } catch { /* ignore */ }
};

export const closePaperPosition = async (id: number, closePrice: number) => {
  if (!isWailsGoReady() || !b().ClosePaperPosition) return;
  try { await b().ClosePaperPosition!(id, closePrice); } catch { /* ignore */ }
};

export const reopenPaperPosition = async (id: number) => {
  if (!isWailsGoReady() || !b().ReopenPaperPosition) return;
  try { await b().ReopenPaperPosition!(id); } catch { /* ignore */ }
};

export const deletePaperPosition = async (id: number) => {
  if (!isWailsGoReady() || !b().DeletePaperPosition) return;
  try { await b().DeletePaperPosition!(id); } catch { /* ignore */ }
};

export const getPaperRiskSummary = async (): Promise<PaperRiskSummary | null> => {
  if (!isWailsGoReady() || !b().GetPaperRiskSummary) return null;
  try { return await b().GetPaperRiskSummary!(); } catch { return null; }
};

export const getPaperStats = async (): Promise<PaperStats | null> => {
  if (!isWailsGoReady() || !b().GetPaperStats) return null;
  try {
    const s = await b().GetPaperStats!();
    if (!s) return null;
    return {
      openCount: s.openCount ?? 0,
      closedCount: s.closedCount ?? 0,
      winRate: s.winRate ?? 0,
      expectancy: s.expectancy ?? 0,
      payoffRatio: s.payoffRatio ?? 0,
      profitFactor: s.profitFactor ?? 0,
      maxLoss: s.maxLoss ?? 0,
      bySource: Array.isArray(s.bySource) ? s.bySource : [],
    };
  } catch { return null; }
};

// 按低吸退出纪律自动平仓（用真实前向日K，仅确认收盘），返回平仓笔数
export const applyPaperExitRules = async (): Promise<number> => {
  if (!isWailsGoReady() || !b().ApplyPaperExitRules) return 0;
  try { return (await b().ApplyPaperExitRules!()) || 0; } catch { return 0; }
};

// 自动平仓原因 → 中文
export const EXIT_REASON_LABEL: Record<string, string> = {
  stop_loss: '止损-5%',
  ma10: '破10日线',
  ma20: '破20日线',
  turnover: '换手>12%',
  time_stop: '5日<3%',
  take_profit: '止盈+15%',
  window_end: '到期',
};
export const exitReasonText = (reason?: string): string => {
  if (!reason) return '';
  const half = reason.startsWith('half_');
  const key = half ? reason.slice(5) : reason;
  const base = EXIT_REASON_LABEL[key] || key;
  return half ? `减半·${base}` : base;
};

export const SOURCE_LABEL: Record<string, string> = {
  ...STRATEGY_SOURCE_LABELS,
};

export { STRATEGY_SOURCE_FILTERS, getStrategySourceLabel };
