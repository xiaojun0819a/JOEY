import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Wallet, X, Trash2, LogOut, RefreshCw, Loader2, Undo2, Plus, Search, LineChart, Radar } from 'lucide-react';
import StrategyAccountDialog from './StrategyAccountDialog';
import TailForwardDialog from './TailForwardDialog';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';
import { StrategyScorecard } from './StrategyScorecard';
import {
  addPaperPosition, listPaperPositions, updatePaperPosition, closePaperPosition, reopenPaperPosition, deletePaperPosition, getPaperStats,
  applyPaperExitRules, exitReasonText, getPaperRiskSummary,
  SOURCE_LABEL, STRATEGY_SOURCE_FILTERS, type PaperPosition, type PaperStats, type PaperSourceStat, type PaperRiskSummary,
} from '../services/paperService';
import { getStockRealTimeData, searchStocks } from '../services/stockService';
import type { Stock } from '../types';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onOpenStock?: (stock: Stock) => void | Promise<void>;
}

type SourceFilter = string;

const round2 = (v: number) => Math.round(v * 100) / 100;
const formatOpenDate = (value?: string) => {
  if (!value) return '--';
  return value.length > 10 ? value.slice(0, 16).replace('T', ' ') : value;
};
const parseDateOnly = (value?: string) => {
  const dateText = (value || '').slice(0, 10);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateText)) return null;
  const [year, month, day] = dateText.split('-').map(Number);
  return new Date(year, month - 1, day);
};
const getHoldDays = (openDate?: string, closeDate?: string) => {
  const start = parseDateOnly(openDate);
  if (!start) return '--';
  const end = parseDateOnly(closeDate) || new Date();
  start.setHours(0, 0, 0, 0);
  end.setHours(0, 0, 0, 0);
  const days = Math.floor((end.getTime() - start.getTime()) / 86400000) + 1;
  return `${Math.max(days, 1)}天`;
};
const normalizeSymbol = (value: string) => {
  const raw = value.trim().toLowerCase();
  if (!raw) return '';
  if (/^(sh|sz|bj)\d{6}$/.test(raw)) return raw;
  if (/^\d{6}$/.test(raw)) {
    if (raw.startsWith('6') || raw.startsWith('9')) return `sh${raw}`;
    if (raw.startsWith('8') || raw.startsWith('4')) return `bj${raw}`;
    return `sz${raw}`;
  }
  return raw;
};

const emptyStats = (): PaperStats => ({
  openCount: 0,
  closedCount: 0,
  winRate: 0,
  expectancy: 0,
  payoffRatio: 0,
  profitFactor: 0,
  maxLoss: 0,
  bySource: [],
});

const normalizeSource = (source?: string) => source || 'manual';

const toStock = (position: PaperPosition): Stock => ({
  symbol: String(position.symbol || '').trim().toLowerCase(),
  name: position.name || position.symbol,
  price: position.currentPrice || position.closePrice || position.openPrice || position.costPrice || 0,
  change: 0,
  changePercent: 0,
  volume: 0,
  amount: 0,
  marketCap: '',
  sector: '',
  open: 0,
  high: 0,
  low: 0,
  preClose: 0,
});

const sourceMatchesFilter = (source: string | undefined, filter: SourceFilter) => {
  const src = normalizeSource(source);
  if (filter === 'all') return true;
  if (filter === 'lowbuy-v1') return src === filter || src === 'lowbuy';
  if (filter === 'limit-pullback-v1') return src === filter;
  if (filter === 'triple-volume-v5') return src === filter || src === 'triple-volume';
  if (filter === 'tail-buy-v6') return src === filter || src === 'tail-buy';
  if (filter === 'hot-money-v7') return src === filter || src === 'hot-money';
  if (filter === 'dip-entry-v8') return src === filter || src === 'dip-entry';
  if (filter === 'monster-v9') return src === filter || src === 'monster' || src === '捉妖策略6';
  if (filter === 'monster-v10') return src === filter || src === '捉妖策略10';
  if (filter === 'taillazy-v2') return src === filter || src === 'taillazy';
  if (filter === 'latechase-v3') return src === filter || src === 'latechase';
  if (filter === 'wave-v1') return src === filter || src === 'wave';
  if (filter === 'caoyuan-standard4a') return src === filter || src === 'caoyuan-standard4a-strict';
  if (filter === 'caoyuan-zhuang4b') return src === filter || src === 'caoyuan-zhuang4b-strict';
  return src === filter;
};

const buildPaperStats = (positions: PaperPosition[]): PaperStats => {
  const out = emptyStats();
  const bySource = new Map<string, PaperSourceStat>();
  const sumWin = new Map<string, number>();
  const sumLoss = new Map<string, number>();
  const winCnt = new Map<string, number>();
  const lossCnt = new Map<string, number>();
  const maxLoss = new Map<string, number>();
  let totalWin = 0;
  let globalSumRet = 0;
  let globalSumWin = 0;
  let globalSumLoss = 0;
  let globalWinCnt = 0;
  let globalLossCnt = 0;
  let globalMaxLoss = 0;

  const ensureSource = (source: string) => {
    const key = normalizeSource(source);
    let stat = bySource.get(key);
    if (!stat) {
      stat = {
        source: key,
        total: 0,
        closed: 0,
        win: 0,
        winRate: 0,
        avgReturn: 0,
        totalReturn: 0,
        avgWin: 0,
        avgLoss: 0,
        payoffRatio: 0,
        profitFactor: 0,
        maxLoss: 0,
      };
      bySource.set(key, stat);
    }
    return stat;
  };

  positions.forEach((p) => {
    const src = normalizeSource(p.source);
    const st = ensureSource(src);
    st.total += 1;
    if (p.status === 'open') {
      out.openCount += 1;
      return;
    }
    const ret = Number(p.profitPct || 0);
    st.closed += 1;
    st.totalReturn += ret;
    out.closedCount += 1;
    globalSumRet += ret;
    if (ret > 0) {
      st.win += 1;
      totalWin += 1;
      sumWin.set(src, (sumWin.get(src) || 0) + ret);
      winCnt.set(src, (winCnt.get(src) || 0) + 1);
      globalSumWin += ret;
      globalWinCnt += 1;
    } else {
      sumLoss.set(src, (sumLoss.get(src) || 0) + ret);
      lossCnt.set(src, (lossCnt.get(src) || 0) + 1);
      globalSumLoss += ret;
      globalLossCnt += 1;
      if (ret < (maxLoss.get(src) || 0)) maxLoss.set(src, ret);
      if (ret < globalMaxLoss) globalMaxLoss = ret;
    }
  });

  if (out.closedCount > 0) {
    out.winRate = round2(totalWin * 100 / out.closedCount);
    out.expectancy = round2(globalSumRet / out.closedCount);
  }
  if (globalWinCnt > 0 && globalLossCnt > 0 && globalSumLoss !== 0) {
    out.payoffRatio = round2((globalSumWin / globalWinCnt) / Math.abs(globalSumLoss / globalLossCnt));
  }
  if (globalSumLoss !== 0) out.profitFactor = round2(globalSumWin / Math.abs(globalSumLoss));
  out.maxLoss = round2(globalMaxLoss);

  bySource.forEach((st, src) => {
    if (st.closed > 0) {
      st.winRate = round2(st.win * 100 / st.closed);
      st.avgReturn = round2(st.totalReturn / st.closed);
    }
    const srcWinCnt = winCnt.get(src) || 0;
    const srcLossCnt = lossCnt.get(src) || 0;
    const srcWinSum = sumWin.get(src) || 0;
    const srcLossSum = sumLoss.get(src) || 0;
    if (srcWinCnt > 0) st.avgWin = round2(srcWinSum / srcWinCnt);
    if (srcLossCnt > 0) st.avgLoss = round2(srcLossSum / srcLossCnt);
    if (st.avgLoss !== 0) st.payoffRatio = round2(st.avgWin / Math.abs(st.avgLoss));
    if (srcLossSum !== 0) st.profitFactor = round2(srcWinSum / Math.abs(srcLossSum));
    st.maxLoss = round2(maxLoss.get(src) || 0);
  });

  out.bySource = Array.from(bySource.values()).sort((a, b) => b.total - a.total);
  return out;
};

export const PaperPortfolioDialog: React.FC<Props> = ({ isOpen, onClose, onOpenStock }) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const dark = colors.isDark;
  const [list, setList] = useState<PaperPosition[]>([]);
  const [stats, setStats] = useState<PaperStats | null>(null);
  const [risk, setRisk] = useState<PaperRiskSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [closingId, setClosingId] = useState<number | null>(null);
  const [closePriceInput, setClosePriceInput] = useState('');
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>('all');
  const [showManualAdd, setShowManualAdd] = useState(false);
  const [showAccount, setShowAccount] = useState(false);
  const [showTailFwd, setShowTailFwd] = useState(false);
  const [manualSymbol, setManualSymbol] = useState('');
  const [manualName, setManualName] = useState('');
  const [manualPrice, setManualPrice] = useState('');
  const [manualShares, setManualShares] = useState('1000');
  const [manualLoading, setManualLoading] = useState(false);
  const [manualMessage, setManualMessage] = useState('');
  const timer = useRef<ReturnType<typeof setInterval>>();

  const refresh = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    const [l, s, rk] = await Promise.all([listPaperPositions(), getPaperStats(), getPaperRiskSummary()]);
    setList(l);
    setStats(s);
    setRisk(rk);
    if (showLoading) setLoading(false);
  }, []);

  useEffect(() => {
    if (!isOpen) { if (timer.current) clearInterval(timer.current); return; }
    // 打开时先按低吸退出纪律自动平仓一次（确认收盘的前向日K），再刷新
    void (async () => { await applyPaperExitRules(); await refresh(true); })();
    timer.current = setInterval(() => void refresh(false), 12000); // 盘中实时刷新
    return () => { if (timer.current) clearInterval(timer.current); };
  }, [isOpen, refresh]);

  const lookupManualStock = useCallback(async (rawSymbol = manualSymbol) => {
    const symbol = normalizeSymbol(rawSymbol);
    if (!symbol) {
      setManualMessage('先输入股票代码');
      return;
    }
    setManualLoading(true);
    setManualMessage('');
    setManualSymbol(symbol);
    try {
      const [matches, realtime] = await Promise.all([
        searchStocks(symbol.replace(/^(sh|sz|bj)/, '')).catch(() => []),
        getStockRealTimeData([symbol]).catch(() => []),
      ]);
      const rt = realtime.find(item => item.symbol.toLowerCase() === symbol) || realtime[0];
      const match = matches.find(item => item.symbol.toLowerCase() === symbol || item.symbol.toLowerCase().endsWith(symbol.slice(-6))) || matches[0];
      if (rt?.name || match?.name) setManualName(rt?.name || match?.name || '');
      if (rt?.price && rt.price > 0) setManualPrice(rt.price.toFixed(2));
      if (!rt && !match) setManualMessage('没查到股票信息，可手动填写名称和成本价');
    } finally {
      setManualLoading(false);
    }
  }, [manualSymbol]);

  if (!isOpen) return null;

  const filteredList = list.filter(p => sourceMatchesFilter(p.source, sourceFilter));
  const visibleStats = sourceFilter === 'all' ? (stats || buildPaperStats(list)) : buildPaperStats(filteredList);
  const hasAnyPosition = list.length > 0;
  const hasVisiblePosition = filteredList.length > 0;

  const saveCost = async (p: PaperPosition, cost: number, shares: number) => {
    await updatePaperPosition(p.id, cost, shares);
    void refresh(false);
  };
  const doClose = async (p: PaperPosition) => {
    const price = Number(closePriceInput) || p.currentPrice || p.costPrice;
    await closePaperPosition(p.id, price);
    setClosingId(null);
    setClosePriceInput('');
    void refresh(false);
  };
  const doReopen = async (id: number) => { await reopenPaperPosition(id); void refresh(false); };
  const doDelete = async (id: number) => { await deletePaperPosition(id); void refresh(false); };

  const submitManualAdd = async () => {
    const symbol = normalizeSymbol(manualSymbol);
    const price = Number(manualPrice);
    const shares = Number(manualShares);
    if (!symbol) {
      setManualMessage('股票代码不能为空');
      return;
    }
    if (!Number.isFinite(price) || price <= 0) {
      setManualMessage('成本价/最新价必须大于0');
      return;
    }
    if (!Number.isFinite(shares) || shares <= 0) {
      setManualMessage('数量必须大于0');
      return;
    }
    setManualLoading(true);
    setManualMessage('');
    try {
      await addPaperPosition(symbol, manualName || symbol, 'manual', price, Math.round(shares));
      setSourceFilter('manual');
      setManualSymbol('');
      setManualName('');
      setManualPrice('');
      setManualShares('1000');
      setShowManualAdd(false);
      await refresh(true);
    } finally {
      setManualLoading(false);
    }
  };

  const th = `px-2 py-2 text-left font-medium text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const td = `px-2 py-1.5 text-xs`;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1180px] max-w-[96vw] h-[760px] max-h-[92vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        {/* header */}
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex flex-1 items-center gap-2 pr-4">
            <Wallet className="h-5 w-5 text-amber-400" />
            <button
              onClick={() => setShowAccount(true)}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border border-amber-400/40 bg-amber-400/10 text-amber-200 hover:bg-amber-400/20 transition-colors text-xs font-medium whitespace-nowrap"
              title="策略账户：10万固定本金·自动买卖·闭环反馈，看真实alpha"
            >
              <LineChart className="h-3.5 w-3.5" />
              <span>策略账户</span>
            </button>
            <button
              onClick={() => setShowTailFwd(true)}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border border-cyan-400/40 bg-cyan-400/10 text-cyan-200 hover:bg-cyan-400/20 transition-colors text-xs font-medium whitespace-nowrap"
              title="尾盘2:30实盘向前验证：实时盘口判可成交，只记能买进的票"
            >
              <Radar className="h-3.5 w-3.5" />
              <span>2:30买点</span>
            </button>
            <div className="flex-1 text-center">
              <div className="text-sm font-semibold fin-text-primary">模拟持仓 · 筛选系统胜率验证</div>
              <div className="text-[11px] fin-text-tertiary">现价×1000股建仓 · 净收益已扣双边成本(≈0.35%) · 仅低吸类自动按低吸纪律平仓 · 草元/波段/手动需单独规则或手动平仓</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={() => refresh(true)} className="p-1.5 rounded fin-hover" title="刷新">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 fin-text-secondary" />}
            </button>
            <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
          </div>
        </div>

        {/* 计分卡 */}
        {visibleStats.closedCount > 0 && (
          <div className={`px-5 py-2.5 border-b fin-divider ${dark ? 'bg-slate-900/40' : 'bg-slate-50'}`}>
            <StrategyScorecard
              title={`${sourceFilter === 'all' ? '模拟持仓' : SOURCE_LABEL[sourceFilter]} · 前向计分卡（扣成本净收益口径）`}
              metrics={{
                trades: visibleStats.closedCount,
                winRate: visibleStats.winRate,
                expectancy: visibleStats.expectancy,
                payoffRatio: visibleStats.payoffRatio,
                profitFactor: visibleStats.profitFactor,
                maxLoss: visibleStats.maxLoss,
              }}
            />
            {/* 风控概览 */}
            {risk && risk.positionCount > 0 && (
              <div className="mt-2 px-2.5 py-1.5 rounded-lg border fin-divider flex items-center gap-3 text-xs flex-wrap bg-white/[0.02]">
                <span className="fin-text-tertiary shrink-0">风控(稳健·自动平+预警)</span>
                <span className="fin-text-secondary">组合浮盈 <b className={risk.profitPct >= 0 ? 'text-red-400' : 'text-green-400'}>{risk.profitPct >= 0 ? '+' : ''}{risk.profitPct}%</b></span>
                <span className="fin-text-secondary">最大单票 <b className={risk.maxSinglePct > risk.singleCap ? 'text-amber-400' : ''}>{risk.maxSinglePct}%</b><span className="fin-text-tertiary">/上限{risk.singleCap}%</span></span>
                {risk.sectorTop[0] && <span className="fin-text-secondary">最大板块 <b className={risk.sectorTop[0].pct > risk.sectorCap ? 'text-amber-400' : ''}>{risk.sectorTop[0].name} {risk.sectorTop[0].pct}%</b><span className="fin-text-tertiary">/上限{risk.sectorCap}%</span></span>}
                <span className="fin-text-secondary">回撤 <b className={risk.drawdownAlert ? 'text-amber-400' : ''}>-{risk.drawdownFromPeak}%</b><span className="fin-text-tertiary">/预警{risk.drawdownAlertPct}%</span></span>
                {risk.warnings.length > 0 && <span className="text-amber-300 shrink-0">⚠️ {risk.warnings.length}项超限</span>}
              </div>
            )}
            {risk && risk.warnings.length > 0 && (
              <div className="mt-1 text-[11px] text-amber-300/90 leading-relaxed">{risk.warnings.join(' · ')}</div>
            )}
            {/* 分系统明细 */}
            <div className="flex items-center gap-3 mt-2 text-xs overflow-x-auto">
              <span className="fin-text-tertiary shrink-0">当前{SOURCE_LABEL[sourceFilter] || '全部'} · 持仓{visibleStats.openCount} · 已平{visibleStats.closedCount}</span>
              <span className="fin-text-tertiary">|</span>
              {visibleStats.bySource.map(s => (
                <span key={s.source} className="shrink-0 fin-text-secondary">
                  {SOURCE_LABEL[s.source] || s.source}
                  <span className="ml-1 fin-text-tertiary">{s.total}笔</span>
                  {s.closed > 0 && <span className={`ml-1 font-medium ${s.winRate >= 50 ? 'text-red-400' : 'text-green-400'}`}>胜{s.winRate.toFixed(0)}%</span>}
                  {s.closed > 0 && <span className={`ml-1 ${s.avgReturn >= 0 ? 'text-red-400' : 'text-green-400'}`}>期望{s.avgReturn >= 0 ? '+' : ''}{s.avgReturn.toFixed(1)}%</span>}
                  {s.closed > 0 && <span className="ml-1 fin-text-tertiary">赔{s.payoffRatio.toFixed(1)}</span>}
                </span>
              ))}
            </div>
          </div>
        )}
        {visibleStats.closedCount === 0 && (
          <div className={`flex items-center gap-3 px-5 py-2 border-b fin-divider text-xs ${dark ? 'bg-slate-900/40' : 'bg-slate-50'}`}>
            <span className="fin-text-tertiary">{SOURCE_LABEL[sourceFilter] || '全部'} · 持仓{visibleStats.openCount} · 暂无已平仓样本，平仓后生成计分卡</span>
          </div>
        )}

        {/* 来源筛选 */}
        <div className={`flex items-center justify-between gap-3 px-5 py-2 border-b fin-divider text-xs ${dark ? 'bg-slate-950/30' : 'bg-white'}`}>
          <div className="flex items-center gap-2">
            <span className="fin-text-tertiary">来源筛选</span>
            <div className={`inline-flex rounded-lg border overflow-hidden ${dark ? 'border-slate-700 bg-slate-900' : 'border-slate-200 bg-slate-50'}`}>
              {STRATEGY_SOURCE_FILTERS.map(f => {
                const active = sourceFilter === f.key;
                const count = list.filter(p => sourceMatchesFilter(p.source, f.key)).length;
                return (
                  <button
                    key={f.key}
                    type="button"
                    onClick={() => setSourceFilter(f.key)}
                    className={`px-3 py-1.5 border-r last:border-r-0 transition-colors ${dark ? 'border-slate-700' : 'border-slate-200'} ${
                      active
                        ? 'bg-amber-500/20 text-amber-300 font-semibold'
                        : 'fin-text-secondary hover:bg-white/10'
                    }`}
                  >
                    {f.label}<span className="ml-1 fin-text-tertiary">{count}</span>
                  </button>
                );
              })}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="fin-text-tertiary">
              计分卡按当前来源重算 · 低吸/波段独立验胜率
            </div>
            <button
              type="button"
              onClick={() => {
                setShowManualAdd(prev => !prev);
                setSourceFilter('manual');
                setManualMessage('');
              }}
              className={`inline-flex items-center gap-1 rounded border px-2.5 py-1.5 font-semibold transition-colors ${
                showManualAdd
                  ? 'border-amber-400/40 bg-amber-500/20 text-amber-200'
                  : dark
                    ? 'border-slate-700 bg-slate-900/70 text-slate-300 hover:border-amber-400/40 hover:text-amber-200'
                    : 'border-slate-200 bg-white text-slate-600 hover:border-amber-300 hover:text-amber-700'
              }`}
              title="手动加入模拟持仓组"
            >
              <Plus className="h-3.5 w-3.5" />
              新增手动持仓
            </button>
          </div>
        </div>

        {showManualAdd && (
          <div className={`border-b fin-divider px-5 py-2.5 text-xs ${dark ? 'bg-slate-950/45' : 'bg-slate-50'}`}>
            <div className="grid grid-cols-[148px_1fr_112px_112px_auto] items-end gap-2">
              <label className="min-w-0">
                <span className="mb-1 block fin-text-tertiary">股票代码</span>
                <input
                  value={manualSymbol}
                  onChange={event => {
                    setManualSymbol(event.target.value);
                    setManualMessage('');
                  }}
                  onBlur={() => void lookupManualStock()}
                  onKeyDown={event => {
                    if (event.key === 'Enter') void lookupManualStock();
                  }}
                  placeholder="如 600519"
                  className={`w-full rounded border px-2 py-1.5 font-mono outline-none ${
                    dark ? 'border-slate-700 bg-slate-900 text-slate-100' : 'border-slate-300 bg-white text-slate-800'
                  }`}
                />
              </label>
              <label className="min-w-0">
                <span className="mb-1 block fin-text-tertiary">股票名称</span>
                <input
                  value={manualName}
                  onChange={event => setManualName(event.target.value)}
                  placeholder="自动识别，可手动改"
                  className={`w-full rounded border px-2 py-1.5 outline-none ${
                    dark ? 'border-slate-700 bg-slate-900 text-slate-100' : 'border-slate-300 bg-white text-slate-800'
                  }`}
                />
              </label>
              <label>
                <span className="mb-1 block fin-text-tertiary">建仓价</span>
                <input
                  type="number"
                  step={0.01}
                  value={manualPrice}
                  onChange={event => setManualPrice(event.target.value)}
                  placeholder="最新价"
                  className={`w-full rounded border px-2 py-1.5 text-right font-mono outline-none ${
                    dark ? 'border-slate-700 bg-slate-900 text-slate-100' : 'border-slate-300 bg-white text-slate-800'
                  }`}
                />
              </label>
              <label>
                <span className="mb-1 block fin-text-tertiary">数量</span>
                <input
                  type="number"
                  step={100}
                  value={manualShares}
                  onChange={event => setManualShares(event.target.value)}
                  className={`w-full rounded border px-2 py-1.5 text-right font-mono outline-none ${
                    dark ? 'border-slate-700 bg-slate-900 text-slate-100' : 'border-slate-300 bg-white text-slate-800'
                  }`}
                />
              </label>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => void lookupManualStock()}
                  className={`inline-flex h-[30px] items-center gap-1 rounded border px-2 ${
                    dark ? 'border-slate-700 text-slate-300 hover:bg-slate-800' : 'border-slate-300 text-slate-600 hover:bg-slate-100'
                  }`}
                  title="查询名称和最新价"
                >
                  {manualLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                  查询
                </button>
                <button
                  type="button"
                  onClick={() => void submitManualAdd()}
                  disabled={manualLoading}
                  className="inline-flex h-[30px] items-center gap-1 rounded border border-amber-400/40 bg-amber-500/15 px-3 font-semibold text-amber-200 hover:bg-amber-500/25 disabled:opacity-60"
                >
                  <Plus className="h-3.5 w-3.5" />
                  保存
                </button>
              </div>
            </div>
            <div className="mt-1.5 flex items-center justify-between gap-2">
              <span className={manualMessage ? 'text-amber-300' : 'fin-text-tertiary'}>
                {manualMessage || '保存后归入「手动」组；手动组不会套用低吸退出纪律，需要你手动平仓或单独规则。'}
              </span>
              <button
                type="button"
                onClick={() => setShowManualAdd(false)}
                className="fin-text-tertiary hover:text-amber-300"
              >
                收起
              </button>
            </div>
          </div>
        )}

        {/* 列表 */}
        <div className="flex-1 overflow-auto fin-scrollbar">
          {!hasAnyPosition ? (
            <div className="h-full flex items-center justify-center text-sm fin-text-tertiary">
              还没有模拟持仓 —— 在低吸/波段选股里点「添加 → 加模拟持仓」
            </div>
          ) : !hasVisiblePosition ? (
            <div className="h-full flex items-center justify-center text-sm fin-text-tertiary">
              当前来源没有模拟持仓
            </div>
          ) : (
            <table className="w-full">
              <colgroup>
                <col className="w-[120px]" />
                <col className="w-[112px]" />
                <col className="w-[120px]" />
                <col className="w-[90px]" />
                <col className="w-[160px]" />
                <col className="w-[160px]" />
                <col className="w-[140px]" />
                <col className="w-[120px]" />
                <col className="w-[130px]" />
                <col className="w-[160px]" />
              </colgroup>
              <thead className={`sticky top-0 ${dark ? 'bg-slate-900' : 'bg-white'}`}>
                <tr className="border-b fin-divider">
                  <th className={th}>股票</th>
                  <th className={`${th} text-center`}>来源</th>
                  <th className={`${th} text-center`}>加入时间</th>
                  <th className={`${th} text-center`}>持有天数</th>
                  <th className={`${th} text-right`}>成本价</th>
                  <th className={`${th} text-right`}>数量</th>
                  <th className={`${th} text-right`}>现价/平仓价</th>
                  <th className={`${th} text-right`}>盈亏</th>
                  <th className={`${th} text-right`}>盈亏%</th>
                  <th className={`${th} text-center`}>状态/操作</th>
                </tr>
              </thead>
              <tbody>
                {filteredList.map(p => {
                  const isOpen = p.status === 'open';
                  const win = (p.profitPct || 0) >= 0;
                  return (
                    <tr key={p.id} className={`border-b fin-divider-soft ${dark ? 'hover:bg-slate-800/30' : 'hover:bg-slate-50'}`}>
                      <td className={td}>
                        <button
                          type="button"
                          onClick={() => void onOpenStock?.(toStock(p))}
                          className="block max-w-[118px] truncate text-left font-medium fin-text-primary hover:text-amber-300 hover:underline underline-offset-2"
                          title="打开全屏四图K线"
                        >
                          {p.name || p.symbol}
                        </button>
                        <div className="text-[10px] font-mono fin-text-tertiary">{p.symbol}</div>
                      </td>
                      <td className={`${td} text-center`}>
                        <span className={`text-[10px] px-1.5 py-0.5 rounded ${dark ? 'bg-slate-700 text-slate-300' : 'bg-slate-200 text-slate-600'}`}>{SOURCE_LABEL[p.source] || p.source}</span>
                      </td>
                      <td className={`${td} text-center font-mono text-[11px] fin-text-tertiary`}>
                        {formatOpenDate(p.openDate)}
                      </td>
                      <td className={`${td} text-center font-mono text-[11px] fin-text-secondary`}>
                        {getHoldDays(p.openDate, p.status === 'closed' ? p.closeDate : '')}
                      </td>
                      <td className={`${td} text-right`}>
                        {isOpen ? (
                          <input type="number" step={0.01} defaultValue={p.costPrice}
                            onBlur={e => { const v = Number(e.target.value); if (v > 0 && v !== p.costPrice) saveCost(p, v, p.shares); }}
                            className={`w-20 px-1 py-0.5 rounded text-right font-mono ${dark ? 'bg-slate-900 text-slate-100 border border-slate-700' : 'bg-white text-slate-800 border border-slate-300'}`} />
                        ) : <span className="font-mono">{p.costPrice.toFixed(2)}</span>}
                      </td>
                      <td className={`${td} text-right`}>
                        {isOpen ? (
                          <input type="number" step={100} defaultValue={p.shares}
                            onBlur={e => { const v = Number(e.target.value); if (v > 0 && v !== p.shares) saveCost(p, p.costPrice, v); }}
                            className={`w-20 px-1 py-0.5 rounded text-right font-mono ${dark ? 'bg-slate-900 text-slate-100 border border-slate-700' : 'bg-white text-slate-800 border border-slate-300'}`} />
                        ) : <span className="font-mono">{p.shares}</span>}
                      </td>
                      <td className={`${td} text-right font-mono ${cc.getColorClass(win)}`}>
                        {(p.currentPrice || 0).toFixed(2)}
                        {isOpen && p.stopPrice ? (
                          <div className="text-[9px] fin-text-tertiary font-normal">
                            <span className="text-green-400/80">止{p.stopPrice.toFixed(2)}</span> <span className="text-red-400/70">盈{(p.tpPrice || 0).toFixed(2)}</span>
                          </div>
                        ) : null}
                      </td>
                      <td className={`${td} text-right font-mono ${cc.getColorClass(win)}`}>{(p.profitAmount || 0) >= 0 ? '+' : ''}{Math.round(p.profitAmount || 0)}</td>
                      <td className={`${td} text-right font-mono font-bold ${cc.getColorClass(win)}`}>{(p.profitPct || 0) >= 0 ? '+' : ''}{(p.profitPct || 0).toFixed(2)}%</td>
                      <td className={`${td} text-center`}>
                        {!isOpen ? (
                          <span className="inline-flex items-center justify-center gap-2">
                            <span className="text-[10px] fin-text-tertiary">
                              已平 {p.closeDate}
                              {p.exitReason && <span className="ml-1 px-1 py-0.5 rounded bg-amber-500/15 text-amber-400">{exitReasonText(p.exitReason)}</span>}
                            </span>
                            <button
                              onClick={() => doReopen(p.id)}
                              className="inline-flex items-center gap-0.5 text-[11px] px-1.5 py-0.5 rounded text-amber-400 hover:bg-amber-500/15"
                              title="撤回平仓，恢复为未平仓并从计分卡移除"
                            >
                              <Undo2 className="h-3 w-3" />撤回
                            </button>
                          </span>
                        ) : closingId === p.id ? (
                          <span className="inline-flex items-center gap-1">
                            <input autoFocus type="number" step={0.01} placeholder={String((p.currentPrice || 0).toFixed(2))}
                              value={closePriceInput} onChange={e => setClosePriceInput(e.target.value)}
                              onKeyDown={e => { if (e.key === 'Enter') doClose(p); if (e.key === 'Escape') setClosingId(null); }}
                              className={`w-16 px-1 py-0.5 rounded text-right font-mono text-[11px] ${dark ? 'bg-slate-900 text-slate-100 border border-slate-700' : 'bg-white border border-slate-300'}`} />
                            <button onClick={() => doClose(p)} className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/20 text-amber-400">确认</button>
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1">
                            <button onClick={() => { setClosingId(p.id); setClosePriceInput(''); }} className="inline-flex items-center gap-0.5 text-[11px] px-1.5 py-0.5 rounded text-amber-400 hover:bg-amber-500/15" title="平仓">
                              <LogOut className="h-3 w-3" />平仓
                            </button>
                            <button onClick={() => doDelete(p.id)} className="p-0.5 rounded text-slate-400 hover:text-red-400 hover:bg-red-500/15" title="删除"><Trash2 className="h-3.5 w-3.5" /></button>
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
      <StrategyAccountDialog isOpen={showAccount} onClose={() => setShowAccount(false)} />
      <TailForwardDialog isOpen={showTailFwd} onClose={() => setShowTailFwd(false)} />
    </div>
  );
};

export default PaperPortfolioDialog;
