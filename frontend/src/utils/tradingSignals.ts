import { Time } from 'lightweight-charts';
import { KLineData } from '../types';
import { parseTime } from './indicators';

export type TradingSignalLevel = 'watch' | 'A' | 'A+' | 'S-' | 'S' | 'risk';
export type TradingSignalAction = 'observe' | 'buy' | 'reduce' | 'sell';

export interface TradingSignal {
  time: Time;
  rawTime: string;
  level: TradingSignalLevel;
  action: TradingSignalAction;
  title: string;
  reason: string;
  price: number;
  score: number;
  flags: TradingSignalFlags;
}

export interface TradingSignalFlags {
  trendStrong: boolean;
  trendWeak: boolean;
  trendHold: boolean;
  trendCash: boolean;
  eatFishStart: boolean;
  eatFishContinue: boolean;
  eatFish: boolean;
  moneyFire: boolean;
  gz: boolean;
  strongGz: boolean;
  superGz: boolean;
  coreBuy: boolean;
  superBreakout: boolean;
  takeProfit: boolean;
  sellReduce: boolean;
  sellClear: boolean;
  controlScore: number;
}

const EMPTY_FLAGS: TradingSignalFlags = {
  trendStrong: false,
  trendWeak: false,
  trendHold: false,
  trendCash: false,
  eatFishStart: false,
  eatFishContinue: false,
  eatFish: false,
  moneyFire: false,
  gz: false,
  strongGz: false,
  superGz: false,
  coreBuy: false,
  superBreakout: false,
  takeProfit: false,
  sellReduce: false,
  sellClear: false,
  controlScore: 0,
};

function sma(values: number[], period: number): number[] {
  const out = Array(values.length).fill(NaN);
  let sum = 0;
  for (let i = 0; i < values.length; i++) {
    sum += values[i];
    if (i >= period) sum -= values[i - period];
    if (i >= period - 1) out[i] = sum / period;
  }
  return out;
}

function ema(values: number[], period: number): number[] {
  const out = Array(values.length).fill(NaN);
  if (values.length === 0) return out;
  const seedLen = Math.min(period, values.length);
  let prev = 0;
  for (let i = 0; i < seedLen; i++) prev += values[i];
  prev /= seedLen;
  out[seedLen - 1] = prev;

  const k = 2 / (period + 1);
  for (let i = seedLen; i < values.length; i++) {
    prev = values[i] * k + prev * (1 - k);
    out[i] = prev;
  }
  return out;
}

function highest(values: number[], end: number, period: number, excludeCurrent = false): number {
  const last = excludeCurrent ? end - 1 : end;
  const first = Math.max(0, last - period + 1);
  let max = -Infinity;
  for (let i = first; i <= last; i++) max = Math.max(max, values[i]);
  return max;
}

function lowest(values: number[], end: number, period: number): number {
  const first = Math.max(0, end - period + 1);
  let min = Infinity;
  for (let i = first; i <= end; i++) min = Math.min(min, values[i]);
  return min;
}

function crossedUp(a: number[], b: number[], i: number): boolean {
  return i > 0 && Number.isFinite(a[i]) && Number.isFinite(b[i]) && a[i] > b[i] && a[i - 1] <= b[i - 1];
}

function crossedDown(a: number[], b: number[], i: number): boolean {
  return i > 0 && Number.isFinite(a[i]) && Number.isFinite(b[i]) && a[i] < b[i] && a[i - 1] >= b[i - 1];
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function daysSince(lastIndex: number, i: number): number {
  return lastIndex < 0 ? Infinity : i - lastIndex;
}

function compactReasons(parts: Array<[boolean, string]>): string {
  return parts.filter(([ok]) => ok).map(([, text]) => text).join(' / ');
}

export function calculateTradingSignals(data: KLineData[]): TradingSignal[] {
  if (data.length < 35) return [];

  const close = data.map(d => d.close);
  const high = data.map(d => d.high);
  const low = data.map(d => d.low);
  const open = data.map(d => d.open);
  const volume = data.map(d => d.volume || 0);
  const amount = data.map(d => d.amount || d.close * (d.volume || 0) * 100);

  const ma5 = sma(close, 5);
  const ma10 = sma(close, 10);
  const ma20 = sma(close, 20);
  const ma60 = sma(close, 60);
  const ema12 = ema(close, 12);
  const ema13 = ema(close, 13);
  const ema26 = ema(close, 26);
  const ema34 = ema(close, 34);
  const ema55 = ema(close, 55);
  const ema90 = ema(close, 90);
  const dif = close.map((_, i) => ema12[i] - ema26[i]);
  const dea = ema(dif.map(v => Number.isFinite(v) ? v : 0), 9);
  const vol5 = sma(volume, 5);
  const vol20 = sma(volume, 20);
  const amount20 = sma(amount, 20);

  const signals: TradingSignal[] = [];
  let lastEatFish = -1;

  for (let i = 1; i < data.length; i++) {
    if (!Number.isFinite(ma20[i]) || !Number.isFinite(ema34[i])) continue;

    const prevClose = close[i - 1] || close[i];
    const pct = prevClose > 0 ? (close[i] - prevClose) / prevClose : 0;
    const range = Math.max(high[i] - low[i], 0.01);
    const closePosition = (close[i] - low[i]) / range;
    const trendStrong = Number.isFinite(ema55[i])
      ? ema13[i] > ema34[i] && ema34[i] > ema55[i] && close[i] > ma20[i]
      : ema13[i] > ema34[i] && close[i] > ma20[i];
    const trendWeak = close[i] < ma20[i] || ema13[i] < ema34[i];
    const trendHold = ma5[i] > ma10[i] && close[i] > ma20[i];
    const trendCash = ma5[i] < ma10[i] || close[i] < ma20[i];

    const eatFishStart = Number.isFinite(ema55[i])
      ? crossedUp(ema13, ema55, i) && ema13[i] > ema34[i] && close[i] > ema13[i]
      : crossedUp(ma5, ma20, i) && close[i] > ma10[i];
    const eatFishContinue = lastEatFish >= 0
      && daysSince(lastEatFish, i) <= 30
      && ema13[i] > ema34[i]
      && close[i] > ma10[i]
      && closePosition > 0.45
      && pct > -0.025;
    const eatFish = eatFishStart || eatFishContinue;
    if (eatFish) lastEatFish = i;

    const prevHigh20 = i >= 20 ? highest(close, i, 20, true) : highest(close, i, Math.max(2, i), true);
    const high20 = highest(high, i, 20);
    const low20 = lowest(low, i, 20);
    const volumeExpansion = volume[i] > Math.max(volume[i - 1] * 1.2, (vol5[i] || 0) * 1.05);
    const amountOk = amount[i] > 50_000_000 || amount[i] > (amount20[i] || 0) * 1.25;
    const moneyFire = close[i] > prevHigh20
      && volumeExpansion
      && amountOk
      && close[i] > open[i]
      && closePosition > 0.6;

    const energy = volume[i] * (closePosition - 0.5);
    const energyPrev = volume[i - 1] * (((close[i - 1] - low[i - 1]) / Math.max(high[i - 1] - low[i - 1], 0.01)) - 0.5);
    const shortBull = ma5[i] > ma10[i] && close[i] > ma10[i];
    const midBull = ma10[i] > ma20[i] && (!Number.isFinite(ma60[i]) || ma20[i] >= ma60[i] * 0.98);
    const gz = trendHold && energy > 0 && energy >= energyPrev * 0.8 && shortBull && midBull;
    const controlScore = Number.isFinite(ema90[i])
      ? clamp(((ema13[i] - ema90[i]) / ema90[i]) * 1000 + 50, 0, 100)
      : clamp(((ma5[i] - ma20[i]) / ma20[i]) * 1500 + 50, 0, 100);
    const entryScore = (eatFish ? 30 : 0)
      + (moneyFire ? 25 : 0)
      + (gz ? 20 : 0)
      + (trendStrong ? 15 : 0)
      + (trendHold ? 10 : 0);
    const strongGz = gz && controlScore >= 60;
    const superGz = gz && controlScore >= 80;
    const highZoneDev = high20 > 0 ? (high20 - close[i]) / high20 : 1;
    let highZoneStreak = 0;
    for (let j = i; j >= 0 && j > i - 5; j -= 1) {
      if (Number.isFinite(ema55[j]) && close[j] > ema55[j] * 1.08) {
        highZoneStreak += 1;
      } else {
        break;
      }
    }
    const highZoneBlocked = highZoneStreak >= 5 && highZoneDev < 0.05;

    const macdDead = crossedDown(dif, dea, i);
    const nearUpperRange = high20 > low20 && close[i] > low20 + (high20 - low20) * 0.82;
    const recentEatFish = daysSince(lastEatFish, i) <= 30;
    const prevTrendWeak = close[i - 1] < ma20[i - 1] || ema13[i - 1] < ema34[i - 1];
    const prevTrendCash = ma5[i - 1] < ma10[i - 1] || close[i - 1] < ma20[i - 1];
    const energyTurnNegative = energy < 0 && energyPrev >= 0;
    const nearHighWarning = high20 > 0
      && close[i] >= high20 * 0.965
      && volume[i] < (vol5[i] || volume[i]) * 0.7;
    const ma5SlipWarning = Number.isFinite(ma5[i]) && close[i] < ma5[i] * 0.99;
    const sellWarning = nearHighWarning || ma5SlipWarning;
    const takeProfit = recentEatFish
      && ((nearUpperRange && pct < 0 && volume[i] > (vol5[i] || 0) * 1.15) || macdDead);
    const sellReduce = macdDead
      || (trendWeak && !prevTrendWeak)
      || (trendCash && !prevTrendCash)
      || (recentEatFish && energyTurnNegative);
    const sellClear = close[i] < ma20[i]
      && volume[i] > (vol20[i] || volume[i - 1]) * 1.1
      && pct < -0.025;

    const coreBuy = entryScore >= 70 && !highZoneBlocked;
    const superBreakout = coreBuy && close[i] > prevHigh20 && volume[i] > volume[i - 1] * 1.1;

    const flags: TradingSignalFlags = {
      trendStrong,
      trendWeak,
      trendHold,
      trendCash,
      eatFishStart,
      eatFishContinue,
      eatFish,
      moneyFire,
      gz,
      strongGz,
      superGz,
      coreBuy,
      superBreakout,
      takeProfit,
      sellReduce,
      sellClear,
      controlScore,
    };

    const base = {
      time: parseTime(data[i].time),
      rawTime: data[i].time,
      price: close[i],
      flags,
    };

    if (sellClear) {
      signals.push({
        ...base,
        level: 'risk',
        action: 'sell',
        title: '清仓风险',
        score: -90,
        reason: compactReasons([[trendWeak, '趋势转弱'], [trendCash, '趋势持币'], [macdDead, 'MACD死叉'], [energy < 0, '能量转负'], [true, '放量跌破MA20']]),
      });
      continue;
    }

    if (takeProfit || sellReduce || sellWarning) {
      const warningOnly = sellWarning && !takeProfit && !sellReduce;
      signals.push({
        ...base,
        level: 'risk',
        action: 'reduce',
        title: takeProfit ? '止盈/减仓' : (warningOnly ? '高位预警减仓' : '减仓观察'),
        score: takeProfit ? -70 : (warningOnly ? -36 : -45),
        reason: compactReasons([
          [takeProfit, '鱼身后高位转弱'],
          [nearHighWarning, '近高位缩量背离'],
          [ma5SlipWarning, '收盘跌破MA5超1%'],
          [trendWeak, '趋势转弱'],
          [trendCash, '趋势持币'],
          [macdDead, 'MACD死叉'],
          [energy < 0, '能量转负'],
        ]),
      });
      continue;
    }

    if (coreBuy) {
      signals.push({
        ...base,
        level: 'S',
        action: 'buy',
        title: superBreakout ? 'S级超强买点' : 'S级核心买点',
        score: superBreakout ? 98 : 92,
        reason: compactReasons([[eatFish, '吃鱼身'], [moneyFire, '异动起爆'], [gz, '多周期共振'], [trendStrong, '强势趋势'], [trendHold, '趋势持股']]),
      });
    } else if (eatFish && moneyFire) {
      signals.push({
        ...base,
        level: 'A+',
        action: 'buy',
        title: 'A+鱼身点火',
        score: 82,
        reason: compactReasons([[eatFishStart, '吃鱼启动'], [eatFishContinue, '吃鱼延续'], [moneyFire, '资金点火'], [trendStrong, '强势趋势']]),
      });
    } else if (eatFish && superGz) {
      signals.push({
        ...base,
        level: 'S-',
        action: 'buy',
        title: 'S-高控盘鱼身',
        score: 78,
        reason: compactReasons([[eatFish, '吃鱼身'], [superGz, '超强共振'], [true, `控盘度${controlScore.toFixed(0)}`]]),
      });
    } else if (eatFish && gz) {
      signals.push({
        ...base,
        level: 'A',
        action: 'buy',
        title: 'A级鱼身共振',
        score: 72,
        reason: compactReasons([[eatFishStart, '吃鱼启动'], [eatFishContinue, '吃鱼延续'], [gz, '多周期共振'], [strongGz, '控盘增强']]),
      });
    } else if (trendStrong && trendHold && !signals.some(s => s.rawTime === data[i].time)) {
      const prevTrendOk = i > 1 && ma5[i - 1] > ma10[i - 1] && close[i - 1] > ma20[i - 1];
      if (!prevTrendOk) {
        signals.push({
          ...base,
          level: 'watch',
          action: 'observe',
          title: '观察',
          score: 45,
          reason: '强势趋势 + 趋势持股，加入观察池',
        });
      }
    }
  }

  return signals;
}

export function calculateIntradayTradingSignals(data: KLineData[], preClose = 0, dayKData: KLineData[] = [], includeBingGeT = false): TradingSignal[] {
  if (data.length < 12) return [];

  const close = data.map(d => d.close);
  const high = data.map(d => d.high);
  const low = data.map(d => d.low);
  const open = data.map(d => d.open);
  const volume = data.map(d => d.volume || 0);
  const avgLine = data.map((d, i) => d.avg || sma(close.slice(0, i + 1), Math.min(i + 1, 5))[i] || d.close);

  const ma5 = sma(close, 5);
  const ma10 = sma(close, 10);
  const ma20 = sma(close, 20);
  const vol5 = sma(volume, 5);
  const vol20 = sma(volume, 20);
  const ema12 = ema(close, 12);
  const ema26 = ema(close, 26);
  const dif = close.map((_, i) => ema12[i] - ema26[i]);
  const dea = ema(dif.map(v => Number.isFinite(v) ? v : 0), 9);

  const dailyClose = dayKData.map(d => d.close);
  const dailyHigh = dayKData.map(d => d.high);
  const dailyEma55 = ema(dailyClose, 55);
  const dailyLast = dailyClose.length - 1;
  const dailyHigh20 = dailyLast >= 0
    ? highest(dailyHigh, dailyLast, Math.min(20, dailyLast + 1))
    : 0;
  const dailyDevFromHigh = dailyLast >= 0 && dailyHigh20 > 0
    ? (dailyHigh20 - dailyClose[dailyLast]) / dailyHigh20
    : 1;
  let dailyHighZoneStreak = 0;
  for (let j = dailyLast; j >= 0 && j > dailyLast - 5; j -= 1) {
    if (Number.isFinite(dailyEma55[j]) && dailyClose[j] > dailyEma55[j] * 1.08) {
      dailyHighZoneStreak += 1;
    } else {
      break;
    }
  }
  const dailyHighZoneBlocked = dailyHighZoneStreak >= 5 && dailyDevFromHigh < 0.05;

  const signals: TradingSignal[] = [];
  let aboveAvgCount = 0;
  let belowAvgCount = 0;
  let lastBuyIndex = -1;
  let lastRiskIndex = -1;
  let lastBuyPrice = Number.NaN;
  let highSinceBuy = -Infinity;
  let dayHigh = high[0];
  let dayLow = low[0];
  const BUY_COOLDOWN_BARS = 8;
  const RISK_COOLDOWN_BARS = 6;

  for (let i = 1; i < data.length; i++) {
    dayHigh = Math.max(dayHigh, high[i]);
    dayLow = Math.min(dayLow, low[i]);
    if (!Number.isFinite(ma10[i]) || !Number.isFinite(avgLine[i])) continue;

    const prevClose = close[i - 1] || close[i];
    const pctFromPrevClose = preClose > 0 ? (close[i] - preClose) / preClose : 0;
    const minutePct = prevClose > 0 ? (close[i] - prevClose) / prevClose : 0;
    const range = Math.max(high[i] - low[i], 0.01);
    const closePosition = (close[i] - low[i]) / range;
    const aboveAvg = close[i] >= avgLine[i];
    aboveAvgCount = aboveAvg ? aboveAvgCount + 1 : 0;
    belowAvgCount = !aboveAvg ? belowAvgCount + 1 : 0;

    const trendStrong = aboveAvg && ma5[i] > ma10[i] && close[i] >= ma5[i];
    const trendWeak = !aboveAvg || ma5[i] < ma10[i];
    const trendHold = aboveAvgCount >= 3 && trendStrong;
    const trendCash = belowAvgCount >= 3 || close[i] < ma10[i];
    const intradayRangePct = preClose > 0 ? (dayHigh - dayLow) / preClose : 0;
    const weakTape = pctFromPrevClose < -0.02 && (ma5[i] < ma10[i] || belowAvgCount >= 3);
    const hotTape = intradayRangePct > 0.04 && volume[i] > (vol20[i] || volume[i - 1] || volume[i]) * 1.15;
    const marketMult = weakTape ? 1.5 : hotTape ? 0.6 : 1;
    const buyCooldownBars = Math.max(3, Math.min(12, Math.round(BUY_COOLDOWN_BARS * marketMult)));
    const riskCooldownBars = Math.max(3, Math.min(10, Math.round(RISK_COOLDOWN_BARS * (weakTape ? 1.2 : 1))));
    const prevHigh = highest(high, i, Math.min(20, i), true);
    const volumeExpansion = volume[i] > Math.max((vol5[i] || 0) * 1.35, volume[i - 1] * 1.15);
    const pullbackHold = i >= 20
      && close[i] >= avgLine[i]
      && low[i] <= avgLine[i] * 1.003
      && closePosition > 0.55
      && aboveAvgCount >= 2
      && pctFromPrevClose > -0.015;
    const breakout = close[i] > prevHigh
      && volumeExpansion
      && close[i] > open[i]
      && closePosition > 0.6;
    const moneyFire = breakout || (pullbackHold && volume[i] > (vol5[i] || 0) * 1.05);
    const gz = trendHold && close[i] > ma10[i] && (!Number.isFinite(ma20[i]) || ma10[i] >= ma20[i] * 0.998);
    const eatFishStart = crossedUp(close, avgLine, i) && ma5[i] >= ma10[i] && volumeExpansion;
    const eatFishContinue = pullbackHold || (aboveAvgCount >= 5 && ma5[i] > ma10[i] && minutePct > -0.004);
    const eatFish = eatFishStart || eatFishContinue;
    const controlScore = clamp(((close[i] - avgLine[i]) / avgLine[i]) * 2500 + aboveAvgCount * 4 + 45, 0, 100);
    const entryScore = (eatFish ? 30 : 0)
      + (moneyFire ? 25 : 0)
      + (gz ? 20 : 0)
      + (trendStrong ? 15 : 0)
      + (trendHold ? 10 : 0);
    const strongGz = gz && controlScore >= 60;
    const superGz = gz && controlScore >= 80;
    const coreBuy = entryScore >= 70
      && controlScore >= 62
      && aboveAvgCount >= 3
      && pctFromPrevClose > -0.006;
    const superBreakout = coreBuy && breakout && close[i] >= dayHigh * 0.998;

    const macdDead = crossedDown(dif, dea, i);
    const fallFromHigh = dayHigh > 0 ? (dayHigh - close[i]) / dayHigh : 0;
    const avgBreak = crossedDown(close, avgLine, i) || belowAvgCount >= 3;
    if (Number.isFinite(lastBuyPrice)) {
      highSinceBuy = Math.max(highSinceBuy, high[i]);
    }
    const floatProfit = Number.isFinite(lastBuyPrice) ? (close[i] - lastBuyPrice) / lastBuyPrice : 0;
    const trailingPct = floatProfit >= 0.15 ? 0.08 : floatProfit >= 0.08 ? 0.05 : floatProfit >= 0.03 ? 0.025 : Number.NaN;
    const trailingLine = Number.isFinite(trailingPct) && Number.isFinite(highSinceBuy)
      ? highSinceBuy * (1 - trailingPct)
      : Number.NaN;
    const takeProfit = Number.isFinite(trailingLine)
      && close[i] < trailingLine
      && volume[i] > (vol5[i] || volume[i - 1] || volume[i]) * 0.8;
    const sellReduce = avgBreak || macdDead || (pctFromPrevClose > 0.03 && fallFromHigh >= 0.018);
    const sellClear = close[i] < avgLine[i] * 0.995
      && close[i] < ma10[i]
      && volume[i] > (vol20[i] || volume[i - 1]) * 1.2
      && minutePct < -0.006;

    const flags: TradingSignalFlags = {
      trendStrong,
      trendWeak,
      trendHold,
      trendCash,
      eatFishStart,
      eatFishContinue,
      eatFish,
      moneyFire,
      gz,
      strongGz,
      superGz,
      coreBuy,
      superBreakout,
      takeProfit,
      sellReduce,
      sellClear,
      controlScore,
    };

    const base = {
      time: parseTime(data[i].time),
      rawTime: data[i].time,
      price: close[i],
      flags,
    };

    const canEmitRisk = lastRiskIndex < 0 || i - lastRiskIndex >= riskCooldownBars;
    if (sellClear && canEmitRisk) {
      lastRiskIndex = i;
      lastBuyPrice = Number.NaN;
      highSinceBuy = -Infinity;
      signals.push({
        ...base,
        level: 'risk',
        action: 'sell',
        title: '分时清仓风险',
        score: -88,
        reason: compactReasons([[true, '放量跌破均价线'], [trendCash, '分时转持币'], [macdDead, 'MACD死叉'], [true, '短线破位']]),
      });
      continue;
    }

    if ((takeProfit || sellReduce) && canEmitRisk) {
      lastRiskIndex = i;
      signals.push({
        ...base,
        level: 'risk',
        action: 'reduce',
        title: takeProfit ? '分时止盈' : '分时减仓',
        score: takeProfit ? -72 : -48,
        reason: compactReasons([[takeProfit, '跌破动态止盈线'], [avgBreak, '跌破均价线'], [macdDead, 'MACD死叉'], [fallFromHigh >= 0.018, '高位回撤']]),
      });
      continue;
    }

    const canEmitBuy = lastBuyIndex < 0 || i - lastBuyIndex >= buyCooldownBars;
    if (coreBuy && canEmitBuy) {
      lastBuyIndex = i;
      lastBuyPrice = close[i];
      highSinceBuy = high[i];
      signals.push({
        ...base,
        level: superBreakout ? 'S' : 'A+',
        action: 'buy',
        title: superBreakout ? '分时S级点火' : '分时A+买点',
        score: superBreakout ? 92 : 82,
        reason: compactReasons([[eatFishStart, '上穿均价线'], [eatFishContinue, '均价线承接'], [moneyFire, '放量点火'], [gz, '分时共振'], [trendHold, '站稳均价']]),
      });
    } else if (pullbackHold && strongGz && controlScore >= 58 && canEmitBuy) {
      if (dailyHighZoneBlocked) {
        signals.push({
          ...base,
          level: 'watch',
          action: 'observe',
          title: '分时观察',
          score: 40,
          reason: '日K处于高位区，分时低吸降级观察',
        });
        continue;
      }
      lastBuyIndex = i;
      lastBuyPrice = close[i];
      highSinceBuy = high[i];
      signals.push({
        ...base,
        level: 'A',
        action: 'buy',
        title: '分时低吸承接',
        score: 70,
        reason: compactReasons([[true, '回踩均价线不破'], [strongGz, '控盘共振'], [trendHold, '分时趋势持股']]),
      });
    } else if (trendHold && !signals.some(s => s.rawTime === data[i].time)) {
      const prevTrendHold = i > 1 && close[i - 1] >= avgLine[i - 1] && ma5[i - 1] > ma10[i - 1];
      if (!prevTrendHold) {
        signals.push({
          ...base,
          level: 'watch',
          action: 'observe',
          title: '分时观察',
          score: 42,
          reason: '站上均价线 + 短线趋势向上，等待放量或回踩承接',
        });
      }
    }
  }

  // —— 兵哥分时做T(2026-06-03收费版核心,升级接入)——
  // 阻力=L1+P1*7/8、支撑=L1+P1*0.5/8,其中 H1=max(昨收,日内高)、L1=min(昨收,日内低)(运行值因果化,不用收盘定格);
  // T卖:C 在阻力下待≥2根后上穿阻力(冲高触阻力高抛);T买:C 在支撑上待≥2根后跌破支撑(急杀破支撑低吸);
  // 起涨:C/当日VWAP 比价线 EXPMA30 上穿 EXPMA60;加仓:限制散户线(EXPMA120×中间价轴)在均价线上方且 C 上穿。
  // 仅"兵哥做T"主图模板选中且单交易日数据(分时1m)启用——默认分时不打,做T是日内行为。
  const dates = new Set(data.map(d => String(d.time).slice(0, 10)));
  if (includeBingGeT && dates.size === 1 && data.length >= 30) {
    const n = data.length;
    const { support, resistance } = bingGeTLevels(data, preClose);
    let cumPV = 0;
    let cumV = 0;
    let cumC = 0;
    const ratio: number[] = new Array(n);
    const vwapArr: number[] = new Array(n);
    const midAxis: number[] = new Array(n);
    for (let i = 0; i < n; i++) {
      cumPV += close[i] * (volume[i] || 0);
      cumV += volume[i] || 0;
      cumC += close[i];
      const vwap = cumV > 0 ? cumPV / cumV : close[i];
      vwapArr[i] = vwap;
      // 原式 VBNH 有个 ±5% 的价带回退:C/VWAP 落在 [0.95,1.05] 之外时改用 MA(C,全程)。
      // 之前注释说"VWAP 天然在价带内"所以省了 —— 那只在振幅小的日子成立。
      // 大涨大跌日(截图里有研新材 +9.76%)C/VWAP 会冲出价带,省掉这段会让
      // 比价线整体偏移,起涨/加仓的位置跟通达信对不上。
      const inBand = vwap > 0 && close[i] / vwap >= 0.95 && close[i] / vwap <= 1.05;
      const vbnh = inBand ? vwap : cumC / (i + 1);
      ratio[i] = vbnh > 0 ? close[i] / vbnh : 1;
    }
    // 中间价轴:与通达信**逐字对齐**(2026-08-05 用户明确要求)。
    //
    // 原式 LIJINA9:=CONST(HHV(C,250)) —— CONST 取全序列最后一个值,
    // 等于把**当日收盘后才知道的最高/最低**当常数套回每一根K线。
    // ⚠️这就是通达信自己在图上标「用到未来数据」的来源:
    //    上午十点画出来的信号,用到了下午三点的数据。
    // 盘后复盘与通达信完全一致,但**盘中实时会随新高新低跳位置** —— 这是用户选的口径。
    let dayHi = -Infinity;
    let dayLo = Infinity;
    for (let i = 0; i < n; i++) {
      dayHi = Math.max(dayHi, close[i]);
      dayLo = Math.min(dayLo, close[i]);
    }
    const axisSpan = dayHi - dayLo;              // 轴差
    const axisCenter = (dayHi + dayLo) / 2;      // 中价轴
    const midConst = axisCenter - axisSpan * 15 / 130; // (50-HL3)*轴差/HL4+中价轴,HL3=65,HL4=130
    for (let i = 0; i < n; i++) midAxis[i] = midConst;
    const emaR30 = ema(ratio, 30);
    const emaR60 = ema(ratio, 60);
    const emaR120 = ema(ratio, 120);
    const mkBase = (i: number) => ({
      time: parseTime(data[i].time),
      rawTime: data[i].time,
      price: close[i],
      flags: EMPTY_FLAGS,
    });
    let lastTBuy = -999;
    let lastTSell = -999;
    let lastRise = -999;
    let lastAdd = -999;
    for (let i = 3; i < n; i++) {
      // T卖:前两根都在阻力下方,本根收上阻力
      if (close[i] > resistance[i] && close[i - 1] <= resistance[i - 1] && close[i - 2] <= resistance[i - 2] && i - lastTSell >= 6) {
        signals.push({
          ...mkBase(i), level: 'risk', action: 'reduce', score: 70,
          title: '兵哥T·卖出', reason: '冲高触及日内阻力位(高低区间7/8),做T高抛',
        });
        lastTSell = i;
      }
      // T买:前两根都在支撑上方,本根跌破支撑
      if (close[i] < support[i] && close[i - 1] >= support[i - 1] && close[i - 2] >= support[i - 2] && i - lastTBuy >= 6) {
        signals.push({
          ...mkBase(i), level: 'A+', action: 'buy', score: 72,
          title: '兵哥T·买入', reason: '急杀跌破日内支撑位(高低区间0.5/8),做T低吸',
        });
        lastTBuy = i;
      }
      // 起涨:VWAP比价 EXPMA30 上穿 EXPMA60
      if (crossedUp(emaR30, emaR60, i) && i - lastRise >= 30) {
        signals.push({
          ...mkBase(i), level: 'A', action: 'buy', score: 58,
          title: '起涨', reason: 'VWAP比价快线上穿慢线,日内动能转强',
        });
        lastRise = i;
      }
      // 加仓:限制散户线在均价线上方且价格上穿它
      const retailLine = emaR120[i] * midAxis[i];
      const retailLinePrev = emaR120[i - 1] * midAxis[i - 1];
      // ⚠️必须用当日累计 VWAP,不能用外面那个 avgLine ——
      // avgLine 在缺 d.avg 时会退化成 5 根均线,跟原式的 SUM(C*V)/SUM(V) 差得远,
      // 「加仓」会打在完全不相干的位置。
      const avgV = vwapArr[i];
      if (Number.isFinite(retailLine) && retailLine > avgV
        && close[i] > retailLine && close[i - 1] <= retailLinePrev && i - lastAdd >= 30) {
        signals.push({
          ...mkBase(i), level: 'A+', action: 'buy', score: 62,
          title: '加仓', reason: '价格上穿散户线且散户线站上均价线,追击确认',
        });
        lastAdd = i;
      }
    }
    signals.sort((a, b) => String(a.rawTime).localeCompare(String(b.rawTime)));
  }

  return signals;
}

// bingGeTLevels 兵哥做T的日内支撑/阻力(**当日定格值**,与通达信一致;跨日各自定格)。
// 阻力=L1+P1*7/8(绿),支撑=L1+P1*0.5/8(紫),H1/L1 含昨收。
//
// ⚠️2026-08-05 从"运行值"改为定格值,用户明确选了与通达信逐位一致:
// 原式 H1/L1 用 DYNAINFO(5)/(6)(当日最高/最低),盘后回看是收盘后的定值,
// 每根K线都按同一条水平线判 LONGCROSS —— 运行值会让早盘信号打在完全不同的位置。
// 代价同 CONST():**盘中实时时这两条线会随当日新高新低移动**,历史信号位置跟着变。
export function bingGeTLevels(data: KLineData[], preClose = 0): { support: number[]; resistance: number[] } {
  const support: number[] = new Array(data.length);
  const resistance: number[] = new Array(data.length);
  // 先按日分组算出各日最终高低,再整日回填同一个定值
  const dayRange = new Map<string, { hi: number; lo: number }>();
  for (const d of data) {
    const dp = String(d.time).slice(0, 10);
    const r = dayRange.get(dp) || { hi: -Infinity, lo: Infinity };
    r.hi = Math.max(r.hi, d.high || d.close);
    r.lo = Math.min(r.lo, d.low || d.close);
    dayRange.set(dp, r);
  }
  data.forEach((d, i) => {
    const r = dayRange.get(String(d.time).slice(0, 10))!;
    const h1 = preClose > 0 ? Math.max(preClose, r.hi) : r.hi;
    const l1 = preClose > 0 ? Math.min(preClose, r.lo) : r.lo;
    const p1 = h1 - l1;
    resistance[i] = l1 + p1 * 7 / 8;
    support[i] = l1 + p1 * 0.5 / 8;
  });
  return { support, resistance };
}

// ===== 兵哥做T · 主图全套视觉元素(与通达信公式逐段对应,2026-08-05)=====
//
// 通达信那份公式除了买卖点,还画了一堆东西:两组趋势线、底部的 ∠开始/黄棒/起涨 柱标、
// 上涨/加仓 文字、左下角机构买卖比、右侧资金流条。lightweight-charts 的 markers
// 画不了这些,全部算好坐标交给 overlay canvas 自绘。
// 口径与原式的对应关系逐条写在各段注释里;凡取不到的字段(流通股本 FINANCE(7))
// 因只影响比例不影响过零/交叉判断,按 1 处理,判断结果不变。

export interface BingGeTTrendSeg { i1: number; p1: number; i2: number; p2: number }
export interface BingGeTStick { i: number; yFrom: number; yTo: number; color: string; text?: string; textColor?: string }
export interface BingGeTText { i: number; y: number; text: string; color: string }
export interface BingGeTOverlay {
  trendDown: BingGeTTrendSeg[];  // 下降压力线(黄)
  trendUp: BingGeTTrendSeg[];    // 上涨支撑线(红)
  sticks: BingGeTStick[];        // ∠开始(紫)/黄棒/起涨(青) 底部柱标
  texts: BingGeTText[];          // 上涨/加仓 文字
  zhuli: number[];               // 主力线 = EXPMA(C/VBNH,60)×中间价轴(紫点线)
  vwap: number[];                // 当日累计均价线(红绿分色的分界)
  instBuyPct: number;            // 机构买%(大单口径:该分钟成交额>160万)
  instSellPct: number;
  flowBuyWan: number;            // 资金条:全量上涨分钟成交额合计(万)
  flowSellWan: number;
}

export function computeBingGeTOverlay(data: KLineData[]): BingGeTOverlay | null {
  const n = data.length;
  if (n < 30) return null;
  const close = data.map(d => d.close);
  const high = data.map(d => d.high || d.close);
  const low = data.map(d => d.low || d.close);
  const volume = data.map(d => d.volume || 0);

  // —— VBNH 比价基线(同信号计算:VWAP 带 ±5% 回退全程均线)——
  let cumPV = 0; let cumV = 0; let cumC = 0;
  const ratio: number[] = new Array(n);
  const vwap: number[] = new Array(n);
  for (let i = 0; i < n; i++) {
    cumPV += close[i] * volume[i];
    cumV += volume[i];
    cumC += close[i];
    const vw = cumV > 0 ? cumPV / cumV : close[i];
    vwap[i] = vw;
    const inBand = vw > 0 && close[i] / vw >= 0.95 && close[i] / vw <= 1.05;
    const vbnh = inBand ? vw : cumC / (i + 1);
    ratio[i] = vbnh > 0 ? close[i] / vbnh : 1;
  }
  const e20 = ema(ratio, 20);   // 起动线
  const e30 = ema(ratio, 30);
  const e60 = ema(ratio, 60);   // 主力线基
  const e120 = ema(ratio, 120); // 散户线基

  // —— 中间价轴 / 轴差:CONST(HHV/LLV(C,250)) → 当日收盘定格(用户选的通达信口径)——
  let dayHiC = -Infinity; let dayLoC = Infinity;
  for (const c of close) { dayHiC = Math.max(dayHiC, c); dayLoC = Math.min(dayLoC, c); }
  const span = dayHiC - dayLoC;                       // 轴差
  const midConst = (dayHiC + dayLoC) / 2 - span * 15 / 130; // 中间价轴
  const zhuli = e60.map(v => v * midConst);
  const retail = e120.map(v => v * midConst);          // 限制散户线

  // —— LIJINA 资金链(FINANCE(7) 流通股本取不到 → 按 1;只影响幅度不影响过零判断)——
  // LIJINA=加权换手×累计均价;LIJINA1=∑(涨分钟 LIJINA×V) − ∑(跌分钟 LIJINA×V)
  const lijina1: number[] = new Array(n);
  const lijin3: number[] = new Array(n);
  let accA = 0; let accB = 0; let upV = 0; let dnV = 0;
  for (let i = 0; i < n; i++) {
    const h0 = volume[i];
    const h1 = i >= 1 ? volume[i - 1] : 0;
    const h2 = i >= 2 ? volume[i - 2] : 0;
    const w = (h0 * 0.5 + h1 * 0.33 + h2 * 0.17) * (cumSum(close, i) / (i + 1));
    if (i >= 1 && close[i] > close[i - 1]) { accA += w * volume[i]; upV += volume[i]; }
    else if (i >= 1 && close[i] < close[i - 1]) { accB -= w * volume[i]; dnV += volume[i]; }
    lijina1[i] = accA + accB;
    lijin3[i] = upV - dnV;
  }

  // FILTER(X,N):首个信号后 N 根内屏蔽重复
  const filter = (cond: (i: number) => boolean, gap: number): number[] => {
    const out: number[] = [];
    let last = -Infinity;
    for (let i = 3; i < n; i++) {
      if (i - last < gap) continue;
      if (cond(i)) { out.push(i); last = i; }
    }
    return out;
  };
  const crossZero = (i: number) => lijina1[i] > 0 && lijina1[i - 1] <= 0;
  const llv3 = (i: number) => Math.min(volume[i], volume[i - 1] ?? Infinity, volume[i - 2] ?? Infinity);

  const sticks: BingGeTStick[] = [];
  const texts: BingGeTText[] = [];
  // ∠开始:CROSS(LIJINA1,0)&&C>REF(C,2)&&V>LLV(V,3)*3,FILTER 15,紫柱到 45% 轴差
  for (const i of filter(i => crossZero(i) && close[i] > close[i - 2] && volume[i] > llv3(i) * 3, 15)) {
    sticks.push({ i, yFrom: dayLoC, yTo: dayLoC + span * 0.45, color: '#e879f9', text: '∠开始', textColor: '#e879f9' });
  }
  // 黄棒:CROSS(LIJINA1,0)&&(C>REF(C,2)||(LIJIN3>0&&LIJIN3>LIJINA1)),FILTER 30,到 30%
  for (const i of filter(i => crossZero(i) && (close[i] > close[i - 2] || (lijin3[i] > 0 && lijin3[i] > lijina1[i])), 30)) {
    sticks.push({ i, yFrom: dayLoC, yTo: dayLoC + span * 0.30, color: '#facc15' });
  }
  // 起涨:CROSS(EXPMA30,EXPMA60),青柱到 15% + 黄字(原式 STICKLINE 无 FILTER,逐次都画)
  for (let i = 3; i < n; i++) {
    if (crossedUp(e30, e60, i)) {
      sticks.push({ i, yFrom: dayLoC, yTo: dayLoC + span * 0.15, color: '#22d3ee', text: '起涨', textColor: '#facc15' });
    }
  }
  // 上涨:FILTER(CROSS(起动线,1.01),30) 紫字贴底
  for (const i of filter(i => e20[i] > 1.01 && e20[i - 1] <= 1.01, 30)) {
    texts.push({ i, y: dayLoC, text: '上涨', color: '#e879f9' });
  }
  // 加仓:FILTER(散户线>均价线 && CROSS(C,散户线),30),黄字在均价线与散户线中间
  for (const i of filter(i => retail[i] > vwap[i] && close[i] > retail[i] && close[i - 1] <= retail[i - 1], 30)) {
    texts.push({ i, y: vwap[i] + (retail[i] - vwap[i]) / 2, text: '加仓', color: '#facc15' });
  }

  // —— 趋势线:DRAWLINE(cond1,p1,cond2,p2,1) → 最近 cond1 点连到下一个 cond2 点并右延 ——
  const hhv = (arr: number[], i: number, nWin: number) => { let m = -Infinity; for (let k = Math.max(0, i - nWin + 1); k <= i; k++) m = Math.max(m, arr[k]); return m; };
  const llv = (arr: number[], i: number, nWin: number) => { let m = Infinity; for (let k = Math.max(0, i - nWin + 1); k <= i; k++) m = Math.min(m, arr[k]); return m; };
  const pairSegs = (
    c1: (i: number) => boolean, p1f: (i: number) => number,
    c2: (i: number) => boolean, p2f: (i: number) => number,
  ): BingGeTTrendSeg[] => {
    const segs: BingGeTTrendSeg[] = [];
    let lastC1 = -1;
    for (let i = 5; i < n; i++) {
      if (lastC1 >= 0 && i > lastC1 && c2(i)) {
        segs.push({ i1: lastC1, p1: p1f(lastC1), i2: i, p2: p2f(i) });
        lastC1 = -1;
      }
      if (c1(i)) lastC1 = i;
    }
    return segs;
  };
  // 下降压力线(黄):HIGH>=HHV(H,30) 的高点 → LOW<=LLV(L,20) 处的 HHV(H,20)
  const trendDown = pairSegs(
    i => high[i] >= hhv(high, i, 30), i => high[i],
    i => low[i] <= llv(low, i, 20), i => hhv(high, i, 20),
  );
  // 上涨支撑线(红):LOW<=LLV(L,20) 的低点 → HIGH>=HHV(H,10) 处的 LLV(L,10)
  const trendUp = pairSegs(
    i => low[i] <= llv(low, i, 20), i => low[i],
    i => high[i] >= hhv(high, i, 10), i => llv(low, i, 10),
  );

  // —— 机构买卖比(大单:分钟成交额/8>20 即 >160万)与资金条(全量)——
  let instBuy = 0; let instSell = 0; let flowBuy = 0; let flowSell = 0;
  for (let i = 1; i < n; i++) {
    const amtWan = volume[i] * close[i] / 100; // V(手)×C/100 ≈ 万元,与原式同口径
    if (close[i] > close[i - 1]) flowBuy += amtWan;
    else if (close[i] < close[i - 1]) flowSell += amtWan;
    if (amtWan / 8 <= 20) continue;
    if (close[i] > close[i - 1]) instBuy += amtWan;
    else if (close[i] < close[i - 1]) instSell += amtWan;
  }
  const instTotal = instBuy + instSell;
  return {
    trendDown, trendUp, sticks, texts, zhuli, vwap,
    instBuyPct: instTotal > 0 ? Math.round(instBuy / instTotal * 100) : 0,
    instSellPct: instTotal > 0 ? Math.round(instSell / instTotal * 100) : 0,
    flowBuyWan: flowBuy, flowSellWan: flowSell,
  };
}

function cumSum(arr: number[], upto: number): number {
  let s = 0;
  for (let k = 0; k <= upto; k++) s += arr[k];
  return s;
}

export function getLatestTradingSignal(signals: TradingSignal[]): TradingSignal | null {
  for (let i = signals.length - 1; i >= 0; i--) {
    if (signals[i].action !== 'observe') return signals[i];
  }
  return signals[signals.length - 1] || null;
}

export function emptyTradingFlags(): TradingSignalFlags {
  return { ...EMPTY_FLAGS };
}
