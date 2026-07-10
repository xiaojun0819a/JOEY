import React, { useEffect, useRef, useCallback, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, Eye, EyeOff, MoveHorizontal, Target, Zap, ZoomIn, ZoomOut } from 'lucide-react';
import {
  createChart,
  createSeriesMarkers,
  IChartApi,
  ISeriesMarkersPluginApi,
  ISeriesApi,
  CandlestickData,
  HistogramData,
  LineData,
  SeriesMarker,
  Time,
  CrosshairMode,
  LineStyle,
  CandlestickSeries,
  LineSeries,
  HistogramSeries,
  SeriesType,
  MouseEventParams,
  LogicalRange,
} from 'lightweight-charts';
import { KLineData, TimePeriod, Stock, FundFlowSeries } from '../types';
import { useTheme } from '../contexts/ThemeContext';
import { evaluateTdxFormula } from '../utils/tdxEngine';
import { getTdxFormula, TDX_MAIN_GROUPS, TDX_SUB_GROUPS } from '../utils/tdxCatalog';
import { getF10Valuation } from '../services/f10Service';

// 股本/估值缓存,供 TDX 引擎筹码估算(WINNER/COST)与 CAPITAL/FINANCE;跨图表实例共享
interface StockFinCtx { floatShares?: number; totalShares?: number; pe?: number; pb?: number; floatMcap?: number; totalMcap?: number }
const finCtxCache = new Map<string, StockFinCtx>();
import { useCandleColor } from '../contexts/CandleColorContext';
import { ResizeHandle } from './ResizeHandle';
import { useIndicator } from '../contexts/IndicatorContext';
import { getF10Overview } from '../services/f10Service';
import { GetStockIntraday } from '../../wailsjs/go/main/App';
import {
  parseTime,
  calculateSMA,
  calculateEMA,
  calculateBOLL,
  calculateMACD,
  calculateRSI,
  calculateKDJ,
  calculateCCI,
  calculateWR,
} from '../utils/indicators';
import { calculateIntradayTradingSignals, calculateTradingSignals, getLatestTradingSignal, TradingSignal } from '../utils/tradingSignals';

const VOLUME_MIN = 18;
const VOLUME_MAX = 420;
const VOLUME_DEFAULT = 56;
// 四宫格(3副图)区域总高度，默认按容器高度比例初始化，仍保留手动上下拖拽。
const GRID_SUB_MIN = 150;
const GRID_SUB_MAX = 560;
const GRID_SUB_DEFAULT = 300;
const GRID_SUB_HEIGHT_RATIO = 0.45;
const PRICE_SCALE_MIN_WIDTH = 72;
const SHOW_MAIN_TRADING_MARKERS = false;

interface StockChartProps {
  data: KLineData[];
  updateMode: 'full' | 'incremental' | 'refresh';
  period: TimePeriod;
  onPeriodChange: (p: TimePeriod) => void;
  stock?: Stock;
  dayKData?: KLineData[];
  mainChartTemplate?: MainChartTemplate;
  onMainChartTemplateChange?: (template: MainChartTemplate) => void;
  showTemplateSelect?: boolean; // 在周期栏内显示主图模板切换(全屏图表用)
  showAuction?: boolean; // 分时图前叠加当日集合竞价段(默认关,头部按钮开启)
  initialGridMode?: boolean;
  initialSubChartType?: SubChartType;
  initialSubType2?: SubChartType;
  initialSubType3?: SubChartType;
  visibleRangeBars?: number;
}

// 副图类型
export type SubChartType =
  | 'volume'
  | 'macd'
  | 'rsi'
  | 'kdj'
  | 'cci'
  | 'wr'
  | 'volumeSupport'
  | 'mainFundFlow'
  | 'lowBuySignal'
  | 'trendStrength'
  | 'fundTurn'
  | 'sellRisk'
  | 'vipAnomaly'
  | 'vipShortEnergy'
  | 'vipFiveDragon'
  | `tdx:${string}`;
type SubSelectOption = SubChartType | 'openEatFishMain';

const SUB_OPTIONS: { id: SubSelectOption; label: string }[] = [
  { id: 'volume', label: '成交量' },
  { id: 'macd', label: 'MACD' },
  { id: 'kdj', label: 'KDJ' },
  { id: 'rsi', label: 'RSI' },
  { id: 'cci', label: 'CCI' },
  { id: 'wr', label: 'WR' },
  { id: 'volumeSupport', label: '缩量承接' },
  { id: 'mainFundFlow', label: '主力资金' },
  { id: 'lowBuySignal', label: '低吸信号' },
  { id: 'trendStrength', label: '趋势强度' },
  { id: 'fundTurn', label: '资金拐点' },
  { id: 'sellRisk', label: '卖点风险' },
  { id: 'vipAnomaly', label: '1异动监控VIP' },
  { id: 'vipShortEnergy', label: '2短线能量VIP' },
  { id: 'vipFiveDragon', label: '3五维擒龙VIP' },
  { id: 'openEatFishMain', label: '主图：开仓吃鱼' },
];

// 四窗口自动组合:一键设置 主图模板 + 三个副图(仅全屏看板的组合下拉使用)
interface ChartCombo {
  id: string;
  label: string;
  main: MainChartTemplate;
  subs: [SubChartType, SubChartType, SubChartType];
}
const CHART_COMBOS: ChartCombo[] = [
  { id: 'fishBody', label: '鱼身组合', main: 'openEatFish', subs: ['vipAnomaly', 'vipShortEnergy', 'vipFiveDragon'] },
  {
    // 套餐A 打板/情绪周期:涨停判定(客观事实) + 影线拆量 + RSI/DMI状态带 + 开盘箱体大小单
    id: 'limitUpMood',
    label: '打板情绪',
    main: 'tdx:连板王主图指标公式源码',
    subs: ['tdx:监控资金波动指标公式', 'tdx:多彩共振指标公式源码', 'tdx:AI分时主图指标公式源码'],
  },
  {
    // 套餐B 波段持仓:结构标注主图(原筹码云主图因COST无数据已删) + 量均线体系 + 中期区间位置 + 三级撑压
    id: 'chipSwing',
    label: '波段持仓',
    main: 'tdx:通用面板1_结构标注主图',
    subs: ['tdx:放量起飞指标公式源码', 'tdx:抓住黑马指标公式源码', 'tdx:短中长线撑压主图指标公式源码'],
  },
  {
    // 套餐C 趋势跟踪+风控:唯一带止损止盈线的公式 + DMI趋势状态 + 量能 + 均线结构注释
    id: 'trendRisk',
    label: '趋势风控',
    main: 'tdx:战赢趋势主图指标公式源码',
    subs: ['tdx:多彩共振指标公式源码', 'tdx:监控资金波动指标公式', 'tdx:唐能通精准买卖源码'],
  },
  {
    // 诚实版通用面板:结构标注(涨停/均线状态) + 情绪数板 + 量能结构 + 位置读数;只陈述事实不画买卖点
    id: 'honestPanel',
    label: '诚实面板',
    main: 'tdx:通用面板1_结构标注主图',
    subs: ['tdx:通用面板2_情绪数板副图', 'tdx:通用面板3_量能结构副图', 'tdx:通用面板4_位置读数副图'],
  },
];

// 副图下拉的常规选项集合;组合可能把主图类TDX塞进副图窗口,不在此集合时下拉补一个回显项
const TDX_SUB_OPTION_IDS = new Set(TDX_SUB_GROUPS.flatMap(g => g.items.map(f => f.id)));

function isVipSubChartType(type: SubChartType): type is 'vipAnomaly' | 'vipShortEnergy' | 'vipFiveDragon' {
  return type === 'vipAnomaly' || type === 'vipShortEnergy' || type === 'vipFiveDragon';
}

// 指标线颜色常量
const MA_COLORS = ['#facc15', '#a855f7', '#f97316', '#38bdf8', '#f43f5e'];
const EMA_COLORS = ['#06b6d4', '#ec4899'];
const BOLL_COLOR = '#e91e63';
const CHART_FONT_FAMILY = 'Menlo, Monaco, Consolas, monospace';
const VIP_COLORS = {
  gray: '#808080',
  rising: '#ff8040', // TDX COLOR4080FF
  hot: '#8000ff', // TDX COLORFF0080
  red: '#ff0000',
  green: '#00ff00',
  yellow: '#ffff00',
  lired: '#ff8f8f',
  cyan: '#00ffff',
  magenta: '#ff00ff',
  diamond: '#ff5577',
};

type IndicatorLegendItem = {
  label: string;
  value: string;
  color: string;
};

function readableTextColor(backgroundColor: string): string {
  const raw = backgroundColor.trim().replace('#', '');
  if (!/^[0-9a-fA-F]{3}$|^[0-9a-fA-F]{6}$/.test(raw)) return '#ffffff';
  const hex = raw.length === 3
    ? raw.split('').map(char => char + char).join('')
    : raw;
  const red = parseInt(hex.slice(0, 2), 16) / 255;
  const green = parseInt(hex.slice(2, 4), 16) / 255;
  const blue = parseInt(hex.slice(4, 6), 16) / 255;
  const toLinear = (value: number) => (
    value <= 0.03928 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4)
  );
  const luminance = 0.2126 * toLinear(red) + 0.7152 * toLinear(green) + 0.0722 * toLinear(blue);
  return luminance > 0.52 ? '#0f172a' : '#ffffff';
}

type FundFlowPoint = {
  time: Time;
  rawTime: string;
  mainNet: number;
  mainRatio: number | null;
  source: 'f10' | 'proxy';
};

type FundFlowState = {
  points: FundFlowPoint[];
  source: 'f10' | 'proxy' | 'none';
  loading: boolean;
  error?: string;
};

type SignalScorePoint = {
  time: Time;
  value: number;
  color?: string;
};

export type VipAnomalyPoint = {
  time: Time;
  rawTime: string;
  sz2: number;
  sz3: number;
  sz13: boolean;
  eatFish: boolean;
  anomaly: boolean;
  fire: boolean;
  superSignal: boolean;
  superCount: number;
  purpleTop: number;
  purpleBottom: number;
  escape: boolean; // 6.0 逃顶信号(趋势高位拐头/能量峰值回落),绿色钻石卖出
};

export type VipAnomalySeries = {
  points: VipAnomalyPoint[];
  latest?: VipAnomalyPoint;
  startPrice: number;
  endPrice: number;
};

export type VipShortEnergyPoint = {
  time: Time;
  rawTime: string;
  sz2: number;
  sz3: number;
  sz13: boolean;
  anomaly: boolean;
  fire: boolean;
  strongCondition: boolean;
  strongCount: number;
  control: number;
  controlScaled: number;
  controlWinnerProxy: boolean;
  startControl: boolean;
  mainShip: boolean;
  macdGolden: boolean;
  macdDead: boolean;
};

export type VipShortEnergySeries = {
  points: VipShortEnergyPoint[];
  latest?: VipShortEnergyPoint;
  startPrice: number;
  endPrice: number;
};

export type VipFiveDragonPoint = {
  time: Time;
  rawTime: string;
  openGap: number;
  controlDegree: number;
  lowControl: number;
  midControl: number;
  highControl: number;
  buySignal: boolean;
  trendBull: boolean;
  energyBull: boolean;
  midBull: boolean;
  shortBull: boolean;
  resonance: boolean;
};

export type VipFiveDragonSeries = {
  points: VipFiveDragonPoint[];
  latest?: VipFiveDragonPoint;
  startPrice: number;
  endPrice: number;
};

export type MainChartTemplate = 'standard' | 'openEatFish' | `tdx:${string}`;

export type OpenEatFishPoint = {
  time: Time;
  rawTime: string;
  open: number;
  high: number;
  low: number;
  close: number;
  ma5: number;
  ma10: number;
  ma20: number;
  ma30: number;
  ma60: number;
  lowerLimit: number;
  lifeLine: number;
  abc2: number;
  a: number;
  b: number;
  strong: boolean;
  weak: boolean;
  eatFish: boolean;
  openEatFish: boolean;
  takeProfit: boolean;
  breakTakeProfit: boolean;
  base: number;
};

export type OpenEatFishSeries = {
  points: OpenEatFishPoint[];
  latest?: OpenEatFishPoint;
  ma5: LineData[];
  ma10: LineData[];
  ma20: LineData[];
  ma30: LineData[];
  ma60: LineData[];
  lowerLimit: LineData[];
  upperBound: LineData[];
  lowerBound: LineData[]; // 透明撑底线:保证彩带下方的止盈钻石(×0.86)在自动缩放范围内
};

type RangeStats = {
  startTime: string;
  endTime: string;
  bars: number;
  open: number;
  close: number;
  high: number;
  low: number;
  changePct: number;
  changeVal: number;
  volume: number;
  amount: number;
};

function formatRangeVolume(v: number): string {
  if (v >= 1e8) return (v / 1e8).toFixed(2) + '亿';
  if (v >= 1e4) return (v / 1e4).toFixed(2) + '万';
  return String(Math.round(v));
}
function formatRangeAmount(v: number): string {
  if (v >= 1e8) return (v / 1e8).toFixed(2) + '亿';
  if (v >= 1e4) return (v / 1e4).toFixed(2) + '万';
  return v.toFixed(0);
}

function formatHoverVolume(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--';
  const abs = Math.abs(value);
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
}

function formatHoverAmount(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--';
  const abs = Math.abs(value);
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
}

function formatSignedFixed(value: number, digits = 2): string {
  const sign = value > 0 ? '+' : '';
  return `${sign}${value.toFixed(digits)}`;
}

function formatHoverTradeDate(timeStr: string): string {
  const datePart = timeStr.slice(0, 10).replace(/\//g, '-');
  const [year, month, day] = datePart.split('-').map(Number);
  if (!year || !month || !day) return timeStr.slice(0, 10).replace(/-/g, '/');
  const weekday = ['日', '一', '二', '三', '四', '五', '六'][new Date(Date.UTC(year, month - 1, day)).getUTCDay()];
  return `${year}/${String(month).padStart(2, '0')}/${String(day).padStart(2, '0')}/${weekday}`;
}

function chartTimeToSearchKey(time: Time): string {
  if (typeof time === 'number') {
    return new Date(time * 1000).toISOString().slice(0, 19).replace('T', ' ');
  }
  if (typeof time === 'string') {
    return time.replace('T', ' ').replace(/\//g, '-');
  }
  if (time && typeof time === 'object') {
    const day = time as { year?: number; month?: number; day?: number };
    if (day.year && day.month && day.day) {
      return `${day.year}-${String(day.month).padStart(2, '0')}-${String(day.day).padStart(2, '0')}`;
    }
  }
  return String(time);
}

function findKLineByChartTime(data: KLineData[], time: Time): KLineData | null {
  const searchKey = chartTimeToSearchKey(time);
  const searchMinute = searchKey.slice(0, 16);
  const searchDate = searchKey.slice(0, 10);

  return data.find((item) => {
    const rawKey = item.time.replace('T', ' ').replace(/\//g, '-');
    const parsedKey = chartTimeToSearchKey(parseTime(item.time));
    return rawKey === searchKey
      || parsedKey === searchKey
      || rawKey.slice(0, 16) === searchMinute
      || parsedKey.slice(0, 16) === searchMinute
      || rawKey.slice(0, 10) === searchDate
      || parsedKey.slice(0, 10) === searchDate;
  }) || null;
}

// 把 lightweight-charts 的 Time（日K=字符串/business-day，分时=秒级数字）或原始时间串统一成毫秒
function timeToMs(t: unknown): number {
  if (typeof t === 'number') return t * 1000; // 分时：秒级时间戳
  if (typeof t === 'string') {
    const iso = t.length > 10 ? t.replace(' ', 'T') + 'Z' : t + 'T00:00:00Z';
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? 0 : ms;
  }
  if (t && typeof t === 'object' && 'year' in (t as Record<string, unknown>)) {
    const o = t as { year: number; month?: number; day?: number };
    return Date.UTC(o.year, (o.month || 1) - 1, o.day || 1);
  }
  return 0;
}

// 统计 [ta, tb] 区间内的K线数据（涨跌幅以区间前一根收盘为基准，符合通达信口径）
function computeRangeStats(data: KLineData[], ta: Time, tb: Time): RangeStats | null {
  const tA = timeToMs(ta);
  const tB = timeToMs(tb);
  const t1 = Math.min(tA, tB);
  const t2 = Math.max(tA, tB);
  const idxs: number[] = [];
  for (let i = 0; i < data.length; i++) {
    const t = timeToMs(data[i].time);
    if (t >= t1 && t <= t2) idxs.push(i);
  }
  if (idxs.length === 0) return null;
  const first = data[idxs[0]];
  const last = data[idxs[idxs.length - 1]];
  let hi = -Infinity, lo = Infinity, vol = 0, amt = 0;
  for (const i of idxs) {
    const d = data[i];
    if (d.high > hi) hi = d.high;
    if (d.low > 0 && d.low < lo) lo = d.low;
    vol += d.volume || 0;
    amt += d.amount || d.close * (d.volume || 0) * 100;
  }
  const preIdx = idxs[0] - 1;
  const base = preIdx >= 0 ? data[preIdx].close : first.open;
  const changeVal = last.close - base;
  const changePct = base > 0 ? (changeVal / base) * 100 : 0;
  return {
    startTime: String(first.time).slice(0, 10),
    endTime: String(last.time).slice(0, 10),
    bars: idxs.length,
    open: first.open, close: last.close, high: hi, low: lo,
    changePct, changeVal, volume: vol, amount: amt,
  };
}

function valueAtTime<T extends { time: Time; value: number }>(points: T[], time: Time | null): number | null {
  if (time == null) return null;
  const key = String(time);
  const point = points.find(p => String(p.time) === key);
  return point && Number.isFinite(point.value) ? point.value : null;
}

function formatIndicatorValue(value: number | null | undefined, digits = 2): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(digits) : '--';
}

function formatCompactVolume(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--';
  const abs = Math.abs(value);
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (abs >= 1e6) return `${(value / 1e6).toFixed(2)}M`;
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
}

function formatSignedCompactAmount(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--';
  const sign = value > 0 ? '+' : '';
  return `${sign}${formatCompactVolume(value)}`;
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function findKLineByCandleHit(
  chart: IChartApi,
  series: ISeriesApi<SeriesType, Time> | null,
  data: KLineData[],
  point: { x: number; y: number },
): KLineData | null {
  if (!series) return null;
  const time = chart.timeScale().coordinateToTime(point.x);
  const item = time ? findKLineByChartTime(data, time) : null;
  if (!item) return null;

  const centerX = chart.timeScale().timeToCoordinate(parseTime(item.time));
  const highY = series.priceToCoordinate(item.high);
  const lowY = series.priceToCoordinate(item.low);
  const openY = series.priceToCoordinate(item.open);
  const closeY = series.priceToCoordinate(item.close);
  if (
    centerX == null
    || highY == null
    || lowY == null
    || openY == null
    || closeY == null
  ) return null;

  const barSpacing = chart.timeScale().options().barSpacing;
  const bodyHalfWidth = clampNumber(barSpacing * 0.36, 4, 18);
  const wickHalfWidth = clampNumber(barSpacing * 0.1, 2.5, 5);
  const yPadding = 3;
  const xDistance = Math.abs(point.x - centerX);
  const bodyTop = Math.min(openY, closeY);
  const bodyBottom = Math.max(openY, closeY);
  const wickTop = Math.min(highY, lowY);
  const wickBottom = Math.max(highY, lowY);
  const isBodyHit = xDistance <= bodyHalfWidth
    && point.y >= bodyTop - yPadding
    && point.y <= bodyBottom + yPadding;
  const isWickHit = xDistance <= wickHalfWidth
    && point.y >= wickTop - yPadding
    && point.y <= wickBottom + yPadding;

  return isBodyHit || isWickHit ? item : null;
}

function toFiniteNumber(value: unknown): number | null {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null;
  if (typeof value === 'string') {
    const normalized = value.replace(/,/g, '').trim();
    if (!normalized) return null;
    const parsed = Number(normalized);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function getOptionalKLineNumber(data: KLineData, keys: string[]): number | null {
  const record = data as unknown as Record<string, unknown>;
  for (const key of keys) {
    const value = toFiniteNumber(record[key]);
    if (value != null) return value;
  }
  return null;
}

function latestLineValue<T extends { time: Time; value: number }>(points: T[], time: Time | null): number | null {
  const exact = valueAtTime(points, time);
  if (exact != null) return exact;
  for (let i = points.length - 1; i >= 0; i -= 1) {
    const value = points[i]?.value;
    if (Number.isFinite(value)) return value;
  }
  return null;
}

function latestFundValue(points: FundFlowPoint[], time: Time | null): FundFlowPoint | null {
  if (points.length === 0) return null;
  if (time != null) {
    const key = String(time);
    const exact = points.find(point => String(point.time) === key);
    return exact || null;
  }
  return points[points.length - 1];
}

function simpleMovingNumbers(values: number[], period: number): number[] {
  const result = Array(values.length).fill(NaN);
  if (period <= 0) return result;
  let sum = 0;
  for (let i = 0; i < values.length; i += 1) {
    sum += values[i] || 0;
    if (i >= period) sum -= values[i - period] || 0;
    if (i >= period - 1) result[i] = sum / period;
  }
  return result;
}

function emaNumbers(values: number[], period: number): number[] {
  const result = Array(values.length).fill(NaN);
  if (values.length === 0 || period <= 0) return result;
  const k = 2 / (period + 1);
  let ema = values[0] || 0;
  result[0] = ema;
  for (let i = 1; i < values.length; i += 1) {
    const value = Number.isFinite(values[i]) ? values[i] : ema;
    ema = value * k + ema * (1 - k);
    result[i] = ema;
  }
  return result;
}

function tdxSmaNumbers(values: number[], period: number, weight = 1): number[] {
  const result = Array(values.length).fill(NaN);
  if (values.length === 0 || period <= 0) return result;
  let prev = Number.isFinite(values[0]) ? values[0] : 0;
  result[0] = prev;
  for (let i = 1; i < values.length; i += 1) {
    const value = Number.isFinite(values[i]) ? values[i] : prev;
    prev = (weight * value + (period - weight) * prev) / period;
    result[i] = prev;
  }
  return result;
}

function rollingHighest(values: number[], period: number): number[] {
  return values.map((_, index) => {
    const start = Math.max(0, index - period + 1);
    let max = -Infinity;
    for (let i = start; i <= index; i += 1) {
      if (Number.isFinite(values[i])) max = Math.max(max, values[i]);
    }
    return max === -Infinity ? NaN : max;
  });
}

function rollingLowest(values: number[], period: number): number[] {
  return values.map((_, index) => {
    const start = Math.max(0, index - period + 1);
    let min = Infinity;
    for (let i = start; i <= index; i += 1) {
      if (Number.isFinite(values[i])) min = Math.min(min, values[i]);
    }
    return min === Infinity ? NaN : min;
  });
}

function rollingAveDev(values: number[], period: number): number[] {
  return values.map((_, index) => {
    if (index < period - 1) return NaN;
    const start = index - period + 1;
    let sum = 0;
    for (let i = start; i <= index; i += 1) sum += values[i] || 0;
    const avg = sum / period;
    let dev = 0;
    for (let i = start; i <= index; i += 1) dev += Math.abs((values[i] || 0) - avg);
    return dev / period;
  });
}

function rollingStd(values: number[], period: number): number[] {
  return values.map((_, index) => {
    if (index < period - 1) return NaN;
    const start = index - period + 1;
    let sum = 0;
    for (let i = start; i <= index; i += 1) sum += values[i] || 0;
    const avg = sum / period;
    let variance = 0;
    for (let i = start; i <= index; i += 1) variance += Math.pow((values[i] || 0) - avg, 2);
    return Math.sqrt(variance / period);
  });
}

function refValue(values: number[], index: number, offset: number): number {
  const refIndex = index - offset;
  if (refIndex < 0 || refIndex >= values.length) return NaN;
  return values[refIndex];
}

function crossUpValues(a: number[], b: number[], index: number): boolean {
  if (index <= 0) return false;
  return Number.isFinite(a[index])
    && Number.isFinite(b[index])
    && Number.isFinite(a[index - 1])
    && Number.isFinite(b[index - 1])
    && a[index] > b[index]
    && a[index - 1] <= b[index - 1];
}

function countTrueSince(values: boolean[], start: number, end: number): number {
  let count = 0;
  for (let i = Math.max(0, start); i <= end; i += 1) {
    if (values[i]) count += 1;
  }
  return count;
}

function lastTrueIndex(values: boolean[], index: number): number {
  for (let i = index; i >= 0; i -= 1) {
    if (values[i]) return i;
  }
  return -1;
}

function existTrue(values: boolean[], index: number, period: number): boolean {
  const start = Math.max(0, index - period + 1);
  for (let i = start; i <= index; i += 1) {
    if (values[i]) return true;
  }
  return false;
}

function lineDataFromValues(data: KLineData[], values: number[]): LineData[] {
  return values
    .map((value, index) => ({ time: parseTime(data[index].time), value }))
    .filter(point => Number.isFinite(point.value));
}

export function buildVipAnomalySeries(data: KLineData[]): VipAnomalySeries {
  if (data.length === 0) {
    return { points: [], startPrice: 0, endPrice: 0 };
  }

  const close = data.map(item => item.close || 0);
  const high = data.map(item => item.high || item.close || 0);
  const low = data.map(item => item.low || item.close || 0);
  const volume = data.map(item => item.volume || 0);
  const amount = data.map(item => item.amount || item.close * (item.volume || 0) * 100);
  const tp3 = data.map(item => (item.close + item.high + item.low) / 3);

  const ema1 = emaNumbers(close, 2);
  const ema2 = emaNumbers(ema1, 2);
  const ema3 = emaNumbers(ema2, 2);
  const fastSlowLine = emaNumbers(ema3, 2);
  const slowFastLine = emaNumbers(fastSlowLine.map((_, index) => refValue(fastSlowLine, index, 1)), 2);
  const lifeLine = emaNumbers(emaNumbers(close, 8), 13);
  const abc4 = emaNumbers(tp3, 5);

  const sz1 = emaNumbers(close, 10);
  const llvSz1 = rollingLowest(sz1, 10);
  const sz2 = sz1.map((value, index) => llvSz1[index] > 0 ? (value / llvSz1[index] - 1) * 100 : 0);
  const sz3 = sz2.map(value => -value);
  const hhv14 = rollingHighest(high, 14);
  const llvLow14 = rollingLowest(low, 14);
  const sz4 = close.map((value, index) => {
    const range = hhv14[index] - llvLow14[index];
    return range !== 0 ? 100 * (hhv14[index] - value) / range : 0;
  });
  const diffClose = close.map((value, index) => index === 0 ? 0 : value - close[index - 1]);
  const gain = diffClose.map(value => Math.max(value, 0));
  const absDiff = diffClose.map(value => Math.abs(value));
  const sz6Gain = tdxSmaNumbers(gain, 9, 1);
  const sz6Abs = tdxSmaNumbers(absDiff, 9, 1);
  const sz6 = sz6Gain.map((value, index) => sz6Abs[index] !== 0 ? value / sz6Abs[index] * 100 : 0);
  const hhv9 = rollingHighest(high, 9);
  const llvLow9 = rollingLowest(low, 9);
  const sz7 = close.map((value, index) => {
    const range = hhv9[index] - llvLow9[index];
    return range !== 0 ? (value - llvLow9[index]) / range * 100 : 0;
  });
  const sz8 = tdxSmaNumbers(sz7, 3, 1);
  const sz9 = tdxSmaNumbers(sz8, 3, 1);
  const sz10 = sz8.map((value, index) => 3 * value - 2 * sz9[index]);
  const sz11 = close.map((_, index) => sz6[index] >= 76 || sz10[index] > 95);
  const sz12 = sz4.map(value => value < 28);
  const sz13 = sz11.map((value, index) => value || sz12[index]);

  const sz14 = emaNumbers(close, 20);
  const sz15 = emaNumbers(close, 30);
  const sz16 = emaNumbers(close, 35);
  const sz17 = emaNumbers(close, 40);
  const sz18 = emaNumbers(close, 45);
  const sz19 = emaNumbers(close, 90);
  const sz20 = emaNumbers(close, 98);
  const sz21 = emaNumbers(close, 106);
  const sz22 = emaNumbers(close, 114);
  const sz23 = emaNumbers(close, 140);
  const sz24 = emaNumbers(close, 148);
  const sz25 = emaNumbers(close, 156);
  const sz26 = emaNumbers(close, 164);

  const sz27 = close.map((_, index) => Math.max(sz15[index], sz16[index], sz17[index], sz18[index]));
  const sz28 = close.map((_, index) => Math.min(sz15[index], sz16[index], sz17[index], sz18[index]));
  const sz29 = close.map((_, index) => Math.max(sz19[index], sz20[index], sz21[index], sz22[index]));
  const sz30 = close.map((_, index) => Math.min(sz19[index], sz20[index], sz21[index], sz22[index]));
  const sz31 = close.map((_, index) => Math.max(sz23[index], sz24[index], sz25[index], sz26[index]));
  const sz32 = close.map((_, index) => Math.min(sz23[index], sz24[index], sz25[index], sz26[index]));
  const sz33 = close.map((_, index) => Math.max(sz27[index], sz29[index], sz31[index]));
  const sz34 = close.map((_, index) => Math.min(sz28[index], sz30[index], sz32[index]));
  const sz35 = sz33.map((value, index) => sz34[index] !== 0 ? value / sz34[index] : Infinity);
  const sz36 = low.map((value, index) => sz27[index] !== 0 ? value / sz27[index] : Infinity);
  const sz37 = close.map((value, index) => (
    sz35[index] < 1.9
    && refValue(sz36, index, 1) < 1.12
    && value > sz14[index]
    && value > sz33[index]
    && sz36[index] < 1.18
  ));
  const sz38 = amount.map(value => value / 10000);
  const llvSz38Two = rollingLowest(sz38, 2);
  const sz39 = sz38.map((value, index) => llvSz38Two[index] > 200 && value > 800);
  const sz40 = close.map((value, index) => index > 0 && close[index - 1] !== 0 ? value / close[index - 1] : 1);

  const sz41 = emaNumbers(close, 5);
  const sz42 = emaNumbers(close, 10);
  const sz43 = emaNumbers(close, 20);
  const sz44 = close.map((_, index) => Math.max(sz41[index], sz42[index], sz43[index]));
  const sz45 = close.map((_, index) => Math.min(sz41[index], sz42[index], sz43[index]));
  const sz46 = close.map((value, index) => (
    low[index] < sz45[index]
    && value > sz44[index]
    && volume[index] > refValue(volume, index, 1) * 1.05
    && sz39[index]
    && sz40[index] > 1.03
    && sz37[index]
  ));

  const sz47 = tp3;
  const ma81 = simpleMovingNumbers(sz47, 81);
  const aveDev81 = rollingAveDev(sz47, 81);
  const sz48 = sz47.map((value, index) => aveDev81[index] !== 0 ? (value - ma81[index]) * 1000 / (15 * aveDev81[index]) : NaN);
  const sz49 = close.map((_, index) => (
    crossUpValues(sz48, Array(data.length).fill(100), index)
    && volume[index] > refValue(volume, index, 1) * 1.05
    && sz39[index]
    && sz40[index] > 1.03
    && sz37[index]
  ));

  const sz50 = simpleMovingNumbers(close, 30);
  const sz51 = simpleMovingNumbers(close, 60);
  const sz52 = simpleMovingNumbers(close, 90);
  const sz53 = simpleMovingNumbers(close, 240);
  const sz54 = close.map((_, index) => Math.abs(sz50[index] / sz51[index] - 1));
  const sz55 = close.map((_, index) => Math.abs(sz51[index] / sz52[index] - 1));
  const sz56 = close.map((_, index) => Math.abs(sz50[index] / sz52[index] - 1));
  const sz58 = sz40.map(value => value - 1);
  const sz59 = close.map((_, index) => (sz50[index] + sz51[index] + sz52[index]) / 3);
  const sz60 = close.map((value, index) => value > sz59[index] * 1.00 && value < sz59[index] * 1.35);
  const sz61 = sz53.map((value, index) => {
    const ref = refValue(sz53, index, 20);
    return ref !== 0 ? value / ref : NaN;
  });
  const sz62 = sz61.map(value => Math.abs(value - 1));
  const sz63 = sz62.map(value => value < 0.12);
  const sz64 = close.map((_, index) => (
    sz54[index] < 0.12
    && sz55[index] < 0.12
    && sz56[index] < 0.12
    && sz58[index] > 0.02
    && sz60[index]
    && sz63[index]
    && sz59[index] > sz53[index]
  ));
  const sz65 = close.map((_, index) => sz64[index] && volume[index] > refValue(volume, index, 1) * 1.05 && sz39[index] && sz37[index]);
  const sz66 = close.map((value, index) => (
    sz35[index] < 1.45
    && refValue(sz36, index, 1) < 1.12
    && value > sz14[index]
    && value > sz33[index]
    && sz36[index] < 1.18
    && sz40[index] > 1.03
    && volume[index] > refValue(volume, index, 1) * 1.05
    && sz39[index]
    && sz37[index]
  ));
  const sz67 = close.map((value, index) => (
    low[index] < sz34[index]
    && value > sz33[index]
    && sz40[index] > 1.03
    && volume[index] > refValue(volume, index, 1) * 1.05
  ));

  // 6.0 鱼头/鱼尾门控:仅红彩带多头区间触发鱼头(与主图彩带同口径),鱼尾自动屏蔽
  const a13 = emaNumbers(tp3, 13);
  const abc2 = emaNumbers(tp3, 14);
  const strong = a13.map((value, index) => value > refValue(a13, index, 1) && lifeLine[index] < abc2[index]);
  const maBull = close.map((_, index) => (
    sz41[index] > refValue(sz41, index, 1) && sz42[index] > refValue(sz41, index, 1) // 按源码原样:S42>REF(S41,1)
  ));
  const sz68 = close.map((_, index) => (
    (sz46[index] || sz49[index] || sz65[index] || sz66[index] || sz67[index])
    && maBull[index]
    && sz40[index] > 1.03
    && existTrue(strong, index, 3)
    && strong[index]
  ));
  const sz71 = sz68.map((value, index) => (value ? sz2[index] : 0));

  // 6.0 逃顶止盈模块:55日随机值趋势>=90拐头(10日去重) 或 短均差能量峰值回落
  const hhv55 = rollingHighest(high, 55);
  const llv55 = rollingLowest(low, 55);
  const rsv55 = close.map((value, index) => {
    const range = hhv55[index] - llv55[index];
    return range !== 0 ? (value - llv55[index]) / range * 100 : 0;
  });
  const rsv55Sma = tdxSmaNumbers(rsv55, 5, 1);
  const rsv55Sma2 = tdxSmaNumbers(rsv55Sma, 3, 1);
  const var113 = rsv55Sma.map((value, index) => 3 * value - 2 * rsv55Sma2[index]);
  const trendLine = emaNumbers(var113, 3).map(value => value - 10);
  const trendTurn = trendLine.map((value, index) => {
    const prev = refValue(trendLine, index, 1);
    return prev !== 0 && Number.isFinite(prev) ? (value - prev) / prev * 100 : 0;
  });
  const var116: boolean[] = new Array(data.length).fill(false);
  {
    let cool = -1;
    for (let index = 0; index < data.length; index += 1) {
      if (index > cool && trendLine[index] >= 90 && trendTurn[index] < 0) {
        var116[index] = true;
        cool = index + 10;
      }
    }
  }
  const ma1to9 = [1, 3, 5, 7, 9].map(n => simpleMovingNumbers(close, n));
  const ma2to10 = [2, 4, 6, 8, 10].map(n => simpleMovingNumbers(close, n));
  const var13 = close.map((_, index) => ma1to9.reduce((sum, arr) => sum + arr[index], 0) / 5);
  const var14 = close.map((_, index) => ma2to10.reduce((sum, arr) => sum + arr[index], 0) / 5);
  const var13Ema = emaNumbers(var13, 2);
  const var14Ema = emaNumbers(var14, 5);
  const var16 = var13Ema.map((value, index) => Math.max(value - var14Ema[index], 0) * 200);
  const var17 = emaNumbers(var16, 5);
  const reduceSignal = var17.map((value, index) => (
    value < refValue(var17, index, 1)
    && refValue(var17, index, 1) >= refValue(var17, index, 2)
    && value > 0
  ));
  const escape = var116.map((value, index) => value || reduceSignal[index]);

  const eatFish = close.map((value, index) => {
    const crossLife = crossUpValues(abc4, lifeLine, index);
    const prevNearLife = refValue(abc4, index, 1) < lifeLine[index] * 1.02;
    return (crossLife || (abc4[index] > lifeLine[index] && prevNearLife))
      && fastSlowLine[index] > slowFastLine[index]
      && value > fastSlowLine[index];
  });

  const newWave = sz68.map((value, index) => value && (index === 0 || !sz68[index - 1]));
  let lastWaveStart = -1;
  const superCondition = close.map((_, index) => {
    if (newWave[index]) lastWaveStart = index;
    return lastWaveStart >= 0 && index - lastWaveStart >= 1 && sz40[index] > 1.03;
  });

  lastWaveStart = -1;
  const points = data.map((item, index) => {
    if (newWave[index]) lastWaveStart = index;
    const superCount = lastWaveStart >= 0 ? countTrueSince(superCondition, lastWaveStart, index) : 0;
    const anomaly = sz68[index];
    const fire = anomaly && sz40[index] > 1.03;
    const multiplier = anomaly ? 6 : 4;
    return {
      time: parseTime(item.time),
      rawTime: item.time,
      sz2: Number.isFinite(sz2[index]) ? sz2[index] : 0,
      sz3: Number.isFinite(sz3[index]) ? sz3[index] : 0,
      sz13: sz13[index],
      eatFish: eatFish[index],
      anomaly,
      fire,
      superSignal: superCondition[index],
      superCount,
      purpleTop: sz71[index] * multiplier,
      purpleBottom: -sz71[index] * multiplier,
      escape: escape[index],
    };
  });

  return {
    points,
    latest: points[points.length - 1],
    startPrice: close[0] || 0,
    endPrice: close[close.length - 1] || 0,
  };
}

export function buildOpenEatFishSeries(data: KLineData[]): OpenEatFishSeries {
  if (data.length === 0) {
    return { points: [], ma5: [], ma10: [], ma20: [], ma30: [], ma60: [], lowerLimit: [], upperBound: [], lowerBound: [] };
  }

  const close = data.map(item => item.close || 0);
  const open = data.map(item => item.open || item.close || 0);
  const high = data.map(item => item.high || item.close || 0);
  const low = data.map(item => item.low || item.close || 0);
  const volume = data.map(item => item.volume || 0);
  const tp3 = data.map(item => (item.close + item.high + item.low) / 3);

  const ma5 = simpleMovingNumbers(close, 5);
  const ma10 = simpleMovingNumbers(close, 10);
  const ma20 = simpleMovingNumbers(close, 20);
  const ma30 = simpleMovingNumbers(close, 30);
  const ma60 = simpleMovingNumbers(close, 60);
  const mid = simpleMovingNumbers(close, 26);
  const std20 = rollingStd(close, 20);
  const lowerLimit = close.map((_, index) => mid[index] - 2 * std20[index]);

  const var1 = close.map((value, index) => index > 0 && value > close[index - 1]);
  const var2 = close.map((value, index) => Boolean(var1[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var3 = close.map((value, index) => Boolean(var2[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var4 = close.map((value, index) => Boolean(var3[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var5 = close.map((value, index) => Boolean(var4[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var6 = close.map((value, index) => Boolean(var5[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var7 = close.map((value, index) => Boolean(var6[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var8 = close.map((value, index) => Boolean(var7[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var9 = close.map((value, index) => Boolean(var8[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const varA = close.map((value, index) => Boolean(var9[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const varB = close.map((value, index) => Boolean(varA[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const varC = close.map((value, index) => Boolean(varB[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const holdStock = close.map((_, index) => (
    var1[index] || var2[index] || var3[index] || var4[index] || var5[index] || var6[index] ||
    var7[index] || var8[index] || var9[index] || varA[index] || varB[index] || varC[index]
  ));

  const varD = close.map((value, index) => index > 1 && value < close[index - 1] && value < close[index - 2]);
  const varE = close.map((value, index) => Boolean(varD[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const varF = close.map((value, index) => Boolean(varE[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var10 = close.map((value, index) => Boolean(varF[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var11 = close.map((value, index) => Boolean(var10[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var12 = close.map((value, index) => Boolean(var11[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var13 = close.map((value, index) => Boolean(var12[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var14 = close.map((value, index) => Boolean(var13[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var15 = close.map((value, index) => Boolean(var14[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var16 = close.map((value, index) => Boolean(var15[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const var17 = close.map((value, index) => Boolean(var16[index - 1]) && value <= close[index - 1] && value >= refValue(close, index, 2));
  const var18 = close.map((value, index) => Boolean(var17[index - 1]) && value >= close[index - 1] && value <= refValue(close, index, 2));
  const holdCash = close.map((_, index) => (
    varD[index] || varE[index] || varF[index] || var10[index] || var11[index] || var12[index] ||
    var13[index] || var14[index] || var15[index] || var16[index] || var17[index] || var18[index]
  ));
  const var19 = close.map((_, index) => Boolean(holdCash[index - 1]) && var1[index]);
  const var1A = close.map((_, index) => Boolean(holdStock[index - 1]) && varD[index]);
  const vol5 = simpleMovingNumbers(volume, 5);
  const breakTakeProfit = close.map((_, index) => (
    crossUpValues(ma10, close, index)
    && refValue(close, index, 1) > refValue(ma5, index, 1)
    && volume[index] > vol5[index] * 1.2
    && Number.isFinite(vol5[index])
  ));
  const takeProfit = close.map((_, index) => var1A[index] || breakTakeProfit[index]);

  const ema1 = emaNumbers(close, 2);
  const ema2 = emaNumbers(ema1, 2);
  const ema3 = emaNumbers(ema2, 2);
  const fastSlowLine = emaNumbers(ema3, 2);
  const slowFastLine = emaNumbers(fastSlowLine.map((_, index) => refValue(fastSlowLine, index, 1)), 2);
  const lifeLine = emaNumbers(emaNumbers(close, 8), 13);
  const abc2 = emaNumbers(tp3, 14);
  const abc4 = emaNumbers(tp3, 5);
  const eatFish = close.map((value, index) => {
    const crossLife = crossUpValues(abc4, lifeLine, index);
    const prevNearLife = refValue(abc4, index, 1) < lifeLine[index] * 1.02;
    return (crossLife || (abc4[index] > lifeLine[index] && prevNearLife))
      && fastSlowLine[index] > slowFastLine[index]
      && value > fastSlowLine[index];
  });

  const jj = tp3;
  const a = emaNumbers(jj, 13);
  const b = a.map((_, index) => refValue(a, index, 1));
  const trendHoldStock = a.map((value, index) => value > b[index]);
  const strong = a.map((value, index) => value > b[index] && lifeLine[index] < abc2[index]);
  const allowShortBuy = strong.map((isStrong, index) => isStrong && trendHoldStock[index] && existTrue(eatFish, index, 60));
  const openEatFish = close.map((_, index) => var19[index] && allowShortBuy[index]);

  const upperBound = high.map((value, index) => Math.max(
    value,
    close[index],
    ma5[index] || value,
    ma10[index] || value,
    ma20[index] || value,
    ma30[index] || value,
    ma60[index] || value,
    lowerLimit[index] || value,
    abc2[index] || value,
    lifeLine[index] || value,
    value * 1.12 + 0.18,
    value * 1.15,
  ));

  const points = data.map((item, index) => ({
    time: parseTime(item.time),
    rawTime: item.time,
    open: open[index],
    high: high[index],
    low: low[index],
    close: close[index],
    ma5: ma5[index],
    ma10: ma10[index],
    ma20: ma20[index],
    ma30: ma30[index],
    ma60: ma60[index],
    lowerLimit: lowerLimit[index],
    lifeLine: lifeLine[index],
    abc2: abc2[index],
    a: a[index],
    b: b[index],
    strong: strong[index],
    weak: !strong[index],
    eatFish: eatFish[index],
    openEatFish: openEatFish[index],
    takeProfit: takeProfit[index],
    breakTakeProfit: breakTakeProfit[index],
    base: high[index] * 1.12,
  }));

  // 透明撑底:止盈钻石画在 MIN(ABC2,生命价线)×0.86,需纳入自动缩放,否则被裁出可视区
  const lowerBound = low.map((value, index) => {
    const ribbonFloor = Number.isFinite(abc2[index]) && Number.isFinite(lifeLine[index])
      ? Math.min(abc2[index], lifeLine[index]) * 0.855
      : value;
    return Math.min(value, Number.isFinite(lowerLimit[index]) ? lowerLimit[index] : value, ribbonFloor);
  });

  return {
    points,
    latest: points[points.length - 1],
    ma5: lineDataFromValues(data, ma5),
    ma10: lineDataFromValues(data, ma10),
    ma20: lineDataFromValues(data, ma20),
    ma30: lineDataFromValues(data, ma30),
    ma60: lineDataFromValues(data, ma60),
    lowerLimit: lineDataFromValues(data, lowerLimit),
    upperBound: lineDataFromValues(data, upperBound),
    lowerBound: lineDataFromValues(data, lowerBound),
  };
}

function getVipBaseEnergyColor(point: VipAnomalyPoint, prevPoint?: VipAnomalyPoint): string {
  const prev = prevPoint ? prevPoint.sz2 * 2 : point.sz2 * 2;
  const current = point.sz2 * 2;
  const rising = current > prev;
  if (current > 20 && rising) return VIP_COLORS.hot;
  if (rising) return VIP_COLORS.rising;
  return VIP_COLORS.gray;
}

export function buildVipShortEnergySeries(data: KLineData[]): VipShortEnergySeries {
  if (data.length === 0) {
    return { points: [], startPrice: 0, endPrice: 0 };
  }

  const close = data.map(item => item.close || 0);
  const high = data.map(item => item.high || item.close || 0);
  const low = data.map(item => item.low || item.close || 0);
  const volume = data.map(item => item.volume || 0);
  const amount = data.map(item => item.amount || item.close * (item.volume || 0) * 100);
  const tp3 = data.map(item => (item.close + item.high + item.low) / 3);

  const sz1 = emaNumbers(close, 10);
  const llvSz1 = rollingLowest(sz1, 10);
  const sz2 = sz1.map((value, index) => llvSz1[index] > 0 ? (value / llvSz1[index] - 1) * 100 : 0);
  const sz3 = sz2.map(value => -value);
  const hhv14 = rollingHighest(high, 14);
  const llvLow14 = rollingLowest(low, 14);
  const sz4 = close.map((value, index) => {
    const range = hhv14[index] - llvLow14[index];
    return range !== 0 ? 100 * (hhv14[index] - value) / range : 0;
  });
  const diffClose = close.map((value, index) => index === 0 ? 0 : value - close[index - 1]);
  const gain = diffClose.map(value => Math.max(value, 0));
  const absDiff = diffClose.map(value => Math.abs(value));
  const sz6Gain = tdxSmaNumbers(gain, 9, 1);
  const sz6Abs = tdxSmaNumbers(absDiff, 9, 1);
  const sz6 = sz6Gain.map((value, index) => sz6Abs[index] !== 0 ? value / sz6Abs[index] * 100 : 0);
  const hhv9 = rollingHighest(high, 9);
  const llvLow9 = rollingLowest(low, 9);
  const sz7 = close.map((value, index) => {
    const range = hhv9[index] - llvLow9[index];
    return range !== 0 ? (value - llvLow9[index]) / range * 100 : 0;
  });
  const sz8 = tdxSmaNumbers(sz7, 3, 1);
  const sz9 = tdxSmaNumbers(sz8, 3, 1);
  const sz10 = sz8.map((value, index) => 3 * value - 2 * sz9[index]);
  const sz11 = close.map((_, index) => sz6[index] >= 76 || sz10[index] > 95);
  const sz12 = sz4.map(value => value < 25);
  const sz13 = sz11.map((value, index) => value || sz12[index]);

  const sz14 = emaNumbers(close, 20);
  const sz15 = emaNumbers(close, 30);
  const sz16 = emaNumbers(close, 35);
  const sz17 = emaNumbers(close, 40);
  const sz18 = emaNumbers(close, 45);
  const sz19 = emaNumbers(close, 9);
  const sz20 = emaNumbers(close, 98);
  const sz21 = emaNumbers(close, 106);
  const sz22 = emaNumbers(close, 114);
  const sz23 = emaNumbers(close, 140);
  const sz24 = emaNumbers(close, 148);
  const sz25 = emaNumbers(close, 156);
  const sz26 = emaNumbers(close, 164);

  const sz27 = close.map((_, index) => Math.max(sz15[index], sz16[index], sz17[index], sz18[index]));
  const sz28 = close.map((_, index) => Math.min(sz15[index], sz16[index], sz17[index], sz18[index]));
  const sz29 = close.map((_, index) => Math.max(sz19[index], sz20[index], sz21[index], sz22[index]));
  const sz30 = close.map((_, index) => Math.min(sz19[index], sz20[index], sz21[index], sz22[index]));
  const sz31 = close.map((_, index) => Math.max(sz23[index], sz24[index], sz25[index], sz26[index]));
  const sz32 = close.map((_, index) => Math.min(sz23[index], sz24[index], sz25[index], sz26[index]));
  const sz33 = close.map((_, index) => Math.max(sz27[index], sz29[index], sz31[index]));
  const sz34 = close.map((_, index) => Math.min(sz28[index], sz30[index], sz32[index], sz28[index]));
  const sz35 = sz33.map((value, index) => sz34[index] !== 0 ? value / sz34[index] : Infinity);
  const sz36 = low.map((value, index) => sz27[index] !== 0 ? value / sz27[index] : Infinity);
  const sz37 = close.map((value, index) => (
    sz35[index] < 1.3
    && refValue(sz36, index, 1) < 1.05
    && value > sz14[index]
    && value > sz33[index]
    && sz36[index] < 1.08
  ));
  const sz38 = amount.map(value => value / 10000);
  const llvSz38Two = rollingLowest(sz38, 2);
  const sz39 = sz38.map((value, index) => llvSz38Two[index] > 2000 && value > 8000);
  const sz40 = close.map((value, index) => index > 0 && close[index - 1] !== 0 ? value / close[index - 1] : 1);

  const sz41 = emaNumbers(close, 5);
  const sz42 = emaNumbers(close, 10);
  const sz43 = emaNumbers(close, 20);
  const sz44 = close.map((_, index) => Math.max(sz41[index], sz42[index], sz43[index]));
  const sz45 = close.map((_, index) => Math.min(sz41[index], sz42[index], sz43[index]));
  const sz46 = close.map((value, index) => (
    low[index] < sz45[index]
    && value > sz44[index]
    && volume[index] > refValue(volume, index, 1) * 1.2
    && sz39[index]
    && sz40[index] > 1.029
    && sz37[index]
  ));

  const ma81 = simpleMovingNumbers(tp3, 81);
  const aveDev81 = rollingAveDev(tp3, 81);
  const sz48 = tp3.map((value, index) => aveDev81[index] !== 0 ? (value - ma81[index]) * 1000 / (15 * aveDev81[index]) : NaN);
  const sz49 = close.map((_, index) => (
    crossUpValues(sz48, Array(data.length).fill(100), index)
    && volume[index] > refValue(volume, index, 1) * 1.2
    && sz39[index]
    && sz40[index] > 1.029
    && sz37[index]
  ));

  const sz50 = simpleMovingNumbers(close, 30);
  const sz51 = simpleMovingNumbers(close, 60);
  const sz52 = simpleMovingNumbers(close, 90);
  const sz53 = simpleMovingNumbers(close, 240);
  const sz54 = close.map((_, index) => Math.abs(sz50[index] / sz51[index] - 1));
  const sz55 = close.map((_, index) => Math.abs(sz51[index] / sz52[index] - 1));
  const sz56 = close.map((_, index) => Math.abs(sz50[index] / sz52[index] - 1));
  const sz58 = sz40.map(value => value - 1);
  const sz59 = close.map((_, index) => (sz50[index] + sz51[index] + sz52[index]) / 3);
  const sz60 = close.map((value, index) => value > sz59[index] * 1.04 && value < sz59[index] * 1.15);
  const sz61 = sz53.map((value, index) => {
    const ref = refValue(sz53, index, 20);
    return ref !== 0 ? value / ref : NaN;
  });
  const sz62 = sz61.map(value => Math.abs(value - 1));
  const sz63 = sz62.map(value => value < 0.04);
  const sz64 = close.map((_, index) => (
    sz54[index] < 0.04
    && sz55[index] < 0.04
    && sz56[index] < 0.04
    && sz58[index] > 0.04
    && sz60[index]
    && sz63[index]
    && sz59[index] > sz53[index]
  ));
  const sz65 = close.map((_, index) => sz64[index] && volume[index] > refValue(volume, index, 1) * 1.2 && sz39[index] && sz37[index]);
  const sz66 = close.map((value, index) => (
    sz35[index] < 1.15
    && refValue(sz36, index, 1) < 1.04
    && value > sz14[index]
    && value > sz33[index]
    && sz36[index] < 1.08
    && sz40[index] > 1.04
    && volume[index] > refValue(volume, index, 1) * 1.2
    && sz39[index]
    && sz37[index]
  ));
  const sz67 = close.map((value, index) => (
    low[index] < sz34[index]
    && value > sz33[index]
    && sz40[index] > 1.05
    && volume[index] > refValue(volume, index, 1) * 1.2
  ));
  const anomaly = close.map((_, index) => sz46[index] || sz49[index] || sz65[index] || sz66[index] || sz67[index]);
  const strongCondition = close.map((_, index) => {
    const lastAnomaly = lastTrueIndex(anomaly, index);
    const n1 = lastAnomaly >= 0 ? index - lastAnomaly : Infinity;
    return lastAnomaly >= 0 && n1 >= 1 && n1 <= 50 && sz40[index] > 1.05;
  });

  const var1 = emaNumbers(emaNumbers(close, 9), 9);
  const control = var1.map((value, index) => {
    const prev = refValue(var1, index, 1);
    return prev !== 0 ? (value - prev) / prev * 1000 : 0;
  });
  const controlScaled = control.map(value => value * 40);
  const startControl = control.map((_, index) => crossUpValues(control, Array(data.length).fill(0), index));
  const mainShip = control.map((value, index) => value < refValue(control, index, 1) && value > 0);
  const mainShipFirst = mainShip.map((value, index) => value && (index === 0 || !mainShip[index - 1]));

  const ema12 = emaNumbers(close, 12);
  const ema26 = emaNumbers(close, 26);
  const dif = ema12.map((value, index) => value - ema26[index]);
  const dea = emaNumbers(dif, 9);
  const macdGolden = close.map((_, index) => crossUpValues(dif, dea, index));
  const macdDead = close.map((_, index) => crossUpValues(dea, dif, index));

  const points = data.map((item, index) => {
    const lastAnomaly = lastTrueIndex(anomaly, index);
    const countStart = lastAnomaly >= 0 ? lastAnomaly : index;
    const strongCount = lastAnomaly >= 0 ? countTrueSince(strongCondition, countStart, index) : 0;
    return {
      time: parseTime(item.time),
      rawTime: item.time,
      sz2: Number.isFinite(sz2[index]) ? sz2[index] : 0,
      sz3: Number.isFinite(sz3[index]) ? sz3[index] : 0,
      sz13: sz13[index],
      anomaly: anomaly[index],
      fire: anomaly[index] && sz40[index] > 1.04,
      strongCondition: strongCondition[index],
      strongCount,
      control: Number.isFinite(control[index]) ? control[index] : 0,
      controlScaled: Number.isFinite(controlScaled[index]) ? controlScaled[index] : 0,
      // 通达信 WINNER/COST 需要真实筹码成本分布；这里仅用能量强度做紫色控盘层代理。
      controlWinnerProxy: controlScaled[index] > 0 && sz2[index] * 8 > 80,
      startControl: startControl[index],
      mainShip: mainShipFirst[index],
      macdGolden: macdGolden[index],
      macdDead: macdDead[index],
    };
  });

  return {
    points,
    latest: points[points.length - 1],
    startPrice: close[0] || 0,
    endPrice: close[close.length - 1] || 0,
  };
}

export function buildVipFiveDragonSeries(data: KLineData[]): VipFiveDragonSeries {
  if (data.length === 0) {
    return { points: [], startPrice: 0, endPrice: 0 };
  }

  const close = data.map(item => item.close || 0);
  const open = data.map(item => item.open || item.close || 0);
  const high = data.map(item => item.high || item.close || 0);
  const low = data.map(item => item.low || item.close || 0);
  const volume = data.map(item => item.volume || 0);

  const openGap = close.map((_, index) => {
    const prevClose = refValue(close, index, 1);
    return prevClose > 0 ? (open[index] - prevClose) / prevClose * 100 : 0;
  });
  const aaa = close.map((value, index) => (3 * value + open[index] + high[index] + low[index]) / 6);
  const emaAaa12 = emaNumbers(aaa, 12);
  const emaAaa36 = emaNumbers(aaa, 36);
  const controlDegree = emaAaa12.map((value, index) => {
    const prevEma36 = refValue(emaAaa36, index, 1);
    return prevEma36 > 0 ? (value - prevEma36) / prevEma36 * 100 + 50 : 50;
  });
  const lowControl = controlDegree.map(value => value >= 50 && value < 60 ? value : 0);
  const midControl = controlDegree.map(value => value >= 60 && value < 80 ? value : 0);
  const highControl = controlDegree.map(value => value >= 80 ? value : 0);

  const closeEma12 = emaNumbers(close, 12);
  const closeEma26 = emaNumbers(close, 26);
  const diff = closeEma12.map((value, index) => value - closeEma26[index]);
  const dea = emaNumbers(diff, 9);

  const hhv55 = rollingHighest(high, 55);
  const llv55 = rollingLowest(low, 55);
  const rsv1 = close.map((value, index) => {
    const range = hhv55[index] - llv55[index];
    return range !== 0 ? (value - llv55[index]) / range * 100 : 0;
  });
  const k = tdxSmaNumbers(rsv1, 13, 1);
  const d = tdxSmaNumbers(k, 8, 1);

  const hhv21 = rollingHighest(high, 21);
  const llv21 = rollingLowest(low, 21);
  const rsv = close.map((value, index) => {
    const range = hhv21[index] - llv21[index];
    return range !== 0 ? -(hhv21[index] - value) / range * 100 : 0;
  });
  const lwr1 = tdxSmaNumbers(rsv, 13, 1);
  const lwr2 = tdxSmaNumbers(lwr1, 17, 1);

  const mav = close.map((value, index) => (value * 2 + high[index] + low[index]) / 4);
  const mavEma13 = emaNumbers(mav, 13);
  const mavEma55 = emaNumbers(mav, 55);
  const sk = mavEma13.map((value, index) => value - mavEma55[index]);
  const sd = emaNumbers(sk, 7);
  const emptyMain = sk.map((value, index) => (-2 * (value - sd[index])) * 3.8);
  const bullMain = sk.map((value, index) => (2 * (value - sd[index])) * 3.8);

  const ma5 = simpleMovingNumbers(close, 5);
  const ma5Ref = ma5.map((_, index) => refValue(ma5, index, 1));
  const gu3 = close.map((value, index) => (2 * value + high[index] + low[index]) / 4);
  const gu4 = rollingLowest(low, 34);
  const gu5 = rollingHighest(high, 34);
  const mainRaw = gu3.map((value, index) => {
    const range = gu5[index] - gu4[index];
    return range !== 0 ? (value - gu4[index]) / range * 100 : 0;
  });
  const mainForce = emaNumbers(mainRaw, 13);
  const retailSeed = mainForce.map((value, index) => 0.667 * refValue(mainForce, index, 1) + 0.333 * value);
  const retail = emaNumbers(retailSeed.map(value => Number.isFinite(value) ? value : 0), 2);
  const buySignal = close.map((_, index) => (
    (
      bullMain[index] > emptyMain[index]
      && ma5Ref[index] <= ma5[index]
      && (mainForce[index] > retail[index] || diff[index] > dea[index])
    )
    || (diff[index] > dea[index] && k[index] > d[index] && lwr1[index] > lwr2[index])
  ));

  const x3 = close.map((value, index) => (value + low[index] + high[index]) / 3);
  const x4 = emaNumbers(x3, 6);
  const x5 = emaNumbers(x4, 5);
  const trendBull = x4.map((value, index) => value >= x5[index]);

  const energy = close.map((value, index) => {
    const mid = (high[index] + low[index]) / 2;
    return mid !== 0 ? Math.sqrt(Math.max(volume[index], 0)) * ((value - mid) / mid) : 0;
  });
  const smoothEnergy = emaNumbers(energy, 10);
  const energyInertia = emaNumbers(smoothEnergy, 10);
  const energyBull = energyInertia.map(value => value > 0);

  const x2Raw = close.map((value, index) => {
    const range = hhv21[index] - llv21[index];
    return range !== 0 ? (value - llv21[index]) / range * 100 : 0;
  });
  const midLong = tdxSmaNumbers(x2Raw, 5, 1);
  const midShort = tdxSmaNumbers(midLong, 10, 1);
  const midBull = midLong.map((value, index) => value > midShort[index]);

  const hhv10 = rollingHighest(high, 10);
  const llv10 = rollingLowest(low, 10);
  const x1Raw = close.map((value, index) => {
    const range = hhv10[index] - llv10[index];
    return range !== 0 ? (value - llv10[index]) / range * 100 : 0;
  });
  const shortLong = tdxSmaNumbers(x1Raw, 5, 1);
  const shortEmpty = tdxSmaNumbers(shortLong, 5, 1);
  const shortBull = shortLong.map((value, index) => value > shortEmpty[index]);

  const points = data.map((item, index) => {
    const resonance = buySignal[index] && energyBull[index] && midBull[index] && shortBull[index];
    return {
      time: parseTime(item.time),
      rawTime: item.time,
      openGap: Number.isFinite(openGap[index]) ? openGap[index] : 0,
      controlDegree: Number.isFinite(controlDegree[index]) ? controlDegree[index] : 50,
      lowControl: Number.isFinite(lowControl[index]) ? lowControl[index] : 0,
      midControl: Number.isFinite(midControl[index]) ? midControl[index] : 0,
      highControl: Number.isFinite(highControl[index]) ? highControl[index] : 0,
      buySignal: buySignal[index],
      trendBull: trendBull[index],
      energyBull: energyBull[index],
      midBull: midBull[index],
      shortBull: shortBull[index],
      resonance,
    };
  });

  return {
    points,
    latest: points[points.length - 1],
    startPrice: close[0] || 0,
    endPrice: close[close.length - 1] || 0,
  };
}

function buildVolumeMA(data: KLineData[], period: number): LineData[] {
  const values = data.map(item => item.volume || 0);
  const ma = simpleMovingNumbers(values, period);
  return ma
    .map((value, index) => ({ time: parseTime(data[index].time), value }))
    .filter(point => Number.isFinite(point.value));
}

function buildVolumeSupportScore(data: KLineData[]): SignalScorePoint[] {
  const volumes = data.map(item => item.volume || 0);
  const vol5 = simpleMovingNumbers(volumes, 5);
  const vol20 = simpleMovingNumbers(volumes, 20);
  return data.map((item, index) => {
    const prevClose = index > 0 ? data[index - 1].close : item.open;
    const pct = prevClose > 0 ? ((item.close - prevClose) / prevClose) * 100 : 0;
    const ratio20 = Number.isFinite(vol20[index]) && vol20[index] > 0 ? item.volume / vol20[index] : 1;
    const ratio5 = Number.isFinite(vol5[index]) && vol5[index] > 0 ? item.volume / vol5[index] : 1;
    const closePos = (item.close - item.low) / Math.max(item.high - item.low, 0.01);
    const shrinkScore = clampNumber((1.18 - ratio20) * 48, -25, 42);
    const stabilityScore = clampNumber((closePos - 0.36) * 44, -18, 28);
    const pctScore = pct >= -3 && pct <= 1.5 ? 18 : (pct < -3 ? -18 : -10);
    const value = clampNumber(50 + shrinkScore + stabilityScore + pctScore - Math.max(0, ratio5 - 1.25) * 20, 0, 100);
    return {
      time: parseTime(item.time),
      value,
      color: value >= 70 ? '#f97316' : value >= 55 ? '#eab308' : '#64748b',
    };
  });
}

function buildTrendStrengthScore(data: KLineData[]): SignalScorePoint[] {
  const close = data.map(item => item.close);
  const ma5 = simpleMovingNumbers(close, 5);
  const ma10 = simpleMovingNumbers(close, 10);
  const ma20 = simpleMovingNumbers(close, 20);
  const ma60 = simpleMovingNumbers(close, 60);
  return data.map((item, index) => {
    const base20 = Number.isFinite(ma20[index]) && ma20[index] > 0 ? ma20[index] : item.close;
    const base60 = Number.isFinite(ma60[index]) && ma60[index] > 0 ? ma60[index] : base20;
    let value = 50;
    if (Number.isFinite(ma5[index]) && Number.isFinite(ma10[index])) value += ma5[index] >= ma10[index] ? 12 : -12;
    if (Number.isFinite(ma10[index]) && Number.isFinite(ma20[index])) value += ma10[index] >= ma20[index] ? 14 : -14;
    value += clampNumber(((item.close - base20) / base20) * 260, -22, 22);
    value += clampNumber(((base20 - base60) / base60) * 180, -12, 12);
    return {
      time: parseTime(item.time),
      value: clampNumber(value, 0, 100),
      color: value >= 65 ? '#ef4444' : value >= 48 ? '#f59e0b' : '#22c55e',
    };
  });
}

function buildLowBuySignalScore(data: KLineData[], signals: TradingSignal[]): SignalScorePoint[] {
  const signalByTime = new Map(signals.map(signal => [String(signal.time), signal]));
  const volumeSupport = buildVolumeSupportScore(data);
  const trendStrength = buildTrendStrengthScore(data);
  return data.map((item, index) => {
    const t = parseTime(item.time);
    const signal = signalByTime.get(String(t));
    const prevClose = index > 0 ? data[index - 1].close : item.open;
    const pct = prevClose > 0 ? ((item.close - prevClose) / prevClose) * 100 : 0;
    let value = 0.58 * (volumeSupport[index]?.value || 50) + 0.28 * (trendStrength[index]?.value || 50);
    if (pct >= -3 && pct <= 1.5) value += 8;
    if (signal?.action === 'buy') value += signal.level === 'S' ? 24 : 16;
    if (signal?.action === 'reduce' || signal?.action === 'sell') value -= 22;
    value = clampNumber(value, 0, 100);
    return {
      time: t,
      value,
      color: value >= 76 ? '#f97316' : value >= 62 ? '#eab308' : '#64748b',
    };
  });
}

function buildSellRiskScore(data: KLineData[], signals: TradingSignal[]): SignalScorePoint[] {
  const signalByTime = new Map(signals.map(signal => [String(signal.time), signal]));
  const close = data.map(item => item.close);
  const volume = data.map(item => item.volume || 0);
  const ma5 = simpleMovingNumbers(close, 5);
  const ma20 = simpleMovingNumbers(close, 20);
  const vol5 = simpleMovingNumbers(volume, 5);
  return data.map((item, index) => {
    const t = parseTime(item.time);
    const signal = signalByTime.get(String(t));
    const recentStart = Math.max(0, index - 19);
    const high20 = Math.max(...data.slice(recentStart, index + 1).map(row => row.high));
    const nearHigh = high20 > 0 ? item.close / high20 : 0;
    const prevClose = index > 0 ? data[index - 1].close : item.open;
    const pct = prevClose > 0 ? ((item.close - prevClose) / prevClose) * 100 : 0;
    let value = 18;
    if (nearHigh > 0.96) value += 18;
    if (Number.isFinite(ma5[index]) && item.close < ma5[index] * 0.99) value += 18;
    if (Number.isFinite(ma20[index]) && item.close < ma20[index]) value += 22;
    if (Number.isFinite(vol5[index]) && item.volume > vol5[index] * 1.18 && pct < 0) value += 18;
    if (signal?.action === 'reduce') value += 22;
    if (signal?.action === 'sell') value += 36;
    if (signal?.action === 'buy') value -= 14;
    value = clampNumber(value, 0, 100);
    return {
      time: t,
      value,
      color: value >= 70 ? '#22c55e' : value >= 45 ? '#f59e0b' : '#64748b',
    };
  });
}

function buildProxyFundFlow(data: KLineData[]): FundFlowPoint[] {
  if (data.length === 0) return [];
  const amount20 = simpleMovingNumbers(data.map(item => item.amount || item.close * (item.volume || 0) * 100), 20);
  return data.map((item, index) => {
    const amount = item.amount || item.close * (item.volume || 0) * 100;
    const range = Math.max(item.high - item.low, 0.01);
    const closePosition = (item.close - item.low) / range;
    const prevClose = index > 0 ? data[index - 1].close : item.open;
    const pct = prevClose > 0 ? (item.close - prevClose) / prevClose : 0;
    const volumeBias = Number.isFinite(amount20[index]) && amount20[index] > 0
      ? clampNumber(amount / amount20[index], 0.35, 2.2)
      : 1;
    const directional = clampNumber((closePosition - 0.5) * 2 + pct * 6, -1, 1);
    const mainNet = amount * directional * volumeBias * 0.18;
    return {
      time: parseTime(item.time),
      rawTime: item.time,
      mainNet,
      mainRatio: amount > 0 ? (mainNet / amount) * 100 : null,
      source: 'proxy',
    };
  });
}

function parseF10FundFlowPoints(fundFlow?: FundFlowSeries): FundFlowPoint[] {
  const fields = fundFlow?.fields || [];
  const lines = fundFlow?.lines || [];
  if (!fields.length || !lines.length) return [];
  const indexOf = (field: string) => fields.findIndex(item => item.toLowerCase() === field.toLowerCase());
  const dateIdx = indexOf('f51');
  const mainIdx = indexOf('f52');
  const ratioIdx = indexOf('f57');
  if (dateIdx < 0 || mainIdx < 0) return [];

  const points: FundFlowPoint[] = [];
  for (const line of lines) {
      const rawTime = String(line[dateIdx] || '').slice(0, 10);
      const mainNet = toFiniteNumber(line[mainIdx]);
    if (!rawTime || mainNet == null) continue;
    points.push({
        time: parseTime(rawTime),
        rawTime,
        mainNet,
        mainRatio: ratioIdx >= 0 ? toFiniteNumber(line[ratioIdx]) : null,
        source: 'f10' as const,
    });
  }
  return points.sort((a, b) => a.rawTime.localeCompare(b.rawTime));
}

function extractKLineFundFlow(data: KLineData[]): FundFlowPoint[] {
  const points: FundFlowPoint[] = [];
  for (const item of data) {
      const amount = item.amount || item.close * (item.volume || 0) * 100;
      const mainNet = getOptionalKLineNumber(item, ['mainNet', 'mainNetInflow', 'main_net']);
      const mainRatio = getOptionalKLineNumber(item, ['mainPct', 'mainNetInflowRatio', 'main_pct']);
    if (mainNet == null && mainRatio == null) continue;
      const finalMainNet = mainNet ?? (mainRatio != null && amount > 0 ? amount * mainRatio / 100 : 0);
    points.push({
        time: parseTime(item.time),
        rawTime: item.time,
        mainNet: finalMainNet,
        mainRatio: mainRatio ?? (amount > 0 ? finalMainNet / amount * 100 : null),
        source: 'f10' as const,
    });
  }
  return points;
}

function signalPriority(signal: TradingSignal): number {
  if (signal.action === 'sell') return 120;
  if (signal.action === 'reduce' && signal.level === 'risk') return 110;
  if (signal.level === 'S') return 100;
  if (signal.level === 'S-') return 90;
  if (signal.level === 'A+') return 80;
  if (signal.level === 'A') return 70;
  return 60;
}

function markerTextForSignal(signal: TradingSignal, isIntraday: boolean): string {
  if (signal.action === 'sell') return '清';
  if (signal.action === 'reduce' || signal.level === 'risk') return '减';
  if (isIntraday) {
    if (signal.level === 'S') return 'S';
    if (signal.level === 'S-') return 'S-';
    if (signal.level === 'A+') return 'A+';
    return 'A';
  }
  return signal.level;
}

function shouldKeepSignalForMarker(signal: TradingSignal, isIntraday: boolean): boolean {
  if (signal.action === 'observe') return false;
  if (!isIntraday) return true;
  if (signal.level === 'risk') return true;
  return signal.level === 'S' || signal.level === 'S-' || signal.level === 'A+';
}

function compactSignalsForMarkers(signals: TradingSignal[], isIntraday: boolean): TradingSignal[] {
  const filtered = signals.filter(signal => shouldKeepSignalForMarker(signal, isIntraday));
  const windowed = filtered.slice(-(isIntraday ? 220 : 260));
  const compacted: TradingSignal[] = [];

  let lastBuySourceIndex = -9999;
  let lastRiskSourceIndex = -9999;
  let lastBuyCompactedIndex = -1;
  let lastRiskCompactedIndex = -1;

  for (let i = 0; i < windowed.length; i += 1) {
    const signal = windowed[i];
    const isBuy = signal.action === 'buy';
    const minGap = isBuy ? (isIntraday ? 7 : 3) : (isIntraday ? 6 : 3);

    if (isBuy) {
      if (i - lastBuySourceIndex < minGap && lastBuyCompactedIndex >= 0) {
        if (signalPriority(signal) > signalPriority(compacted[lastBuyCompactedIndex])) {
          compacted[lastBuyCompactedIndex] = signal;
        }
        continue;
      }
      compacted.push(signal);
      lastBuySourceIndex = i;
      lastBuyCompactedIndex = compacted.length - 1;
      continue;
    }

    if (i - lastRiskSourceIndex < minGap && lastRiskCompactedIndex >= 0) {
      if (signalPriority(signal) > signalPriority(compacted[lastRiskCompactedIndex])) {
        compacted[lastRiskCompactedIndex] = signal;
      }
      continue;
    }
    compacted.push(signal);
    lastRiskSourceIndex = i;
    lastRiskCompactedIndex = compacted.length - 1;
  }

  return compacted.slice(-(isIntraday ? 26 : 60));
}

// 批量移除 series 并清空 ref
function clearSeriesArray(chart: IChartApi, refs: React.MutableRefObject<ISeriesApi<SeriesType, Time>[]>) {
  for (const s of refs.current) {
    try { chart.removeSeries(s); } catch { /* already removed */ }
  }
  refs.current = [];
}

// 将 UTC 秒级时间戳格式化为 YYYY-MM-DD HH:mm
function formatTimestamp(ts: number): string {
  const d = new Date(ts * 1000);
  const Y = d.getUTCFullYear();
  const M = String(d.getUTCMonth() + 1).padStart(2, '0');
  const D = String(d.getUTCDate()).padStart(2, '0');
  const h = String(d.getUTCHours()).padStart(2, '0');
  const m = String(d.getUTCMinutes()).padStart(2, '0');
  return `${Y}-${M}-${D} ${h}:${m}`;
}

// 格式化时间显示，统一为 YYYY-MM-DD HH:MM:SS
function formatTimeDisplay(timeStr: string): string {
  if (timeStr.length > 10) {
    return timeStr.slice(0, 19);
  }
  return timeStr.slice(0, 10) + ' 00:00:00';
}

export const StockChartLW: React.FC<StockChartProps> = ({
  data,
  updateMode,
  period,
  onPeriodChange,
  stock,
  dayKData = [],
  mainChartTemplate: controlledMainChartTemplate,
  onMainChartTemplateChange,
  showTemplateSelect = false,
  showAuction = false,
  initialGridMode,
  initialSubChartType,
  initialSubType2,
  initialSubType3,
  visibleRangeBars,
}) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const { config: indicatorConfig, updateIndicator } = useIndicator();
  const isTrendLinePeriod = period === '1m' || period === '5d';
  const initialGridModeValue = initialGridMode ?? !isTrendLinePeriod;
  const initialSubChartTypeValue = initialSubChartType ?? 'volume';
  const initialSubType2Value = initialSubType2 ?? 'macd';
  const initialSubType3Value = initialSubType3 ?? 'kdj';
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const volumeContainerRef = useRef<HTMLDivElement>(null);
  const mainOverlayCanvasRef = useRef<HTMLCanvasElement>(null);
  const vipOverlayCanvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const volumeChartRef = useRef<IChartApi | null>(null);
  const mainSeriesRef = useRef<ISeriesApi<SeriesType, Time> | null>(null);
  const volumeSeriesRef = useRef<ISeriesApi<SeriesType, Time> | null>(null);
  // 集合竞价独立窗(通达信样式):分时线前拼 whitespace 占位让时间轴含竞价时段、分时线右移空出左侧,
  // overlay canvas 在该区自绘价格台阶+背景框。数据来自 NAS intraday 采集,无数据时隐藏。
  const auctionOverlayCanvasRef = useRef<HTMLCanvasElement>(null);
  const [auctionTicks, setAuctionTicks] = useState<{ time: string; price: number; volume: number }[]>([]);
  // 竞价段按"价格变化点"降采样(相邻同价合并),保留台阶观感、点数远少于原始tick;时间保留到秒级。
  const sampledAuction = useMemo(() => {
    const src = auctionTicks;
    if (src.length < 2) return [] as { time: string; price: number; volume: number }[];
    const out = [src[0]];
    for (let i = 1; i < src.length; i++) {
      if (src[i].price !== out[out.length - 1].price) out.push(src[i]);
    }
    const last = src[src.length - 1];
    if (out[out.length - 1] !== last) out.push(last);
    return out;
  }, [auctionTicks]);
  const vipAxisSeriesRef = useRef<ISeriesApi<SeriesType, Time> | null>(null);
  const vipAxisPaneRefs = useRef<Array<ISeriesApi<SeriesType, Time> | null>>([null, null, null]);
  const maSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const emaSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const bollSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const openEatFishSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const tdxMainSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const tdxMainMarkersRef = useRef<ISeriesMarkersPluginApi<Time> | null>(null);
  const subSeriesRefs = useRef<ISeriesApi<SeriesType, Time>[]>([]);
  const signalMarkersRef = useRef<ISeriesMarkersPluginApi<Time> | null>(null);
  const seriesTypeRef = useRef<'line' | 'candle' | null>(null);
  const hasFittedRef = useRef(false);

  const [volumeHeight, setVolumeHeight] = useState(VOLUME_DEFAULT);
  const [gridSubHeight, setGridSubHeight] = useState(GRID_SUB_DEFAULT);
  const [subPaneRects, setSubPaneRects] = useState<Array<{ top: number; height: number }>>([]);
  const gridModeRef2 = useRef(initialGridModeValue); // 供 resize 回调读取最新模式
  const hasCustomSubHeightRef = useRef(false);

  const handleVolumeResize = useCallback((delta: number) => {
    hasCustomSubHeightRef.current = true;
    if (gridModeRef2.current) {
      setGridSubHeight(prev => Math.max(GRID_SUB_MIN, Math.min(GRID_SUB_MAX, prev - delta)));
    } else {
      setVolumeHeight(prev => Math.max(VOLUME_MIN, Math.min(VOLUME_MAX, prev - delta)));
    }
  }, []);

  // series → 指标类型映射（用于点击识别）
  type MainIndicatorType = 'ma' | 'ema' | 'boll';
  const seriesIndicatorMap = useRef<Map<ISeriesApi<SeriesType, Time>, MainIndicatorType>>(new Map());

  // 浮动配置面板状态
  const [indicatorPopup, setIndicatorPopup] = React.useState<{
    type: MainIndicatorType;
    xRatio: number;
    yRatio: number;
  } | null>(null);
  const [chartViewport, setChartViewport] = useState({ width: 0, height: 0 });
  const [uncontrolledMainChartTemplate, setUncontrolledMainChartTemplate] = React.useState<MainChartTemplate>('standard');
  const mainChartTemplate = controlledMainChartTemplate ?? uncontrolledMainChartTemplate;
  const setMainChartTemplate = useCallback((template: MainChartTemplate) => {
    setUncontrolledMainChartTemplate(template);
    onMainChartTemplateChange?.(template);
  }, [onMainChartTemplateChange]);

  const [subChartType, setSubChartType] = React.useState<SubChartType>(initialSubChartTypeValue);
  const subChartTypeRef = useRef<SubChartType>(initialSubChartTypeValue);

  // 四宫格(主图+3副图)模式
  const [gridMode, setGridMode] = React.useState(initialGridModeValue);
  const [tdxMainHint, setTdxMainHint] = React.useState(''); // TDX主图模板无可显示输出时的提示
  const [floatSharesTick, setFloatSharesTick] = React.useState(0); // 流通股本到位后触发TDX重算
  const gridModeRef = useRef(initialGridModeValue);
  const [subType2, setSubType2] = React.useState<SubChartType>(initialSubType2Value);
  const subType2Ref = useRef<SubChartType>(initialSubType2Value);
  const [subType3, setSubType3] = React.useState<SubChartType>(initialSubType3Value);
  const subType3Ref = useRef<SubChartType>(initialSubType3Value);
  const [fundFlowState, setFundFlowState] = React.useState<FundFlowState>({
    points: [],
    source: 'none',
    loading: false,
  });
  const [vipOverlayRevision, setVipOverlayRevision] = React.useState(0);
  // 每个副图 pane 的 series 引用 [pane0, pane1, pane2]
  const subPaneRefs = useRef<ISeriesApi<SeriesType, Time>[][]>([[], [], []]);

  // ===== 区间统计：左键直接在K线上拖框选取，统计区间数据 =====
  const draggingRef = useRef(false);
  const [rangeStart, setRangeStart] = React.useState<Time | null>(null);
  const [rangeEnd, setRangeEnd] = React.useState<Time | null>(null);
  const [rangeStats, setRangeStats] = React.useState<RangeStats | null>(null);
  const [rangeCoords, setRangeCoords] = React.useState<{ x1: number; y1: number; x2: number; y2: number } | null>(null);
  const rangeYRef = useRef<{ y1: number; y2: number }>({ y1: 0, y2: 0 });
  const safeDataRef = useRef<KLineData[]>([]);
  const isTrendLinePeriodRef = useRef(false);

  const safeData = data || [];
  safeDataRef.current = safeData;
  const hasData = safeData.length > 0;
  isTrendLinePeriodRef.current = isTrendLinePeriod;
  const isVipOverlayType = isVipSubChartType(subChartType);
  const showVipOverlay = !gridMode && !isTrendLinePeriod && isVipOverlayType;
  const vipPaneTypes = [subChartType, subType2, subType3] as const;
  const showVipOverlayCanvas = safeData.length > 0 && !isTrendLinePeriod && (
    showVipOverlay || (gridMode && vipPaneTypes.some(isVipSubChartType))
  );
  const preClose = stock?.preClose || 0;

  const [hoverData, setHoverData] = React.useState<KLineData | null>(null);
  const [hoverPoint, setHoverPoint] = React.useState<{ x: number; y: number } | null>(null);
  const lastData = safeData[safeData.length - 1];
  const displayData = hoverData || lastData;
  const displayTime = displayData?.time ? parseTime(displayData.time) : null;
  const tradingSignals = useMemo(
    () => (isTrendLinePeriod
      ? calculateIntradayTradingSignals(safeData, preClose, dayKData)
      : calculateTradingSignals(safeData)),
    [isTrendLinePeriod, safeData, preClose, dayKData],
  );
  const latestTradingSignal = useMemo(() => getLatestTradingSignal(tradingSignals), [tradingSignals]);
  const openEatFishMainSeries = useMemo(
    () => (!isTrendLinePeriod && mainChartTemplate === 'openEatFish' ? buildOpenEatFishSeries(safeData) : null),
    [isTrendLinePeriod, mainChartTemplate, safeData],
  );
  const klineFundFlowPoints = useMemo(() => extractKLineFundFlow(safeData), [safeData]);
  const klineHasFundFlow = klineFundFlowPoints.length > 0;
  const fundFlowPoints = useMemo(() => {
    if (klineFundFlowPoints.length > 0) return klineFundFlowPoints;
    if (fundFlowState.points.length > 0) return fundFlowState.points;
    return [];
  }, [fundFlowState.points, klineFundFlowPoints]);
  const fundFlowSource = useMemo<'f10' | 'proxy' | 'none'>(() => {
    if (klineFundFlowPoints.length > 0 || fundFlowState.points.length > 0) return 'f10';
    return fundFlowState.source;
  }, [fundFlowState.source, fundFlowState.points.length, klineFundFlowPoints.length]);

  const chartColors = useMemo(() => ({
    background: colors.isDark ? '#0f172a' : '#ffffff',
    textColor: colors.isDark ? '#94a3b8' : '#64748b',
    gridColor: colors.isDark ? '#1e293b' : '#e2e8f0',
    upColor: cc.upColor,
    downColor: cc.downColor,
    priceLineColor: colors.isDark ? '#64748b' : '#94a3b8',
  }), [colors.isDark, cc.upColor, cc.downColor]);

  useEffect(() => {
    if (isTrendLinePeriod || !stock?.symbol || klineHasFundFlow) {
      setFundFlowState({ points: [], source: 'none', loading: false });
      return;
    }

    let cancelled = false;
    setFundFlowState(prev => ({
      points: prev.points,
      source: prev.points.length > 0 ? prev.source : 'none',
      loading: true,
      error: undefined,
    }));

    getF10Overview(stock.symbol)
      .then(overview => {
        if (cancelled) return;
        const points = parseF10FundFlowPoints(overview?.fundFlow);
        setFundFlowState({
          points,
          source: points.length > 0 ? 'f10' : 'none',
          loading: false,
          error: points.length > 0 ? undefined : '暂无真实资金流序列',
        });
      })
      .catch(error => {
        if (cancelled) return;
        setFundFlowState({
          points: [],
          source: 'none',
          loading: false,
          error: error instanceof Error ? error.message : '资金流获取失败',
        });
      });

    return () => {
      cancelled = true;
    };
  }, [isTrendLinePeriod, klineHasFundFlow, stock?.symbol]);

  const mainIndicatorLegend = useMemo<IndicatorLegendItem[]>(() => {
    if (isTrendLinePeriod || !displayTime) return [];
    const items: IndicatorLegendItem[] = [];

    if (indicatorConfig.ma.enabled) {
      indicatorConfig.ma.periods.forEach((periodValue, idx) => {
        const points = safeData.length >= periodValue ? calculateSMA(safeData, periodValue) : [];
        items.push({
          label: `MA${periodValue}`,
          value: formatIndicatorValue(valueAtTime(points, displayTime)),
          color: MA_COLORS[idx % MA_COLORS.length],
        });
      });
    }

    if (indicatorConfig.ema.enabled) {
      indicatorConfig.ema.periods.forEach((periodValue, idx) => {
        const points = calculateEMA(safeData, periodValue);
        items.push({
          label: `EMA${periodValue}`,
          value: formatIndicatorValue(valueAtTime(points, displayTime)),
          color: EMA_COLORS[idx % EMA_COLORS.length],
        });
      });
    }

    if (indicatorConfig.boll.enabled) {
      const { mid, upper, lower } = calculateBOLL(safeData, indicatorConfig.boll.period, indicatorConfig.boll.multiplier);
      items.push(
        { label: 'BOLL:M', value: formatIndicatorValue(valueAtTime(mid, displayTime)), color: BOLL_COLOR },
        { label: 'BOLL:U', value: formatIndicatorValue(valueAtTime(upper, displayTime)), color: BOLL_COLOR },
        { label: 'BOLL:L', value: formatIndicatorValue(valueAtTime(lower, displayTime)), color: BOLL_COLOR },
      );
    }

    return items;
  }, [displayTime, indicatorConfig, isTrendLinePeriod, safeData]);

  const getSubLegendItems = useCallback((type: SubChartType): IndicatorLegendItem[] => {
    if (!displayTime) return [];
    if (type === 'volume') {
      return [{
        label: 'VOL',
        value: formatCompactVolume(displayData?.volume),
        color: displayData && displayData.close >= displayData.open ? chartColors.upColor : chartColors.downColor,
      }];
    }
    if (type === 'macd') {
      const { dif, dea, histogram } = calculateMACD(safeData, indicatorConfig.macd.fast, indicatorConfig.macd.slow, indicatorConfig.macd.signal);
      const hist = valueAtTime(histogram, displayTime);
      return [
        { label: 'DIF', value: formatIndicatorValue(valueAtTime(dif, displayTime)), color: '#3b82f6' },
        { label: 'DEA', value: formatIndicatorValue(valueAtTime(dea, displayTime)), color: '#eab308' },
        { label: 'MACD', value: formatIndicatorValue(hist), color: hist != null && hist < 0 ? '#22c55e' : '#ef4444' },
      ];
    }
    if (type === 'kdj') {
      const { k, d, j } = calculateKDJ(safeData, indicatorConfig.kdj.period, indicatorConfig.kdj.k, indicatorConfig.kdj.d);
      return [
        { label: 'J', value: formatIndicatorValue(valueAtTime(j, displayTime)), color: '#a855f7' },
        { label: 'K', value: formatIndicatorValue(valueAtTime(k, displayTime)), color: '#3b82f6' },
        { label: 'D', value: formatIndicatorValue(valueAtTime(d, displayTime)), color: '#eab308' },
      ];
    }
    if (type === 'rsi') {
      return [{ label: 'RSI', value: formatIndicatorValue(valueAtTime(calculateRSI(safeData, indicatorConfig.rsi.period), displayTime)), color: '#a855f7' }];
    }
    if (type === 'cci') {
      return [{ label: 'CCI', value: formatIndicatorValue(valueAtTime(calculateCCI(safeData, 14), displayTime)), color: '#06b6d4' }];
    }
    if (type === 'wr') {
      return [{ label: 'WR', value: formatIndicatorValue(valueAtTime(calculateWR(safeData, 14), displayTime)), color: '#f59e0b' }];
    }
    if (type === 'volumeSupport') {
      const score = latestLineValue(buildVolumeSupportScore(safeData), displayTime);
      const vol5 = valueAtTime(buildVolumeMA(safeData, 5), displayTime);
      const vol20 = valueAtTime(buildVolumeMA(safeData, 20), displayTime);
      const ratio = displayData?.volume && vol20 && vol20 > 0 ? displayData.volume / vol20 : null;
      return [
        { label: '承接', value: formatIndicatorValue(score, 0), color: '#f97316' },
        { label: '量/20', value: ratio != null ? ratio.toFixed(2) : '--', color: '#38bdf8' },
        { label: 'V5', value: formatCompactVolume(vol5), color: '#eab308' },
      ];
    }
    if (type === 'mainFundFlow') {
      const point = latestFundValue(fundFlowPoints, displayTime);
      const usingProxy = !point && fundFlowSource !== 'f10';
      const proxyPoint = usingProxy ? latestFundValue(buildProxyFundFlow(safeData), displayTime) : null;
      const finalPoint = point || proxyPoint;
      return [
        {
          label: point ? '主力' : (fundFlowState.loading ? '主力' : '代理'),
          value: fundFlowState.loading && !finalPoint ? '加载中' : formatSignedCompactAmount(finalPoint?.mainNet),
          color: finalPoint && finalPoint.mainNet < 0 ? '#22c55e' : '#ef4444',
        },
        {
          label: point ? '强度' : '说明',
          value: point ? formatIndicatorValue(point.mainRatio, 2) + '%' : (fundFlowState.loading ? '真实源' : '量价'),
          color: point ? '#f97316' : '#64748b',
        },
      ];
    }
    if (type === 'lowBuySignal') {
      const score = latestLineValue(buildLowBuySignalScore(safeData, tradingSignals), displayTime);
      const latestSignal = latestTradingSignal?.rawTime === displayData?.time ? latestTradingSignal : null;
      return [
        { label: '低吸', value: formatIndicatorValue(score, 0), color: '#f97316' },
        { label: '信号', value: latestSignal?.title || '--', color: '#eab308' },
      ];
    }
    if (type === 'trendStrength') {
      const score = latestLineValue(buildTrendStrengthScore(safeData), displayTime);
      return [
        { label: '趋势', value: formatIndicatorValue(score, 0), color: '#ef4444' },
        { label: 'MA10', value: formatIndicatorValue(displayData?.ma10), color: '#a855f7' },
        { label: 'MA20', value: formatIndicatorValue(displayData?.ma20), color: '#f97316' },
      ];
    }
    if (type === 'fundTurn') {
      const point = latestFundValue(fundFlowPoints, displayTime);
      const proxyPoint = point ? null : latestFundValue(buildProxyFundFlow(safeData), displayTime);
      const finalPoint = point || proxyPoint;
      return [
        { label: point ? '拐点' : '代理拐点', value: formatSignedCompactAmount(finalPoint?.mainNet), color: finalPoint && finalPoint.mainNet < 0 ? '#22c55e' : '#ef4444' },
        { label: point ? '强度' : '源', value: point ? formatIndicatorValue(point.mainRatio, 2) + '%' : (fundFlowState.loading ? '加载' : '量价'), color: '#38bdf8' },
      ];
    }
    if (type === 'sellRisk') {
      const score = latestLineValue(buildSellRiskScore(safeData, tradingSignals), displayTime);
      return [
        { label: '卖险', value: formatIndicatorValue(score, 0), color: '#22c55e' },
        { label: '最新', value: latestTradingSignal?.level === 'risk' ? latestTradingSignal.title : '--', color: '#f59e0b' },
      ];
    }
    if (type === 'vipAnomaly') {
      const series = buildVipAnomalySeries(safeData);
      const point = series.points.find(item => String(item.time) === String(displayTime)) || series.latest;
      const start = series.startPrice || point?.sz2 || 0;
      const end = displayData?.close || series.endPrice || start;
      const diff = end - start;
      const pct = start > 0 ? (end / start - 1) * 100 : 0;
      return [
        { label: '至今资金差价', value: formatIndicatorValue(diff), color: '#f8fafc' },
        { label: '至今资金涨幅%', value: formatIndicatorValue(pct), color: '#f8fafc' },
        { label: '异动现主力进', value: point?.anomaly ? '1.00' : '0.00', color: '#eab308' },
      ];
    }
    if (type === 'vipShortEnergy') {
      const series = buildVipShortEnergySeries(safeData);
      const point = series.points.find(item => String(item.time) === String(displayTime)) || series.latest;
      const start = series.startPrice || 0;
      const end = displayData?.close || series.endPrice || start;
      const pct = start > 0 ? (end / start - 1) * 100 : 0;
      return [
        { label: '至今涨幅%', value: formatIndicatorValue(pct), color: '#f8fafc' },
        { label: '异动', value: point?.anomaly ? '1.00' : '0.00', color: VIP_COLORS.yellow },
        { label: '控盘', value: formatIndicatorValue(point?.controlScaled, 2), color: point && point.controlScaled < 0 ? '#f8fafc' : VIP_COLORS.red },
      ];
    }
    if (type === 'vipFiveDragon') {
      const series = buildVipFiveDragonSeries(safeData);
      const point = series.points.find(item => String(item.time) === String(displayTime)) || series.latest;
      return [
        { label: '开幅', value: formatIndicatorValue(point?.openGap, 2), color: '#f8fafc' },
        { label: '控盘度', value: formatIndicatorValue(point?.controlDegree, 2), color: VIP_COLORS.yellow },
        { label: '低控盘', value: formatIndicatorValue(point?.lowControl, 2), color: VIP_COLORS.yellow },
        { label: '中控盘', value: formatIndicatorValue(point?.midControl, 2), color: VIP_COLORS.lired },
        { label: '高控盘', value: formatIndicatorValue(point?.highControl, 2), color: VIP_COLORS.magenta },
        { label: 'GZ', value: point?.resonance ? '1.00' : '0.00', color: point?.resonance ? VIP_COLORS.red : '#1d4ed8' },
      ];
    }
    return [];
  }, [
    chartColors.downColor,
    chartColors.upColor,
    displayData,
    displayTime,
    fundFlowPoints,
    fundFlowSource,
    fundFlowState.loading,
    indicatorConfig,
    latestTradingSignal,
    safeData,
    tradingSignals,
  ]);

  const periods: { id: TimePeriod; label: string }[] = [
    { id: '1m', label: '分时' },
    { id: '5d', label: '5日' },
    { id: '30m', label: '30分' },
    { id: '60m', label: '60分' },
    { id: '1d', label: '日K' },
    { id: '1w', label: '周K' },
    { id: '1mo', label: '月K' },
  ];

  const getPriceColor = useCallback((price: number) => {
    if (preClose <= 0) return colors.isDark ? 'text-slate-100' : 'text-slate-700';
    if (price > preClose) return cc.upClass;
    if (price < preClose) return cc.downClass;
    return colors.isDark ? 'text-slate-100' : 'text-slate-700';
  }, [preClose, colors.isDark, cc.upClass, cc.downClass]);

  const formatChangePercent = useCallback((price: number) => {
    if (preClose <= 0) return '0.00%';
    const percent = ((price - preClose) / preClose) * 100;
    const sign = percent >= 0 ? '+' : '';
    return `${sign}${percent.toFixed(2)}%`;
  }, [preClose]);

  const formatChange = useCallback((price: number) => {
    if (preClose <= 0) return '0.00';
    const change = price - preClose;
    const sign = change >= 0 ? '+' : '';
    return `${sign}${change.toFixed(2)}`;
  }, [preClose]);

  // 清除所有 series（不销毁图表实例）
  // 拉取当日集合竞价段(仅分时1m)。日期跟随分时数据首根;9:13-9:27 盘中竞价窗口内 15s 轮询看生长。
  const auctionDateKey = showAuction && period === '1m' ? String(safeData[0]?.time || '').slice(0, 10) : '';
  useEffect(() => {
    // 关闭/非分时/无股票 → 清空;但日期暂不可得(分时刷新的空中间态)时保留上次数据,不清空——
    // 否则 4s 刷新会把竞价数据抖没,主图竞价折线随之消失。
    if (!showAuction || period !== '1m' || !stock?.symbol) {
      setAuctionTicks([]);
      return;
    }
    if (!auctionDateKey) {
      return;
    }
    let cancelled = false;
    const load = async () => {
      try {
        const res: any = await GetStockIntraday(stock.symbol, auctionDateKey);
        if (!cancelled) {
          const list = (res && res.auction) || [];
          setAuctionTicks(list.map((t: any) => ({ time: String(t.time), price: Number(t.price), volume: Number(t.volume) || 0 })));
        }
      } catch {
        if (!cancelled) setAuctionTicks([]);
      }
    };
    load();
    const now = new Date();
    const hm = now.getHours() * 100 + now.getMinutes();
    let timer: number | undefined;
    if (hm >= 913 && hm <= 927) timer = window.setInterval(load, 15000);
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [period, stock?.symbol, auctionDateKey, showAuction]);

  // 竞价数据异步到达时,主渲染的 fitContent 往往已错过(updateMode 非 full),导致竞价段落在可视窗口左侧外。
  // 这里在竞价数据变化时补一次 fitContent,把 9:15-9:25 段纳入可视范围(仅竞价数据真变时触发,不受 4s 刷新影响)。
  useEffect(() => {
    if (!showAuction || period !== '1m' || sampledAuction.length < 2) return;
    const chart = chartRef.current;
    const vc = volumeChartRef.current;
    if (!chart) return;
    const id = requestAnimationFrame(() => {
      try {
        chart.timeScale().fitContent();
        vc?.timeScale().fitContent();
      } catch { /* chart 已卸载 */ }
    });
    return () => cancelAnimationFrame(id);
  }, [sampledAuction, showAuction, period]);

  const clearAllSeries = useCallback(() => {
    const chart = chartRef.current;
    const volumeChart = volumeChartRef.current;
    if (!chart || !volumeChart) return;

    if (mainSeriesRef.current) {
      if (signalMarkersRef.current) {
        signalMarkersRef.current.detach();
        signalMarkersRef.current = null;
      }
      try { chart.removeSeries(mainSeriesRef.current); } catch { /* already removed */ }
      mainSeriesRef.current = null;
    }
    clearSeriesArray(chart, maSeriesRefs);
    clearSeriesArray(chart, emaSeriesRefs);
    clearSeriesArray(chart, bollSeriesRefs);
    clearSeriesArray(chart, openEatFishSeriesRefs);
    // 清空所有副图 pane 的 series + 多余 pane
    for (const arr of subPaneRefs.current) {
      for (const s of arr) {
        try { volumeChart.removeSeries(s); } catch { /* already removed */ }
      }
    }
    subPaneRefs.current = [[], [], []];
    vipAxisSeriesRef.current = null;
    vipAxisPaneRefs.current = [null, null, null];
    clearSeriesArray(volumeChart, subSeriesRefs);
    if (volumeSeriesRef.current) {
      try { volumeChart.removeSeries(volumeSeriesRef.current); } catch { /* already removed */ }
      volumeSeriesRef.current = null;
    }
    try {
      const ps = volumeChart.panes();
      for (let i = ps.length - 1; i >= 1; i--) volumeChart.removePane(i);
    } catch { /* ignore */ }
    seriesTypeRef.current = null;
    hasFittedRef.current = false;
  }, []);

  // ========== 唯一的图表创建：组件挂载时创建，卸载时销毁 ==========
  useEffect(() => {
    if (!chartContainerRef.current || !volumeContainerRef.current) return;
    let disposed = false;

    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { color: '#0f172a' },
        textColor: '#94a3b8',
        attributionLogo: false,
        fontFamily: CHART_FONT_FAMILY,
      },
      grid: { vertLines: { color: '#1e293b' }, horzLines: { color: '#1e293b' } },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: {
        borderColor: '#1e293b',
        scaleMargins: { top: 0.15, bottom: 0.15 },
        minimumWidth: PRICE_SCALE_MIN_WIDTH,
      },
      timeScale: { borderColor: '#1e293b', timeVisible: true, secondsVisible: false },
      localization: { timeFormatter: (time: Time) => typeof time === 'number' ? formatTimestamp(time) : String(time) },
      handleScroll: true,
      handleScale: true,
    });

    const volumeChart = createChart(volumeContainerRef.current, {
      layout: {
        background: { color: '#0f172a' },
        textColor: '#94a3b8',
        attributionLogo: false,
        fontFamily: CHART_FONT_FAMILY,
      },
      grid: { vertLines: { color: '#1e293b' }, horzLines: { color: '#1e293b' } },
      rightPriceScale: {
        borderColor: '#1e293b',
        scaleMargins: { top: 0.15, bottom: 0.1 },
        minimumWidth: PRICE_SCALE_MIN_WIDTH,
      },
      timeScale: { borderColor: '#1e293b', timeVisible: true, secondsVisible: false },
      localization: { timeFormatter: (time: Time) => typeof time === 'number' ? formatTimestamp(time) : String(time) },
      handleScroll: true,
      handleScale: true,
    });

    chartRef.current = chart;
    volumeChartRef.current = volumeChart;

    // 同步时间轴
    const syncMainToSub = (range: LogicalRange | null) => {
      if (disposed || !range || volumeChartRef.current !== volumeChart) return;
      volumeChart.timeScale().setVisibleLogicalRange(range);
    };
    const syncSubToMain = (range: LogicalRange | null) => {
      if (disposed || !range || chartRef.current !== chart) return;
      chart.timeScale().setVisibleLogicalRange(range);
    };
    chart.timeScale().subscribeVisibleLogicalRangeChange(syncMainToSub);
    volumeChart.timeScale().subscribeVisibleLogicalRangeChange(syncSubToMain);

    // resize
    const resizeObserver = new ResizeObserver(() => {
      if (disposed || chartRef.current !== chart || volumeChartRef.current !== volumeChart) return;
      if (chartContainerRef.current && volumeContainerRef.current) {
        const w = chartContainerRef.current.clientWidth;
        const h = chartContainerRef.current.clientHeight;
        const vh = volumeContainerRef.current.clientHeight;
        setChartViewport(prev => (prev.width === w && prev.height === h ? prev : { width: w, height: h }));
        if (!hasCustomSubHeightRef.current && gridModeRef2.current) {
          const totalChartHeight = h + vh;
          const nextGridHeight = Math.max(
            GRID_SUB_MIN,
            Math.min(GRID_SUB_MAX, Math.round(totalChartHeight * GRID_SUB_HEIGHT_RATIO)),
          );
          setGridSubHeight(prev => (Math.abs(prev - nextGridHeight) > 2 ? nextGridHeight : prev));
        }
        if (w > 0 && h > 0) chart.applyOptions({ width: w, height: h });
        if (w > 0 && vh > 0) volumeChart.applyOptions({ width: w, height: vh });
      }
    });
    resizeObserver.observe(chartContainerRef.current);
    resizeObserver.observe(volumeContainerRef.current);

    // 仅在组件卸载时销毁
    return () => {
      disposed = true;
      resizeObserver.disconnect();
      try { chart.timeScale().unsubscribeVisibleLogicalRangeChange(syncMainToSub); } catch { /* already disposed */ }
      try { volumeChart.timeScale().unsubscribeVisibleLogicalRangeChange(syncSubToMain); } catch { /* already disposed */ }
      chartRef.current = null;
      volumeChartRef.current = null;
      mainSeriesRef.current = null;
      volumeSeriesRef.current = null;
      const markers = signalMarkersRef.current;
      signalMarkersRef.current = null;
      maSeriesRefs.current = [];
      emaSeriesRefs.current = [];
      bollSeriesRefs.current = [];
      openEatFishSeriesRefs.current = [];
      subSeriesRefs.current = [];
      seriesTypeRef.current = null;
      try { markers?.detach(); } catch { /* already disposed */ }
      try { chart.remove(); } catch { /* already disposed */ }
      try { volumeChart.remove(); } catch { /* already disposed */ }
    };
  }, []); // 空依赖 —— 只执行一次

  // ========== 主题变化：applyOptions 更新样式，不销毁图表 ==========
  useEffect(() => {
    const chart = chartRef.current;
    const volumeChart = volumeChartRef.current;
    if (!chart || !volumeChart) return;

    const layoutOpts = {
      layout: {
        background: { color: chartColors.background },
        textColor: chartColors.textColor,
        fontFamily: CHART_FONT_FAMILY,
      },
      grid: { vertLines: { color: chartColors.gridColor }, horzLines: { color: chartColors.gridColor } },
      rightPriceScale: { borderColor: chartColors.gridColor, minimumWidth: PRICE_SCALE_MIN_WIDTH },
      timeScale: { borderColor: chartColors.gridColor },
    };
    chart.applyOptions(layoutOpts);
    volumeChart.applyOptions(layoutOpts);

    // 更新 K线蜡烛颜色（颜色模式切换时生效）
    if (mainSeriesRef.current && seriesTypeRef.current === 'candle' && mainChartTemplate === 'standard') {
      mainSeriesRef.current.applyOptions({
        upColor: chartColors.upColor,
        downColor: chartColors.downColor,
        wickUpColor: chartColors.upColor,
        wickDownColor: chartColors.downColor,
      });
    }

    // 更新成交量柱颜色（仅副图为成交量时）
    if (volumeSeriesRef.current && subChartTypeRef.current === 'volume' && safeData.length > 0) {
      const volData: HistogramData[] = safeData.map(d => ({
        time: parseTime(d.time),
        value: d.volume,
        color: d.close >= d.open ? chartColors.upColor + '99' : chartColors.downColor + '99',
      }));
      volumeSeriesRef.current.setData(volData);
    }
  }, [chartColors, mainChartTemplate, safeData]);

  useEffect(() => {
    const canvas = mainOverlayCanvasRef.current;
    const container = chartContainerRef.current;
    const chart = chartRef.current;
    const series = mainSeriesRef.current;
    if (!canvas || !container || !chart || !series || mainChartTemplate !== 'openEatFish' || isTrendLinePeriod || safeData.length === 0) {
      if (canvas) {
        const ctx = canvas.getContext('2d');
        ctx?.clearRect(0, 0, canvas.width, canvas.height);
      }
      return;
    }

    let frame = 0;
    const openEatFish = openEatFishMainSeries;
    if (!openEatFish) return;
    const timeScale = chart.timeScale();
    const draw = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;
      if (width <= 0 || height <= 0) return;

      const dpr = window.devicePixelRatio || 1;
      const pixelWidth = Math.floor(width * dpr);
      const pixelHeight = Math.floor(height * dpr);
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth;
        canvas.height = pixelHeight;
      }
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;

      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);
      ctx.save();
      ctx.beginPath();
      ctx.rect(0, 0, Math.max(0, width - PRICE_SCALE_MIN_WIDTH), height);
      ctx.clip();

      const xOf = (point: OpenEatFishPoint): number | null => {
        const x = timeScale.timeToCoordinate(point.time);
        const numeric = typeof x === 'number' ? Number(x) : NaN;
        return Number.isFinite(numeric) ? numeric : null;
      };
      const yOf = (value: number) => {
        const y = series.priceToCoordinate(value);
        return typeof y === 'number' && Number.isFinite(y) ? y : null;
      };
      const xs = openEatFish.points
        .map(point => xOf(point))
        .filter((x): x is number => typeof x === 'number')
        .sort((a, b) => a - b);
      let spacing = 12;
      for (let i = 1; i < xs.length; i += 1) {
        const gap = xs[i] - xs[i - 1];
        if (gap > 1) spacing = Math.min(spacing, gap);
      }
      spacing = clampNumber(spacing, 4, 28);
      const ribbonWidth = clampNumber(spacing * 2.2, 11, 24);
      const labelGap = clampNumber(spacing * 6, 44, 92);

      const visiblePoints = openEatFish.points
        .map((point, index) => ({ point, index, x: xOf(point) }))
        .filter((item): item is { point: OpenEatFishPoint; index: number; x: number } => (
          typeof item.x === 'number' && item.x >= -spacing * 2 && item.x <= width + spacing * 2
        ));

      const drawStick = (x: number, topValue: number, bottomValue: number, color: string, barWidth: number, alpha = 1) => {
        const yA = yOf(topValue);
        const yB = yOf(bottomValue);
        if (yA == null || yB == null) return;
        const top = Math.min(yA, yB);
        const bottom = Math.max(yA, yB);
        ctx.save();
        ctx.globalAlpha = alpha;
        ctx.fillStyle = color;
        ctx.fillRect(Math.round(x - barWidth / 2) + 0.5, top, Math.max(1, Math.round(barWidth)), Math.max(1, bottom - top));
        ctx.restore();
      };

      const drawText = (text: string, x: number, yValue: number, color: string, align: CanvasTextAlign = 'center') => {
        const y = yOf(yValue);
        if (y == null) return;
        ctx.save();
        ctx.font = `700 11px ${CHART_FONT_FAMILY}`;
        ctx.textAlign = align;
        ctx.textBaseline = 'middle';
        ctx.fillStyle = color;
        ctx.shadowColor = '#000000';
        ctx.shadowBlur = 3;
        ctx.fillText(text, x, y);
        ctx.restore();
      };

      const drawArrow = (x: number, yValue: number, color: string, direction: 'up' | 'down') => {
        const y = yOf(yValue);
        if (y == null) return;
        const size = clampNumber(spacing * 0.36, 5, 9);
        ctx.save();
        ctx.fillStyle = color;
        ctx.strokeStyle = '#000000';
        ctx.lineWidth = 0.8;
        ctx.beginPath();
        if (direction === 'up') {
          ctx.moveTo(x, y - size);
          ctx.lineTo(x + size * 0.8, y + size * 0.6);
          ctx.lineTo(x + size * 0.25, y + size * 0.6);
          ctx.lineTo(x + size * 0.25, y + size * 1.35);
          ctx.lineTo(x - size * 0.25, y + size * 1.35);
          ctx.lineTo(x - size * 0.25, y + size * 0.6);
          ctx.lineTo(x - size * 0.8, y + size * 0.6);
        } else {
          ctx.moveTo(x, y + size);
          ctx.lineTo(x + size * 0.8, y - size * 0.6);
          ctx.lineTo(x + size * 0.25, y - size * 0.6);
          ctx.lineTo(x + size * 0.25, y - size * 1.35);
          ctx.lineTo(x - size * 0.25, y - size * 1.35);
          ctx.lineTo(x - size * 0.25, y - size * 0.6);
          ctx.lineTo(x - size * 0.8, y - size * 0.6);
        }
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
        ctx.restore();
      };

      const drawDiamond = (x: number, yValue: number, fill = VIP_COLORS.diamond, stroke = '#ffc0cb') => {
        const y = yOf(yValue);
        if (y == null) return;
        const r = clampNumber(spacing * 0.24, 4, 7);
        ctx.save();
        ctx.fillStyle = fill;
        ctx.strokeStyle = stroke;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x, y - r);
        ctx.lineTo(x + r * 1.3, y);
        ctx.lineTo(x, y + r);
        ctx.lineTo(x - r * 1.3, y);
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
        ctx.restore();
      };

      // 彩带(2026.07.08版):红绿互斥无重叠——强势红带填 ABC2↔生命价线;
      // 弱势绿带悬挂在下方:生命价线→MIN(ABC2,生命价线)×0.97
      visiblePoints.forEach(({ point, x }) => {
        if (!Number.isFinite(point.abc2) || !Number.isFinite(point.lifeLine)) return;
        if (point.strong) {
          drawStick(x, point.abc2, point.lifeLine, '#ff0000', ribbonWidth, 0.88);
        } else {
          const greenLow = Math.min(point.abc2, point.lifeLine) * 0.97;
          drawStick(x, point.lifeLine, greenLow, '#00cc00', ribbonWidth, 0.88);
        }
      });
      const trendStickWidth = clampNumber(spacing * 0.48, 2, 7);
      visiblePoints.forEach(({ point, x }) => {
        if (Number.isFinite(point.a) && Number.isFinite(point.b)) {
          drawStick(x, point.a, point.b, point.a > point.b ? VIP_COLORS.yellow : '#0000ff', trendStickWidth, 0.95);
        }
      });

      let lastEatFishTextX = -Infinity;
      let lastOpenTextX = -Infinity;
      let lastProfitTextX = -Infinity;
      visiblePoints.forEach(({ point, x }) => {
        if (point.openEatFish) {
          drawArrow(x, point.base, VIP_COLORS.red, 'up');
          if (x - lastOpenTextX >= labelGap) {
            drawText('开仓吃鱼', x + spacing * 0.35, point.base + 0.09, VIP_COLORS.red, 'left');
            lastOpenTextX = x;
          }
        }
        if (point.takeProfit) {
          drawArrow(x, point.base + 0.02, '#22c55e', 'down');
          if (x - lastProfitTextX >= labelGap * 0.72) {
            drawText('及时止盈', x + spacing * 0.35, point.base + 0.11, '#22c55e', 'left');
            lastProfitTextX = x;
          }
        }
        if (point.eatFish) {
          drawArrow(x, point.base - 0.12, VIP_COLORS.cyan, 'up');
          if (x - lastEatFishTextX >= labelGap * 0.62 || point.openEatFish) {
            drawText('◆吃鱼身', x + spacing * 0.28, point.base - 0.05, VIP_COLORS.cyan, 'left');
            lastEatFishTextX = x;
          }
          drawDiamond(x, point.lifeLine * 0.91);
        }
      });

      // 顶层最后绘制:止盈绿钻石固定在彩带底部(MIN(ABC2,生命价线)×0.86),不被彩带遮挡——"绿色钻石必走"
      visiblePoints.forEach(({ point, x }) => {
        if (point.takeProfit && Number.isFinite(point.abc2) && Number.isFinite(point.lifeLine)) {
          drawDiamond(x, Math.min(point.abc2, point.lifeLine) * 0.86, '#16a34a', '#86efac');
        }
      });

      ctx.restore();
    };

    const scheduleDraw = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(draw);
    };

    scheduleDraw();
    const timeout = window.setTimeout(scheduleDraw, 120);
    chart.timeScale().subscribeVisibleLogicalRangeChange(scheduleDraw);
    const resizeObserver = new ResizeObserver(scheduleDraw);
    resizeObserver.observe(container);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(scheduleDraw);
      resizeObserver.disconnect();
    };
  }, [mainChartTemplate, isTrendLinePeriod, openEatFishMainSeries, chartViewport.width, chartViewport.height]);

  // 集合竞价独立窗:在左侧竞价时段(whitespace 占位区)自绘 通达信式 台阶价折线 + 深灰背景 + 分隔竖线。
  // y 用分时线 priceToCoordinate 精确对齐价格轴;x 用 timeToCoordinate(竞价 whitespace 点已在轴上)。
  useEffect(() => {
    const canvas = auctionOverlayCanvasRef.current;
    const container = chartContainerRef.current;
    const chart = chartRef.current;
    const series = mainSeriesRef.current;
    const datePrefix = String(safeData[0]?.time || '').slice(0, 10);
    const active = showAuction && period === '1m' && sampledAuction.length >= 2 && !!datePrefix;
    if (!canvas || !container || !chart || !series || !active) {
      if (canvas) { const ctx = canvas.getContext('2d'); ctx?.clearRect(0, 0, canvas.width, canvas.height); }
      return;
    }
    const timeScale = chart.timeScale();
    let frame = 0;
    const draw = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;
      if (width <= 0 || height <= 0) return;
      const dpr = window.devicePixelRatio || 1;
      const pw = Math.floor(width * dpr);
      const ph = Math.floor(height * dpr);
      if (canvas.width !== pw || canvas.height !== ph) { canvas.width = pw; canvas.height = ph; }
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);

      const pts = sampledAuction.map(t => {
        const x = timeScale.timeToCoordinate(parseTime(`${datePrefix} ${t.time}`));
        const y = series.priceToCoordinate(t.price);
        return (typeof x === 'number' && Number.isFinite(x) && typeof y === 'number' && Number.isFinite(y))
          ? { x: x as number, y: y as number } : null;
      }).filter((p): p is { x: number; y: number } => p !== null);
      if (pts.length < 2) return;

      const xRight = pts[pts.length - 1].x + 2;
      // 价格区裁剪(排除右侧价格刻度带),竞价台阶极端价超出上下边界时被裁而非画出界
      ctx.save();
      ctx.beginPath();
      ctx.rect(0, 0, Math.max(0, width - PRICE_SCALE_MIN_WIDTH), height);
      ctx.clip();

      // 深灰背景框 + 右侧分隔竖线
      ctx.fillStyle = 'rgba(30,41,59,0.4)';
      ctx.fillRect(0, 0, Math.max(0, xRight), height);
      ctx.strokeStyle = 'rgba(148,163,184,0.45)';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(Math.round(xRight) + 0.5, 0);
      ctx.lineTo(Math.round(xRight) + 0.5, height);
      ctx.stroke();

      // 台阶价折线(白色):先水平到当前 x(用前一点 y),再垂直到当前 y —— 通达信竞价台阶
      ctx.strokeStyle = '#e2e8f0';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(pts[0].x, pts[0].y);
      for (let i = 1; i < pts.length; i++) {
        ctx.lineTo(pts[i].x, pts[i - 1].y);
        ctx.lineTo(pts[i].x, pts[i].y);
      }
      ctx.stroke();
      ctx.restore();
    };
    const scheduleDraw = () => { window.cancelAnimationFrame(frame); frame = window.requestAnimationFrame(draw); };
    scheduleDraw();
    const timeout = window.setTimeout(scheduleDraw, 120);
    timeScale.subscribeVisibleLogicalRangeChange(scheduleDraw);
    const ro = new ResizeObserver(scheduleDraw);
    ro.observe(container);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
      timeScale.unsubscribeVisibleLogicalRangeChange(scheduleDraw);
      ro.disconnect();
    };
  }, [showAuction, period, sampledAuction, safeData, chartColors, chartViewport.width, chartViewport.height]);

  useEffect(() => {
    const canvas = vipOverlayCanvasRef.current;
    const container = volumeContainerRef.current;
    const volumeChart = volumeChartRef.current;
    if (!canvas || !container || !volumeChart || !showVipOverlayCanvas) {
      if (canvas) {
        const ctx = canvas.getContext('2d');
        ctx?.clearRect(0, 0, canvas.width, canvas.height);
      }
      return;
    }

    let frame = 0;
    const anomalyPoints = buildVipAnomalySeries(safeData).points;
    const shortEnergyPoints = buildVipShortEnergySeries(safeData).points;
    const fiveDragonPoints = buildVipFiveDragonSeries(safeData).points;
    const pointsForType = (type: SubChartType) => {
      if (type === 'vipShortEnergy') return shortEnergyPoints;
      if (type === 'vipFiveDragon') return fiveDragonPoints;
      return anomalyPoints;
    };
    const paneTypes: SubChartType[] = gridMode ? [subChartType, subType2, subType3] : [subChartType];
    const timeScale = volumeChart.timeScale();
    const draw = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;
      if (width <= 0 || height <= 0) return;

      const dpr = window.devicePixelRatio || 1;
      const pixelWidth = Math.floor(width * dpr);
      const pixelHeight = Math.floor(height * dpr);
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth;
        canvas.height = pixelHeight;
      }
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;

      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);

      const panes = volumeChart.panes();
      const getPaneRect = (paneIndex: number) => {
        const paneElement = panes[paneIndex]?.getHTMLElement?.();
        if (paneElement) {
          const containerRect = container.getBoundingClientRect();
          const paneRect = paneElement.getBoundingClientRect();
          const top = clampNumber(paneRect.top - containerRect.top, 0, height);
          const paneHeight = clampNumber(paneRect.height, 1, Math.max(1, height - top));
          return { top, height: paneHeight };
        }
        const paneCount = gridMode ? 3 : 1;
        const paneHeight = height / paneCount;
        return { top: paneIndex * paneHeight, height: paneHeight };
      };

      const drawVipPane = (paneType: SubChartType, paneIndex: number) => {
        if (!isVipSubChartType(paneType)) return;
        const axisSeries = vipAxisPaneRefs.current[paneIndex] || (paneIndex === 0 ? vipAxisSeriesRef.current : null);
        const points = pointsForType(paneType);
        if (!axisSeries || points.length === 0) return;
        const paneRect = getPaneRect(paneIndex);
        const paneTop = paneRect.top;
        const paneHeight = paneRect.height;

        ctx.save();
        ctx.beginPath();
        ctx.rect(0, paneTop, Math.max(0, width - PRICE_SCALE_MIN_WIDTH), paneHeight);
        ctx.clip();
        try {
      const xOf = (point: { time: Time }) => {
        const x = timeScale.timeToCoordinate(point.time);
        const numeric = typeof x === 'number' ? Number(x) : NaN;
        return Number.isFinite(numeric) ? numeric : null;
      };
      const yOf = (value: number) => {
        const y = axisSeries.priceToCoordinate(value);
        const numeric = typeof y === 'number' ? Number(y) : NaN;
        return Number.isFinite(numeric) ? numeric + paneTop : null;
      };
      const xs = points
        .map(point => xOf(point))
        .filter((x): x is number => typeof x === 'number')
        .sort((a, b) => a - b);
      let spacing = 12;
      for (let i = 1; i < xs.length; i += 1) {
        const gap = xs[i] - xs[i - 1];
        if (gap > 1) spacing = Math.min(spacing, gap);
      }
      spacing = clampNumber(spacing, 4, 26);
      const baseWidth = clampNumber(spacing * 0.72, 5, 16);
      const midWidth = clampNumber(spacing * 0.48, 4, 11);
      const narrowWidth = clampNumber(spacing * 0.3, 2, 7);
      const labelGap = clampNumber(spacing * 6.5, 46, 92);

      const drawStick = (x: number, yTopValue: number, yBottomValue: number, color: string, barWidth: number, alpha = 1) => {
        const yA = yOf(yTopValue);
        const yB = yOf(yBottomValue);
        if (yA == null || yB == null) return;
        const top = Math.min(yA, yB);
        const bottom = Math.max(yA, yB);
        const h = Math.max(1, bottom - top);
        ctx.save();
        ctx.globalAlpha = alpha;
        ctx.fillStyle = color;
        ctx.fillRect(Math.round(x - barWidth / 2) + 0.5, top, Math.max(1, Math.round(barWidth)), h);
        ctx.restore();
      };

      const drawOutlinedStick = (x: number, yTopValue: number, yBottomValue: number, color: string, barWidth: number) => {
        const yA = yOf(yTopValue);
        const yB = yOf(yBottomValue);
        if (yA == null || yB == null) return;
        const top = Math.min(yA, yB);
        const bottom = Math.max(yA, yB);
        const h = Math.max(1, bottom - top);
        ctx.save();
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.25;
        ctx.strokeRect(Math.round(x - barWidth / 2) + 0.5, top + 0.5, Math.max(1, Math.round(barWidth)), Math.max(1, h - 1));
        ctx.restore();
      };

      const drawLine = (values: number[], color: string, widthPx: number) => {
        ctx.save();
        ctx.strokeStyle = color;
        ctx.lineWidth = widthPx;
        ctx.lineJoin = 'round';
        ctx.lineCap = 'round';
        ctx.beginPath();
        let open = false;
        points.forEach((point, index) => {
          const x = xOf(point);
          const y = yOf(values[index]);
          if (x == null || y == null) {
            open = false;
            return;
          }
          if (!open) {
            ctx.moveTo(x, y);
            open = true;
          } else {
            ctx.lineTo(x, y);
          }
        });
        ctx.stroke();
        ctx.restore();
      };

      const drawText = (text: string, x: number, yValue: number, color: string, size = 11, align: CanvasTextAlign = 'center') => {
        const y = yOf(yValue);
        if (y == null) return;
        ctx.save();
        ctx.font = `700 ${size}px ${CHART_FONT_FAMILY}`;
        ctx.textAlign = align;
        ctx.textBaseline = 'middle';
        ctx.fillStyle = color;
        ctx.shadowColor = '#000000';
        ctx.shadowBlur = 2;
        ctx.fillText(text, x, y);
        ctx.restore();
      };

      const drawDiamond = (x: number, yValue: number, color: string) => {
        const y = yOf(yValue);
        if (y == null) return;
        const r = clampNumber(spacing * 0.23, 4, 7);
        ctx.save();
        ctx.fillStyle = color;
        ctx.strokeStyle = '#ffc0cb';
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x, y - r);
        ctx.lineTo(x + r * 1.35, y);
        ctx.lineTo(x, y + r);
        ctx.lineTo(x - r * 1.35, y);
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
        ctx.restore();
      };

      const visiblePoints = points
        .map((point, index) => ({ point, index, x: xOf(point) }))
        .reduce<Array<{ point: (typeof points)[number]; index: number; x: number }>>((acc, item) => {
          if (typeof item.x === 'number' && item.x >= -spacing && item.x <= width + spacing) {
            acc.push({ point: item.point, index: item.index, x: item.x });
          }
          return acc;
        }, []);

      if (paneType === 'vipFiveDragon') {
        const dragonVisible = visiblePoints as Array<{ point: VipFiveDragonPoint; index: number; x: number }>;
        const rowWidth = clampNumber(spacing * 0.58, 4, 13);
        const controlWidth = clampNumber(spacing * 0.42, 3, 10);
        const textGap = clampNumber(spacing * 0.72, 6, 14);
        const leftLabelX = 14;
        const drawRow = (bull: boolean, x: number, top: number, bottom: number) => {
          drawStick(x, top, bottom, bull ? VIP_COLORS.red : VIP_COLORS.green, rowWidth, 0.98);
        };
        const drawFixedText = (text: string, x: number, yValue: number, color: string, size = 11, align: CanvasTextAlign = 'center') => {
          drawText(text, x, yValue, color, size, align);
        };

        dragonVisible.forEach(({ point, x }) => {
          if (point.controlDegree >= 80) {
            drawStick(x, 4.5, 4, VIP_COLORS.magenta, controlWidth, 0.98);
          } else if (point.controlDegree >= 60) {
            drawStick(x, 4.5, 4, VIP_COLORS.lired, controlWidth, 0.98);
          } else if (point.controlDegree >= 58) {
            drawStick(x, 4.5, 4, VIP_COLORS.yellow, controlWidth, 0.98);
          }
        });

        let lastSignalX = -Infinity;
        dragonVisible.forEach(({ point, x }) => {
          if (x - lastSignalX >= textGap) {
            drawFixedText(point.buySignal ? '买' : '卖', x, 3.2, point.buySignal ? VIP_COLORS.red : VIP_COLORS.green, 10);
            lastSignalX = x;
          }
          drawRow(point.trendBull, x, 2.5, 1.5);
          drawRow(point.energyBull, x, 1, 0);
          drawRow(point.midBull, x, -0.5, -1.5);
          drawRow(point.shortBull, x, -2, -3);
        });

        [
          { text: '控盘', y: 3.05 },
          { text: '趋势', y: 2.0 },
          { text: '量能', y: 0.5 },
          { text: '中期', y: -1.0 },
          { text: '短期', y: -2.5 },
        ].forEach(item => {
          drawFixedText(item.text, leftLabelX, item.y, VIP_COLORS.yellow, 11, 'left');
        });
        return;
      }

      if (paneType === 'vipShortEnergy') {
        const shortVisible = visiblePoints as Array<{ point: VipShortEnergyPoint; index: number; x: number }>;
        const controlWidth = clampNumber(spacing * 0.42, 3, 9);
        const energyWidth = clampNumber(spacing * 0.66, 4, 13);
        const ribbonWidth = clampNumber(spacing * 0.54, 3, 10);
        const slimWidth = clampNumber(spacing * 0.28, 2, 6);
        const shortLabelGap = clampNumber(spacing * 5, 38, 78);

        // 通达信原始层级：先画量能彩带，再叠加主力控盘柱。
        shortVisible.forEach(({ point, index, x }) => {
          const prevPoint = shortEnergyPoints[index - 1];
          const prev = prevPoint ? prevPoint.sz2 * 8 : point.sz2 * 8;
          const current = point.sz2 * 8;
          const color = current > 80 && current > prev
            ? VIP_COLORS.hot
            : (current > prev ? VIP_COLORS.rising : VIP_COLORS.gray);
          drawStick(x, point.sz2 * 8, point.sz3 * 8, color, energyWidth, 0.78);
        });
        shortVisible.forEach(({ point, x }) => {
          if (point.sz13) {
            drawStick(x, point.sz2 * 4, point.sz3 * 4, VIP_COLORS.red, ribbonWidth, 0.86);
          } else if (point.sz2 > 0) {
            drawOutlinedStick(x, point.sz2 * 4, point.sz3 * 4, VIP_COLORS.green, ribbonWidth);
          }
        });
        shortVisible.forEach(({ point, x }) => {
          if (!point.anomaly) return;
          drawStick(x, point.sz2 * 4, point.sz2 * 16, VIP_COLORS.yellow, slimWidth, 0.98);
          drawStick(x, point.sz3 * 4, point.sz3 * 16, VIP_COLORS.yellow, slimWidth, 0.98);
        });
        shortVisible.forEach(({ point, x }) => {
          if (point.controlScaled < 0) {
            drawStick(x, point.controlScaled, 0, '#ffffff', controlWidth, 0.98);
          }
        });
        shortVisible.forEach(({ point, index, x }) => {
          if (point.controlScaled <= 0) return;
          const prev = shortEnergyPoints[index - 1]?.controlScaled ?? point.controlScaled;
          const rising = point.controlScaled > prev;
          const color = point.controlWinnerProxy ? VIP_COLORS.magenta : (rising ? VIP_COLORS.red : VIP_COLORS.green);
          drawStick(x, point.controlScaled, 0, color, controlWidth, 0.98);
        });

        const yZero = yOf(0);
        if (yZero != null) {
          ctx.save();
          ctx.strokeStyle = '#ffffff';
          ctx.globalAlpha = 0.72;
          ctx.lineWidth = 1;
          ctx.beginPath();
          ctx.moveTo(0, yZero);
          ctx.lineTo(Math.max(0, width - PRICE_SCALE_MIN_WIDTH), yZero);
          ctx.stroke();
          ctx.restore();
        }

        type VipShortLabel = {
          text: string;
          x: number;
          yValue: number;
          color: string;
          size?: number;
          priority: number;
        };
        const topLabels: VipShortLabel[] = [];
        const bottomLabels: VipShortLabel[] = [];
        const addShortLabel = (target: VipShortLabel[], label: VipShortLabel, direction: 'up' | 'down') => {
          const y = yOf(label.yValue);
          if (y == null) return;
          const sameXLabels = [...topLabels, ...bottomLabels].filter(item => Math.abs(item.x - label.x) <= Math.max(2, spacing * 0.42));
          const occupied = sameXLabels
            .map(item => yOf(item.yValue))
            .filter((itemY): itemY is number => typeof itemY === 'number');
          const minGap = label.size ? label.size + 2 : 13;
          let adjustedY = y;
          let guard = 0;
          while (occupied.some(itemY => Math.abs(itemY - adjustedY) < minGap) && guard < 8) {
            adjustedY += direction === 'up' ? -minGap : minGap;
            guard += 1;
          }
          const adjustedValue = axisSeries.coordinateToPrice(adjustedY - paneTop);
          target.push({
            ...label,
            yValue: typeof adjustedValue === 'number' && Number.isFinite(adjustedValue) ? Number(adjustedValue) : label.yValue,
          });
        };

        let lastTopTextX = -Infinity;
        let lastBottomTextX = -Infinity;
        shortVisible.forEach(({ point, x }) => {
          if (point.anomaly && x - lastTopTextX >= shortLabelGap) {
            addShortLabel(topLabels, { text: '异动', x, yValue: point.sz2 * 20, color: VIP_COLORS.yellow, size: 11, priority: 20 }, 'up');
            lastTopTextX = x;
          }
          if (point.strongCondition && x - lastTopTextX >= shortLabelGap * 0.55) {
            const text = point.strongCount === 1 ? '转强' : point.strongCount === 2 ? '主升' : '超强';
            const yValue = point.strongCount === 1 ? point.sz2 * 21 : point.strongCount === 2 ? point.sz2 * 22 : point.sz2 * 23;
            addShortLabel(topLabels, {
              text,
              x,
              yValue,
              color: point.strongCount >= 3 ? VIP_COLORS.magenta : VIP_COLORS.red,
              size: 11,
              priority: 30,
            }, 'up');
            lastTopTextX = x;
          }
          if (point.startControl && x - lastTopTextX >= shortLabelGap * 0.45) {
            addShortLabel(topLabels, { text: '主力★拉升', x, yValue: point.sz2 * 26, color: VIP_COLORS.yellow, size: 11, priority: 40 }, 'up');
            lastTopTextX = x;
          }
          if (point.macdGolden && x - lastTopTextX >= shortLabelGap * 0.45) {
            addShortLabel(topLabels, { text: '↑金叉', x, yValue: point.sz2 * 29 + 2, color: VIP_COLORS.red, size: 11, priority: 10 }, 'up');
            lastTopTextX = x;
          }
          if (point.macdDead && x - lastTopTextX >= shortLabelGap * 0.45) {
            addShortLabel(topLabels, { text: '↓死叉', x, yValue: point.sz2 * 32 + 2, color: VIP_COLORS.green, size: 11, priority: 10 }, 'up');
            lastTopTextX = x;
          }
          if (point.fire && x - lastBottomTextX >= shortLabelGap * 0.62) {
            addShortLabel(bottomLabels, { text: '点火', x, yValue: point.sz2 * -16, color: VIP_COLORS.yellow, size: 11, priority: 20 }, 'down');
            lastBottomTextX = x;
          }
          if (point.mainShip && x - lastBottomTextX >= shortLabelGap * 0.45) {
            addShortLabel(bottomLabels, { text: '减仓', x, yValue: point.sz2 * -20, color: '#ffffff', size: 11, priority: 30 }, 'down');
            addShortLabel(bottomLabels, { text: '↓', x: x + 16, yValue: point.sz2 * -20, color: VIP_COLORS.green, size: 13, priority: 31 }, 'down');
            lastBottomTextX = x;
          }
        });
        [...topLabels, ...bottomLabels]
          .sort((a, b) => a.priority - b.priority)
          .forEach(label => drawText(label.text, label.x, label.yValue, label.color, label.size ?? 11));
        return;
      }
      const anomalyVisible = visiblePoints as Array<{ point: VipAnomalyPoint; index: number; x: number }>;
      const anomalyPointsForDraw = points as VipAnomalyPoint[];

      // TDX layer 1-3: base full stickline from 森舟2*2 to 森舟3*2.
      anomalyVisible.forEach(({ point, index, x }) => {
        drawStick(x, point.sz2 * 2, point.sz3 * 2, getVipBaseEnergyColor(point, anomalyPointsForDraw[index - 1]), baseWidth, 0.95);
      });

      // TDX layer 4-5: red/green middle sticks from 森舟2 to 森舟3, narrower and on top.
      anomalyVisible.forEach(({ point, x }) => {
        if (point.sz13) {
          drawStick(x, point.sz2, point.sz3, VIP_COLORS.red, midWidth, 0.92);
        } else if (point.sz2 > 0) {
          drawOutlinedStick(x, point.sz2, point.sz3, VIP_COLORS.green, midWidth);
        }
      });

      // TDX layer 6: 吃鱼身 cyan covers both full and middle range.
      anomalyVisible.forEach(({ point, x }) => {
        if (!point.eatFish) return;
        drawStick(x, point.sz2 * 2, point.sz3 * 2, VIP_COLORS.cyan, baseWidth, 0.9);
        drawStick(x, point.sz2, point.sz3, VIP_COLORS.cyan, midWidth, 0.98);
      });

      // TDX layer 7: 异动 yellow highlight from inner bar to 4x outer.
      anomalyVisible.forEach(({ point, x }) => {
        if (!point.anomaly) return;
        drawStick(x, point.sz2, point.sz2 * 4, VIP_COLORS.yellow, narrowWidth, 0.98);
        drawStick(x, point.sz3, point.sz3 * 4, VIP_COLORS.yellow, narrowWidth, 0.98);
      });

      // Purple strength lines and center axis, drawn above bars.
      const yZero = yOf(0);
      if (yZero != null) {
        ctx.save();
        ctx.strokeStyle = VIP_COLORS.magenta;
        ctx.lineWidth = 1;
        ctx.globalAlpha = 0.95;
        ctx.beginPath();
        ctx.moveTo(0, yZero);
        ctx.lineTo(Math.max(0, width - PRICE_SCALE_MIN_WIDTH), yZero);
        ctx.stroke();
        ctx.restore();
      }
      drawLine(anomalyPointsForDraw.map(point => point.purpleTop), VIP_COLORS.magenta, 2.4);
      drawLine(anomalyPointsForDraw.map(point => point.purpleBottom), VIP_COLORS.magenta, 2.4);

      let lastAnomalyTextX = -Infinity;
      let lastEatFishTextX = -Infinity;
      let lastSuperTextX = -Infinity;
      type VipLabel = {
        text: string;
        x: number;
        yValue: number;
        color: string;
        size?: number;
        align?: CanvasTextAlign;
        priority: number;
        layer: number;
      };
      const normalLabels: VipLabel[] = [];
      const eatFishLabels: VipLabel[] = [];
      const addLabel = (target: VipLabel[], label: VipLabel) => {
        const y = yOf(label.yValue);
        if (y == null) return;
        const sameXLabels = [...normalLabels, ...eatFishLabels].filter(item => Math.abs(item.x - label.x) <= Math.max(2, spacing * 0.42));
        const occupied = sameXLabels
          .map(item => yOf(item.yValue))
          .filter((itemY): itemY is number => typeof itemY === 'number');
        const minGap = label.size ? label.size + 2 : 13;
        let adjustedY = y;
        let guard = 0;
        while (occupied.some(itemY => Math.abs(itemY - adjustedY) < minGap) && guard < 8) {
          adjustedY -= minGap;
          guard += 1;
        }
        const adjustedValue = axisSeries.coordinateToPrice(adjustedY - paneTop);
        target.push({
          ...label,
          yValue: typeof adjustedValue === 'number' && Number.isFinite(adjustedValue) ? Number(adjustedValue) : label.yValue,
        });
      };
      let lastEscapeTextX = -Infinity;
      anomalyVisible.forEach(({ point, x }) => {
        const showAnomalyText = point.anomaly && (x - lastAnomalyTextX >= labelGap || point.superSignal);
        const showEatFishText = point.eatFish && (x - lastEatFishTextX >= labelGap || point.superSignal);
        const showSuperText = point.superSignal && x - lastSuperTextX >= labelGap * 0.72;
        // 6.0:鱼头文字仅多头红彩带出现(anomaly 已含强势门控)
        if (point.anomaly && showAnomalyText) {
          drawText('异动', x, point.sz2 * 4 + 20, VIP_COLORS.yellow, 11);
          drawText('鱼头', x, point.sz3 * 4 - 40, VIP_COLORS.red, 11);
          lastAnomalyTextX = x;
        }
        // 6.0 逃顶预警:下箭头+白字,规避高位回落
        if (point.escape && x - lastEscapeTextX >= labelGap * 0.72) {
          drawText('↓', x, point.sz3 * 4 - 25, VIP_COLORS.green, 13);
          drawText('逃顶', x, point.sz3 * 4 - 45, '#ffffff', 11);
          lastEscapeTextX = x;
        }
        if (showSuperText) {
          const text = point.superCount === 1
            ? '转强1起爆'
            : point.superCount === 2
              ? '2主升'
              : point.superCount === 3
                ? '3超强'
                : '超强';
          addLabel(normalLabels, {
            text,
            x,
            yValue: point.sz2 * 4 + 5,
            color: point.superCount >= 3 ? VIP_COLORS.magenta : VIP_COLORS.red,
            size: 11,
            priority: 30,
            layer: 2,
          });
          lastSuperTextX = x;
        }
        if (point.eatFish) {
          if (showEatFishText) {
            addLabel(eatFishLabels, {
              text: '◆吃鱼身',
              x: x + spacing * 0.18,
              yValue: point.sz2 * 4 + 12,
              color: VIP_COLORS.cyan,
              size: 11,
              align: 'left',
              priority: 100,
              layer: 3,
            });
            lastEatFishTextX = x;
          }
          drawDiamond(x, point.sz2 - 35, VIP_COLORS.diamond);
        }
      });
      // 6.0 顶层最后绘制:能量柱顶部的逃顶绿钻石("绿色钻石卖出")
      anomalyVisible.forEach(({ point, x }) => {
        if (point.escape) drawDiamond(x, point.sz2 * 4 + 30, '#16a34a');
      });
      normalLabels
        .sort((a, b) => a.layer - b.layer || a.priority - b.priority)
        .forEach(label => drawText(label.text, label.x, label.yValue, label.color, label.size || 11, label.align || 'center'));
      eatFishLabels
        .sort((a, b) => a.x - b.x)
        .forEach(label => drawText(label.text, label.x, label.yValue, label.color, label.size || 11, label.align || 'left'));
        } finally {
          ctx.restore();
        }
      };

      paneTypes.forEach((type, index) => drawVipPane(type, index));
    };

    const scheduleDraw = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(draw);
    };

    scheduleDraw();
    const timeout = window.setTimeout(scheduleDraw, 80);
    timeScale.subscribeVisibleLogicalRangeChange(scheduleDraw);
    const resizeObserver = new ResizeObserver(scheduleDraw);
    resizeObserver.observe(container);
    const observePanes = () => {
      try {
        volumeChart
          .panes()
          .map(pane => pane.getHTMLElement?.())
          .filter((element): element is HTMLElement => Boolean(element))
          .forEach(element => resizeObserver.observe(element));
      } catch { /* pane DOM may be rebuilding */ }
    };
    observePanes();
    const paneObserveTimers = [
      window.setTimeout(observePanes, 80),
      window.setTimeout(observePanes, 240),
    ];
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
      paneObserveTimers.forEach(timer => window.clearTimeout(timer));
      timeScale.unsubscribeVisibleLogicalRangeChange(scheduleDraw);
      resizeObserver.disconnect();
    };
  }, [showVipOverlayCanvas, safeData, subChartType, subType2, subType3, gridMode, volumeHeight, gridSubHeight, chartColors.gridColor, vipOverlayRevision]);

  useEffect(() => {
    const container = volumeContainerRef.current;
    const volumeChart = volumeChartRef.current;
    if (!container || !volumeChart || !hasData) {
      setSubPaneRects([]);
      return;
    }

    let frame = 0;
    const paneCount = gridMode ? 3 : 1;
    const updatePaneRects = () => {
      const currentContainer = volumeContainerRef.current;
      const currentChart = volumeChartRef.current;
      if (!currentContainer || !currentChart) return;
      const height = currentContainer.clientHeight;
      const containerRect = currentContainer.getBoundingClientRect();
      const panes = currentChart.panes();
      const next = Array.from({ length: paneCount }, (_, index) => {
        const paneElement = panes[index]?.getHTMLElement?.();
        if (paneElement) {
          const paneRect = paneElement.getBoundingClientRect();
          return {
            top: clampNumber(paneRect.top - containerRect.top, 0, Math.max(0, height)),
            height: clampNumber(paneRect.height, 1, Math.max(1, height)),
          };
        }
        const fallbackHeight = paneCount > 0 ? height / paneCount : height;
        return { top: index * fallbackHeight, height: fallbackHeight };
      });
      setSubPaneRects(prev => {
        if (
          prev.length === next.length &&
          prev.every((item, index) => (
            Math.abs(item.top - next[index].top) < 0.5 &&
            Math.abs(item.height - next[index].height) < 0.5
          ))
        ) {
          return prev;
        }
        return next;
      });
    };
    const scheduleUpdate = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(updatePaneRects);
    };

    const resizeObserver = new ResizeObserver(scheduleUpdate);
    resizeObserver.observe(container);
    const observePanes = () => {
      try {
        volumeChart
          .panes()
          .map(pane => pane.getHTMLElement?.())
          .filter((element): element is HTMLElement => Boolean(element))
          .forEach(element => resizeObserver.observe(element));
      } catch { /* pane DOM may be rebuilding */ }
      scheduleUpdate();
    };

    observePanes();
    const paneObserveTimers = [
      window.setTimeout(observePanes, 80),
      window.setTimeout(observePanes, 240),
    ];
    return () => {
      window.cancelAnimationFrame(frame);
      paneObserveTimers.forEach(timer => window.clearTimeout(timer));
      resizeObserver.disconnect();
    };
  }, [hasData, gridMode, volumeHeight, gridSubHeight, subChartType, subType2, subType3, vipOverlayRevision]);

  const compactMarkerSignals = useMemo(
    () => compactSignalsForMarkers(tradingSignals, isTrendLinePeriod),
    [tradingSignals, isTrendLinePeriod],
  );

  const signalMarkerData = useMemo<SeriesMarker<Time>[]>(() => (
    SHOW_MAIN_TRADING_MARKERS ? (
    compactMarkerSignals.map(signal => ({
      time: signal.time,
      position: signal.action === 'buy' ? 'belowBar' : 'aboveBar',
      shape: signal.action === 'buy' ? 'arrowUp' : 'arrowDown',
      color: signal.action === 'buy' ? '#f97316' : '#22c55e',
      text: markerTextForSignal(signal, isTrendLinePeriod),
      size: isTrendLinePeriod ? 0.75 : 0.9,
    }))
    ) : []
  ), [compactMarkerSignals, isTrendLinePeriod]);

  // ========== 周期变化：更新 timeScale 选项 + 交互模式 ==========
  useEffect(() => {
    const chart = chartRef.current;
    const volumeChart = volumeChartRef.current;
    if (!chart || !volumeChart) return;

    // 左键拖拽改作"框选区间"，平移导航交给滚轮，故关闭 pressedMouseMove
    const noDragPan = isTrendLinePeriod
      ? false
      : { mouseWheel: true, pressedMouseMove: false, horzTouchDrag: true, vertTouchDrag: true };
    chart.applyOptions({
      timeScale: { timeVisible: isTrendLinePeriod, secondsVisible: false },
      rightPriceScale: { minimumWidth: PRICE_SCALE_MIN_WIDTH },
      handleScroll: noDragPan,
      handleScale: !isTrendLinePeriod,
    });
    volumeChart.applyOptions({
      timeScale: { timeVisible: isTrendLinePeriod, secondsVisible: false },
      rightPriceScale: { minimumWidth: PRICE_SCALE_MIN_WIDTH },
      handleScroll: noDragPan,
      handleScale: !isTrendLinePeriod,
    });
  }, [isTrendLinePeriod]);

  // 切换周期时重置 fit 状态，避免沿用旧 X 轴可视范围
  useEffect(() => {
    hasFittedRef.current = false;
  }, [period, stock?.symbol, visibleRangeBars]);

  // 走势模式固定显示成交量副图，避免隐藏 tab 后无法恢复
  useEffect(() => {
    if (isTrendLinePeriod) {
      if (subChartTypeRef.current !== 'volume') {
        setSubChartType('volume');
        subChartTypeRef.current = 'volume';
      }
      if (gridModeRef.current) {
        gridModeRef.current = false;
        gridModeRef2.current = false;
        setGridMode(false);
      }
      return;
    }
    if (!gridModeRef.current) {
      gridModeRef.current = true;
      gridModeRef2.current = true;
      setGridMode(true);
    }
  }, [isTrendLinePeriod]);

  // ========== 副图辅助函数 ==========
  // 清空所有副图 pane 的 series，并移除多出来的 pane（恢复单副图）
  const clearSubChart = useCallback(() => {
    const vc = volumeChartRef.current;
    if (!vc) return;
    for (const arr of subPaneRefs.current) {
      for (const s of arr) {
        try { vc.removeSeries(s); } catch { /* already removed */ }
      }
    }
    subPaneRefs.current = [[], [], []];
    vipAxisSeriesRef.current = null;
    vipAxisPaneRefs.current = [null, null, null];
    volumeSeriesRef.current = null;
    subSeriesRefs.current = [];
    try {
      const ps = vc.panes();
      for (let i = ps.length - 1; i >= 1; i--) vc.removePane(i);
    } catch { /* removePane 不可用则忽略 */ }
  }, []);

  // 把单个指标渲染进副图的指定 pane，返回所建 series
  const buildSub = useCallback((type: SubChartType, chartData: KLineData[], paneIndex: number): ISeriesApi<SeriesType, Time>[] => {
    const vc = volumeChartRef.current;
    if (!vc || chartData.length === 0) return [];
    const base = { priceLineVisible: false, lastValueVisible: false };
    if (typeof type === 'string' && type.startsWith('tdx:')) {
      // 通达信公式副图:引擎求值 → 柱(直方近似)+线+标记
      const formula = getTdxFormula(type);
      if (!formula) return [];
      const out = evaluateTdxFormula(formula.id, formula.source, chartData, {
        code: stock?.symbol, name: stock?.name,
        ...(stock?.symbol ? finCtxCache.get(stock.symbol) : undefined),
      });
      const series: ISeriesApi<SeriesType, Time>[] = [];
      const times = chartData.map(d => parseTime(d.time));
      for (const st of out.sticks) {
        const hs = vc.addSeries(HistogramSeries, { ...base, color: st.color || '#64748b', title: '' }, paneIndex);
        hs.setData(times.map((t, i) => (st.segs[i]
          ? { time: t, value: st.segs[i]![1], color: st.color || '#64748b' }
          : { time: t, value: 0, color: 'rgba(0,0,0,0)' })) as HistogramData[]);
        series.push(hs);
      }
      for (const ln of out.lines) {
        const lsr = vc.addSeries(LineSeries, {
          ...base,
          color: ln.color || '#38bdf8',
          lineWidth: Math.min(4, Math.max(1, ln.width || 1)) as 1 | 2 | 3 | 4,
          lineStyle: ln.dotted ? LineStyle.Dotted : LineStyle.Solid,
          title: '',
        }, paneIndex);
        lsr.setData(times.map((t, i) => (ln.values[i] == null ? { time: t } : { time: t, value: ln.values[i] as number })) as LineData[]);
        series.push(lsr);
      }
      if (out.markers.length > 0 && series.length === 0) {
        // 只有标记没有线/柱的公式:加一条透明零线承载标记
        const host = vc.addSeries(LineSeries, { ...base, color: 'rgba(0,0,0,0)', title: '' }, paneIndex);
        host.setData(times.map(t => ({ time: t, value: 0 })) as LineData[]);
        series.push(host);
      }
      if (out.markers.length > 0 && series.length > 0) {
        createSeriesMarkers(series[series.length - 1], out.markers.map(m => ({
          time: times[m.index],
          position: m.above ? 'aboveBar' as const : 'belowBar' as const,
          color: m.color || '#eab308',
          shape: m.above ? 'arrowDown' as const : 'arrowUp' as const,
          text: m.text,
        })));
      }
      return series;
    }
    if (type === 'volume') {
      const s = vc.addSeries(HistogramSeries, { priceFormat: { type: 'volume' }, priceLineVisible: false, lastValueVisible: false }, paneIndex);
      const volData = chartData.map(d => ({
        time: parseTime(d.time), value: d.volume,
        color: d.close >= d.open ? chartColors.upColor + '99' : chartColors.downColor + '99',
      })) as HistogramData[];
      // 分时+竞价开关开:量图前拼竞价匹配量柱(与主图竞价段同点位,保证上下两图时间轴对齐)
      if (showAuction && period === '1m' && sampledAuction.length >= 2) {
        const datePrefix = String(chartData[0]?.time || '').slice(0, 10);
        if (datePrefix) {
          const preClosePrice = preClose;
          const auctionBars = sampledAuction.map(t => ({
            time: parseTime(`${datePrefix} ${t.time}`),
            value: t.volume,
            color: (preClosePrice > 0 && t.price < preClosePrice ? chartColors.downColor : chartColors.upColor) + '66',
          })) as HistogramData[];
          s.setData([...auctionBars, ...volData]);
          return [s];
        }
      }
      s.setData(volData);
      return [s];
    }
    if (type === 'macd') {
      const { dif, dea, histogram } = calculateMACD(chartData, indicatorConfig.macd.fast, indicatorConfig.macd.slow, indicatorConfig.macd.signal);
      const a = vc.addSeries(LineSeries, { ...base, color: '#3b82f6', lineWidth: 1, title: '' }, paneIndex); a.setData(dif);
      const b = vc.addSeries(LineSeries, { ...base, color: '#eab308', lineWidth: 1, title: '' }, paneIndex); b.setData(dea);
      const c = vc.addSeries(HistogramSeries, { ...base, title: '' }, paneIndex); c.setData(histogram as any);
      return [a, b, c];
    }
    if (type === 'rsi') {
      const r = vc.addSeries(LineSeries, { ...base, color: '#a855f7', lineWidth: 1, title: '' }, paneIndex);
      r.setData(calculateRSI(chartData, indicatorConfig.rsi.period));
      r.createPriceLine({ price: 70, color: '#ef444480', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      r.createPriceLine({ price: 30, color: '#22c55e80', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [r];
    }
    if (type === 'kdj') {
      const { k, d, j } = calculateKDJ(chartData, indicatorConfig.kdj.period, indicatorConfig.kdj.k, indicatorConfig.kdj.d);
      const ks = vc.addSeries(LineSeries, { ...base, color: '#3b82f6', lineWidth: 1, title: '' }, paneIndex); ks.setData(k);
      const ds = vc.addSeries(LineSeries, { ...base, color: '#eab308', lineWidth: 1, title: '' }, paneIndex); ds.setData(d);
      const js = vc.addSeries(LineSeries, { ...base, color: '#a855f7', lineWidth: 1, title: '' }, paneIndex); js.setData(j);
      return [ks, ds, js];
    }
    if (type === 'cci') {
      const s = vc.addSeries(LineSeries, { ...base, color: '#06b6d4', lineWidth: 1, title: '' }, paneIndex);
      s.setData(calculateCCI(chartData, 14));
      s.createPriceLine({ price: 100, color: '#ef444480', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      s.createPriceLine({ price: -100, color: '#22c55e80', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s];
    }
    if (type === 'wr') {
      const s = vc.addSeries(LineSeries, { ...base, color: '#f59e0b', lineWidth: 1, title: '' }, paneIndex);
      s.setData(calculateWR(chartData, 14));
      s.createPriceLine({ price: -20, color: '#ef444480', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      s.createPriceLine({ price: -80, color: '#22c55e80', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s];
    }
    if (type === 'volumeSupport') {
      const volumes = chartData.map(item => item.volume || 0);
      const vol20 = simpleMovingNumbers(volumes, 20);
      const s = vc.addSeries(HistogramSeries, { priceFormat: { type: 'volume' }, priceLineVisible: false, lastValueVisible: false }, paneIndex);
      s.setData(chartData.map((d, index) => {
        const isShrink = Number.isFinite(vol20[index]) && vol20[index] > 0 && d.volume <= vol20[index] * 0.82;
        const isStable = d.close >= Math.max(d.open, d.low + (d.high - d.low) * 0.45);
        return {
          time: parseTime(d.time),
          value: d.volume,
          color: isShrink && isStable ? '#f97316aa' : (d.close >= d.open ? chartColors.upColor + '77' : chartColors.downColor + '77'),
        };
      }) as HistogramData[]);
      const v5 = vc.addSeries(LineSeries, { ...base, color: '#eab308', lineWidth: 1, title: '' }, paneIndex);
      v5.setData(buildVolumeMA(chartData, 5));
      const v20 = vc.addSeries(LineSeries, { ...base, color: '#38bdf8', lineWidth: 1, title: '' }, paneIndex);
      v20.setData(buildVolumeMA(chartData, 20));
      return [s, v5, v20];
    }
    if (type === 'mainFundFlow' || type === 'fundTurn') {
      const realPoints = fundFlowPoints.length > 0 ? fundFlowPoints : [];
      const proxyPoints = realPoints.length > 0 ? [] : buildProxyFundFlow(chartData);
      const points = realPoints.length > 0 ? realPoints : proxyPoints;
      const s = vc.addSeries(HistogramSeries, { ...base, title: '' }, paneIndex);
      s.setData(points.map(point => ({
        time: point.time,
        value: point.mainNet,
        color: point.mainNet >= 0 ? '#ef4444aa' : '#22c55eaa',
      })) as HistogramData[]);
      const mainValues = points.map(point => point.mainNet);
      const ma = simpleMovingNumbers(mainValues, type === 'fundTurn' ? 3 : 5)
        .map((value, index) => ({ time: points[index]?.time, value }))
        .filter((point): point is LineData => Boolean(point.time) && Number.isFinite(point.value));
      const l = vc.addSeries(LineSeries, {
        ...base,
        color: realPoints.length > 0 ? '#f97316' : '#94a3b8',
        lineWidth: 1,
        title: '',
      }, paneIndex);
      l.setData(ma);
      l.createPriceLine({ price: 0, color: '#64748b99', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s, l];
    }
    if (type === 'lowBuySignal') {
      const score = buildLowBuySignalScore(chartData, tradingSignals);
      const s = vc.addSeries(LineSeries, { ...base, color: '#f97316', lineWidth: 2, title: '' }, paneIndex);
      s.setData(score.map(({ time, value }) => ({ time, value })));
      s.createPriceLine({ price: 75, color: '#f9731688', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      s.createPriceLine({ price: 55, color: '#64748b88', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s];
    }
    if (type === 'trendStrength') {
      const score = buildTrendStrengthScore(chartData);
      const s = vc.addSeries(LineSeries, { ...base, color: '#ef4444', lineWidth: 2, title: '' }, paneIndex);
      s.setData(score.map(({ time, value }) => ({ time, value })));
      s.createPriceLine({ price: 65, color: '#ef444488', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      s.createPriceLine({ price: 45, color: '#22c55e88', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s];
    }
    if (type === 'sellRisk') {
      const score = buildSellRiskScore(chartData, tradingSignals);
      const s = vc.addSeries(LineSeries, { ...base, color: '#22c55e', lineWidth: 2, title: '' }, paneIndex);
      s.setData(score.map(({ time, value }) => ({ time, value })));
      s.createPriceLine({ price: 70, color: '#22c55e88', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      s.createPriceLine({ price: 45, color: '#f59e0b88', lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: false, title: '' });
      return [s];
    }
    if (type === 'vipAnomaly' || type === 'vipShortEnergy' || type === 'vipFiveDragon') {
      const isFiveDragon = type === 'vipFiveDragon';
      const isShortEnergy = type === 'vipShortEnergy';
      const vip = isFiveDragon
        ? buildVipFiveDragonSeries(chartData)
        : isShortEnergy
          ? buildVipShortEnergySeries(chartData)
          : buildVipAnomalySeries(chartData);
      const points = vip.points;
      const refs: ISeriesApi<SeriesType, Time>[] = [];
      const addLine = (data: LineData[], color: string, lineWidth = 1) => {
        const s = vc.addSeries(LineSeries, {
          ...base,
          color,
          lineWidth: lineWidth as 1 | 2 | 3 | 4,
          title: '',
          crosshairMarkerVisible: false,
        }, paneIndex);
        s.setData(data);
        refs.push(s);
        return s;
      };

      const values = isFiveDragon
        ? [-3.2, 4.7]
        : isShortEnergy
          ? (points as VipShortEnergyPoint[]).flatMap(point => [
          point.sz2 * 32 + 2,
          point.sz3 * 20,
          point.sz2 * 16,
          point.sz3 * 16,
          point.controlScaled,
        ])
          : (points as VipAnomalyPoint[]).flatMap(point => [
          point.sz2 * 4,
          point.sz3 * 4,
          point.purpleTop,
          point.purpleBottom,
        ]);
      const rawMaxAbs = Math.max(8, ...values.map(value => Math.abs(Number.isFinite(value) ? value : 0)));
      const maxAbs = isFiveDragon ? 4.8 : rawMaxAbs * 1.18 + 8;
      const minAbs = isFiveDragon ? -3.25 : -maxAbs;
      const upper = addLine(points.map(point => ({ time: point.time, value: maxAbs })), '#00000000', 1);
      addLine(points.map(point => ({ time: point.time, value: minAbs })), '#00000000', 1);
      vipAxisPaneRefs.current[paneIndex] = upper;
      if (paneIndex === 0) {
        vipAxisSeriesRef.current = upper;
      }
      window.requestAnimationFrame(() => setVipOverlayRevision(prev => prev + 1));
      upper.createPriceLine({ price: 0, color: (isFiveDragon ? '#7f1d1d' : isShortEnergy ? '#ffffff' : VIP_COLORS.magenta) + 'cc', lineWidth: 1, lineStyle: LineStyle.Solid, axisLabelVisible: false, title: '' });
      return refs;
    }
    return [];
  }, [chartColors, fundFlowPoints, indicatorConfig, tradingSignals, stock?.symbol, stock?.name, floatSharesTick, showAuction, period, sampledAuction, preClose]);

  // 渲染副图：pane0=主选指标；四宫格模式再渲染 pane1/pane2
  const renderSubChart = useCallback((type: SubChartType, chartData: KLineData[]) => {
    const vc = volumeChartRef.current;
    if (!vc || chartData.length === 0) return;
    const s0 = buildSub(type, chartData, 0);
    subPaneRefs.current[0] = s0;
    volumeSeriesRef.current = type === 'volume' ? (s0[0] || null) : null;
    subSeriesRefs.current = type === 'volume' ? [] : s0;

    if (gridModeRef.current && !isTrendLinePeriod) {
      subPaneRefs.current[1] = buildSub(subType2Ref.current, chartData, 1);
      subPaneRefs.current[2] = buildSub(subType3Ref.current, chartData, 2);
    }
  }, [buildSub, isTrendLinePeriod]);

  // ========== 核心：数据更新（切换股票/周期 = 全量，增量推送 = setData） ==========
  useEffect(() => {
    const chart = chartRef.current;
    const volumeChart = volumeChartRef.current;
    if (!chart || !volumeChart) return;

    // 数据为空时清除 series 并返回
    if (safeData.length === 0) {
      clearAllSeries();
      return;
    }

    const needType = isTrendLinePeriod ? 'line' : 'candle';

    // series 类型不匹配时（分时 <-> K线切换），清除旧 series
    if (seriesTypeRef.current !== null && seriesTypeRef.current !== needType) {
      clearAllSeries();
    }

    // ---------- 走势图（分时 / 5日） ----------
    if (isTrendLinePeriod) {
      if (!mainSeriesRef.current) {
        // 首次创建 series
        const lineSeries = chart.addSeries(LineSeries, {
          color: '#38bdf8', lineWidth: 2, priceLineVisible: false, lastValueVisible: true,
        });
        mainSeriesRef.current = lineSeries;
        seriesTypeRef.current = 'line';

        if (preClose > 0) {
          lineSeries.createPriceLine({
            price: preClose, color: chartColors.priceLineColor,
            lineWidth: 1, lineStyle: LineStyle.Dashed, axisLabelVisible: true, title: '昨收',
          });
        }

        // 均价线
        const avgSeries = chart.addSeries(LineSeries, {
          color: '#facc15', lineWidth: 1, priceLineVisible: false, lastValueVisible: false,
        });
        maSeriesRefs.current = [avgSeries];

        // 成交量
        volumeSeriesRef.current = volumeChart.addSeries(HistogramSeries, {
          priceFormat: { type: 'volume' },
          priceLineVisible: false,
          lastValueVisible: false,
        });
      }

      // 更新数据。分时+竞价开启:前拼竞价 whitespace 占位点(只有 time 无 value),让时间轴含 9:15-9:25、
      // 分时线右移空出左侧竞价窗;分时线本身不在竞价段绘制(whitespace),竞价台阶由 overlay canvas 画。
      const datePrefix = String(safeData[0]?.time || '').slice(0, 10);
      const withAuction = showAuction && period === '1m' && sampledAuction.length >= 2 && datePrefix;
      const auctionWhitespace = withAuction
        ? sampledAuction.map(t => ({ time: parseTime(`${datePrefix} ${t.time}`) } as LineData))
        : [];
      const lineData: LineData[] = safeData.map(d => ({ time: parseTime(d.time), value: d.close }));
      mainSeriesRef.current.setData([...auctionWhitespace, ...lineData]);
      if (!signalMarkersRef.current) {
        signalMarkersRef.current = createSeriesMarkers(mainSeriesRef.current, signalMarkerData, { zOrder: 'top' });
      } else {
        signalMarkersRef.current.setMarkers(signalMarkerData);
      }

      if (maSeriesRefs.current.length > 0) {
        const avgData: LineData[] = safeData.filter(d => d.avg).map(d => ({ time: parseTime(d.time), value: d.avg! }));
        maSeriesRefs.current[0].setData(avgData);
      }
    }
    // ---------- K线图 ----------
    else {
      if (!mainSeriesRef.current) {
        const candleSeries = chart.addSeries(CandlestickSeries, {
          upColor: mainChartTemplate === 'openEatFish' ? '#ff0000' : chartColors.upColor,
          downColor: mainChartTemplate === 'openEatFish' ? '#00ff00' : chartColors.downColor,
          wickUpColor: mainChartTemplate === 'openEatFish' ? '#ff0000' : chartColors.upColor,
          wickDownColor: mainChartTemplate === 'openEatFish' ? '#00ff00' : chartColors.downColor,
          borderVisible: false,
        });
        mainSeriesRef.current = candleSeries;
        seriesTypeRef.current = 'candle';
      }

      // 更新 K线 数据
      const openEatFishSeries = mainChartTemplate === 'openEatFish' ? openEatFishMainSeries : null;
      mainSeriesRef.current.applyOptions({
        upColor: mainChartTemplate === 'openEatFish' ? '#ff0000' : chartColors.upColor,
        downColor: mainChartTemplate === 'openEatFish' ? '#00ff00' : chartColors.downColor,
        wickUpColor: mainChartTemplate === 'openEatFish' ? '#ff0000' : chartColors.upColor,
        wickDownColor: mainChartTemplate === 'openEatFish' ? '#00ff00' : chartColors.downColor,
        borderVisible: false,
      });
      const eatFishByTime = new Set((openEatFishSeries?.points || []).filter(point => point.eatFish).map(point => String(point.time)));
      const candleData: CandlestickData[] = safeData.map(d => {
        const time = parseTime(d.time);
        const isEatFish = eatFishByTime.has(String(time));
        return {
          time,
          open: d.open,
          high: d.high,
          low: d.low,
          close: d.close,
          color: mainChartTemplate === 'openEatFish' && isEatFish ? VIP_COLORS.cyan : undefined,
          wickColor: mainChartTemplate === 'openEatFish' && isEatFish ? VIP_COLORS.cyan : undefined,
          borderColor: mainChartTemplate === 'openEatFish' && isEatFish ? VIP_COLORS.cyan : undefined,
        };
      });
      mainSeriesRef.current.setData(candleData);
      if (!signalMarkersRef.current) {
        signalMarkersRef.current = createSeriesMarkers(mainSeriesRef.current, signalMarkerData, { zOrder: 'top' });
      } else {
        signalMarkersRef.current.setMarkers(signalMarkerData);
      }

      // --- MA 均线（配置驱动） ---
      for (const s of maSeriesRefs.current) seriesIndicatorMap.current.delete(s);
      clearSeriesArray(chart, maSeriesRefs);
      clearSeriesArray(chart, openEatFishSeriesRefs);

      // --- TDX 主图公式叠加(线+买卖点标记;引擎求值,不支持的子结构自动降级) ---
      clearSeriesArray(chart, tdxMainSeriesRefs);
      if (typeof mainChartTemplate === 'string' && mainChartTemplate.startsWith('tdx:')) {
        const tdxFormula = getTdxFormula(mainChartTemplate);
        if (tdxFormula) {
          const tdxOut = evaluateTdxFormula(tdxFormula.id, tdxFormula.source, safeData, {
            code: stock?.symbol, name: stock?.name,
            ...(stock?.symbol ? finCtxCache.get(stock.symbol) : undefined),
          });
          const tdxTimes = safeData.map(d => parseTime(d.time));
          const added: ISeriesApi<SeriesType, Time>[] = [];
          for (const ln of tdxOut.lines) {
            // 主图上过滤明显非价格量纲的线(如0-100震荡值),避免把价格轴拉爆
            let vmin = Infinity, vmax = -Infinity;
            for (const v of ln.values) { if (v != null) { if (v < vmin) vmin = v; if (v > vmax) vmax = v; } }
            const pmin = Math.min(...safeData.map(d => d.low)), pmax = Math.max(...safeData.map(d => d.high));
            if (!(vmax >= pmin * 0.3 && vmin <= pmax * 3)) continue;
            const lsr = chart.addSeries(LineSeries, {
              color: ln.color || '#38bdf8',
              lineWidth: Math.min(4, Math.max(1, ln.width || 1)) as 1 | 2 | 3 | 4,
              lineStyle: ln.dotted ? LineStyle.Dotted : LineStyle.Solid,
              priceLineVisible: false,
              lastValueVisible: false,
              title: '',
            });
            lsr.setData(tdxTimes.map((t, i) => (ln.values[i] == null ? { time: t } : { time: t, value: ln.values[i] as number })) as LineData[]);
            added.push(lsr);
          }
          const markerItems: { idx: number; m: { time: Time; position: 'aboveBar' | 'belowBar'; color: string; shape: 'arrowDown' | 'arrowUp' | 'square'; text: string } }[] = [];
          for (const m of tdxOut.markers) {
            markerItems.push({
              idx: m.index,
              m: {
                time: tdxTimes[m.index],
                position: m.above ? 'aboveBar' : 'belowBar',
                color: m.color || '#eab308',
                shape: m.above ? 'arrowDown' : 'arrowUp',
                text: m.text,
              },
            });
          }
          // 稀疏信号柱(STICKLINE)转方块标记;密集重涂K线的(段数>20%)跳过,避免刷屏
          for (const st of tdxOut.sticks) {
            const idxs: number[] = [];
            st.segs.forEach((seg, i) => { if (seg) idxs.push(i); });
            if (idxs.length === 0 || idxs.length > safeData.length * 0.2) continue;
            for (const i of idxs) {
              const seg = st.segs[i]!;
              const above = Math.max(seg[0], seg[1]) >= safeData[i].close;
              markerItems.push({
                idx: i,
                m: { time: tdxTimes[i], position: above ? 'aboveBar' : 'belowBar', color: st.color || '#eab308', shape: 'square', text: '' },
              });
            }
          }
          markerItems.sort((x, y) => x.idx - y.idx);
          const tdxMarkerList = markerItems.map(x => x.m);
          if (tdxMainMarkersRef.current) tdxMainMarkersRef.current.setMarkers(tdxMarkerList);
          else if (tdxMarkerList.length > 0) tdxMainMarkersRef.current = createSeriesMarkers(mainSeriesRef.current, tdxMarkerList);
          tdxMainSeriesRefs.current = added;
          const hint = added.length === 0 && tdxMarkerList.length === 0
            ? `该公式在当前K线区间无可显示输出${tdxOut.notes.length ? `:${tdxOut.notes[0]}` : '(可能只在特定信号日打标)'}`
            : '';
          setTdxMainHint(prev => (prev === hint ? prev : hint));
        }
      } else {
        if (tdxMainMarkersRef.current) tdxMainMarkersRef.current.setMarkers([]);
        setTdxMainHint(prev => (prev === '' ? prev : ''));
      }
      if (mainChartTemplate === 'openEatFish' && openEatFishSeries) {
        const addTemplateLine = (points: LineData[], color: string, lineWidth: 1 | 2 = 1) => {
          const series = chart.addSeries(LineSeries, {
            color,
            lineWidth,
            priceLineVisible: false,
            lastValueVisible: false,
            title: '',
          });
          series.setData(points);
          openEatFishSeriesRefs.current.push(series);
          return series;
        };
        addTemplateLine(openEatFishSeries.ma5, VIP_COLORS.yellow, 1);
        addTemplateLine(openEatFishSeries.ma10, '#669933', 1);
        addTemplateLine(openEatFishSeries.ma20, '#008000', 2);
        addTemplateLine(openEatFishSeries.ma30, '#ff937f', 2);
        addTemplateLine(openEatFishSeries.ma60, '#ffffff', 2);
        addTemplateLine(openEatFishSeries.lowerLimit, '#ffffff', 2);
        addTemplateLine(openEatFishSeries.upperBound, '#00000000', 1);
        addTemplateLine(openEatFishSeries.lowerBound, '#00000000', 1);
      } else if (indicatorConfig.ma.enabled) {
        indicatorConfig.ma.periods.forEach((p, idx) => {
          const maSeries = chart.addSeries(LineSeries, {
            color: MA_COLORS[idx % MA_COLORS.length], lineWidth: 1,
            priceLineVisible: false, lastValueVisible: false, title: '',
          });
          maSeries.setData(safeData.length >= p ? calculateSMA(safeData, p) : []);
          maSeriesRefs.current.push(maSeries);
          seriesIndicatorMap.current.set(maSeries, 'ma');
        });
      }

      // --- EMA 均线 ---
      for (const s of emaSeriesRefs.current) seriesIndicatorMap.current.delete(s);
      clearSeriesArray(chart, emaSeriesRefs);
      if (mainChartTemplate === 'standard' && indicatorConfig.ema.enabled) {
        indicatorConfig.ema.periods.forEach((p, idx) => {
          const emaSeries = chart.addSeries(LineSeries, {
            color: EMA_COLORS[idx % EMA_COLORS.length], lineWidth: 1,
            priceLineVisible: false, lastValueVisible: false, title: '',
          });
          emaSeries.setData(calculateEMA(safeData, p));
          emaSeriesRefs.current.push(emaSeries);
          seriesIndicatorMap.current.set(emaSeries, 'ema');
        });
      }

      // --- BOLL 布林带 ---
      for (const s of bollSeriesRefs.current) seriesIndicatorMap.current.delete(s);
      clearSeriesArray(chart, bollSeriesRefs);
      if (mainChartTemplate === 'standard' && indicatorConfig.boll.enabled) {
        const { mid, upper, lower } = calculateBOLL(safeData, indicatorConfig.boll.period, indicatorConfig.boll.multiplier);
        const midSeries = chart.addSeries(LineSeries, {
          color: BOLL_COLOR, lineWidth: 1, priceLineVisible: false, lastValueVisible: false, title: '',
        });
        midSeries.setData(mid);
        const upperSeries = chart.addSeries(LineSeries, {
          color: BOLL_COLOR, lineWidth: 1, lineStyle: LineStyle.Dashed,
          priceLineVisible: false, lastValueVisible: false, title: '',
        });
        upperSeries.setData(upper);
        const lowerSeries = chart.addSeries(LineSeries, {
          color: BOLL_COLOR, lineWidth: 1, lineStyle: LineStyle.Dashed,
          priceLineVisible: false, lastValueVisible: false, title: '',
        });
        lowerSeries.setData(lower);
        bollSeriesRefs.current = [midSeries, upperSeries, lowerSeries];
        for (const s of bollSeriesRefs.current) seriesIndicatorMap.current.set(s, 'boll');
      }
    }

    // ========== 副图渲染 ==========
    clearSubChart();
    const subChartTypeToRender: SubChartType = isTrendLinePeriod ? 'volume' : subChartTypeRef.current;
    renderSubChart(subChartTypeToRender, safeData);

    // full: 用户主动切换股票/周期 → fitContent；refresh: 定时刷新 → 保留缩放；增量仅首次 fit
    const shouldFit = safeData.length > 0 && (
      updateMode === 'full' || (!hasFittedRef.current && safeData.length > 1)
    );
    if (shouldFit) {
      const rangeBars = Math.floor(Number(visibleRangeBars || 0));
      if (rangeBars > 0 && safeData.length > 0 && !isTrendLinePeriod) {
        const bars = Math.min(rangeBars, safeData.length);
        const range = {
          from: Math.max(0, safeData.length - bars) as number,
          to: Math.max(0, safeData.length - 1) as number,
        };
        chart.timeScale().setVisibleLogicalRange(range);
        volumeChart.timeScale().setVisibleLogicalRange(range);
      } else {
        chart.timeScale().fitContent();
        volumeChart.timeScale().fitContent();
      }
      hasFittedRef.current = true;
    }
  }, [safeData, updateMode, preClose, isTrendLinePeriod, chartColors, clearAllSeries, clearSubChart, renderSubChart, indicatorConfig, signalMarkerData, mainChartTemplate, openEatFishMainSeries, visibleRangeBars, stock?.symbol, stock?.name, floatSharesTick, period, sampledAuction, showAuction]);

  // 副图指标独立于主图叠加开关，用户可自由切换 VOL/MACD/KDJ/RSI/CCI/WR（不再随设置回退）

  // ========== 副图切换 ==========
  const rerenderSubs = useCallback(() => {
    clearSubChart();
    renderSubChart(subChartTypeRef.current, safeData);
    volumeChartRef.current?.timeScale().fitContent();
  }, [clearSubChart, renderSubChart, safeData]);

  const handleSubChartSwitch = useCallback((type: SubChartType) => {
    setSubChartType(type);
    subChartTypeRef.current = type;
    rerenderSubs();
  }, [rerenderSubs]);

  const handleSub2Switch = useCallback((type: SubChartType) => {
    setSubType2(type);
    subType2Ref.current = type;
    rerenderSubs();
  }, [rerenderSubs]);
  const handleSub3Switch = useCallback((type: SubChartType) => {
    setSubType3(type);
    subType3Ref.current = type;
    rerenderSubs();
  }, [rerenderSubs]);

  const setWindowMode = useCallback((nextGridMode: boolean) => {
    if (gridModeRef.current === nextGridMode) return;
    gridModeRef.current = nextGridMode;
    gridModeRef2.current = nextGridMode;
    setGridMode(nextGridMode);
    requestAnimationFrame(() => rerenderSubs());
  }, [rerenderSubs]);

  // 应用四窗口组合:主图模板 + 三副图 + 四图模式,趋势周期自动切日K
  const applyCombo = useCallback((combo: ChartCombo) => {
    setSubChartType(combo.subs[0]);
    subChartTypeRef.current = combo.subs[0];
    setSubType2(combo.subs[1]);
    subType2Ref.current = combo.subs[1];
    setSubType3(combo.subs[2]);
    subType3Ref.current = combo.subs[2];
    setWindowMode(true);
    setMainChartTemplate(combo.main);
    if (isTrendLinePeriod) onPeriodChange('1d');
    rerenderSubs();
  }, [setWindowMode, setMainChartTemplate, isTrendLinePeriod, onPeriodChange, rerenderSubs]);

  // 当前四窗口状态若恰好等于某个组合,下拉回显该组合;手动改任一窗口即回落到占位项
  const activeComboId = gridMode
    ? (CHART_COMBOS.find(c =>
        c.main === mainChartTemplate
        && c.subs[0] === subChartType
        && c.subs[1] === subType2
        && c.subs[2] === subType3,
      )?.id ?? '')
    : '';

  const clearRange = useCallback(() => {
    setRangeStart(null);
    setRangeEnd(null);
    setRangeStats(null);
    setRangeCoords(null);
  }, []);

  // 换股 / 切换周期时自动关闭区间统计
  useEffect(() => {
    clearRange();
  }, [stock?.symbol, period, clearRange]);

  // 拉股本/估值(一次一只,缓存),供筹码类 TDX 公式(WINNER/COST)与 CAPITAL/FINANCE
  useEffect(() => {
    const sym = stock?.symbol;
    if (!sym || finCtxCache.has(sym)) return;
    let cancelled = false;
    getF10Valuation(sym).then(v => {
      if (cancelled) return;
      finCtxCache.set(sym, {
        floatShares: v?.floatShares || 0,
        totalShares: v?.totalShares || 0,
        pe: v?.peTtm || 0,
        pb: v?.pb || 0,
        floatMcap: v?.floatMarketCap || 0,
        totalMcap: v?.totalMarketCap || 0,
      });
      setFloatSharesTick(t => t + 1);
    }).catch(() => { /* 拿不到就保持无筹码/财务估算 */ });
    return () => { cancelled = true; };
  }, [stock?.symbol]);

  // 选中区间的像素坐标（随缩放/滚动更新），用于绘制高亮框
  useEffect(() => {
    if (draggingRef.current) return; // 拖拽中用实时像素，不被时间换算覆盖
    const chart = chartRef.current;
    if (!chart || rangeStart == null) { setRangeCoords(null); return; }
    const update = () => {
      if (draggingRef.current) return;
      const ts = chart.timeScale();
      const x1 = ts.timeToCoordinate(rangeStart);
      const x2 = rangeEnd != null ? ts.timeToCoordinate(rangeEnd) : x1;
      const { y1, y2 } = rangeYRef.current;
      if (x1 != null && x2 != null) setRangeCoords({ x1: Math.min(x1, x2), x2: Math.max(x1, x2), y1, y2 });
      else setRangeCoords(null);
    };
    update();
    const ts = chart.timeScale();
    ts.subscribeVisibleLogicalRangeChange(update);
    return () => ts.unsubscribeVisibleLogicalRangeChange(update);
  }, [rangeStart, rangeEnd, chartViewport]);

  // 区间统计：左键在K线上直接拖一个框 → 框选区间（无需点任何按钮；平移用滚轮）
  useEffect(() => {
    const el = chartContainerRef.current;
    if (!el) return;
    let startX = 0;
    let startY = 0;
    let armed = false; // 当前这次按下是否进入框选
    const rect = () => el.getBoundingClientRect();
    const getX = (clientX: number) => clientX - rect().left;
    const getY = (clientY: number) => clientY - rect().top;

    const down = (e: MouseEvent) => {
      if (e.button !== 0 || isTrendLinePeriodRef.current) return;
      armed = true;
      draggingRef.current = false;
      startX = getX(e.clientX);
      startY = getY(e.clientY);
    };
    const move = (e: MouseEvent) => {
      if (!armed) return;
      const x = getX(e.clientX);
      const y = getY(e.clientY);
      // 移动超过阈值才算框选（避免普通点击触发）
      if (!draggingRef.current && Math.abs(x - startX) <= 4 && Math.abs(y - startY) <= 4) return;
      if (!draggingRef.current) {
        draggingRef.current = true;
        setRangeStart(null);
        setRangeEnd(null);
        setRangeStats(null);
      }
      rangeYRef.current = { y1: Math.min(startY, y), y2: Math.max(startY, y) };
      setRangeCoords({ x1: Math.min(startX, x), x2: Math.max(startX, x), y1: Math.min(startY, y), y2: Math.max(startY, y) });
    };
    const up = (e: MouseEvent) => {
      armed = false;
      if (!draggingRef.current) return;
      draggingRef.current = false;
      const chart = chartRef.current;
      const x = getX(e.clientX);
      if (chart) {
        const ts = chart.timeScale();
        const ta = ts.coordinateToTime(Math.min(startX, x));
        const tb = ts.coordinateToTime(Math.max(startX, x));
        if (ta != null && tb != null) {
          setRangeStart(ta);
          setRangeEnd(tb);
          setRangeStats(computeRangeStats(safeDataRef.current, ta, tb));
        } else {
          setRangeCoords(null);
        }
      }
    };

    // 用捕获阶段，确保在图表 canvas 处理之前拿到 mousedown
    el.addEventListener('mousedown', down, true);
    window.addEventListener('mousemove', move, true);
    window.addEventListener('mouseup', up, true);
    return () => {
      el.removeEventListener('mousedown', down, true);
      window.removeEventListener('mousemove', move, true);
      window.removeEventListener('mouseup', up, true);
      draggingRef.current = false;
    };
  }, []);

  // ========== 点击指标线弹出配置面板 ==========
  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    const getSeriesPrice = (raw: unknown): number | null => {
      if (!raw || typeof raw !== 'object') return null;
      const record = raw as Record<string, unknown>;
      if (typeof record.value === 'number') return record.value;
      if (typeof record.close === 'number') return record.close;
      return null;
    };

    const clickHandler = (param: MouseEventParams<Time>) => {
      if (draggingRef.current) return; // 正在框选时不触发指标弹窗
      if (!param.seriesData || !param.point) {
        setIndicatorPopup(null);
        return;
      }
      const clickY = param.point.y;
      const HIT_THRESHOLD = 12; // px
      let bestDist = Infinity;
      let bestType: MainIndicatorType | null = null;

      for (const [series, data] of param.seriesData) {
        const indType = seriesIndicatorMap.current.get(series);
        if (!indType) continue;
        const price = getSeriesPrice(data);
        if (price == null) continue;
        const coord = series.priceToCoordinate(price);
        if (coord == null) continue;
        const dist = Math.abs(coord - clickY);
        if (dist < bestDist) {
          bestDist = dist;
          bestType = indType;
        }
      }

      if (bestType && bestDist <= HIT_THRESHOLD) {
        const width = chartContainerRef.current?.clientWidth || 1;
        const height = chartContainerRef.current?.clientHeight || 1;
        setIndicatorPopup({
          type: bestType,
          xRatio: param.point.x / width,
          yRatio: param.point.y / height,
        });
      } else {
        setIndicatorPopup(null);
      }
    };

    chart.subscribeClick(clickHandler);
    return () => chart.unsubscribeClick(clickHandler);
  }, []);

	  // ========== 十字光标 ==========
	  useEffect(() => {
	    const chart = chartRef.current;
	    if (!chart) return;
	
	    const handler = (param: MouseEventParams<Time>) => {
	      if (!param.point || isTrendLinePeriodRef.current || draggingRef.current) {
	        setHoverData(null);
	        setHoverPoint(null);
	        return;
	      }
	      const found = findKLineByCandleHit(chart, mainSeriesRef.current, safeDataRef.current, param.point);
	      setHoverData(found);
	      setHoverPoint(found ? { x: param.point.x, y: param.point.y } : null);
		    };
	
	    chart.subscribeCrosshairMove(handler);
	    return () => chart.unsubscribeCrosshairMove(handler);
	  }, []);

	  useEffect(() => {
	    const chart = chartRef.current;
	    const el = chartContainerRef.current;
	    if (!chart || !el || isTrendLinePeriod) return;

		    const handleMove = (event: MouseEvent) => {
		      if (draggingRef.current) {
		        setHoverData(null);
		        setHoverPoint(null);
		        return;
		      }
		      const rect = el.getBoundingClientRect();
		      const x = event.clientX - rect.left;
		      const y = event.clientY - rect.top;
	      if (x < 0 || y < 0 || x > rect.width || y > rect.height) {
	        setHoverData(null);
	        setHoverPoint(null);
	        return;
	      }

	      const found = findKLineByCandleHit(chart, mainSeriesRef.current, safeDataRef.current, { x, y });
	      setHoverData(found);
	      setHoverPoint(found ? { x, y } : null);
		    };

	    const handleLeave = () => {
	      setHoverData(null);
	      setHoverPoint(null);
	    };

	    el.addEventListener('mousemove', handleMove);
	    el.addEventListener('mouseleave', handleLeave);
	    return () => {
	      el.removeEventListener('mousemove', handleMove);
	      el.removeEventListener('mouseleave', handleLeave);
	    };
	  }, [isTrendLinePeriod]);

  // ========== 统计数据 memo ==========
  const todayHigh = useMemo(() => safeData.length > 0 ? Math.max(...safeData.map(d => d.high)) : 0, [safeData]);
  const todayLow = useMemo(() => safeData.length > 0 ? Math.min(...safeData.map(d => d.low)) : 0, [safeData]);
  const totalVolume = useMemo(() => safeData.reduce((sum, d) => sum + d.volume, 0), [safeData]);
  const currentPrice = stock?.price || lastData?.close || 0;
  const currentAvg = lastData?.avg || 0;
  const hoverTooltip = useMemo(() => {
    if (isTrendLinePeriod || !hoverData) return null;

    const index = safeData.findIndex(item => item === hoverData || item.time === hoverData.time);
    const baseClose = index > 0 ? safeData[index - 1].close : hoverData.open;
    const change = hoverData.close - baseClose;
    const changePct = baseClose > 0 ? (change / baseClose) * 100 : 0;
    const amplitude = baseClose > 0 ? ((hoverData.high - hoverData.low) / baseClose) * 100 : 0;
    const amount = hoverData.amount ?? hoverData.close * (hoverData.volume || 0) * 100;
    const turnover = getOptionalKLineNumber(hoverData, ['turnoverRate', 'turnover', 'turnover_rate']);
    const width = 118;
    const height = 214;
    const chart = chartRef.current;
    const series = mainSeriesRef.current;
    const fallbackX = chart?.timeScale().timeToCoordinate(parseTime(hoverData.time)) ?? 16;
    const fallbackY = series?.priceToCoordinate(hoverData.close) ?? 16;
    const anchorX = hoverPoint?.x ?? fallbackX;
    const anchorY = hoverPoint?.y ?? fallbackY;
    const left = clampNumber(anchorX + 12, 4, Math.max(4, chartViewport.width - width - 4));
    const top = clampNumber(anchorY + 12, 4, Math.max(4, chartViewport.height - height - 4));
    const chartRect = chartContainerRef.current?.getBoundingClientRect();
    const viewportWidth = typeof window !== 'undefined' ? window.innerWidth : chartViewport.width;
    const viewportHeight = typeof window !== 'undefined' ? window.innerHeight : chartViewport.height;
    const fixedLeft = clampNumber((chartRect?.left ?? 0) + left, 4, Math.max(4, viewportWidth - width - 4));
    const fixedTop = clampNumber((chartRect?.top ?? 0) + top, 4, Math.max(4, viewportHeight - height - 4));

    return {
      left,
      top,
      fixedLeft,
      fixedTop,
      date: formatHoverTradeDate(hoverData.time),
      rows: [
        { label: '开盘', value: hoverData.open.toFixed(2) },
        { label: '最高', value: hoverData.high.toFixed(2) },
        { label: '最低', value: hoverData.low.toFixed(2) },
        { label: '收盘', value: hoverData.close.toFixed(2) },
        { label: '总量', value: formatHoverVolume(hoverData.volume) },
        { label: '换手', value: turnover != null ? `${turnover.toFixed(2)}%` : '--' },
        { label: '总额', value: formatHoverAmount(amount) },
        { label: '振幅', value: `${amplitude.toFixed(2)}%` },
        { label: '涨跌', value: formatSignedFixed(change) },
        { label: '涨幅', value: `${formatSignedFixed(changePct)}%` },
      ],
      isUp: change >= 0,
    };
  }, [chartViewport.height, chartViewport.width, hoverData, hoverPoint, isTrendLinePeriod, safeData]);

	  // ========== 渲染（图表容器始终保留在 DOM 中，避免销毁重建） ==========
  const indicatorPopupLeft = indicatorPopup
    ? Math.min(
      Math.max(indicatorPopup.xRatio * chartViewport.width, 0),
      Math.max(chartViewport.width - 200, 0),
    )
    : 0;
  const indicatorPopupTop = indicatorPopup
    ? Math.min(
      Math.max(indicatorPopup.yRatio * chartViewport.height, 0),
      Math.max(chartViewport.height - 120, 0),
    )
    : 0;
  const getSignalClasses = (signal: TradingSignal) => {
    if (signal.level === 'risk') {
      return colors.isDark
        ? 'border-emerald-500/30 bg-emerald-950/80 text-emerald-100'
        : 'border-emerald-500/30 bg-emerald-50/95 text-emerald-800';
    }
    if (signal.level === 'S' || signal.level === 'S-' || signal.level === 'A+') {
      return colors.isDark
        ? 'border-orange-500/35 bg-orange-950/80 text-orange-100'
        : 'border-orange-500/30 bg-orange-50/95 text-orange-800';
    }
    return colors.isDark
      ? 'border-sky-500/30 bg-sky-950/80 text-sky-100'
      : 'border-sky-500/30 bg-sky-50/95 text-sky-800';
  };
  const getSignalIcon = (signal: TradingSignal) => {
    if (signal.level === 'risk') return <AlertTriangle size={14} />;
    if (signal.level === 'S' || signal.level === 'S-' || signal.level === 'A+') return <Zap size={14} />;
    return <Target size={14} />;
  };
  const renderBottomLegend = (items: IndicatorLegendItem[], extraClass = '', style?: React.CSSProperties) => {
    if (items.length === 0) return null;
    return (
      <div
        className={`absolute left-1.5 right-12 z-20 flex flex-wrap items-end gap-1 pointer-events-none ${extraClass}`}
        style={style}
      >
        {items.map((item, index) => {
          const labelTextColor = readableTextColor(item.color);
          const labelIsLight = labelTextColor !== '#ffffff';
          return (
            <div
              key={`${item.label}-${item.color}-${index}`}
              className={`inline-flex h-5 items-center overflow-hidden rounded border text-[10px] font-mono tabular-nums shadow-sm ${
                colors.isDark ? 'border-slate-950/50 bg-slate-950/64' : 'border-white/70 bg-white/72'
              }`}
            >
              <span
                className="flex h-full items-center px-1.5 font-bold"
                style={{
                  backgroundColor: item.color,
                  color: labelTextColor,
                  boxShadow: labelIsLight ? 'inset 0 0 0 1px rgba(15,23,42,0.12)' : undefined,
                  textShadow: labelIsLight ? '0 1px 0 rgba(255,255,255,0.22)' : '0 1px 1px rgba(0,0,0,0.45)',
                }}
              >
                {item.label}
              </span>
              <span
                className={`flex h-full min-w-[46px] items-center justify-end px-1.5 font-bold ${
                  colors.isDark ? 'text-slate-100' : 'text-slate-800'
                }`}
              >
                {item.value}
              </span>
            </div>
          );
        })}
      </div>
    );
  };
  const renderSubSelect = (
    value: SubChartType,
    onChange: (type: SubChartType) => void,
    style?: React.CSSProperties,
    extraClass = '',
  ) => (
    <select
      value={value}
      onChange={(e) => {
        const next = e.target.value as SubSelectOption;
        if (next === 'openEatFishMain') {
          setMainChartTemplate('openEatFish');
          if (isTrendLinePeriod) onPeriodChange('1d');
          return;
        }
        onChange(next);
      }}
      style={{
        ...style,
        backgroundColor: colors.isDark ? 'rgba(30, 41, 59, 0.94)' : 'rgba(255, 255, 255, 0.94)',
        borderColor: colors.isDark ? '#475569' : '#cbd5e1',
        color: colors.isDark ? '#f8fafc' : '#334155',
        colorScheme: colors.isDark ? 'dark' : 'light',
      }}
      className={`absolute left-1 z-30 mt-0.5 max-w-[150px] rounded border px-1.5 py-0.5 text-[10px] font-semibold outline-none shadow ${
        colors.isDark
          ? 'border-slate-600 bg-slate-800/92 text-slate-100'
          : 'border-slate-300 bg-white/92 text-slate-700'
      } ${extraClass}`}
    >
      {SUB_OPTIONS.map(o => <option key={o.id} value={o.id}>{o.label}</option>)}
      {TDX_SUB_GROUPS.map(g => (
        <optgroup key={g.category} label={`通达信·${g.category}`}>
          {g.items.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}
        </optgroup>
      ))}
      {value.startsWith('tdx:') && !TDX_SUB_OPTION_IDS.has(value) && (
        <option value={value}>主图·{getTdxFormula(value)?.name ?? '通达信'}</option>
      )}
    </select>
  );

  const hoverTooltipPortal = hoverTooltip && typeof document !== 'undefined'
    ? createPortal(
      <div
        className="pointer-events-none fixed w-[118px] overflow-hidden rounded-md border font-mono text-[11px] leading-[18px] text-slate-200 shadow-[0_14px_30px_rgba(2,6,23,0.42)] backdrop-blur-sm tabular-nums"
        style={{
          left: hoverTooltip.fixedLeft,
          top: hoverTooltip.fixedTop,
          zIndex: 9999,
          backgroundColor: 'rgba(11, 20, 36, 0.92)',
          borderColor: 'rgba(100, 116, 139, 0.7)',
        }}
      >
        <div
          className="flex h-7 items-center border-b px-1.5 text-[12px] font-bold text-slate-100"
          style={{ borderColor: 'rgba(100, 116, 139, 0.45)' }}
        >
          <span>{hoverTooltip.date}</span>
        </div>
        <div className="space-y-0 px-1.5 py-1.5">
          {hoverTooltip.rows.map((row) => {
            const isChangeRow = row.label === '涨跌' || row.label === '涨幅';
            return (
              <div key={row.label} className="flex items-center whitespace-nowrap">
                <span className="w-8 shrink-0 text-slate-500">{row.label}</span>
                <span className={`ml-1 w-[68px] shrink-0 text-right font-semibold ${isChangeRow ? cc.getColorClass(hoverTooltip.isUp) : 'text-slate-200'}`}>
                  {row.value}
                </span>
              </div>
            );
          })}
        </div>
      </div>,
      document.body,
    )
    : null;

  return (
    <>
    <div className="h-full w-full fin-panel flex flex-col relative" onMouseDown={() => setIndicatorPopup(null)}>
      {/* 加载提示（叠加在图表上方） */}
      {!hasData && (
        <div className="absolute inset-0 z-20 flex items-center justify-center fin-panel">
          <span className={`text-sm animate-pulse ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
            加载市场数据中...
          </span>
        </div>
      )}

      {/* Header */}
      <div className="relative z-30 flex items-center px-2 py-1 border-b fin-divider fin-panel-strong">
        <div className="flex gap-1 shrink-0">
          {periods.map((p) => (
            <button
              key={p.id}
              onClick={() => onPeriodChange(p.id)}
              className={`text-xs px-3 py-1 rounded transition-colors ${
                period === p.id
                  ? (colors.isDark ? 'bg-slate-800/80' : 'bg-slate-200/80') + ' text-accent-2 font-bold'
                  : (colors.isDark ? 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40' : 'text-slate-500 hover:text-slate-700 hover:bg-slate-200/40')
              }`}
            >
              {p.label}
            </button>
          ))}
          {showTemplateSelect && (
            <select
              value={mainChartTemplate}
              onChange={(e) => {
                const next = e.target.value as MainChartTemplate;
                setMainChartTemplate(next);
                if (next !== 'standard' && isTrendLinePeriod) onPeriodChange('1d');
              }}
              className={`ml-2 h-[24px] rounded border px-2 text-xs font-semibold outline-none transition-colors ${
                colors.isDark
                  ? 'border-slate-700 bg-slate-900/70 text-slate-200 hover:border-accent/60'
                  : 'border-slate-300 bg-white/80 text-slate-700 hover:border-accent/60'
              }`}
              style={{ colorScheme: colors.isDark ? 'dark' : 'light' }}
              title="主图模板;非标准模板会自动切到日K"
            >
              <option value="standard">主图：标准</option>
              <option value="openEatFish">主图：开仓吃鱼</option>
              {TDX_MAIN_GROUPS.map(g => (
                <optgroup key={g.category} label={`通达信·${g.category}`}>
                  {g.items.map(f => (
                    <option key={f.id} value={f.id}>{f.name}</option>
                  ))}
                </optgroup>
              ))}
            </select>
          )}
          {showTemplateSelect && (
            <select
              value={activeComboId}
              onChange={(e) => {
                const combo = CHART_COMBOS.find(c => c.id === e.target.value);
                if (combo) applyCombo(combo);
              }}
              className={`ml-1.5 h-[24px] rounded border px-2 text-xs font-semibold outline-none transition-colors ${
                colors.isDark
                  ? 'border-amber-500/50 bg-slate-900/70 text-amber-200 hover:border-amber-400'
                  : 'border-amber-500/60 bg-white/80 text-amber-700 hover:border-amber-500'
              }`}
              style={{ colorScheme: colors.isDark ? 'dark' : 'light' }}
              title="四窗口自动组合:一键设置主图模板+三个副图"
            >
              <option value="">四窗口组合</option>
              {CHART_COMBOS.map(c => (
                <option key={c.id} value={c.id}>{c.label}</option>
              ))}
            </select>
          )}
          <div className={`flex items-center gap-2 ml-3 pl-3 border-l ${colors.isDark ? 'border-slate-700' : 'border-slate-300'}`}>
            {!isTrendLinePeriod && (
              <>
              <div className={`flex items-center gap-1 text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
                <ZoomIn size={12} />
                <ZoomOut size={12} />
                <span>滚轮</span>
              </div>
              <div className={`flex items-center gap-1 text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
                <MoveHorizontal size={12} />
                <span>拖拽</span>
              </div>
              <div className={`flex items-center gap-1 text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
                <Target size={12} />
                <span>拖框=区间统计</span>
              </div>
              </>
            )}
          </div>
        </div>

        {/* 数据信息栏：固定在右侧可用区域，避免切换周期时撑宽/抖动 */}
        <div className={`ml-3 min-w-0 flex-1 text-xs font-mono tabular-nums ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
          <div className="flex items-center justify-end gap-2 overflow-x-auto whitespace-nowrap [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden">
          {isTrendLinePeriod ? (
            <>
              <span className="shrink-0 w-44 text-right">时间: <span className={colors.isDark ? 'text-slate-300' : 'text-slate-600'}>{displayData ? formatTimeDisplay(displayData.time) : '--'}</span></span>
              <span className="shrink-0 w-24 text-right">价格: <span className={getPriceColor(displayData?.close || 0)}>{displayData?.close?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-24 text-right">均价: <span className="text-yellow-500">{displayData?.avg?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-24 text-right">涨跌: <span className={getPriceColor(currentPrice)}>{formatChange(displayData?.close || preClose)}</span></span>
              <span className="shrink-0 w-24 text-right">幅度: <span className={getPriceColor(currentPrice)}>{formatChangePercent(displayData?.close || preClose)}</span></span>
            </>
          ) : (
            <>
              <span className="shrink-0 w-44 text-right">时间: <span className={colors.isDark ? 'text-slate-300' : 'text-slate-600'}>{displayData ? formatTimeDisplay(displayData.time) : '--'}</span></span>
              <span className="shrink-0 w-20 text-right">收: <span className="text-accent-2">{displayData?.close?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-20 text-right">开: {displayData?.open?.toFixed(2) || '--'}</span>
              <span className="shrink-0 w-20 text-right">高: <span className={cc.upClass}>{displayData?.high?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-20 text-right">低: <span className={cc.downClass}>{displayData?.low?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-24 text-right">MA5: <span className="text-yellow-500">{displayData?.ma5?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-24 text-right">MA10: <span className="text-purple-500">{displayData?.ma10?.toFixed(2) || '--'}</span></span>
              <span className="shrink-0 w-24 text-right">MA20: <span className="text-orange-500">{displayData?.ma20?.toFixed(2) || '--'}</span></span>
            </>
          )}
          </div>
        </div>
      </div>

      {/* 走势图信息栏 */}
      {isTrendLinePeriod && (
        <div className={`flex items-center justify-between px-3 py-1.5 border-b fin-divider text-xs ${colors.isDark ? 'bg-slate-900/30' : 'bg-slate-100/50'}`}>
          <div className="flex gap-4">
            <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>最高: <span className={cc.upClass}>{todayHigh.toFixed(2)}</span></span>
            <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>最低: <span className={cc.downClass}>{todayLow.toFixed(2)}</span></span>
            <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>昨收: <span className={colors.isDark ? 'text-slate-300' : 'text-slate-600'}>{preClose.toFixed(2)}</span></span>
          </div>
          <div className="flex gap-4">
            <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>均价: <span className="text-yellow-500">{currentAvg.toFixed(2)}</span></span>
            <span className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>总量: <span className={colors.isDark ? 'text-slate-300' : 'text-slate-600'}>{(totalVolume / 100).toFixed(0)}手</span></span>
          </div>
        </div>
      )}

      {/* 主图表区域 */}
      <div className="flex-1 min-h-0 relative">
        <div className="absolute inset-0" ref={chartContainerRef} />
        <canvas
          ref={mainOverlayCanvasRef}
          className={`absolute inset-0 z-10 pointer-events-none ${mainChartTemplate === 'openEatFish' && !isTrendLinePeriod ? 'block' : 'hidden'}`}
        />
        {/* 集合竞价独立窗:分时(1m)+竞价开关时,在左侧竞价时段自绘台阶价+背景框(通达信样式) */}
        <canvas
          ref={auctionOverlayCanvasRef}
          className={`absolute inset-0 z-10 pointer-events-none ${showAuction && period === '1m' ? 'block' : 'hidden'}`}
        />
        {!isTrendLinePeriod && mainChartTemplate === 'standard' && renderBottomLegend(mainIndicatorLegend, 'bottom-1.5')}
        {!isTrendLinePeriod && typeof mainChartTemplate === 'string' && mainChartTemplate.startsWith('tdx:') && tdxMainHint && (
          <div className="absolute left-1/2 top-10 z-20 -translate-x-1/2 rounded border border-amber-500/40 bg-slate-900/85 px-3 py-1.5 text-[11px] text-amber-200 pointer-events-none">
            {tdxMainHint}
          </div>
        )}
        {!isTrendLinePeriod && mainChartTemplate === 'openEatFish' && openEatFishMainSeries && (
          <div className="absolute left-1 top-1 z-20 flex max-w-[calc(100%-84px)] flex-wrap items-center gap-x-1.5 gap-y-0 text-[10px] font-mono font-bold leading-4 pointer-events-none">
            <span className="text-slate-300">{stock?.name || ''}({period === '1d' ? '日线' : period === '1w' ? '周线' : '月线'})</span>
            <span className="text-slate-400">兵哥开仓吃鱼</span>
            <span style={{ color: VIP_COLORS.yellow }}>MA5: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.ma5, displayTime))}</span>
            <span style={{ color: '#669933' }}>MA10: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.ma10, displayTime))}</span>
            <span style={{ color: '#00aa00' }}>MA20: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.ma20, displayTime))}</span>
            <span style={{ color: '#ff937f' }}>MA30: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.ma30, displayTime))}</span>
            <span className="text-white">MA60: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.ma60, displayTime))}</span>
            <span className="text-white">下极限: {formatIndicatorValue(valueAtTime(openEatFishMainSeries.lowerLimit, displayTime))}</span>
          </div>
        )}
        {!isTrendLinePeriod && hasData && (
          <div
            className={`absolute bottom-2 right-2 z-30 flex overflow-hidden rounded border text-[10px] font-semibold shadow ${
              colors.isDark
                ? 'border-slate-600 bg-slate-900/82 text-slate-300'
                : 'border-slate-300 bg-white/88 text-slate-600'
            }`}
            title="切换副图窗口数量"
          >
            <button
              type="button"
              onClick={() => setWindowMode(false)}
              className={`px-2 py-1 transition-colors ${
                !gridMode
                  ? (colors.isDark ? 'bg-orange-950/80 text-orange-100' : 'bg-orange-50 text-orange-700')
                  : (colors.isDark ? 'hover:bg-slate-800 hover:text-white' : 'hover:bg-slate-100 hover:text-slate-900')
              }`}
            >
              单窗口
            </button>
            <button
              type="button"
              onClick={() => setWindowMode(true)}
              className={`border-l px-2 py-1 transition-colors ${
                colors.isDark ? 'border-slate-600' : 'border-slate-300'
              } ${
                gridMode
                  ? (colors.isDark ? 'bg-orange-950/80 text-orange-100' : 'bg-orange-50 text-orange-700')
                  : (colors.isDark ? 'hover:bg-slate-800 hover:text-white' : 'hover:bg-slate-100 hover:text-slate-900')
              }`}
            >
              四窗口
            </button>
          </div>
        )}

        {/* 区间统计：拖出的矩形选框 */}
        {!isTrendLinePeriod && rangeCoords && (
          <div
            className="absolute z-10 pointer-events-none border border-amber-400/80 bg-amber-400/12"
            style={{
              left: rangeCoords.x1,
              top: rangeCoords.y1,
              width: Math.max(rangeCoords.x2 - rangeCoords.x1, 1),
              height: Math.max(rangeCoords.y2 - rangeCoords.y1, 1),
            }}
          />
        )}
        {/* 区间统计结果面板 */}
        {!isTrendLinePeriod && rangeStats && (
          <div className={`absolute top-1 right-1 z-30 w-[228px] rounded-lg border shadow-xl text-[11px] ${colors.isDark ? 'bg-slate-900/95 border-slate-700 text-slate-300' : 'bg-white/95 border-slate-200 text-slate-600'}`}>
            <div className={`flex items-center justify-between px-2.5 py-1.5 border-b ${colors.isDark ? 'border-slate-700' : 'border-slate-200'}`}>
              <span className={`font-semibold ${colors.isDark ? 'text-slate-200' : 'text-slate-700'}`}>区间统计</span>
              <button onClick={clearRange} className="text-slate-400 hover:text-red-400" title="关闭">✕</button>
            </div>
            {(
              <div className="px-2.5 py-2 space-y-1 font-mono tabular-nums">
                <div className="flex justify-between"><span className="text-slate-500">区间</span><span>{rangeStats.startTime} ~ {rangeStats.endTime}</span></div>
                <div className="flex justify-between"><span className="text-slate-500">周期数</span><span>{rangeStats.bars} 根</span></div>
                <div className="flex justify-between">
                  <span className="text-slate-500">涨跌幅</span>
                  <span className={cc.getColorClass(rangeStats.changePct >= 0)}>{rangeStats.changePct >= 0 ? '+' : ''}{rangeStats.changePct.toFixed(2)}%</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-500">涨跌价</span>
                  <span className={cc.getColorClass(rangeStats.changeVal >= 0)}>{rangeStats.changeVal >= 0 ? '+' : ''}{rangeStats.changeVal.toFixed(2)}</span>
                </div>
                <div className="flex justify-between"><span className="text-slate-500">开盘 / 收盘</span><span>{rangeStats.open.toFixed(2)} / {rangeStats.close.toFixed(2)}</span></div>
                <div className="flex justify-between"><span className="text-slate-500">最高 / 最低</span><span>{rangeStats.high.toFixed(2)} / {rangeStats.low.toFixed(2)}</span></div>
                <div className="flex justify-between"><span className="text-slate-500">成交量</span><span>{formatRangeVolume(rangeStats.volume)}</span></div>
                <div className="flex justify-between"><span className="text-slate-500">成交额</span><span>{formatRangeAmount(rangeStats.amount)}</span></div>
              </div>
            )}
          </div>
        )}

        {/* 主图指标图例 - TradingView 风格 */}
        {!isTrendLinePeriod && (
          <div className="absolute top-1 left-1 z-20 flex flex-col gap-0.5 pointer-events-auto">
            {/* MA */}
            <div className="flex items-center gap-1.5 text-[11px] font-mono">
              <button
                className="opacity-60 hover:opacity-100 transition-opacity"
                onClick={() => updateIndicator('ma', { enabled: !indicatorConfig.ma.enabled })}
                title={indicatorConfig.ma.enabled ? '隐藏 MA' : '显示 MA'}
              >
                {indicatorConfig.ma.enabled
                  ? <Eye size={12} className="text-yellow-500" />
                  : <EyeOff size={12} className={colors.isDark ? 'text-slate-600' : 'text-slate-400'} />}
              </button>
              {indicatorConfig.ma.enabled && indicatorConfig.ma.periods.map((p, i) => (
                <span key={p} style={{ color: MA_COLORS[i % MA_COLORS.length] }}>
                  MA{p}
                </span>
              ))}
              {!indicatorConfig.ma.enabled && (
                <span className={colors.isDark ? 'text-slate-600 line-through' : 'text-slate-400 line-through'}>MA</span>
              )}
            </div>
            {/* EMA */}
            <div className="flex items-center gap-1.5 text-[11px] font-mono">
              <button
                className="opacity-60 hover:opacity-100 transition-opacity"
                onClick={() => updateIndicator('ema', { enabled: !indicatorConfig.ema.enabled })}
                title={indicatorConfig.ema.enabled ? '隐藏 EMA' : '显示 EMA'}
              >
                {indicatorConfig.ema.enabled
                  ? <Eye size={12} className="text-cyan-500" />
                  : <EyeOff size={12} className={colors.isDark ? 'text-slate-600' : 'text-slate-400'} />}
              </button>
              {indicatorConfig.ema.enabled && indicatorConfig.ema.periods.map((p, i) => (
                <span key={p} style={{ color: EMA_COLORS[i % EMA_COLORS.length] }}>
                  EMA{p}
                </span>
              ))}
              {!indicatorConfig.ema.enabled && (
                <span className={colors.isDark ? 'text-slate-600 line-through' : 'text-slate-400 line-through'}>EMA</span>
              )}
            </div>
            {/* BOLL */}
            <div className="flex items-center gap-1.5 text-[11px] font-mono">
              <button
                className="opacity-60 hover:opacity-100 transition-opacity"
                onClick={() => updateIndicator('boll', { enabled: !indicatorConfig.boll.enabled })}
                title={indicatorConfig.boll.enabled ? '隐藏 BOLL' : '显示 BOLL'}
              >
                {indicatorConfig.boll.enabled
                  ? <Eye size={12} style={{ color: BOLL_COLOR }} />
                  : <EyeOff size={12} className={colors.isDark ? 'text-slate-600' : 'text-slate-400'} />}
              </button>
              {indicatorConfig.boll.enabled ? (
                <span style={{ color: BOLL_COLOR }}>BOLL({indicatorConfig.boll.period},{indicatorConfig.boll.multiplier})</span>
              ) : (
                <span className={colors.isDark ? 'text-slate-600 line-through' : 'text-slate-400 line-through'}>BOLL</span>
              )}
            </div>
          </div>
        )}

        {latestTradingSignal && chartViewport.height >= 250 && (
          <div className={`absolute top-2 right-12 z-20 max-w-[320px] rounded border px-3 py-2 shadow-sm backdrop-blur ${getSignalClasses(latestTradingSignal)}`}>
            <div className="flex items-center gap-2 text-xs">
              <span className="shrink-0">{getSignalIcon(latestTradingSignal)}</span>
              <span className="font-bold">{latestTradingSignal.title}</span>
              <span className="font-mono opacity-80">{latestTradingSignal.rawTime.slice(0, 10)}</span>
            </div>
            <div className="mt-1 line-clamp-2 text-[11px] opacity-85">
              {latestTradingSignal.reason}
            </div>
          </div>
        )}

        {/* 指标快捷配置浮动面板 */}
        {indicatorPopup && (
          <div
            className={`absolute z-30 rounded shadow-lg border text-xs p-2 min-w-[180px] ${
              colors.isDark
                ? 'bg-slate-800 border-slate-700 text-slate-200'
                : 'bg-white border-slate-300 text-slate-700'
            }`}
            style={{
              left: indicatorPopupLeft,
              top: indicatorPopupTop,
            }}
            onClick={(e) => e.stopPropagation()}
            onMouseDown={(e) => e.stopPropagation()}
          >
            {/* MA / EMA 配置（共用结构） */}
            {(indicatorPopup.type === 'ma' || indicatorPopup.type === 'ema') && (() => {
              const key = indicatorPopup.type;
              const label = key === 'ma' ? 'MA 均线' : 'EMA 指数均线';
              const cfg = indicatorConfig[key];
              return (
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between">
                    <span className="font-bold">{label}</span>
                    <label className="flex items-center gap-1 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={cfg.enabled}
                        onChange={(e) => updateIndicator(key, { enabled: e.target.checked })}
                        className="accent-blue-500"
                      />
                      <span className="text-[10px]">显示</span>
                    </label>
                  </div>
                  <div>
                    <span className={`text-[10px] ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>周期（逗号分隔）</span>
                    <input
                      className={`w-full mt-0.5 px-1.5 py-0.5 rounded text-xs border ${
                        colors.isDark ? 'bg-slate-700 border-slate-600' : 'bg-slate-50 border-slate-300'
                      }`}
                      value={cfg.periods.join(',')}
                      onChange={(e) => {
                        const periods = e.target.value.split(',').map(Number).filter(n => n > 0);
                        if (periods.length > 0) updateIndicator(key, { periods });
                      }}
                    />
                  </div>
                </div>
              );
            })()}

            {/* BOLL 配置 */}
            {indicatorPopup.type === 'boll' && (
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between">
                  <span className="font-bold">BOLL 布林带</span>
                  <label className="flex items-center gap-1 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={indicatorConfig.boll.enabled}
                      onChange={(e) => updateIndicator('boll', { enabled: e.target.checked })}
                      className="accent-blue-500"
                    />
                    <span className="text-[10px]">显示</span>
                  </label>
                </div>
                <div className="grid grid-cols-2 gap-1.5">
                  <div>
                    <span className={`text-[10px] ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>周期</span>
                    <input
                      type="number"
                      className={`w-full mt-0.5 px-1.5 py-0.5 rounded text-xs border ${
                        colors.isDark ? 'bg-slate-700 border-slate-600' : 'bg-slate-50 border-slate-300'
                      }`}
                      value={indicatorConfig.boll.period}
                      onChange={(e) => updateIndicator('boll', { period: Number(e.target.value) || 20 })}
                    />
                  </div>
                  <div>
                    <span className={`text-[10px] ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>倍数</span>
                    <input
                      type="number"
                      step="0.1"
                      className={`w-full mt-0.5 px-1.5 py-0.5 rounded text-xs border ${
                        colors.isDark ? 'bg-slate-700 border-slate-600' : 'bg-slate-50 border-slate-300'
                      }`}
                      value={indicatorConfig.boll.multiplier}
                      onChange={(e) => updateIndicator('boll', { multiplier: Number(e.target.value) || 2 })}
                    />
                  </div>
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* 拖拽分隔条（上下拉调整副图区高度） */}
      <ResizeHandle direction="vertical" onResize={handleVolumeResize} />

      {/* 副图区域：单图/四宫格都用 pane 左上角下拉切换指标 */}
      <div className="relative border-t fin-divider" style={{ height: gridMode ? gridSubHeight : volumeHeight }}>
        <div className="absolute inset-0" ref={volumeContainerRef} />
        <canvas
          ref={vipOverlayCanvasRef}
          className={`absolute inset-0 z-10 pointer-events-none ${showVipOverlayCanvas ? 'block' : 'hidden'}`}
        />
        {!isTrendLinePeriod && hasData && !gridMode && (
          renderSubSelect(subChartType, handleSubChartSwitch)
        )}
        {hasData && !gridMode && !isTrendLinePeriod && isVipOverlayType && (
          <>
            <div className="absolute left-[156px] right-16 top-1 z-20 pointer-events-none flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[10px] font-mono tabular-nums">
              {getSubLegendItems(subChartType).map((item, index) => (
                <span key={`${item.label}-${item.value}-${index}`} style={{ color: item.color }}>
                  {item.value ? `${item.label}: ${item.value}` : item.label}
                </span>
              ))}
            </div>
            {subChartType === 'vipAnomaly' && (
              <div className="absolute left-5 top-10 z-20 pointer-events-none text-[11px] font-bold text-yellow-300">
                资金点火，起爆拉升
              </div>
            )}
          </>
        )}
        {hasData && !gridMode && !(isVipOverlayType && !isTrendLinePeriod) && renderBottomLegend(getSubLegendItems(isTrendLinePeriod ? 'volume' : subChartType), 'bottom-1.5')}
        {gridMode && !isTrendLinePeriod && hasData && (
          <>
            {([
              subChartType,
              subType2,
              subType3,
            ]).map((type, i) => (
              <React.Fragment key={`${type}-${i}`}>
                {renderBottomLegend(
                  getSubLegendItems(type),
                  '',
                  {
                    top: subPaneRects[i]
                      ? Math.max(0, subPaneRects[i].top + subPaneRects[i].height - 23)
                      : `calc(${((i + 1) * 100) / 3}% - 23px)`,
                  },
                )}
              </React.Fragment>
            ))}
            {renderSubSelect(subChartType, handleSubChartSwitch, { top: subPaneRects[0]?.top ?? '0%' })}
            {renderSubSelect(subType2, handleSub2Switch, { top: subPaneRects[1]?.top ?? `${100 / 3}%` })}
            {renderSubSelect(subType3, handleSub3Switch, { top: subPaneRects[2]?.top ?? `${200 / 3}%` })}
          </>
        )}
      </div>
    </div>
    {hoverTooltipPortal}
    </>
  );
};
