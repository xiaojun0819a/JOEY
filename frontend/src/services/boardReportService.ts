import { AskBoardReport, GenerateBoardReport, GetCachedBoardReport } from '../../wailsjs/go/main/App';
import type { main } from '../../wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type BoardReportQARequest = main.AskBoardReportRequest;
export type BoardReportQAResponse = main.AskBoardReportResponse;
export type BoardReportGenerateRequest = main.GenerateBoardReportRequest;
export type BoardReportGenerateResponse = main.GenerateBoardReportResponse;
export type BoardReportCachedResponse = main.GetCachedBoardReportResponse;

export const generateBoardReport = async (req: BoardReportGenerateRequest): Promise<BoardReportGenerateResponse | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('看板报告生成', 'go');
    return null;
  }
  return await GenerateBoardReport(req);
};

// 查询后端缓存的老陈完整报告(未命中 found=false);浏览器预览模式静默返回 null,不弹警告
export const getCachedBoardReport = async (stockCode: string, period: string): Promise<BoardReportCachedResponse | null> => {
  if (!isWailsGoReady()) return null;
  return await GetCachedBoardReport(stockCode, period);
};

export const askBoardReport = async (req: BoardReportQARequest): Promise<BoardReportQAResponse | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('报告问答', 'go');
    return null;
  }
  return await AskBoardReport(req);
};
