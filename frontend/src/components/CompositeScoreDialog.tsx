import React, { useEffect, useState } from 'react';
import { Award, X, Loader2, RefreshCw, ShieldAlert, ShieldCheck, FlaskConical } from 'lucide-react';
import { useTheme } from '../contexts/ThemeContext';
import {
  runCompositeScore, getCompositeValidation,
  type CompositeScoreResult, type CompositeValidationResult,
} from '../services/compositeService';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onOpenStock?: (symbol: string, name: string, price: number) => void;
}

const PRESETS = [
  { key: 'value', label: '长线价值' },
  { key: 'boom', label: '中线景气' },
];

// 三维分数条:质量50/结构30/催化20
const ScoreBar: React.FC<{ v: number; max: number; cls: string }> = ({ v, max, cls }) => (
  <div className="flex items-center gap-1.5">
    <div className="h-1.5 w-14 rounded bg-white/10 overflow-hidden">
      <div className={`h-full rounded ${cls}`} style={{ width: `${Math.min(100, (v / max) * 100)}%` }} />
    </div>
    <span className="font-mono text-[11px] fin-text-secondary w-8 text-right">{v}</span>
  </div>
);

export const CompositeScoreDialog: React.FC<Props> = ({ isOpen, onClose, onOpenStock }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [preset, setPreset] = useState('value');
  const [tab, setTab] = useState<'rank' | 'verify'>('rank');
  const [loading, setLoading] = useState(false);
  const [res, setRes] = useState<CompositeScoreResult | null>(null);
  const [verify, setVerify] = useState<CompositeValidationResult | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [error, setError] = useState('');

  const run = async (p: string) => {
    setLoading(true); setError('');
    try {
      setRes(await runCompositeScore(p));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  const loadVerify = async () => {
    try { setVerify(await getCompositeValidation()); } catch (e) { setError((e as Error).message); }
  };

  useEffect(() => {
    if (isOpen) { void run(preset); void loadVerify(); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  if (!isOpen) return null;

  const rows = res?.rows ?? [];
  const vetoed = res?.vetoedRows ?? [];
  const th = `px-2 py-1.5 font-medium text-[11px] ${dark ? 'text-slate-400' : 'text-slate-500'}`;
  const excessCls = (v: number) => (v >= 0 ? 'text-rose-300' : 'text-emerald-300');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1080px] max-w-[95vw] h-[760px] max-h-[92vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Award className="h-5 w-5 text-amber-400" />
            <div>
              <div className="text-sm font-semibold fin-text-primary">综合评分选股 · 否决做门 / 评分排序 / 状态门管执行</div>
              <div className="text-[11px] fin-text-tertiary">质量0-50(基本面) + 结构0-30(诚实面板事实) + 催化0-20(涨停/龙虎榜/竞价);评分只回答买什么,不回答何时买</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex rounded-lg overflow-hidden border fin-divider text-xs">
              {PRESETS.map(p => (
                <button key={p.key} onClick={() => { setPreset(p.key); void run(p.key); }}
                  className={`px-3 py-1.5 ${preset === p.key ? 'bg-amber-500/20 text-amber-200' : 'fin-text-secondary hover:bg-white/5'}`}>{p.label}</button>
              ))}
            </div>
            <div className="flex rounded-lg overflow-hidden border fin-divider text-xs">
              <button onClick={() => setTab('rank')} className={`px-3 py-1.5 ${tab === 'rank' ? 'bg-white/10 fin-text-primary' : 'fin-text-secondary hover:bg-white/5'}`}>评分榜</button>
              <button onClick={() => { setTab('verify'); void loadVerify(); }} className={`px-3 py-1.5 flex items-center gap-1 ${tab === 'verify' ? 'bg-white/10 fin-text-primary' : 'fin-text-secondary hover:bg-white/5'}`}>
                <FlaskConical className="h-3 w-3" />验证报告
              </button>
            </div>
            <button onClick={() => void run(preset)} className="p-1.5 rounded fin-hover" title="重新评分(自动落当日快照)">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 fin-text-secondary" />}
            </button>
            <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
          </div>
        </div>

        {loading && !res ? (
          <div className="flex-1 flex items-center justify-center text-sm fin-text-tertiary">
            <Loader2 className="h-5 w-5 animate-spin mr-2" />评分中…(逐股拉取K线计算结构事实,约需十几秒)
          </div>
        ) : tab === 'rank' ? (
          <div className="flex-1 overflow-auto fin-scrollbar p-4">
            <div className="flex items-center gap-4 text-xs mb-2 px-1 flex-wrap">
              <span className="fin-text-secondary">评分日 <b className="fin-text-primary">{res?.runDate || '—'}</b></span>
              <span className="fin-text-secondary">基本面通过 {res?.universeCount || 0}</span>
              <span className="fin-text-secondary">入榜 <b className="text-amber-300">{rows.length}</b> · 技术否决 {vetoed.length}</span>
              {res?.snapshotSaved && <span className="text-[11px] text-emerald-300">快照已落库(供30/60日验证)</span>}
              {(error || res?.warning) && <span className="text-[11px] text-amber-300">{error || res?.warning}</span>}
            </div>
            <div className="text-[11px] fin-text-tertiary mb-3 px-1 leading-relaxed">{res?.rulesText}</div>
            <table className="w-full">
              <thead><tr className="border-b fin-divider-soft">
                <th className={`${th} text-left`}>#</th>
                <th className={`${th} text-left`}>股票</th>
                <th className={`${th} text-right`}>现价</th>
                <th className={`${th} text-right`}>总分</th>
                <th className={`${th} text-left`}>质量/50</th>
                <th className={`${th} text-left`}>结构/30</th>
                <th className={`${th} text-left`}>催化/20</th>
                <th className={`${th} text-left`}>状态门</th>
              </tr></thead>
              <tbody>
                {rows.map((r, i) => (
                  <React.Fragment key={r.symbol}>
                    <tr className="border-b fin-divider-soft hover:bg-white/5 cursor-pointer"
                      onClick={() => setExpanded(expanded === r.symbol ? null : r.symbol)}>
                      <td className="px-2 py-1.5 text-xs fin-text-tertiary">{i + 1}</td>
                      <td className="px-2 py-1.5 text-xs">
                        <button
                          className="text-left hover:underline"
                          onClick={(e) => { e.stopPropagation(); onOpenStock?.(r.symbol, r.name, r.price); }}
                          title="打开个股图表"
                        >
                          <div className="fin-text-primary">{r.name}</div>
                          <div className="text-[10px] font-mono fin-text-tertiary">{r.symbol} · {r.marketCapYi}亿</div>
                        </button>
                      </td>
                      <td className="px-2 py-1.5 text-xs text-right font-mono fin-text-secondary">{r.price}</td>
                      <td className="px-2 py-1.5 text-right"><span className="text-sm font-bold text-amber-300 font-mono">{r.total}</span></td>
                      <td className="px-2 py-1.5"><ScoreBar v={r.quality} max={50} cls="bg-indigo-400" /></td>
                      <td className="px-2 py-1.5"><ScoreBar v={r.structure} max={30} cls="bg-cyan-400" /></td>
                      <td className="px-2 py-1.5"><ScoreBar v={r.catalyst} max={20} cls="bg-rose-400" /></td>
                      <td className="px-2 py-1.5">
                        {r.gateOk ? (
                          <span className="inline-flex items-center gap-1 text-[11px] text-emerald-300"><ShieldCheck className="h-3.5 w-3.5" />可执行</span>
                        ) : (
                          <span className="inline-flex items-center gap-1 text-[11px] text-rose-300" title={(r.gateReasons ?? []).join(';')}>
                            <ShieldAlert className="h-3.5 w-3.5" />{(r.gateReasons ?? []).join('/')}
                          </span>
                        )}
                      </td>
                    </tr>
                    {expanded === r.symbol && (
                      <tr className="border-b fin-divider-soft">
                        <td colSpan={8} className="px-4 py-2 text-[11px] leading-relaxed bg-white/[0.03]">
                          <div><span className="text-indigo-300 font-semibold">质量:</span> <span className="fin-text-secondary">{(r.qualityFacts ?? []).join(' · ') || '—'}</span></div>
                          <div><span className="text-cyan-300 font-semibold">结构:</span> <span className="fin-text-secondary">{(r.structFacts ?? []).join(' · ') || '—'}</span></div>
                          <div><span className="text-rose-300 font-semibold">催化:</span> <span className="fin-text-secondary">{(r.catalystFacts ?? []).join(' · ') || '无(不扣分,只加分)'}</span></div>
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                ))}
                {rows.length === 0 && (
                  <tr><td colSpan={8} className="px-2 py-8 text-center text-xs fin-text-tertiary">无入榜股票(基本面初筛先要通过;可切换预设或刷新财务数据)</td></tr>
                )}
              </tbody>
            </table>
            {vetoed.length > 0 && (
              <div className="mt-4">
                <div className="text-xs font-semibold fin-text-secondary mb-1.5 px-1">被否决(基本面已过,技术一票否决)</div>
                <div className="space-y-1">
                  {vetoed.map(r => (
                    <div key={r.symbol} className="flex items-center gap-2 px-2 py-1 rounded bg-white/[0.03] text-[11px]">
                      <span className="fin-text-primary">{r.name}</span>
                      <span className="font-mono fin-text-tertiary">{r.symbol}</span>
                      <span className="text-rose-300">{(r.vetoReasons ?? []).join(';')}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : (
          <div className="flex-1 overflow-auto fin-scrollbar p-4">
            <div className="text-[11px] fin-text-tertiary mb-3 px-1 leading-relaxed">
              每次评分自动按交易日落 Top50 快照;这里对每个快照日的 Top10 等权组合,计算 30/60 个交易日后的收益。
              {verify?.costNote ? ` ${verify.costNote}。` : ''}
              验证的意义:权重没有资格被优化,只有资格被验证——若某维度与超额无关,做减法删掉它。
            </div>
            {verify?.warning && <div className="text-xs text-amber-300 px-1 mb-2">{verify.warning}</div>}
            <table className="w-full">
              <thead><tr className="border-b fin-divider-soft">
                <th className={`${th} text-left`}>快照日</th>
                <th className={`${th} text-right`}>持有(交易日)</th>
                <th className={`${th} text-right`}>股数</th>
                <th className={`${th} text-right`}>组合收益(扣费)</th>
                <th className={`${th} text-right`}>全市场等权基准</th>
                <th className={`${th} text-right`}>超额</th>
                <th className={`${th} text-left`}>状态</th>
              </tr></thead>
              <tbody>
                {(verify?.rows ?? []).map((v, i) => (
                  <tr key={`${v.runDate}-${v.horizonDays}-${i}`} className="border-b fin-divider-soft">
                    <td className="px-2 py-1.5 text-xs font-mono fin-text-secondary">{v.runDate}</td>
                    <td className="px-2 py-1.5 text-xs text-right font-mono fin-text-secondary">{v.horizonDays}</td>
                    <td className="px-2 py-1.5 text-xs text-right font-mono fin-text-secondary">{v.matured ? v.n : '—'}</td>
                    <td className={`px-2 py-1.5 text-xs text-right font-mono ${v.matured ? excessCls(v.portRet) : 'fin-text-tertiary'}`}>{v.matured ? `${v.portRet >= 0 ? '+' : ''}${v.portRet}%` : '—'}</td>
                    <td className={`px-2 py-1.5 text-xs text-right font-mono ${v.matured ? excessCls(v.benchRet) : 'fin-text-tertiary'}`}>{v.matured ? `${v.benchRet >= 0 ? '+' : ''}${v.benchRet}%` : '—'}</td>
                    <td className={`px-2 py-1.5 text-xs text-right font-mono font-bold ${v.matured ? excessCls(v.excess) : 'fin-text-tertiary'}`}>{v.matured ? `${v.excess >= 0 ? '+' : ''}${v.excess}%` : '—'}</td>
                    <td className="px-2 py-1.5 text-[11px]">{v.matured
                      ? <span className="text-emerald-300">已成熟</span>
                      : <span className="fin-text-tertiary">待成熟({v.daysElapsed}/{v.horizonDays})</span>}</td>
                  </tr>
                ))}
                {(verify?.rows ?? []).length === 0 && (
                  <tr><td colSpan={7} className="px-2 py-8 text-center text-xs fin-text-tertiary">暂无快照;运行一次评分即自动落库,30/60个交易日后回来看超额</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
};

export default CompositeScoreDialog;
