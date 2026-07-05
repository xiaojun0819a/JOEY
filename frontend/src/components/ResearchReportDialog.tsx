import React, { useEffect, useRef, useState } from 'react';
import { FileDown, FileText, Loader2, RefreshCw, X } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import { Stock } from '../types';
import { ResearchReportStatus, getResearchReport, getResearchReportHTML, startResearchReport, openResearchReportFile } from '../services/researchService';

interface ResearchReportDialogProps {
  isOpen: boolean;
  onClose: () => void;
  stock: Stock | null;
}

const fmtElapsed = (sec: number) => {
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return m > 0 ? `${m}分${s.toString().padStart(2, '0')}秒` : `${s}秒`;
};

// 投研报告(V5.0 机构级深度诊断):异步生成(15-25分钟),关窗不中断,完成后可下载 Word
export const ResearchReportDialog: React.FC<ResearchReportDialogProps> = ({ isOpen, onClose, stock }) => {
  const { colors } = useTheme();
  const [st, setSt] = useState<ResearchReportStatus | null>(null);
  const [reportHTML, setReportHTML] = useState(''); // 成品 HTML(iframe 展示,markstream 渲不动4万字)
  const [tick, setTick] = useState(0); // 本地秒表(running 时显示流逝时间)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const tickRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopTimers = () => {
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
    if (tickRef.current) { clearInterval(tickRef.current); tickRef.current = null; }
  };

  const refresh = async (code: string) => {
    const res = await getResearchReport(code);
    if (res) {
      setSt(res);
      if (res.status !== 'running') stopTimers();
      if (res.status === 'done' && res.report) {
        const html = await getResearchReportHTML(code);
        if (html) setReportHTML(html);
      }
    }
  };

  const ensureTimers = (code: string) => {
    if (!pollRef.current) pollRef.current = setInterval(() => void refresh(code), 10000);
    if (!tickRef.current) tickRef.current = setInterval(() => setTick(t => t + 1), 1000);
  };

  useEffect(() => {
    if (!isOpen || !stock?.symbol) return;
    setSt(null);
    setReportHTML('');
    setTick(0);
    void refresh(stock.symbol);
    return stopTimers;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, stock?.symbol]);

  useEffect(() => {
    if (st?.status === 'running' && stock?.symbol) ensureTimers(stock.symbol);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [st?.status, stock?.symbol]);

  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const start = async () => {
    if (!stock?.symbol) return;
    const res = await startResearchReport(stock.symbol, stock.name || '');
    if (res) {
      setSt(res);
      setTick(0);
      ensureTimers(stock.symbol);
    }
  };

  const shellClass = colors.isDark ? 'border-slate-700 bg-[#08111f] text-slate-100' : 'border-slate-200 bg-white text-slate-900';
  const panelClass = colors.isDark ? 'border-slate-700 bg-slate-950/45 text-slate-200' : 'border-slate-200 bg-white text-slate-800';
  const mutedClass = colors.isDark ? 'text-slate-400' : 'text-slate-500';
  const runningSec = st?.status === 'running' ? (st.elapsedSec || 0) + tick : 0;

  return (
    <div
      className="fixed inset-0 z-[95] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
      style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
      onClick={onClose}
    >
      <div className={`flex h-[92vh] w-[1200px] max-w-[96vw] flex-col overflow-hidden rounded-lg border shadow-2xl ${shellClass}`} onClick={e => e.stopPropagation()}>
        <header className={`flex shrink-0 items-center justify-between gap-3 border-b px-4 py-3 ${colors.isDark ? 'border-slate-800 bg-slate-950/95' : 'border-slate-200 bg-white/95'}`}>
          <div className="flex min-w-0 items-center gap-2">
            <FileText className="h-4 w-4 shrink-0 text-cyan-300" />
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">投研报告 · {stock?.name || ''} {stock?.symbol || ''}</div>
              <div className={`mt-0.5 text-[11px] ${mutedClass}`}>
                机构级深度诊断 V5.0 旗舰增强版
                {st?.status === 'done' && ` · ${st.finishedAt}${st.modelName ? ` · ${st.modelName}` : ''}`}
                {st?.status === 'running' && ` · 生成中 ${fmtElapsed(runningSec)}`}
              </div>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {st?.status === 'done' && (
              <button
                type="button"
                onClick={() => st && openResearchReportFile(st)}
                className="inline-flex h-8 items-center gap-1.5 rounded border border-emerald-400/40 bg-emerald-500/10 px-3 text-xs font-semibold text-emerald-100 transition-colors hover:bg-emerald-500/20"
                title="下载/打开 Word 报告"
              >
                <FileDown className="h-3.5 w-3.5" />
                下载 Word
              </button>
            )}
            {(st?.status === 'done' || st?.status === 'failed') && (
              <button
                type="button"
                onClick={() => void start()}
                className="inline-flex h-8 items-center gap-1.5 rounded border border-cyan-400/35 bg-cyan-400/10 px-2.5 text-xs font-semibold text-cyan-100 transition-colors hover:bg-cyan-400/20"
                title="重新生成(约15-25分钟)"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                重新生成
              </button>
            )}
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-8 w-8 items-center justify-center rounded border border-slate-700 text-slate-400 transition-colors hover:border-red-400/50 hover:bg-red-500/10 hover:text-red-200"
              title="关闭(生成中关闭不影响后台任务)"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-auto px-4 py-3">
          {!st || st.status === 'idle' ? (
            <div className={`flex min-h-[420px] flex-col items-center justify-center rounded-md border p-8 text-center ${panelClass}`}>
              <FileText className="mb-3 h-8 w-8 text-cyan-300" />
              <div className="text-base font-bold">机构级深度诊断报告 V5.0</div>
              <div className={`mt-3 max-w-[560px] text-xs leading-relaxed ${mutedClass}`}>
                买方机构研究员 + 游资情绪交易员 + 量化策略分析师三重视角,16 章完整框架:
                投资摘要 / 公司画像 / 产业链 / 同行对比 / 财务拆解 / 成长拐点 / 题材热度 /
                资金博弈 / 鱼身定位 / 量化评分 / 概率预测 / 交易计划卡 / 风险矩阵 / 估值推演 / 最终结论。
                生成后可下载 Word 文件。
              </div>
              <div className={`mt-2 text-[11px] ${mutedClass}`}>预计 15-25 分钟,在 NAS 后台生成,期间可关闭本窗口随时回来查看。</div>
              <button
                type="button"
                onClick={() => void start()}
                disabled={!stock?.symbol}
                className="mt-5 inline-flex h-9 items-center gap-2 rounded border border-cyan-400/45 bg-cyan-400/15 px-5 text-sm font-semibold text-cyan-100 transition-colors hover:bg-cyan-400/25 disabled:opacity-50"
              >
                <FileText className="h-4 w-4" />
                开始生成
              </button>
            </div>
          ) : st.status === 'running' ? (
            <div className={`flex min-h-[420px] flex-col items-center justify-center rounded-md border p-8 text-center ${panelClass}`}>
              <Loader2 className="mb-3 h-8 w-8 animate-spin text-cyan-300" />
              <div className="text-sm font-semibold">深度报告生成中 · 已用时 {fmtElapsed(runningSec)}</div>
              <div className={`mt-2 max-w-[520px] text-xs leading-relaxed ${mutedClass}`}>
                正在逐章调用行情、财务、估值、同行、研报、资金流、龙虎榜、筹码、新闻等工具并写作(约15-25分钟)。
                可关闭本窗口,生成在 NAS 后台继续,回来重开即可看进度。
              </div>
            </div>
          ) : st.status === 'failed' ? (
            <div className="rounded-md border border-rose-400/35 bg-rose-500/10 p-4 text-sm text-rose-100">
              <div className="font-semibold">投研报告生成失败</div>
              <div className="mt-2 text-xs leading-relaxed text-rose-100/85">{st.error || '未知错误,请重试'}</div>
            </div>
          ) : reportHTML ? (
            /* 成品 HTML 走 iframe:样式隔离、表格完整,和 Word 打开一致;markstream 渲 4 万字会空白 */
            <iframe
              title="research-report"
              srcDoc={reportHTML}
              style={{ width: '100%', height: '100%', minHeight: '74vh', border: 0, borderRadius: 8, background: '#ffffff' }}
            />
          ) : (
            <div className={`flex min-h-[420px] items-center justify-center rounded-md border p-6 ${panelClass}`}>
              <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-300" />
              <span className="text-sm">正在加载报告内容…</span>
            </div>
          )}
        </main>
      </div>
    </div>
  );
};
