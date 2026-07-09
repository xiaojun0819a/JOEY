import { AskBoardReport, GenerateBoardReport, GetCachedBoardReport } from '../../wailsjs/go/main/App';
import type { main } from '../../wailsjs/go/models';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type BoardReportQARequest = main.AskBoardReportRequest;
export type BoardReportQAResponse = main.AskBoardReportResponse;
export type BoardReportGenerateRequest = main.GenerateBoardReportRequest;
export type BoardReportGenerateResponse = main.GenerateBoardReportResponse;
export type BoardReportCachedResponse = main.GetCachedBoardReportResponse;

// 老陈完整报告改「异步启动 + 轮询」:生成要 3-8 分钟,同步 RPC 经 Cloudflare 隧道会被
// ~100 秒超时掐断(Windows 走公网必中)。拆成 StartBoardReport(秒回)+ GetBoardReportStatus(每几秒拉一次,
// 每次都快),隧道就不会掐;UI 也不再卡死。onProgress 可选,用于显示已生成秒数。
export const generateBoardReport = async (
  req: BoardReportGenerateRequest,
  onProgress?: (elapsedSec: number) => void,
): Promise<BoardReportGenerateResponse | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('看板报告生成', 'go');
    return null;
  }
  const app = (window as any).go?.main?.App;
  // 新后端:异步启动+轮询。老后端(没有 StartBoardReport)回落到旧的同步调用。
  if (!app || typeof app.StartBoardReport !== 'function' || typeof app.GetBoardReportStatus !== 'function') {
    return await GenerateBoardReport(req);
  }

  const start = await app.StartBoardReport(req);
  if (start && start.status === 'failed') {
    return { success: false, error: start.error || '启动生成失败' } as BoardReportGenerateResponse;
  }

  const deadline = Date.now() + 16 * 60 * 1000; // 最长等 16 分钟
  const period = (req as any).period || '';
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 3000));
    let st: any;
    try {
      st = await app.GetBoardReportStatus((req as any).stockCode, period);
    } catch {
      continue; // 单次轮询抖动不致命,继续
    }
    if (!st) continue;
    if (typeof st.elapsedSec === 'number' && onProgress) onProgress(st.elapsedSec);
    if (st.status === 'done') {
      return {
        success: true, stockCode: st.stockCode, stockName: st.stockName, report: st.report,
        agentId: st.agentId, agentName: st.agentName, modelName: st.modelName, generatedAt: st.generatedAt,
      } as BoardReportGenerateResponse;
    }
    if (st.status === 'failed') {
      return { success: false, error: st.error || '生成失败' } as BoardReportGenerateResponse;
    }
    // running / idle → 继续轮询
  }
  return { success: false, error: '生成超时(超过16分钟),请稍后重试' } as BoardReportGenerateResponse;
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
