import React, { useCallback, useEffect, useRef, useState } from 'react';
import { X, Loader2, RefreshCw } from 'lucide-react';
import { GetStockIntraday } from '../../wailsjs/go/main/App';
import { useTheme } from '../contexts/ThemeContext';

interface AuctionTick { time: string; price: number; volume: number; b1?: number; s1?: number }
interface Props {
  isOpen: boolean;
  onClose: () => void;
  symbol: string;
  name: string;
  preClose: number;
}

// 独立「集合竞价」视图:9:15-9:25 单独放大成一屏——上方竞价价格阶梯线、下方累积匹配量上升柱。
// 纯 canvas 自绘(直接像素映射,不依赖图表坐标系),避免嵌入分时图的时序/压缩问题。
export const AuctionChartDialog: React.FC<Props> = ({ isOpen, onClose, symbol, name, preClose }) => {
  const { colors } = useTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [ticks, setTicks] = useState<AuctionTick[]>([]);
  const [loading, setLoading] = useState(false);
  const [tip, setTip] = useState('');

  const load = useCallback(async () => {
    if (!symbol) return;
    setLoading(true); setTip('');
    try {
      const today = new Date().toISOString().slice(0, 10);
      const res: any = await GetStockIntraday(symbol, today);
      const list: AuctionTick[] = ((res && res.auction) || []).map((t: any) => ({
        time: String(t.time), price: Number(t.price), volume: Number(t.volume) || 0,
        b1: Number(t.b1) || 0, s1: Number(t.s1) || 0,
      }));
      setTicks(list);
      if (list.length < 2) setTip('今日暂无集合竞价数据(竞价采集在交易日 9:15 起,或该股未被采集)。');
    } catch {
      setTip('集合竞价数据获取失败。');
    } finally {
      setLoading(false);
    }
  }, [symbol]);

  useEffect(() => { if (isOpen) void load(); }, [isOpen, load]);

  // 竞价时段每 5s 刷新看生长
  useEffect(() => {
    if (!isOpen) return;
    const now = new Date();
    const hm = now.getHours() * 100 + now.getMinutes();
    if (hm < 913 || hm > 927) return;
    const id = window.setInterval(() => void load(), 5000);
    return () => window.clearInterval(id);
  }, [isOpen, load]);

  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas || ticks.length < 2) return;
    const W = canvas.clientWidth, H = canvas.clientHeight;
    if (W <= 0 || H <= 0) return;
    const dpr = window.devicePixelRatio || 1;
    if (canvas.width !== Math.floor(W * dpr) || canvas.height !== Math.floor(H * dpr)) {
      canvas.width = Math.floor(W * dpr); canvas.height = Math.floor(H * dpr);
    }
    const ctx = canvas.getContext('2d'); if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, W, H);

    const padL = 58, padR = 58, padT = 12, padB = 24;
    const gap = 10;
    const priceH = Math.round((H - padT - padB - gap) * 0.62);
    const volH = (H - padT - padB - gap) - priceH;
    const plotW = W - padL - padR;
    const priceTop = padT, priceBot = padT + priceH;
    const volTop = priceBot + gap, volBot = volTop + volH;

    // 价格对称范围(以昨收为中心)
    const ref = preClose > 0 ? preClose : ticks[0].price;
    let maxDev = 0;
    for (const t of ticks) maxDev = Math.max(maxDev, Math.abs(t.price - ref));
    if (maxDev <= 0) maxDev = ref * 0.01;
    maxDev *= 1.12;
    const pMin = ref - maxDev, pMax = ref + maxDev;
    const xOf = (i: number) => padL + (ticks.length <= 1 ? 0 : (i / (ticks.length - 1)) * plotW);
    const yOfPrice = (p: number) => priceTop + (1 - (p - pMin) / (pMax - pMin)) * priceH;

    // 网格
    ctx.strokeStyle = colors.isDark ? 'rgba(148,163,184,0.12)' : 'rgba(100,116,139,0.15)';
    ctx.lineWidth = 1;
    for (let k = 0; k <= 4; k++) {
      const y = priceTop + (k / 4) * priceH;
      ctx.beginPath(); ctx.moveTo(padL, y); ctx.lineTo(W - padR, y); ctx.stroke();
    }
    // 昨收基准线(红虚线)
    const yRef = yOfPrice(ref);
    ctx.strokeStyle = '#ef4444'; ctx.setLineDash([4, 3]); ctx.lineWidth = 1;
    ctx.beginPath(); ctx.moveTo(padL, yRef); ctx.lineTo(W - padR, yRef); ctx.stroke();
    ctx.setLineDash([]);

    // 价格阶梯线(白色):水平到当前 x(用前点 y)再垂直到当前 y
    ctx.strokeStyle = '#f8fafc'; ctx.lineWidth = 1.4;
    ctx.beginPath();
    ctx.moveTo(xOf(0), yOfPrice(ticks[0].price));
    for (let i = 1; i < ticks.length; i++) {
      ctx.lineTo(xOf(i), yOfPrice(ticks[i - 1].price));
      ctx.lineTo(xOf(i), yOfPrice(ticks[i].price));
    }
    ctx.stroke();

    // 价格/涨跌幅刻度
    ctx.font = '11px -apple-system,system-ui,sans-serif';
    ctx.textBaseline = 'middle';
    for (let k = 0; k <= 4; k++) {
      const p = pMax - (k / 4) * (pMax - pMin);
      const y = priceTop + (k / 4) * priceH;
      const up = p >= ref;
      ctx.fillStyle = up ? '#ef4444' : '#22c55e';
      ctx.textAlign = 'right';
      ctx.fillText(p.toFixed(2), padL - 6, y);
      const pct = ((p / ref - 1) * 100);
      ctx.textAlign = 'left';
      ctx.fillText(`${pct >= 0 ? '+' : ''}${pct.toFixed(2)}%`, W - padR + 6, y);
    }

    // 成交量(累积匹配量):上升柱,红(价≥昨收)/绿。
    let maxVol = 0;
    for (const t of ticks) maxVol = Math.max(maxVol, t.volume);
    if (maxVol <= 0) maxVol = 1;
    const barW = Math.max(1, plotW / ticks.length * 0.8);
    for (let i = 0; i < ticks.length; i++) {
      const t = ticks[i];
      const h = (t.volume / maxVol) * (volH - 2);
      ctx.fillStyle = (t.price >= ref ? '#ef4444' : '#22c55e') + 'dd';
      ctx.fillRect(xOf(i) - barW / 2, volBot - h, barW, h);
    }
    // 量峰值标注
    ctx.fillStyle = colors.isDark ? '#94a3b8' : '#64748b';
    ctx.textAlign = 'right'; ctx.textBaseline = 'top';
    ctx.fillText(`${Math.round(maxVol)}手`, W - padR - 4, volTop + 2);

    // 时间标注
    ctx.fillStyle = colors.isDark ? '#94a3b8' : '#64748b';
    ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    ctx.fillText('09:15 ~ 09:25', padL + plotW / 2, volBot + 6);

    // 开盘未匹配量(自选/持仓股才有,临撮合时刻)
    const last = [...ticks].reverse().find(t => (t.b1 || 0) > 0 || (t.s1 || 0) > 0);
    if (last) {
      const unm = Math.abs((last.b1 || 0) - (last.s1 || 0));
      const buyMore = (last.b1 || 0) >= (last.s1 || 0);
      ctx.fillStyle = buyMore ? '#ef4444' : '#22c55e';
      ctx.textAlign = 'left'; ctx.textBaseline = 'top';
      ctx.fillText(`开盘未匹配 ${Math.round(unm)}手 ${buyMore ? '买多' : '卖多'}`, padL + 4, priceTop + 2);
    }
  }, [ticks, preClose, colors.isDark]);

  useEffect(() => {
    if (!isOpen) return;
    const id = requestAnimationFrame(draw);
    const ro = new ResizeObserver(() => draw());
    if (canvasRef.current) ro.observe(canvasRef.current);
    return () => { cancelAnimationFrame(id); ro.disconnect(); };
  }, [isOpen, draw]);

  if (!isOpen) return null;
  const last = ticks[ticks.length - 1];
  const pct = last && preClose > 0 ? (last.price / preClose - 1) * 100 : 0;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[720px] max-w-[94vw] h-[560px] max-h-[88vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <div className="text-sm font-semibold fin-text-primary">集合竞价 · {name} {symbol}</div>
            {last && (
              <span className={`text-xs font-mono ${pct >= 0 ? 'text-rose-400' : 'text-emerald-400'}`}>
                {last.price.toFixed(2)} ({pct >= 0 ? '+' : ''}{pct.toFixed(2)}%)
              </span>
            )}
            <span className="text-[11px] fin-text-tertiary">昨收 {preClose > 0 ? preClose.toFixed(2) : '—'}</span>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={() => void load()} className="p-1.5 rounded fin-hover" title="刷新">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 fin-text-secondary" />}
            </button>
            <button onClick={onClose} className="p-1.5 rounded fin-hover"><X className="h-4 w-4 fin-text-secondary" /></button>
          </div>
        </div>
        <div className="flex-1 relative p-2">
          {tip ? (
            <div className="absolute inset-0 flex items-center justify-center text-xs fin-text-tertiary px-6 text-center">{tip}</div>
          ) : (
            <canvas ref={canvasRef} className="w-full h-full" />
          )}
        </div>
        <div className="px-5 py-2 border-t fin-divider text-[11px] fin-text-tertiary leading-relaxed">
          上=竞价价格阶梯线(昨收红虚线),下=累积匹配量上升柱。9:15-9:20 可挂撤单、9:20-9:25 只挂不撤、9:25 撮合出开盘价。开盘未匹配量仅自选/持仓股有。
        </div>
      </div>
    </div>
  );
};

export default AuctionChartDialog;
