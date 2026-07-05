import { isWailsGoReady } from '../utils/wailsEnv';

export interface MarketRegime {
  regime: 'bull' | 'bear' | 'neutral' | string;
  emoji: string;
  label: string;
  limitUp: number;
  limitDown: number;
  amountYi: number;
  shPrice: number;
  shMA20: number;
  aboveMA20: boolean;
  asOf: string;
  available: boolean;
}

export interface MarketStyleItem {
  key: 'large' | 'mid' | 'small' | 'micro' | string;
  name: string;
  indexName: string;
  code: string;
  changePercent: number;
  source: string;
}

export interface MarketStylePreference {
  label: string;
  subLabel: string;
  scenario: string;
  strengthGap: number;
  strongKey: string;
  weakKey: string;
  items: MarketStyleItem[];
  asOf: string;
  available: boolean;
  dataNote?: string;
  regimeFallback?: MarketRegime;
}

type GoBridge = {
  GetMarketRegime?: () => Promise<MarketRegime>;
  GetMarketStylePreference?: () => Promise<MarketStylePreference>;
};
const bridge = (): GoBridge => {
  const win = window as unknown as { go?: { main?: { App?: GoBridge } } };
  return win.go?.main?.App || {};
};

export const getMarketRegime = async (): Promise<MarketRegime | null> => {
  if (!isWailsGoReady()) return null;
  const b = bridge();
  if (!b.GetMarketRegime) return null;
  try {
    return await b.GetMarketRegime();
  } catch {
    return null;
  }
};

export const getMarketStylePreference = async (): Promise<MarketStylePreference | null> => {
  if (!isWailsGoReady()) return null;
  const b = bridge();
  if (!b.GetMarketStylePreference) return null;
  try {
    return await b.GetMarketStylePreference();
  } catch {
    return null;
  }
};
