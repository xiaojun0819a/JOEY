// 交易台账服务
import { GetTradeJournal, GetTradeJournalStats, SaveTradeJournal, DeleteTradeJournal, SellStockPosition } from '@wailsjs/go/main/App';
import type { models } from '@wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

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
