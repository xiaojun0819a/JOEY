// 交易台账服务
import { GetTradeJournal, GetTradeJournalStats, SaveTradeJournal, DeleteTradeJournal, SellStockPosition, ReduceStockPosition, RealizedPnLOfStock } from '@wailsjs/go/main/App';
import type { models } from '@wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';
import { reportToBackend } from '../utils/reportToBackend';

export type TradeEntry = models.TradeJournalEntry;
export type TradeStats = models.TradeJournalStats;
export type TradeReq = models.TradeJournalRequest;

export const getTradeJournal = async (): Promise<TradeEntry[]> => {
  if (!isWailsGoReady()) { warnWailsUnavailable('交易台账', 'go'); return []; }
  return (await GetTradeJournal()) || [];
};

export const getTradeJournalStats = async (): Promise<TradeStats | null> => {
  if (!isWailsGoReady()) { warnWailsUnavailable('台账统计', 'go'); return null; }
  return await GetTradeJournalStats();
};

export const saveTradeJournal = async (req: TradeReq): Promise<string> => {
  if (!isWailsGoReady()) { warnWailsUnavailable('保存交易', 'go'); return 'browser-mode'; }
  return await SaveTradeJournal(req);
};

export const deleteTradeJournal = async (id: number): Promise<string> => {
  if (!isWailsGoReady()) { warnWailsUnavailable('删除交易', 'go'); return 'browser-mode'; }
  return await DeleteTradeJournal(id);
};

export const sellStockPosition = async (code: string, sellPrice: number, sellDate: string): Promise<string> => {
  if (!isWailsGoReady()) { warnWailsUnavailable('卖出', 'go'); return 'browser-mode'; }
  return await SellStockPosition(code, sellPrice, sellDate);
};

// 减仓:卖一部分,成本价不变(已实现盈亏由后端拆一笔进台账)。
// 别用"改持仓数量"代替它——那条路不记台账,减掉那部分赚的钱会凭空消失。
export const reduceStockPosition = async (code: string, sellShares: number, sellPrice: number, sellDate: string): Promise<string> => {
  reportToBackend('减仓', `前端发起 ${code} ${sellShares}股 @${sellPrice}`);
  if (!isWailsGoReady()) {
    warnWailsUnavailable('减仓', 'go');
    reportToBackend('减仓', '❌ isWailsGoReady()=false,没调后端');
    return '后端未就绪(browser-mode)';
  }
  const fn = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } })?.go?.main?.App?.ReduceStockPosition;
  if (typeof fn !== 'function') {
    reportToBackend('减仓', '❌ window.go.main.App.ReduceStockPosition 不存在 —— app 版本与前端不匹配,需重新构建');
    return 'app 里没有减仓方法(版本不匹配,请重新构建)';
  }
  try {
    const r = await ReduceStockPosition(code, sellShares, sellPrice, sellDate);
    reportToBackend('减仓', `后端返回:${r}`);
    return r;
  } catch (e) {
    const msg = e instanceof Error ? `${e.name}: ${e.message}` : String(e);
    reportToBackend('减仓', `❌ 调用抛异常:${msg}`);
    return `调用失败:${msg}`;
  }
};

export const realizedPnLOfStock = async (code: string): Promise<number> => {
  if (!isWailsGoReady()) return 0;
  return await RealizedPnLOfStock(code);
};
