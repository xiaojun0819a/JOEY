import React, { useState, useEffect, useRef, useMemo } from 'react';
import { Stock, MarketIndex, KLineData } from '../types';
import { searchStocks, StockSearchResult, getKLineData } from '../services/stockService';
import { getHeldPositions } from '../services/sessionService';
import {
  getStockGroups, setStockGroups,
  getStockGroupDefs, addStockGroupDef, renameStockGroupDef, deleteStockGroupDef,
  type StockGroup,
} from '../services/watchlistService';
import { calculateTradingSignals, getLatestTradingSignal } from '../utils/tradingSignals';
import { Plus, TrendingUp, TrendingDown, Search, X, ArrowUpDown, Tag, Check, Filter, ChevronDown, Pencil, Trash2 } from 'lucide-react';
import { MarketIndices } from './MarketIndices';
import { Sparkline } from './Sparkline';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';

type SortMode = 'default' | 'changePercent' | 'signal' | 'pnl';

const SORT_LABELS: Record<SortMode, string> = {
  default: '自选顺序',
  changePercent: '按涨跌幅',
  signal: '按信号强度',
  pnl: '按持仓盈亏',
};

const TRADE_JOURNAL_GROUP_ID = 'trade-journal';

type GroupFilter = 'all' | string;

interface StockMeta {
  spark: number[]; // 近20日收盘
  badgeText: string; // S / S- / A+ / A / 减 / 卖 / —
  badgeKind: 'strong' | 'buy' | 'sell' | 'none';
  score: number; // 信号强度（买点为正，风险为负）
}

const EMPTY_META: StockMeta = { spark: [], badgeText: '—', badgeKind: 'none', score: 0 };

// 由日K计算"当前信号徽章"：只认最近3根内的信号，否则显示 —
function computeMeta(kline: KLineData[]): StockMeta {
  const spark = kline.slice(-20).map(d => d.close).filter(v => Number.isFinite(v));
  if (kline.length < 35) return { ...EMPTY_META, spark };
  const signals = calculateTradingSignals(kline);
  const latest = getLatestTradingSignal(signals);
  if (!latest) return { ...EMPTY_META, spark };
  const idx = kline.findIndex(d => d.time === latest.rawTime);
  const barsAgo = idx >= 0 ? kline.length - 1 - idx : 99;
  if (barsAgo > 3) return { ...EMPTY_META, spark };
  switch (latest.level) {
    case 'S':
      return { spark, badgeText: 'S', badgeKind: 'strong', score: latest.score };
    case 'S-':
      return { spark, badgeText: 'S-', badgeKind: 'strong', score: latest.score };
    case 'A+':
      return { spark, badgeText: 'A+', badgeKind: 'buy', score: latest.score };
    case 'A':
      return { spark, badgeText: 'A', badgeKind: 'buy', score: latest.score };
    case 'risk':
      return { spark, badgeText: latest.action === 'sell' ? '卖' : '减', badgeKind: 'sell', score: latest.score };
    default:
      return { ...EMPTY_META, spark };
  }
}

interface StockListProps {
  stocks: Stock[]; // The current watchlist
  selectedSymbol: string;
  onSelect: (symbol: string) => void;
  onAddStock: (stock: Stock) => void;
  onRemoveStock?: (symbol: string) => void;
  marketIndices?: MarketIndex[];
}

const normalizeInputSymbol = (raw: string): string | null => {
  const value = raw.trim().toLowerCase();
  if (!value) return null;
  if (/^(sh|sz|bj)\d{6}$/.test(value)) return value;
  if (!/^\d{6}$/.test(value)) return null;
  if (value.startsWith('6') || value.startsWith('5') || value.startsWith('9')) return `sh${value}`;
  if (value.startsWith('8') || value.startsWith('4') || value.startsWith('92')) return `bj${value}`;
  return `sz${value}`;
};

export const StockList: React.FC<StockListProps> = ({
  stocks,
  selectedSymbol,
  onSelect,
  onAddStock,
  onRemoveStock,
  marketIndices
}) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const [searchTerm, setSearchTerm] = useState('');
  const [searchResults, setSearchResults] = useState<StockSearchResult[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const searchRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const [sortMode, setSortMode] = useState<SortMode>('default');
  const [showSortMenu, setShowSortMenu] = useState(false);
  const sortRef = useRef<HTMLDivElement>(null);
  const [metaMap, setMetaMap] = useState<Record<string, StockMeta>>({});
  const [costMap, setCostMap] = useState<Record<string, number>>({});
  // 迷你走势图模式：daily=近20日日线趋势 / intraday=当天分时
  const [sparkMode, setSparkMode] = useState<'daily' | 'intraday'>(() => {
    return (localStorage.getItem('watchlistSparkMode') as 'daily' | 'intraday') || 'daily';
  });
  const [intradayMap, setIntradayMap] = useState<Record<string, number[]>>({});
  const [activeGroup, setActiveGroup] = useState<GroupFilter>('all');
  const [showGroupMenu, setShowGroupMenu] = useState(false);
  const groupFilterRef = useRef<HTMLDivElement>(null);
  const [groupsMap, setGroupsMap] = useState<Record<string, string[]>>({});
  const [groupMenuFor, setGroupMenuFor] = useState<string | null>(null);
  const groupMenuRef = useRef<HTMLDivElement>(null);
  const [groupDefs, setGroupDefs] = useState<StockGroup[]>([]);
  const [editingGroupId, setEditingGroupId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState('');
  const [newGroupName, setNewGroupName] = useState('');
  const [showNewGroupInput, setShowNewGroupInput] = useState(false);
  const [rowNewGroupFor, setRowNewGroupFor] = useState<string | null>(null);
  const [rowNewGroupName, setRowNewGroupName] = useState('');

  const symbolsKey = stocks.map(s => s.symbol).join(',');
  const groupName = (id: string): string => groupDefs.find(g => g.id === id)?.name || id;
  const findWatchedStock = (symbol: string): Stock | undefined => {
    const normalized = String(symbol || '').trim().toLowerCase();
    if (!normalized) return undefined;
    return stocks.find(s => s.symbol.toLowerCase() === normalized);
  };

  const reloadGroups = async () => {
    const [m, defs] = await Promise.all([getStockGroups(), getStockGroupDefs()]);
    setGroupsMap(m);
    setGroupDefs(defs);
  };

  // 加载分组映射 + 分组定义
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const [m, defs] = await Promise.all([getStockGroups(), getStockGroupDefs()]);
      if (!cancelled) { setGroupsMap(m); setGroupDefs(defs); }
    })();
    return () => { cancelled = true; };
  }, [symbolsKey]);

  // 外部(如选股弹窗的"添加")改了分组后广播事件，这里重载
  useEffect(() => {
    const onChanged = () => { void reloadGroups(); };
    window.addEventListener('watchlist-groups-changed', onChanged);
    return () => window.removeEventListener('watchlist-groups-changed', onChanged);
  }, []);

  const handleAddGroup = async () => {
    const name = newGroupName.trim();
    if (!name) return;
    await addStockGroupDef(name);
    setNewGroupName('');
    setShowNewGroupInput(false);
    await reloadGroups();
  };

  const handleRenameGroup = async (id: string) => {
    if (id === TRADE_JOURNAL_GROUP_ID) return;
    const name = editingName.trim();
    if (name) await renameStockGroupDef(id, name);
    setEditingGroupId(null);
    setEditingName('');
    await reloadGroups();
  };

  const handleDeleteGroup = async (id: string) => {
    if (id === TRADE_JOURNAL_GROUP_ID) return;
    await deleteStockGroupDef(id);
    if (activeGroup === id) setActiveGroup('all');
    await reloadGroups();
  };

  // 新建分组并立即把某股票加入
  const handleCreateAndAssign = async (symbol: string, name: string) => {
    const g = await addStockGroupDef(name.trim());
    if (g) {
      const cur = (groupsMap[symbol] || []);
      const next = [...cur, g.id];
      setGroupsMap(prev => ({ ...prev, [symbol]: next }));
      await setStockGroups(symbol, next);
    }
    await reloadGroups();
  };

  // 分组弹窗点外部关闭
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (groupMenuRef.current && !groupMenuRef.current.contains(e.target as Node)) setGroupMenuFor(null);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  const toggleGroup = async (symbol: string, gid: string) => {
    const cur = (groupsMap[symbol] || []);
    const next = cur.includes(gid) ? cur.filter(g => g !== gid) : [...cur, gid];
    setGroupsMap(prev => ({ ...prev, [symbol]: next }));
    await setStockGroups(symbol, next);
  };

  // 拉每只自选的日K，算迷你走势 + 信号徽章（自选变化时刷新）
  useEffect(() => {
    let cancelled = false;
    const symbols = symbolsKey ? symbolsKey.split(',') : [];
    if (symbols.length === 0) {
      setMetaMap({});
      return;
    }
    // 限流并发，避免一次性几十只票的日K请求打爆通达信 socket（会拖垮选股扫描的行情）
    (async () => {
      const CONCURRENCY = 3;
      let cursor = 0;
      const worker = async () => {
        while (!cancelled) {
          const i = cursor++;
          if (i >= symbols.length) return;
          const sym = symbols[i];
          let meta = EMPTY_META;
          try {
            const kline = await getKLineData(sym, '1d', 60);
            meta = computeMeta(Array.isArray(kline) ? kline : []);
          } catch {
            meta = EMPTY_META;
          }
          if (!cancelled) setMetaMap(prev => ({ ...prev, [sym]: meta }));
        }
      };
      await Promise.all(
        Array.from({ length: Math.min(CONCURRENCY, symbols.length) }, () => worker()),
      );
    })();
    return () => {
      cancelled = true;
    };
  }, [symbolsKey]);

  // 当天分时模式：额外拉每只自选的1分钟分时收盘序列（仅在该模式下，限流并发）
  useEffect(() => {
    if (sparkMode !== 'intraday') return;
    let cancelled = false;
    const symbols = symbolsKey ? symbolsKey.split(',') : [];
    if (symbols.length === 0) return;
    (async () => {
      const CONCURRENCY = 3;
      let cursor = 0;
      const worker = async () => {
        while (!cancelled) {
          const i = cursor++;
          if (i >= symbols.length) return;
          const sym = symbols[i];
          let closes: number[] = [];
          try {
            // '1m' 的第三参是分钟K根数(非天数)，传 240 取当天全部分时
            const kline = await getKLineData(sym, '1m', 240);
            if (Array.isArray(kline)) closes = kline.map(k => k.close).filter(v => Number.isFinite(v));
          } catch { /* ignore */ }
          if (!cancelled) setIntradayMap(prev => ({ ...prev, [sym]: closes }));
        }
      };
      await Promise.all(Array.from({ length: Math.min(CONCURRENCY, symbols.length) }, () => worker()));
    })();
    return () => { cancelled = true; };
  }, [symbolsKey, sparkMode]);

  // 拉持仓成本，供"按持仓盈亏"排序
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const held = await getHeldPositions();
      if (cancelled) return;
      const m: Record<string, number> = {};
      for (const h of held) {
        if (h?.position?.shares > 0 && h.position.costPrice > 0) m[h.stockCode] = h.position.costPrice;
      }
      setCostMap(m);
    })();
    return () => {
      cancelled = true;
    };
  }, [symbolsKey, sortMode]);

  // 排序/分组下拉点外部关闭
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (sortRef.current && !sortRef.current.contains(e.target as Node)) setShowSortMenu(false);
      if (groupFilterRef.current && !groupFilterRef.current.contains(e.target as Node)) setShowGroupMenu(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  const pnlOf = (s: Stock): number => {
    const cost = costMap[s.symbol];
    if (!cost || cost <= 0 || !s.price) return -Infinity; // 无持仓排到末尾
    return (s.price - cost) / cost;
  };

  const sortedStocks = useMemo(() => {
    // 先按分组过滤
    const filtered = activeGroup === 'all'
      ? stocks
      : stocks.filter(s => (groupsMap[s.symbol] || []).includes(activeGroup));
    if (sortMode === 'default') return filtered;
    const arr = [...filtered];
    if (sortMode === 'changePercent') {
      arr.sort((a, b) => b.changePercent - a.changePercent);
    } else if (sortMode === 'signal') {
      arr.sort((a, b) => (metaMap[b.symbol]?.score ?? 0) - (metaMap[a.symbol]?.score ?? 0));
    } else if (sortMode === 'pnl') {
      arr.sort((a, b) => pnlOf(b) - pnlOf(a));
    }
    return arr;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stocks, sortMode, metaMap, costMap, activeGroup, groupsMap]);

  const groupCount = (gid: string) => stocks.filter(s => (groupsMap[s.symbol] || []).includes(gid)).length;

  // 点击外部关闭下拉
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) {
        setShowDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // 搜索防抖
  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }

    if (!searchTerm.trim()) {
      setSearchResults([]);
      setShowDropdown(false);
      return;
    }

    setIsSearching(true);
    debounceRef.current = setTimeout(async () => {
      const results = await searchStocks(searchTerm);
      // 搜索结果保留已在自选/分组中的股票，方便直接点进去查看。
      const safeResults = Array.isArray(results) ? results : [];
      const seen = new Set<string>();
      const dedupedResults = safeResults.filter(r => {
        const key = String(r.symbol || '').toLowerCase();
        if (!key || seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      setSearchResults(dedupedResults);
      setShowDropdown(dedupedResults.length > 0);
      setIsSearching(false);
    }, 300);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [searchTerm]);

  // 选择搜索结果：已存在则直接打开，不存在才添加。
  const handleSelectResult = (result: StockSearchResult) => {
    const existing = findWatchedStock(result.symbol);
    if (existing) {
      onSelect(existing.symbol);
      setSearchTerm('');
      setShowDropdown(false);
      return;
    }
    const newStock: Stock = {
      symbol: result.symbol,
      name: result.name,
      price: 0,
      change: 0,
      changePercent: 0,
      volume: 0,
      amount: 0,
      marketCap: '',
      sector: result.industry,
      open: 0,
      high: 0,
      low: 0,
      preClose: 0,
    };
    onAddStock(newStock);
    setSearchTerm('');
    setShowDropdown(false);
  };

  const normalizedSymbol = normalizeInputSymbol(searchTerm);
  const canQuickAdd = Boolean(normalizedSymbol);
  const canQuickAddNotExist = Boolean(
    normalizedSymbol && !findWatchedStock(normalizedSymbol),
  );

  const handleQuickAdd = () => {
    if (!normalizedSymbol) return;
    const existing = findWatchedStock(normalizedSymbol);
    if (existing) {
      onSelect(existing.symbol);
      setSearchTerm('');
      setShowDropdown(false);
      return;
    }
    const plainCode = normalizedSymbol.slice(2);
    const stock: Stock = {
      symbol: normalizedSymbol,
      name: plainCode,
      price: 0,
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
    };
    onAddStock(stock);
    setSearchTerm('');
    setSearchResults([]);
    setShowDropdown(false);
  };

  return (
    <div className="flex flex-col h-full relative">
      <div className="p-4 border-b fin-divider-soft">
        {/* 大盘指数 */}
        <div className="mb-4 pb-3 border-b fin-divider-soft flex justify-center">
          <MarketIndices indices={marketIndices || []} />
        </div>
        <div ref={searchRef} className="relative z-50">
          <div className="relative">
            <Search className={`absolute left-3 top-2.5 h-4 w-4 ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`} />
            <input
              type="text"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              onFocus={() => searchResults.length > 0 && setShowDropdown(true)}
              onKeyDown={(e) => {
                if (e.key !== 'Enter') return;
                e.preventDefault();
                if (showDropdown && searchResults.length > 0) {
                  handleSelectResult(searchResults[0]);
                  return;
                }
                if (canQuickAdd) {
                  handleQuickAdd();
                }
              }}
              placeholder="搜索股票代码或名称..."
              className={`w-full fin-input rounded-lg pl-9 pr-24 py-2 text-sm ${colors.isDark ? 'placeholder-slate-500' : 'placeholder-slate-400'}`}
            />
            <button
              type="button"
              onClick={handleQuickAdd}
              disabled={!canQuickAddNotExist}
              className={`absolute right-2 top-1.5 h-7 px-2 rounded text-[11px] flex items-center gap-1 border transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                colors.isDark
                  ? 'bg-slate-700/80 border-slate-500 text-slate-200 hover:bg-slate-600'
                  : 'bg-slate-100 border-slate-300 text-slate-700 hover:bg-slate-200'
              }`}
              title={canQuickAdd && !canQuickAddNotExist ? '已在自选中' : '快速添加代码'}
            >
              <Plus className="h-3 w-3" />
              添加
            </button>
            {isSearching && (
              <div className="absolute right-16 top-2.5 h-4 w-4 border-2 border-accent border-t-transparent rounded-full animate-spin" />
            )}
          </div>

          {/* 搜索下拉结果 */}
          {showDropdown && (
            <div className={`absolute top-full left-0 right-0 mt-1 max-h-64 overflow-y-auto rounded-lg shadow-xl text-left ${colors.isDark ? 'bg-slate-800 border border-slate-600' : 'bg-white border border-slate-300'}`}>
              {searchResults.map((result) => {
                const existing = findWatchedStock(result.symbol);
                const myResultGroups = existing ? (groupsMap[existing.symbol] || []) : [];
                return (
                  <div
                    key={result.symbol}
                    onClick={() => handleSelectResult(result)}
                    className={`px-3 py-2 cursor-pointer border-b last:border-b-0 ${colors.isDark ? 'hover:bg-slate-700 border-slate-700' : 'hover:bg-slate-100 border-slate-200'}`}
                    title={existing ? '已在自选/分组中，点击打开' : '点击添加并打开'}
                  >
                    <div className="flex justify-between items-center gap-2">
                      <div className="min-w-0">
                        <span className={colors.isDark ? 'text-slate-200' : 'text-slate-700'}>{result.name}</span>
                        <span className="ml-2 font-mono text-accent-2 text-sm">{result.symbol}</span>
                      </div>
                      <div className="shrink-0 flex items-center gap-1">
                        {existing && (
                          <span className="rounded border border-accent/30 bg-accent/15 px-1.5 py-0.5 text-[10px] leading-none text-accent">
                            已添加
                          </span>
                        )}
                        <span className={`text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{result.market}</span>
                      </div>
                    </div>
                    <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-1">
                      {result.industry && (
                        <span className={`text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{result.industry}</span>
                      )}
                      {myResultGroups.slice(0, 2).map(g => (
                        <span key={g} title={groupName(g)} className="max-w-[72px] truncate rounded border border-accent/25 bg-accent/10 px-1 py-px text-[10px] leading-none text-accent">
                          {groupName(g)}
                        </span>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* 分组筛选 + 排序栏 */}
      {stocks.length > 0 && (
        <div className="px-3 py-1.5 border-b fin-divider-soft flex items-center justify-between gap-2">
          <div ref={groupFilterRef} className="relative">
            <button
              onClick={() => setShowGroupMenu(v => !v)}
              className={`flex items-center gap-1 text-[11px] px-2 py-1 rounded transition-colors ${
                activeGroup !== 'all'
                  ? 'bg-accent/20 text-accent font-medium'
                  : colors.isDark ? 'text-slate-300 hover:bg-slate-700/50' : 'text-slate-600 hover:bg-slate-200/60'
              }`}
            >
              <Filter size={12} />
              <span>{activeGroup === 'all' ? `全部 ${stocks.length}` : `${groupName(activeGroup)} ${groupCount(activeGroup)}`}</span>
              <ChevronDown size={12} />
            </button>
            {showGroupMenu && (
              <div className={`absolute left-0 top-full mt-1 z-50 rounded-lg shadow-xl overflow-hidden min-w-[150px] ${colors.isDark ? 'bg-slate-800 border border-slate-600' : 'bg-white border border-slate-300'}`}>
                <button
                  onClick={() => { setActiveGroup('all'); setShowGroupMenu(false); }}
                  className={`flex items-center justify-between w-full px-3 py-1.5 text-xs transition-colors ${activeGroup === 'all' ? 'text-accent font-medium' : colors.isDark ? 'text-slate-300 hover:bg-slate-700' : 'text-slate-600 hover:bg-slate-100'}`}
                >
                  <span>全部</span>
                  <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>{stocks.length}</span>
                </button>
                {groupDefs.map(g => {
                  const isTradeJournalGroup = g.id === TRADE_JOURNAL_GROUP_ID;
                  return (
                    <div key={g.id} className={`group/gi flex items-center px-2 py-1 ${colors.isDark ? 'hover:bg-slate-700' : 'hover:bg-slate-100'}`}>
                      {editingGroupId === g.id && !isTradeJournalGroup ? (
                        <input
                          autoFocus
                          value={editingName}
                          onChange={e => setEditingName(e.target.value)}
                          onClick={e => e.stopPropagation()}
                          onKeyDown={e => { if (e.key === 'Enter') handleRenameGroup(g.id); if (e.key === 'Escape') { setEditingGroupId(null); } }}
                          onBlur={() => handleRenameGroup(g.id)}
                          className={`flex-1 min-w-0 text-xs px-1 py-0.5 rounded ${colors.isDark ? 'bg-slate-900 text-slate-100 border border-slate-600' : 'bg-white text-slate-800 border border-slate-300'}`}
                        />
                      ) : (
                        <>
                          <button
                            onClick={() => { setActiveGroup(g.id); setShowGroupMenu(false); }}
                            className={`flex-1 min-w-0 flex items-center justify-between text-xs text-left ${activeGroup === g.id ? 'text-accent font-medium' : colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}
                          >
                            <span className="truncate">{g.name}</span>
                            <span className={`ml-2 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{groupCount(g.id)}</span>
                          </button>
                          {!isTradeJournalGroup && (
                            <>
                              <button
                                onClick={(e) => { e.stopPropagation(); setEditingGroupId(g.id); setEditingName(g.name); }}
                                className="opacity-0 group-hover/gi:opacity-100 p-0.5 ml-1 rounded hover:bg-accent/20 text-slate-400 hover:text-accent"
                                title="重命名"
                              >
                                <Pencil size={11} />
                              </button>
                              <button
                                onClick={(e) => { e.stopPropagation(); handleDeleteGroup(g.id); }}
                                className="opacity-0 group-hover/gi:opacity-100 p-0.5 rounded hover:bg-red-500/20 text-slate-400 hover:text-red-400"
                                title="删除分组"
                              >
                                <Trash2 size={11} />
                              </button>
                            </>
                          )}
                        </>
                      )}
                    </div>
                  );
                })}
                <div className={`border-t ${colors.isDark ? 'border-slate-700' : 'border-slate-200'}`}>
                  {showNewGroupInput ? (
                    <div className="flex items-center gap-1 px-2 py-1.5">
                      <input
                        autoFocus
                        value={newGroupName}
                        onChange={e => setNewGroupName(e.target.value)}
                        onClick={e => e.stopPropagation()}
                        onKeyDown={e => { if (e.key === 'Enter') handleAddGroup(); if (e.key === 'Escape') { setShowNewGroupInput(false); setNewGroupName(''); } }}
                        placeholder="新分组名"
                        className={`flex-1 min-w-0 text-xs px-1 py-0.5 rounded ${colors.isDark ? 'bg-slate-900 text-slate-100 border border-slate-600 placeholder-slate-500' : 'bg-white text-slate-800 border border-slate-300 placeholder-slate-400'}`}
                      />
                      <button onClick={handleAddGroup} className="p-0.5 rounded text-accent hover:bg-accent/20"><Check size={13} /></button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setShowNewGroupInput(true)}
                      className={`flex items-center gap-1 w-full px-3 py-1.5 text-xs transition-colors ${colors.isDark ? 'text-slate-400 hover:bg-slate-700 hover:text-slate-200' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'}`}
                    >
                      <Plus size={12} /> 新建分组
                    </button>
                  )}
                </div>
              </div>
            )}
          </div>
          <div className="flex items-center gap-1">
          {/* 走势图模式：日线趋势 / 当天分时 */}
          <button
            onClick={() => setSparkMode(m => { const n = m === 'daily' ? 'intraday' : 'daily'; localStorage.setItem('watchlistSparkMode', n); return n; })}
            className={`text-[11px] px-1.5 py-1 rounded transition-colors ${sparkMode === 'intraday' ? 'bg-accent/20 text-accent font-medium' : colors.isDark ? 'text-slate-400 hover:bg-slate-700/50' : 'text-slate-500 hover:bg-slate-200/60'}`}
            title={sparkMode === 'intraday' ? '走势图：当天分时（点切回日线趋势）' : '走势图：近20日日线趋势（点切当天分时）'}
          >
            {sparkMode === 'intraday' ? '分时' : '日线'}
          </button>
          <div ref={sortRef} className="relative">
            <button
              onClick={() => setShowSortMenu(v => !v)}
              className={`flex items-center gap-1 text-[11px] px-2 py-1 rounded transition-colors ${colors.isDark ? 'text-slate-300 hover:bg-slate-700/50' : 'text-slate-600 hover:bg-slate-200/60'}`}
            >
              <ArrowUpDown size={12} />
              <span>{SORT_LABELS[sortMode]}</span>
            </button>
            {showSortMenu && (
              <div className={`absolute right-0 top-full mt-1 z-50 rounded-lg shadow-xl overflow-hidden ${colors.isDark ? 'bg-slate-800 border border-slate-600' : 'bg-white border border-slate-300'}`}>
                {(Object.keys(SORT_LABELS) as SortMode[]).map(mode => (
                  <button
                    key={mode}
                    onClick={() => { setSortMode(mode); setShowSortMenu(false); }}
                    className={`block w-full text-left px-3 py-1.5 text-xs whitespace-nowrap transition-colors ${
                      mode === sortMode
                        ? 'text-accent font-medium'
                        : colors.isDark ? 'text-slate-300 hover:bg-slate-700' : 'text-slate-600 hover:bg-slate-100'
                    }`}
                  >
                    {SORT_LABELS[mode]}
                  </button>
                ))}
              </div>
            )}
          </div>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-y-auto fin-scrollbar">
        {sortedStocks.length === 0 && stocks.length > 0 && activeGroup !== 'all' && (
          <div className={`px-4 py-8 text-center text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
            「{groupName(activeGroup)}」分组暂无股票<br />
            <span className="text-[11px]">在股票行右上角 <Tag size={11} className="inline" /> 加入分组</span>
          </div>
        )}
        {sortedStocks.map((stock) => {
          const isSelected = stock.symbol === selectedSymbol;
          const isPositive = stock.change >= 0;
          const meta = metaMap[stock.symbol] || EMPTY_META;
          const cost = costMap[stock.symbol];
          const pnl = cost && cost > 0 && stock.price ? (stock.price - cost) / cost : null;
          const myGroups = (groupsMap[stock.symbol] || []);

          return (
            <div
              key={stock.symbol}
              onClick={() => onSelect(stock.symbol)}
              className={`group relative p-4 border-b fin-divider-soft cursor-pointer transition-colors ${colors.isDark ? 'hover:bg-slate-800/40' : 'hover:bg-slate-100/60'} ${isSelected ? (colors.isDark ? 'bg-slate-800/40' : 'bg-slate-100/60') + ' border-l-4 border-l-accent' : 'border-l-4 border-l-transparent'}`}
            >
              {/* 右上角操作：分组 + 删除，hover 才出现（透明背景，浮动图标） */}
              <div className={`absolute top-1 right-1 z-20 flex items-center gap-0.5 transition-opacity ${
                groupMenuFor === stock.symbol ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
              }`}>
                <div className="relative" ref={groupMenuFor === stock.symbol ? groupMenuRef : undefined}>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      setGroupMenuFor(groupMenuFor === stock.symbol ? null : stock.symbol);
                    }}
                    className={`p-0.5 rounded transition-all hover:bg-accent/20 ${
                      myGroups.length > 0 ? 'text-accent' : colors.isDark ? 'text-slate-400' : 'text-slate-500'
                    }`}
                    title="加入分组"
                  >
                    <Tag size={14} />
                  </button>
                  {groupMenuFor === stock.symbol && (
                    <div
                      onClick={(e) => e.stopPropagation()}
                      className={`absolute right-0 top-full mt-1 z-50 rounded-lg shadow-xl overflow-hidden min-w-[96px] ${colors.isDark ? 'bg-slate-800 border border-slate-600' : 'bg-white border border-slate-300'}`}
                    >
                      <div className={`px-2.5 py-1 text-[10px] ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>加入分组</div>
                      {groupDefs.map(g => {
                        const on = myGroups.includes(g.id);
                        const isTradeJournalGroup = g.id === TRADE_JOURNAL_GROUP_ID;
                        return (
                          <button
                            key={g.id}
                            onClick={() => {
                              if (!isTradeJournalGroup) toggleGroup(stock.symbol, g.id);
                            }}
                            disabled={isTradeJournalGroup}
                            className={`flex items-center justify-between w-full px-2.5 py-1.5 text-xs transition-colors ${
                              isTradeJournalGroup
                                ? colors.isDark ? 'cursor-default text-slate-500' : 'cursor-default text-slate-400'
                                : colors.isDark ? 'hover:bg-slate-700 text-slate-200' : 'hover:bg-slate-100 text-slate-700'
                            }`}
                          >
                            <span className="truncate">{g.name}</span>
                            {on && <Check size={12} className="text-accent shrink-0" />}
                          </button>
                        );
                      })}
                      <div className={`border-t ${colors.isDark ? 'border-slate-700' : 'border-slate-200'}`}>
                        {rowNewGroupFor === stock.symbol ? (
                          <div className="flex items-center gap-1 px-2 py-1.5">
                            <input
                              autoFocus
                              value={rowNewGroupName}
                              onChange={e => setRowNewGroupName(e.target.value)}
                              onKeyDown={e => {
                                if (e.key === 'Enter' && rowNewGroupName.trim()) { handleCreateAndAssign(stock.symbol, rowNewGroupName); setRowNewGroupFor(null); setRowNewGroupName(''); }
                                if (e.key === 'Escape') { setRowNewGroupFor(null); setRowNewGroupName(''); }
                              }}
                              placeholder="新分组名"
                              className={`flex-1 min-w-0 text-xs px-1 py-0.5 rounded ${colors.isDark ? 'bg-slate-900 text-slate-100 border border-slate-600 placeholder-slate-500' : 'bg-white text-slate-800 border border-slate-300 placeholder-slate-400'}`}
                            />
                            <button
                              onClick={() => { if (rowNewGroupName.trim()) { handleCreateAndAssign(stock.symbol, rowNewGroupName); setRowNewGroupFor(null); setRowNewGroupName(''); } }}
                              className="p-0.5 rounded text-accent hover:bg-accent/20"
                            >
                              <Check size={13} />
                            </button>
                          </div>
                        ) : (
                          <button
                            onClick={() => { setRowNewGroupFor(stock.symbol); setRowNewGroupName(''); }}
                            className={`flex items-center gap-1 w-full px-2.5 py-1.5 text-xs transition-colors ${colors.isDark ? 'text-slate-400 hover:bg-slate-700 hover:text-slate-200' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'}`}
                          >
                            <Plus size={12} /> 新建分组
                          </button>
                        )}
                      </div>
                    </div>
                  )}
                </div>
                {onRemoveStock && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onRemoveStock(stock.symbol);
                    }}
                    className={`p-0.5 rounded transition-all hover:bg-white/10 ${colors.isDark ? 'text-slate-400 hover:text-slate-200' : 'text-slate-500 hover:text-slate-700'}`}
                    title="移除自选"
                  >
                    <X size={15} strokeWidth={2.5} />
                  </button>
                )}
              </div>
              <div className="flex justify-between items-start mb-1">
                <div className="flex-1 min-w-0 pr-10">
                  <div className="flex items-center gap-1.5">
                    <span className={`font-bold truncate ${colors.isDark ? 'text-slate-100' : 'text-slate-800'}`}>{stock.name}</span>
                  </div>
                  <div className={`text-xs font-mono truncate text-left ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>{stock.symbol}</div>
                  <div className="mt-1 flex min-h-[18px] max-w-full flex-wrap items-center gap-1 overflow-hidden">
                    <SignalBadge meta={meta} />
                    {myGroups.map(g => (
                      <span key={g} title={groupName(g)} className="max-w-[92px] shrink-0 truncate px-1 py-px rounded text-[9px] leading-none bg-accent/15 text-accent border border-accent/25">
                        {groupName(g)}
                      </span>
                    ))}
                  </div>
                </div>
                <div className="text-right shrink-0">
                  <div className={`font-mono ${cc.getColorClass(isPositive)}`}>
                    {stock.price.toFixed(2)}
                  </div>
                  <div className={`text-xs font-mono flex items-center justify-end ${cc.getColorClass(isPositive)}`}>
                    {isPositive ? <TrendingUp size={12} className="mr-1"/> : <TrendingDown size={12} className="mr-1"/>}
                    {isPositive ? '+' : ''}{stock.changePercent.toFixed(2)}%
                  </div>
                  {/* 迷你走势图：涨跌幅下方（日线趋势 / 当天分时） */}
                  <div className="flex justify-end mt-1 h-5">
                    <Sparkline
                      values={sparkMode === 'intraday' && intradayMap[stock.symbol]?.length ? intradayMap[stock.symbol] : meta.spark}
                      positive={isPositive}
                    />
                  </div>
                </div>
              </div>
              <div className={`flex justify-between items-center text-xs mt-1 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
                <span>量: {formatVolume(stock.volume)}</span>
                {pnl !== null ? (
                  <span className={`font-mono ${cc.getColorClass(pnl >= 0)}`}>
                    持仓 {pnl >= 0 ? '+' : ''}{(pnl * 100).toFixed(2)}%
                  </span>
                ) : stock.sector ? (
                  <span className={`fin-chip px-1.5 py-0.5 rounded ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{stock.sector}</span>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

// 信号徽章：S/S-=强买(红) A+/A=买点(琥珀) 减/卖=风险(绿) —=无(灰)
const SignalBadge: React.FC<{ meta: StockMeta }> = ({ meta }) => {
  if (meta.badgeKind === 'none') {
    return <span className="text-[10px] text-slate-500 shrink-0">—</span>;
  }
  const cls =
    meta.badgeKind === 'strong'
      ? 'bg-red-500/20 text-red-400 border-red-500/30'
      : meta.badgeKind === 'buy'
      ? 'bg-amber-500/20 text-amber-400 border-amber-500/30'
      : 'bg-green-500/20 text-green-400 border-green-500/30';
  return (
    <span className={`shrink-0 px-1 py-px rounded text-[10px] font-bold leading-none border ${cls}`} title="当前信号">
      {meta.badgeText}
    </span>
  );
};

// 格式化成交量
const formatVolume = (vol: number): string => {
  if (vol >= 100000000) return (vol / 100000000).toFixed(2) + '亿';
  if (vol >= 10000) return (vol / 10000).toFixed(0) + '万';
  return vol.toString();
};
