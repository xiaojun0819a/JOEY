import React, { useState, useEffect, useCallback } from 'react';
import { X, RefreshCw, Gavel, Plus, Check, ChevronDown, ChevronUp } from 'lucide-react';
import { GetAuctionFinal, GetAuctionPicksG, GetAuctionPicksC, GetStockIntraday, GetStockFocusTicks } from '../../wailsjs/go/main/App';
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
  prevVolume?: number; // 昨日成交量(手),后端从日K补齐
  matchG?: boolean;    // 命中「集合竞价稳健版选股公式 V2.1」(G),后端全市场评估
  matchC?: boolean;    // 命中「集合竞价·温和放量高开选股 v2.0」(C),后端全市场评估
}

interface AuctionTick {
  time: string;
  price: number;
  pct: number;
  volume: number;
  amount: number;
  b1?: number; // 买一量(重点池竞价时段有值)
  s1?: number; // 卖一量;min(b1,s1)=匹配量,差值=未匹配量(在多的一侧)
}

interface AuctionBoardDialogProps {
  isOpen: boolean;
  onClose: () => void;
  watchlistSymbols: string[];
  onAddToWatchlist: (stock: Stock) => Promise<boolean>;
  onOpenChart?: (symbol: string, name: string, preClose: number) => void; // 点名字进独立竞价大视图
}

const fmtYi = (v: number) => (v > 0 ? (v / 1e8).toFixed(2) : '--');

// 好竞价结构判定(据《集合竞价》文档的看涨形态:一字锁死/资金抢筹/诱空洗盘/稳步吸筹)。
// 注:文档形态靠竞价过程红绿柱演化,榜单只有 9:25 定型值,此处用"翻红+放量"最简代理;
// 真正的形态识别(9:20后红柱放大/绿柱暴增)在点开的独立竞价视图里做。
function auctionStructure(r: { pct: number; volumeRatio: number }): { good: boolean; tag: string; strong: boolean } {
  if (r.pct >= 5 && r.volumeRatio >= 2) return { good: true, tag: '抢筹', strong: true };   // 资金抢筹/强势翻红放量
  if (r.pct > 0.3 && r.volumeRatio >= 1.5) return { good: true, tag: '吸筹', strong: false }; // 稳步吸筹/温和翻红
  return { good: false, tag: '', strong: false };
}

// 优选个股 =「集合竞价稳健版选股公式 V2.1」(G),16 条逐条由后端 GetAuctionPicksG 全市场精确评估
// (适度高开1~4% + 竞价放量竞昨量比4~15% + 竞价额强 + 趋势向上 + 非高位 + 昨日不过热 + 良好承接 …)。
// 榜单只有 9:25 定型值,无法在前端算日K技术面,故直接用后端标记的 matchG。


const localDateStr = (d: Date) => {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
};

// 通达信式竞价图:上=匹配价折线;下=量区,底部正立柱为匹配量,顶部倒挂柱为未匹配量
// (红=未匹配在买侧/买盘意愿强,绿=在卖侧)。未匹配量需盘口数据,目前仅自选/持仓股(重点池3s采集)有。
const AuctionSparkline: React.FC<{ ticks: AuctionTick[]; up: string; down: string }> = ({ ticks, up, down }) => {
  if (!ticks || ticks.length < 2) {
    return <div className="text-xs opacity-60 py-3 text-center">该股无竞价过程数据(可能停牌/无委托,或早于采集启用日)</div>;
  }
  const W = 560, PH = 84, VH = 64, GAP = 8, PAD = 6;
  const H = PH + GAP + VH;
  const prices = ticks.map(t => t.price);
  const min = Math.min(...prices), max = Math.max(...prices);
  const span = max - min || 1;
  const n = ticks.length;
  const x = (i: number) => PAD + (i / (n - 1)) * (W - PAD * 2);
  const y = (p: number) => PAD + (1 - (p - min) / span) * (PH - PAD * 2);
  const pts = ticks.map((t, i) => `${x(i).toFixed(1)},${y(t.price).toFixed(1)}`).join(' ');
  const last = ticks[n - 1];
  const lineColor = last.pct >= 0 ? up : down;
  const idx920 = ticks.findIndex(t => t.time >= '09:20');

  // 量区:有盘口(b1/s1)→匹配量=min,未匹配=差值挂顶;无盘口→退化为匹配量柱
  const hasDepth = ticks.some(t => (t.b1 || 0) > 0 || (t.s1 || 0) > 0);
  const matched = ticks.map(t => (hasDepth ? Math.min(t.b1 || 0, t.s1 || 0) : t.volume || 0));
  const unmatched = ticks.map(t => (hasDepth ? Math.abs((t.b1 || 0) - (t.s1 || 0)) : 0));
  const unmatchedBuySide = ticks.map(t => (t.b1 || 0) > (t.s1 || 0));
  const volMax = Math.max(...matched, ...unmatched, 1);
  const volTop = PH + GAP;
  const barW = Math.max(1.2, ((W - PAD * 2) / n) * 0.6);
  const vh = (v: number) => (v / volMax) * (VH - 4);

  return (
    <div className="py-2">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxHeight: H + 14 }}>
        {idx920 > 0 && (
          <line x1={x(idx920)} y1={PAD} x2={x(idx920)} y2={H} stroke="currentColor" strokeOpacity="0.25" strokeDasharray="3,3" />
        )}
        <polyline points={pts} fill="none" stroke={lineColor} strokeWidth="1.8" />
        <line x1={PAD} y1={volTop} x2={W - PAD} y2={volTop} stroke="currentColor" strokeOpacity="0.15" />
        {ticks.map((t, i) => {
          const cx = x(i) - barW / 2;
          const m = vh(matched[i]);
          const u = vh(unmatched[i]);
          return (
            <g key={i}>
              {m > 0.5 && (
                <rect x={cx} y={volTop + VH - m} width={barW} height={m}
                  fill={t.price >= (ticks[i - 1]?.price ?? t.price) ? up : down} opacity="0.85" />
              )}
              {u > 0.5 && (
                <rect x={cx} y={volTop} width={barW} height={u}
                  fill={unmatchedBuySide[i] ? up : down} opacity="0.45" />
              )}
            </g>
          );
        })}
      </svg>
      <div className="flex justify-between text-[11px] opacity-70 px-1">
        <span>9:15</span>
        <span>9:20┆不可撤单</span>
        <span>9:25 定型 {last.price.toFixed(2)}({last.pct >= 0 ? '+' : ''}{last.pct.toFixed(2)}%)</span>
      </div>
      <div className="text-[11px] opacity-55 px-1 mt-0.5">
        {hasDepth
          ? '下柱=匹配量,顶部倒挂=未匹配量(红:剩在买侧·买盘强;绿:剩在卖侧)'
          : '柱=匹配量;未匹配量仅自选/持仓股有(加自选后次日生效)'}
      </div>
    </div>
  );
};

export const AuctionBoardDialog: React.FC<AuctionBoardDialogProps> = ({ isOpen, onClose, watchlistSymbols, onAddToWatchlist, onOpenChart }) => {
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
  const [filterMode, setFilterMode] = useState<'all' | 'good' | 'G' | 'C'>('all'); // 全部/好结构/优选G/优选C
  const [picks, setPicks] = useState<{ G: AuctionRow[] | null; C: AuctionRow[] | null }>({ G: null, C: null }); // 全市场公式命中,懒加载
  const [picksLoading, setPicksLoading] = useState(false);

  const load = useCallback(async (d: string, autoBack: boolean) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('竞价榜', 'go');
      return;
    }
    setLoading(true);
    setExpanded('');
    setTip('');
    setPicks({ G: null, C: null }); // 换日重置公式命中缓存
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

  // 切到"优选G/优选C"时按需全市场评估公式(候选常不在竞价额 Top100 内,故走专用全市场接口)
  useEffect(() => {
    if ((filterMode !== 'G' && filterMode !== 'C') || picks[filterMode] !== null || !date || !isWailsGoReady()) return;
    const mode = filterMode;
    let alive = true;
    setPicksLoading(true);
    (async () => {
      try {
        const fn = mode === 'G' ? GetAuctionPicksG : GetAuctionPicksC;
        const list = (await fn(date)) as unknown as AuctionRow[] | null;
        if (alive) setPicks(p => ({ ...p, [mode]: list || [] }));
      } catch {
        if (alive) setPicks(p => ({ ...p, [mode]: [] }));
      } finally {
        if (alive) setPicksLoading(false);
      }
    })();
    return () => { alive = false; };
  }, [filterMode, picks, date]);

  const toggleExpand = async (code: string) => {
    if (expanded === code) {
      setExpanded('');
      return;
    }
    setExpanded(code);
    setTicks([]);
    setTicksLoading(true);
    try {
      // 优先重点池3s线(带买一/卖一量→可画未匹配量),回落全A竞价5s线(只有匹配量)
      const [res, focus] = await Promise.all([
        GetStockIntraday(code, date) as Promise<any>,
        (GetStockFocusTicks(code, date) as Promise<any>).catch(() => null),
      ]);
      const focusAuction = ((focus as AuctionTick[]) || []).filter(
        t => t.time <= '09:26:00' && ((t.b1 || 0) > 0 || (t.s1 || 0) > 0)
      );
      setTicks(focusAuction.length >= 2 ? focusAuction : ((res && res.auction) || []));
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
            <button
              onClick={() => setFilterMode(m => m === 'good' ? 'all' : 'good')}
              className={`rounded border px-2.5 py-1 text-xs font-medium transition-colors ${
                filterMode === 'good'
                  ? 'border-rose-400/70 text-rose-300 bg-rose-500/15'
                  : colors.isDark ? 'border-slate-700 text-slate-400 hover:border-rose-400/50 hover:text-rose-300' : 'border-slate-300 text-slate-500 hover:border-rose-400/60'
              }`}
              title="好结构:翻红+放量(量比≥1.5)。看涨形态含一字锁死/资金抢筹/诱空洗盘/稳步吸筹;剔除竞价收绿、缩量的弱势票。"
            >
              好结构{filterMode === 'good' ? ' ✓' : ''}
            </button>
            <button
              onClick={() => setFilterMode(m => m === 'G' ? 'all' : 'G')}
              className={`rounded border px-2.5 py-1 text-xs font-medium transition-colors ${
                filterMode === 'G'
                  ? 'border-amber-400/70 text-amber-300 bg-amber-500/15'
                  : colors.isDark ? 'border-slate-700 text-slate-400 hover:border-amber-400/50 hover:text-amber-300' : 'border-slate-300 text-slate-500 hover:border-amber-400/60'
              }`}
              title="优选G(集合竞价稳健版 V2.1):适度高开1~4% + 竞价放量(竞昨量比4~15%) + 竞价额强 + 昨额≥2亿 + 趋势向上(站上20线/5≥10/20线上行) + 非高位 + 昨日不过热 + 良好承接。16条全市场精确筛,极严,常空仓。"
            >
              优选G{filterMode === 'G' ? ' ✓' : ''}
            </button>
            <button
              onClick={() => setFilterMode(m => m === 'C' ? 'all' : 'C')}
              className={`rounded border px-2.5 py-1 text-xs font-medium transition-colors ${
                filterMode === 'C'
                  ? 'border-sky-400/70 text-sky-300 bg-sky-500/15'
                  : colors.isDark ? 'border-slate-700 text-slate-400 hover:border-sky-400/50 hover:text-sky-300' : 'border-slate-300 text-slate-500 hover:border-sky-400/60'
              }`}
              title="优选C(集合竞价·温和放量高开 v2.0):温和高开0.5~3% + 竞价量≥前5日均量8% + 换手≥0.15% + 竞价额>500万 + 昨日未涨停/跌停 + 10日涨幅<25% + 站上20日线 + 非次新/非ST/非北交。全市场精确筛。"
            >
              优选C{filterMode === 'C' ? ' ✓' : ''}
            </button>
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
          {(filterMode === 'G' || filterMode === 'C') && picksLoading && (
            <div className="py-16 text-center text-sm opacity-60">
              全市场评估{filterMode === 'G' ? '「集合竞价稳健版 V2.1」' : '「集合竞价·温和放量高开 v2.0」'}中...
            </div>
          )}
          {(filterMode === 'G' || filterMode === 'C') && !picksLoading && picks[filterMode] !== null && picks[filterMode]!.length === 0 && (
            <div className="py-16 text-center text-sm opacity-60">
              当日全市场无个股满足优选{filterMode}全部条件<br />
              <span className="text-xs opacity-70">
                {filterMode === 'G'
                  ? '该公式极严(竞价放量4~15%∩温和高开1~4%∩趋势健康),空仓属正常'
                  : '该公式要求竞价量≥前5日均量8%且温和高开,空仓属正常'}
              </span>
            </div>
          )}
          {filterMode !== 'G' && filterMode !== 'C' && loading && rows.length === 0 && <div className="py-16 text-center text-sm opacity-60">加载中...</div>}
          {filterMode !== 'G' && filterMode !== 'C' && !loading && rows.length === 0 && !tip && <div className="py-16 text-center text-sm opacity-60">暂无数据</div>}
          {(() => {
            const displayRows = filterMode === 'good' ? rows.filter(r => auctionStructure(r).good)
              : filterMode === 'G' ? (picks.G || [])
              : filterMode === 'C' ? (picks.C || [])
              : rows;
            return displayRows.length > 0 && (
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
                {displayRows.map((r, i) => {
                  const inWatch = added[r.stockCode] || watchlistSymbols.includes(r.stockCode);
                  const isOpenRow = expanded === r.stockCode;
                  const struct = auctionStructure(r);
                  const preferred = !!r.matchG || !!r.matchC;
                  return (
                    <React.Fragment key={r.stockCode}>
                      <tr
                        onClick={() => toggleExpand(r.stockCode)}
                        className={`cursor-pointer border-t fin-divider ${colors.isDark ? 'hover:bg-slate-800/60' : 'hover:bg-slate-100'}`}
                      >
                        <td className="px-2 py-1.5 opacity-50">{i + 1}</td>
                        <td className="px-2 py-1.5">
                          <button
                            type="button"
                            className="font-medium hover:underline hover:text-accent-2 text-left"
                            onClick={e => {
                              e.stopPropagation();
                              const pc = Math.abs(100 + r.pct) > 0.001 ? r.price / (1 + r.pct / 100) : 0;
                              onOpenChart?.(r.stockCode, r.name, pc);
                            }}
                            title="打开独立集合竞价大视图"
                          >
                            {r.name}
                          </button>
                          {struct.good && (
                            <span className={`ml-1.5 rounded px-1 py-0.5 text-[10px] font-medium ${struct.strong ? 'bg-rose-500/20 text-rose-300' : 'bg-amber-500/20 text-amber-300'}`}>
                              {struct.tag}
                            </span>
                          )}
                          {preferred && (
                            <span className={`ml-1 rounded px-1 py-0.5 text-[10px] font-medium ${r.matchG ? 'bg-amber-500/20 text-amber-300' : 'bg-sky-500/20 text-sky-300'}`} title={r.matchG ? '命中集合竞价稳健版 V2.1(G)' : '命中集合竞价·温和放量高开 v2.0(C)'}>
                              优选{r.matchG ? 'G' : 'C'}
                            </span>
                          )}
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
            );
          })()}
        </div>
      </div>
    </div>
  );
};

export default AuctionBoardDialog;
