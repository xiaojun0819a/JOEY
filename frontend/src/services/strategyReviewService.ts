import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export interface StrategyReviewRequest {
  strategyId: string;
  strategyName?: string;
  signalDate?: string;
  reviewDate?: string;
  reviewSymbols?: string[];
}

export interface StrategyReviewNews {
  time: string;
  content: string;
  url?: string;
}

export interface StrategyReviewMarket {
  reviewDate: string;
  shPrice: number;
  shChangePercent: number;
  limitUpCount: number;
  limitDownCount: number;
  totalAmount: number;
  summary: string;
}

export interface StrategyReviewItem {
  symbol: string;
  name: string;
  rank: number;
  industry: string;
  businessSummary?: string;
  businessSource?: string;
  signalPrice: number;
  signalChangePercent: number;
  signalScore: number;
  signalReasons: string[];
  signalTriggers: string[];
  signalRisks: string[];
  reviewDate: string;
  open: number;
  high: number;
  low: number;
  close: number;
  dayChangePercent: number;
  closeReturnPercent: number;
  highReturnPercent: number;
  turnoverRate: number;
  amount: number;
  mainNetInflow: number;
  mainNetInflowPct: number;
  mainFlowSource: string;
  klineSummary: string;
  fundSummary: string;
  outcome: string;
  suggestions: string[];
  news: StrategyReviewNews[];
}

export interface StrategyReviewResult {
  strategyId: string;
  strategyName: string;
  signalDate: string;
  reviewDate: string;
  generatedAt: string;
  pickCount: number;
  reviewedCount: number;
  winRate: number;
  avgCloseReturn: number;
  avgHighReturn: number;
  hit3Rate: number;
  market: StrategyReviewMarket;
  news: StrategyReviewNews[];
  items: StrategyReviewItem[];
  optimization: string[];
  warning?: string;
  dataSourceNotes?: string[];
}

type GoBridge = {
  GetStrategyNextDayReview?: (req: StrategyReviewRequest) => Promise<StrategyReviewResult>;
};

const getGoBridge = (): GoBridge => {
  const win = window as unknown as { go?: { main?: { App?: GoBridge } } };
  return win.go?.main?.App || {};
};

export const getStrategyNextDayReview = async (req: StrategyReviewRequest): Promise<StrategyReviewResult | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('策略次日复盘', 'go');
    return null;
  }
  const bridge = getGoBridge();
  if (!bridge.GetStrategyNextDayReview) {
    throw new Error('当前版本未暴露 GetStrategyNextDayReview 接口，请重启 Wails 开发服务');
  }
  return await bridge.GetStrategyNextDayReview(req);
};
