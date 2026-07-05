import React, { useEffect, useRef, useState } from 'react';
import { Plus, Check, Star, Bookmark, ListChecks, Wallet, FolderPlus, Tag } from 'lucide-react';
import type { Stock } from '../types';
import {
  getStockGroupDefs, addStockGroupDef, getStockGroups, setStockGroups, type StockGroup,
} from '../services/watchlistService';
import { addPaperPosition } from '../services/paperService';
import { getStrategySourceLabel, shouldAutoTagStrategySource } from '../utils/strategySource';
import { useTheme } from '../contexts/ThemeContext';

const PAPER_GROUP = '模拟持仓';

interface Props {
  stock: Stock;
  inWatch?: boolean; // 已在自选股池
  source?: string;   // 来源筛选系统：lowbuy-v1/wave-v1/caoyuan...
  onAddToWatchlist: (stock: Stock) => Promise<boolean> | void;
  onChanged?: () => void;
  className?: string;
  variant?: 'button' | 'tagIcon';
  menuAlign?: 'left' | 'right';
}

const FIXED: { name: string; icon: React.ElementType }[] = [
  { name: '自选', icon: Star },
  { name: '收藏', icon: Bookmark },
  { name: '候选', icon: ListChecks },
  { name: '模拟持仓', icon: Wallet },
];

export const AddToGroupButton: React.FC<Props> = ({
  stock,
  inWatch,
  source,
  onAddToWatchlist,
  onChanged,
  className,
  variant = 'button',
  menuAlign = 'right',
}) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const sourceKey = source || 'manual';
  const sourceLabel = getStrategySourceLabel(sourceKey);
  const shouldTagSource = shouldAutoTagStrategySource(sourceKey);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [doneTag, setDoneTag] = useState<string | null>(null);
  const [newName, setNewName] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [menuView, setMenuView] = useState<'actions' | 'groups'>('actions');
  const [groupDefs, setGroupDefs] = useState<StockGroup[]>([]);
  const [myGroupIds, setMyGroupIds] = useState<string[]>([]);
  const ref = useRef<HTMLDivElement>(null);

  const added = !!doneTag || !!inWatch;

  // 打开菜单时拉取该票当前分组，给已加的分组打勾
  const openMenu = async () => {
    const next = !open;
    setOpen(next);
    setShowNew(false);
    setMenuView('actions');
    if (next) {
      try {
        const [map, defs] = await Promise.all([getStockGroups(), getStockGroupDefs()]);
        setGroupDefs(defs);
        setMyGroupIds(map[stock.symbol] || []);
      } catch { /* ignore */ }
    }
  };

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
        setShowNew(false);
        setMenuView('actions');
      }
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  const hasGroupName = (groupName: string) => (
    groupDefs.some(group => group.name === groupName && myGroupIds.includes(group.id))
  );

  const refreshGroups = async () => {
    const [map, defs] = await Promise.all([getStockGroups(), getStockGroupDefs()]);
    setGroupDefs(defs);
    setMyGroupIds(map[stock.symbol] || []);
    return { map, defs };
  };

  const openGroupChooser = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await onAddToWatchlist(stock);
      await refreshGroups();
      setMenuView('groups');
      setShowNew(false);
      onChanged?.();
      window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
    } finally {
      setBusy(false);
    }
  };

  const toggleExistingGroup = async (group: StockGroup) => {
    if (busy) return;
    setBusy(true);
    try {
      await onAddToWatchlist(stock);
      const map = await getStockGroups();
      const cur = map[stock.symbol] || [];
      const next = cur.includes(group.id)
        ? cur.filter(id => id !== group.id)
        : [...cur, group.id];
      await setStockGroups(stock.symbol, next);
      setMyGroupIds(next);
      setDoneTag(group.name);
      onChanged?.();
      window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
    } finally {
      setBusy(false);
    }
  };

  const createAndAssignGroup = async (groupName: string) => {
    const name = groupName.trim();
    if (!name || busy) return;
    setBusy(true);
    try {
      await onAddToWatchlist(stock);
      let defs = groupDefs.length > 0 ? groupDefs : await getStockGroupDefs();
      let group = defs.find(item => item.name === name);
      if (!group) {
        group = await addStockGroupDef(name) || undefined;
        if (group) defs = [...defs, group];
      }
      if (group) {
        const map = await getStockGroups();
        const cur = map[stock.symbol] || [];
        const next = cur.includes(group.id) ? cur : [...cur, group.id];
        await setStockGroups(stock.symbol, next);
        setGroupDefs(defs);
        setMyGroupIds(next);
        setDoneTag(name);
      }
      setNewName('');
      setShowNew(false);
      setMenuView('groups');
      onChanged?.();
      window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
    } finally {
      setBusy(false);
    }
  };

  // 加入自选股池 + 打分组标签（组不存在则新建）
  const addToGroup = async (groupName: string) => {
    const name = groupName.trim();
    if (!name || busy) return;
    setBusy(true);
    try {
      await onAddToWatchlist(stock); // 加入自选（已存在则后端忽略）
      let defs = groupDefs.length > 0 ? groupDefs : await getStockGroupDefs();
      const ensureGroup = async (groupLabel: string): Promise<StockGroup | null> => {
        let g: StockGroup | null | undefined = defs.find(d => d.name === groupLabel);
        if (!g) {
          g = await addStockGroupDef(groupLabel);
          if (g) defs = [...defs, g];
        }
        return g || null;
      };
      const groupNames = [name];
      if (shouldTagSource && sourceLabel !== name && !groupNames.includes(sourceLabel)) {
        groupNames.push(sourceLabel);
      }
      const groups = (await Promise.all(groupNames.map(ensureGroup))).filter((g): g is StockGroup => Boolean(g));
      let nextGroupIds = myGroupIds;
      if (groups.length > 0) {
        const map = await getStockGroups();
        const cur = map[stock.symbol] || [];
        const next = [...cur];
        for (const g of groups) {
          if (!next.includes(g.id)) next.push(g.id);
        }
        if (next.length !== cur.length) await setStockGroups(stock.symbol, next);
        nextGroupIds = next;
      }
      // 加模拟持仓：同时建一笔纸上持仓（默认现价×1000股），并记录来源筛选系统
      if (name === PAPER_GROUP && stock.price > 0) {
        await addPaperPosition(stock.symbol, stock.name, sourceKey, stock.price, 1000);
      }
      setDoneTag(name === PAPER_GROUP && shouldTagSource ? sourceLabel : name);
      setGroupDefs(defs);
      setMyGroupIds(nextGroupIds);
      onChanged?.();
      // 通知主界面自选列表刷新分组（避免新建分组未同步）
      window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
    } finally {
      setBusy(false);
      setOpen(false);
      setShowNew(false);
      setNewName('');
    }
  };

  const itemCls = `flex items-center gap-2 w-full px-3 py-1.5 text-xs text-left transition-colors ${dark ? 'text-slate-200 hover:bg-slate-700' : 'text-slate-700 hover:bg-slate-100'}`;
  const groupItemCls = `flex items-center justify-between gap-3 w-full px-4 py-2.5 text-sm text-left font-semibold transition-colors ${dark ? 'text-slate-100 hover:bg-slate-700' : 'text-slate-700 hover:bg-slate-100'}`;

  const menuAlignClass = menuAlign === 'left' ? 'left-0' : 'right-0';

  return (
    <div ref={ref} className={`${variant === 'tagIcon' ? '' : 'relative'} ${className || ''}`}>
      {variant === 'tagIcon' ? (
        <button
          type="button"
          onClick={openMenu}
          disabled={busy}
          className={`flex h-5 w-5 items-center justify-center rounded-md transition-colors disabled:opacity-50 ${
            dark
              ? 'text-amber-300 hover:bg-amber-500/12 hover:text-amber-200'
              : 'text-amber-700 hover:bg-amber-100/70 hover:text-amber-800'
          }`}
          title={doneTag ? `已加入：${doneTag}` : added ? '设置股票分组' : '设置股票分组'}
        >
          <Tag className="h-4 w-4" />
        </button>
      ) : (
        <button
          onClick={openMenu}
          disabled={busy}
          className={`flex items-center gap-1 px-2.5 py-1.5 rounded-lg border text-xs transition-colors disabled:opacity-50 ${
            added
              ? 'border-green-500/40 text-green-400 bg-green-500/10'
              : dark ? 'border-slate-600 text-slate-200 hover:border-accent/50 hover:text-accent-2' : 'border-slate-300 text-slate-600 hover:border-accent/50'
          }`}
        >
          {added ? <Check className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
          <span>{doneTag ? `已加·${doneTag}` : added ? '已添加' : '添加'}</span>
        </button>
      )}

      {open && (
        <div className={`absolute ${menuAlignClass} top-full mt-1 z-50 rounded-lg border shadow-xl overflow-hidden ${
          menuView === 'groups' ? 'w-[234px]' : 'w-[176px]'
        } ${dark ? 'bg-slate-800 border-slate-600' : 'bg-white border-slate-300'}`}>
          {menuView === 'actions' ? (
            <>
              {shouldTagSource && (
                <div className={`px-3 py-1.5 text-[10px] border-b ${dark ? 'border-slate-700 text-amber-300 bg-slate-900/50' : 'border-slate-200 text-amber-700 bg-amber-50'}`}>
                  策略标签：{sourceLabel}
                </div>
              )}
              {FIXED.map(f => {
                const has = f.name === '自选' ? !!inWatch : hasGroupName(f.name);
                const handleClick = f.name === '自选' ? openGroupChooser : () => addToGroup(f.name);
                return (
                  <button key={f.name} onClick={handleClick} className={itemCls}>
                    <f.icon className="h-3.5 w-3.5 shrink-0 text-accent-2" />
                    <span className="flex-1">加{f.name}</span>
                    {has && <Check className="h-3.5 w-3.5 text-green-400" />}
                  </button>
                );
              })}
              <div className={`border-t ${dark ? 'border-slate-700' : 'border-slate-200'}`}>
                {showNew ? (
                  <div className="flex items-center gap-1 px-2 py-1.5">
                    <input
                      autoFocus
                      value={newName}
                      onChange={e => setNewName(e.target.value)}
                      onClick={e => e.stopPropagation()}
                      onKeyDown={e => { if (e.key === 'Enter') createAndAssignGroup(newName); if (e.key === 'Escape') setShowNew(false); }}
                      placeholder="新分组名"
                      className={`flex-1 min-w-0 text-xs px-1 py-0.5 rounded ${dark ? 'bg-slate-900 text-slate-100 border border-slate-600 placeholder-slate-500' : 'bg-white text-slate-800 border border-slate-300 placeholder-slate-400'}`}
                    />
                    <button onClick={() => createAndAssignGroup(newName)} className="p-0.5 rounded text-accent hover:bg-accent/20"><Check className="h-3.5 w-3.5" /></button>
                  </div>
                ) : (
                  <button onClick={() => { setMenuView('groups'); setShowNew(true); }} className={itemCls}>
                    <FolderPlus className="h-3.5 w-3.5 shrink-0 text-slate-400" />
                    <span>新建分组</span>
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <div className={`px-4 py-2 text-[12px] font-semibold ${dark ? 'text-slate-500' : 'text-slate-400'}`}>
                加入分组
              </div>
              <div className="max-h-[380px] overflow-y-auto fin-scrollbar">
                {groupDefs.map(group => {
                  const checked = myGroupIds.includes(group.id);
                  return (
                    <button key={group.id} type="button" onClick={() => toggleExistingGroup(group)} className={groupItemCls}>
                      <span className="min-w-0 truncate">{group.name}</span>
                      {checked && <Check className="h-4 w-4 shrink-0 text-amber-300" />}
                    </button>
                  );
                })}
                {groupDefs.length === 0 && (
                  <div className={`px-4 py-3 text-xs ${dark ? 'text-slate-500' : 'text-slate-400'}`}>
                    暂无分组
                  </div>
                )}
              </div>
              <div className={`border-t ${dark ? 'border-slate-700' : 'border-slate-200'}`}>
                {showNew ? (
                  <div className="flex items-center gap-1 px-3 py-2">
                    <input
                      autoFocus
                      value={newName}
                      onChange={e => setNewName(e.target.value)}
                      onClick={e => e.stopPropagation()}
                      onKeyDown={e => { if (e.key === 'Enter') createAndAssignGroup(newName); if (e.key === 'Escape') setShowNew(false); }}
                      placeholder="新分组名"
                      className={`flex-1 min-w-0 text-xs px-2 py-1 rounded ${dark ? 'bg-slate-900 text-slate-100 border border-slate-600 placeholder-slate-500' : 'bg-white text-slate-800 border border-slate-300 placeholder-slate-400'}`}
                    />
                    <button onClick={() => createAndAssignGroup(newName)} className="p-1 rounded text-accent hover:bg-accent/20"><Check className="h-4 w-4" /></button>
                  </div>
                ) : (
                  <button onClick={() => setShowNew(true)} className={`flex w-full items-center gap-2 px-4 py-2.5 text-sm font-semibold transition-colors ${dark ? 'text-slate-400 hover:bg-slate-700 hover:text-slate-100' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'}`}>
                    <FolderPlus className="h-4 w-4 shrink-0" />
                    <span>新建分组</span>
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};

export default AddToGroupButton;
