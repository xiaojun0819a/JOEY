// 自选股服务 - 调用后端API
import { GetWatchlist, AddToWatchlist, RemoveFromWatchlist } from '@wailsjs/go/main/App';
import type { Stock } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

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
