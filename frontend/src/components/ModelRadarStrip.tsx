import React, { useMemo, useState } from 'react';
import { AlertTriangle, Filter, Flame, Layers3, RefreshCw, ShieldCheck, Target, TimerReset } from 'lucide-react';
import { F10Overview, KLineData, MarketIndex, Stock, StockValuation, TimePeriod } from '../types';
import { useTheme } from '../contexts/ThemeContext';
import {
  calculateIntradayTradingSignals,
  calculateTradingSignals,
  getLatestTradingSignal,
} from '../utils/tradingSignals';

interface ModelRadarStripProps {
  stock: Stock;
  kLineData: KLineData[];
  period: TimePeriod;
  panelHeight?: number;
  dayKLineData?: KLineData[];
  weekKLineData?: KLineData[];
  monthKLineData?: KLineData[];
  marketIndices?: MarketIndex[];
  f10Overview?: F10Overview | null;
  valuationSnapshot?: StockValuation | null;
  marketMessage?: string;
  marketStatusText?: string;
  marketStatusCode?: string;
  onForceSync?: () => void;
  syncing?: boolean;
}

type StatusTone = 'pass' | 'warn' | 'risk' | 'neutral';

type MarketCapBucket = {
  label: '微盘' | '小盘' | '中小盘' | '中盘' | '大盘' | '超大盘' | '未知';
  tone: StatusTone;
};

type CycleInsight = {
  name: '日' | '周' | '月';
  ready: boolean;
  bullish: boolean;
  tone: StatusTone;
  trendText: string;
  fresh: { tone: StatusTone; text: string };
};

type ScoreBreakdownItem = {
  key: string;
  label: string;
  value: number;
  detail: string;
};

function parseNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value !== 'string') return null;
  const raw = value.trim();
  if (!raw) return null;
  const matched = raw.replace(/,/g, '').match(/-?\d+(\.\d+)?/);
  if (!matched) return null;
  const num = Number(matched[0]);
  if (!Number.isFinite(num)) return null;
  return num;
}

function parseMarketCapYi(marketCap: string): number | null {
  const num = parseNumber(marketCap);
  if (num == null) return null;
  if (/万亿/.test(marketCap)) return num * 10000;
  if (/亿/.test(marketCap)) return num;
  if (/万/.test(marketCap)) return num / 10000;
  if (num > 1000000) return num / 100000000;
  return num;
}

function toMarketCapYi(value: unknown): number | null {
  if (value == null) return null;
  if (typeof value === 'string') return parseMarketCapYi(value);
  const num = parseNumber(value);
  if (num == null || num <= 0) return null;
  if (num >= 1000000) return num / 100000000;
  return num;
}

function parseLocalDateTime(raw: string): number | null {
  const text = raw.trim();
  if (!text) return null;

  const full = text.match(/^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$/);
  if (full) {
    const [, y, mo, d, hh = '00', mm = '00', ss = '00'] = full;
    const value = new Date(
      Number(y),
      Number(mo) - 1,
      Number(d),
      Number(hh),
      Number(mm),
      Number(ss),
      0,
    ).getTime();
    return Number.isFinite(value) ? value : null;
  }

  const hm = text.match(/^(\d{2}):(\d{2})(?::(\d{2}))?$/);
  if (hm) {
    const now = new Date();
    const [, hh, mm, ss = '00'] = hm;
    const value = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate(),
      Number(hh),
      Number(mm),
      Number(ss),
      0,
    ).getTime();
    return Number.isFinite(value) ? value : null;
  }
  return null;
}

function minutesAgo(ts: number | null): number | null {
  if (ts == null) return null;
  const diff = (Date.now() - ts) / 60000;
  if (!Number.isFinite(diff)) return null;
  return Math.max(0, diff);
}

function freshnessLabel(mins: number | null, freshThreshold: number, warmThreshold: number): { tone: StatusTone; text: string } {
  if (mins == null) return { tone: 'neutral', text: '待同步' };
  if (mins <= freshThreshold) return { tone: 'pass', text: `最新(${mins.toFixed(0)}m)` };
  if (mins <= warmThreshold) return { tone: 'warn', text: `稍旧(${mins.toFixed(0)}m)` };
  return { tone: 'risk', text: `过期(${mins.toFixed(0)}m)` };
}

function isLikelyTradingByClock(): boolean {
  const now = new Date();
  const day = now.getDay();
  if (day === 0 || day === 6) return false;
  const minutes = now.getHours() * 60 + now.getMinutes();
  const inMorning = minutes >= (9 * 60 + 15) && minutes <= (11 * 60 + 30);
  const inAfternoon = minutes >= (13 * 60) && minutes <= (15 * 60);
  return inMorning || inAfternoon;
}

function klineFreshnessLabel(
  mins: number | null,
  isIntraday: boolean,
  marketStatusCode?: string,
): { tone: StatusTone; text: string } {
  const status = (marketStatusCode || '').toLowerCase();
  const marketClosed = status
    ? status !== 'trading'
    : !isLikelyTradingByClock();
  if (!marketClosed) {
    return freshnessLabel(mins, isIntraday ? 6 : 1440, isIntraday ? 20 : 4320);
  }
  if (mins == null) return { tone: 'neutral', text: '待同步' };
  if (isIntraday) {
    if (mins <= 60 * 72) return { tone: 'pass', text: `休市最新(${mins.toFixed(0)}m)` };
    if (mins <= 60 * 24 * 7) return { tone: 'warn', text: `休市稍旧(${mins.toFixed(0)}m)` };
    return { tone: 'risk', text: `休市过期(${mins.toFixed(0)}m)` };
  }
  if (mins <= 60 * 24 * 7) return { tone: 'pass', text: `最新(${mins.toFixed(0)}m)` };
  if (mins <= 60 * 24 * 21) return { tone: 'warn', text: `稍旧(${mins.toFixed(0)}m)` };
  return { tone: 'risk', text: `过期(${mins.toFixed(0)}m)` };
}

function extractMetric(source: Record<string, unknown> | undefined, keys: string[]): number | null {
  if (!source) return null;
  const lowered = keys.map(k => k.toLowerCase());
  for (const [key, value] of Object.entries(source)) {
    const k = key.toLowerCase();
    if (!lowered.some(target => k.includes(target))) continue;
    const parsed = parseNumber(value);
    if (parsed != null) return parsed;
  }
  return null;
}

function extractMetricFromMany(
  sources: Array<Record<string, unknown> | undefined>,
  keys: string[],
  transform: (value: unknown) => number | null = parseNumber,
): number | null {
  const lowered = keys.map(k => k.toLowerCase());
  for (const source of sources) {
    if (!source) continue;
    for (const [key, value] of Object.entries(source)) {
      const keyLower = key.toLowerCase();
      if (!lowered.some(target => keyLower === target || keyLower.includes(target))) continue;
      const parsed = transform(value);
      if (parsed != null) return parsed;
    }
  }
  return null;
}

function simpleMA(data: KLineData[], period: number): number | null {
  if (data.length < period) return null;
  let sum = 0;
  for (let i = data.length - period; i < data.length; i++) sum += data[i].close;
  return sum / period;
}

function simpleMAAt(data: KLineData[], period: number, end: number): number | null {
  if (end < period - 1) return null;
  let sum = 0;
  for (let i = end - period + 1; i <= end; i++) sum += data[i].close;
  return sum / period;
}

function avgVolume(data: KLineData[], period: number): number | null {
  if (data.length < period) return null;
  let sum = 0;
  for (let i = data.length - period; i < data.length; i++) sum += data[i].volume || 0;
  return sum / period;
}

function evaluateCycle(name: '日' | '周' | '月', data: KLineData[]): CycleInsight {
  if (data.length < 25) {
    return {
      name,
      ready: false,
      bullish: false,
      tone: 'neutral',
      trendText: '待同步',
      fresh: { tone: 'neutral', text: '待同步' },
    };
  }
  const latest = data[data.length - 1];
  const ma10 = simpleMA(data, 10);
  const ma20 = simpleMA(data, 20);
  const prevMa20 = simpleMAAt(data, 20, data.length - 2);
  const slopeUp = ma20 != null && prevMa20 != null ? ma20 >= prevMa20 * 0.998 : false;
  const slopeDown = ma20 != null && prevMa20 != null ? ma20 <= prevMa20 * 0.995 : false;

  let bullish = false;
  let tone: StatusTone = 'warn';
  let trendText = '震荡';
  if (ma10 != null && ma20 != null) {
    bullish = latest.close > ma20 * 1.005 && ma10 > ma20 && slopeUp;
    const bearish = latest.close < ma20 * 0.99 || (ma10 < ma20 && slopeDown);
    if (bullish) {
      tone = 'pass';
      trendText = '多头';
    } else if (bearish) {
      tone = 'risk';
      trendText = '偏弱';
    }
  }

  const mins = minutesAgo(parseLocalDateTime(latest.time || ''));
  const fresh = name === '日'
    ? freshnessLabel(mins, 60 * 24 * 2, 60 * 24 * 6)
    : name === '周'
      ? freshnessLabel(mins, 60 * 24 * 10, 60 * 24 * 35)
      : freshnessLabel(mins, 60 * 24 * 45, 60 * 24 * 120);
  if (fresh.tone === 'risk' && tone === 'pass') {
    tone = 'warn';
  }

  return {
    name,
    ready: true,
    bullish,
    tone,
    trendText,
    fresh,
  };
}

function clampScore(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, Math.round(value)));
}

function formatTime(rawTime: string): string {
  if (rawTime.length > 16) return rawTime.slice(5, 16);
  if (rawTime.length > 10) return rawTime.slice(5);
  return rawTime;
}

function toneClasses(isDark: boolean, tone: StatusTone): string {
  if (tone === 'pass') {
    return isDark
      ? 'text-emerald-300 bg-emerald-500/10 border-emerald-500/25'
      : 'text-emerald-700 bg-emerald-100/80 border-emerald-300';
  }
  if (tone === 'warn') {
    return isDark
      ? 'text-amber-300 bg-amber-500/10 border-amber-500/25'
      : 'text-amber-700 bg-amber-100/80 border-amber-300';
  }
  if (tone === 'risk') {
    return isDark
      ? 'text-red-300 bg-red-500/10 border-red-500/25'
      : 'text-red-700 bg-red-100/80 border-red-300';
  }
  return isDark
    ? 'text-slate-300 bg-slate-500/10 border-slate-500/20'
    : 'text-slate-600 bg-slate-100/90 border-slate-300';
}

function classifyMarketCap(marketCapYi: number | null): MarketCapBucket {
  if (marketCapYi == null || !Number.isFinite(marketCapYi) || marketCapYi <= 0) {
    return { label: '未知', tone: 'neutral' };
  }
  if (marketCapYi < 50) {
    return { label: '微盘', tone: 'pass' };
  }
  if (marketCapYi <= 100) {
    return { label: '小盘', tone: 'pass' };
  }
  if (marketCapYi <= 300) {
    return { label: '中小盘', tone: 'warn' };
  }
  if (marketCapYi <= 800) {
    return { label: '中盘', tone: 'risk' };
  }
  if (marketCapYi <= 2000) {
    return { label: '大盘', tone: 'risk' };
  }
  return { label: '超大盘', tone: 'risk' };
}

const ModelRadarStrip: React.FC<ModelRadarStripProps> = ({
  stock,
  kLineData,
  period,
  panelHeight = 138,
  dayKLineData = [],
  weekKLineData = [],
  monthKLineData = [],
  marketIndices = [],
  f10Overview,
  valuationSnapshot,
  marketMessage = '',
  marketStatusText,
  marketStatusCode,
  onForceSync,
  syncing = false,
}) => {
  const { colors } = useTheme();
  const isDark = colors.isDark;
  const [showDetails, setShowDetails] = useState(false);
  const minPanelHeight = 72;
  const compactPanelHeight = 138;
  const maxPanelHeight = 560;
  const expandedPanelHeight = Math.max(minPanelHeight, Math.min(maxPanelHeight, panelHeight));
  const shouldExpandByHeight = expandedPanelHeight > compactPanelHeight + 4;
  const isExpanded = showDetails || shouldExpandByHeight;
  const renderedPanelHeight = showDetails
    ? Math.max(expandedPanelHeight, compactPanelHeight)
    : expandedPanelHeight;

  const computed = useMemo(() => {
    const isMinuteTrendPeriod = period === '1m' || period === '5d';
    const signals = isMinuteTrendPeriod
      ? calculateIntradayTradingSignals(kLineData, stock.preClose || 0)
      : calculateTradingSignals(kLineData);
    const latestSignal = getLatestTradingSignal(signals);
    const latestBar = kLineData[kLineData.length - 1];
    const scope = kLineData.slice(-120);
    const low = scope.length > 0 ? Math.min(...scope.map(k => k.low)) : null;
    const high = scope.length > 0 ? Math.max(...scope.map(k => k.high)) : null;
    const rangePos = low != null && high != null && high > low && latestBar
      ? (latestBar.close - low) / (high - low)
      : null;
    const marketCapFromQuote = parseMarketCapYi(stock.marketCap || '');
    const valuationCapYi = valuationSnapshot?.totalMarketCap
      ? valuationSnapshot.totalMarketCap / 100000000
      : null;
    const f10ValuationCapYi = f10Overview?.valuation?.totalMarketCap
      ? f10Overview.valuation.totalMarketCap / 100000000
      : null;

    const latestIndicators = f10Overview?.mainIndicators?.latest?.[0] as Record<string, unknown> | undefined;
    const operationsIndicators = f10Overview?.operations?.latestIndicators as Record<string, unknown> | undefined;
    const operationsIndicatorsExtra = f10Overview?.operations?.latestIndicatorsExtra as Record<string, unknown> | undefined;
    const operationsIndicatorsQuote = f10Overview?.operations?.latestIndicatorsQuote as Record<string, unknown> | undefined;
    const marketCapFromIndicators = extractMetricFromMany(
      [latestIndicators, operationsIndicators, operationsIndicatorsExtra, operationsIndicatorsQuote],
      ['TOTAL_MARKET_CAP', 'TOTALCAP', 'TOTAL_CAP', 'F116', '总市值', 'marketcap'],
      toMarketCapYi,
    );

    const totalShares = (
      valuationSnapshot?.totalShares
      || f10Overview?.valuation?.totalShares
      || extractMetricFromMany(
        [latestIndicators, operationsIndicators, operationsIndicatorsExtra, operationsIndicatorsQuote],
        ['TOTAL_SHARES', 'F84', '总股本', 'shares'],
      )
      || 0
    );
    const marketCapFromShares = totalShares > 0 && stock.price > 0
      ? (totalShares * stock.price) / 100000000
      : null;

    const marketCapYi = marketCapFromQuote ?? valuationCapYi ?? f10ValuationCapYi ?? marketCapFromIndicators ?? marketCapFromShares;
    const marketCapBucket = classifyMarketCap(marketCapYi);
    const isSmallCap = marketCapYi != null ? marketCapYi <= 100 : false;
    const isStName = /(ST|退)/i.test(stock.name || '');
    const company = (f10Overview?.company || {}) as Record<string, unknown>;
    const sector = String(
      stock.sector
      || company.industry
      || company.industryName
      || company.industryname
      || '',
    );
    const lowElastic = /(农业|农林|林业|建筑|公用事业|钢铁|煤炭)/.test(sector);
    const ma20 = simpleMA(kLineData, 20);
    const nearSafetyLine = latestBar && ma20 ? Math.abs(latestBar.close - ma20) / ma20 <= 0.03 : false;
    const latestBarTime = latestBar?.time || '';

    const klineTs = parseLocalDateTime(latestBarTime);
    const klineMins = minutesAgo(klineTs);
    const klineFresh = klineFreshnessLabel(klineMins, isMinuteTrendPeriod, marketStatusCode);

    const quoteFresh = stock.price > 0 && stock.volume > 0;

    const f10Ts = parseLocalDateTime(f10Overview?.updatedAt || '');
    const f10Mins = minutesAgo(f10Ts);
    const f10Fresh = freshnessLabel(f10Mins, 60 * 24 * 7, 60 * 24 * 30);

    const telegraphMatch = marketMessage.match(/^\[(\d{2}:\d{2}(?::\d{2})?)\]/);
    const telegraphTs = parseLocalDateTime(telegraphMatch?.[1] || '');
    const telegraphMins = minutesAgo(telegraphTs);
    const telegraphFresh = freshnessLabel(telegraphMins, 10, 60);

    const roe = extractMetric(latestIndicators, ['roe', '净资产收益率']);
    const profitYoY = extractMetric(latestIndicators, ['净利润同比', '归母净利润同比', 'profit']);
    const debtRatio = extractMetric(latestIndicators, ['资产负债率', 'debt']);
    let financeTone: StatusTone = 'neutral';
    let financeText = '待核验(F10)';
    if (roe != null || profitYoY != null || debtRatio != null) {
      const passROE = roe == null || roe >= 8;
      const passProfit = profitYoY == null || profitYoY >= 0;
      const passDebt = debtRatio == null || debtRatio <= 70;
      if (passROE && passProfit && passDebt) {
        financeTone = 'pass';
        financeText = '财务兜底通过';
      } else if ((roe != null && roe < 5) || (profitYoY != null && profitYoY < -20) || (debtRatio != null && debtRatio > 80)) {
        financeTone = 'risk';
        financeText = '财务存在雷点';
      } else {
        financeTone = 'warn';
        financeText = '财务中性偏谨慎';
      }
    }

    const controlScore = latestSignal?.flags.controlScore ?? null;
    const moneyFire = latestSignal?.flags.moneyFire ?? false;
    const gz = latestSignal?.flags.gz ?? false;
    const coreBuy = latestSignal?.flags.coreBuy ?? false;

    const dayCycle = evaluateCycle('日', dayKLineData.length > 0 ? dayKLineData : (period === '1d' ? kLineData : []));
    const weekCycle = evaluateCycle('周', weekKLineData.length > 0 ? weekKLineData : (period === '1w' ? kLineData : []));
    const monthCycle = evaluateCycle('月', monthKLineData.length > 0 ? monthKLineData : (period === '1mo' ? kLineData : []));
    const cycles = [dayCycle, weekCycle, monthCycle];
    const readyCycles = cycles.filter(c => c.ready);
    const bullishCycles = readyCycles.filter(c => c.bullish).length;

    let resonanceTone: StatusTone = 'neutral';
    let resonanceText = '多周期待同步';
    if (readyCycles.length >= 2) {
      if (bullishCycles === readyCycles.length) {
        resonanceTone = 'pass';
        resonanceText = '日周月同向共振';
      } else if (bullishCycles >= 2) {
        resonanceTone = 'warn';
        resonanceText = '多数周期共振';
      } else {
        resonanceTone = 'risk';
        resonanceText = '周期分歧，先控仓';
      }
    }

    const multiCycleTone: StatusTone = readyCycles.length === 3 ? 'pass' : readyCycles.length >= 2 ? 'warn' : 'risk';
    const multiCycleText = readyCycles.length === 3
      ? '完整'
      : readyCycles.length === 0
        ? '待同步'
        : `补数中(${readyCycles.length}/3)`;

    const dayForVolume = dayKLineData.length > 0 ? dayKLineData : (period === '1d' ? kLineData : []);
    const dayLast = dayForVolume[dayForVolume.length - 1];
    const dayMa20 = simpleMA(dayForVolume, 20);
    const dayVol5 = avgVolume(dayForVolume, 5);
    const dayVol20 = avgVolume(dayForVolume, 20);
    const dayPrev = dayForVolume[dayForVolume.length - 2];
    const dayPrevHigh20 = dayForVolume.length >= 21
      ? Math.max(...dayForVolume.slice(-21, -1).map(item => item.high))
      : null;
    const breakout = !!(dayLast && dayPrevHigh20 != null && dayVol5 != null && dayVol20 != null
      && dayLast.close > dayPrevHigh20
      && dayLast.volume > Math.max(dayVol5 * 1.25, dayVol20 * 1.1));
    const pullbackHold = !!(dayLast && dayMa20 != null && dayVol5 != null
      && dayLast.low <= dayMa20 * 1.01
      && dayLast.close >= dayMa20 * 0.99
      && dayLast.close <= dayMa20 * 1.03
      && dayLast.volume < dayVol5 * 0.92);
    const volumeDivergence = !!(dayLast && dayPrev && dayVol5 != null && dayForVolume.length >= 15
      && dayLast.close >= Math.max(...dayForVolume.slice(-15).map(item => item.close)) * 0.995
      && dayLast.close > dayPrev.close
      && dayLast.volume < dayVol5 * 0.75);

    let volumeTone: StatusTone = 'neutral';
    let volumeText = '量价待同步';
    if (dayForVolume.length >= 25) {
      if (breakout) {
        volumeTone = 'pass';
        volumeText = '放量突破确认';
      } else if (pullbackHold) {
        volumeTone = 'pass';
        volumeText = '缩量回踩承接';
      } else if (volumeDivergence) {
        volumeTone = 'risk';
        volumeText = '价强量弱，警惕背离';
      } else {
        volumeTone = 'warn';
        volumeText = '等待放量确认';
      }
    }

    const marketAvgChange = marketIndices.length > 0
      ? marketIndices.reduce((sum, item) => sum + (item.changePercent || 0), 0) / marketIndices.length
      : null;
    const marketUpCount = marketIndices.filter(item => item.changePercent >= 0).length;
    const marketDownCount = marketIndices.length - marketUpCount;
    let marketEnvTone: StatusTone = 'neutral';
    let marketEnvText = '大盘待同步';
    if (marketIndices.length > 0 && marketAvgChange != null) {
      if (marketUpCount >= Math.ceil(marketIndices.length * 0.6) && marketAvgChange > 0.15) {
        marketEnvTone = 'pass';
        marketEnvText = '市场环境偏强';
      } else if (marketDownCount >= Math.ceil(marketIndices.length * 0.6) && marketAvgChange < -0.2) {
        marketEnvTone = 'risk';
        marketEnvText = '市场环境偏弱';
      } else {
        marketEnvTone = 'warn';
        marketEnvText = '市场分化震荡';
      }
    }

    const relativeStrength = marketAvgChange == null ? null : (stock.changePercent || 0) - marketAvgChange;
    let linkageTone: StatusTone = 'neutral';
    let linkageText = '联动待同步';
    if (relativeStrength != null) {
      if ((stock.changePercent || 0) > 0 && relativeStrength >= 1.2) {
        linkageTone = 'pass';
        linkageText = '强于大盘，具备龙头弹性';
      } else if (relativeStrength >= 0) {
        linkageTone = 'warn';
        linkageText = '跟随市场，等待强化';
      } else {
        linkageTone = 'risk';
        linkageText = '弱于大盘，谨慎追高';
      }
    }

    const chgPct = stock.changePercent || 0;
    const riskHighChase = (rangePos != null && rangePos >= 0.8) || chgPct >= 7;
    const riskFastKill = kLineData.length >= 8
      ? latestBar.close <= kLineData[kLineData.length - 8].close * 0.9
      : false;

    const dayMa5 = simpleMA(dayForVolume, 5);
    const dayMa10 = simpleMA(dayForVolume, 10);
    const recentCloseHigh15 = dayForVolume.length >= 15
      ? Math.max(...dayForVolume.slice(-15).map(item => item.close))
      : null;
    const nearHighShrinkVolume = !!(
      dayLast && recentCloseHigh15 != null && dayVol5 != null
      && dayLast.close >= recentCloseHigh15 * 0.995
      && dayLast.volume < dayVol5 * 0.8
    );
    const breakMa5Hard = !!(
      dayLast && dayMa5 != null
      && dayLast.close < dayMa5 * 0.98
    );
    const pullbackStableForBuy = !!(
      dayLast && dayMa5 != null && dayMa10 != null
      && dayLast.low <= dayMa10 * 1.01
      && dayLast.close >= dayMa10 * 0.99
      && dayLast.close >= dayMa5 * 0.99
    );
    const nextVolumeConfirm = !!(
      dayLast && dayVol5 != null
      && dayLast.volume > dayVol5 * 1.5
    );

    const resonanceScore = readyCycles.length === 3 && bullishCycles === 3
      ? 20
      : bullishCycles >= 2
        ? 12
        : bullishCycles === 1
          ? 5
          : 0;
    const controlScorePart = controlScore == null ? 0 : Math.round((controlScore / 100) * 15);
    const relativeStrengthScore = relativeStrength == null
      ? 0
      : relativeStrength >= 1.2
        ? 10
        : relativeStrength >= 0
          ? 6
          : relativeStrength > -1
            ? 2
            : 0;
    const volumeVerifyScore = dayForVolume.length < 25
      ? 0
      : breakout
        ? 15
        : pullbackHold
          ? 12
          : volumeDivergence
            ? 6
            : (moneyFire || gz ? 8 : 5);
    const zonePenalty = rangePos == null
      ? 0
      : rangePos >= 0.8
        ? (nearSafetyLine ? -3 : -5)
        : 0;
    const marketPenalty = marketEnvTone === 'risk' ? -6 : marketEnvTone === 'warn' ? -3 : 0;
    const divergencePenalty = volumeDivergence ? -5 : 0;
    const freshnessPenalty = klineFresh.tone === 'risk' ? -8 : klineFresh.tone === 'warn' ? -3 : 0;
    const baseScore = 20;
    const compositeScore = clampScore(
      baseScore
      + resonanceScore
      + controlScorePart
      + relativeStrengthScore
      + volumeVerifyScore
      + zonePenalty
      + marketPenalty
      + divergencePenalty
      + freshnessPenalty,
    );

    const scoreBreakdown: ScoreBreakdownItem[] = [
      { key: 'res', label: '多周期共振', value: resonanceScore, detail: readyCycles.length >= 2 ? resonanceText : '周期待同步' },
      { key: 'ctl', label: '控盘度', value: controlScorePart, detail: controlScore == null ? '待同步' : `${controlScore.toFixed(0)}/100` },
      { key: 'rs', label: '相对强弱', value: relativeStrengthScore, detail: relativeStrength == null ? '待同步' : `${relativeStrength >= 0 ? '+' : ''}${relativeStrength.toFixed(2)}%` },
      { key: 'vol', label: '量价验证', value: volumeVerifyScore, detail: volumeText },
      { key: 'zone', label: '区间位置', value: zonePenalty, detail: rangePos == null ? '待同步' : rangePos >= 0.8 ? '高位区' : '非高位' },
      { key: 'mkt', label: '大盘环境', value: marketPenalty, detail: marketEnvText },
    ];

    const buyTriggerChecks = [
      { label: '缩量回踩 MA5/MA10 企稳', pass: pullbackStableForBuy },
      { label: '放量确认 > 1.5x 五日均量', pass: nextVolumeConfirm || breakout },
      { label: '大盘结构未破坏', pass: marketEnvTone !== 'risk' },
      { label: '综合评分 >= 70', pass: compositeScore >= 70 },
    ];
    const sellTriggerChecks = [
      { label: '高位量价背离', pass: volumeDivergence && (rangePos != null && rangePos >= 0.8) },
      { label: '创新高量能萎缩', pass: nearHighShrinkVolume },
      { label: '跌破 MA5 超过 2%', pass: breakMa5Hard },
      { label: '能量转负/风险信号', pass: (latestSignal?.action === 'sell') || (latestSignal?.action === 'reduce' && latestSignal.level === 'risk') || riskFastKill },
    ];
    const buyPassCount = buyTriggerChecks.filter(item => item.pass).length;
    const sellPassCount = sellTriggerChecks.filter(item => item.pass).length;
    const buyMissingText = buyTriggerChecks.filter(item => !item.pass).map(item => item.label).slice(0, 2).join('；');
    const sellHitText = sellTriggerChecks.filter(item => item.pass).map(item => item.label).slice(0, 2).join('；');
    const readinessChecks = [
      { label: '日K样本>=25', pass: dayForVolume.length >= 25 },
      { label: '周期数据>=2组', pass: readyCycles.length >= 2 },
      { label: '大盘指数已同步', pass: marketIndices.length > 0 },
      { label: 'K线未过期', pass: klineFresh.tone !== 'risk' },
    ];
    const dataReady = readinessChecks.every(item => item.pass);
    const dataMissingText = readinessChecks.filter(item => !item.pass).map(item => item.label).slice(0, 2).join('；');

    const buyReady = dataReady && buyPassCount === buyTriggerChecks.length;
    const sellReady = dataReady && (sellPassCount >= 2 || latestSignal?.action === 'sell');

    let scoreTone: StatusTone = 'risk';
    let scoreStateText = '风险减仓';
    if (!dataReady) {
      scoreTone = 'neutral';
      scoreStateText = '数据不足';
    } else if (compositeScore >= 70) {
      scoreTone = 'pass';
      scoreStateText = '买点提示';
    } else if (compositeScore >= 50) {
      scoreTone = 'warn';
      scoreStateText = '等待观察';
    }

    let actionText = '等待信号，暂不追高';
    let actionTone: StatusTone = scoreTone;
    if (!dataReady) {
      actionText = `数据不足：${dataMissingText || '等待同步完成'}`;
      actionTone = 'neutral';
    } else if (klineFresh.tone === 'risk') {
      actionText = 'K线数据较旧，等待同步后再决策';
      actionTone = 'risk';
    } else if (sellReady) {
      actionText = '卖点触发，优先减仓/清仓';
      actionTone = 'risk';
    } else if (buyReady) {
      actionText = '买点触发，可分批试仓';
      actionTone = 'pass';
    } else if (compositeScore >= 60 && buyPassCount >= 2) {
      actionText = '接近买点，等待放量确认';
      actionTone = 'warn';
    }

    let positionTone: StatusTone = 'neutral';
    let positionText = '建议仓位 20%-30%';
    if (!dataReady) {
      positionTone = 'neutral';
      positionText = '建议仓位 0%-10%，先等数据齐全';
    } else if (klineFresh.tone === 'risk' || readyCycles.length < 2 || compositeScore < 50) {
      positionTone = 'risk';
      positionText = '建议仓位 0%-20%，先补数据';
    } else if (sellReady || riskHighChase || riskFastKill || latestSignal?.action === 'sell') {
      positionTone = 'risk';
      positionText = '建议仓位 0%-10%，优先风控';
    } else if (latestSignal?.action === 'reduce' || compositeScore < 70) {
      positionTone = 'warn';
      positionText = '建议仓位 20%-40%，执行减仓';
    } else if (buyReady) {
      positionTone = 'pass';
      positionText = '建议仓位 30%-50%，分批试仓';
    }

    return {
      latestSignal,
      marketCapYi,
      marketCapBucket,
      isSmallCap,
      isStName,
      lowElastic,
      sector,
      financeTone,
      financeText,
      controlScore,
      moneyFire,
      gz,
      coreBuy,
      rangePos,
      nearSafetyLine,
      riskHighChase,
      riskFastKill,
      actionText,
      actionTone,
      quoteFresh,
      klineFresh,
      f10Fresh,
      telegraphFresh,
      dayCycle,
      weekCycle,
      monthCycle,
      resonanceTone,
      resonanceText,
      multiCycleTone,
      multiCycleText,
      breakout,
      pullbackHold,
      volumeDivergence,
      volumeTone,
      volumeText,
      linkageTone,
      linkageText,
      relativeStrength,
      marketEnvTone,
      marketEnvText,
      marketAvgChange,
      marketUpCount,
      marketDownCount,
      positionTone,
      positionText,
      compositeScore,
      scoreTone,
      scoreStateText,
      scoreBreakdown,
      buyTriggerChecks,
      sellTriggerChecks,
      buyReady,
      sellReady,
      buyPassCount,
      sellPassCount,
      buyMissingText,
      sellHitText,
      dataReady,
      dataMissingText,
      latestBarTime,
      latestTelegraph: marketMessage,
    };
  }, [period, kLineData, stock, f10Overview, valuationSnapshot, marketMessage, marketStatusCode, dayKLineData, weekKLineData, monthKLineData, marketIndices]);

  const stateTone = (ok: boolean, fallback: StatusTone = 'warn'): StatusTone => (ok ? 'pass' : fallback);

  return (
    <div className="px-2 py-1 border-b fin-divider-soft shrink-0">
      <div className="fin-panel-soft border fin-divider rounded-lg px-2.5 py-1.5">
        <div className="flex items-center justify-between mb-1">
          <div className="flex items-center gap-2 min-w-0">
            <Target className="h-4 w-4 text-accent-2 shrink-0" />
            <span className={`text-xs font-semibold tracking-wide ${isDark ? 'text-slate-100' : 'text-slate-700'}`}>
              波段模型驾驶舱
            </span>
            {computed.latestSignal && (
              <span className={`text-[10px] px-2 py-0.5 rounded border ${toneClasses(isDark, computed.latestSignal.level === 'risk' ? 'risk' : computed.latestSignal.action === 'buy' ? 'pass' : 'warn')}`}>
                {computed.latestSignal.title} · {formatTime(computed.latestSignal.rawTime)}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1 flex-wrap justify-end">
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.quoteFresh ? 'pass' : 'warn')}`}
              title="行情同步状态：基于当前盘口/报价是否已拿到有效价格与成交量。"
            >
              行情: {computed.quoteFresh ? '已同步' : '待同步'}
            </span>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.klineFresh.tone)}`}
              title="K线新鲜度：按最后一根K线时间与当前时间的分钟差计算。"
            >
              K线: {computed.klineFresh.text}
            </span>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.telegraphFresh.tone)}`}
              title="快讯新鲜度：按最新快讯时间与当前时间的分钟差计算。"
            >
              快讯: {computed.telegraphFresh.text}
            </span>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.f10Fresh.tone)}`}
              title="F10新鲜度：公司基本面摘要更新时间。"
            >
              F10: {computed.f10Fresh.text}
            </span>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.multiCycleTone)}`}
              title="多周期数据完整度：日/周/月K 是否同步齐全。"
            >
              多周期: {computed.multiCycleText}
            </span>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${toneClasses(isDark, marketStatusCode === 'trading' ? 'pass' : 'neutral')}`}
              title="市场状态：交易中/午休/收盘/节假日休市。"
            >
              市场: {marketStatusText || '未知'}
            </span>
            <button
              type="button"
              onClick={() => setShowDetails(prev => !prev)}
              className={`text-[10px] px-1.5 py-0.5 rounded border ${
                isDark
                  ? 'text-slate-200 border-slate-500/40 bg-slate-500/10 hover:bg-slate-500/20'
                  : 'text-slate-700 border-slate-300 bg-slate-100/80 hover:bg-slate-200/80'
              }`}
              title={isExpanded ? '收起模型详情，扩大K线区域' : '展开模型详情'}
            >
              {isExpanded ? '收起详情' : '展开详情'}
            </button>
            {onForceSync && (
              <button
                type="button"
                onClick={onForceSync}
                className={`text-[10px] px-1.5 py-0.5 rounded border flex items-center gap-1 ${
                  isDark
                    ? 'text-slate-200 border-slate-500/40 bg-slate-500/10 hover:bg-slate-500/20'
                    : 'text-slate-700 border-slate-300 bg-slate-100/80 hover:bg-slate-200/80'
                }`}
                disabled={syncing}
                title="强制同步最新行情与K线"
              >
                <RefreshCw className={`h-3 w-3 ${syncing ? 'animate-spin' : ''}`} />
                {syncing ? '同步中' : '同步最新'}
              </button>
            )}
          </div>
        </div>

        <div
          className="overflow-y-auto pr-1 fin-scrollbar"
          style={{ maxHeight: renderedPanelHeight }}
        >
          <div className="grid grid-cols-1 xl:grid-cols-5 lg:grid-cols-3 md:grid-cols-2 gap-1.5 text-[10px] leading-tight items-start">
          <section className={`self-start min-w-0 rounded border fin-divider px-1.5 py-1 ${isDark ? 'bg-slate-900/25' : 'bg-white/60'}`}>
            <div className="flex items-center gap-1 mb-1">
              <Filter className="h-3.5 w-3.5 text-accent-2" />
              <span className="font-semibold" title="先做风险排除与目标池过滤：市值、ST、板块弹性、财务兜底。">1. 漏斗初筛</span>
            </div>
            <div className="flex flex-wrap gap-1">
              <span
                className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.marketCapBucket.tone)}`}
                title="市值分层：<50亿微盘，50-100亿小盘，100-300亿中小盘，300-800亿中盘，800-2000亿大盘，>2000亿超大盘。"
              >
                {computed.marketCapBucket.label}: {computed.marketCapYi != null ? `${computed.marketCapYi.toFixed(1)}亿` : '未知'}
              </span>
              <span
                className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, stateTone(!computed.isStName, 'risk'))}`}
                title="ST排雷：名称含 ST/退 视为高风险，直接判为未通过。"
              >
                ST排雷: {computed.isStName ? '未通过' : '通过'}
              </span>
              <span
                className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, stateTone(!computed.lowElastic))}`}
                title="板块弹性：农业/林业/建筑/公用事业/钢铁/煤炭等被视为低弹性，短线爆发力偏弱。"
              >
                板块弹性: {computed.sector ? (computed.lowElastic ? '偏低' : '正常') : '待同步'}
              </span>
              <span
                className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.financeTone)}`}
                title="财务兜底：参考ROE、利润同比、资产负债率做基础排雷，不作为唯一买卖依据。"
              >
                {computed.financeText}
              </span>
            </div>
          </section>

          <section className={`self-start min-w-0 rounded border fin-divider px-1.5 py-1 ${isDark ? 'bg-slate-900/25' : 'bg-white/60'}`}>
            <div className="flex items-center gap-1 mb-1">
              <Layers3 className="h-3.5 w-3.5 text-accent-2" />
              <span className="font-semibold" title="日/周/月趋势同向性。至少2个周期同步向上，才更容易出现高胜率波段。">2. 多周期共振</span>
            </div>
            <div className="flex flex-wrap gap-1 mb-0.5">
              {[computed.dayCycle, computed.weekCycle, computed.monthCycle].map(cycle => (
                <span
                  key={cycle.name}
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, cycle.tone)}`}
                  title={`${cycle.name}K趋势与新鲜度：${cycle.trendText} / ${cycle.fresh.text}`}
                >
                  {cycle.name}: {cycle.trendText}
                </span>
              ))}
            </div>
            <div className={`px-1.5 py-1 rounded border ${toneClasses(isDark, computed.resonanceTone)}`} title="共振结论：周期越一致，右侧拐点成功率通常越高。">
              {computed.resonanceText}
            </div>
          </section>

          <section className={`self-start min-w-0 rounded border fin-divider px-1.5 py-1 ${isDark ? 'bg-slate-900/25' : 'bg-white/60'}`}>
            <div className="flex items-center gap-1 mb-1">
              <Flame className="h-3.5 w-3.5 text-accent-2" />
              <span className="font-semibold" title="量价结构评分：控盘、放量突破、缩量回踩、价量背离。">3. 量价验证</span>
            </div>
            <div className="space-y-0.5">
              <div className="flex items-center justify-between">
                <span
                  className={isDark ? 'text-slate-400' : 'text-slate-500'}
                  title="控盘度(0-100)：分数越高，表示趋势与量价结构越强；低分代表当下结构偏弱。"
                >
                  控盘度
                </span>
                {computed.controlScore == null ? (
                  <span className={isDark ? 'text-slate-300' : 'text-slate-600'}>待同步</span>
                ) : (
                  <span
                    className={`font-mono ${computed.controlScore >= 70 ? 'text-emerald-400' : computed.controlScore >= 55 ? 'text-amber-400' : isDark ? 'text-slate-300' : 'text-slate-600'}`}
                    title={`当前控盘度 ${computed.controlScore.toFixed(0)}。一般70+偏强，55-70中性，55以下偏弱。`}
                  >
                    {computed.controlScore.toFixed(0)}/100
                  </span>
                )}
              </div>
              <div className={`h-1 rounded-full overflow-hidden ${isDark ? 'bg-slate-700/70' : 'bg-slate-200'}`}>
                <div
                  className={`h-full ${
                    (computed.controlScore ?? 0) >= 70 ? 'bg-emerald-500' :
                    (computed.controlScore ?? 0) >= 55 ? 'bg-amber-500' : 'bg-slate-500'
                  }`}
                  style={{ width: `${computed.controlScore ?? 0}%` }}
                />
              </div>
              <div className="flex flex-wrap gap-1">
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, stateTone(computed.moneyFire, 'neutral'))}`}
                  title="资金点火：通常要求突破关键高点并放量，代表短线启动迹象。"
                >
                  资金点火
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, stateTone(computed.gz, 'neutral'))}`}
                  title="共振：短中周期趋势方向一致，且量价能量没有明显衰减。"
                >
                  共振
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, stateTone(computed.coreBuy, 'neutral'))}`}
                  title="核心买点：吃鱼身 + 资金点火 + 共振 + 趋势强/持股 同时满足。"
                >
                  核心买点
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.breakout ? 'pass' : 'neutral')}`}
                  title="放量突破：站上近20日高点且成交量明显放大。"
                >
                  放量突破
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.pullbackHold ? 'pass' : 'neutral')}`}
                  title="缩量回踩：回踩MA20附近且量能收缩，代表抛压减弱。"
                >
                  缩量回踩
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.volumeDivergence ? 'risk' : 'neutral')}`}
                  title="量价背离：股价走强但成交量无法配合，需防冲高回落。"
                >
                  量价背离
                </span>
              </div>
              <div className={`px-1.5 py-1 rounded border ${toneClasses(isDark, computed.volumeTone)}`} title="量价结论。">
                {computed.volumeText}
              </div>
            </div>
          </section>

          <section className={`self-start min-w-0 rounded border fin-divider px-1.5 py-1 ${isDark ? 'bg-slate-900/25' : 'bg-white/60'}`}>
            <div className="flex items-center gap-1 mb-1">
              <TimerReset className="h-3.5 w-3.5 text-accent-2" />
              <span className="font-semibold" title="板块联动 + 大盘过滤：优先做强于指数的个股。">4. 联动环境</span>
            </div>
            <div className="space-y-0.5">
              <div className="flex items-center justify-between">
                <span className={isDark ? 'text-slate-400' : 'text-slate-500'} title="当前标的所属板块，仅作为联动参考。">所属板块</span>
                <span className={isDark ? 'text-slate-200' : 'text-slate-700'}>
                  {computed.sector || '待同步'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className={isDark ? 'text-slate-400' : 'text-slate-500'} title="相对强弱=个股涨跌幅-指数平均涨跌幅。">相对强弱</span>
                {computed.relativeStrength == null ? (
                  <span className={isDark ? 'text-slate-300' : 'text-slate-600'}>待同步</span>
                ) : (
                  <span className={computed.relativeStrength >= 0 ? 'text-emerald-400' : 'text-red-400'}>
                    {computed.relativeStrength >= 0 ? '+' : ''}{computed.relativeStrength.toFixed(2)}%
                  </span>
                )}
              </div>
              <div
                className={`px-1.5 py-1 rounded border ${toneClasses(isDark, computed.linkageTone)}`}
                title="联动结论：优先做强于大盘且板块活跃标的。"
              >
                {computed.linkageText}
              </div>
              <div
                className={`px-1.5 py-1 rounded border ${toneClasses(isDark, computed.marketEnvTone)}`}
                title="市场过滤：指数环境弱时，系统会自动收缩仓位建议。"
              >
                {computed.marketEnvText}
                {computed.marketAvgChange != null ? `（指数均值 ${computed.marketAvgChange >= 0 ? '+' : ''}${computed.marketAvgChange.toFixed(2)}%）` : ''}
              </div>
            </div>
          </section>

          <section className={`self-start min-w-0 rounded border fin-divider px-1.5 py-1 ${isDark ? 'bg-slate-900/25' : 'bg-white/60'}`}>
            <div className="flex items-center gap-1 mb-1">
              <ShieldCheck className="h-3.5 w-3.5 text-accent-2" />
              <span className="font-semibold" title="只做拐点，机械执行：买点试仓、卖点减仓、风险清仓。">5. 择时与仓位</span>
            </div>
            <div className="space-y-1">
              <div className="flex items-center justify-between gap-2">
                <span className={isDark ? 'text-slate-400' : 'text-slate-500'}>综合评分</span>
                <span className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.scoreTone)}`}>
                  {computed.compositeScore}/100 · {computed.scoreStateText}
                </span>
              </div>
              <div className={`h-1.5 rounded-full overflow-hidden ${isDark ? 'bg-slate-700/70' : 'bg-slate-200'}`}>
                <div
                  className={`h-full ${computed.compositeScore >= 70 ? 'bg-emerald-500' : computed.compositeScore >= 50 ? 'bg-amber-500' : 'bg-red-500'}`}
                  style={{ width: `${computed.compositeScore}%` }}
                />
              </div>
              <div className="grid grid-cols-2 gap-1">
                {computed.scoreBreakdown.map(item => (
                  <span
                    key={item.key}
                    className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, item.value >= 0 ? (item.value > 0 ? 'pass' : 'neutral') : 'risk')}`}
                    title={`${item.label}：${item.detail}`}
                  >
                    {item.label}: {item.value >= 0 ? '+' : ''}{item.value}
                  </span>
                ))}
              </div>
              <div className="flex flex-wrap gap-1">
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.buyReady ? 'pass' : 'warn')}`}
                  title="买点触发条件：缩量回踩企稳 / 放量确认 / 大盘结构未破坏 / 评分>=70。"
                >
                  买点条件: {computed.buyPassCount}/4
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.sellReady ? 'risk' : 'neutral')}`}
                  title="卖点触发条件：高位背离 / 创新高缩量 / 跌破MA5 2% / 能量转负。"
                >
                  卖点条件: {computed.sellPassCount}/4
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.actionTone)}`}
                  title="纪律动作：观望/试仓/减仓/清仓。"
                >
                  动作: {computed.actionText}
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.positionTone)}`}
                  title="仓位建议会跟随数据完整度、多周期共振和市场环境动态调整。"
                >
                  {computed.positionText}
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.rangePos != null && computed.rangePos <= 0.35 ? 'pass' : computed.rangePos != null && computed.rangePos >= 0.8 ? 'risk' : 'warn')}`}
                  title="近120根K线的价格区间位置。"
                >
                  区间: {computed.rangePos == null ? '--' : computed.rangePos <= 0.35 ? '底部区' : computed.rangePos >= 0.8 ? '高位区' : '中位区'}
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.nearSafetyLine ? 'pass' : 'warn')}`}
                  title="黄色安全线(MA20)附近通常回撤可控，偏离大时需要防追高。"
                >
                  安全线: {computed.nearSafetyLine ? '贴近' : '偏离'}
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.riskHighChase ? 'risk' : 'pass')}`}
                  title="高位追涨风险：区间位置过高或当日涨幅过大时，默认禁止追涨。"
                >
                  高位追涨: {computed.riskHighChase ? '禁止' : '可控'}
                </span>
                <span
                  className={`px-1.5 py-0.5 rounded border ${toneClasses(isDark, computed.riskFastKill ? 'risk' : 'neutral')}`}
                  title="A杀风险：短期快速回撤是否达到警戒阈值。"
                >
                  A杀风险: {computed.riskFastKill ? '升高' : '一般'}
                </span>
              </div>
              <div className={`text-[10px] ${isDark ? 'text-slate-400' : 'text-slate-500'}`}>
                {!computed.dataReady
                  ? `数据未就绪：${computed.dataMissingText || '等待同步'}`
                  : computed.buyReady
                  ? '买点已满足：可执行分批试仓。'
                  : `买点未满足：${computed.buyMissingText || '等待更多条件确认'}`}
              </div>
              <div className={`text-[10px] ${isDark ? 'text-slate-400' : 'text-slate-500'}`}>
                {!computed.dataReady
                  ? '卖点判断暂缓：先等数据同步完成。'
                  : computed.sellReady
                  ? `卖点已触发：${computed.sellHitText || '风险信号已触发'}`
                  : '卖点未触发：继续跟踪量价与趋势。'}
              </div>
            </div>
            <div
              className={`mt-0.5 flex items-center gap-1 ${isDark ? 'text-slate-400' : 'text-slate-500'}`}
              title="快讯只用于情绪辅助，不单独作为买卖依据。"
            >
              <AlertTriangle className="h-3 w-3 shrink-0" />
              <span className="line-clamp-1">
                {computed.latestTelegraph ? `快讯源: ${computed.latestTelegraph.replace(/^\[[^\]]+\]\s*/, '').slice(0, 26)}...` : '快讯未同步，谨慎解读高位情绪。'}
              </span>
            </div>
          </section>
          </div>
        </div>
      </div>
    </div>
  );
};

export default ModelRadarStrip;
