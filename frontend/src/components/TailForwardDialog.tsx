import React, { useEffect, useState } from 'react';
import { Radar, X, Loader2, Plus, Check } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import {
  runTailForwardScanAll, getTailForwardConfig, setTailForwardConfig,
  type TailForwardResult, type TailForwardConfig,
} from '../services/tailForwardService';
import { addPaperPosition } from '../services/paperService';

interface Props { isOpen: boolean; onClose: () => void; }

export const TailForwardDialog: React.FC<Props> = ({ isOpen, onClose }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [cfg, setCfg] = useState<TailForwardConfig>({ enabled: false, auto: false });
  const [loading, setLoading] = useState(false);
  const [res, setRes] = useState<TailForwardResult | null>(null);
  const [added, setAdded] = useState<Record<string, boolean>>({});

  useEffect(() => { if (isOpen) void getTailForwardConfig().then(setCfg); }, [isOpen]);
  if (!isOpen) return null;

  const saveCfg = async (next: TailForwardConfig) => { setCfg(next); await setTailForwardConfig(next.enabled, next.auto); };
  const run = async () => {
    setLoading(true); setAdded({});
    try { setRes(await runTailForwardScanAll(cfg.auto)); } finally { setLoading(false); }
  };
  const addOne = async (symbol: string, name: string, price: number, source: string) => {
    await addPaperPosition(symbol, name, source || 'manual', price, 1000);
    setAdded(p => ({ ...p, [source + symbol]: true }));
  };

  const th = `px-2 py-1.5 text-left font-medium text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const td = 'px-2 py-2 text-xs';

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[760px] max-w-[94vw] h-[640px] max-h-[88vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Radar className="h-5 w-5 text-cyan-400" />
            <div>
              <div className="text-sm font-semibold fin-text-primary">尾盘 2:30 多策略自动选股</div>
              <div className="text-[11px] fin-text-tertiary">实时盘口判"封死涨停买不进"，只记可成交的票 · 用真实数据验证 2:30 入场到底行不行</div>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
        </div>

        {/* 控制区 */}
        <div className="px-5 py-3 border-b fin-divider flex items-center gap-4 flex-wrap">
          <label className="flex items-center gap-1.5 text-xs fin-text-secondary cursor-pointer">
            <input type="checkbox" checked={cfg.enabled} onChange={e => saveCfg({ ...cfg, enabled: e.target.checked })} />
            交易日 14:30 定时自动扫
          </label>
          <div className="flex rounded-lg overflow-hidden border fin-divider text-xs">
            <button onClick={() => saveCfg({ ...cfg, auto: false })}
              className={`px-3 py-1 ${!cfg.auto ? 'bg-cyan-500/20 text-cyan-200' : 'fin-text-secondary hover:bg-white/5'}`}>出清单·我确认</button>
            <button onClick={() => saveCfg({ ...cfg, auto: true })}
              className={`px-3 py-1 ${cfg.auto ? 'bg-cyan-500/20 text-cyan-200' : 'fin-text-secondary hover:bg-white/5'}`}>自动记入</button>
          </div>
          <button onClick={run} disabled={loading}
            className="ml-auto flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-cyan-500/15 border border-cyan-400/40 text-cyan-200 hover:bg-cyan-500/25 text-xs font-medium disabled:opacity-50">
            {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Radar className="h-3.5 w-3.5" />}
            立即跑一次
          </button>
        </div>

        <div className="flex-1 overflow-auto fin-scrollbar p-4">
          {!res ? (
            <div className="text-xs fin-text-tertiary text-center py-10">
              点「立即跑一次」用当前实时行情把 9 个技术策略各扫一遍(各取Top3),判断每只能否买进。<br />
              开「定时自动扫」后,交易日 14:30 会自动跑;「自动记入」则直接把可买的票计入模拟持仓。
            </div>
          ) : (
            <>
              {res.warning && <div className="text-[11px] text-amber-300 mb-2">{res.warning}</div>}
              <div className="flex items-center gap-4 text-xs mb-3 px-1">
                <span className="fin-text-tertiary">{res.asOf}</span>
                <span className="fin-text-secondary">可买 <b className="text-rose-300">{res.buyableCount}</b></span>
                <span className="fin-text-secondary">封死买不进 <b className="text-emerald-300">{res.sealedCount}</b></span>
                {res.auto && <span className="fin-text-secondary">已自动记入 <b className="text-cyan-300">{res.addedCount}</b></span>}
                <span className="ml-auto text-[10px] fin-text-tertiary">{res.auto ? '自动记入模式' : '清单模式·手动确认'}</span>
              </div>
              <table className="w-full">
                <thead><tr className="border-b fin-divider-soft">
                  <th className={th}>股票</th><th className={th}>策略</th><th className={`${th} text-right`}>现价</th><th className={`${th} text-right`}>涨幅</th>
                  <th className={`${th} text-center`}>可成交</th><th className={`${th} text-right`}>操作</th>
                </tr></thead>
                <tbody>
                  {res.candidates.map(c => (
                    <tr key={(c.source || '') + c.symbol} className="border-b fin-divider-soft">
                      <td className={td}><div className="fin-text-primary">{c.name}</div><div className="text-[10px] font-mono fin-text-tertiary">{c.symbol}</div></td>
                      <td className={`${td} text-[10px] fin-text-secondary`}>{c.sourceLabel || c.source}</td>
                      <td className={`${td} text-right font-mono fin-text-secondary`}>{c.price}</td>
                      <td className={`${td} text-right font-mono ${c.changePct >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{c.changePct >= 0 ? '+' : ''}{c.changePct}%</td>
                      <td className={`${td} text-center`}>
                        {c.buyable
                          ? <span className="text-[10px] px-1.5 py-0.5 rounded bg-rose-500/15 text-rose-300">可买</span>
                          : <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300" title={c.reason}>买不进</span>}
                      </td>
                      <td className={`${td} text-right`}>
                        {c.alreadyHeld ? <span className="text-[10px] fin-text-tertiary">已持仓</span>
                          : c.added || added[(c.source || '') + c.symbol] ? <span className="inline-flex items-center gap-0.5 text-[10px] text-cyan-300"><Check className="h-3 w-3" />已记入</span>
                          : c.buyable ? <button onClick={() => addOne(c.symbol, c.name, c.price, c.source || 'manual')}
                              className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded bg-cyan-500/15 text-cyan-200 hover:bg-cyan-500/25"><Plus className="h-3 w-3" />加入</button>
                          : <span className="text-[10px] fin-text-tertiary">—</span>}
                      </td>
                    </tr>
                  ))}
                  {res.candidates.length === 0 && <tr><td className={`${td} fin-text-tertiary`} colSpan={6}>当前无策略信号</td></tr>}
                </tbody>
              </table>
              <div className="mt-3 text-[10px] fin-text-tertiary px-1 leading-relaxed">
                注：「买不进」= 当前封死涨停、盘口无卖盘,记进去等于自欺;只有「可买」的才记入。买入后由风控引擎(短线稳健:−8%止损/保本/移动止损/+25%止盈)自动平仓。
                提醒:回测已证伪这些短线策略没稳定 alpha —— 这里是往后攒"真实可成交"样本来验证,不是说它们能赚。
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default TailForwardDialog;
