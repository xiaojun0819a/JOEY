// 市场数据服务 - 调用后端API
import { GetStockRealTimeData, GetKLineData, GetOrderBook, SearchStocks } from '@wailsjs/go/main/App';
import type { Stock, KLineData, OrderBook } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

// 股票搜索结果类型
export interface StockSearchResult {
  symbol: string;
  name: string;
  industry: string;
  market: string;
}

export const getStockRealTimeData = async (codes: string[]): Promise<Stock[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('实时行情', 'go');
    return [];
  }
  return await GetStockRealTimeData(codes);
};

export const getKLineData = async (code: string, period: string, days: number): Promise<KLineData[]> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('K线数据', 'go');
    return [];
  }
  return await GetKLineData(code, period, days);
};

// 获取真实五档盘口数据
export const getOrderBook = async (code: string): Promise<OrderBook> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('五档盘口', 'go');
    return { bids: [], asks: [] };
  }
  return await GetOrderBook(code);
};

// 搜索股票
export const searchStocks = async (keyword: string): Promise<StockSearchResult[]> => {
  if (!keyword.trim()) return [];
  if (!isWailsGoReady()) {
    warnWailsUnavailable('股票搜索', 'go');
    return [];
  }
  try {
    const r = await SearchStocks(keyword);
    return Array.isArray(r) ? (r as StockSearchResult[]) : [];
  } catch (e) {
    console.error('[searchStocks] 失败', e);
    return [];
  }
};
