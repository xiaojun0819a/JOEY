import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Bookmark, Check, FolderPlus, ListChecks, Star, Wallet } from 'lucide-react';
import type { Stock } from '../types';
import {
  addStockGroupDef,
  getStockGroupDefs,
  getStockGroups,
  setStockGroups,
  type StockGroup,
} from '../services/watchlistService';
import { addPaperPosition } from '../services/paperService';
import { getStrategySourceLabel, shouldAutoTagStrategySource } from '../utils/strategySource';
import { useTheme } from '../contexts/ThemeContext';

const PAPER_GROUP = '模拟持仓';

const FIXED: { name: string; icon: React.ElementType }[] = [
  { name: '自选', icon: Star },
  { name: '收藏', icon: Bookmark },
  { name: '候选', icon: ListChecks },
  { name: PAPER_GROUP, icon: Wallet },
];

interface BatchAddToGroupButtonProps {
  stocks: Stock[];
  source?: string;
  disabled?: boolean;
  buttonLabel?: string;
  onAddToWatchlist: (stock: Stock) => Promise<boolean> | void;
  onDone?: (groupName: string, count: number) => void;
  align?: 'left' | 'right';
}

export const BatchAddToGroupButton: React.FC<BatchAddToGroupButtonProps> = ({
  stocks,
  source,
  disabled,
  buttonLabel = '确认批量添加',
  onAddToWatchlist,
  onDone,
  align = 'right',
}) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const sourceKey = source || 'manual';
  const sourceLabel = getStrategySourceLabel(sourceKey);
  const shouldTagSource = shouldAutoTagStrategySource(sourceKey);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [activeGroupName, setActiveGroupName] = useState<string | null>(null);
  const [showNew, setShowNew] = useState(false);
  const [newName, setNewName] = useState('');
  const [groupDefs, setGroupDefs] = useState<StockGroup[]>([]);
  const ref = useRef<HTMLDivElement>(null);

  const uniqueStocks = useMemo(() => {
    const seen = new Set<string>();
    return stocks.filter((stock) => {
      const key = String(stock.symbol || '').toLowerCase();
      if (!key || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  }, [stocks]);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
        setShowNew(false);
      }
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  const ensureGroups = async (names: string[]): Promise<StockGroup[]> => {
    let defs = groupDefs.length > 0 ? groupDefs : await getStockGroupDefs();
    const out: StockGroup[] = [];
    for (const raw of names) {
      const name = raw.trim();
      if (!name) continue;
      let group = defs.find(item => item.name === name);
      if (!group) {
        group = await addStockGroupDef(name) || undefined;
        if (group) defs = [...defs, group];
      }
      if (group) out.push(group);
    }
    setGroupDefs(defs);
    return out;
  };

  const applyBatch = async (groupName: string) => {
    const name = groupName.trim();
    if (!name || busy || uniqueStocks.length === 0) return;
    setBusy(true);
    setActiveGroupName(name);
    try {
      const groupNames = name === '自选' ? [] : [name];
      if (shouldTagSource && !groupNames.includes(sourceLabel)) {
        groupNames.push(sourceLabel);
      }
      const groups = groupNames.length > 0 ? await ensureGroups(groupNames) : [];
      const groupIds = groups.map(group => group.id);
      const map = groupIds.length > 0 ? await getStockGroups() : {};

      for (const stock of uniqueStocks) {
        await onAddToWatchlist(stock);
        if (groupIds.length > 0) {
          const cur = map[stock.symbol] || [];
          const next = [...cur];
          for (const id of groupIds) {
            if (!next.includes(id)) next.push(id);
          }
          if (next.length !== cur.length) {
            await setStockGroups(stock.symbol, next);
            map[stock.symbol] = next;
          }
        }
        if (name === PAPER_GROUP && stock.price > 0) {
          await addPaperPosition(stock.symbol, stock.name, sourceKey, stock.price, 1000);
        }
      }

      window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
      onDone?.(name, uniqueStocks.length);
      setOpen(false);
      setShowNew(false);
      setNewName('');
    } finally {
      setBusy(false);
      setActiveGroupName(null);
    }
  };

  const openMenu = async () => {
    if (disabled || uniqueStocks.length === 0) return;
    const next = !open;
    setOpen(next);
    setShowNew(false);
    if (next) {
      try {
        setGroupDefs(await getStockGroupDefs());
      } catch {
        setGroupDefs([]);
      }
    }
  };

  const itemCls = `flex items-center gap-3 w-full px-4 py-2.5 text-left text-sm font-semibold transition-colors ${
    dark ? 'text-slate-100 bg-[#162235] hover:bg-[#1d2b43]' : 'text-slate-700 bg-white hover:bg-slate-100'
  }`;
  const menuAlignClass = align === 'left' ? 'left-0' : 'right-0';

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        type="button"
        onClick={openMenu}
        disabled={disabled || uniqueStocks.length === 0 || busy}
        className={`rounded-lg border px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
          dark
            ? 'border-amber-400/45 bg-amber-500/10 text-amber-200 hover:bg-amber-500/18'
            : 'border-amber-300 bg-amber-50 text-amber-700 hover:bg-amber-100'
        }`}
        title={uniqueStocks.length > 0 ? `批量添加 ${uniqueStocks.length} 只` : '请先勾选股票'}
      >
        {busy ? '批量处理中...' : `${buttonLabel}${uniqueStocks.length > 0 ? `(${uniqueStocks.length})` : ''}`}
      </button>

      {open && (
        <div className={`absolute ${menuAlignClass} top-full z-[120] mt-2 w-[220px] overflow-hidden rounded-xl border shadow-[0_20px_60px_rgba(0,0,0,0.65)] ring-1 ${
          dark ? 'border-slate-500 bg-[#162235] ring-black/70' : 'border-slate-300 bg-white ring-slate-900/10'
        }`}>
          {shouldTagSource && (
            <div className={`border-b px-4 py-2 text-center text-sm font-bold ${
              dark ? 'border-slate-600 bg-[#101827] text-amber-300' : 'border-slate-200 bg-amber-50 text-amber-700'
            }`}>
              策略标签：{sourceLabel}
            </div>
          )}

          {FIXED.map(item => (
            <button key={item.name} type="button" onClick={() => applyBatch(item.name)} disabled={busy} className={`${itemCls} disabled:opacity-70`}>
              <item.icon className="h-5 w-5 shrink-0 text-amber-300" />
              <span className="flex-1">加{item.name}</span>
              {busy && activeGroupName === item.name && <Check className="h-4 w-4 text-emerald-400" />}
            </button>
          ))}

          <div className={`border-t ${dark ? 'border-slate-600 bg-[#162235]' : 'border-slate-200 bg-white'}`}>
            {showNew ? (
              <div className={`flex items-center gap-2 px-3 py-2 ${dark ? 'bg-[#162235]' : 'bg-white'}`}>
                <input
                  autoFocus
                  value={newName}
                  onChange={e => setNewName(e.target.value)}
                  onClick={e => e.stopPropagation()}
                  onKeyDown={e => {
                    if (e.key === 'Enter') void applyBatch(newName);
                    if (e.key === 'Escape') setShowNew(false);
                  }}
                  placeholder="新分组名"
                  className={`min-w-0 flex-1 rounded px-2 py-1 text-xs ${
                    dark ? 'border border-slate-600 bg-slate-900 text-slate-100 placeholder-slate-500' : 'border border-slate-300 bg-white text-slate-800 placeholder-slate-400'
                  }`}
                />
                <button
                  type="button"
                  onClick={() => applyBatch(newName)}
                  disabled={busy}
                  className="rounded p-1 text-amber-300 hover:bg-amber-500/15 disabled:opacity-60"
                >
                  <Check className={`h-4 w-4 ${busy && activeGroupName === newName.trim() ? 'text-emerald-400' : ''}`} />
                </button>
              </div>
            ) : (
              <button type="button" onClick={() => setShowNew(true)} className={itemCls}>
                <FolderPlus className="h-5 w-5 shrink-0 text-slate-400" />
                <span>新建分组</span>
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default BatchAddToGroupButton;
