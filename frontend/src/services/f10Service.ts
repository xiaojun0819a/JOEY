import { GetF10Overview, GetF10Valuation } from '@wailsjs/go/main/App';
import type { F10Overview, StockValuation } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export const getF10Overview = async (code: string): Promise<F10Overview | null> => {
  if (!code) return null;
  if (!isWailsGoReady()) {
    warnWailsUnavailable('F10概览', 'go');
    return null;
  }
  return await GetF10Overview(code) as F10Overview;
};

export const getF10Valuation = async (code: string): Promise<StockValuation | null> => {
  if (!code) return null;
  if (!isWailsGoReady()) {
    warnWailsUnavailable('估值快照', 'go');
    return null;
  }
  return await GetF10Valuation(code) as StockValuation;
};
