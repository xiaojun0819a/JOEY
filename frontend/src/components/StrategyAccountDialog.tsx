import React, { useEffect, useState } from 'react';
import { Wallet, X, Loader2, RefreshCw } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import { runStrategyAccount, runPaperStrategyAccount, EXIT_REASON_CN, type StrategyAccountResult } from '../services/accountService';
import { getPaperStats } from '../services/paperService';
import { STRATEGY_SOURCE_LABELS } from '../utils/strategySource';

interface Props { isOpen: boolean; onClose: () => void; }

type Strat = { key: string; label: string };
type Mode = 'paper' | 'backtest';

// 理论回测：全市场历史自动回测
const BACKTEST_STRATS: Strat[] = [
  { key: 'lowbuy', label: '低吸账户' },
  { key: 'taillazy', label: '尾盘懒人账户' },
  { key: 'hotmoney', label: '游资突破7' },
  { key: 'monster', label: '捉妖9' },
  { key: 'dipentry', label: '低吸入场8' },
];

// 净值曲线 SVG
const EquityCurve: React.FC<{ pts: { date: string; value: number }[]; capital: number }> = ({ pts, capital }) => {
  if (!pts || pts.length < 2) return <div className="text-xs fin-text-tertiary py-8 text-center">无净值数据</div>;
  const vals = pts.map(p => p.value);
  const min = Math.min(...vals, capital), max = Math.max(...vals, capital);
  const W = 720, H = 160, pad = 4;
  const x = (i: number) => pad + (i / (pts.length - 1)) * (W - 2 * pad);
  const y = (v: number) => pad + (1 - (v - min) / (max - min || 1)) * (H - 2 * pad);
  const line = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ');
  const baseY = y(capital);
  const up = pts[pts.length - 1].value >= capital;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 160 }} preserveAspectRatio="none">
      <line x1={pad} y1={baseY} x2={W - pad} y2={baseY} stroke="currentColor" strokeWidth={1} strokeDasharray="4 4" className="fin-text-tertiary" opacity={0.5} />
      <path d={line} fill="none" stroke={up ? '#fb7185' : '#34d399'} strokeWidth={1.6} />
    </svg>
  );
};

export const StrategyAccountDialog: React.FC<Props> = ({ isOpen, onClose }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [mode, setMode] = useState<Mode>('paper');
  const [paperStrats, setPaperStrats] = useState<Strat[]>([]);
  const [strategy, setStrategy] = useState('');
  const [loading, setLoading] = useState(false);
  const [res, setRes] = useState<StrategyAccountResult | null>(null);
  const [useRisk, setUseRisk] = useState(false);

  const strats = mode === 'paper' ? paperStrats : BACKTEST_STRATS;

  const load = async (m: Mode, strat: string, risk = useRisk) => {
    if (!strat) { setRes(null); return; }
    setLoading(true);
    try {
      setRes(m === 'paper' ? await runPaperStrategyAccount(strat) : await runStrategyAccount(strat, 250, risk));
    } finally { setLoading(false); }
  };

  // 打开时拉取"我加过模拟持仓"的策略来源
  useEffect(() => {
    if (!isOpen) return;
    let alive = true;
    (async () => {
      const stats = await getPaperStats();
      if (!alive) return;
      const list = (stats?.bySource || [])
        .filter(s => s.total > 0)
        .map(s => ({ key: s.source, label: STRATEGY_SOURCE_LABELS[s.source] || s.source }));
      setPaperStrats(list);
      if (list.length === 0) {
        setMode('backtest');
        setStrategy('lowbuy');
      } else {
        setStrategy(list[0].key);
      }
    })();
    return () => { alive = false; };
  }, [isOpen]);

  // mode / strategy / 风控开关 变化时重算
  useEffect(() => { if (isOpen && strategy) void load(mode, strategy, useRisk); /* eslint-disable-next-line */ }, [isOpen, mode, strategy, useRisk]);
  if (!isOpen) return null;

  const switchMode = (m: Mode) => {
    if (m === mode) return;
    setMode(m);
    setStrategy(m === 'paper' ? (paperStrats[0]?.key || '') : 'lowbuy');
  };

  const colorPct = (v: number) => (v >= 0 ? 'text-rose-300' : 'text-emerald-300');
  const fmt = (v: number, s = false) => `${s && v >= 0 ? '+' : ''}${v.toFixed(2)}%`;
  const th = `px-2 py-1.5 text-left font-medium text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const td = 'px-2 py-1.5 text-xs';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1080px] max-w-[95vw] h-[760px] max-h-[90vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        {/* header */}
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-3">
            <Wallet className="h-5 w-5 text-amber-400" />
            <div>
              <div className="text-sm font-semibold fin-text-primary">
                策略账户
                {/* 模式切换 */}
                <span className="ml-3 inline-flex rounded-md overflow-hidden border fin-divider text-[11px] align-middle">
                  <button onClick={() => switchMode('paper')}
                    className={`px-2 py-0.5 ${mode === 'paper' ? 'bg-amber-400/20 text-amber-200' : 'fin-text-secondary hover:bg-white/5'}`}>实盘跟踪</button>
                  <button onClick={() => switchMode('backtest')}
                    className={`px-2 py-0.5 ${mode === 'backtest' ? 'bg-amber-400/20 text-amber-200' : 'fin-text-secondary hover:bg-white/5'}`}>理论回测</button>
                </span>
              </div>
              <div className="text-[11px] fin-text-tertiary">
                {mode === 'paper'
                  ? '我加进模拟持仓的票 · 按策略分组 · 从加入日起真实数据盯市(今日实时) · 平仓同步 · 扣成本0.35%'
                  : '全市场历史回测 · 10万本金 · 尾盘进场 · Top3/限6仓 · 机械纪律平仓 · 冷却3日 · 扣成本0.35%'}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex rounded-lg overflow-hidden border fin-divider text-xs max-w-[520px] overflow-x-auto fin-scrollbar">
              {strats.length === 0 ? (
                <span className="px-3 py-1.5 fin-text-tertiary whitespace-nowrap">暂无模拟持仓</span>
              ) : strats.map(s => (
                <button key={s.key} onClick={() => setStrategy(s.key)}
                  className={`px-3 py-1.5 whitespace-nowrap ${strategy === s.key ? 'bg-accent/15 text-accent-2' : 'fin-text-secondary hover:bg-white/5'}`}>
                  {s.label}
                </button>
              ))}
            </div>
            {mode === 'backtest' && (
              <label className="flex items-center gap-1 text-[11px] fin-text-secondary cursor-pointer whitespace-nowrap px-1" title="所有持仓改套统一风控线(短线稳健:-8%硬止损/保本/移动止损/+25%止盈/10日时停)替代策略原纪律，对比净值曲线">
                <input type="checkbox" checked={useRisk} onChange={e => setUseRisk(e.target.checked)} />
                套风控
              </label>
            )}
            <button onClick={() => load(mode, strategy)} className="p-1.5 rounded fin-hover" title="重算">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 fin-text-secondary" />}
            </button>
            <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
          </div>
        </div>

        {loading && !res ? (
          <div className="flex-1 flex items-center justify-center text-sm fin-text-tertiary"><Loader2 className="h-5 w-5 animate-spin mr-2" />计算账户中…</div>
        ) : !res || res.warning && !res.equity?.length ? (
          <div className="flex-1 flex items-center justify-center text-sm text-amber-300">{res?.warning || '无数据'}</div>
        ) : (
          <div className="flex-1 overflow-auto fin-scrollbar p-4 space-y-4">
            {/* 顶部大数 */}
            <div className="grid grid-cols-4 gap-3">
              <div className="rounded-lg border fin-divider px-3 py-2">
                <div className="text-[10px] fin-text-tertiary">期末净值</div>
                <div className={`text-lg font-bold font-mono ${colorPct(res.returnPct)}`}>{Math.round(res.finalEquity).toLocaleString()}<span className="text-xs">元</span></div>
                <div className={`text-[11px] ${colorPct(res.returnPct)}`}>{fmt(res.returnPct, true)}</div>
              </div>
              <div className="rounded-lg border fin-divider px-3 py-2">
                <div className="text-[10px] fin-text-tertiary">最大回撤</div>
                <div className="text-lg font-bold font-mono text-emerald-300">-{res.maxDrawdown.toFixed(2)}%</div>
              </div>
              <div className="rounded-lg border fin-divider px-3 py-2 ring-1 ring-amber-500/25">
                <div className="text-[10px] fin-text-tertiary" title="收益 − 同期等权全A，>0才算跑赢市场">超额(vs市场)</div>
                <div className={`text-lg font-bold font-mono ${colorPct(res.excess)}`}>{fmt(res.excess, true)}</div>
                <div className="text-[11px] fin-text-tertiary">市场{fmt(res.benchmark, true)}</div>
              </div>
              <div className="rounded-lg border fin-divider px-3 py-2">
                <div className="text-[10px] fin-text-tertiary">当前持仓 / 现金</div>
                <div className="text-lg font-bold font-mono fin-text-primary">{res.holdings?.length || 0}只</div>
                <div className="text-[11px] fin-text-tertiary">现金 {Math.round(res.cash).toLocaleString()}</div>
              </div>
            </div>

            {/* 计分卡 */}
            <div className="flex items-center gap-4 text-xs flex-wrap px-1">
              <span className="fin-text-secondary">已平仓 <b className="fin-text-primary">{res.closedTrades}</b>笔</span>
              <span className="fin-text-secondary">胜率 <b className={res.winRate >= 50 ? 'text-rose-300' : ''}>{res.winRate.toFixed(1)}%</b></span>
              <span className="fin-text-secondary">期望/笔 <b className={colorPct(res.expectancy)}>{fmt(res.expectancy, true)}</b></span>
              <span className="fin-text-secondary">赔率 <b>{res.payoffRatio.toFixed(2)}</b></span>
              <span className="fin-text-secondary">盈利因子 <b className={res.profitFactor >= 1 ? 'text-rose-300' : 'text-emerald-300'}>{res.profitFactor.toFixed(2)}</b></span>
              <span className="fin-text-secondary">持有 <b>{res.avgHoldDays.toFixed(1)}</b>天</span>
              <span className="ml-auto fin-text-tertiary">{res.startDate} ~ {res.endDate}</span>
            </div>

            {/* 净值曲线 */}
            <div className="rounded-lg border fin-divider px-2 pt-2 pb-1">
              <div className="text-[11px] fin-text-tertiary px-1 mb-1">净值曲线（虚线={mode === 'paper' ? '投入本金线' : '10万本金线'}）</div>
              <EquityCurve pts={res.equity} capital={res.capital} />
            </div>

            {/* 一句话结论 */}
            <div className={`text-[12px] px-3 py-2 rounded-lg ${res.excess >= 0 ? 'bg-rose-500/10 text-rose-200' : 'bg-emerald-500/10 text-emerald-200'}`}>
              {res.excess >= 0
                ? `这套扣成本后跑赢市场 ${fmt(res.excess, true)}，真有 alpha。`
                : `绝对${res.returnPct >= 0 ? '赚' : '亏'} ${fmt(res.returnPct, true)}，但跑输市场 ${fmt(res.excess, true)}——只是 beta（甚至不如躺着买指数），别被"赚钱"迷惑。`}
            </div>

            <div className="grid grid-cols-2 gap-4">
              {/* 当前持仓 */}
              <div>
                <div className="text-xs font-semibold fin-text-primary mb-1">当前持仓（{res.holdings?.length || 0}）<span className="text-[10px] font-normal fin-text-tertiary">· 现价=实时(同模拟持仓源)</span></div>
                <table className="w-full">
                  <thead><tr className="border-b fin-divider-soft"><th className={th}>股票</th><th className={`${th} text-right`}>成本/现价</th><th className={`${th} text-right`}>持有</th><th className={`${th} text-right`}>浮盈</th></tr></thead>
                  <tbody>
                    {(res.holdings || []).map(h => (
                      <tr key={h.symbol} className="border-b fin-divider-soft">
                        <td className={td}><div className="fin-text-primary">{h.name}</div><div className="text-[10px] font-mono fin-text-tertiary">{h.symbol}</div></td>
                        <td className={`${td} text-right font-mono fin-text-secondary`}>{h.entryPrice}/{h.currentPrice}</td>
                        <td className={`${td} text-right fin-text-tertiary`}>{h.holdDays}天</td>
                        <td className={`${td} text-right font-mono font-semibold ${colorPct(h.unrealizedPct)}`}>{fmt(h.unrealizedPct, true)}</td>
                      </tr>
                    ))}
                    {(!res.holdings || res.holdings.length === 0) && <tr><td className={`${td} fin-text-tertiary`} colSpan={4}>空仓</td></tr>}
                  </tbody>
                </table>
              </div>
              {/* 平仓记录 */}
              <div>
                <div className="text-xs font-semibold fin-text-primary mb-1">平仓记录（最近）</div>
                <table className="w-full">
                  <thead><tr className="border-b fin-divider-soft"><th className={th}>股票</th><th className={`${th} text-right`}>收益</th><th className={`${th} text-right`}>持有</th><th className={`${th} text-right`}>离场</th></tr></thead>
                  <tbody>
                    {(res.trades || []).slice(0, 30).map((t, i) => (
                      <tr key={`${t.symbol}-${i}`} className="border-b fin-divider-soft">
                        <td className={td}><div className="fin-text-primary">{t.name || t.symbol}</div><div className="text-[10px] fin-text-tertiary">{t.exitDate}</div></td>
                        <td className={`${td} text-right font-mono font-semibold ${colorPct(t.returnPct)}`}>{fmt(t.returnPct, true)}</td>
                        <td className={`${td} text-right fin-text-tertiary`}>{t.holdDays}天</td>
                        <td className={`${td} text-right`}><span className="text-[10px] px-1 py-0.5 rounded bg-slate-500/15 fin-text-tertiary">{EXIT_REASON_CN(t.exitReason)}</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default StrategyAccountDialog;
