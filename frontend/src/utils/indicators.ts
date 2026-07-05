// 技术指标计算模块
// 所有函数为纯函数，接收 KLineData[] 和参数，返回 LineData[] 或专用结构

import { Time, LineData } from 'lightweight-charts';
import { KLineData } from '../types';

// ========== 工具函数 ==========

/** 解析时间字符串为 lightweight-charts Time 格式 */
export function parseTime(timeStr: string): Time {
  if (timeStr.length > 10) {
    const [datePart, timePart] = timeStr.split(' ');
    const [year, month, day] = datePart.split('-').map(Number);
    const [hour, minute, second] = timePart.split(':').map(Number);
    const utcTimestamp = Date.UTC(year, month - 1, day, hour, minute, second || 0);
    return Math.floor(utcTimestamp / 1000) as Time;
  }
  return timeStr as Time;
}

// ========== 主图指标 ==========

/** 简单移动平均线 SMA */
export function calculateSMA(data: KLineData[], period: number): LineData[] {
  const result: LineData[] = [];
  for (let i = period - 1; i < data.length; i++) {
    let sum = 0;
    for (let j = 0; j < period; j++) {
      sum += data[i - j].close;
    }
    result.push({ time: parseTime(data[i].time), value: sum / period });
  }
  return result;
}

/** 指数移动平均线 EMA */
export function calculateEMA(data: KLineData[], period: number): LineData[] {
  if (data.length === 0) return [];
  const k = 2 / (period + 1);
  const result: LineData[] = [];
  // 首个 EMA 用前 period 个收盘价的 SMA 作为种子
  let ema = 0;
  for (let i = 0; i < Math.min(period, data.length); i++) {
    ema += data[i].close;
  }
  ema /= Math.min(period, data.length);
  result.push({ time: parseTime(data[Math.min(period, data.length) - 1].time), value: ema });

  for (let i = period; i < data.length; i++) {
    ema = data[i].close * k + ema * (1 - k);
    result.push({ time: parseTime(data[i].time), value: ema });
  }
  return result;
}

/** 布林带 BOLL — 返回 { mid, upper, lower } */
export function calculateBOLL(
  data: KLineData[],
  period: number,
  multiplier: number,
): { mid: LineData[]; upper: LineData[]; lower: LineData[] } {
  const mid: LineData[] = [];
  const upper: LineData[] = [];
  const lower: LineData[] = [];

  for (let i = period - 1; i < data.length; i++) {
    let sum = 0;
    for (let j = 0; j < period; j++) sum += data[i - j].close;
    const ma = sum / period;

    let variance = 0;
    for (let j = 0; j < period; j++) variance += (data[i - j].close - ma) ** 2;
    const std = Math.sqrt(variance / period);

    const t = parseTime(data[i].time);
    mid.push({ time: t, value: ma });
    upper.push({ time: t, value: ma + multiplier * std });
    lower.push({ time: t, value: ma - multiplier * std });
  }
  return { mid, upper, lower };
}

// ========== 副图指标 ==========

/** MACD 指标 — 返回 { dif, dea, histogram } */
export interface HistogramItem {
  time: Time;
  value: number;
  color: string;
}

export function calculateMACD(
  data: KLineData[],
  fast: number,
  slow: number,
  signal: number,
): { dif: LineData[]; dea: LineData[]; histogram: HistogramItem[] } {
  if (data.length < slow) return { dif: [], dea: [], histogram: [] };

  // 计算内部 EMA 序列（返回纯数值数组）
  const emaCalc = (closes: number[], period: number): number[] => {
    const k = 2 / (period + 1);
    const result: number[] = [];
    let ema = 0;
    for (let i = 0; i < period; i++) ema += closes[i];
    ema /= period;
    for (let i = 0; i < period; i++) result.push(NaN);
    result[period - 1] = ema;
    for (let i = period; i < closes.length; i++) {
      ema = closes[i] * k + ema * (1 - k);
      result.push(ema);
    }
    return result;
  };

  const closes = data.map(d => d.close);
  const emaFast = emaCalc(closes, fast);
  const emaSlow = emaCalc(closes, slow);

  // DIF = EMA(fast) - EMA(slow)
  const difArr: number[] = [];
  for (let i = 0; i < data.length; i++) {
    difArr.push(isNaN(emaFast[i]) || isNaN(emaSlow[i]) ? NaN : emaFast[i] - emaSlow[i]);
  }

  // DEA = EMA(DIF, signal)
  const validDif = difArr.filter(v => !isNaN(v));
  if (validDif.length < signal) return { dif: [], dea: [], histogram: [] };

  const dif: LineData[] = [];
  const dea: LineData[] = [];
  const histogram: HistogramItem[] = [];

  const kk = 2 / (signal + 1);
  let deaVal = 0;
  for (let i = 0; i < signal; i++) deaVal += validDif[i];
  deaVal /= signal;

  const startIdx = data.length - validDif.length;
  for (let i = 0; i < validDif.length; i++) {
    const dataIdx = startIdx + i;
    const t = parseTime(data[dataIdx].time);
    const difVal = validDif[i];

    if (i < signal - 1) {
      dif.push({ time: t, value: difVal });
      continue;
    }
    if (i === signal - 1) {
      // 第一个 DEA 点
    } else {
      deaVal = difVal * kk + deaVal * (1 - kk);
    }

    const hist = (difVal - deaVal) * 2;
    dif.push({ time: t, value: difVal });
    dea.push({ time: t, value: deaVal });
    histogram.push({ time: t, value: hist, color: hist >= 0 ? '#ef4444' : '#22c55e' });
  }

  return { dif, dea, histogram };
}

/** RSI 相对强弱指标 */
export function calculateRSI(data: KLineData[], period: number): LineData[] {
  if (data.length < period + 1) return [];
  const result: LineData[] = [];

  let avgGain = 0;
  let avgLoss = 0;
  for (let i = 1; i <= period; i++) {
    const diff = data[i].close - data[i - 1].close;
    if (diff > 0) avgGain += diff;
    else avgLoss += Math.abs(diff);
  }
  avgGain /= period;
  avgLoss /= period;

  const rsi = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);
  result.push({ time: parseTime(data[period].time), value: rsi });

  for (let i = period + 1; i < data.length; i++) {
    const diff = data[i].close - data[i - 1].close;
    const gain = diff > 0 ? diff : 0;
    const loss = diff < 0 ? Math.abs(diff) : 0;
    avgGain = (avgGain * (period - 1) + gain) / period;
    avgLoss = (avgLoss * (period - 1) + loss) / period;
    const val = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);
    result.push({ time: parseTime(data[i].time), value: val });
  }
  return result;
}

/** KDJ 随机指标 — 返回 { k, d, j } */
export function calculateKDJ(
  data: KLineData[],
  period: number,
  kSmooth: number,
  dSmooth: number,
): { k: LineData[]; d: LineData[]; j: LineData[] } {
  if (data.length < period) return { k: [], d: [], j: [] };

  const kResult: LineData[] = [];
  const dResult: LineData[] = [];
  const jResult: LineData[] = [];

  let prevK = 50;
  let prevD = 50;

  for (let i = period - 1; i < data.length; i++) {
    let highest = -Infinity;
    let lowest = Infinity;
    for (let j = 0; j < period; j++) {
      highest = Math.max(highest, data[i - j].high);
      lowest = Math.min(lowest, data[i - j].low);
    }

    const rsv = highest === lowest
      ? 50
      : ((data[i].close - lowest) / (highest - lowest)) * 100;
    const kVal = (prevK * (kSmooth - 1) + rsv) / kSmooth;
    const dVal = (prevD * (dSmooth - 1) + kVal) / dSmooth;
    const jVal = 3 * kVal - 2 * dVal;

    const t = parseTime(data[i].time);
    kResult.push({ time: t, value: kVal });
    dResult.push({ time: t, value: dVal });
    jResult.push({ time: t, value: jVal });

    prevK = kVal;
    prevD = dVal;
  }

  return { k: kResult, d: dResult, j: jResult };
}

/** CCI 顺势指标：(TP - SMA(TP,n)) / (0.015 * 平均绝对偏差) */
export function calculateCCI(data: KLineData[], period = 14): LineData[] {
  const result: LineData[] = [];
  if (data.length < period) return result;
  const tp = data.map(d => (d.high + d.low + d.close) / 3);
  for (let i = period - 1; i < data.length; i++) {
    let sum = 0;
    for (let j = i - period + 1; j <= i; j++) sum += tp[j];
    const ma = sum / period;
    let mad = 0;
    for (let j = i - period + 1; j <= i; j++) mad += Math.abs(tp[j] - ma);
    mad /= period;
    const cci = mad === 0 ? 0 : (tp[i] - ma) / (0.015 * mad);
    result.push({ time: parseTime(data[i].time), value: cci });
  }
  return result;
}

/** WR 威廉指标(%R)：(HHV - C) / (HHV - LLV) * -100，范围 -100~0 */
export function calculateWR(data: KLineData[], period = 14): LineData[] {
  const result: LineData[] = [];
  if (data.length < period) return result;
  for (let i = period - 1; i < data.length; i++) {
    let hh = -Infinity;
    let ll = Infinity;
    for (let j = i - period + 1; j <= i; j++) {
      if (data[j].high > hh) hh = data[j].high;
      if (data[j].low < ll) ll = data[j].low;
    }
    const wr = hh === ll ? 0 : ((hh - data[i].close) / (hh - ll)) * -100;
    result.push({ time: parseTime(data[i].time), value: wr });
  }
  return result;
}
