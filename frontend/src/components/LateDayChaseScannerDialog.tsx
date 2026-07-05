import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AlertTriangle, BarChart3, Check, CheckCircle2, Clock3, Loader2, RefreshCw, TrendingUp, X } from 'lucide-react';
import type { Stock } from '../types';
import { AddToGroupButton } from './AddToGroupButton';
import { BatchAddToGroupButton } from './BatchAddToGroupButton';
import { StrategyReviewDialog } from './StrategyReviewDialog';
import { runLateDayChaseScanner } from '../services/scannerService';

export interface LateDayChaseScannerRequest {
  limit: number;
  rankLimit: number;
  includeBeijing: boolean;
  minChangePct?: number;
  maxChangePct?: number;
  minVolumeRatio?: number;
  minTurnoverRate?: number;
  maxTurnoverRate?: number;
  minFloatCap?: number;
  maxFloatCap?: number;
  requireBuySignal?: boolean;
}

export interface LateDayChaseScannerItem {
  symbol: string;
  name: string;
  rank: number;
  price: number;
  changePercent: number;
  volumeRatio: number;
  turnoverRate: number;
  amount: number;
  totalMarketCap: number;
  floatMarketCap: number;
  industry: string;
  score: number;
  volumeStepPassed: boolean;
  maBullishPassed: boolean;
  intradayStrengthPassed: boolean;
  buySignalReady: boolean;
  intradayAboveAvgRatio: number;
  stockIntradayReturn: number;
  indexIntradayReturn: number;
  ma5: number;
  ma10: number;
  ma20: number;
  lastHighTime: string;
  triggers: string[];
  reasons: string[];
  riskFlags: string[];
  buyPointHint: string;
  stopLossHint: string;
  updatedAt: string;
}

export interface LateDayChaseScannerResult {
  asOf: string;
  ruleVersion: string;
  universeCount: number;
  rankLimit: number;
  rankedCount: number;
  candidateCount: number;
  selectedCount: number;
  items: LateDayChaseScannerItem[];
  warning?: string;
}

interface LateDayChaseScannerDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onAddToWatchlist: (stock: Stock) => Promise<boolean> | void;
  onOpenStock?: (stock: Stock) => void | Promise<void>;
  watchlistSymbols?: string[];
}

const formatAmount = (value: number): string => {
  if (!Number.isFinite(value)) return '--';
  const abs = Math.abs(value);
  const sign = value < 0 ? '-' : '';
  if (abs >= 1e8) return `${sign}${(abs / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${sign}${(abs / 1e4).toFixed(2)}万`;
  return `${sign}${abs.toFixed(0)}`;
};

const toWatchStock = (item: LateDayChaseScannerItem): Stock => {
  const preClose = item.price / (1 + item.changePercent / 100);
  return {
    symbol: item.symbol,
    name: item.name,
    price: item.price,
    change: item.price - preClose,
    changePercent: item.changePercent,
    volume: 0,
    amount: item.amount,
    marketCap: item.floatMarketCap > 0 ? `${(item.floatMarketCap / 1e8).toFixed(1)}亿` : '',
    sector: item.industry,
    open: 0,
    high: 0,
    low: 0,
    preClose: Number.isFinite(preClose) ? preClose : 0,
  };
};

export const LateDayChaseScannerDialog: React.FC<LateDayChaseScannerDialogProps> = ({
  isOpen,
  onClose,
  onAddToWatchlist,
  onOpenStock,
  watchlistSymbols = [],
}) => {
  const [limit, setLimit] = useState(20);
  const [rankLimit, setRankLimit] = useState(60);
  const [includeBeijing, setIncludeBeijing] = useState(false);
  const [requireBuySignal, setRequireBuySignal] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<LateDayChaseScannerResult | null>(null);
  const [selected, setSelected] = useState<LateDayChaseScannerItem | null>(null);
  const [showNextDayReview, setShowNextDayReview] = useState(false);
  const [batchSelectMode, setBatchSelectMode] = useState(false);
  const [batchSelectedSymbols, setBatchSelectedSymbols] = useState<string[]>([]);
  const scanRequestIdRef = useRef(0);

  const watchlistSet = useMemo(
    () => new Set((watchlistSymbols || []).map(symbol => String(symbol).toLowerCase())),
    [watchlistSymbols],
  );

  const runScan = async () => {
    const requestId = ++scanRequestIdRef.current;
    setLoading(true);
    setError('');
    setResult(null);
    setSelected(null);
    setBatchSelectMode(false);
    setBatchSelectedSymbols([]);
    try {
      const res = await runLateDayChaseScanner({
        limit,
        rankLimit,
        includeBeijing,
        minChangePct: 3,
        maxChangePct: 5,
        minVolumeRatio: 1,
        minTurnoverRate: 5,
        maxTurnoverRate: 10,
        minFloatCap: 50e8,
        maxFloatCap: 200e8,
        requireBuySignal,
      });
      if (requestId !== scanRequestIdRef.current) return;
      if (!res) {
        setError('当前为浏览器预览模式，请从 Wails 开发入口打开后再扫描。');
        setResult(null);
        setSelected(null);
        return;
      }
      setResult(res);
      setSelected(res.items[0] || null);
    } catch (err) {
      if (requestId !== scanRequestIdRef.current) return;
      setError(err instanceof Error ? err.message : '扫描失败，请稍后重试');
      setResult(null);
      setSelected(null);
    } finally {
      if (requestId === scanRequestIdRef.current) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    if (!isOpen) return;
    setBatchSelectMode(false);
    setBatchSelectedSymbols([]);
  }, [isOpen]);

  if (!isOpen) return null;

  const batchSelectedSet = new Set(batchSelectedSymbols);
  const batchStocks = (result?.items || [])
    .filter(item => batchSelectedSet.has(item.symbol))
    .map(toWatchStock);
  const toggleBatchStock = (symbol: string) => {
    setBatchSelectedSymbols(prev => (
      prev.includes(symbol) ? prev.filter(item => item !== symbol) : [...prev, symbol]
    ));
  };
  const exitBatchMode = () => {
    setBatchSelectMode(false);
    setBatchSelectedSymbols([]);
  };

  return (
    <>
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1120px] h-[720px] max-w-[94vw] max-h-[88vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-rose-300" />
            <div className="text-left">
              <div className="text-sm font-semibold fin-text-primary">尾盘强势股</div>
              <div className="text-[11px] fin-text-tertiary">涨幅榜60 · 3%-5% · 量比/换手/流通市值 · 日K与分时确认</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={runScan}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border fin-divider hover:border-rose-400/50 transition-colors disabled:opacity-60"
              title="按尾盘强势股规则重新扫描"
            >
              {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              <span>{loading ? '扫描中...' : '重新扫描'}</span>
            </button>
            <button
              type="button"
              onClick={() => setShowNextDayReview(true)}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-amber-400/45 text-amber-200 bg-amber-500/8 hover:bg-amber-500/15 transition-colors"
              title="对尾盘强势策略上一交易日选出的股票做次日收盘复盘"
            >
              <BarChart3 className="h-3.5 w-3.5" />
              <span>次日复盘</span>
            </button>
            <button onClick={onClose} className="p-2 rounded-lg fin-hover transition-colors" title="关闭">
              <X className="h-4 w-4 fin-text-secondary" />
            </button>
          </div>
        </div>

        <div className="px-4 py-2 border-b fin-divider-soft flex items-center gap-4 text-xs">
          <label className="flex items-center gap-2">
            <span className="fin-text-secondary">涨幅榜</span>
            <input
              type="number"
              min={20}
              max={300}
              value={rankLimit}
              onChange={(e) => setRankLimit(Math.min(300, Math.max(20, Number(e.target.value) || 60)))}
              className="w-20 px-2 py-1 rounded fin-input text-xs"
            />
          </label>
          <label className="flex items-center gap-2">
            <span className="fin-text-secondary">返回数量</span>
            <input
              type="number"
              min={5}
              max={100}
              value={limit}
              onChange={(e) => setLimit(Math.min(100, Math.max(5, Number(e.target.value) || 20)))}
              className="w-20 px-2 py-1 rounded fin-input text-xs"
            />
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={requireBuySignal}
              onChange={(e) => setRequireBuySignal(e.target.checked)}
            />
            <span className="fin-text-secondary">只看买点触发</span>
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={includeBeijing}
              onChange={(e) => setIncludeBeijing(e.target.checked)}
            />
            <span className="fin-text-secondary">包含北交所</span>
          </label>
          {result && (
            <div className="ml-auto flex items-center gap-3 fin-text-tertiary">
              <span>全市场 {result.universeCount}</span>
              <span>涨幅榜 {result.rankedCount}/{result.rankLimit}</span>
              <span>技术验证 {result.candidateCount}</span>
              <span>入选 {result.selectedCount}</span>
              <span>{result.asOf}</span>
            </div>
          )}
        </div>

        {result?.warning && (
          <div className="px-4 py-2 border-b fin-divider-soft text-xs">
            <div className="inline-flex items-center gap-1.5 px-2 py-1 rounded border border-amber-500/35 text-amber-300 bg-amber-500/8">
              <AlertTriangle className="h-3.5 w-3.5" />
              <span>{result.warning}</span>
            </div>
          </div>
        )}

        <div className="flex-1 flex overflow-hidden">
          <div className="w-[60%] border-r fin-divider-soft overflow-auto fin-scrollbar">
            {loading && (
              <div className="h-full flex items-center justify-center text-sm fin-text-secondary">
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                正在验证涨幅榜候选...
              </div>
            )}
            {!loading && error && (
              <div className="p-4 text-sm text-red-300">{error}</div>
            )}
            {!loading && !error && result && result.items.length === 0 && (
              <div className="p-4 text-sm fin-text-secondary">未找到符合规则的标的。</div>
            )}
            {!loading && !error && result && result.items.length > 0 && (
              <>
              <div className="flex items-center justify-end gap-2 border-b fin-divider-soft px-2 py-1.5 text-xs fin-panel-strong">
                {batchSelectMode ? (
                  <>
                    <span className="mr-auto fin-text-tertiary">批量模式 · 已选 {batchStocks.length} 只</span>
                    <BatchAddToGroupButton
                      stocks={batchStocks}
                      source="latechase-v3"
                      onAddToWatchlist={onAddToWatchlist}
                      onDone={exitBatchMode}
                    />
                    <button
                      type="button"
                      onClick={exitBatchMode}
                      className="rounded border border-slate-600/70 px-2.5 py-1.5 text-[11px] font-semibold fin-text-tertiary hover:bg-slate-500/10"
                    >
                      取消
                    </button>
                  </>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setBatchSelectedSymbols([]);
                      setBatchSelectMode(true);
                    }}
                    className="rounded border border-amber-400/45 bg-amber-500/10 px-2.5 py-1.5 text-[11px] font-semibold text-amber-200 hover:bg-amber-500/18"
                  >
                    批量操作
                  </button>
                )}
              </div>
              <table className="w-full text-xs">
                <thead className="sticky top-0 fin-panel-strong border-b fin-divider-soft">
                  <tr>
                    <th className="text-left px-2 py-2">股票</th>
                    <th className="text-right px-2 py-2">评分</th>
                    <th className="text-right px-2 py-2">涨幅</th>
                    <th className="text-right px-2 py-2">量比</th>
                    <th className="text-right px-2 py-2">换手</th>
                    <th className="text-right px-2 py-2">流通</th>
                    <th className="text-center px-2 py-2">买点</th>
                    <th className="text-center px-2 py-2">操作</th>
                    {batchSelectMode && <th className="w-8 px-1 py-2" aria-label="批量勾选" />}
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((item) => {
                    const selectedRow = selected?.symbol === item.symbol;
                    const inWatch = watchlistSet.has(item.symbol.toLowerCase());
                    return (
                      <tr
                        key={item.symbol}
                        className={`cursor-pointer border-b fin-divider-soft ${selectedRow ? 'bg-rose-500/10' : 'hover:bg-slate-500/5'}`}
                        onClick={() => setSelected(item)}
                      >
                        <td className="px-2 py-2 text-left">
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelected(item);
                              void onOpenStock?.(toWatchStock(item));
                            }}
                            className="font-medium fin-text-primary hover:text-rose-300 hover:underline underline-offset-2 text-left"
                            title="打开全屏四图K线"
                          >
                            {item.name}
                          </button>
                          <div className="fin-text-tertiary">#{item.rank} · {item.symbol} · {item.industry || '未知'}</div>
                        </td>
                        <td className="px-2 py-2 text-right text-rose-300 font-semibold">{item.score.toFixed(1)}</td>
                        <td className="px-2 py-2 text-right text-rose-300">+{item.changePercent.toFixed(2)}%</td>
                        <td className="px-2 py-2 text-right">{item.volumeRatio.toFixed(2)}</td>
                        <td className="px-2 py-2 text-right">{item.turnoverRate.toFixed(2)}%</td>
                        <td className="px-2 py-2 text-right">{(item.floatMarketCap / 1e8).toFixed(1)}亿</td>
                        <td className="px-2 py-2 text-center">
                          {item.buySignalReady ? (
                            <CheckCircle2 className="h-3.5 w-3.5 inline text-emerald-300" />
                          ) : (
                            <Clock3 className="h-3.5 w-3.5 inline text-amber-300" />
                          )}
                        </td>
                        <td className="px-2 py-2 text-center" onClick={(e) => e.stopPropagation()}>
                          {batchSelectMode ? (
                            <span className="text-[11px] fin-text-tertiary">勾选</span>
                          ) : (
                            <div className="inline-flex justify-center">
                              <AddToGroupButton
                                stock={toWatchStock(item)}
                                source="latechase-v3"
                                inWatch={inWatch}
                                onAddToWatchlist={onAddToWatchlist}
                              />
                            </div>
                          )}
                        </td>
                        {batchSelectMode && (
                          <td className="px-1 py-2 text-center" onClick={(e) => e.stopPropagation()}>
                            <button
                              type="button"
                              onClick={() => toggleBatchStock(item.symbol)}
                              className={`inline-flex h-4 w-4 items-center justify-center rounded-[3px] border transition-colors ${
                                batchSelectedSet.has(item.symbol)
                                  ? 'border-amber-300 bg-amber-400 text-slate-950'
                                  : 'border-slate-500/80 bg-slate-950/20 hover:border-amber-300'
                              }`}
                              title={batchSelectedSet.has(item.symbol) ? '取消勾选' : '勾选批量添加'}
                            >
                              {batchSelectedSet.has(item.symbol) && <Check className="h-3 w-3" />}
                            </button>
                          </td>
                        )}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              </>
            )}
          </div>

          <div className="flex-1 overflow-auto fin-scrollbar p-3 text-left">
            {!selected ? (
              <div className="text-sm fin-text-secondary">点击左侧标的查看结构细节。</div>
            ) : (
              <div className="space-y-3">
                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-semibold fin-text-primary">{selected.name} <span className="font-normal fin-text-tertiary">({selected.symbol})</span></div>
                      <div className="text-xs fin-text-tertiary">{selected.industry} · 涨幅榜 #{selected.rank}</div>
                    </div>
                    <div className="text-right">
                      <div className="text-lg font-mono fin-text-primary">{selected.price.toFixed(2)}</div>
                      <div className="text-xs text-rose-300">+{selected.changePercent.toFixed(2)}%</div>
                    </div>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-2 text-xs">
                  <div className="fin-panel-soft border fin-divider rounded-lg p-2">
                    <div className="fin-text-tertiary">量比 / 换手</div>
                    <div className="mt-1 fin-text-primary">{selected.volumeRatio.toFixed(2)} / {selected.turnoverRate.toFixed(2)}%</div>
                  </div>
                  <div className="fin-panel-soft border fin-divider rounded-lg p-2">
                    <div className="fin-text-tertiary">流通市值 / 成交额</div>
                    <div className="mt-1 fin-text-primary">{(selected.floatMarketCap / 1e8).toFixed(1)}亿 / {formatAmount(selected.amount)}</div>
                  </div>
                  <div className="fin-panel-soft border fin-divider rounded-lg p-2">
                    <div className="fin-text-tertiary">均线</div>
                    <div className="mt-1 fin-text-primary">MA5 {selected.ma5.toFixed(2)} · MA10 {selected.ma10.toFixed(2)} · MA20 {selected.ma20.toFixed(2)}</div>
                  </div>
                  <div className="fin-panel-soft border fin-divider rounded-lg p-2">
                    <div className="fin-text-tertiary">分时强度</div>
                    <div className="mt-1 fin-text-primary">{(selected.intradayAboveAvgRatio * 100).toFixed(0)}% 在线上 · 个股 {selected.stockIntradayReturn.toFixed(2)}% / 上证 {selected.indexIntradayReturn.toFixed(2)}%</div>
                  </div>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">命中条件</div>
                  <div className="flex flex-wrap gap-1.5">
                    {selected.triggers.map((trigger) => (
                      <span key={trigger} className="text-[11px] px-2 py-0.5 rounded border border-rose-400/35 text-rose-200">{trigger}</span>
                    ))}
                  </div>
                  {selected.riskFlags.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {selected.riskFlags.map((risk) => (
                        <span key={risk} className="text-[11px] px-2 py-0.5 rounded border border-amber-500/35 text-amber-300">{risk}</span>
                      ))}
                    </div>
                  )}
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">结构说明</div>
                  <ul className="text-xs fin-text-secondary space-y-1 list-disc pl-4">
                    {selected.reasons.map((reason, idx) => (
                      <li key={`${selected.symbol}-reason-${idx}`}>{reason}</li>
                    ))}
                  </ul>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">执行纪律</div>
                  <div className="text-xs fin-text-secondary space-y-1">
                    <div>买点：{selected.buyPointHint}</div>
                    <div>止损：{selected.stopLossHint}</div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
    <StrategyReviewDialog
      isOpen={showNextDayReview}
      onClose={() => setShowNextDayReview(false)}
      strategyId="latechase-v3"
      strategyName="尾盘强势策略3"
      signalDate={result?.asOf}
    />
    </>
  );
};

export default LateDayChaseScannerDialog;
