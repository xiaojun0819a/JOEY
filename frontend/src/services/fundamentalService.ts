import { isWailsGoReady } from '../utils/wailsEnv';

export interface FundamentalCandidate {
  symbol: string;
  name: string;
  price: number;
  marketCapYi: number;
  annRoe: number;
  revYoY: number;
  profitYoY: number;
  grossMargin: number;
  cfps: number;
  eps: number;
  amountYi: number;
  debtRatio: number;
  valPctile: number;
  goodwillRatio: number;
  dividendYield: number;
  score: number;
  passed: string[];
}
export interface FundamentalScanResult {
  preset: string;
  presetLabel: string;
  reportDate: string;
  universeCount: number;
  candidates: FundamentalCandidate[];
  warning: string;
  rulesText: string;
}

type Bridge = {
  RunFundamentalScan?: (preset: string) => Promise<FundamentalScanResult>;
  RefreshFundamentals?: () => Promise<string>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

export const runFundamentalScan = async (preset: string): Promise<FundamentalScanResult | null> => {
  if (!isWailsGoReady() || !b().RunFundamentalScan) return null;
  try { return await b().RunFundamentalScan!(preset); } catch { return null; }
};
export const refreshFundamentals = async (): Promise<string> => {
  if (!isWailsGoReady() || !b().RefreshFundamentals) return '桥接未就绪';
  try { return await b().RefreshFundamentals!(); } catch (e) { return String(e); }
};
