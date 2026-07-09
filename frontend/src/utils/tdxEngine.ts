// 通达信(TDX)公式解释器:把 .txt 公式源码解析求值成图表可渲染的输出。
// 覆盖本库 93 个公式的高频函数子集;缺数据的函数(COST/FINANCE/DYNAINFO/WINNER 等)
// 求值为 NaN 并记录降级说明——依赖它们的输出线自动消失,其余照常显示。
import type { KLineData } from '../types';

// ---------- 输出模型 ----------
export interface TdxLine {
  name: string;
  color?: string;
  width?: number;
  dotted?: boolean;
  values: (number | null)[];
}
export interface TdxStick {
  color?: string;
  // 每根bar: [base, top] 或 null
  segs: ([number, number] | null)[];
  wide: boolean; // 宽柱(width>=0.5)
}
export interface TdxMarker {
  index: number;
  text: string;
  color?: string;
  above: boolean;
  price?: number;
}
export interface TdxOutput {
  lines: TdxLine[];
  sticks: TdxStick[];
  markers: TdxMarker[];
  notes: string[]; // 降级/不支持说明
}

// ---------- 词法 ----------
type Tok = { t: 'id' | 'num' | 'str' | 'op'; v: string };

function tokenize(src: string): Tok[] {
  // 去注释 {...} 与 //...  ;规范全角
  src = src.replace(/\{[^}]*\}/g, ' ').replace(/\/\/[^\n]*/g, ' ')
    .replace(/[，]/g, ',').replace(/[；]/g, ';').replace(/[（]/g, '(').replace(/[）]/g, ')')
    .replace(/[：]/g, ':').replace(/[‘’']/g, "'").replace(/[“”]/g, "'");
  const toks: Tok[] = [];
  let i = 0;
  const isIdStart = (c: string) => /[A-Za-z_-￿]/.test(c);
  const isId = (c: string) => /[A-Za-z0-9_.-￿]/.test(c);
  while (i < src.length) {
    const c = src[i];
    if (/\s/.test(c)) { i++; continue; }
    if (c === "'") { // 字符串
      let j = i + 1, s = '';
      while (j < src.length && src[j] !== "'") { s += src[j]; j++; }
      toks.push({ t: 'str', v: s }); i = j + 1; continue;
    }
    if (/[0-9]/.test(c) || (c === '.' && /[0-9]/.test(src[i + 1] || ''))) {
      let j = i, s = '';
      while (j < src.length && /[0-9.]/.test(src[j])) { s += src[j]; j++; }
      toks.push({ t: 'num', v: s }); i = j; continue;
    }
    if (isIdStart(c)) {
      let j = i, s = '';
      while (j < src.length && isId(src[j])) { s += src[j]; j++; }
      toks.push({ t: 'id', v: s }); i = j; continue;
    }
    // 运算符
    const two = src.slice(i, i + 2);
    if (two === '!=') { toks.push({ t: 'op', v: '<>' }); i += 2; continue; } // C风格不等号 → TDX <>
    if (two === '==') { toks.push({ t: 'op', v: '=' }); i += 2; continue; }
    if (two === ':=' || two === '>=' || two === '<=' || two === '<>' || two === '&&' || two === '||') {
      toks.push({ t: 'op', v: two }); i += 2; continue;
    }
    toks.push({ t: 'op', v: c }); i++; continue;
  }
  return toks;
}

// ---------- 语法 ----------
type Expr =
  | { k: 'num'; v: number }
  | { k: 'str'; v: string }
  | { k: 'var'; name: string }
  | { k: 'call'; name: string; args: Expr[] }
  | { k: 'bin'; op: string; l: Expr; r: Expr }
  | { k: 'un'; op: string; e: Expr };

interface Stmt {
  name?: string;       // 变量名(无名=纯绘图语句)
  visible: boolean;    // ':' 输出 / ':=' 隐藏
  expr: Expr;
  attrs: string[];     // COLORRED/LINETHICK2/POINTDOT/NODRAW/COLORSTICK...
}

class Parser {
  toks: Tok[]; i = 0;
  constructor(toks: Tok[]) { this.toks = toks; }
  peek(o = 0) { return this.toks[this.i + o]; }
  next() { return this.toks[this.i++]; }
  eat(v: string) { const t = this.peek(); if (t && t.t === 'op' && t.v === v) { this.i++; return true; } return false; }

  parseAll(): Stmt[] {
    const out: Stmt[] = [];
    while (this.i < this.toks.length) {
      // 跳过空分号
      if (this.eat(';')) continue;
      const st = this.parseStmt();
      if (st) out.push(st);
      // 吃到分号或结尾
      while (this.i < this.toks.length && !this.eat(';')) this.i++;
    }
    return out;
  }

  parseStmt(): Stmt | null {
    const start = this.i;
    let name: string | undefined;
    let visible = true;
    const t0 = this.peek(), t1 = this.peek(1);
    if (t0 && t0.t === 'id' && t1 && t1.t === 'op' && (t1.v === ':' || t1.v === ':=')) {
      name = t0.v; visible = t1.v === ':';
      this.i += 2;
    }
    let expr: Expr;
    try {
      expr = this.parseExpr();
    } catch {
      this.i = start; return null;
    }
    const attrs: string[] = [];
    while (this.eat(',')) {
      const a = this.peek();
      if (a && a.t === 'id') { attrs.push(a.v.toUpperCase()); this.i++; }
      else if (a && a.t === 'num') { attrs.push(a.v); this.i++; }
      else break;
    }
    return { name, visible, expr, attrs };
  }

  parseExpr(): Expr { return this.parseOr(); }
  parseOr(): Expr {
    let l = this.parseAnd();
    for (;;) {
      const t = this.peek();
      if (t && ((t.t === 'op' && t.v === '||') || (t.t === 'id' && t.v.toUpperCase() === 'OR'))) {
        this.i++; l = { k: 'bin', op: 'OR', l, r: this.parseAnd() };
      } else return l;
    }
  }
  parseAnd(): Expr {
    let l = this.parseCmp();
    for (;;) {
      const t = this.peek();
      if (t && ((t.t === 'op' && t.v === '&&') || (t.t === 'id' && t.v.toUpperCase() === 'AND'))) {
        this.i++; l = { k: 'bin', op: 'AND', l, r: this.parseCmp() };
      } else return l;
    }
  }
  parseCmp(): Expr {
    let l = this.parseAdd();
    for (;;) {
      const t = this.peek();
      if (t && t.t === 'op' && ['>', '<', '>=', '<=', '=', '<>'].includes(t.v)) {
        this.i++; l = { k: 'bin', op: t.v, l, r: this.parseAdd() };
      } else return l;
    }
  }
  parseAdd(): Expr {
    let l = this.parseMul();
    for (;;) {
      const t = this.peek();
      if (t && t.t === 'op' && (t.v === '+' || t.v === '-')) {
        this.i++; l = { k: 'bin', op: t.v, l, r: this.parseMul() };
      } else return l;
    }
  }
  parseMul(): Expr {
    let l = this.parseUnary();
    for (;;) {
      const t = this.peek();
      if (t && t.t === 'op' && (t.v === '*' || t.v === '/')) {
        this.i++; l = { k: 'bin', op: t.v, l, r: this.parseUnary() };
      } else return l;
    }
  }
  parseUnary(): Expr {
    const t = this.peek();
    if (t && t.t === 'op' && t.v === '-') { this.i++; return { k: 'un', op: '-', e: this.parseUnary() }; }
    if (t && t.t === 'op' && t.v === '+') { this.i++; return this.parseUnary(); }
    if (t && t.t === 'id' && t.v.toUpperCase() === 'NOT' && !(this.peek(1)?.t === 'op' && this.peek(1)?.v === '(')) {
      this.i++; return { k: 'un', op: 'NOT', e: this.parseUnary() };
    }
    return this.parsePrimary();
  }
  parsePrimary(): Expr {
    const t = this.next();
    if (!t) throw new Error('eof');
    if (t.t === 'num') return { k: 'num', v: parseFloat(t.v) };
    if (t.t === 'str') return { k: 'str', v: t.v };
    if (t.t === 'op' && t.v === '(') {
      const e = this.parseExpr();
      this.eat(')');
      return e;
    }
    if (t.t === 'id') {
      if (this.peek()?.t === 'op' && this.peek()?.v === '(') {
        this.i++; // (
        const args: Expr[] = [];
        if (!(this.peek()?.t === 'op' && this.peek()?.v === ')')) {
          args.push(this.parseExpr());
          while (this.eat(',')) args.push(this.parseExpr());
        }
        this.eat(')');
        return { k: 'call', name: t.v.toUpperCase(), args };
      }
      return { k: 'var', name: t.v.toUpperCase() };
    }
    throw new Error('unexpected ' + t.v);
  }
}

// ---------- 求值 ----------
type Ser = Float64Array;           // NaN=空
type Val = Ser | number | string;  // 序列 / 标量 / 字符串

const isSer = (v: Val): v is Ser => v instanceof Float64Array;

function toSer(v: Val, n: number): Ser {
  if (isSer(v)) return v;
  const s = new Float64Array(n);
  s.fill(typeof v === 'number' ? v : NaN);
  return s;
}
const num = (v: Val): number => (typeof v === 'number' ? v : isSer(v) ? v[v.length - 1] : NaN);

function bin(op: string, a: Val, b: Val, n: number): Val {
  if (typeof a === 'number' && typeof b === 'number') return scalarBin(op, a, b);
  const sa = toSer(a, n), sb = toSer(b, n), out = new Float64Array(n);
  for (let i = 0; i < n; i++) out[i] = scalarBin(op, sa[i], sb[i]);
  return out;
}
function scalarBin(op: string, x: number, y: number): number {
  switch (op) {
    case '+': return x + y;
    case '-': return x - y;
    case '*': return x * y;
    case '/': return y === 0 ? NaN : x / y;
    case '>': return x > y ? 1 : 0;
    case '<': return x < y ? 1 : 0;
    case '>=': return x >= y ? 1 : 0;
    case '<=': return x <= y ? 1 : 0;
    case '=': return Math.abs(x - y) < 1e-9 ? 1 : 0;
    case '<>': return Math.abs(x - y) < 1e-9 ? 0 : 1;
    case 'AND': return x !== 0 && !Number.isNaN(x) && y !== 0 && !Number.isNaN(y) ? 1 : 0;
    case 'OR': return (x !== 0 && !Number.isNaN(x)) || (y !== 0 && !Number.isNaN(y)) ? 1 : 0;
    default: return NaN;
  }
}

// TDX 颜色:COLOR 后 6 位十六进制是 BBGGRR
const NAMED_COLORS: Record<string, string> = {
  COLORRED: '#ef4444', COLORGREEN: '#22c55e', COLORYELLOW: '#eab308', COLORWHITE: '#e2e8f0',
  COLORCYAN: '#22d3ee', COLORMAGENTA: '#e879f9', COLORBLUE: '#3b82f6', COLORBLACK: '#64748b',
  COLORGRAY: '#94a3b8', COLORLIGRAY: '#cbd5e1', COLORLIRED: '#f87171', COLORLIGREEN: '#4ade80',
  COLORLIBLUE: '#93c5fd', COLORBROWN: '#b45309', COLORPURPLE: '#a855f7', COLORORANGE: '#f97316',
  COLORPINK: '#f472b6',
};
function attrColor(attrs: string[]): string | undefined {
  for (const a of attrs) {
    if (NAMED_COLORS[a]) return NAMED_COLORS[a];
    const m = /^COLOR([0-9A-F]{6})$/.exec(a);
    if (m) {
      const bb = m[1].slice(0, 2), gg = m[1].slice(2, 4), rr = m[1].slice(4, 6);
      return `#${rr}${gg}${bb}`;
    }
  }
  return undefined;
}
function attrWidth(attrs: string[]): number | undefined {
  for (const a of attrs) {
    const m = /^LINETHICK([0-9])$/.exec(a);
    if (m) return Math.min(4, Math.max(1, parseInt(m[1], 10)));
  }
  return undefined;
}

class Evaluator {
  n: number;
  env = new Map<string, Val>();
  out: TdxOutput = { lines: [], sticks: [], markers: [], notes: [] };
  noted = new Set<string>();
  bars: KLineData[];
  stockCode: string;
  stockName: string;
  floatShares: number;
  // 筹码估算(换手衰减模型,与通达信同源方法):每日直方图快照
  private chipHists?: Float64Array[] | null;
  private chipTotals?: Float64Array;
  private chipSeed = 0;   // 预热完成(累计换手≥0.8)的起始索引,之前为 NaN
  private chipLo = 0;
  private chipStep = 1;

  ctx: TdxContext;

  constructor(bars: KLineData[], ctx?: TdxContext) {
    this.bars = bars;
    this.ctx = ctx ?? {};
    this.stockCode = (ctx?.code ?? '').replace(/\D/g, '');
    this.stockName = ctx?.name ?? '';
    this.floatShares = ctx?.floatShares && ctx.floatShares > 0 ? ctx.floatShares : 0;
    this.n = bars.length;
    const n = this.n;
    const mk = (f: (b: KLineData, i: number) => number) => {
      const s = new Float64Array(n);
      for (let i = 0; i < n; i++) s[i] = f(bars[i], i);
      return s;
    };
    const C = mk(b => b.close), O = mk(b => b.open), H = mk(b => b.high), L = mk(b => b.low), V = mk(b => b.volume);
    for (const [k, v] of Object.entries({
      CLOSE: C, C, OPEN: O, O, HIGH: H, H, LOW: L, L, VOL: V, V,
      AMOUNT: mk(b => (b as any).amount ?? b.volume * b.close),
      AMO: mk(b => (b as any).amount ?? b.volume * b.close),
    })) this.env.set(k, v as Ser);
    // 常见环境量:没有的给 NaN;DRAWNULL 即"空点",NaN 正是其语义
    for (const k of ['HYBLOCK', 'DYBLOCK', 'GNBLOCK', 'CODE', 'STKNAME', 'DRAWNULL']) this.env.set(k, NaN);
    // CAPITAL/TOTALCAPITAL 单位为"手"(与 VOL 同),有股本数据就能算换手
    this.env.set('CAPITAL', this.floatShares > 0 ? this.floatShares / 100 : NaN);
    this.env.set('TOTALCAPITAL', ctx?.totalShares && ctx.totalShares > 0 ? ctx.totalShares / 100 : (this.floatShares > 0 ? this.floatShares / 100 : NaN));
    this.env.set('ISLASTBAR', mk((_, i) => (i === n - 1 ? 1 : 0)));
    this.env.set('CURRBARSCOUNT', mk((_, i) => n - i));
    this.env.set('BARPOS', mk((_, i) => i + 1));
    // 日期系列:按K线时间求值(TDX口径 DATE=(年-1900)*10000+月*100+日)
    const dateSer = new Float64Array(n), yearSer = new Float64Array(n), monthSer = new Float64Array(n), daySer = new Float64Array(n), weekSer = new Float64Array(n);
    for (let i = 0; i < n; i++) {
      const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(bars[i].time));
      if (!m) { dateSer[i] = yearSer[i] = monthSer[i] = daySer[i] = weekSer[i] = NaN; continue; }
      const y = +m[1], mo = +m[2], d = +m[3];
      dateSer[i] = (y - 1900) * 10000 + mo * 100 + d;
      yearSer[i] = y; monthSer[i] = mo; daySer[i] = d;
      weekSer[i] = new Date(y, mo - 1, d).getDay();
    }
    this.env.set('DATE', dateSer);
    this.env.set('YEAR', yearSer);
    this.env.set('MONTH', monthSer);
    this.env.set('DAY', daySer);
    this.env.set('WEEKDAY', weekSer);
  }

  note(s: string) {
    if (!this.noted.has(s)) { this.noted.add(s); this.out.notes.push(s); }
  }

  // 构建筹码分布(换手衰减):老筹码按 (1-换手) 衰减,当日成交按三角分布铺进 [低,高]。
  // 通达信的 WINNER/COST 同为估算模型,非真实股东数据。
  ensureChips(): boolean {
    if (this.chipHists !== undefined) return this.chipHists !== null;
    const n = this.n;
    if (!this.floatShares || n < 30) {
      this.chipHists = null;
      this.note('WINNER/COST 缺流通股本或K线过短→空');
      return false;
    }
    const F = this.floatShares;
    // 量的单位自校准(手/股/万股不定):选使近250日换手中位数落在 0.1%~35% 的倍率
    const vols: number[] = [];
    for (let i = Math.max(0, n - 250); i < n; i++) vols.push(this.bars[i].volume);
    const sorted = [...vols].sort((a, b) => a - b);
    const medV = sorted[Math.floor(sorted.length / 2)] || 0;
    let scale = 1;
    for (const s of [1, 100, 10000, 0.01]) {
      const t = (medV * s) / F;
      if (t >= 0.001 && t <= 0.35) { scale = s; break; }
    }
    let lo = Infinity, hi = -Infinity;
    for (const b of this.bars) { if (b.low < lo) lo = b.low; if (b.high > hi) hi = b.high; }
    if (!(hi > lo)) { this.chipHists = null; return false; }
    const NB = 120;
    const step = (hi - lo) / NB;
    const hist = new Float64Array(NB);
    const hists: Float64Array[] = new Array(n);
    const totals = new Float64Array(n);
    let total = 0, cumT = 0, seed = n;
    for (let i = 0; i < n; i++) {
      const b = this.bars[i];
      const t = Math.min(0.9, Math.max(0.0002, (b.volume * scale) / F));
      const keep = 1 - t;
      for (let k = 0; k < NB; k++) hist[k] *= keep;
      total *= keep;
      // 当日新筹码 t 份:三角权重,峰在 (H+L+2C)/4
      const l = b.low, h = b.high, peak = (b.high + b.low + 2 * b.close) / 4;
      const b0 = Math.max(0, Math.min(NB - 1, Math.floor((l - lo) / step)));
      const b1 = Math.max(0, Math.min(NB - 1, Math.floor((h - lo) / step)));
      const span = Math.max(1e-9, h - l);
      let wsum = 0;
      const ws: number[] = [];
      for (let k = b0; k <= b1; k++) {
        const px = lo + (k + 0.5) * step;
        const w = 1 - Math.min(1, Math.abs(px - peak) / span);
        ws.push(w + 0.05); wsum += w + 0.05;
      }
      for (let k = b0; k <= b1; k++) hist[k] += (t * ws[k - b0]) / wsum;
      total += t;
      cumT += t;
      if (seed === n && (cumT >= 0.8 || i >= 120)) seed = i;
      hists[i] = hist.slice();
      totals[i] = total;
    }
    this.chipHists = hists;
    this.chipTotals = totals;
    this.chipSeed = Math.max(seed, 20);
    this.chipLo = lo;
    this.chipStep = step;
    this.note('WINNER/COST 为本地换手衰减估算(与通达信同法),预热段为空');
    return true;
  }

  // 获利盘比例:价格 ≤ price 的筹码占比(0~1)
  chipCdf(i: number, price: number): number {
    const hist = this.chipHists![i], total = this.chipTotals![i];
    if (!total) return NaN;
    const pos = (price - this.chipLo) / this.chipStep;
    const full = Math.floor(pos);
    let s = 0;
    for (let k = 0; k < Math.min(full, hist.length); k++) s += hist[k];
    if (full >= 0 && full < hist.length) s += hist[full] * Math.min(1, Math.max(0, pos - full));
    return Math.min(1, Math.max(0, s / total));
  }

  // 成本分位:累计筹码达到 pct% 的价格
  chipCostPrice(i: number, pct: number): number {
    const hist = this.chipHists![i], total = this.chipTotals![i];
    if (!total) return NaN;
    const target = (Math.min(100, Math.max(0, pct)) / 100) * total;
    let s = 0;
    for (let k = 0; k < hist.length; k++) {
      if (s + hist[k] >= target) {
        const frac = hist[k] > 0 ? (target - s) / hist[k] : 0;
        return this.chipLo + (k + frac) * this.chipStep;
      }
      s += hist[k];
    }
    return this.chipLo + hist.length * this.chipStep;
  }

  truthyAt(v: Val, i: number): boolean {
    const x = isSer(v) ? v[i] : typeof v === 'number' ? v : NaN;
    return x !== 0 && !Number.isNaN(x);
  }

  strAt(e: Expr, i: number): string {
    // 字符串域最小实现:字面量 / STRCAT / CON2STR / 数值转串
    if (e.k === 'str') return e.v;
    if (e.k === 'call' && e.name === 'STRCAT') return e.args.map(a => this.strAt(a, i)).join('');
    if (e.k === 'call' && (e.name === 'CON2STR' || e.name === 'NUMTOSTR')) {
      const v = this.ev(e.args[0]);
      const d = e.args[1] ? Math.max(0, Math.round(num(this.ev(e.args[1])))) : 2;
      const x = isSer(v) ? v[i] : num(v);
      return Number.isNaN(x) ? '' : x.toFixed(d);
    }
    const v = this.ev(e);
    const x = isSer(v) ? v[i] : typeof v === 'number' ? v : NaN;
    return Number.isNaN(x) ? String(v ?? '') : String(Math.round(x * 100) / 100);
  }

  run(stmts: Stmt[]) {
    for (const st of stmts) {
      try {
        this.stmt(st);
      } catch (err) {
        this.note(`语句跳过: ${(err as Error).message?.slice(0, 40)}`);
      }
    }
    // 标记按语句逐条产出,跨语句时间会乱序;lightweight-charts 的 setMarkers 要求时间升序
    this.out.markers.sort((a, b) => a.index - b.index);
    return this.out;
  }

  stmt(st: Stmt) {
    // 纯绘图调用
    if (!st.name && st.expr.k === 'call' && this.drawCall(st.expr, st.attrs)) return;
    const v = this.ev(st.expr);
    if (st.name) this.env.set(st.name.toUpperCase(), v);
    // 可见输出且非 NODRAW → 画线
    const hide = st.attrs.includes('NODRAW');
    // 带名可见,或 IF(...)+POINTDOT 这类带属性的隐藏赋值不画
    if (st.visible && !hide) {
      // 绘图调用作为可见语句(如 A:STICKLINE(...))也处理
      if (st.expr.k === 'call' && this.drawCall(st.expr, st.attrs)) return;
      const ser = toSer(v, this.n);
      // 全 NaN 的线不输出
      let any = false;
      for (let i = 0; i < this.n; i++) if (!Number.isNaN(ser[i])) { any = true; break; }
      if (!any) return;
      this.out.lines.push({
        name: st.name || `L${this.out.lines.length + 1}`,
        color: attrColor(st.attrs),
        width: attrWidth(st.attrs),
        dotted: st.attrs.includes('POINTDOT') || st.attrs.includes('DOTLINE') || st.attrs.includes('CIRCLEDOT'),
        values: Array.from(ser, x => (Number.isNaN(x) ? null : x)),
      });
    }
  }

  // 绘图函数;返回 true 表示已消费
  drawCall(e: Expr & { k: 'call' }, attrs: string[]): boolean {
    const name = e.name;
    const color = attrColor(attrs);
    if (name === 'STICKLINE') {
      const cond = this.ev(e.args[0]);
      const p1 = this.ev(e.args[1]), p2 = this.ev(e.args[2]);
      const w = e.args[3] ? num(this.ev(e.args[3])) : 0.8;
      const segs: ([number, number] | null)[] = new Array(this.n).fill(null);
      const s1 = toSer(p1, this.n), s2 = toSer(p2, this.n);
      for (let i = 0; i < this.n; i++) {
        if (this.truthyAt(cond, i) && !Number.isNaN(s1[i]) && !Number.isNaN(s2[i])) segs[i] = [s1[i], s2[i]];
      }
      this.out.sticks.push({ color, segs, wide: w >= 0.5 });
      return true;
    }
    if (name === 'DRAWTEXT' || name === 'DRAWNUMBER') {
      const cond = this.ev(e.args[0]);
      const price = this.ev(e.args[1]);
      const ps = toSer(price, this.n);
      const vs = name === 'DRAWNUMBER' ? toSer(this.ev(e.args[2]), this.n) : null;
      const items: { index: number; text: string; above: boolean; price?: number }[] = [];
      for (let i = 0; i < this.n; i++) {
        if (this.truthyAt(cond, i)) {
          const text = vs
            ? (Number.isNaN(ps[i]) || Number.isNaN(vs[i]) ? '' : String(Math.round(vs[i] * 100) / 100))
            : this.strAt(e.args[2], i).slice(0, 6);
          if (!text) continue;
          const above = !Number.isNaN(ps[i]) && ps[i] >= (this.bars[i].close || 0);
          items.push({ index: i, text, above, price: Number.isNaN(ps[i]) ? undefined : ps[i] });
        }
      }
      // 超上限时保留最近的标记(截旧不截新)
      for (const it of items.slice(-400)) this.out.markers.push({ ...it, color });
      return true;
    }
    if (name === 'DRAWICON') {
      const cond = this.ev(e.args[0]);
      const price = this.ev(e.args[1]);
      const icon = e.args[2] ? num(this.ev(e.args[2])) : 1;
      const ICONS: Record<number, string> = { 1: '↑', 2: '↓', 3: '◆', 4: '★', 5: '▲', 6: '▼', 7: '⊙', 8: '✚', 9: '☀', 10: '●', 11: '♥', 12: '✿', 13: '☂', 34: '❀', 38: '⚑' };
      const ps = toSer(price, this.n);
      let cnt = 0;
      for (let i = 0; i < this.n && cnt < 400; i++) {
        if (this.truthyAt(cond, i)) {
          const above = !Number.isNaN(ps[i]) && ps[i] >= (this.bars[i].close || 0);
          this.out.markers.push({ index: i, text: ICONS[icon] || '◆', color, above, price: Number.isNaN(ps[i]) ? undefined : ps[i] });
          cnt++;
        }
      }
      return true;
    }
    if (name === 'DRAWBAND') {
      // 带状区域降级为两条线
      const a = toSer(this.ev(e.args[0]), this.n);
      const b = toSer(this.ev(e.args[2] ?? e.args[1]), this.n);
      this.out.lines.push({ name: 'BAND1', color: '#a855f766', values: Array.from(a, x => (Number.isNaN(x) ? null : x)) });
      this.out.lines.push({ name: 'BAND2', color: '#94a3b866', values: Array.from(b, x => (Number.isNaN(x) ? null : x)) });
      this.note('DRAWBAND 以双线近似');
      return true;
    }
    if (name === 'PARTLINE') {
      const cond = this.ev(e.args[0]);
      const val = toSer(this.ev(e.args[1]), this.n);
      const values: (number | null)[] = new Array(this.n).fill(null);
      for (let i = 0; i < this.n; i++) if (this.truthyAt(cond, i) && !Number.isNaN(val[i])) values[i] = val[i];
      this.out.lines.push({ name: 'PART', color: color || attrColor(e.args.slice(2).map(() => '')) || undefined, values });
      return true;
    }
    if (name === 'DRAWLINE') {
      // DRAWLINE(COND1,PRICE1,COND2,PRICE2,EXPAND):连接最近一个 COND1 点与最近一个 COND2 点,EXPAND>0 向右延伸
      const c1 = this.ev(e.args[0]), p1 = toSer(this.ev(e.args[1]), this.n);
      const c2 = this.ev(e.args[2]), p2 = toSer(this.ev(e.args[3]), this.n);
      const expand = e.args[4] ? num(this.ev(e.args[4])) : 0;
      let i1 = -1, i2 = -1;
      for (let i = this.n - 1; i >= 0; i--) {
        if (i1 < 0 && this.truthyAt(c1, i)) i1 = i;
        if (i2 < 0 && this.truthyAt(c2, i)) i2 = i;
        if (i1 >= 0 && i2 >= 0) break;
      }
      if (i1 < 0 || i2 < 0 || i1 === i2) return true;
      const a = Math.min(i1, i2), b = Math.max(i1, i2);
      const va = i1 < i2 ? p1[i1] : p2[i2], vb = i1 < i2 ? p2[i2] : p1[i1];
      if (Number.isNaN(va) || Number.isNaN(vb)) return true;
      const slope = (vb - va) / (b - a);
      const end = expand > 0 ? this.n - 1 : b;
      const values: (number | null)[] = new Array(this.n).fill(null);
      for (let i = a; i <= end; i++) values[i] = va + slope * (i - a);
      this.out.lines.push({
        name: `DL${this.out.lines.length + 1}`,
        color,
        width: attrWidth(attrs),
        dotted: attrs.includes('DOTLINE') || attrs.includes('POINTDOT'),
        values,
      });
      return true;
    }
    if (['DRAWKLINE', 'DRAWRECTREL', 'DRAWGBK', 'DRAWTEXT_FIX', 'DRAWNUMBER_FIX', 'FILLRGN', 'FLOATRGN', 'VERTLINE', 'HORLINE', 'POLYLINE', 'DRAWSL'].includes(name)) {
      this.note(`${name} 不支持,已跳过`);
      return true;
    }
    return false;
  }

  ev(e: Expr): Val {
    const n = this.n;
    switch (e.k) {
      case 'num': return e.v;
      case 'str': return e.v;
      case 'var': {
        const v = this.env.get(e.name);
        if (v === undefined) { this.note(`未知变量 ${e.name}→空`); return NaN; }
        return v;
      }
      case 'un': {
        const v = this.ev(e.e);
        if (e.op === '-') return bin('-', 0, v, n);
        // NOT
        const s = toSer(v, n), out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = s[i] === 0 || Number.isNaN(s[i]) ? 1 : 0;
        return out;
      }
      case 'bin': return bin(e.op, this.ev(e.l), this.ev(e.r), n);
      case 'call': return this.call(e);
    }
  }

  call(e: Expr & { k: 'call' }): Val {
    const n = this.n;
    const A = (i: number) => this.ev(e.args[i]);
    const S = (i: number) => toSer(this.ev(e.args[i]), n);
    const N = (i: number, d = 0) => (e.args[i] ? Math.round(num(this.ev(e.args[i]))) : d);
    const name = e.name;

    switch (name) {
      case 'REF': case 'REFX': {
        const x = S(0), out = new Float64Array(n);
        const p = this.ev(e.args[1]);
        const sign = name === 'REFX' ? -1 : 1;
        if (isSer(p)) {
          for (let i = 0; i < n; i++) {
            const k = i - sign * Math.round(p[i]);
            out[i] = k >= 0 && k < n ? x[k] : NaN;
          }
        } else {
          const d = sign * Math.round(p as number);
          for (let i = 0; i < n; i++) { const k = i - d; out[i] = k >= 0 && k < n ? x[k] : NaN; }
        }
        return out;
      }
      case 'MA': return rollMean(S(0), N(1, 5));
      case 'EMA': case 'EXPMA': case 'EXPMEMA': return ema(S(0), N(1, 5));
      case 'SMA': return smaTdx(S(0), N(1, 5), N(2, 1));
      case 'WMA': return wma(S(0), N(1, 5));
      case 'DMA': {
        const x = S(0), a = toSer(this.ev(e.args[1]), n), out = new Float64Array(n);
        let prev = NaN;
        for (let i = 0; i < n; i++) {
          const w = Math.min(1, Math.max(0, a[i]));
          prev = Number.isNaN(prev) ? x[i] : w * x[i] + (1 - w) * prev;
          out[i] = prev;
        }
        return out;
      }
      case 'AMA': return ema(S(0), Math.max(2, N(1, 10)));
      case 'HHV': return rollExt(S(0), N(1, 0), Math.max);
      case 'LLV': return rollExt(S(0), N(1, 0), Math.min);
      case 'HHVBARS': return extBars(S(0), N(1, 0), true);
      case 'LLVBARS': return extBars(S(0), N(1, 0), false);
      case 'SUM': {
        const x = S(0), p = N(1, 0), out = new Float64Array(n);
        let acc = 0;
        for (let i = 0; i < n; i++) {
          const xi = Number.isNaN(x[i]) ? 0 : x[i];
          if (p <= 0) { acc += xi; out[i] = acc; }
          else {
            let s = 0;
            for (let j = Math.max(0, i - p + 1); j <= i; j++) s += Number.isNaN(x[j]) ? 0 : x[j];
            out[i] = s;
          }
        }
        return out;
      }
      case 'COUNT': {
        const x = S(0), p = N(1, 0), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          let c = 0;
          const from = p <= 0 ? 0 : Math.max(0, i - p + 1);
          for (let j = from; j <= i; j++) if (x[j] !== 0 && !Number.isNaN(x[j])) c++;
          out[i] = c;
        }
        return out;
      }
      case 'EVERY': {
        const x = S(0), p = N(1, 1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          let ok = i >= p - 1 ? 1 : 0;
          for (let j = Math.max(0, i - p + 1); j <= i && ok; j++) if (x[j] === 0 || Number.isNaN(x[j])) ok = 0;
          out[i] = ok;
        }
        return out;
      }
      case 'EXIST': case 'EXISTR': {
        const x = S(0), p = N(1, 1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          let ok = 0;
          for (let j = Math.max(0, i - p + 1); j <= i && !ok; j++) if (x[j] !== 0 && !Number.isNaN(x[j])) ok = 1;
          out[i] = ok;
        }
        return out;
      }
      case 'CROSS': {
        const a = S(0), b = S(1), out = new Float64Array(n);
        for (let i = 1; i < n; i++) out[i] = a[i - 1] <= b[i - 1] && a[i] > b[i] ? 1 : 0;
        return out;
      }
      case 'LONGCROSS': {
        const a = S(0), b = S(1), p = N(2, 1), out = new Float64Array(n);
        for (let i = 1; i < n; i++) {
          let held = 1;
          for (let j = Math.max(1, i - p); j < i && held; j++) if (!(a[j] < b[j])) held = 0;
          out[i] = held && a[i] > b[i] ? 1 : 0;
        }
        return out;
      }
      case 'IF': case 'IFF': {
        const c = S(0), a = S(1), b = S(2), out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = c[i] !== 0 && !Number.isNaN(c[i]) ? a[i] : b[i];
        return out;
      }
      case 'MAX': {
        const a = S(0), b = S(1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = Math.max(a[i], b[i]);
        return out;
      }
      case 'MIN': {
        const a = S(0), b = S(1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = Math.min(a[i], b[i]);
        return out;
      }
      case 'ABS': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.abs(x[i]); return out; }
      case 'NOT': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = x[i] === 0 || Number.isNaN(x[i]) ? 1 : 0; return out; }
      case 'BETWEEN': {
        const x = S(0), a = S(1), b = S(2), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          const lo = Math.min(a[i], b[i]), hi = Math.max(a[i], b[i]);
          out[i] = x[i] >= lo && x[i] <= hi ? 1 : 0;
        }
        return out;
      }
      case 'RANGE': {
        const a = S(0), x = S(1), b = S(2), out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = x[i] > a[i] && x[i] < b[i] ? 1 : 0;
        return out;
      }
      case 'BARSLAST': {
        const x = S(0), out = new Float64Array(n);
        let last = -1;
        for (let i = 0; i < n; i++) {
          if (x[i] !== 0 && !Number.isNaN(x[i])) last = i;
          out[i] = last < 0 ? NaN : i - last;
        }
        return out;
      }
      case 'BARSSINCE': {
        const x = S(0), out = new Float64Array(n);
        let first = -1;
        for (let i = 0; i < n; i++) {
          if (first < 0 && x[i] !== 0 && !Number.isNaN(x[i])) first = i;
          out[i] = first < 0 ? NaN : i - first;
        }
        return out;
      }
      case 'BARSLASTCOUNT': {
        const x = S(0), out = new Float64Array(n);
        let run = 0;
        for (let i = 0; i < n; i++) { run = x[i] !== 0 && !Number.isNaN(x[i]) ? run + 1 : 0; out[i] = run; }
        return out;
      }
      case 'BARSCOUNT': { const out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = i + 1; return out; }
      case 'BARSNEXT': { this.note('BARSNEXT 未来函数→空'); return NaN; }
      case 'FILTER': {
        const x = S(0), p = N(1, 5), out = new Float64Array(n);
        let cool = -1;
        for (let i = 0; i < n; i++) {
          if (i > cool && x[i] !== 0 && !Number.isNaN(x[i])) { out[i] = 1; cool = i + p; }
          else out[i] = 0;
        }
        return out;
      }
      case 'TFILTER': {
        // TFILTER(信号,N,·) 近似为 FILTER 语义:信号触发后 N 日内不再重复
        const x = S(0), p = N(1, 5), out = new Float64Array(n);
        let cool = -1;
        for (let i = 0; i < n; i++) {
          if (i > cool && x[i] !== 0 && !Number.isNaN(x[i])) { out[i] = 1; cool = i + p; }
          else out[i] = 0;
        }
        return out;
      }
      case 'BACKSET': {
        const x = S(0), p = N(1, 1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          if (x[i] !== 0 && !Number.isNaN(x[i])) for (let j = Math.max(0, i - p + 1); j <= i; j++) out[j] = 1;
        }
        return out;
      }
      case 'CONST': { const x = S(0); return Number.isNaN(x[n - 1]) ? NaN : x[n - 1]; }
      case 'STD': {
        const x = S(0), p = Math.max(2, N(1, 5)), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          if (i < p - 1) { out[i] = NaN; continue; }
          let m = 0; for (let j = i - p + 1; j <= i; j++) m += x[j]; m /= p;
          let v = 0; for (let j = i - p + 1; j <= i; j++) v += (x[j] - m) ** 2;
          out[i] = Math.sqrt(v / (p - 1));
        }
        return out;
      }
      case 'AVEDEV': {
        const x = S(0), p = Math.max(1, N(1, 5)), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          if (i < p - 1) { out[i] = NaN; continue; }
          let m = 0; for (let j = i - p + 1; j <= i; j++) m += x[j]; m /= p;
          let v = 0; for (let j = i - p + 1; j <= i; j++) v += Math.abs(x[j] - m);
          out[i] = v / p;
        }
        return out;
      }
      case 'SLOPE': {
        const x = S(0), p = Math.max(2, N(1, 5)), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          if (i < p - 1) { out[i] = NaN; continue; }
          let sx = 0, sy = 0, sxy = 0, sxx = 0;
          for (let j = 0; j < p; j++) { const y = x[i - p + 1 + j]; sx += j; sy += y; sxy += j * y; sxx += j * j; }
          const d = p * sxx - sx * sx;
          out[i] = d === 0 ? NaN : (p * sxy - sx * sy) / d;
        }
        return out;
      }
      case 'ROUND': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.round(x[i]); return out; }
      case 'INTPART': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.trunc(x[i]); return out; }
      case 'POW': { const a = S(0), b = S(1), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.pow(a[i], b[i]); return out; }
      case 'SQRT': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.sqrt(x[i]); return out; }
      case 'LN': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.log(x[i]); return out; }
      case 'LOG': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.log10(x[i]); return out; }
      case 'ATAN': { const x = S(0), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.atan(x[i]); return out; }
      case 'ZIG': {
        // 事后之字转向(天然未来函数,仅展示用)
        const x = S(1) ? S(0) : S(0);
        const pct = e.args[1] ? num(this.ev(e.args[1])) : 5;
        return zigzag(x, pct / 100);
      }
      case 'ZTPRICE': { const a = S(0), b = S(1), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.round(a[i] * (1 + b[i]) * 100) / 100; return out; }
      case 'DTPRICE': { const a = S(0), b = S(1), out = new Float64Array(n); for (let i = 0; i < n; i++) out[i] = Math.round(a[i] * (1 - b[i]) * 100) / 100; return out; }
      case 'RGB': {
        const r = Math.round(num(A(0))), g = Math.round(num(A(1))), b = Math.round(num(A(2)));
        return `#${[r, g, b].map(x => Math.min(255, Math.max(0, x)).toString(16).padStart(2, '0')).join('')}`;
      }
      case 'STRCAT': case 'CON2STR': case 'NUMTOSTR': return '';
      case 'SUMBARS': {
        const x = S(0), t = S(1), out = new Float64Array(n);
        for (let i = 0; i < n; i++) {
          let s = 0, k = 0;
          for (let j = i; j >= 0; j--) { s += Number.isNaN(x[j]) ? 0 : x[j]; k++; if (s >= t[i]) break; }
          out[i] = k;
        }
        return out;
      }
      // 筹码函数:本地换手衰减估算(需 ctx.floatShares)
      case 'WINNER': {
        if (!this.ensureChips()) return NaN;
        const x = e.args[0] ? S(0) : (this.env.get('C') as Ser);
        const out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = i < this.chipSeed ? NaN : this.chipCdf(i, x[i]);
        return out;
      }
      case 'COST': {
        if (!this.ensureChips()) return NaN;
        const pct = e.args[0] ? num(this.ev(e.args[0])) : 50;
        const out = new Float64Array(n);
        for (let i = 0; i < n; i++) out[i] = i < this.chipSeed ? NaN : this.chipCostPrice(i, pct);
        return out;
      }
      case 'PWINNER': case 'COSTEX':
        this.note(`${name} 区间筹码函数不支持→空`); return NaN;
      case 'DYNAINFO': this.note('DYNAINFO 盘口动态数据→空'); return NaN;
      case 'FINANCE': {
        // 从 F10 估值推导的字段;资产负债表细项(10~31)未接入
        const idx = N(0, 0);
        const c0 = this.bars[this.n - 1]?.close ?? NaN;
        const { totalShares, pe, pb, floatMcap, totalMcap } = this.ctx;
        let v = NaN;
        switch (idx) {
          case 1: v = totalShares && totalShares > 0 ? totalShares : NaN; break; // 总股本(股)
          case 3: { // 板块代码:1主板 3创业板 4科创
            const d = this.stockCode;
            v = !d ? NaN : d.startsWith('30') ? 3 : d.startsWith('68') ? 4 : (d.startsWith('60') || d.startsWith('00')) ? 1 : 0;
            break;
          }
          case 7: v = this.floatShares > 0 ? this.floatShares : NaN; break;        // 流通股本(股)
          case 19: v = pb && pb > 0 && totalMcap ? totalMcap / pb : NaN; break;    // 股东权益(元,由市值/PB推)
          case 30: v = pe && pe > 0 && totalMcap ? totalMcap / pe : NaN; break;    // 净利润TTM(元,由市值/PE推)
          case 33: v = pe && pe > 0 ? c0 / pe : NaN; break;                        // 每股收益TTM
          case 34: v = pb && pb > 0 ? c0 / pb : NaN; break;                        // 每股净资产
          case 40: v = floatMcap && floatMcap > 0 ? floatMcap : NaN; break;        // 流通市值(元)
          case 41: v = totalMcap && totalMcap > 0 ? totalMcap : NaN; break;        // 总市值(元)
          default: break;
        }
        if (Number.isNaN(v)) this.note(`FINANCE(${idx}) 未接入→空`);
        return v;
      }
      case 'CODELIKE': {
        if (!this.stockCode) { this.note('CODELIKE 无股票代码上下文→按否'); return 0; }
        const pat = e.args[0] ? this.strAt(e.args[0], 0) : '';
        return pat && this.stockCode.startsWith(pat) ? 1 : 0;
      }
      case 'NAMELIKE': case 'NAMEINCLUDE': {
        if (!this.stockName) { this.note(`${name} 无股票名称上下文→按否`); return 0; }
        const pat = e.args[0] ? this.strAt(e.args[0], 0) : '';
        if (!pat) return 0;
        return (name === 'NAMELIKE' ? this.stockName.startsWith(pat) : this.stockName.includes(pat)) ? 1 : 0;
      }
      case 'INBLOCK': return 0;
      case 'DATE': case 'YEAR': case 'MONTH': case 'DAY': case 'WEEKDAY':
        return this.env.get(name) as Ser;
      case 'REFDATE': case 'DATETODAY': case 'TIME':
        this.note(`${name} 日期函数→空`); return NaN;
      case 'DRAWLINE': return NaN; // 带名的 DRAWLINE 语句会先走一次 ev,真正绘制在 drawCall;这里静默
      case 'EXTERNSTR': case 'BLOCKSETNUM': case 'HORCALC': case 'SELECT': case 'TESTSKIP':
        this.note(`${name} 不支持→空`); return NaN;
      case 'OSS': this.note('OSS 非标函数→空'); return NaN;
      default: {
        this.note(`未实现函数 ${name}→空`);
        return NaN;
      }
    }
  }
}

// ---------- 数学工具 ----------
function rollMean(x: Ser, p: number): Ser {
  const n = x.length, out = new Float64Array(n);
  if (p <= 1) return x.slice() as Ser;
  let s = 0, cnt = 0;
  for (let i = 0; i < n; i++) {
    const xi = x[i];
    if (!Number.isNaN(xi)) { s += xi; cnt++; }
    if (i >= p) { const xo = x[i - p]; if (!Number.isNaN(xo)) { s -= xo; cnt--; } }
    out[i] = i >= p - 1 && cnt > 0 ? s / cnt : NaN;
  }
  return out;
}
function ema(x: Ser, p: number): Ser {
  const n = x.length, out = new Float64Array(n);
  const k = 2 / (p + 1);
  let prev = NaN;
  for (let i = 0; i < n; i++) {
    const xi = x[i];
    if (Number.isNaN(xi)) { out[i] = prev; continue; }
    prev = Number.isNaN(prev) ? xi : xi * k + prev * (1 - k);
    out[i] = prev;
  }
  return out;
}
function smaTdx(x: Ser, p: number, m: number): Ser {
  const n = x.length, out = new Float64Array(n);
  let prev = NaN;
  for (let i = 0; i < n; i++) {
    const xi = x[i];
    if (Number.isNaN(xi)) { out[i] = prev; continue; }
    prev = Number.isNaN(prev) ? xi : (xi * m + prev * (p - m)) / p;
    out[i] = prev;
  }
  return out;
}
function wma(x: Ser, p: number): Ser {
  const n = x.length, out = new Float64Array(n);
  const denom = (p * (p + 1)) / 2;
  for (let i = 0; i < n; i++) {
    if (i < p - 1) { out[i] = NaN; continue; }
    let s = 0;
    for (let j = 0; j < p; j++) s += x[i - j] * (p - j);
    out[i] = s / denom;
  }
  return out;
}
function rollExt(x: Ser, p: number, f: (...v: number[]) => number): Ser {
  const n = x.length, out = new Float64Array(n);
  for (let i = 0; i < n; i++) {
    const from = p <= 0 ? 0 : Math.max(0, i - p + 1);
    let v = NaN;
    for (let j = from; j <= i; j++) {
      if (!Number.isNaN(x[j])) v = Number.isNaN(v) ? x[j] : f(v, x[j]);
    }
    out[i] = v;
  }
  return out;
}
function extBars(x: Ser, p: number, isMax: boolean): Ser {
  const n = x.length, out = new Float64Array(n);
  for (let i = 0; i < n; i++) {
    const from = p <= 0 ? 0 : Math.max(0, i - p + 1);
    let best = NaN, bi = i;
    for (let j = from; j <= i; j++) {
      if (Number.isNaN(x[j])) continue;
      if (Number.isNaN(best) || (isMax ? x[j] >= best : x[j] <= best)) { best = x[j]; bi = j; }
    }
    out[i] = i - bi;
  }
  return out;
}
function zigzag(x: Ser, pct: number): Ser {
  const n = x.length, out = new Float64Array(n).fill(NaN);
  if (n === 0) return out;
  let pivot = 0, dir = 0;
  const pts: number[] = [0];
  for (let i = 1; i < n; i++) {
    const chg = (x[i] - x[pivot]) / x[pivot];
    if (dir >= 0 && chg <= -pct) { pts.push(pivot); dir = -1; pivot = i; }
    else if (dir <= 0 && chg >= pct) { pts.push(pivot); dir = 1; pivot = i; }
    else if ((dir >= 0 && x[i] > x[pivot]) || (dir <= 0 && x[i] < x[pivot])) pivot = i;
  }
  pts.push(pivot); pts.push(n - 1);
  const uniq = [...new Set(pts)].sort((a, b) => a - b);
  for (let k = 0; k < uniq.length - 1; k++) {
    const a = uniq[k], b = uniq[k + 1];
    for (let i = a; i <= b; i++) out[i] = x[a] + ((x[b] - x[a]) * (i - a)) / Math.max(1, b - a);
  }
  return out;
}

// ---------- 入口 ----------
export interface TdxContext {
  code?: string; // 股票代码(可带交易所前缀,内部只取数字),供 CODELIKE 判定板块
  name?: string; // 股票名称,供 NAMELIKE 判定 ST
  floatShares?: number; // 流通股本(股),供 WINNER/COST 筹码估算与 CAPITAL/FINANCE(7)
  totalShares?: number; // 总股本(股)
  pe?: number;          // 市盈率TTM → FINANCE(30/33)
  pb?: number;          // 市净率 → FINANCE(19/34)
  floatMcap?: number;   // 流通市值(元) → FINANCE(40)
  totalMcap?: number;   // 总市值(元) → FINANCE(41)
}

const cache = new Map<string, TdxOutput>();

export function evaluateTdxFormula(id: string, source: string, bars: KLineData[], ctx?: TdxContext): TdxOutput {
  const key = `${id}|${bars.length}|${bars[bars.length - 1]?.time ?? ''}|${bars[bars.length - 1]?.close ?? ''}|${ctx?.code ?? ''}|${ctx?.floatShares ?? 0}`;
  const hit = cache.get(key);
  if (hit) return hit;
  let out: TdxOutput;
  try {
    const stmts = new Parser(tokenize(source)).parseAll();
    out = new Evaluator(bars, ctx).run(stmts);
    if (!(ctx?.floatShares && ctx.floatShares > 0) && /CAPITAL/i.test(source)) out.notes.push('CAPITAL(流通盘)无数据,换手/市值类条件不成立');
  } catch (err) {
    out = { lines: [], sticks: [], markers: [], notes: [`公式解析失败: ${(err as Error).message}`] };
  }
  if (cache.size > 40) cache.clear();
  cache.set(key, out);
  return out;
}
