import React, { useState } from 'react';
import { ChevronDown, AlertTriangle, Activity } from 'lucide-react';
import type { DiagnosisSummary } from '../utils/diagnosisSummary';
import { useTheme } from '../contexts/ThemeContext';

interface Props {
  summary: DiagnosisSummary;
  marketStatusCode?: string;
}

const isLive = (s?: string) => s === 'trading' || s === 'pre_market' || s === 'lunch_break';

export const DiagnosisSummaryBar: React.FC<Props> = ({ summary, marketStatusCode }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [open, setOpen] = useState(false);

  const live = isLive(marketStatusCode);
  const title = live ? '实时观点' : '今日复盘';

  const stancePill =
    summary.stance === 'bull'
      ? 'bg-red-500/15 text-red-400 border-red-500/30'
      : summary.stance === 'bear'
        ? 'bg-green-500/15 text-green-400 border-green-500/30'
        : 'bg-slate-500/15 text-slate-300 border-slate-500/30';

  return (
    <div className={`sticky top-0 z-10 -mx-4 -mt-4 mb-3 px-4 py-2.5 border-b backdrop-blur ${dark ? 'bg-slate-900/85 border-slate-700/60' : 'bg-white/90 border-slate-200'}`}>
      <button onClick={() => setOpen(o => !o)} className="w-full flex items-center gap-2 text-left">
        <span className={`flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded ${dark ? 'text-slate-400 bg-slate-800' : 'text-slate-500 bg-slate-100'}`}>
          <Activity size={10} />{title}
        </span>
        <span className={`text-xs px-1.5 py-0.5 rounded border font-medium ${stancePill}`}>{summary.stanceText}</span>
        {/* 三方分歧比分 */}
        <span className="flex items-center gap-1 text-[11px]">
          <span className="text-red-400">多{summary.bull}</span>
          <span className={dark ? 'text-slate-500' : 'text-slate-400'}>中{summary.neutral}</span>
          <span className="text-green-400">空{summary.bear}</span>
        </span>
        {summary.confidence > 0 && (
          <span className={`text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`}>置信 {summary.confidence}</span>
        )}
        {summary.highDivergence && (
          <span className="flex items-center gap-0.5 text-[10px] px-1 py-0.5 rounded bg-amber-500/15 text-amber-400 border border-amber-500/30">
            <AlertTriangle size={9} />分歧大
          </span>
        )}
        <ChevronDown size={14} className={`ml-auto shrink-0 transition-transform ${dark ? 'text-slate-500' : 'text-slate-400'} ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div className={`mt-2 text-xs leading-relaxed ${dark ? 'text-slate-300' : 'text-slate-600'}`}>
          {summary.conclusion
            ? <p>{summary.conclusion}</p>
            : <p className={dark ? 'text-slate-500' : 'text-slate-400'}>本轮专家结论见下方讨论。</p>}
          {summary.highDivergence && (
            <p className="mt-1 text-amber-400">⚠ 专家意见分歧较大，建议谨慎参考、降低仓位。</p>
          )}
        </div>
      )}
    </div>
  );
};

export default DiagnosisSummaryBar;
