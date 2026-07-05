import React, { useEffect, useState } from 'react';
import { BarChart3, X, Loader2, RefreshCw, Database, Plus, Check } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import { runFundamentalScan, refreshFundamentals, type FundamentalScanResult } from '../services/fundamentalService';
import { addPaperPosition } from '../services/paperService';

interface Props { isOpen: boolean; onClose: () => void; }

const PRESETS = [
  { key: 'value', label: '长线价值 A' },
  { key: 'boom', label: '中线景气 B' },
];

export const FundamentalScanDialog: React.FC<Props> = ({ isOpen, onClose }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [preset, setPreset] = useState('value');
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshMsg, setRefreshMsg] = useState('');
  const [res, setRes] = useState<FundamentalScanResult | null>(null);
  const [added, setAdded] = useState<Record<string, boolean>>({});

  const sourceOf = (p: string) => (p === 'boom' ? 'fund-boom' : 'fund-value');
  const addOne = async (symbol: string, name: string, price: number) => {
    await addPaperPosition(symbol, name, sourceOf(preset), price, 1000);
    setAdded(s => ({ ...s, [symbol]: true }));
  };
  const addTopN = async (n: number) => {
    for (const c of (res?.candidates || []).slice(0, n)) {
      if (!added[c.symbol] && c.price > 0) await addOne(c.symbol, c.name, c.price);
    }
  };

  const load = async (p: string) => {
    setLoading(true); setAdded({});
    try { setRes(await runFundamentalScan(p)); } finally { setLoading(false); }
  };
  useEffect(() => { if (isOpen) void load(preset); /* eslint-disable-next-line */ }, [isOpen, preset]);
  if (!isOpen) return null;

  const doRefresh = async () => {
    setRefreshing(true); setRefreshMsg('');
    try { setRefreshMsg(await refreshFundamentals()); await load(preset); } finally { setRefreshing(false); }
  };

  const th = `px-2 py-1.5 text-right font-medium text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const td = 'px-2 py-1.5 text-xs text-right font-mono';
  const pos = (v: number) => (v >= 0 ? 'text-rose-300' : 'text-emerald-300');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1040px] max-w-[95vw] h-[740px] max-h-[90vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <BarChart3 className="h-5 w-5 text-indigo-400" />
            <div>
              <div className="text-sm font-semibold fin-text-primary">基本面选股 · 量化初筛(框架①②)</div>
              <div className="text-[11px] fin-text-tertiary">批量财务硬规则筛全市场 → 候选清单,你再做步骤③人工定性(商业模式/壁垒/政策)</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex rounded-lg overflow-hidden border fin-divider text-xs">
              {PRESETS.map(p => (
                <button key={p.key} onClick={() => setPreset(p.key)}
                  className={`px-3 py-1.5 ${preset === p.key ? 'bg-indigo-500/20 text-indigo-200' : 'fin-text-secondary hover:bg-white/5'}`}>{p.label}</button>
              ))}
            </div>
            <button onClick={() => addTopN(10)} disabled={!res?.candidates?.length} className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg border border-indigo-400/40 bg-indigo-500/10 text-indigo-200 hover:bg-indigo-500/20 text-xs disabled:opacity-40" title="把前10只加入模拟持仓跟踪验证">
              <Plus className="h-3.5 w-3.5" />Top10加入跟踪
            </button>
            <button onClick={doRefresh} disabled={refreshing} className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg border fin-divider text-xs fin-text-secondary hover:bg-white/5 disabled:opacity-50" title="重新拉取最新财报入库">
              {refreshing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Database className="h-3.5 w-3.5" />}刷新财务
            </button>
            <button onClick={() => load(preset)} className="p-1.5 rounded fin-hover">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 fin-text-secondary" />}
            </button>
            <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
          </div>
        </div>

        {loading && !res ? (
          <div className="flex-1 flex items-center justify-center text-sm fin-text-tertiary"><Loader2 className="h-5 w-5 animate-spin mr-2" />筛选中…</div>
        ) : (
          <div className="flex-1 overflow-auto fin-scrollbar p-4">
            <div className="flex items-center gap-4 text-xs mb-2 px-1 flex-wrap">
              <span className="fin-text-secondary">报告期 <b className="fin-text-primary">{res?.reportDate || '—'}</b></span>
              <span className="fin-text-secondary">全市场 {res?.universeCount || 0}</span>
              <span className="fin-text-secondary">入选 <b className="text-indigo-300">{res?.candidates?.length || 0}</b></span>
              {refreshMsg && <span className="text-[11px] text-emerald-300">{refreshMsg}</span>}
              {res?.warning && <span className="text-[11px] text-amber-300">{res.warning}</span>}
            </div>
            <div className="text-[11px] fin-text-tertiary mb-3 px-1 leading-relaxed">{res?.rulesText}</div>
            <table className="w-full">
              <thead><tr className="border-b fin-divider-soft">
                <th className={`${th} text-left`}>#</th>
                <th className={`${th} text-left`}>股票</th>
                <th className={th}>市值(亿)</th><th className={th}>ROE年化</th><th className={th}>净利增速</th>
                <th className={th}>营收增速</th><th className={th}>毛利率</th><th className={th}>负债率</th>
                <th className={th}>商誉占净资</th><th className={th}>估值分位</th><th className={th}>成交(亿)</th><th className={th}>评分</th><th className={th}>跟踪</th>
              </tr></thead>
              <tbody>
                {(res?.candidates || []).map((c, i) => (
                  <tr key={c.symbol} className="border-b fin-divider-soft hover:bg-white/5">
                    <td className={`${td} text-left fin-text-tertiary`}>{i + 1}</td>
                    <td className="px-2 py-1.5 text-left text-xs"><div className="fin-text-primary">{c.name}</div><div className="text-[10px] font-mono fin-text-tertiary">{c.symbol}</div></td>
                    <td className={`${td} fin-text-secondary`}>{c.marketCapYi}</td>
                    <td className={`${td} text-rose-300`}>{c.annRoe}%</td>
                    <td className={`${td} ${pos(c.profitYoY)}`}>{c.profitYoY >= 0 ? '+' : ''}{c.profitYoY}%</td>
                    <td className={`${td} ${pos(c.revYoY)}`}>{c.revYoY >= 0 ? '+' : ''}{c.revYoY}%</td>
                    <td className={`${td} fin-text-secondary`}>{c.grossMargin}%</td>
                    <td className={`${td} ${c.debtRatio > 60 ? 'text-amber-300' : 'fin-text-secondary'}`}>{c.debtRatio}%</td>
                    <td className={`${td} ${c.goodwillRatio > 15 ? 'text-amber-300' : 'fin-text-secondary'}`}>{c.goodwillRatio}%</td>
                    <td className={`${td} ${c.valPctile <= 20 ? 'text-emerald-300' : 'fin-text-secondary'}`}>{c.valPctile}%</td>
                    <td className={`${td} fin-text-tertiary`}>{c.amountYi}</td>
                    <td className={`${td} text-indigo-300 font-semibold`}>{c.score}</td>
                    <td className={`${td}`}>
                      {added[c.symbol]
                        ? <span className="inline-flex items-center gap-0.5 text-[10px] text-emerald-300"><Check className="h-3 w-3" />已加</span>
                        : <button onClick={() => addOne(c.symbol, c.name, c.price)} className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded bg-indigo-500/15 text-indigo-200 hover:bg-indigo-500/25"><Plus className="h-3 w-3" />加入</button>}
                    </td>
                  </tr>
                ))}
                {(!res?.candidates || res.candidates.length === 0) && !loading && (
                  <tr><td className="px-2 py-6 text-xs fin-text-tertiary text-center" colSpan={13}>无入选 —— 若提示无财务数据,先点「刷新财务」拉取最新财报</td></tr>
                )}
              </tbody>
            </table>
            <div className="mt-3 text-[10px] fin-text-tertiary px-1 leading-relaxed">
              注：这是量化初筛(步骤①②),命中票仍需你做步骤③人工定性 —— 生意模式能否一句话讲清、壁垒能否被复制、管理层稳健与否、赛道政策是扶持还是打压。
              数据为单季同比,有季节性/低基数噪音(已剔极端值),严谨可看 TTM/年报。市占率/负债率/PE分位/减持为二期或人工。
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default FundamentalScanDialog;
