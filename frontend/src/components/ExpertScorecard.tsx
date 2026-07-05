import React, { useState } from 'react';
import { TrendingUp, TrendingDown, Shield, Percent, Clock, ChevronDown, AlertTriangle, ListFilter } from 'lucide-react';
import type { ExpertCard } from '../utils/expertCard';
import { useTheme } from '../contexts/ThemeContext';

interface Props {
  card: ExpertCard;
}

const mmdd = (d?: string) => (d && d.length >= 10 ? d.slice(5) : d || '');

export const ExpertScorecard: React.FC<Props> = ({ card }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [eviOpen, setEviOpen] = useState(card.evidence.length > 0 && card.evidence.length <= 4);

  const cell = `rounded-xl px-3 py-2.5 border ${dark ? 'bg-slate-900/40 border-slate-700/50' : 'bg-slate-50 border-slate-200'}`;
  const label = `flex items-center gap-1 text-[11px] mb-0.5 ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const val = `text-sm font-medium leading-snug ${dark ? 'text-slate-100' : 'text-slate-800'}`;
  const muted = dark ? 'text-slate-500' : 'text-slate-400';

  const stanceColor = card.side === 'bull' ? 'text-red-400' : card.side === 'bear' ? 'text-green-400' : (dark ? 'text-slate-200' : 'text-slate-700');
  const tierBadge =
    card.confTier === 'high' ? 'bg-green-500/15 text-green-400 border-green-500/30'
      : card.confTier === 'low' ? 'bg-amber-500/15 text-amber-400 border-amber-500/30'
        : 'bg-slate-500/15 text-slate-300 border-slate-500/30';

  const decisionCells: Array<{ icon: React.ElementType; cls: string; name: string; value?: string; price?: string }> = [];
  if (card.buy) decisionCells.push({ icon: TrendingUp, cls: 'text-red-400', name: '买点', value: card.buy });
  if (card.sell) decisionCells.push({ icon: TrendingDown, cls: 'text-green-400', name: '卖点', value: card.sell });
  if (card.stop) decisionCells.push({ icon: Shield, cls: 'text-amber-400', name: '止损', value: card.stop, price: card.stopPrice });
  if (card.position) decisionCells.push({ icon: Percent, cls: dark ? 'text-slate-400' : 'text-slate-500', name: '仓位', value: card.position });

  return (
    <div className="space-y-2.5">
      {/* 置信徽章（有立场才有意义） */}
      {card.hasStance && (
        <div className="flex justify-end -mt-1">
          <span className={`inline-flex items-center gap-1 text-[11px] px-2 py-1 rounded-lg border ${tierBadge}`}>
            {card.confTier === 'low' && <AlertTriangle size={11} />}
            {card.confBadge}
          </span>
        </div>
      )}

      {/* 立场仪表 */}
      {card.hasStance && (
        <div className={`rounded-xl px-3.5 py-3 border ${dark ? 'bg-slate-900/40 border-slate-700/50' : 'bg-slate-50 border-slate-200'}`}>
          <div className="flex items-center justify-between mb-2.5">
            <span className={`text-sm ${dark ? 'text-slate-300' : 'text-slate-600'}`}>立场</span>
            <span className={`text-sm font-bold ${stanceColor}`}>{card.stanceText} · {card.confidence}</span>
          </div>
          <div className="relative h-2 rounded-full" style={{ background: 'linear-gradient(90deg,#22c55e 0%,#eab308 50%,#ef4444 100%)' }}>
            <div className="absolute top-1/2 w-3.5 h-3.5 rounded-full bg-white border-2 border-slate-800 shadow" style={{ left: `${card.gaugePos}%`, transform: 'translate(-50%,-50%)' }} />
          </div>
          <div className={`flex justify-between text-[11px] mt-1.5 ${muted}`}>
            <span>看空</span><span>中性</span><span>看多</span>
          </div>
        </div>
      )}

      {/* 决策：一行一图标（存在才显示） */}
      {decisionCells.length > 0 && (
        <div className="grid grid-cols-2 gap-2">
          {decisionCells.map(c => (
            <div key={c.name} className={cell}>
              <div className={label}><c.icon size={12} className={c.cls} />{c.name}</div>
              {c.price ? (
                <div className={val}>
                  <span className="text-base font-bold">{c.price}</span>
                  <span className={`ml-1 text-[11px] ${muted}`}>{(c.value || '').replace(c.price, '').replace(/^[^一-龥]*/, '').slice(0, 8)}</span>
                </div>
              ) : (
                <div className={`${val} line-clamp-2`}>{c.value}</div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* 其余结构化字段（影响链路/阶段/量化/催化/price-in…）：标签在上、内容占满整行 */}
      {card.extras.length > 0 && (
        <div className={`rounded-xl border px-3 py-2.5 space-y-2.5 ${dark ? 'bg-slate-900/40 border-slate-700/50' : 'bg-slate-50 border-slate-200'}`}>
          {card.extras.map((e, i) => (
            <div key={i}>
              <span className={`inline-block text-[11px] px-1.5 py-0.5 rounded mb-1 ${dark ? 'bg-slate-700/60 text-slate-300' : 'bg-slate-200 text-slate-600'}`}>{e.label}</span>
              <div className={`text-sm leading-relaxed ${dark ? 'text-slate-200' : 'text-slate-700'}`}>{e.value}</div>
            </div>
          ))}
        </div>
      )}

      {/* 有效期倒计时 */}
      {card.validityDate && (
        <div className={`flex items-center justify-between rounded-xl px-3.5 py-2.5 border ${dark ? 'bg-blue-500/10 border-blue-500/25' : 'bg-blue-50 border-blue-200'}`}>
          <span className={`flex items-center gap-1.5 text-sm ${dark ? 'text-blue-200' : 'text-blue-700'}`}><Clock size={14} />信号有效期</span>
          <span className={`text-sm font-medium ${dark ? 'text-blue-200' : 'text-blue-700'}`}>至 {mmdd(card.validityDate)} · 剩 {card.validityDaysLeft} 天</span>
        </div>
      )}

      {/* 核心证据折叠 */}
      {card.evidence.length > 0 && (
        <div className={`rounded-xl border overflow-hidden ${dark ? 'border-slate-700/50' : 'border-slate-200'}`}>
          <button onClick={() => setEviOpen(o => !o)} className={`flex items-center justify-between w-full px-3.5 py-2.5 text-sm ${dark ? 'text-slate-300 hover:bg-slate-800/40' : 'text-slate-600 hover:bg-slate-100'}`}>
            <span className="flex items-center gap-1.5"><ListFilter size={13} />核心证据 · {card.evidence.length} 条</span>
            <ChevronDown size={15} className={`transition-transform ${eviOpen ? 'rotate-180' : ''}`} />
          </button>
          {eviOpen && (
            <div className={`px-3.5 pb-3 pt-1 space-y-2 ${dark ? 'text-slate-300' : 'text-slate-600'}`}>
              {card.evidence.map((e, i) => (
                <div key={i} className="flex gap-2 text-sm leading-relaxed">
                  <span className={`shrink-0 ${muted}`}>{i + 1}</span><span>{e}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* 样本区间 footer */}
      {card.sampleStart && card.sampleEnd && (
        <div className={`text-[11px] pt-1 border-t ${dark ? 'text-slate-500 border-slate-700/50' : 'text-slate-400 border-slate-200'}`}>
          统计样本区间 {card.sampleStart} 至 {card.sampleEnd}
        </div>
      )}
    </div>
  );
};

export default ExpertScorecard;
