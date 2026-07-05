// 自选股服务 - 调用后端API
import {
  GetWatchlist, AddToWatchlist, RemoveFromWatchlist,
  GetStockGroups, SetStockGroups,
  GetStockGroupDefs, AddStockGroupDef, RenameStockGroupDef, DeleteStockGroupDef,
  SyncTradeJournalWatchGroup,
} from '@wailsjs/go/main/App';
import type { Stock } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export interface StockGroup {
  id: string;
  name: string;
}

// getStockGroups 读取 symbol -> 分组ID列表 映射
export const getStockGroups = async (): Promise<Record<string, string[]>> => {
  if (!isWailsGoReady()) return {};
  try {
    const m = await GetStockGroups();
    return (m as Record<string, string[]>) || {};
  } catch {
    return {};
  }
};

// setStockGroups 覆盖式设置某股票所属分组（空数组=移出所有分组）
export const setStockGroups = async (symbol: string, groups: string[]): Promise<void> => {
  if (!isWailsGoReady()) return;
  try {
    await SetStockGroups(symbol, groups);
  } catch {
    /* ignore */
  }
};

// ===== 分组定义（用户自定义） =====
export const getStockGroupDefs = async (): Promise<StockGroup[]> => {
  if (!isWailsGoReady()) return [];
  try {
    const list = await GetStockGroupDefs();
    return Array.isArray(list) ? (list as StockGroup[]) : [];
  } catch {
    return [];
  }
};

export const addStockGroupDef = async (name: string): Promise<StockGroup | null> => {
  if (!isWailsGoReady()) return null;
  try {
    const g = await AddStockGroupDef(name);
    return (g as StockGroup) || null;
  } catch {
    return null;
  }
};

export const renameStockGroupDef = async (id: string, name: string): Promise<void> => {
  if (!isWailsGoReady()) return;
  try { await RenameStockGroupDef(id, name); } catch { /* ignore */ }
};

export const deleteStockGroupDef = async (id: string): Promise<void> => {
  if (!isWailsGoReady()) return;
  try { await DeleteStockGroupDef(id); } catch { /* ignore */ }
};

export const syncTradeJournalWatchGroup = async (): Promise<void> => {
  if (!isWailsGoReady()) return;
  try {
    await SyncTradeJournalWatchGroup();
  } catch {
    /* ignore */
  }
};

export const getWatchlist = async (): Promise<Stock[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取自选股', 'go');
    return [];
  }
  return await GetWatchlist() as Stock[];
};

export const addToWatchlist = async (stock: Stock): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('添加自选股', 'go');
    return 'browser-mode:no-op';
  }
  return await AddToWatchlist(stock as any);
};

export const removeFromWatchlist = async (symbol: string): Promise<string> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('删除自选股', 'go');
    return 'browser-mode:no-op';
  }
  return await RemoveFromWatchlist(symbol);
};
