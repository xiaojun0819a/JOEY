import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AlertTriangle, Gauge, Loader2, Pause, Play, RotateCcw, X, Zap } from 'lucide-react';
import {
  getDrillSession,
  getTape,
  loadDrillRecords,
  saveDrillRecord,
  type DrillPick,
  type DrillRecord,
  type DrillSession,
  type TapeTick,
} from '../services/drillService';

// 选股体感练习(2026-07-25 用户提):只看某日 14:50 真实选出的那批票 → 自己盲选一只 →
// 次日真实分时逐笔回放 → 自己按下卖出 → 与"持到收盘/当日最高/开盘就卖"对照。
// 反作弊纪律:①题面来自 GetDrillSession,后端就不发次日价格;②回放的纵轴只按**已揭示**的数据缩放,
// 提前把全天最高点画进坐标轴等于泄题;③次日一字/涨停开盘的票标"买不进",不计入成绩。

interface Props {
  isOpen: boolean;
  onClose: () => void;
  strategyId: string;
  strategyName: string;
  // 点股票名开全屏四图看形态。必须把信号日传出去,由 App 把K线截断在那一天——
  // 否则右边多出来的几根日K直接把答案写在脸上。
  onOpenStock?: (symbol: string, name: string, price: number, asOf: string) => void;
}

const COST_PCT = 0.2; // 往返成本,与学习库 oversoldCostPct 同口径

const fmt = (v: number, digits = 2, plus = false): string => {
  if (!Number.isFinite(v)) return '--';
  return `${plus && v > 0 ? '+' : ''}${v.toFixed(digits)}`;
};

const pctClass = (v: number): string => (v > 0 ? 'text-rose-400' : v < 0 ? 'text-emerald-400' : 'fin-text-secondary');

// 涨停幅度:科创/创业 20cm、北交所 30cm、ST 5cm、其余 10cm
const limitPctOf = (symbol: string, name: string): number => {
  const s = symbol.toLowerCase();
  const digits = s.replace(/^(sh|sz|bj)/, '');
  if (/st/i.test(name)) return 5;
  if (s.startsWith('bj') || digits.startsWith('8') || digits.startsWith('4')) return 30;
  if (digits.startsWith('688') || digits.startsWith('689') || digits.startsWith('300') || digits.startsWith('301')) return 20;
  return 10;
};

// 回放速度:step=每拍推进几个 tick,ms=每拍间隔。
// 2026-07-25 用户调档:慢档再慢 30%、中档再慢 10%、快档不变。
// 推进步长要保持整数(下标不能走小数),所以减速做在间隔上:60ms ÷ 0.7 ≈ 86、÷ 0.9 ≈ 67。
const SPEEDS: { label: string; step: number; ms: number }[] = [
  { label: '慢', step: 1, ms: 86 },
  { label: '中', step: 3, ms: 67 },
  { label: '快', step: 8, ms: 60 },
];

export const FeelDrillDialog: React.FC<Props> = ({ isOpen, onClose, strategyId, strategyName, onOpenStock }) => {
  const [session, setSession] = useState<DrillSession | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [signalDate, setSignalDate] = useState('');

  const [phase, setPhase] = useState<'picking' | 'trading' | 'result'>('picking');
  const [chosen, setChosen] = useState<DrillPick | null>(null);
  const [ticks, setTicks] = useState<TapeTick[]>([]);
  const [idx, setIdx] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speedIdx, setSpeedIdx] = useState(0); // SPEEDS 下标(0=慢),存下标而非步长,免得步长撞车
  const [sellIdx, setSellIdx] = useState<number | null>(null);
  const [records, setRecords] = useState<DrillRecord[]>([]);
  const timerRef = useRef<number | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const dragStartRef = useRef<number | null>(null); // 按下时的横坐标,用来判断"点击"还是"拖动"
  const [dragging, setDragging] = useState(false);
  const [loadingSym, setLoadingSym] = useState(''); // 正在取磁带的票:按钮转圈,避免"点了没反应"再点一下
  // 拖时间轴能往前拉 = 能提前看到后面的走势。允许(用户要的),但要如实记账:
  // 看过的最远位置若超过卖点,这一笔就是"偷看过",不进成绩本。
  const [maxSeen, setMaxSeen] = useState(0);

  // —— 题面 ——
  const loadSession = useCallback(async (date: string) => {
    setLoading(true);
    setError('');
    try {
      const s = await getDrillSession(strategyId, date);
      if (s) {
        setSession(s);
        setSignalDate(s.signalDate);
      }
    } catch (e) {
      setError(String(e));
    }
    setLoading(false);
  }, [strategyId]);

  useEffect(() => {
    if (!isOpen) return;
    setPhase('picking');
    setChosen(null);
    setSellIdx(null);
    setRecords(loadDrillRecords());
    void loadSession('');
  }, [isOpen, loadSession]);

  // —— 回放钟摆 ——
  useEffect(() => {
    if (timerRef.current) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (!playing || phase !== 'trading' || ticks.length === 0) return;
    const sp = SPEEDS[speedIdx] || SPEEDS[0];
    timerRef.current = window.setInterval(() => {
      setIdx((prev) => {
        const next = prev + sp.step;
        const capped = next >= ticks.length - 1 ? ticks.length - 1 : next;
        if (next >= ticks.length - 1) setPlaying(false);
        setMaxSeen((m) => (capped > m ? capped : m));
        return capped;
      });
    }, sp.ms);
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current);
    };
  }, [playing, speedIdx, ticks.length, phase]);

  // 入场价 = 信号日 14:50 的入选价(与实盘一致:2:50 扫描当天就买入,次日只管卖)。
  // 若留痕没存价则退化为次日开盘价。
  const buyPrice = useMemo(() => {
    if (chosen && chosen.price > 0) return chosen.price;
    return ticks.length ? ticks[0].price : 0;
  }, [chosen, ticks]);
  const prevClose = useMemo(() => {
    if (!ticks.length) return 0;
    const t = ticks[0];
    return t.pct !== -100 ? t.price / (1 + t.pct / 100) : t.price;
  }, [ticks]);
  // 次日开盘相对成本的跳空:一字跌停这类伤害必须一眼看见
  const openGap = useMemo(() => {
    if (!ticks.length || !buyPrice) return 0;
    return (ticks[0].price / buyPrice - 1) * 100;
  }, [ticks, buyPrice]);

  const startTrading = async (pick: DrillPick) => {
    if (!session?.nextDate || loadingSym) return; // 已在取磁带就别重入
    setLoadingSym(pick.symbol);
    setLoading(true);
    setError('');
    try {
      const list = await getTape(pick.symbol, session.nextDate, pick.signalClose || pick.price);
      if (list.length < 20) {
        setError(`${pick.name} 在 ${session.nextDate} 两个磁带源都没有存档(自采集 2026-07-06 起 / 回补库按需补),换一只或换一天`);
        setLoading(false);
        setLoadingSym('');
        return;
      }
      setTicks(list);
      setChosen(pick);
      setIdx(0);
      setMaxSeen(0);
      setSellIdx(null);
      setPhase('trading');
      setPlaying(true);
    } catch (e) {
      setError(String(e));
    }
    setLoading(false);
    setLoadingSym('');
  };

  const doSell = useCallback((at: number) => {
    setPlaying(false);
    setSellIdx(at);
    setPhase('result');
  }, []);

  // X / 点遮罩:回放或结算中先退回选票页,不要一路退回策略弹窗——挑票的上下文很贵,
  // 用户常是"看一眼这只不合适,换一只",没必要重新开一遍弹窗选一遍日期。
  const backOrClose = useCallback(() => {
    if (phase === 'picking') {
      onClose();
      return;
    }
    setPlaying(false);
    setChosen(null);
    setSellIdx(null);
    setPhase('picking');
  }, [phase, onClose]);

  // 结算
  const settle = useMemo(() => {
    if (sellIdx === null || !ticks.length || !buyPrice) return null;
    const sell = ticks[sellIdx];
    const gross = (sell.price / buyPrice - 1) * 100;
    const closeT = ticks[ticks.length - 1];
    const holdClose = (closeT.price / buyPrice - 1) * 100 - COST_PCT;
    let maxP = ticks[0].price;
    for (const t of ticks) if (t.price > maxP) maxP = t.price;
    const maxPct = (maxP / buyPrice - 1) * 100 - COST_PCT;
    const below = ticks.filter((t) => t.price <= sell.price).length;
    return {
      sellTime: sell.time,
      sellPrice: sell.price,
      gross,
      net: gross - COST_PCT,
      holdClose,
      openSell: (ticks[0].price / buyPrice - 1) * 100 - COST_PCT,
      maxPct,
      percentile: (below / ticks.length) * 100,
    };
  }, [sellIdx, ticks, buyPrice]);

  // 拖时间轴看过卖点之后的走势 = 这一笔不是盲的,成绩不算数(留 2 格容差防误触)。
  const peeked = useMemo(() => sellIdx !== null && maxSeen > sellIdx + 2, [maxSeen, sellIdx]);

  // 信号日已封涨停 = 14:50 大概率买不进(用户定的铁律:所有策略都要模拟实际交易规则)。
  // 故意不在选票阶段提示——"这只已经涨停了买不进"本身就是该练出来的体感,选了才告诉你。
  const cantBuy = useMemo(() => {
    if (!chosen) return false;
    return chosen.changePct >= limitPctOf(chosen.symbol, chosen.name) - 0.3;
  }, [chosen]);

  // 一字跌停封死全天 = 想卖也卖不掉(挂单排不到),只能被动持有。
  // 判据:全天只有一个成交价 且 该价在跌停位——光看跌停不够,盘中打开过就还是能卖的。
  const sealedDown = useMemo(() => {
    if (!chosen || ticks.length < 2 || !prevClose) return false;
    const flat = ticks.every((t) => t.price === ticks[0].price);
    if (!flat) return false;
    const pct = (ticks[0].price / prevClose - 1) * 100;
    return pct <= -(limitPctOf(chosen.symbol, chosen.name) - 0.3);
  }, [chosen, ticks, prevClose]);

  // 成绩入账(每题一次)
  const savedRef = useRef<string>('');
  useEffect(() => {
    if (phase !== 'result' || !settle || !chosen || !session) return;
    const key = `${session.signalDate}|${chosen.symbol}|${settle.sellTime}`;
    if (savedRef.current === key || cantBuy || peeked) return; // 买不进 / 偷看过后续走势的,都不进成绩本
    savedRef.current = key;
    setRecords(
      saveDrillRecord({
        at: new Date().toISOString().slice(0, 19).replace('T', ' '),
        strategyId,
        signalDate: session.signalDate,
        symbol: chosen.symbol,
        name: chosen.name,
        netPct: settle.net,
        holdClosePct: settle.holdClose,
        maxPct: settle.maxPct,
        sellTime: settle.sellTime,
      })
    );
  }, [phase, settle, chosen, session, strategyId, cantBuy, peeked]);

  // 键盘:空格 播放/暂停,S 卖出
  useEffect(() => {
    if (!isOpen || phase !== 'trading') return;
    const onKey = (e: KeyboardEvent) => {
      if (e.code === 'Space') {
        e.preventDefault();
        setPlaying((p) => !p);
      } else if (e.key === 's' || e.key === 'S') {
        e.preventDefault();
        doSell(idx);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, phase, idx, doSell]);

  const stats = useMemo(() => {
    const mine = records.filter((r) => r.strategyId === strategyId);
    if (!mine.length) return null;
    const avg = mine.reduce((s, r) => s + r.netPct, 0) / mine.length;
    const beat = mine.filter((r) => r.netPct >= r.holdClosePct).length;
    const win = mine.filter((r) => r.netPct > 0).length;
    return { n: mine.length, avg, beat, win };
  }, [records, strategyId]);

  // ⚠️所有 Hook 必须写在 `if (!isOpen) return null` 之前:放后面会变成"关着时少调两个 Hook、
  // 打开时多调两个",React 直接抛错整页白掉(2026-07-25 就这么炸过一次)。
  // 屏幕横坐标 → tick 下标(拖时间轴用)。SVG 是 viewBox 缩放的,得先换算回 viewBox 坐标系。
  const idxFromClientX = useCallback((clientX: number): number => {
    const el = svgRef.current;
    if (!el || ticks.length < 2) return 0;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return 0;
    const vx = ((clientX - rect.left) / rect.width) * 1000; // 1000 = viewBox 宽
    const ratio = (vx - 46) / (1000 - 46 - 54); // padL / padR
    const i = Math.round(ratio * (ticks.length - 1));
    return Math.max(0, Math.min(ticks.length - 1, i));
  }, [ticks.length]);

  const scrubTo = useCallback((clientX: number) => {
    const i = idxFromClientX(clientX);
    setIdx(i);
    setMaxSeen((m) => (i > m ? i : m));
  }, [idxFromClientX]);

  if (!isOpen) return null;

  // ============ 分时图(纵轴只按已揭示数据缩放,不泄题) ============
  const renderTape = () => {
    const W = 1000;
    const H = 300;
    const padL = 46;
    const padR = 54;
    const padT = 8;
    const priceH = 218;
    const volTop = 240;
    const volH = 52;
    const shown = phase === 'result' ? ticks.length : Math.max(idx + 1, 1);
    const revealed = ticks.slice(0, shown);
    if (!revealed.length || !prevClose) return null;

    let maxAbs = 1.2;
    for (const t of revealed) {
      const p = Math.abs((t.price / prevClose - 1) * 100);
      if (p > maxAbs) maxAbs = p;
    }
    const half = Math.ceil(maxAbs * 1.15 * 2) / 2;
    const x = (i: number) => padL + (i / Math.max(ticks.length - 1, 1)) * (W - padL - padR);
    const yOf = (price: number) => {
      const p = (price / prevClose - 1) * 100;
      return padT + ((half - p) / (2 * half)) * priceH;
    };

    const line = revealed.map((t, i) => `${x(i).toFixed(1)},${yOf(t.price).toFixed(1)}`).join(' ');
    const vwapLine = revealed
      .map((t, i) => {
        const vw = t.volume > 0 ? t.amount / (t.volume * 100) : t.price;
        return `${x(i).toFixed(1)},${yOf(vw).toFixed(1)}`;
      })
      .join(' ');

    // 增量成交量(累计差分)
    let maxVol = 1;
    const vols = revealed.map((t, i) => {
      const v = i === 0 ? t.volume : Math.max(t.volume - revealed[i - 1].volume, 0);
      if (v > maxVol) maxVol = v;
      return v;
    });

    const cur = revealed[revealed.length - 1];
    const curY = yOf(cur.price);
    const gridPcts = [half, half / 2, 0, -half / 2, -half];

    // 时间刻度
    const marks: { i: number; label: string }[] = [];
    ['10:30', '11:30', '14:00'].forEach((hm) => {
      const i = ticks.findIndex((t) => t.time >= hm);
      if (i > 0) marks.push({ i, label: hm });
    });

    return (
      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        className="w-full select-none"
        style={{ height: 300, cursor: phase === 'trading' ? 'ew-resize' : 'default', touchAction: 'none' }}
        // 按下只是"预备",不暂停也不跳——横向移动超过阈值才算真拖动。
        // 否则图上误点一下就会暂停+把时间轴跳过去:点「买它」后等磁带那一秒里再点一下,
        // 正好落在刚渲染出来的图上,回放就莫名其妙停在开盘没多久的位置(2026-07-25 实测症状)。
        onPointerDown={phase === 'trading' ? (e) => {
          (e.currentTarget as SVGSVGElement).setPointerCapture(e.pointerId);
          dragStartRef.current = e.clientX;
        } : undefined}
        onPointerMove={phase === 'trading' ? (e) => {
          if (dragStartRef.current === null) return;
          if (!dragging) {
            if (Math.abs(e.clientX - dragStartRef.current) < 5) return; // 手抖级位移不算拖
            setPlaying(false);
            setDragging(true);
          }
          scrubTo(e.clientX);
        } : undefined}
        onPointerUp={phase === 'trading' ? (e) => {
          dragStartRef.current = null;
          setDragging(false);
          try { (e.currentTarget as SVGSVGElement).releasePointerCapture(e.pointerId); } catch { /* 已释放 */ }
        } : undefined}
        onPointerCancel={phase === 'trading' ? () => { dragStartRef.current = null; setDragging(false); } : undefined}
      >
        {gridPcts.map((p) => {
          const y = padT + ((half - p) / (2 * half)) * priceH;
          const isMid = p === 0;
          return (
            <g key={p}>
              <line
                x1={padL} y1={y} x2={W - padR} y2={y}
                stroke={isMid ? 'rgba(244,63,94,.55)' : 'rgba(140,140,140,.18)'}
                strokeWidth={1}
                strokeDasharray={isMid ? '5,4' : undefined}
              />
              <text x={padL - 6} y={y + 3.5} textAnchor="end" fontSize={10} fill="rgba(200,200,200,.6)">
                {(prevClose * (1 + p / 100)).toFixed(2)}
              </text>
              <text x={W - padR + 6} y={y + 3.5} fontSize={10} fill={p > 0 ? 'rgba(244,63,94,.75)' : p < 0 ? 'rgba(16,185,129,.75)' : 'rgba(200,200,200,.6)'}>
                {p > 0 ? '+' : ''}{p.toFixed(1)}%
              </text>
            </g>
          );
        })}
        {marks.map((m) => (
          <g key={m.label}>
            <line x1={x(m.i)} y1={padT} x2={x(m.i)} y2={padT + priceH} stroke="rgba(140,140,140,.16)" strokeWidth={1} />
            <text x={x(m.i)} y={padT + priceH + 12} textAnchor="middle" fontSize={9} fill="rgba(160,160,160,.6)">{m.label}</text>
          </g>
        ))}
        <text x={padL} y={padT + priceH + 12} fontSize={9} fill="rgba(160,160,160,.6)">09:30</text>
        <text x={W - padR} y={padT + priceH + 12} textAnchor="end" fontSize={9} fill="rgba(160,160,160,.6)">15:00</text>

        {vols.map((v, i) => {
          const h = (v / maxVol) * volH;
          const up = revealed[i].price >= (i === 0 ? prevClose : revealed[i - 1].price);
          return (
            <rect
              key={i}
              x={x(i)} y={volTop + volH - h}
              width={Math.max((W - padL - padR) / Math.max(ticks.length, 1), 0.7)}
              height={h}
              fill={up ? 'rgba(244,63,94,.5)' : 'rgba(16,185,129,.5)'}
            />
          );
        })}

        <polyline points={vwapLine} fill="none" stroke="rgba(250,204,21,.85)" strokeWidth={1.2} />
        <polyline points={line} fill="none" stroke="#e5e7eb" strokeWidth={1.5} />

        {/* 成本线:信号日 14:50 的入选价,卖点决策全看它 */}
        <line x1={padL} y1={yOf(buyPrice)} x2={W - padR} y2={yOf(buyPrice)} stroke="rgba(56,189,248,.75)" strokeWidth={1.1} strokeDasharray="6,3" />
        <text x={padL + 4} y={yOf(buyPrice) - 5} fontSize={10} fill="#38bdf8">成本 {buyPrice.toFixed(2)}</text>

        {/* 卖出点 */}
        {sellIdx !== null && (
          <>
            <line x1={x(sellIdx)} y1={padT} x2={x(sellIdx)} y2={padT + priceH} stroke="rgba(250,204,21,.7)" strokeDasharray="4,3" />
            <circle cx={x(sellIdx)} cy={yOf(ticks[sellIdx].price)} r={4} fill="#facc15" />
            <text x={x(sellIdx) + 6} y={yOf(ticks[sellIdx].price) + 14} fontSize={10} fill="#facc15">
              卖 {ticks[sellIdx].price.toFixed(2)}
            </text>
          </>
        )}

        {phase === 'trading' && (
          <>
            {/* 时间游标:可左右拖动(拖粗一点+抓手,好点中) */}
            <line
              x1={x(shown - 1)} y1={padT} x2={x(shown - 1)} y2={volTop + volH}
              stroke={dragging ? 'rgba(56,189,248,.85)' : 'rgba(255,255,255,.35)'}
              strokeWidth={dragging ? 1.6 : 1}
            />
            <circle cx={x(shown - 1)} cy={curY} r={dragging ? 4.5 : 3} fill={dragging ? '#38bdf8' : '#fff'} />
            <rect x={x(shown - 1) - 5} y={padT} width={10} height={priceH} fill="transparent" style={{ cursor: 'ew-resize' }} />
            <text x={Math.min(x(shown - 1) + 6, W - padR - 34)} y={padT + 10} fontSize={10} fill="rgba(200,220,255,.85)">
              {cur.time.slice(0, 5)}
            </text>
          </>
        )}
      </svg>
    );
  };

  const cur = ticks.length ? ticks[Math.min(idx, ticks.length - 1)] : null;
  const floating = cur && buyPrice ? (cur.price / buyPrice - 1) * 100 : 0;

  return (
    <div className="fixed inset-0 z-[96] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={backOrClose} />
      <div className="relative flex h-[840px] max-h-[94vh] w-[1180px] max-w-[96vw] flex-col overflow-hidden rounded-xl border fin-divider fin-panel shadow-2xl">
        <div className="flex items-center border-b fin-divider px-5 py-3">
          <Gauge className="h-5 w-5 text-sky-300 shrink-0" />
          <div className="flex-1 text-center px-2">
            <div className="text-sm font-semibold fin-text-primary">{strategyName} · 选股体感练习</div>
            <div className="text-[11px] fin-text-tertiary">
              只给信号日证据,你盲选一只 → 次日真实分时逐笔回放 → 你自己按卖出 → 和"持到收盘/当日最高"对照
            </div>
          </div>
          <button
            onClick={backOrClose}
            className="rounded-lg p-2 fin-hover shrink-0"
            title={phase === 'picking' ? '关闭练习' : '返回选票(不会退回策略弹窗)'}
          >
            <X className="h-4 w-4 fin-text-secondary" />
          </button>
        </div>

        {/* 顶栏:日期 + 成绩本 */}
        <div className="flex flex-wrap items-center gap-2 border-b fin-divider-soft px-5 py-2 text-xs">
          <span className="fin-text-tertiary">练习日</span>
          <select
            value={signalDate}
            onChange={(e) => {
              setPhase('picking');
              setChosen(null);
              setSellIdx(null);
              void loadSession(e.target.value);
            }}
            className="rounded fin-input px-2 py-1 text-xs"
            disabled={loading}
          >
            {(session?.availableDates || []).map((d) => (
              <option key={d} value={d}>{d}</option>
            ))}
          </select>
          {session?.nextDate && <span className="fin-text-tertiary">次日 {session.nextDate}</span>}
          {loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-sky-300" />}
          <div className="flex-1" />
          {stats && (
            <span className="fin-text-tertiary">
              已练 <b className="fin-text-primary">{stats.n}</b> 次 · 平均净收益{' '}
              <b className={pctClass(stats.avg)}>{fmt(stats.avg, 2, true)}%</b> · 赢机械持有{' '}
              <b className="fin-text-primary">{stats.beat}/{stats.n}</b> · 盈利{' '}
              <b className="fin-text-primary">{stats.win}/{stats.n}</b>
            </span>
          )}
        </div>

        {error && (
          <div className="mx-5 mt-3 flex items-start gap-2 rounded-lg border border-amber-400/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}
        {session?.warning && phase === 'picking' && (
          <div className="mx-5 mt-3 rounded-lg border fin-divider px-3 py-2 text-xs fin-text-tertiary">{session.warning}</div>
        )}

        {/* ============ 选票 ============ */}
        {phase === 'picking' && (
          <div className="flex-1 overflow-auto px-5 py-3">
            <div className="mb-2 text-xs fin-text-tertiary">
              {session?.signalDate} 收盘扫描选出 {session?.picks.length || 0} 只 —— 只看这些证据,挑一只你敢买的
            </div>
            <div className="grid grid-cols-2 gap-3">
              {(session?.picks || []).map((p) => (
                <div
                  key={p.symbol}
                  className="rounded-lg border fin-divider p-3 text-left transition-colors hover:border-sky-400/60"
                >
                  <div className="flex items-baseline gap-2">
                    {onOpenStock && session?.signalDate ? (
                      // 必须有信号日才给点:没有就不能截断,宁可不给看也不能把后续走势放出去(默认要向安全的一侧倒)
                      <button
                        type="button"
                        onClick={() => onOpenStock(p.symbol, p.name, p.price, session.signalDate)}
                        className="text-sm font-semibold text-sky-300 underline decoration-sky-400/40 underline-offset-2 hover:text-sky-200"
                        title={`看${p.name}的全屏四图(K线只画到 ${session?.signalDate},之后走势屏蔽)`}
                      >
                        {p.name}
                      </button>
                    ) : (
                      <span className="text-sm font-semibold fin-text-primary">{p.name}</span>
                    )}
                    <span className="text-[11px] fin-text-tertiary">{p.symbol} · {p.industry}</span>
                    <div className="flex-1" />
                    <span className="text-xs fin-text-tertiary">评分</span>
                    <span className="text-sm font-semibold text-amber-300">{fmt(p.score, 1)}</span>
                  </div>
                  <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[11px]">
                    <span className="fin-text-tertiary">收盘 <b className="fin-text-secondary">{fmt(p.price)}</b></span>
                    <span className="fin-text-tertiary">涨幅 <b className={pctClass(p.changePct)}>{fmt(p.changePct, 2, true)}%</b></span>
                    <span className="fin-text-tertiary">换手 <b className="fin-text-secondary">{fmt(p.turnover)}%</b></span>
                    <span className="fin-text-tertiary">主力 <b className={pctClass(p.mainPct)}>{fmt(p.mainPct, 2, true)}%</b></span>
                  </div>
                  {p.triggers.length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1">
                      {p.triggers.map((t) => (
                        <span key={t} className="rounded border border-sky-400/35 bg-sky-500/10 px-1.5 py-0.5 text-[10px] text-sky-200">{t}</span>
                      ))}
                    </div>
                  )}
                  {p.reasons.length > 0 && (
                    <ul className="mt-2 space-y-0.5 text-[10.5px] fin-text-tertiary">
                      {p.reasons.slice(0, 4).map((r, i) => (<li key={i}>· {r}</li>))}
                    </ul>
                  )}
                  {p.risks.length > 0 && (
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {p.risks.map((r) => (
                        <span key={r} className="rounded border border-amber-400/35 bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-200">{r}</span>
                      ))}
                    </div>
                  )}
                  <div className="mt-2 flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void startTrading(p)}
                      disabled={loading}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-sky-400/50 bg-sky-500/10 px-3 py-1 text-xs font-semibold text-sky-200 hover:bg-sky-500/20 disabled:opacity-50"
                    >
                      {loadingSym === p.symbol
                        ? <><Loader2 className="h-3.5 w-3.5 animate-spin" />正在取次日分时…</>
                        : <><Zap className="h-3.5 w-3.5" />买它 · 开始回放</>}
                    </button>
                    {onOpenStock && <span className="text-[10.5px] fin-text-tertiary">点股票名可先看四图形态(只到信号日)</span>}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ============ 回放 / 结算 ============ */}
        {(phase === 'trading' || phase === 'result') && chosen && (
          <div className="flex-1 overflow-auto px-5 py-3">
            <div className="flex flex-wrap items-baseline gap-3">
              <span className="text-base font-semibold fin-text-primary">{chosen.name}</span>
              <span className="text-xs fin-text-tertiary">{chosen.symbol} · 次日 {session?.nextDate}</span>
              <div className="flex-1" />
              {cur && (
                <>
                  <span className="text-xs fin-text-tertiary">{phase === 'result' ? '收盘' : cur.time}</span>
                  <span className={`text-xl font-bold ${pctClass(cur.price - prevClose)}`}>{fmt(cur.price)}</span>
                  <span className={`text-sm ${pctClass(cur.price - prevClose)}`}>
                    {fmt(((cur.price / prevClose - 1) * 100), 2, true)}%
                  </span>
                  <span className="text-xs fin-text-tertiary">浮盈</span>
                  <span className={`text-sm font-semibold ${pctClass(floating)}`}>{fmt(floating, 2, true)}%</span>
                </>
              )}
            </div>

            <div className="mt-1.5 flex flex-wrap items-center gap-3 text-[11px] fin-text-tertiary">
              <span>成本 <b className="text-sky-300">{fmt(buyPrice)}</b>(信号日 14:50 入选价)</span>
              <span>次日开盘 <b className={pctClass(openGap)}>{fmt(openGap, 2, true)}%</b></span>
              <span>信号日涨幅 {fmt(chosen.changePct, 2, true)}% · 评分 {fmt(chosen.score, 1)}</span>
            </div>

            {cantBuy && (
              <div className="mt-2 flex items-center gap-2 rounded-lg border border-amber-400/40 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-200">
                <AlertTriangle className="h-3.5 w-3.5" />
                信号日已封涨停({fmt(chosen.changePct, 2, true)}%),14:50 大概率买不进 —— 本题只作观察,不计入成绩
              </div>
            )}
            {sealedDown && (
              <div className="mt-2 flex items-center gap-2 rounded-lg border border-amber-400/40 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-200">
                <AlertTriangle className="h-3.5 w-3.5" />
                次日一字跌停、全天封死 —— 实盘挂单排不到,想卖也卖不掉,这一笔只能被动扛到收盘
              </div>
            )}

            <div className="mt-2 rounded-lg border fin-divider bg-black/20 p-2">{renderTape()}</div>

            {phase === 'trading' && (
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => setPlaying((p) => !p)}
                  className="inline-flex items-center gap-1.5 rounded-lg border fin-divider px-3 py-1.5 text-xs fin-hover"
                >
                  {playing ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                  <span>{playing ? '暂停' : '继续'}</span>
                </button>
                <div className="flex items-center gap-1">
                  {SPEEDS.map((s, i) => (
                    <button
                      key={s.label}
                      type="button"
                      onClick={() => setSpeedIdx(i)}
                      className={`rounded-md border px-2 py-1 text-[11px] transition-colors ${
                        speedIdx === i ? 'border-sky-400/60 bg-sky-500/15 text-sky-200' : 'fin-divider fin-text-tertiary hover:border-sky-400/40'
                      }`}
                    >
                      {s.label}
                    </button>
                  ))}
                </div>
                <button
                  type="button"
                  onClick={() => doSell(idx)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-rose-400/60 bg-rose-500/15 px-5 py-1.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/25"
                >
                  <Zap className="h-4 w-4" />
                  卖出(S)
                </button>
                <button
                  type="button"
                  onClick={() => { setPlaying(false); setIdx(ticks.length - 1); setMaxSeen(ticks.length - 1); }}
                  className="rounded-lg border fin-divider px-3 py-1.5 text-xs fin-hover"
                  title="不卖,直接看到收盘(仍可在收盘价卖出)"
                >
                  跳到收盘
                </button>
                <span className="text-[11px] fin-text-tertiary">空格暂停 · S 卖出 · 图上可左右拖时间轴回看</span>
              </div>
            )}

            {phase === 'result' && settle && (
              <div className="mt-3 space-y-3">
                <div className="grid grid-cols-5 gap-3">
                  <div className="rounded-lg border border-sky-400/40 bg-sky-500/8 p-3">
                    <div className="text-[11px] fin-text-tertiary">你的成绩(扣 {COST_PCT}% 成本)</div>
                    <div className={`text-2xl font-bold ${pctClass(settle.net)}`}>{fmt(settle.net, 2, true)}%</div>
                    <div className="text-[11px] fin-text-tertiary">{settle.sellTime} 卖在 {fmt(settle.sellPrice)}</div>
                  </div>
                  <div className="rounded-lg border fin-divider p-3">
                    <div className="text-[11px] fin-text-tertiary">开盘就卖</div>
                    <div className={`text-xl font-semibold ${pctClass(settle.openSell)}`}>{fmt(settle.openSell, 2, true)}%</div>
                    <div className="text-[11px] fin-text-tertiary">最保守的离场</div>
                  </div>
                  <div className="rounded-lg border fin-divider p-3">
                    <div className="text-[11px] fin-text-tertiary">持到收盘</div>
                    <div className={`text-xl font-semibold ${pctClass(settle.holdClose)}`}>{fmt(settle.holdClose, 2, true)}%</div>
                    <div className="text-[11px] fin-text-tertiary">机械纪律基准</div>
                  </div>
                  <div className="rounded-lg border fin-divider p-3">
                    <div className="text-[11px] fin-text-tertiary">当日最高(天花板)</div>
                    <div className={`text-xl font-semibold ${pctClass(settle.maxPct)}`}>{fmt(settle.maxPct, 2, true)}%</div>
                    <div className="text-[11px] fin-text-tertiary">事后诸葛,只作参照</div>
                  </div>
                  <div className="rounded-lg border fin-divider p-3">
                    <div className="text-[11px] fin-text-tertiary">你的卖点分位</div>
                    <div className="text-xl font-semibold fin-text-primary">{settle.percentile.toFixed(0)}%</div>
                    <div className="text-[11px] fin-text-tertiary">全天有这么多时间比你卖得低</div>
                  </div>
                </div>

                <div className="rounded-lg border fin-divider px-3 py-2 text-xs fin-text-secondary">
                  {settle.net >= settle.holdClose && settle.net > 0
                    ? '这一笔你赢了机械持有——离场时点选对了。别急着下结论,单次样本说明不了什么,多练几十次看均值。'
                    : settle.net >= settle.holdClose
                      ? '整体是亏的,但你比死拿到收盘少亏——止损意识在。'
                      : settle.net > 0
                        ? '赚了,但卖早了:拿到收盘更好。这类票的形态值得回看一眼。'
                        : '这笔亏了,而且没跑赢持有。回看分时:是开盘就该走,还是被中途震出来了?'}
                  {sealedDown && ' 本题是一字跌停封死,卖点选哪都一样——这类风险只能靠选股端规避,不是手速问题。'}
                  {peeked && ' ⚠️这一笔拖时间轴看过卖点之后的走势,不是盲判,已不计入成绩本(数字仍照实算给你看)。'}
                </div>

                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => { setPhase('picking'); setChosen(null); setSellIdx(null); }}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-sky-400/50 bg-sky-500/10 px-3 py-1.5 text-xs text-sky-200 hover:bg-sky-500/20"
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                    再练一只(同一天)
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      const dates = session?.availableDates || [];
                      const i = dates.indexOf(signalDate);
                      const next = dates[i + 1] || dates[0];
                      setPhase('picking');
                      setChosen(null);
                      setSellIdx(null);
                      void loadSession(next);
                    }}
                    className="inline-flex items-center gap-1.5 rounded-lg border fin-divider px-3 py-1.5 text-xs fin-hover"
                  >
                    换上一个交易日
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default FeelDrillDialog;
