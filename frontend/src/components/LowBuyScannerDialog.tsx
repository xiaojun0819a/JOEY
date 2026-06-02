import React, { useEffect, useMemo, useState } from 'react';
import { Database, Loader2, RefreshCw, Search, X, AlertTriangle, CheckCircle2, Plus, SlidersHorizontal } from 'lucide-react';
import type { Stock } from '../types';

export interface LowBuyScannerRequest {
  limit: number;
  includeBeijing: boolean;
  historyFilterEnabled?: boolean;
  historyTurnoverDays?: number;
  historyTurnoverMax?: number;
  historyMainFlowDays?: number;
  historyMainFlowPositiveDays?: number;
  historyMAPeriod?: number;
}

export interface HistoryCollectRequest {
  tradeDate?: string;
  includeBeijing: boolean;
}

export interface HistoryCollectResult {
  tradeDate: string;
  startedAt: string;
  finishedAt: string;
  dbPath: string;
  totalCount: number;
  savedCount: number;
  failedCount: number;
  maUpdated: boolean;
  status: string;
  message: string;
}

export interface HistoryAutoCollectRequest {
  enabled: boolean;
  collectStart?: string;
  collectEnd?: string;
  includeBeijing: boolean;
}

export interface HistoryAutoCollectStatus {
  enabled: boolean;
  collectStart: string;
  collectEnd: string;
  includeBeijing: boolean;
  lastCollectDate: string;
  dbPath: string;
  message: string;
}

export interface LowBuyScannerMarketOverview {
  shPrice: number;
  shMA20: number;
  limitUpCount: number;
  limitDownCount: number;
  totalAmount: number;
}

export interface LowBuyScannerItem {
  symbol: string;
  name: string;
  price: number;
  changePercent: number;
  amount: number;
  turnoverRate: number;
  mainNetInflow: number;
  mainNetInflowRatio: number;
  mainFlowSource: string;
  totalMarketCap: number;
  floatMarketCap: number;
  capBucket: string;
  industry: string;
  score: number;
  triggerCount: number;
  triggers: string[];
  reasons: string[];
  riskFlags: string[];
  buyPointHint: string;
  sellPointHint: string;
  stopLossHint: string;
  updatedAt: string;
}

export interface LowBuyScannerResult {
  asOf: string;
  ruleVersion: string;
  universeCount: number;
  candidateCount: number;
  selectedCount: number;
  marketGatePassed: boolean;
  marketGateReasons: string[];
  marketOverview: LowBuyScannerMarketOverview;
  items: LowBuyScannerItem[];
  warning?: string;
}

interface LowBuyScannerDialogProps {
  isOpen: boolean;
  loading: boolean;
  result: LowBuyScannerResult | null;
  error: string;
  onClose: () => void;
  onScan: (req: LowBuyScannerRequest) => void;
  onCollectHistory: (req: HistoryCollectRequest) => Promise<HistoryCollectResult | null>;
  onGetHistoryAutoCollectStatus: () => Promise<HistoryAutoCollectStatus | null>;
  onUpdateHistoryAutoCollect: (req: HistoryAutoCollectRequest) => Promise<HistoryAutoCollectStatus | null>;
  onAddToWatchlist: (stock: Stock) => Promise<boolean>;
  watchlistSymbols: string[];
}

const formatAmount = (value: number): string => {
  if (!Number.isFinite(value)) return '--';
  if (value === 0) return '0';
  if (value < 0) {
    const abs = Math.abs(value);
    if (abs >= 1e8) return `-${(abs / 1e8).toFixed(2)}亿`;
    if (abs >= 1e4) return `-${(abs / 1e4).toFixed(2)}万`;
    return value.toFixed(0);
  }
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
};

const formatMainFlowSource = (source?: string): string => {
  switch ((source || '').toLowerCase()) {
    case 'eastmoney':
      return '东财';
    case 'tencent-fundflow':
      return '腾讯资金流';
    case 'tencent-pk-proxy':
      return '腾讯盘口代理';
    default:
      return '未知';
  }
};

const isGateReasonPassed = (reason: string): boolean => {
  if (!reason) return false;
  return !reason.includes('未达') && !reason.includes('未站上') && !reason.includes('异常');
};

const toWatchStock = (item: LowBuyScannerItem): Stock => {
  const preClose = item.price / (1 + item.changePercent / 100);
  return {
    symbol: item.symbol,
    name: item.name,
    price: item.price,
    change: item.price - preClose,
    changePercent: item.changePercent,
    volume: 0,
    amount: item.amount,
    marketCap: item.totalMarketCap > 0 ? `${(item.totalMarketCap / 1e8).toFixed(1)}亿` : '',
    sector: item.industry,
    open: 0,
    high: 0,
    low: 0,
    preClose: Number.isFinite(preClose) ? preClose : 0,
  };
};

export const LowBuyScannerDialog: React.FC<LowBuyScannerDialogProps> = ({
  isOpen,
  loading,
  result,
  error,
  onClose,
  onScan,
  onCollectHistory,
  onGetHistoryAutoCollectStatus,
  onUpdateHistoryAutoCollect,
  onAddToWatchlist,
  watchlistSymbols,
}) => {
  const [limit, setLimit] = useState(30);
  const [includeBeijing, setIncludeBeijing] = useState(false);
  const [selected, setSelected] = useState<LowBuyScannerItem | null>(null);
  const [adding, setAdding] = useState<Record<string, boolean>>({});
  const [added, setAdded] = useState<Record<string, boolean>>({});
  const [collectingHistory, setCollectingHistory] = useState(false);
  const [historyCollectResult, setHistoryCollectResult] = useState<HistoryCollectResult | null>(null);
  const [historyCollectError, setHistoryCollectError] = useState('');
  const [autoCollectStatus, setAutoCollectStatus] = useState<HistoryAutoCollectStatus | null>(null);
  const [autoCollectSaving, setAutoCollectSaving] = useState(false);
  const [showHistoryPanel, setShowHistoryPanel] = useState(false);
  const [historyFilterEnabled, setHistoryFilterEnabled] = useState(false);
  const [historyTurnoverDays, setHistoryTurnoverDays] = useState(3);
  const [historyTurnoverMax, setHistoryTurnoverMax] = useState(3);
  const [historyMainFlowDays, setHistoryMainFlowDays] = useState(3);
  const [historyMainFlowPositiveDays, setHistoryMainFlowPositiveDays] = useState(2);
  const [historyMAPeriod, setHistoryMAPeriod] = useState(10);

  const watchlistSet = useMemo(
    () => new Set((watchlistSymbols || []).map(s => String(s).toLowerCase())),
    [watchlistSymbols],
  );

  useEffect(() => {
    if (!isOpen) return;
    if (result && result.items.length > 0) {
      setSelected(result.items[0]);
    } else {
      setSelected(null);
    }
  }, [isOpen, result]);

  useEffect(() => {
    if (!isOpen) return;
    void onGetHistoryAutoCollectStatus()
      .then(status => {
        if (status) setAutoCollectStatus(status);
      })
      .catch(() => undefined);
  }, [isOpen, onGetHistoryAutoCollectStatus]);

  if (!isOpen) return null;

  const triggerTotal = historyFilterEnabled ? 7 : 4;

  const runScan = () => {
    onScan({
      limit,
      includeBeijing,
      historyFilterEnabled,
      historyTurnoverDays,
      historyTurnoverMax,
      historyMainFlowDays,
      historyMainFlowPositiveDays,
      historyMAPeriod,
    });
  };

  const toggleHistoryPanel = () => {
    setShowHistoryPanel(prev => !prev);
  };

  const confirmHistoryFilter = () => {
    setHistoryFilterEnabled(true);
    setShowHistoryPanel(false);
    onScan({
      limit,
      includeBeijing,
      historyFilterEnabled: true,
      historyTurnoverDays,
      historyTurnoverMax,
      historyMainFlowDays,
      historyMainFlowPositiveDays,
      historyMAPeriod,
    });
  };

  const handleCollectHistory = async () => {
    if (collectingHistory) return;
    setCollectingHistory(true);
    setHistoryCollectError('');
    try {
      const collectResult = await onCollectHistory({ includeBeijing });
      if (!collectResult) {
        setHistoryCollectError('当前为浏览器预览模式，请从 Wails 开发入口打开后再采集。');
        setHistoryCollectResult(null);
        return;
      }
      setHistoryCollectResult(collectResult);
      if (collectResult.status === 'failed') {
        setHistoryCollectError(collectResult.message || '历史采集失败');
      }
    } catch (err) {
      setHistoryCollectError(err instanceof Error ? err.message : '历史采集失败');
      setHistoryCollectResult(null);
    } finally {
      setCollectingHistory(false);
    }
  };

  const handleToggleAutoCollect = async (enabled: boolean) => {
    setAutoCollectSaving(true);
    setHistoryCollectError('');
    try {
      const next = await onUpdateHistoryAutoCollect({
        enabled,
        collectStart: autoCollectStatus?.collectStart || '16:00',
        collectEnd: autoCollectStatus?.collectEnd || '17:00',
        includeBeijing,
      });
      if (next) {
        setAutoCollectStatus(next);
      }
    } catch (err) {
      setHistoryCollectError(err instanceof Error ? err.message : '自动采集配置保存失败');
    } finally {
      setAutoCollectSaving(false);
    }
  };

  const handleAdd = async (item: LowBuyScannerItem) => {
    const key = item.symbol.toLowerCase();
    if (watchlistSet.has(key) || added[key] || adding[key]) return;
    setAdding(prev => ({ ...prev, [key]: true }));
    try {
      const ok = await onAddToWatchlist(toWatchStock(item));
      if (ok) {
        setAdded(prev => ({ ...prev, [key]: true }));
      }
    } finally {
      setAdding(prev => ({ ...prev, [key]: false }));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1160px] h-[760px] max-w-[94vw] max-h-[88vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Search className="h-5 w-5 text-accent-2" />
            <div className="text-left">
              <div className="text-sm font-semibold fin-text-primary">低吸选股</div>
              <div className="text-[11px] fin-text-tertiary">V1.1 高胜率短线规则（全A扫描）</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="relative">
              <button
                onClick={toggleHistoryPanel}
                disabled={loading}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border transition-colors ${
                  historyFilterEnabled
                    ? 'border-accent/60 text-accent-2 bg-accent/10'
                    : 'fin-divider hover:border-accent/50'
                }`}
                title="打开历史质量筛选设置，确认后才会追加历史维度筛选"
              >
                <SlidersHorizontal className="h-3.5 w-3.5" />
                <span>调节历史维度</span>
              </button>
              {showHistoryPanel && (
                <div className="absolute right-0 top-[calc(100%+8px)] z-[70] w-[420px] fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3 text-xs">
                  <div className="flex items-start justify-between gap-3 mb-3">
                    <div>
                      <div className="text-sm font-semibold fin-text-primary">历史维度筛选</div>
                      <div className="mt-0.5 text-[11px] fin-text-tertiary">确认后在 V1.1 候选上继续收紧，结果可能只剩 1-2 只。</div>
                    </div>
                    <button
                      onClick={() => setShowHistoryPanel(false)}
                      className="p-1 rounded fin-hover"
                      title="关闭"
                    >
                      <X className="h-3.5 w-3.5 fin-text-tertiary" />
                    </button>
                  </div>

                  <div className="space-y-2.5">
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">连续天数</span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        value={historyTurnoverDays}
                        onChange={(e) => setHistoryTurnoverDays(Math.min(20, Math.max(1, Number(e.target.value) || 3)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`turnover-day-${day}`}
                            type="button"
                            onClick={() => setHistoryTurnoverDays(day)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyTurnoverDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">换手低于</span>
                      <input
                        type="number"
                        min={0.1}
                        max={20}
                        step={0.1}
                        value={historyTurnoverMax}
                        onChange={(e) => setHistoryTurnoverMax(Math.min(20, Math.max(0.1, Number(e.target.value) || 3)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[2, 3, 5].map(rate => (
                          <button
                            key={`turnover-rate-${rate}`}
                            type="button"
                            onClick={() => setHistoryTurnoverMax(rate)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyTurnoverMax === rate
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {rate}%
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">主力观察</span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        value={historyMainFlowDays}
                        onChange={(e) => {
                          const next = Math.min(20, Math.max(1, Number(e.target.value) || 3));
                          setHistoryMainFlowDays(next);
                          setHistoryMainFlowPositiveDays(prev => Math.min(prev, next));
                        }}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`flow-day-${day}`}
                            type="button"
                            onClick={() => {
                              setHistoryMainFlowDays(day);
                              setHistoryMainFlowPositiveDays(prev => Math.min(prev, day));
                            }}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMainFlowDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">至少为正</span>
                      <input
                        type="number"
                        min={1}
                        max={historyMainFlowDays}
                        value={historyMainFlowPositiveDays}
                        onChange={(e) => setHistoryMainFlowPositiveDays(Math.min(historyMainFlowDays, Math.max(1, Number(e.target.value) || 2)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`positive-day-${day}`}
                            type="button"
                            disabled={day > historyMainFlowDays}
                            onClick={() => setHistoryMainFlowPositiveDays(day)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMainFlowPositiveDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            } ${day > historyMainFlowDays ? 'opacity-40 cursor-not-allowed' : ''}`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">站上 MA</span>
                      <input
                        type="number"
                        min={3}
                        max={60}
                        value={historyMAPeriod}
                        onChange={(e) => setHistoryMAPeriod(Math.min(60, Math.max(3, Number(e.target.value) || 10)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[5, 10, 20].map(period => (
                          <button
                            key={`ma-${period}`}
                            type="button"
                            onClick={() => setHistoryMAPeriod(period)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMAPeriod === period
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            MA{period}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 rounded border border-amber-500/25 bg-amber-500/8 px-2 py-1.5 text-[11px] text-amber-200">
                    默认：连续3日换手&lt;3%，近3日主力至少2日净流入为正，站上MA10。
                  </div>

                  <div className="mt-3 flex items-center justify-between gap-2">
                    <button
                      onClick={() => {
                        setHistoryFilterEnabled(false);
                        setShowHistoryPanel(false);
                        onScan({
                          limit,
                          includeBeijing,
                          historyFilterEnabled: false,
                          historyTurnoverDays,
                          historyTurnoverMax,
                          historyMainFlowDays,
                          historyMainFlowPositiveDays,
                          historyMAPeriod,
                        });
                      }}
                      disabled={loading}
                      className="px-2.5 py-1.5 rounded-lg border fin-divider fin-text-secondary hover:border-accent/45 transition-colors"
                    >
                      关闭历史筛选
                    </button>
                    <button
                      onClick={confirmHistoryFilter}
                      disabled={loading}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-accent/55 text-accent-2 bg-accent/10 hover:bg-accent/15 transition-colors"
                    >
                      {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SlidersHorizontal className="h-3.5 w-3.5" />}
                      <span>确认筛选</span>
                    </button>
                  </div>
                </div>
              )}
            </div>
            <button
              onClick={runScan}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/50 transition-colors"
              title="开始扫描"
            >
              {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              <span>{loading ? '扫描中...' : '重新扫描'}</span>
            </button>
            <button
              onClick={handleCollectHistory}
              disabled={loading || collectingHistory}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/50 transition-colors"
              title="采集今日全A快照到本地 history.db，用于后续回踩测试"
            >
              {collectingHistory ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Database className="h-3.5 w-3.5" />}
              <span>{collectingHistory ? '采集中...' : '采集历史'}</span>
            </button>
            <label
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/45 transition-colors"
              title={`每日盘后自动采集全A快照，默认 ${autoCollectStatus?.collectStart || '16:00'}-${autoCollectStatus?.collectEnd || '17:00'}，同一天只采一次`}
            >
              {autoCollectSaving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Database className="h-3.5 w-3.5 fin-text-tertiary" />}
              <input
                type="checkbox"
                checked={Boolean(autoCollectStatus?.enabled)}
                disabled={autoCollectSaving}
                onChange={(e) => void handleToggleAutoCollect(e.target.checked)}
              />
              <span className={autoCollectStatus?.enabled ? 'text-accent-2' : 'fin-text-secondary'}>盘后自动</span>
            </label>
            <button onClick={onClose} className="p-2 rounded-lg fin-hover transition-colors">
              <X className="h-4 w-4 fin-text-secondary" />
            </button>
          </div>
        </div>

        <div className="px-4 py-2 border-b fin-divider-soft flex items-center gap-4 text-xs">
          <label className="flex items-center gap-2">
            <span className="fin-text-secondary">返回数量</span>
            <input
              type="number"
              min={10}
              max={200}
              value={limit}
              onChange={(e) => setLimit(Math.min(200, Math.max(10, Number(e.target.value) || 30)))}
              className="w-20 px-2 py-1 rounded fin-input text-xs"
            />
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
              <span>候选 {result.candidateCount}</span>
              <span>入选 {result.selectedCount}</span>
              <span>{result.asOf}</span>
            </div>
          )}
        </div>

        {result && (
          <div className="px-4 py-2 border-b fin-divider-soft text-xs">
            <div className="flex items-center gap-4 flex-wrap">
            <div className={`inline-flex items-center gap-1 ${result.marketGatePassed ? 'text-emerald-300' : 'text-amber-300'}`}>
              {result.marketGatePassed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
              <span>{result.marketGatePassed ? '大盘过滤通过' : '大盘过滤未通过（仅供观察）'}</span>
            </div>
            <span className="fin-text-tertiary">上证 {result.marketOverview.shPrice.toFixed(2)} / MA20 {result.marketOverview.shMA20.toFixed(2)}</span>
            <span className="fin-text-tertiary">涨停 {result.marketOverview.limitUpCount}</span>
            <span className="fin-text-tertiary">跌停 {result.marketOverview.limitDownCount}</span>
            <span className="fin-text-tertiary">两市成交额 {formatAmount(result.marketOverview.totalAmount)}</span>
            </div>
            {result.marketGateReasons && result.marketGateReasons.length > 0 && (
              <div className="mt-2 grid grid-cols-1 md:grid-cols-2 gap-1.5">
                {result.marketGateReasons.map((reason, idx) => {
                  const passed = isGateReasonPassed(reason);
                  return (
                    <div
                      key={`gate-${idx}`}
                      className={`inline-flex items-center gap-1.5 px-2 py-1 rounded border ${
                        passed
                          ? 'border-emerald-500/30 text-emerald-300 bg-emerald-500/8'
                          : 'border-amber-500/30 text-amber-300 bg-amber-500/8'
                      }`}
                    >
                      {passed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
                      <span>{reason}</span>
                    </div>
                  );
                })}
              </div>
            )}
            {result.warning && (
              <div className="mt-2 inline-flex items-center gap-1.5 px-2 py-1 rounded border border-amber-500/35 text-amber-300 bg-amber-500/8">
                <AlertTriangle className="h-3.5 w-3.5" />
                <span>{result.warning}</span>
              </div>
            )}
          </div>
        )}

        {(historyCollectResult || historyCollectError) && (
          <div className="px-4 py-2 border-b fin-divider-soft text-xs">
            {historyCollectResult && (
              <div className={`inline-flex items-center gap-1.5 px-2 py-1 rounded border ${
                historyCollectResult.status === 'success'
                  ? 'border-emerald-500/35 text-emerald-300 bg-emerald-500/8'
                  : 'border-amber-500/35 text-amber-300 bg-amber-500/8'
              }`}>
                {historyCollectResult.status === 'success' ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
                <span>
                  历史采集 {historyCollectResult.tradeDate}：全市场 {historyCollectResult.totalCount}，写入 {historyCollectResult.savedCount}，失败 {historyCollectResult.failedCount}，MA{historyCollectResult.maUpdated ? '已回写' : '未回写'}
                </span>
              </div>
            )}
            {historyCollectError && (
              <div className="mt-1 inline-flex items-center gap-1.5 px-2 py-1 rounded border border-red-500/35 text-red-300 bg-red-500/8">
                <AlertTriangle className="h-3.5 w-3.5" />
                <span>{historyCollectError}</span>
              </div>
            )}
          </div>
        )}

        <div className="flex-1 flex overflow-hidden">
          <div className="w-[56%] border-r fin-divider-soft overflow-auto fin-scrollbar">
            {loading && (
              <div className="h-full flex items-center justify-center text-sm fin-text-secondary">
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                正在扫描全A股票...
              </div>
            )}
            {!loading && error && (
              <div className="p-4 text-sm text-red-300">{error}</div>
            )}
            {!loading && !error && result && result.items.length === 0 && (
              <div className="p-4 text-sm fin-text-secondary">未找到符合规则的标的。</div>
            )}
            {!loading && !error && result && result.items.length > 0 && (
              <table className="w-full text-xs">
                <thead className="sticky top-0 fin-panel-strong border-b fin-divider-soft">
                  <tr>
                    <th className="text-left px-2 py-2">股票</th>
                    <th className="text-right px-2 py-2">评分</th>
                    <th className="text-right px-2 py-2">涨跌</th>
                    <th className="text-right px-2 py-2">换手</th>
                    <th className="text-right px-2 py-2" title="东财主力净流入不可用时，优先补齐腾讯资金流，其次腾讯盘口代理">主力</th>
                    <th className="text-right px-2 py-2">市值</th>
                    <th className="text-center px-2 py-2">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((item) => {
                    const key = item.symbol.toLowerCase();
                    const selectedRow = selected?.symbol === item.symbol;
                    const inWatch = watchlistSet.has(key) || added[key];
                    return (
                      <tr
                        key={item.symbol}
                        className={`cursor-pointer border-b fin-divider-soft ${selectedRow ? 'bg-accent/10' : 'hover:bg-slate-500/5'}`}
                        onClick={() => setSelected(item)}
                      >
                        <td className="px-2 py-2 text-left">
                          <div className="font-medium fin-text-primary">{item.name}</div>
                          <div className="fin-text-tertiary">{item.symbol} · {item.capBucket}</div>
                        </td>
                        <td className="px-2 py-2 text-right text-accent-2 font-semibold">{item.score.toFixed(1)}</td>
                        <td className={`px-2 py-2 text-right ${item.changePercent >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                          {item.changePercent >= 0 ? '+' : ''}{item.changePercent.toFixed(2)}%
                        </td>
                        <td className="px-2 py-2 text-right">{item.turnoverRate.toFixed(2)}%</td>
                        <td className={`px-2 py-2 text-right ${item.mainNetInflow >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                          <div>{Number.isFinite(item.mainNetInflow) ? formatAmount(item.mainNetInflow) : '--'}</div>
                          <div className="text-[10px] fin-text-tertiary">{formatMainFlowSource(item.mainFlowSource)}</div>
                        </td>
                        <td className="px-2 py-2 text-right">{(item.totalMarketCap / 1e8).toFixed(1)}亿</td>
                        <td className="px-2 py-2 text-center">
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              void handleAdd(item);
                            }}
                            disabled={inWatch || adding[key]}
                            className={`inline-flex items-center gap-1 px-2 py-1 rounded border transition-colors ${
                              inWatch
                                ? 'border-emerald-500/35 text-emerald-300 bg-emerald-500/10'
                                : 'border-accent/40 text-accent-2 hover:bg-accent/10'
                            }`}
                          >
                            <Plus className="h-3 w-3" />
                            <span>{inWatch ? '已加' : adding[key] ? '处理中' : '加自选'}</span>
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>

          <div className="flex-1 overflow-auto fin-scrollbar p-3 text-left">
            {!selected ? (
              <div className="text-sm fin-text-secondary">请选择左侧标的查看详情。</div>
            ) : (
              <div className="space-y-3">
                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="text-sm font-semibold fin-text-primary">{selected.name} <span className="font-normal fin-text-tertiary">({selected.symbol})</span></div>
                      <div className="text-xs fin-text-tertiary">{selected.industry} · {selected.capBucket}</div>
                    </div>
                    <div className="text-right">
                      <div className="text-lg font-mono fin-text-primary">{selected.price.toFixed(2)}</div>
                      <div className={`text-xs ${selected.changePercent >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                        {selected.changePercent >= 0 ? '+' : ''}{selected.changePercent.toFixed(2)}%
                      </div>
                    </div>
                  </div>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">触发信号（{selected.triggerCount}/{triggerTotal}）</div>
                  <div className="flex flex-wrap gap-1.5">
                    {selected.triggers.map((t) => (
                      <span key={t} className="text-[11px] px-2 py-0.5 rounded border border-accent/35 text-accent-2">{t}</span>
                    ))}
                  </div>
                  {selected.riskFlags.length > 0 && (
                    <>
                      <div className="text-xs font-semibold fin-text-primary mt-3 mb-1">风险提示</div>
                      <div className="flex flex-wrap gap-1.5">
                        {selected.riskFlags.map((rf) => (
                          <span key={rf} className="text-[11px] px-2 py-0.5 rounded border border-amber-500/35 text-amber-300">{rf}</span>
                        ))}
                      </div>
                    </>
                  )}
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">逻辑说明</div>
                  <ul className="text-xs fin-text-secondary space-y-1 list-disc pl-4">
                    {selected.reasons.map((r, i) => (
                      <li key={`${selected.symbol}-reason-${i}`}>{r}</li>
                    ))}
                  </ul>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">买卖纪律</div>
                  <div className="text-xs fin-text-secondary space-y-1">
                    <div>买点：{selected.buyPointHint}</div>
                    <div>卖点：{selected.sellPointHint}</div>
                    <div>止损：{selected.stopLossHint}</div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default LowBuyScannerDialog;
