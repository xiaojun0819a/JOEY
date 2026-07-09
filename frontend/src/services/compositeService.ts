// 综合评分选股:否决+三层评分+状态门;快照前向验证。

export interface CompositeScoreRow {
  symbol: string;
  name: string;
  price: number;
  quality: number;
  structure: number;
  catalyst: number;
  total: number;
  qualityFacts: string[] | null;
  structFacts: string[] | null;
  catalystFacts: string[] | null;
  gateOk: boolean;
  gateReasons: string[] | null;
  vetoed: boolean;
  vetoReasons: string[] | null;
  annRoe: number;
  marketCapYi: number;
}

export interface CompositeScoreResult {
  runDate: string;
  preset: string;
  presetLabel: string;
  universeCount: number;
  rows: CompositeScoreRow[] | null;
  vetoedRows: CompositeScoreRow[] | null;
  warning: string;
  rulesText: string;
  snapshotSaved: boolean;
}

export interface CompositeValidationRow {
  runDate: string;
  horizonDays: number;
  n: number;
  portRet: number;
  benchRet: number;
  excess: number;
  matured: boolean;
  daysElapsed: number;
}

export interface CompositeValidationResult {
  rows: CompositeValidationRow[] | null;
  costNote: string;
  warning: string;
}

type Bridge = {
  RunCompositeScore?: (preset: string) => Promise<CompositeScoreResult>;
  GetCompositeValidation?: () => Promise<CompositeValidationResult>;
};
const b = (): Bridge => {
  const w = window as unknown as { go?: { main?: { App?: Bridge } } };
  return w.go?.main?.App || {};
};

export async function runCompositeScore(preset: string): Promise<CompositeScoreResult> {
  const fn = b().RunCompositeScore;
  if (!fn) throw new Error('后端未就绪');
  return fn(preset);
}

export async function getCompositeValidation(): Promise<CompositeValidationResult> {
  const fn = b().GetCompositeValidation;
  if (!fn) throw new Error('后端未就绪');
  return fn();
}
