import React, { useState, useEffect, useCallback } from 'react';
import { X, RefreshCw, Gavel, Plus, Check, ChevronDown, ChevronUp } from 'lucide-react';
import { GetAuctionFinal, GetStockIntraday } from '../../wailsjs/go/main/App';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';
import type { Stock } from '../types';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

interface AuctionRow {
  stockCode: string;
  name: string;
  price: number;
  pct: number;
  volume: number;
  amount: number;
  volumeRatio: number;
  floatMcap: number;
}

interface AuctionTick {
  time: string;
  price: number;
  pct: number;
  volume: number;
  amount: number;
}

interface AuctionBoardDialogProps {
  isOpen: boolean;
  onClose: () => void;
  watchlistSymbols: string[];
  onAddToWatchlist: (stock: Stock) => Promise<boolean>;
}

const fmtYi = (v: number) => (v > 0 ? (v / 1e8).toFixed(2) : '--');

const localDateStr = (d: Date) => {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
};

// 竞价过程迷你曲线(9:15-9:25,30s粒度)。前段可撤单常见"虚拉后回落",尾段9:20后才算真实意图。
const AuctionSparkline: React.FC<{ ticks: AuctionTick[]; up: string; down: string }> = ({ ticks, up, down }) => {
  if (!ticks || ticks.length < 2) {
    return <div className="text-xs opacity-60 py-3 text-center">该股无竞价过程数据(可能停牌/无委托)</div>;
  }
  const W = 560, H = 96, PAD = 6;
  const prices = ticks.map(t => t.price);
  const min = Math.min(...prices), max = Math.max(...prices);
  const span = max - min || 1;
  const x = (i: number) => PAD + (i / (ticks.length - 1)) * (W - PAD * 2);
  const y = (p: number) => PAD + (1 - (p - min) / span) * (H - PAD * 2);
  const pts = ticks.map((t, i) => `${x(i).toFixed(1)},${y(t.price).toFixed(1)}`).join(' ');
  const last = ticks[ticks.length - 1];
  const color = last.pct >= 0 ? up : down;
  // 9:20 分界线(之后不可撤单)
  const idx920 = ticks.findIndex(t => t.time >= '09:20');
  return (
    <div className="py-2">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxHeight: 110 }}>
        {idx920 > 0 && (
          <line x1={x(idx920)} y1={PAD} x2={x(idx920)} y2={H - PAD} stroke="currentColor" strokeOpacity="0.25" strokeDasharray="3,3" />
        )}
        <polyline points={pts} fill="none" stroke={color} strokeWidth="1.8" />
      </svg>
      <div className="flex justify-between text-[11px] opacity-70 px-1">
        <span>9:15</span>
        <span>9:20(不可撤单)┆</span>
        <span>
          9:25 定型 {last.price.toFixed(2)}({last.pct >= 0 ? '+' : ''}{last.pct.toFixed(2)}%) · 高{max.toFixed(2)} 低{min.toFixed(2)}
        </span>
      </div>
    </div>
  );
};

export const AuctionBoardDialog: React.FC<AuctionBoardDialogProps> = ({ isOpen, onClose, watchlistSymbols, onAddToWatchlist }) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const [date, setDate] = useState<string>(() => localDateStr(new Date()));
  const [rows, setRows] = useState<AuctionRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [tip, setTip] = useState('');
  const [expanded, setExpanded] = useState<string>('');
  const [ticks, setTicks] = useState<AuctionTick[]>([]);
  const [ticksLoading, setTicksLoading] = useState(false);
  const [added, setAdded] = useState<Record<string, boolean>>({});

  const load = useCallback(async (d: string, autoBack: boolean) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('竞价榜', 'go');
      return;
    }
    setLoading(true);
    setExpanded('');
    setTip('');
    try {
      let cur = d;
      for (let back = 0; back <= (autoBack ? 6 : 0); back++) {
        const list = (await GetAuctionFinal(cur, 100)) as unknown as AuctionRow[] | null;
        if (list && list.length > 0) {
          setRows(list);
          setDate(cur);
          if (cur !== d) setTip(`${d} 无数据,已显示最近有数据的 ${cur}`);
          return;
        }
        const prev = new Date(cur + 'T12:00:00');
        prev.setDate(prev.getDate() - 1);
        cur = localDateStr(prev);
      }
      setRows([]);
      setTip('该日无竞价数据。竞价榜每个交易日 9:25 后生成;历史数据从采集启用日起逐日积累。');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (isOpen) load(localDateStr(new Date()), true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  const toggleExpand = async (code: string) => {
    if (expanded === code) {
      setExpanded('');
      return;
    }
    setExpanded(code);
    setTicks([]);
    setTicksLoading(true);
    try {
      const res: any = await GetStockIntraday(code, date);
      setTicks((res && res.auction) || []);
    } finally {
      setTicksLoading(false);
    }
  };

  const handleAdd = async (r: AuctionRow) => {
    const preClose = Math.abs(100 + r.pct) > 0.001 ? r.price / (1 + r.pct / 100) : 0;
    const ok = await onAddToWatchlist({
      symbol: r.stockCode,
      name: r.name,
      price: r.price,
      change: r.price - preClose,
      changePercent: r.pct,
      volume: r.volume,
      amount: r.amount,
      marketCap: r.floatMcap > 0 ? `${fmtYi(r.floatMcap)}亿` : '',
      sector: '',
      open: 0,
      high: 0,
      low: 0,
      preClose,
    });
    if (ok) setAdded(prev => ({ ...prev, [r.stockCode]: true }));
  };

  if (!isOpen) return null;

  const pctClass = (v: number) => (v > 0 ? cc.upClass : v < 0 ? cc.downClass : '');

  return (
    <div className="fixed inset-0 z-[9000] flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className={`w-[860px] max-w-[94vw] max-h-[86vh] flex flex-col rounded-xl border fin-divider fin-panel shadow-2xl ${colors.isDark ? 'text-slate-200' : 'text-slate-700'}`}>
        {/* 头部 */}
        <div className="flex items-center gap-3 px-5 py-3.5 border-b fin-divider">
          <Gavel className="h-4.5 w-4.5 text-amber-400" />
          <div>
            <div className="font-semibold">集合竞价定型榜</div>
            <div className="text-[11px] opacity-60">9:25 撮合结果 · 按竞价金额排序 · 自建采集数据,点行展开竞价过程曲线</div>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <input
              type="date"
              value={date}
              onChange={e => e.target.value && load(e.target.value, false)}
              className={`rounded border fin-divider px-2 py-1 text-xs outline-none ${colors.isDark ? 'bg-slate-800 text-slate-200' : 'bg-white text-slate-700'}`}
              style={{ colorScheme: colors.isDark ? 'dark' : 'light' }}
            />
            <button onClick={() => load(date, false)} disabled={loading} className="p-1.5 rounded hover:bg-white/10 disabled:opacity-50" title="刷新">
              <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
            <button onClick={onClose} className="p-1.5 rounded hover:bg-white/10">
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* 内容 */}
        <div className="flex-1 overflow-y-auto px-3 py-2">
          {tip && <div className="text-xs text-amber-400/90 px-2 py-1.5">{tip}</div>}
          {loading && rows.length === 0 && <div className="py-16 text-center text-sm opacity-60">加载中...</div>}
          {!loading && rows.length === 0 && !tip && <div className="py-16 text-center text-sm opacity-60">暂无数据</div>}
          {rows.length > 0 && (
            <table className="w-full text-xs">
              <thead>
                <tr className="opacity-60 text-left">
                  <th className="px-2 py-1.5 w-8">#</th>
                  <th className="px-2 py-1.5">名称/代码</th>
                  <th className="px-2 py-1.5 text-right">竞价价</th>
                  <th className="px-2 py-1.5 text-right">幅度</th>
                  <th className="px-2 py-1.5 text-right">竞价金额(亿)</th>
                  <th className="px-2 py-1.5 text-right">量比</th>
                  <th className="px-2 py-1.5 text-right">流通市值(亿)</th>
                  <th className="px-2 py-1.5 w-16 text-center">自选</th>
                  <th className="px-2 py-1.5 w-8"></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => {
                  const inWatch = added[r.stockCode] || watchlistSymbols.includes(r.stockCode);
                  const isOpenRow = expanded === r.stockCode;
                  return (
                    <React.Fragment key={r.stockCode}>
                      <tr
                        onClick={() => toggleExpand(r.stockCode)}
                        className={`cursor-pointer border-t fin-divider ${colors.isDark ? 'hover:bg-slate-800/60' : 'hover:bg-slate-100'}`}
                      >
                        <td className="px-2 py-1.5 opacity-50">{i + 1}</td>
                        <td className="px-2 py-1.5">
                          <span className="font-medium">{r.name}</span>
                          <span className="ml-1.5 opacity-50">{r.stockCode}</span>
                        </td>
                        <td className={`px-2 py-1.5 text-right font-mono ${pctClass(r.pct)}`}>{r.price.toFixed(2)}</td>
                        <td className={`px-2 py-1.5 text-right font-mono ${pctClass(r.pct)}`}>{r.pct >= 0 ? '+' : ''}{r.pct.toFixed(2)}%</td>
                        <td className="px-2 py-1.5 text-right font-mono">{fmtYi(r.amount)}</td>
                        <td className={`px-2 py-1.5 text-right font-mono ${r.volumeRatio >= 3 ? 'text-amber-400' : ''}`}>{r.volumeRatio > 0 ? r.volumeRatio.toFixed(2) : '--'}</td>
                        <td className="px-2 py-1.5 text-right font-mono">{fmtYi(r.floatMcap)}</td>
                        <td className="px-2 py-1.5 text-center" onClick={e => e.stopPropagation()}>
                          {inWatch ? (
                            <Check className="h-3.5 w-3.5 inline text-emerald-400" />
                          ) : (
                            <button onClick={() => handleAdd(r)} className="p-1 rounded hover:bg-white/10" title="加自选">
                              <Plus className="h-3.5 w-3.5" />
                            </button>
                          )}
                        </td>
                        <td className="px-2 py-1.5 opacity-50">{isOpenRow ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}</td>
                      </tr>
                      {isOpenRow && (
                        <tr className="border-t fin-divider">
                          <td colSpan={9} className={`px-4 ${colors.isDark ? 'bg-slate-900/40' : 'bg-slate-50'}`}>
                            {ticksLoading ? (
                              <div className="text-xs opacity-60 py-3 text-center">竞价过程加载中...</div>
                            ) : (
                              <AuctionSparkline ticks={ticks} up={cc.upColor} down={cc.downColor} />
                            )}
                          </td>
                        </tr>
                      )}
                    </React.Fragment>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
};

export default AuctionBoardDialog;
