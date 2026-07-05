import React, { useEffect, useMemo, useState } from 'react';
import { getMarketStylePreference, type MarketStyleItem, type MarketStylePreference } from '../services/marketRegimeService';
import { useTheme } from '../contexts/ThemeContext';

const fmtPct = (value: number) => {
  if (!Number.isFinite(value)) return '--';
  const sign = value > 0 ? '+' : '';
  return `${sign}${value.toFixed(2)}%`;
};

const sortStyleItems = (items: MarketStyleItem[]) => {
  const order = ['large', 'mid', 'small', 'micro'];
  return [...items].sort((a, b) => order.indexOf(a.key) - order.indexOf(b.key));
};

const PLACEHOLDER_STYLE: MarketStylePreference = {
  label: '真实源同步中',
  subLabel: '等待风格指数',
  scenario: '数据同步中',
  strengthGap: 0,
  strongKey: '',
  weakKey: '',
  items: [],
  asOf: '--',
  available: false,
  dataNote: '当前窗口未连接到 Wails 后端桥接时，仅展示样式与规则说明；真实结论以桌面端后端返回为准。',
};

export const MarketRegimeBadge: React.FC = () => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [style, setStyle] = useState<MarketStylePreference>(PLACEHOLDER_STYLE);

  useEffect(() => {
    let stop = false;
    const load = async () => {
      const data = await getMarketStylePreference();
      if (!stop && data) setStyle(data);
    };
    load();
    const t = setInterval(load, 5 * 60 * 1000);
    return () => {
      stop = true;
      clearInterval(t);
    };
  }, []);

  const items = useMemo(() => sortStyleItems(style?.items || []), [style?.items]);

  const label = style.label || style.regimeFallback?.label || '数据加载中';
  const subLabel = style.subLabel || '风格数据同步中';
  const isPositive = label.includes('更强') || label.includes('抗跌') || label.includes('上涨');
  const isWeak = label.includes('领跌') || label.includes('弱');

  const cardTone = dark
    ? isWeak
      ? 'border-emerald-500/35 bg-emerald-500/10 text-emerald-200'
      : 'border-red-500/35 bg-red-500/10 text-red-200'
    : isWeak
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
      : 'border-red-200 bg-red-50 text-red-700';
  const mainTone = isWeak ? 'text-emerald-500' : isPositive ? 'text-red-500' : (dark ? 'text-slate-100' : 'text-slate-700');
  const panel = dark ? 'bg-slate-950 border-slate-700 text-slate-300' : 'bg-white border-slate-200 text-slate-600';
  const muted = dark ? 'text-slate-400' : 'text-slate-500';
  const strongText = dark ? 'text-slate-100' : 'text-slate-800';
  const divider = dark ? 'border-slate-800' : 'border-slate-200';

  return (
    <div className="relative group block shrink-0" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
      <div
        className={`rounded-lg border cursor-default shadow-sm overflow-hidden flex flex-col items-center justify-center ${cardTone}`}
        style={{ width: 128, height: 44, padding: '3px 8px' }}
      >
        <div
          className={`w-full text-center font-bold truncate ${mainTone}`}
          style={{ fontSize: 16, lineHeight: '18px' }}
        >
          {label}
        </div>
        <div
          className={`w-full text-center truncate ${dark ? 'text-slate-300' : 'text-slate-500'}`}
          style={{ marginTop: 2, fontSize: 11, lineHeight: '13px' }}
        >
          {subLabel}
        </div>
      </div>

      <div
        className={`absolute right-0 top-full mt-2 z-50 rounded-xl border shadow-2xl p-3 text-xs opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-opacity overflow-y-auto ${panel}`}
        style={{ width: 520, maxWidth: 'calc(100vw - 24px)', maxHeight: 'calc(100vh - 76px)' }}
      >
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className={`text-sm font-semibold ${strongText}`}>市场风格偏好</div>
            <div className={`mt-1 ${muted}`}>
              基于沪深300、中证500、中证1000、微盘股四类风格对比，1% 强弱差作为显著阈值。
            </div>
          </div>
          <div className="text-right shrink-0">
            <div className={`font-semibold ${mainTone}`}>{label}</div>
            <div className={muted}>{style.asOf || '--'} · {style.scenario || '同步中'}</div>
          </div>
        </div>

        <div className={`mt-3 grid grid-cols-4 gap-1.5 border-y py-2 ${divider}`}>
          {items.length > 0 ? items.map(item => {
            const up = item.changePercent >= 0;
            return (
              <div key={item.key} className={`rounded-md px-2 py-1.5 ${dark ? 'bg-slate-900/80' : 'bg-slate-50'}`}>
                <div className={muted}>{item.name}</div>
                <div className={`mt-0.5 font-mono font-semibold ${up ? 'text-red-500' : 'text-emerald-500'}`}>
                  {fmtPct(item.changePercent)}
                </div>
                <div className={`mt-0.5 truncate text-[10px] ${muted}`} title={`${item.indexName} · ${item.source}`}>
                  {item.indexName}
                </div>
              </div>
            );
          }) : (
            <div className={`col-span-4 rounded-md px-3 py-2 ${dark ? 'bg-slate-900/80' : 'bg-slate-50'}`}>
              真实风格数据等待后端同步；不会用假数据填充结论。
            </div>
          )}
        </div>

        <div className="mt-2 grid gap-3 leading-relaxed" style={{ gridTemplateColumns: '1fr 1.1fr' }}>
          <div>
            <div className={`font-semibold ${strongText}`}>主力净流入</div>
            <div className="mt-1">
              主力净流入 =（大单买入额 + 特大单买入额）-（大单卖出额 + 特大单卖出额），反映当天主力资金净流入情况。
            </div>
            <div className={`mt-2 font-semibold ${strongText}`}>大小单划分</div>
            <div className="mt-1 space-y-0.5">
              <div>特大单：主板 2000手以上或100万元以上；创业板 2000手以上或50万元以上</div>
              <div>大单：主板 600-2000手或30-100万元；创业板 600-2000手或20-50万元</div>
              <div>中单：主板 100-600手或5-30万元；创业板 100-600手或5-20万元</div>
              <div>小单：小于100手或5万元</div>
            </div>
          </div>

          <div>
            <div className={`font-semibold ${strongText}`}>风格策略说明</div>
            <div className="mt-1 space-y-0.5">
              <div>1. 全面上涨且差异≥1%：最强风格更强，最弱风格相对弱势。</div>
              <div>2. 全面上涨且差异&lt;1%：无明显偏好，整体风格上涨。</div>
              <div>3. 涨跌分化且差异≥1%：最强风格更强，最弱风格弱势。</div>
              <div>4. 涨跌分化且差异&lt;1%：无明显偏好，最强风格相对强势。</div>
              <div>5. 全面下跌且差异&lt;1%：无明显偏好，风格整体下跌。</div>
              <div>6. 全面下跌且差异≥1%：最弱风格领跌，最强风格相对抗跌。</div>
            </div>
          </div>
        </div>

        <div className={`mt-2 border-t pt-2 ${divider}`}>
          <span className={strongText}>当前口径：</span>
          强弱差 {fmtPct(style.strengthGap)}。{style.dataNote || '数据源：指数实时行情 + 全A快照。'}
        </div>
      </div>
    </div>
  );
};

export default MarketRegimeBadge;
