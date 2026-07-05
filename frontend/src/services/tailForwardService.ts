import { isWailsGoReady } from '../utils/wailsEnv';

export interface TailForwardCandidate {
  symbol: string;
  name: string;
  source?: string;
  sourceLabel?: string;
  price: number;
  changePct: number;
  score: number;
  buyable: boolean;
  reason: string;
  alreadyHeld: boolean;
  added: boolean;
}
export interface TailForwardResult {
  asOf: string;
  strategy: string;
  auto: boolean;
  candidates: TailForwardCandidate[];
  buyableCount: number;
  sealedCount: number;
  addedCount: number;
  warning: string;
}
export interface TailForwardConfig {
  enabled: boolean;
  auto: boolean;
}

type Bridge = {
  RunTailForwardScan?: (strategy: string, autoBuy: boolean) => Promise<TailForwardResult>;
  RunTailForwardScanAll?: (autoBuy: boolean) => Promise<TailForwardResult>;
  GetTailForwardConfig?: () => Promise<TailForwardConfig>;
  SetTailForwardConfig?: (enabled: boolean, auto: boolean) => Promise<string>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

export const runTailForwardScan = async (strategy: string, autoBuy: boolean): Promise<TailForwardResult | null> => {
  if (!isWailsGoReady() || !b().RunTailForwardScan) return null;
  try { return await b().RunTailForwardScan!(strategy, autoBuy); } catch { return null; }
};

export const runTailForwardScanAll = async (autoBuy: boolean): Promise<TailForwardResult | null> => {
  if (!isWailsGoReady() || !b().RunTailForwardScanAll) return null;
  try { return await b().RunTailForwardScanAll!(autoBuy); } catch { return null; }
};
export const getTailForwardConfig = async (): Promise<TailForwardConfig> => {
  if (!isWailsGoReady() || !b().GetTailForwardConfig) return { enabled: false, auto: false };
  try { return await b().GetTailForwardConfig!(); } catch { return { enabled: false, auto: false }; }
};
export const setTailForwardConfig = async (enabled: boolean, auto: boolean): Promise<void> => {
  if (!isWailsGoReady() || !b().SetTailForwardConfig) return;
  try { await b().SetTailForwardConfig!(enabled, auto); } catch { /* ignore */ }
};
