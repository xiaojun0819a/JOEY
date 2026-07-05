import React, { useEffect, useRef, useState } from 'react';
import { Activity, BarChart3, Check, Loader2, RefreshCw, X, AlertTriangle, CheckCircle2, Flame, Fish, TrendingUp, ShieldAlert } from 'lucide-react';
import type { Stock } from '../types';
import { AddToGroupButton } from './AddToGroupButton';
import { BatchAddToGroupButton } from './BatchAddToGroupButton';
import { StrategyReviewDialog } from './StrategyReviewDialog';
import { runWaveScannerWithGate, type WaveCandidate, type WaveScanResult } from '../services/waveService';

interface WaveScannerDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onAddToWatchlist?: (stock: Stock) => Promise<boolean> | void;
  onOpenStock?: (stock: Stock) => void | Promise<void>;
  watchlistSymbols?: string[];
}

const emptyStock = (code: string, name: string, price: number): Stock => ({
  symbol: code,
  name,
  price,
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

const signalPill = (label: string, active: boolean, tone: 'cyan' | 'amber' | 'red' | 'violet' | 'emerald' = 'amber') => {
  const cls = active
    ? {
        cyan: 'border-cyan-400/40 bg-cyan-400/10 text-cyan-200',
        amber: 'border-amber-400/40 bg-amber-400/10 text-amber-200',
        red: 'border-red-400/40 bg-red-400/10 text-red-200',
        violet: 'border-fuchsia-400/40 bg-fuchsia-400/10 text-fuchsia-200',
        emerald: 'border-emerald-400/40 bg-emerald-400/10 text-emerald-200',
      }[tone]
    : 'border-slate-700/70 bg-slate-900/30 text-slate-500';
  return <span className={`px-1.5 py-0.5 rounded border text-[10px] leading-none ${cls}`}>{label}</span>;
};

const lightDot = (label: string, active: boolean) => (
  <span className={`inline-flex items-center gap-1 text-[10px] ${active ? 'text-red-300' : 'text-emerald-300/70'}`}>
    <span className={`h-1.5 w-1.5 rounded-full ${active ? 'bg-red-400' : 'bg-emerald-500/70'}`} />
    {label}
  </span>
);

const levelClass = (level?: string) => {
  if (level === '超强') return 'text-fuchsia-300 bg-fuchsia-400/10 border-fuchsia-400/30';
  if (level === '主升') return 'text-red-300 bg-red-400/10 border-red-400/30';
  if (level === '开仓吃鱼') return 'text-orange-200 bg-orange-400/10 border-orange-400/30';
  if (level === '转强' || level === '起爆') return 'text-amber-200 bg-amber-400/10 border-amber-400/30';
  if (level === '吃鱼身') return 'text-cyan-200 bg-cyan-400/10 border-cyan-400/30';
  return 'text-slate-300 bg-slate-700/20 border-slate-600/40';
};

const WaveCandidateRow: React.FC<{
  item: WaveCandidate;
  inWatch: boolean;
  onAddToWatchlist?: (stock: Stock) => Promise<boolean> | void;
  onOpenStock?: (stock: Stock) => void | Promise<void>;
  batchSelectMode?: boolean;
  batchSelected?: boolean;
  onToggleBatch?: () => void;
}> = ({ item, inWatch, onAddToWatchlist, onOpenStock, batchSelectMode, batchSelected, onToggleBatch }) => {
  const stock = emptyStock(item.code, item.name, item.price);
  const reasons = item.reasons ?? [];
  const risks = item.risks ?? [];
  return (
    <div className="rounded-xl border fin-divider bg-slate-950/20 px-3 py-2.5 hover:bg-white/[0.04]">
      <div className={`grid ${batchSelectMode ? 'grid-cols-[112px_1fr_92px_84px_26px]' : 'grid-cols-[112px_1fr_92px_84px]'} gap-3 items-start`}>
        <div className="min-w-0">
          <button
            type="button"
            onClick={() => void onOpenStock?.(stock)}
            className="block max-w-full truncate text-left text-sm font-semibold text-slate-100 hover:text-amber-300 hover:underline underline-offset-2"
            title="打开全屏四图K线"
          >
            {item.name}
          </button>
          <div className="mt-0.5 font-mono text-[11px] text-slate-500">{item.code}</div>
        </div>

        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-1.5">
            {signalPill('吃鱼身', !!item.eatFish, 'cyan')}
            {signalPill('开仓吃鱼', !!item.mainOpenFish, 'amber')}
            {signalPill('异动', !!item.relaxedIgnite, 'amber')}
            {signalPill('起爆', !!item.strictIgnite, 'red')}
            {signalPill(item.strongCount >= 3 ? '超强' : item.strongCount === 2 ? '主升' : '转强', !!item.strongSignal, item.strongCount >= 3 ? 'violet' : 'red')}
            {signalPill('主力拉升', !!item.mainControlStart, 'emerald')}
            {signalPill('五灯共振', !!item.gz, 'red')}
            {signalPill('及时止盈', !!item.timelyTakeProfit, 'emerald')}
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            {lightDot('买', !!item.buyState)}
            {lightDot('趋势', !!item.trendBull)}
            {lightDot('量能', !!item.energyBull)}
            {lightDot('中期', !!item.midBull)}
            {lightDot('短期', !!item.shortBull)}
          </div>
          {reasons.length > 0 && (
            <div className="text-[11px] leading-relaxed text-slate-400 line-clamp-2">
              {reasons.slice(0, 4).join('；')}
            </div>
          )}
          {risks.length > 0 && (
            <div className="flex items-start gap-1 text-[11px] text-rose-300">
              <ShieldAlert className="h-3.5 w-3.5 shrink-0 mt-0.5" />
              <span className="line-clamp-1">{risks.join('；')}</span>
            </div>
          )}
        </div>

        <div className="text-right">
          <div className="text-sm font-semibold text-slate-100">{Number(item.price || 0).toFixed(2)}</div>
          <div className="mt-0.5 text-[11px] text-amber-300">控盘 {Number(item.kongpan || 0).toFixed(0)}</div>
          <div className="mt-0.5 text-[10px] text-slate-500">{item.phase || '观察'}</div>
        </div>

        <div className="flex flex-col items-end gap-1.5">
          <span className={`px-2 py-1 rounded-lg border text-[11px] font-semibold ${levelClass(item.level)}`}>
            {item.level || '观察'}
          </span>
          <span className="text-[15px] font-bold text-amber-200">{Number(item.score || 0).toFixed(1)}</span>
          {onAddToWatchlist && !batchSelectMode && (
            <AddToGroupButton stock={stock} source="wave-v1" inWatch={inWatch} onAddToWatchlist={onAddToWatchlist} />
          )}
        </div>

        {batchSelectMode && (
          <div className="flex justify-end pt-1">
            <button
              type="button"
              onClick={onToggleBatch}
              className={`inline-flex h-4 w-4 items-center justify-center rounded-[3px] border transition-colors ${
                batchSelected
                  ? 'border-amber-300 bg-amber-400 text-slate-950'
                  : 'border-slate-500/80 bg-slate-950/20 hover:border-amber-300'
              }`}
              title={batchSelected ? '取消勾选' : '勾选批量添加'}
            >
              {batchSelected && <Check className="h-3 w-3" />}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export const WaveScannerDialog: React.FC<WaveScannerDialogProps> = ({
  isOpen,
  onClose,
  onAddToWatchlist,
  onOpenStock,
  watchlistSymbols = [],
}) => {
  const watchSet = new Set(watchlistSymbols.map(s => String(s).toLowerCase()));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<(WaveScanResult & { scannedAt?: string }) | null>(null);
  const [showNextDayReview, setShowNextDayReview] = useState(false);
  const [batchSelectMode, setBatchSelectMode] = useState(false);
  const [batchSelectedCodes, setBatchSelectedCodes] = useState<string[]>([]);
  const scanRequestIdRef = useRef(0);

  const handleScan = async (useGate = true) => {
    const requestId = ++scanRequestIdRef.current;
    setLoading(true);
    setError(null);
    setResult(null);
    setBatchSelectMode(false);
    setBatchSelectedCodes([]);
    try {
      const res = await runWaveScannerWithGate(useGate);
      if (requestId !== scanRequestIdRef.current) return;
      if (res) setResult({ ...res, scannedAt: new Date().toLocaleString('zh-CN', { hour12: false }) });
    } catch (e) {
      if (requestId !== scanRequestIdRef.current) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (requestId === scanRequestIdRef.current) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    if (!isOpen) return;
    setBatchSelectMode(false);
    setBatchSelectedCodes([]);
  }, [isOpen]);

  if (!isOpen) return null;

  const items = result?.items ?? [];
  const batchSelectedSet = new Set(batchSelectedCodes);
  const batchStocks = items
    .filter(item => batchSelectedSet.has(item.code))
    .map(item => emptyStock(item.code, item.name, item.price));
  const toggleBatchStock = (code: string) => {
    setBatchSelectedCodes(prev => (
      prev.includes(code) ? prev.filter(item => item !== code) : [...prev, code]
    ));
  };
  const exitBatchMode = () => {
    setBatchSelectMode(false);
    setBatchSelectedCodes([]);
  };

  return (
    <>
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="w-[880px] max-w-[94vw] max-h-[84vh] flex flex-col rounded-2xl fin-panel border fin-divider shadow-2xl">
        {/* header */}
        <div className="flex items-center justify-between px-5 py-4 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Activity className="h-5 w-5 text-amber-400" />
            <div>
              <div className="text-sm font-semibold text-slate-100">波段策略 1.0</div>
              <div className="text-[11px] text-slate-400">实时全A快照 + 最近日K + 本地历史库 · 约240日预热 · 不复用低吸规则</div>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-white/10 text-slate-400 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* toolbar */}
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="min-w-0 flex-1 pr-3 text-[11px] text-slate-400">
            {result ? (
              <span className="flex flex-wrap items-center gap-x-1 gap-y-0.5">
                数据日 <span className="text-slate-200">{result.asOf || '--'}</span>
                {result.snapshotAsOf ? <> · 快照 <span className="text-slate-200">{result.snapshotAsOf}</span></> : null}
                {result.scannedAt ? <> · 扫描 <span className="text-slate-200">{result.scannedAt}</span></> : null}
                {' · '}
                {result.gateBypassed ? (
                  <span className="text-amber-300">临时开闸</span>
                ) : result.gatePassed ? (
                  <span className="text-emerald-400">闸门通过</span>
                ) : (
                  <span className="text-rose-400">闸门未过(空仓)</span>
                )}{' '}
                · 命中 <span className="text-slate-200">{result.count}</span> 只
                {typeof result.scannedCount === 'number' ? <> · 预热通过 <span className="text-slate-200">{result.scannedCount}</span></> : null}
                {typeof result.patchedCount === 'number' ? <> · 快照补丁 <span className="text-slate-200">{result.patchedCount}</span></> : null}
                {typeof result.recentKCount === 'number' ? <> · 日K校验 <span className="text-slate-200">{result.recentKCount}</span></> : null}
              </span>
            ) : (
              <span>数据口：实时全A快照 + 最近日K + 本地历史库；需要约240日数据预热，盘后约17:30可自动推送前5</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {onAddToWatchlist && (
              batchSelectMode ? (
                <>
                  <BatchAddToGroupButton
                    stocks={batchStocks}
                    source="wave-v1"
                    onAddToWatchlist={onAddToWatchlist}
                    onDone={exitBatchMode}
                  />
                  <button
                    type="button"
                    onClick={exitBatchMode}
                    className="flex items-center gap-1.5 rounded-lg border border-slate-600/70 px-3 py-1.5 text-xs font-medium text-slate-400 hover:bg-white/5"
                  >
                    取消
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  onClick={() => {
                    setBatchSelectedCodes([]);
                    setBatchSelectMode(true);
                  }}
                  disabled={items.length === 0}
                  className="flex items-center gap-1.5 rounded-lg border border-amber-400/45 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-200 hover:bg-amber-500/15 disabled:opacity-50"
                >
                  批量操作
                </button>
              )
            )}
            <button
              type="button"
              onClick={() => setShowNextDayReview(true)}
              className="flex items-center gap-1.5 rounded-lg border border-amber-400/45 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-200 hover:bg-amber-500/15"
              title="对波段策略上一交易日选出的股票做次日收盘复盘"
            >
              <BarChart3 className="h-3.5 w-3.5" />
              <span>次日复盘</span>
            </button>
            <button
              onClick={() => handleScan(true)}
              disabled={loading}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-amber-500/90 hover:bg-amber-500 text-white text-xs font-medium disabled:opacity-50"
            >
              {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              <span>{loading ? '扫描中(约30-60秒)' : '重新扫描'}</span>
            </button>
          </div>
        </div>

        {/* body */}
        <div className="flex-1 overflow-auto px-5 py-3">
          {error && (
            <div className="flex items-center gap-2 px-3 py-2 mb-3 rounded-lg bg-rose-500/10 text-rose-300 text-xs">
              <AlertTriangle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {result && !result.gatePassed && !result.gateBypassed && (
            <div className="flex items-center justify-between gap-3 px-3 py-3 rounded-lg bg-rose-500/10 text-rose-300 text-xs">
              <div className="flex min-w-0 items-center gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                <span className="truncate">{result.message || '大盘闸门未通过，波段策略今日不出票'}</span>
              </div>
              <button
                type="button"
                onClick={() => handleScan(false)}
                disabled={loading}
                className="shrink-0 rounded-lg border border-amber-400/40 bg-amber-500/15 px-3 py-1.5 text-[11px] font-semibold text-amber-200 hover:bg-amber-500/25 disabled:opacity-50"
                title="只临时绕过本次波段大盘闸门，不改全局规则"
              >
                临时打开闸门，进行筛选
              </button>
            </div>
          )}

          {result?.gateBypassed && (
            <div className="flex items-center gap-2 px-3 py-2 mb-3 rounded-lg bg-amber-500/10 text-amber-200 text-xs">
              <AlertTriangle className="h-4 w-4 shrink-0" />
              <span>{result.message || '已临时打开闸门筛选，仅作观察，不代表大盘环境通过'}</span>
            </div>
          )}

          {result && (result.gatePassed || result.gateBypassed) && items.length === 0 && (
            <div className="flex items-center gap-2 px-3 py-3 rounded-lg bg-slate-500/10 text-slate-300 text-xs">
              <CheckCircle2 className="h-4 w-4 shrink-0" />
              <span>{result.message || '今日无波段开仓信号'}</span>
            </div>
          )}

          {items.length > 0 && (
            <div className="space-y-2">
              <div className="grid grid-cols-3 gap-2 text-[11px]">
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="flex items-center gap-1.5 text-cyan-200 font-semibold"><Fish className="h-3.5 w-3.5" />第一维</div>
                  <div className="mt-1 text-slate-500">吃鱼身 / 开仓吃鱼 / 异动现主力进</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="flex items-center gap-1.5 text-amber-200 font-semibold"><Flame className="h-3.5 w-3.5" />第二维</div>
                  <div className="mt-1 text-slate-500">异动起爆 / 转强 / 主升 / 超强 / 控盘斜率</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="flex items-center gap-1.5 text-red-200 font-semibold"><TrendingUp className="h-3.5 w-3.5" />第三维</div>
                  <div className="mt-1 text-slate-500">买卖状态 + 趋势 + 量能 + 中期 + 短期</div>
                </div>
              </div>
              {items.map((it) => (
                <WaveCandidateRow
                  key={it.code}
                  item={it}
                  inWatch={watchSet.has(it.code.toLowerCase())}
                  onAddToWatchlist={onAddToWatchlist}
                  onOpenStock={onOpenStock}
                  batchSelectMode={batchSelectMode}
                  batchSelected={batchSelectedSet.has(it.code)}
                  onToggleBatch={() => toggleBatchStock(it.code)}
                />
              ))}
            </div>
          )}
        </div>

        {/* footer note */}
        <div className="px-5 py-3 border-t fin-divider text-[11px] text-slate-500 leading-relaxed">
          说明：扫描先用实时全A快照补当天收盘价/成交额，再用本地历史库做约240日指标预热；命中候选会尽量拉最近日K复核。这里的“主力/控盘/资金点火”来自通达信量价公式代理，不是真实账户资金流。波段与低吸为两套独立系统，互不干扰。
        </div>
      </div>
    </div>
    <StrategyReviewDialog
      isOpen={showNextDayReview}
      onClose={() => setShowNextDayReview(false)}
      strategyId="wave-v1"
      strategyName="波段策略 1.0"
      signalDate={result?.asOf}
    />
    </>
  );
};

export default WaveScannerDialog;
