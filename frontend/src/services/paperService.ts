import { isWailsGoReady } from '../utils/wailsEnv';
import { STRATEGY_SOURCE_FILTERS, STRATEGY_SOURCE_LABELS, getStrategySourceLabel, sourceMatchesStrategyKey } from '../utils/strategySource';
import { exitReasonLabel } from '../utils/exitReason';

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
  openedAt?: string; // 建仓精确时刻 "YYYY-MM-DD HH:mm:ss"(旧数据为空)
  closedAt?: string; // 平仓精确时刻(仅盘中实时平仓有)
  addedBy?: string; // auto=策略自动入盘 / manual=手动(含从策略列表点加);旧数据为空
  autoExitOff?: boolean; // true=撤回过自动平仓(用户接管):风控引擎不再自动止盈止损
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
  SetPaperAutoExit?: (id: number, off: boolean) => Promise<string>;
  DeletePaperPosition?: (id: number) => Promise<string>;
  ClearAllPaperPositions?: () => Promise<string>;
  ClearPaperPositionsByIDs?: (ids: number[]) => Promise<string>;
  GetPaperAutoPaused?: () => Promise<boolean>;
  SetPaperAutoPaused?: (paused: boolean) => Promise<string>;
  GetPaperStats?: () => Promise<PaperStats>;
  ApplyPaperExitRules?: () => Promise<number>;
  GetPaperRiskSummary?: () => Promise<PaperRiskSummary>;
  AddTipPicks?: (raw: string, amountPerStock: number, note: string) => Promise<TipPickResult>;
};

// 外部荐股跟单(见 app_tip_picks.go)。成交价=提交时刻实时价,dayOpen 仅供对照。
export interface TipPickRow {
  symbol: string;
  name: string;
  price: number;
  dayOpen: number;
  shares: number;
  amount: number;
  skipped: string; // 非空 = 没记进去的原因
}
export interface TipPickResult {
  date: string;
  at: string;
  source: string;
  note: string;
  rows: TipPickRow[];
  added: number;
  skipped: number;
  warning: string;
}

// addTipPicks 把荐股原文里的代码按固定金额批量建仓。后端会拒绝非交易时段与涨停封死的票,
// 错误原样抛出给调用方显示——这些拒绝正是数据可信的前提,不要吞掉。
export const addTipPicks = async (raw: string, amountPerStock: number, note: string): Promise<TipPickResult> => {
  if (!isWailsGoReady() || !b().AddTipPicks) throw new Error('后端未就绪');
  return await b().AddTipPicks!(raw, amountPerStock, note);
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

export const reopenPaperPosition = async (id: number): Promise<string> => {
  if (!isWailsGoReady() || !b().ReopenPaperPosition) return '';
  try { return (await b().ReopenPaperPosition!(id)) || ''; } catch { return ''; }
};

// 恢复/停用某持仓的自动风控(off=true 引擎跳过该持仓,即「接管」)
export const setPaperAutoExit = async (id: number, off: boolean) => {
  if (!isWailsGoReady() || !b().SetPaperAutoExit) return;
  try { await b().SetPaperAutoExit!(id, off); } catch { /* ignore */ }
};

export const deletePaperPosition = async (id: number) => {
  if (!isWailsGoReady() || !b().DeletePaperPosition) return;
  try { await b().DeletePaperPosition!(id); } catch { /* ignore */ }
};

// 一键清空全部模拟持仓(含已平仓历史+权益曲线+「模拟持仓」分组成员),重新开始
export const clearAllPaperPositions = async (): Promise<string> => {
  if (!isWailsGoReady() || !b().ClearAllPaperPositions) return 'unavailable';
  try { return await b().ClearAllPaperPositions!(); } catch (e) { return String(e); }
};

// 按 ID 批量清除(供"只清当前来源":传当前筛选可见的持仓ID,语义与列表所见一致)
export const clearPaperPositionsByIDs = async (ids: number[]): Promise<string> => {
  if (!ids.length) return 'success:0';
  if (!isWailsGoReady() || !b().ClearPaperPositionsByIDs) return 'unavailable';
  try { return await b().ClearPaperPositionsByIDs!(ids); } catch (e) { return String(e); }
};

// 自动入盘总开关:true=暂停(策略盘后自动扫描不建仓);手动添加不受影响
export const getPaperAutoPaused = async (): Promise<boolean> => {
  if (!isWailsGoReady() || !b().GetPaperAutoPaused) return false;
  try { return await b().GetPaperAutoPaused!(); } catch { return false; }
};
export const setPaperAutoPaused = async (paused: boolean): Promise<string> => {
  if (!isWailsGoReady() || !b().SetPaperAutoPaused) return 'unavailable';
  try { return await b().SetPaperAutoPaused!(paused); } catch (e) { return String(e); }
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

// 模拟持仓的平仓原因 → 中文。映射表见 utils/exitReason(三处共用的单一真相源)。
//
// 库里 exit_reason 为空 = 手动平仓。依据:全项目只有三处写平仓状态
// (CloseOn / ClosePosition / Reopen),风控引擎走 CloseOn 且 reason 必然非空
// (要么来自覆盖全部三种 kind 的映射,要么是 paperExitAt 的字面量),
// 只有「平仓」按钮走的 ClosePaperPosition 硬传 ""。所以空理由必是手动平的,如实标出来——
// 否则手动平和"自动平了却没记理由"在界面上长得一模一样,分不清一笔是纪律执行的结果
// 还是自己拍脑袋卖的,计分卡的胜率也就归不到策略头上。
export const EXIT_MANUAL_LABEL = '手动';
export const exitReasonText = (reason?: string): string =>
  reason ? exitReasonLabel(reason) : EXIT_MANUAL_LABEL;

export const SOURCE_LABEL: Record<string, string> = {
  ...STRATEGY_SOURCE_LABELS,
};

export { STRATEGY_SOURCE_FILTERS, getStrategySourceLabel, sourceMatchesStrategyKey };
