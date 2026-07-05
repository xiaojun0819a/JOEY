import React, { useState, useEffect, useCallback } from 'react';
import { X, FileText, Plus, Trash2, Pencil, ChevronDown, ChevronRight } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';
import { getTradeJournal, getTradeJournalStats, saveTradeJournal, deleteTradeJournal, TradeEntry, TradeStats } from '../services/journalService';
import { getKLineData, getStockRealTimeData, searchStocks } from '../services/stockService';
import { getStrategySourceLabel } from '../utils/strategySource';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onChanged?: () => void | Promise<void>;
}

type Granularity = 'day' | 'week' | 'month';
type Tab = 'list' | 'stats';
type TradeAction = 'build' | 'add' | 'reduce' | 'close' | 'manual';

const todayText = () => new Date().toISOString().slice(0, 10);
const emptyForm = {
  id: 0,
  action: 'build' as TradeAction,
  stockCode: '',
  stockName: '',
  tradeDate: '',
  buyDate: '',
  buyPrice: 0,
  shares: 0,
  sellDate: '',
  sellPrice: 0,
  note: '',
};

const normalizeInputSymbol = (raw: string): string | null => {
  const value = raw.trim().toLowerCase();
  if (!value) return null;
  if (/^(sh|sz|bj)\d{6}$/.test(value)) return value;
  if (!/^\d{6}$/.test(value)) return null;
  if (value.startsWith('6') || value.startsWith('5') || value.startsWith('9')) return `sh${value}`;
  if (value.startsWith('8') || value.startsWith('4') || value.startsWith('92')) return `bj${value}`;
  return `sz${value}`;
};

const actionLabel = (action?: string) => {
  switch (action) {
    case 'build': return '建仓';
    case 'add': return '加仓';
    case 'reduce': return '减仓';
    case 'close': return '平仓';
    default: return '手动';
  }
};

const actionTone = (action?: string, isDark = true) => {
  if (action === 'close') return 'bg-emerald-500/15 text-emerald-300';
  if (action === 'reduce') return 'bg-orange-500/15 text-orange-300';
  if (action === 'add') return 'bg-amber-500/15 text-amber-300';
  if (action === 'build') return 'bg-sky-500/15 text-sky-300';
  return isDark ? 'bg-slate-700 text-slate-300' : 'bg-slate-200 text-slate-600';
};

const tradeColumns = [
  { key: 'stock', label: '股票', width: '17%', align: 'text-center' },
  { key: 'buyDate', label: '建仓日', width: '10%', align: 'text-center' },
  { key: 'cost', label: '成本/现价', width: '10%', align: 'text-center' },
  { key: 'sellDate', label: '卖出', width: '9%', align: 'text-center' },
  { key: 'sellPrice', label: '卖价', width: '7%', align: 'text-center' },
  { key: 'pnl', label: '盈亏', width: '7%', align: 'text-center' },
  { key: 'pnlPct', label: '盈亏%', width: '8%', align: 'text-center' },
  { key: 'holding', label: '持仓', width: '7%', align: 'text-center' },
  { key: 'source', label: '来源', width: '6%', align: 'text-center' },
  { key: 'flow', label: '流水', width: '7%', align: 'text-center' },
  { key: 'actions', label: '操作', width: '12%', align: 'text-center' },
];

export const TradeJournalDialog: React.FC<Props> = ({ isOpen, onClose, onChanged }) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const isDark = colors.isDark;
  const [tab, setTab] = useState<Tab>('list');
  const [gran, setGran] = useState<Granularity>('day');
  const [entries, setEntries] = useState<TradeEntry[]>([]);
  const [stats, setStats] = useState<TradeStats | null>(null);
  const [editing, setEditing] = useState<typeof emptyForm | null>(null);
  const [expanded, setExpanded] = useState<Record<number, boolean>>({});
  const [showClosedHistory, setShowClosedHistory] = useState(false);
  const [autoFillLoading, setAutoFillLoading] = useState(false);

  const load = useCallback(async () => {
    setEntries(await getTradeJournal());
    setStats(await getTradeJournalStats());
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;
    (async () => {
      await load();
      if (!cancelled) {
        await onChanged?.();
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [isOpen, load, onChanged]);

  useEffect(() => {
    if (!editing || editing.action !== 'build') return;
    const normalized = normalizeInputSymbol(editing.stockCode);
    if (!normalized) return;
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      setAutoFillLoading(true);
      try {
        const [matches, realtime, dayK] = await Promise.all([
          searchStocks(normalized.slice(2)).catch(() => []),
          getStockRealTimeData([normalized]).catch(() => []),
          getKLineData(normalized, '1d', 1).catch(() => []),
        ]);
        if (cancelled) return;
        const exact = (matches || []).find(item => item.symbol?.toLowerCase() === normalized);
        const quote = (realtime || []).find(item => item.symbol?.toLowerCase() === normalized);
        const lastK = Array.isArray(dayK) && dayK.length > 0 ? dayK[dayK.length - 1] : null;
        const fillPrice = Number(lastK?.close || quote?.price || 0);
        setEditing(prev => {
          if (!prev || prev.action !== 'build') return prev;
          if (normalizeInputSymbol(prev.stockCode) !== normalized) return prev;
          return {
            ...prev,
            stockCode: normalized,
            stockName: prev.stockName || exact?.name || quote?.name || prev.stockName,
            buyPrice: prev.buyPrice > 0 ? prev.buyPrice : fillPrice,
          };
        });
      } finally {
        if (!cancelled) setAutoFillLoading(false);
      }
    }, 350);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [editing?.action, editing?.stockCode]);

  if (!isOpen) return null;

  const pnlClass = (v: number) => cc.getColorClass(v >= 0);
  const card = `rounded-lg border p-3 ${isDark ? 'border-slate-700 bg-slate-800/30' : 'border-slate-200 bg-slate-50'}`;
  const inputCls = `fin-input rounded px-2 py-1 text-sm ${isDark ? 'text-white' : 'text-slate-800'}`;
  const actionDate = editing?.tradeDate || editing?.buyDate || editing?.sellDate || todayText();
  const isSellAction = editing?.action === 'close' || editing?.action === 'reduce';
  const actionPrice = isSellAction ? Number(editing.sellPrice || 0) : Number(editing?.buyPrice || 0);

  const startAction = (action: TradeAction, entry?: TradeEntry) => {
    const tradeDate = todayText();
    if (entry) {
      setEditing({
        ...emptyForm,
        id: action === 'manual' ? entry.id : 0,
        action,
        stockCode: entry.stockCode,
        stockName: entry.stockName,
        tradeDate,
        buyDate: action === 'close' || action === 'reduce' ? entry.buyDate : tradeDate,
        buyPrice: action === 'manual' ? entry.buyPrice : action === 'add' ? Number(entry.currentPrice || entry.buyPrice || 0) : entry.buyPrice,
        shares: action === 'close' ? entry.shares : action === 'reduce' ? Math.max(100, Math.floor(Number(entry.shares || 0) / 2)) : action === 'manual' ? entry.shares : 1000,
        sellDate: action === 'close' || action === 'reduce' ? tradeDate : entry.sellDate,
        sellPrice: action === 'close' || action === 'reduce' ? Number(entry.currentPrice || entry.sellPrice || 0) : entry.sellPrice,
        note: entry.note || '',
      });
      return;
    }
    setEditing({ ...emptyForm, action, tradeDate, buyDate: tradeDate, sellDate: action === 'close' || action === 'reduce' ? tradeDate : '' });
  };

  const handleSave = async () => {
    if (!editing || !editing.stockCode) return;
    const isClose = editing.action === 'close' || editing.action === 'reduce';
    const isManual = editing.action === 'manual';
    await saveTradeJournal({
      id: isManual ? editing.id : 0,
      action: editing.action,
      stockCode: editing.stockCode,
      stockName: editing.stockName,
      buyDate: isClose ? editing.buyDate : actionDate,
      buyPrice: isClose ? Number(editing.buyPrice) || 0 : actionPrice,
      shares: Number(editing.shares) || 0,
      sellDate: isClose ? actionDate : editing.sellDate,
      sellPrice: isClose ? actionPrice : Number(editing.sellPrice) || 0,
      source: 'manual',
      note: editing.note,
    } as any);
    setEditing(null);
    await load();
    await onChanged?.();
  };

  const handleDelete = async (id: number) => {
    await deleteTradeJournal(id);
    await load();
    await onChanged?.();
  };

  const openEntries = entries.filter(e => e.status === 'open');
  const closedEntries = entries.filter(e => e.status !== 'open');
  const periods = gran === 'day' ? stats?.byDay : gran === 'week' ? stats?.byWeek : stats?.byMonth;
  const s = stats?.summary;
  const renderTradeColGroup = () => (
    <colgroup>
      {tradeColumns.map(col => <col key={col.key} style={{ width: col.width }} />)}
    </colgroup>
  );
  const renderTradeHeader = () => (
    <thead>
      <tr className={`text-xs ${isDark ? 'text-slate-400' : 'text-slate-500'}`}>
        {tradeColumns.map(col => (
          <th key={col.key} className={`${col.align} font-normal py-1 px-2 align-top whitespace-nowrap`}>
            {col.label}
          </th>
        ))}
      </tr>
    </thead>
  );

  const renderRows = (rows: TradeEntry[]) => rows.map(e => {
    const actions = e.actions || [];
    const isExpanded = !!expanded[e.id];
    return (
      <React.Fragment key={e.id}>
        <tr className={`border-t ${isDark ? 'border-slate-800' : 'border-slate-100'}`}>
          <td className="py-1.5 px-2 align-top"><span className={isDark ? 'text-white' : 'text-slate-800'}>{e.stockName || e.stockCode}</span> <span className="text-xs text-slate-400 font-mono">{e.stockCode}</span></td>
          <td className="px-2 align-top text-center text-slate-400 text-xs">{e.buyDate}</td>
          <td className="px-2 align-top text-center">
            <div>{e.buyPrice?.toFixed(2)}</div>
            {e.status === 'open' && <div className="text-[11px] text-slate-500">现 {e.currentPrice ? e.currentPrice.toFixed(2) : '--'}</div>}
          </td>
          <td className="px-2 align-top text-center text-slate-400 text-xs">{e.status === 'open' ? <span className="text-yellow-400">持仓中</span> : e.sellDate}</td>
          <td className="px-2 align-top text-center">{e.status === 'open' ? '--' : e.sellPrice?.toFixed(2)}</td>
          <td className={`px-2 align-top text-center ${pnlClass(e.pnl)}`}>{(e.pnl >= 0 ? '+' : '') + e.pnl.toFixed(0)}</td>
          <td className={`px-2 align-top text-center ${pnlClass(e.pnlPct)}`}>{(e.pnlPct >= 0 ? '+' : '') + e.pnlPct.toFixed(2)}%</td>
          <td className="px-2 align-top text-center text-slate-400">{e.holdDays + '日'}<div className="text-[11px] text-slate-500">{e.shares}股</div></td>
          <td className="px-2 align-top text-center text-xs text-slate-400">{getStrategySourceLabel(e.source)}</td>
          <td className="px-2 align-top text-center">
            <button
              onClick={() => setExpanded(prev => ({ ...prev, [e.id]: !prev[e.id] }))}
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-slate-400 hover:bg-slate-700/40"
            >
              {isExpanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
              {actions.length || 0}笔
            </button>
          </td>
          <td className="px-2 align-top">
            <div className="flex justify-center gap-1">
              {e.status === 'open' && <button onClick={() => startAction('add', e)} className="px-1.5 py-1 rounded hover:bg-amber-500/10 text-amber-300 text-xs">加</button>}
              {e.status === 'open' && <button onClick={() => startAction('reduce', e)} className="px-1.5 py-1 rounded hover:bg-orange-500/10 text-orange-300 text-xs">减</button>}
              {e.status === 'open' && <button onClick={() => startAction('close', e)} className="px-1.5 py-1 rounded hover:bg-emerald-500/10 text-emerald-300 text-xs">平</button>}
              <button onClick={() => startAction('manual', e)} className="p-1 rounded hover:bg-slate-700/40 text-slate-400"><Pencil className="h-3.5 w-3.5" /></button>
              <button onClick={() => handleDelete(e.id)} className="p-1 rounded hover:bg-red-500/10 text-red-400"><Trash2 className="h-3.5 w-3.5" /></button>
            </div>
          </td>
        </tr>
        {isExpanded && (
          <tr className={`${isDark ? 'bg-slate-950/30' : 'bg-slate-50'}`}>
            <td colSpan={11} className="px-4 py-2">
              {actions.length === 0 ? (
                <div className="text-xs text-slate-500">暂无动作流水，旧记录可继续编辑，后续建仓/加仓/平仓会自动保存。</div>
              ) : (
                <div className="grid gap-1">
                  {actions.map(a => (
                    <div key={a.id} className={`grid grid-cols-[70px_92px_90px_90px_110px_110px_1fr] items-center gap-2 rounded px-2 py-1 text-xs ${isDark ? 'bg-slate-900/70' : 'bg-white'}`}>
                      <span className={`w-fit rounded px-1.5 py-0.5 ${actionTone(a.action, isDark)}`}>{actionLabel(a.action)}</span>
                      <span className="font-mono text-slate-400">{a.tradeDate}</span>
                      <span>价 {Number(a.price || 0).toFixed(2)}</span>
                      <span>{a.shares}股</span>
                      <span>额 {Math.round(Number(a.amount || 0))}</span>
                      <span>后 {a.afterShares}股 / {Number(a.afterCost || 0).toFixed(2)}</span>
                      <span className="truncate text-slate-500">{a.note || a.createdAt}</span>
                    </div>
                  ))}
                </div>
              )}
            </td>
          </tr>
        )}
      </React.Fragment>
    );
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className={`relative w-[1180px] max-w-[96vw] max-h-[90vh] overflow-auto rounded-xl border fin-divider shadow-2xl ${isDark ? 'bg-[#0f1722]' : 'bg-white'}`}>
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b fin-divider sticky top-0 z-10 fin-panel-strong">
          <div className="flex items-center gap-2">
            <FileText className="h-4 w-4 text-emerald-400" />
            <span className={`font-semibold ${isDark ? 'text-white' : 'text-slate-800'}`}>交易台账</span>
            <span className={`text-xs ${isDark ? 'text-slate-400' : 'text-slate-500'}`}>实盘复盘 · 验证真实胜率</span>
          </div>
          <button onClick={onClose} className={`p-1 rounded ${isDark ? 'hover:bg-slate-700 text-slate-400' : 'hover:bg-slate-200 text-slate-500'}`}>
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="p-4 space-y-4">
          {/* 总览 */}
          <div className="grid grid-cols-3 md:grid-cols-6 gap-2 text-center">
            <div className={card}><div className="text-xs text-slate-400">胜率</div><div className={`text-lg font-bold ${isDark ? 'text-white' : 'text-slate-800'}`}>{s ? s.winRate.toFixed(1) : '--'}%</div></div>
            <div className={card}><div className="text-xs text-slate-400">总盈亏/浮动</div><div className={`text-lg font-bold ${pnlClass(s?.totalPnl || 0)}`}>{s ? (s.totalPnl >= 0 ? '+' : '') + s.totalPnl.toFixed(0) : '--'}</div></div>
            <div className={card}><div className="text-xs text-slate-400">盈亏比</div><div className={`text-lg font-bold ${isDark ? 'text-white' : 'text-slate-800'}`}>{s ? s.profitFactor.toFixed(2) : '--'}</div></div>
            <div className={card}><div className="text-xs text-slate-400">平均盈亏</div><div className={`text-lg font-bold ${pnlClass(s?.avgPnlPct || 0)}`}>{s ? (s.avgPnlPct >= 0 ? '+' : '') + s.avgPnlPct.toFixed(2) : '--'}%</div></div>
            <div className={card}><div className="text-xs text-slate-400">已平/持仓</div><div className={`text-lg font-bold ${isDark ? 'text-white' : 'text-slate-800'}`}>{s ? `${s.closedCount}/${s.openCount}` : '--'}</div></div>
            <div className={card}><div className="text-xs text-slate-400">平均持仓</div><div className={`text-lg font-bold ${isDark ? 'text-white' : 'text-slate-800'}`}>{s ? s.avgHoldDays.toFixed(1) : '--'}日</div></div>
          </div>

          {/* Tabs */}
          <div className="flex items-center gap-2">
            {(['list', 'stats'] as Tab[]).map(t => (
              <button key={t} onClick={() => setTab(t)} className={`px-3 py-1.5 rounded-lg text-sm ${tab === t ? 'bg-gradient-to-br from-[var(--accent)] to-[var(--accent-2)] text-white' : (isDark ? 'text-slate-400 hover:bg-slate-800' : 'text-slate-500 hover:bg-slate-100')}`}>
                {t === 'list' ? '交易明细' : '周期统计'}
              </button>
            ))}
	            {tab === 'list' && (
	              <div className="ml-auto flex items-center gap-2">
	                <button onClick={() => startAction('build')} className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-sky-500/20 text-sky-200 hover:bg-sky-500/30">
	                  <Plus className="h-4 w-4" />新增
	                </button>
	              </div>
	            )}
            {tab === 'stats' && (
              <div className="ml-auto flex gap-1">
                {(['day', 'week', 'month'] as Granularity[]).map(g => (
                  <button key={g} onClick={() => setGran(g)} className={`px-2.5 py-1 rounded text-sm ${gran === g ? 'bg-[var(--accent)] text-white' : (isDark ? 'text-slate-400 hover:bg-slate-800' : 'text-slate-500 hover:bg-slate-100')}`}>
                    {g === 'day' ? '日' : g === 'week' ? '周' : '月'}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* 手动记一笔表单 */}
          {editing && (
            <div className={`${card} space-y-2`}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className={`px-2 py-1 rounded text-xs font-semibold ${actionTone(editing.action, isDark)}`}>{actionLabel(editing.action)}</span>
                  <span className="text-xs text-slate-400">
                    {editing.action === 'add' && '会写入加仓流水，并自动重算持仓成本'}
                    {editing.action === 'reduce' && '会写入减仓流水，保留剩余持仓和原成本'}
                    {editing.action === 'build' && '新建一条持仓记录，并保存建仓流水'}
                    {editing.action === 'close' && '按当前未平仓记录结算，保存平仓流水'}
                    {editing.action === 'manual' && '编辑整笔汇总记录'}
                  </span>
                </div>
              </div>
              <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
                <div className="relative">
                  <input
                    className={`${inputCls} w-full pr-12`}
                    placeholder="代码 如300319"
                    value={editing.stockCode}
                    onChange={e => setEditing({ ...editing, stockCode: e.target.value })}
                    onBlur={() => {
                      const normalized = normalizeInputSymbol(editing.stockCode);
                      if (normalized && normalized !== editing.stockCode) {
                        setEditing({ ...editing, stockCode: normalized });
                      }
                    }}
                  />
                  {autoFillLoading && (
                    <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[11px] text-slate-400">同步</span>
                  )}
                </div>
                <input className={inputCls} placeholder="名称" value={editing.stockName} onChange={e => setEditing({ ...editing, stockName: e.target.value })} />
                <input className={inputCls} type="date" value={actionDate} onChange={e => setEditing({ ...editing, tradeDate: e.target.value, buyDate: isSellAction ? editing.buyDate : e.target.value, sellDate: isSellAction ? e.target.value : editing.sellDate })} />
                <input
                  className={inputCls}
                  type="number"
                  placeholder={isSellAction ? '卖出价' : '买入价'}
                  value={isSellAction ? (editing.sellPrice || '') : (editing.buyPrice || '')}
                  onChange={e => isSellAction
                    ? setEditing({ ...editing, sellPrice: Number(e.target.value) })
                    : setEditing({ ...editing, buyPrice: Number(e.target.value) })
                  }
                />
                <input className={inputCls} type="number" placeholder="数量(股)" value={editing.shares || ''} onChange={e => setEditing({ ...editing, shares: Number(e.target.value) })} />
                <input className={`${inputCls} md:col-span-5`} placeholder="备注" value={editing.note} onChange={e => setEditing({ ...editing, note: e.target.value })} />
              </div>
              <div className="flex justify-end gap-2">
                <button onClick={() => setEditing(null)} className={`px-3 py-1 rounded text-sm ${isDark ? 'text-slate-400 hover:bg-slate-700' : 'text-slate-500 hover:bg-slate-200'}`}>取消</button>
                <button onClick={handleSave} className="px-3 py-1 rounded text-sm text-white bg-[var(--accent)]">保存</button>
              </div>
            </div>
          )}

	          {/* 明细 */}
	          {tab === 'list' && (
	            <div className="space-y-3">
	              <div className="overflow-auto">
	                <table className="w-full table-fixed text-sm">
	                  {renderTradeColGroup()}
	                  {renderTradeHeader()}
	                  <tbody>
	                    {entries.length === 0 && <tr><td colSpan={11} className="text-center py-6 text-slate-400">还没有交易记录，点「建仓」开始保存买卖流水</td></tr>}
	                    {entries.length > 0 && openEntries.length === 0 && <tr><td colSpan={11} className="text-center py-6 text-slate-400">当前没有持仓中的股票，历史交易在下方展开查看</td></tr>}
	                    {renderRows(openEntries)}
	                  </tbody>
	                </table>
	              </div>
	              {closedEntries.length > 0 && (
	                <div className={`rounded-lg border ${isDark ? 'border-slate-800 bg-slate-950/25' : 'border-slate-200 bg-slate-50'}`}>
	                  <button
	                    type="button"
	                    onClick={() => setShowClosedHistory(prev => !prev)}
	                    className={`flex w-full items-center justify-between px-3 py-2 text-sm transition-colors ${isDark ? 'text-slate-300 hover:bg-slate-800/50' : 'text-slate-700 hover:bg-slate-100'}`}
	                  >
	                    <span className="inline-flex items-center gap-2 font-semibold">
	                      {showClosedHistory ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
	                      历史交易股票记录
	                    </span>
	                    <span className="text-xs text-slate-400">已平仓 {closedEntries.length} 笔</span>
	                  </button>
	                  {showClosedHistory && (
	                    <div className="overflow-auto border-t border-slate-800/70">
	                      <table className="w-full table-fixed text-sm">
	                        {renderTradeColGroup()}
	                        <tbody>{renderRows(closedEntries)}</tbody>
	                      </table>
	                    </div>
	                  )}
	                </div>
	              )}
	            </div>
	          )}

          {/* 周期统计 */}
          {tab === 'stats' && (
            <div className="overflow-auto">
              <table className="w-full text-sm">
                <thead><tr className={`text-xs ${isDark ? 'text-slate-400' : 'text-slate-500'}`}>
                  {['周期', '笔数', '胜率', '盈亏额', '平均盈亏%'].map(h => <th key={h} className="text-left font-normal py-1 px-2">{h}</th>)}
                </tr></thead>
                <tbody>
                  {(!periods || periods.length === 0) && <tr><td colSpan={5} className="text-center py-6 text-slate-400">暂无已平仓交易</td></tr>}
                  {periods?.map(p => (
                    <tr key={p.period} className={`border-t ${isDark ? 'border-slate-800' : 'border-slate-100'}`}>
                      <td className="py-1.5 px-2 font-mono">{p.period}</td>
                      <td className="px-2">{p.trades}</td>
                      <td className="px-2">{p.winRate.toFixed(0)}%</td>
                      <td className={`px-2 ${pnlClass(p.totalPnl)}`}>{(p.totalPnl >= 0 ? '+' : '') + p.totalPnl.toFixed(0)}</td>
                      <td className={`px-2 ${pnlClass(p.avgPnlPct)}`}>{(p.avgPnlPct >= 0 ? '+' : '') + p.avgPnlPct.toFixed(2)}%</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default TradeJournalDialog;
