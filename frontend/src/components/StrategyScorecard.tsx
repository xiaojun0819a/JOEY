import React from 'react';
import { useTheme } from '../contexts/ThemeContext';

// 统一计分卡指标（回测与模拟持仓共用同一口径）
export interface ScorecardMetrics {
  trades: number;          // 样本笔数（已平仓/已完成）
  winRate: number;         // 胜率 %
  expectancy: number;      // 期望值/笔 %（扣成本净收益均值）
  payoffRatio: number;     // 赔率 = 盈利单均值/|亏损单均值|
  profitFactor: number;    // 盈利因子 = 总盈利/总亏损
  maxLoss: number;         // 单笔最大亏损 %（负数）
  excess?: number;         // 超额收益/笔 %（alpha，回测有，模拟暂无）
  benchmark?: number;      // 同期基准 %
  maxDrawdown?: number;    // 最大回撤 %（回测组合）
  avgHold?: number;        // 平均持有天数
}

interface Props {
  title?: string;
  metrics: ScorecardMetrics;
  className?: string;
  dense?: boolean; // 紧凑模式（用于面板头部）
}

const fmtPct = (v: number, sign = false) => `${sign && v >= 0 ? '+' : ''}${v.toFixed(2)}%`;

// 涨红跌绿（A股习惯）：正=红 负=绿
const upDown = (v: number) => (v >= 0 ? 'text-red-400' : 'text-green-400');

export const StrategyScorecard: React.FC<Props> = ({ title, metrics: m, className, dense }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const thin = m.trades < 30; // 样本不足提示

  const tiles: { label: string; value: string; cls?: string; hint?: string; emph?: boolean }[] = [
    { label: '期望值/笔', value: fmtPct(m.expectancy, true), cls: upDown(m.expectancy), emph: true, hint: '每笔扣成本净收益均值，>0才赚钱' },
    { label: '超额/笔(alpha)', value: m.excess === undefined ? '—' : fmtPct(m.excess, true), cls: m.excess === undefined ? 'fin-text-tertiary' : upDown(m.excess), emph: true, hint: '相对同期等权全A的超额，>0才算真本事' },
    { label: '胜率', value: `${m.winRate.toFixed(1)}%`, cls: m.winRate >= 50 ? 'text-red-400' : 'fin-text-secondary' },
    { label: '赔率', value: m.payoffRatio.toFixed(2), cls: m.payoffRatio >= 1 ? 'text-red-400' : 'text-green-400', hint: '盈利单均值/亏损单均值，低胜率要靠高赔率' },
    { label: '盈利因子', value: m.profitFactor.toFixed(2), cls: m.profitFactor >= 1 ? 'text-red-400' : 'text-green-400', hint: '总盈利/总亏损，>1才正期望' },
    { label: '单笔最大亏', value: fmtPct(m.maxLoss), cls: 'text-green-400', hint: '尾部风险，决定能否拿住' },
  ];
  if (m.maxDrawdown !== undefined) tiles.push({ label: '最大回撤', value: fmtPct(-Math.abs(m.maxDrawdown)), cls: 'text-green-400' });
  if (m.benchmark !== undefined) tiles.push({ label: '同期基准', value: fmtPct(m.benchmark, true), cls: upDown(m.benchmark) });
  if (m.avgHold !== undefined) tiles.push({ label: '持有', value: `${m.avgHold.toFixed(1)}天`, cls: 'fin-text-secondary' });

  return (
    <div className={className}>
      {title && (
        <div className="flex items-center gap-2 mb-1.5">
          <span className="text-xs font-semibold fin-text-primary">{title}</span>
          <span className="text-[10px] fin-text-tertiary">样本 {m.trades} 笔</span>
          {thin && <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-400">样本不足·勿信(需≥30)</span>}
        </div>
      )}
      <div className={`grid ${dense ? 'grid-cols-4 gap-1.5' : 'grid-cols-3 sm:grid-cols-4 gap-2'}`}>
        {tiles.map(t => (
          <div
            key={t.label}
            title={t.hint || ''}
            className={`rounded-lg border px-2 py-1.5 ${dark ? 'border-slate-700 bg-slate-800/40' : 'border-slate-200 bg-slate-50'} ${t.emph ? (dark ? 'ring-1 ring-amber-500/25' : 'ring-1 ring-amber-400/30') : ''}`}
          >
            <div className="text-[10px] fin-text-tertiary leading-tight">{t.label}</div>
            <div className={`text-sm font-bold font-mono leading-tight ${t.cls || 'fin-text-primary'}`}>{t.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default StrategyScorecard;
