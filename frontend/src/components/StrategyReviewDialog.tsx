import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, BarChart3, CalendarDays, Loader2, RefreshCw, TrendingUp, X } from 'lucide-react';
import {
  getStrategyNextDayReview,
  type StrategyReviewItem,
  type StrategyReviewResult,
} from '../services/strategyReviewService';

interface StrategyReviewDialogProps {
  isOpen: boolean;
  onClose: () => void;
  strategyId: string;
  strategyName: string;
  signalDate?: string;
}

const dateOnly = (value?: string): string => {
  const raw = String(value || '').trim();
  if (raw.length >= 10 && /^\d{4}-\d{2}-\d{2}/.test(raw)) return raw.slice(0, 10);
  return '';
};

const todayISO = () => {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
};

const formatPct = (value: number, plus = true): string => {
  if (!Number.isFinite(value)) return '--';
  const sign = plus && value > 0 ? '+' : '';
  return `${sign}${value.toFixed(2)}%`;
};

const formatAmount = (value: number): string => {
  if (!Number.isFinite(value) || value === 0) return value === 0 ? '0' : '--';
  const sign = value < 0 ? '-' : '';
  const abs = Math.abs(value);
  if (abs >= 1e8) return `${sign}${(abs / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${sign}${(abs / 1e4).toFixed(2)}万`;
  return `${sign}${abs.toFixed(0)}`;
};

const returnClass = (value: number): string => value >= 0 ? 'text-rose-300' : 'text-emerald-300';

const formatSignalScore = (value: number): string => {
  const score = Number(value || 0);
  return score > 0 ? `评分 ${score.toFixed(1)}` : '评分暂缺';
};

const ReviewItemCard: React.FC<{ item: StrategyReviewItem }> = ({ item }) => (
  <div className="rounded-lg border fin-divider bg-slate-950/20 p-3">
    <div className="flex items-start justify-between gap-3">
      <div className="flex min-w-0 flex-1 flex-col items-start text-left">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold fin-text-primary">{item.name || item.symbol}</span>
          <span className="font-mono text-[11px] fin-text-tertiary">{item.symbol}</span>
          {item.rank > 0 && <span className="rounded border border-slate-700 px-1.5 py-0.5 text-[10px] fin-text-tertiary">#{item.rank}</span>}
        </div>
        <div className="mt-1 w-fit max-w-full text-left text-[11px] fin-text-tertiary">
          入选价 {item.signalPrice?.toFixed?.(2) || '--'} · {formatSignalScore(item.signalScore)} · {item.industry || '行业未知'}
        </div>
        <div
          className="mt-0.5 max-w-[760px] truncate text-left text-[11px] fin-text-secondary"
          title={item.businessSummary ? `${item.businessSummary}${item.businessSource ? `（${item.businessSource}）` : ''}` : '主营暂缺'}
        >
          主营：{item.businessSummary || '暂缺'}
        </div>
      </div>
      <div className="shrink-0 text-right">
        <div className={`text-lg font-bold ${returnClass(item.closeReturnPercent)}`}>{formatPct(item.closeReturnPercent)}</div>
        <div className="text-[11px] fin-text-tertiary">收盘收益</div>
      </div>
    </div>

    <div className="mt-3 grid grid-cols-4 gap-2 text-xs">
      <div className="rounded border fin-divider-soft px-2 py-1.5">
        <div className="fin-text-tertiary">今日收盘</div>
        <div className="mt-1 fin-text-primary">{item.close ? item.close.toFixed(2) : '--'} <span className={returnClass(item.dayChangePercent)}>{formatPct(item.dayChangePercent)}</span></div>
      </div>
      <div className="rounded border fin-divider-soft px-2 py-1.5">
        <div className="fin-text-tertiary">盘中最高</div>
        <div className={`mt-1 ${returnClass(item.highReturnPercent)}`}>{formatPct(item.highReturnPercent)}</div>
      </div>
      <div className="rounded border fin-divider-soft px-2 py-1.5">
        <div className="fin-text-tertiary">主力资金</div>
        <div className={item.mainNetInflow >= 0 ? 'mt-1 text-rose-300' : 'mt-1 text-emerald-300'}>{formatAmount(item.mainNetInflow)}</div>
      </div>
      <div className="rounded border fin-divider-soft px-2 py-1.5">
        <div className="fin-text-tertiary">换手/成交额</div>
        <div className="mt-1 fin-text-primary">{item.turnoverRate ? item.turnoverRate.toFixed(2) : '--'}% / {formatAmount(item.amount)}</div>
      </div>
    </div>

    <div className="mt-3 grid grid-cols-2 gap-3 text-[11px] leading-relaxed">
      <div>
        <div className="mb-1 font-semibold fin-text-primary">昨日入选依据</div>
        <div className="fin-text-secondary">{(item.signalReasons || []).slice(0, 3).join('；') || (item.signalTriggers || []).slice(0, 4).join('；') || '暂无入选理由留痕'}</div>
      </div>
      <div>
        <div className="mb-1 font-semibold fin-text-primary">今日盘点</div>
        <div className="fin-text-secondary">{item.klineSummary || 'K线数据不足'}；{item.fundSummary || '资金数据不足'}</div>
      </div>
    </div>

    <div className="mt-3 rounded border border-amber-400/25 bg-amber-500/5 px-2 py-2 text-[11px] text-amber-200">
      <span className="font-semibold">结论：</span>{item.outcome || '待观察'}
      {(item.suggestions || []).length > 0 && (
        <span className="ml-2 fin-text-secondary">{item.suggestions.slice(0, 2).join('；')}</span>
      )}
    </div>

    {(item.news || []).length > 0 && (
      <div className="mt-2 text-[11px] fin-text-tertiary">
        最新相关消息：{item.news.map(n => `${n.time ? `${n.time} ` : ''}${n.content}`).join('；')}
      </div>
    )}
  </div>
);

export const StrategyReviewDialog: React.FC<StrategyReviewDialogProps> = ({
  isOpen,
  onClose,
  strategyId,
  strategyName,
  signalDate,
}) => {
  const [selectedSignalDate, setSelectedSignalDate] = useState(dateOnly(signalDate));
  const [reviewDate, setReviewDate] = useState(todayISO());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<StrategyReviewResult | null>(null);

  useEffect(() => {
    if (!isOpen) return;
    setSelectedSignalDate(dateOnly(signalDate));
    setReviewDate(todayISO());
  }, [isOpen, signalDate]);

  const loadReview = useCallback(async () => {
    if (!strategyId) return;
    setLoading(true);
    setError('');
    try {
      const res = await getStrategyNextDayReview({
        strategyId,
        strategyName,
        signalDate: selectedSignalDate,
        reviewDate,
      });
      if (!res) {
        setError('当前为浏览器预览模式，请从 Wails 开发入口打开后再复盘。');
        setResult(null);
        return;
      }
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : '策略复盘失败，请稍后重试');
      setResult(null);
    } finally {
      setLoading(false);
    }
  }, [strategyId, strategyName, selectedSignalDate, reviewDate]);

  useEffect(() => {
    if (!isOpen) return;
    void loadReview();
  }, [isOpen, loadReview]);

  const sortedItems = useMemo(() => {
    const rows = [...(result?.items || [])];
    rows.sort((a, b) => b.closeReturnPercent - a.closeReturnPercent);
    return rows;
  }, [result]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-[95] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/65 backdrop-blur-sm" onClick={onClose} />
      <div className="relative flex h-[820px] max-h-[92vh] w-[1180px] max-w-[96vw] flex-col overflow-hidden rounded-xl border fin-divider fin-panel shadow-2xl">
        <div className="flex items-center justify-between border-b fin-divider px-5 py-3">
          <div className="flex items-center gap-2">
            <BarChart3 className="h-5 w-5 text-amber-300" />
            <div>
              <div className="text-sm font-semibold fin-text-primary">{strategyName} · 次日收盘复盘</div>
              <div className="text-[11px] fin-text-tertiary">回看前一交易日入选理由，盘点今日K线/资金/大盘/消息，并给策略优化建议</div>
            </div>
          </div>
          <button onClick={onClose} className="rounded-lg p-2 fin-hover" title="关闭">
            <X className="h-4 w-4 fin-text-secondary" />
          </button>
        </div>

        <div className="flex items-center gap-2 border-b fin-divider-soft px-5 py-2 text-xs">
          <CalendarDays className="h-3.5 w-3.5 fin-text-tertiary" />
          <span className="fin-text-tertiary">选股日</span>
          <input
            type="date"
            value={selectedSignalDate}
            onChange={(e) => setSelectedSignalDate(e.target.value)}
            className="rounded fin-input px-2 py-1 text-xs"
            title="留空时自动读取该策略最近一次扫描记录"
          />
          <span className="fin-text-tertiary">复盘日</span>
          <input
            type="date"
            value={reviewDate}
            onChange={(e) => setReviewDate(e.target.value)}
            className="rounded fin-input px-2 py-1 text-xs"
          />
          <button
            onClick={() => void loadReview()}
            disabled={loading}
            className="inline-flex items-center gap-1.5 rounded-lg border border-amber-400/50 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200 hover:bg-amber-500/15 disabled:opacity-50"
          >
            {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            <span>{loading ? '复盘中...' : '刷新复盘'}</span>
          </button>
          {result && (
            <div className="ml-auto fin-text-tertiary">
              入选 {result.pickCount} · 有效 {result.reviewedCount} · 胜率 <span className="fin-text-primary">{result.winRate.toFixed(1)}%</span>
            </div>
          )}
        </div>

        <div className="flex-1 overflow-auto p-4 fin-scrollbar">
          {loading && (
            <div className="flex h-full items-center justify-center text-sm fin-text-secondary">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              正在补齐今日K线、资金、大盘和消息...
            </div>
          )}

          {!loading && error && (
            <div className="rounded-lg border border-rose-400/30 bg-rose-500/10 p-3 text-sm text-rose-200">{error}</div>
          )}

          {!loading && result && (
            <div className="space-y-3">
              {result.warning && (
                <div className="flex items-center gap-2 rounded-lg border border-amber-400/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  <span>{result.warning}</span>
                </div>
              )}

              <div className="grid grid-cols-5 gap-2 text-xs">
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="fin-text-tertiary">选股日 / 复盘日</div>
                  <div className="mt-1 fin-text-primary">{result.signalDate || '--'} → {result.reviewDate || '--'}</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="fin-text-tertiary">平均收盘收益</div>
                  <div className={`mt-1 font-mono text-base font-semibold ${returnClass(result.avgCloseReturn)}`}>{formatPct(result.avgCloseReturn)}</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="fin-text-tertiary">平均盘中高点</div>
                  <div className={`mt-1 font-mono text-base font-semibold ${returnClass(result.avgHighReturn)}`}>{formatPct(result.avgHighReturn)}</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="fin-text-tertiary">高点≥3%</div>
                  <div className="mt-1 font-mono text-base font-semibold fin-text-primary">{result.hit3Rate.toFixed(1)}%</div>
                </div>
                <div className="rounded-lg border fin-divider bg-slate-950/20 px-3 py-2">
                  <div className="fin-text-tertiary">大盘环境</div>
                  <div className={result.market?.shChangePercent >= 0 ? 'mt-1 text-rose-300' : 'mt-1 text-emerald-300'}>
                    上证 {formatPct(result.market?.shChangePercent || 0)}
                  </div>
                </div>
              </div>

              <div className="rounded-lg border fin-divider bg-slate-950/20 p-3">
                <div className="mb-2 flex items-center gap-1.5 text-xs font-semibold fin-text-primary">
                  <TrendingUp className="h-3.5 w-3.5 text-amber-300" />
                  今日大盘与策略建议
                </div>
                <div className="text-xs fin-text-secondary leading-relaxed">{result.market?.summary || '大盘快照不足'}</div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {(result.optimization || []).map((item, idx) => (
                    <span key={`${item}-${idx}`} className="rounded border border-amber-400/25 bg-amber-500/5 px-2 py-1 text-[11px] text-amber-100">
                      {item}
                    </span>
                  ))}
                </div>
              </div>

              {sortedItems.length === 0 ? (
                <div className="rounded-lg border fin-divider p-6 text-center text-sm fin-text-secondary">
                  暂无可复盘标的。请先回到该策略点“重新扫描”，系统会把入选股票保存为复盘留痕；次日收盘后再点这里。
                </div>
              ) : (
                <div className="space-y-2">
                  {sortedItems.map((item) => (
                    <ReviewItemCard key={item.symbol} item={item} />
                  ))}
                </div>
              )}

              {(result.news || []).length > 0 && (
                <div className="rounded-lg border fin-divider bg-slate-950/20 p-3">
                  <div className="mb-2 text-xs font-semibold fin-text-primary">市场快讯参考</div>
                  <div className="space-y-1 text-[11px] fin-text-secondary">
                    {result.news.map((item, idx) => (
                      <div key={`${item.time}-${idx}`}>
                        <span className="fin-text-tertiary">{item.time}</span> {item.content}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {(result.dataSourceNotes || []).length > 0 && (
                <div className="text-[10px] fin-text-tertiary">
                  数据说明：{(result.dataSourceNotes || []).join('；')}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default StrategyReviewDialog;
