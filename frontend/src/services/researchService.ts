// 投研报告(机构级深度诊断V5.0)服务:异步任务启动/轮询,Word 下载
export interface ResearchReportStatus {
  status: 'idle' | 'running' | 'done' | 'failed';
  stockCode: string;
  stockName: string;
  report: string;
  fileName: string;
  filePath: string;
  error: string;
  modelName: string;
  startedAt: string;
  finishedAt: string;
  elapsedSec: number;
}

const b = () => (window as any).go?.main?.App || {};

export const startResearchReport = async (stockCode: string, stockName: string): Promise<ResearchReportStatus | null> => {
  const api = b();
  if (typeof api.StartResearchReport !== 'function') return null;
  return await api.StartResearchReport(stockCode, stockName);
};

export const getResearchReport = async (stockCode: string): Promise<ResearchReportStatus | null> => {
  const api = b();
  if (typeof api.GetResearchReport !== 'function') return null;
  return await api.GetResearchReport(stockCode);
};

// 报告成品 HTML(与 Word 同源),用 iframe 展示,避免流式渲染器吃不下超长文档
export const getResearchReportHTML = async (stockCode: string): Promise<string> => {
  const api = b();
  if (typeof api.GetResearchReportHTML !== 'function') return '';
  return (await api.GetResearchReportHTML(stockCode)) || '';
};

// 打开/下载 Word:远程模式走 NAS 的 /reports/ 下载,本地模式直接 file:// 打开
export const openResearchReportFile = (st: ResearchReportStatus) => {
  const api = b();
  const remoteBase = (window as any).__jcpRemoteBase as string | undefined;
  const remoteToken = (window as any).__jcpRemoteToken as string | undefined;
  let url = '';
  if (remoteBase && st.fileName) {
    url = `${remoteBase}/reports/${encodeURIComponent(st.fileName)}`;
    if (remoteToken) url += `?token=${encodeURIComponent(remoteToken)}`;
  } else if (st.filePath) {
    url = `file://${encodeURI(st.filePath)}`;
  }
  if (url && typeof api.OpenURL === 'function') {
    api.OpenURL(url);
  }
};
