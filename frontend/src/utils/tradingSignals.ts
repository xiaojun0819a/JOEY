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

export function calculateIntradayTradingSignals(data: KLineData[], preClose = 0, dayKData: KLineData[] = []): TradingSignal[] {
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

  return signals;
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
