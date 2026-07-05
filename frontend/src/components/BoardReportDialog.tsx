import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Bot, FileText, Loader2, RefreshCw, Send, X } from 'lucide-react';
import { NodeRenderer } from 'markstream-react';
import type { F10Overview, KLineData, MarketIndex, Stock, StockValuation, TimePeriod } from '../types';
import ModelRadarStrip from './ModelRadarStrip';
import { askBoardReport, generateBoardReport, getCachedBoardReport } from '../services/boardReportService';
import {
  buildOpenEatFishSeries,
  buildVipAnomalySeries,
  buildVipFiveDragonSeries,
  buildVipShortEnergySeries,
  type VipFiveDragonPoint,
} from './StockChartLW';
import { useTheme } from '../contexts/ThemeContext';
import 'markstream-react/index.css';

interface BoardReportDialogProps {
  isOpen: boolean;
  onClose: () => void;
  stock?: Stock;
  data: KLineData[];
  period: TimePeriod;
  // 波段模型驾驶舱所需(与主页驾驶舱同源同显),全部可选
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

type Tone = 'good' | 'warn' | 'bad' | 'neutral' | 'hot';

type ReportSection = {
  title: string;
  status: string;
  tone: Tone;
  lines: string[];
};

type ReportMetric = {
  label: string;
  value: string;
  tone?: Tone;
};

type BoardReport = {
  valid: boolean;
  score: number;
  stance: string;
  stanceTone: Tone;
  tradeDate: string;
  summary: string;
  nextAction: string;
  metrics: ReportMetric[];
  nextSteps: string[];
  risks: string[];
  sections: ReportSection[];
  process: string[];
};

type QAItem = {
  question: string;
  answer: string;
  modelName?: string;
  createdAt: number;
  error?: string;
};

const PERIOD_LABELS: Record<TimePeriod, string> = {
  '1m': '分时',
  '5d': '5日',
  '1d': '日K',
  '1w': '周K',
  '1mo': '月K',
};

const cleanNumber = (value: number | undefined | null): number | null => (
  typeof value === 'number' && Number.isFinite(value) ? value : null
);

const fmtPrice = (value: number | undefined | null): string => {
  const n = cleanNumber(value);
  return n == null ? '--' : n.toFixed(2);
};

const fmtPct = (value: number | undefined | null, signed = true): string => {
  const n = cleanNumber(value);
  if (n == null) return '--';
  const sign = signed && n > 0 ? '+' : '';
  return `${sign}${n.toFixed(2)}%`;
};

const pctChange = (from: number | undefined, to: number | undefined): number => {
  if (!from || !to || !Number.isFinite(from) || !Number.isFinite(to) || from === 0) return 0;
  return (to / from - 1) * 100;
};

const clamp = (value: number, min: number, max: number): number => Math.max(min, Math.min(max, value));
const round1 = (value: number): number => Math.round(value * 10) / 10;

const countRecent = <T,>(items: T[], bars: number, predicate: (item: T) => boolean): number => {
  const slice = items.slice(Math.max(0, items.length - bars));
  return slice.reduce((sum, item) => sum + (predicate(item) ? 1 : 0), 0);
};

const lastSignalText = <T extends { rawTime: string }>(items: T[], predicate: (item: T) => boolean): string => {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) return items[index].rawTime.slice(0, 10);
  }
  return '近段未见';
};

const formatVolumeRatio = (latest: KLineData | undefined, data: KLineData[]): string => {
  if (!latest || data.length < 6) return '--';
  const base = data.slice(-6, -1).map(item => item.volume || 0).filter(value => value > 0);
  if (base.length === 0) return '--';
  const avg = base.reduce((sum, value) => sum + value, 0) / base.length;
  return avg > 0 ? `${(latest.volume / avg).toFixed(2)}x` : '--';
};

const countBullDimensions = (point?: VipFiveDragonPoint): number => {
  if (!point) return 0;
  return [point.buySignal, point.trendBull, point.energyBull, point.midBull, point.shortBull].filter(Boolean).length;
};

const controlLabel = (point?: VipFiveDragonPoint): string => {
  if (!point) return '控盘未知';
  if (point.highControl > 0 || point.controlDegree >= 80) return '高控盘';
  if (point.midControl > 0 || point.controlDegree >= 60) return '中控盘';
  if (point.lowControl > 0 || point.controlDegree >= 50) return '低控盘';
  return '控盘偏弱';
};

const scoreWaveV1Like = (
  main: ReturnType<typeof buildOpenEatFishSeries>['latest'],
  anomaly: ReturnType<typeof buildVipAnomalySeries>['latest'],
  short: ReturnType<typeof buildVipShortEnergySeries>['latest'],
  dragon: ReturnType<typeof buildVipFiveDragonSeries>['latest'],
  recentRelaxed: boolean,
  recentStrict: boolean,
): number => {
  let score = 24;
  let firstScore = 0;
  if (main?.openEatFish) firstScore = 18;
  else if (main?.eatFish) firstScore = 14;
  else if (anomaly?.anomaly) firstScore = 10;
  else if (recentRelaxed) firstScore = 5;

  let secondScore = 0;
  if (short?.anomaly) secondScore += 8;
  else if (recentStrict) secondScore += 4;
  if (short?.strongCondition) {
    if ((short.strongCount || 0) >= 3) secondScore += 10;
    else if ((short.strongCount || 0) === 2) secondScore += 7;
    else secondScore += 4;
  }
  if (short?.startControl) secondScore += 4;
  if (short?.strongCondition && (short.strongCount || 0) >= 2) secondScore += 3;
  secondScore = clamp(secondScore, 0, 22);

  const redLights = countBullDimensions(dragon);
  let lampScore = redLights * 3;
  if (dragon?.resonance) lampScore += 5;
  lampScore = clamp(lampScore, 0, 20);

  const kongpanScore = clamp(((dragon?.controlDegree || 50) - 50) * 0.45, 0, 18);

  score += firstScore + secondScore + lampScore + kongpanScore;
  if (short?.mainShip) score -= 12;
  if (main?.takeProfit) score -= 18;
  if (main?.breakTakeProfit) score -= 16;
  if (!dragon?.energyBull) score -= 4;
  if (!dragon?.trendBull) score -= 4;

  return round1(clamp(score, 0, 100));
};

const buildBoardReport = (stock: Stock | undefined, data: KLineData[]): BoardReport => {
  const safeData = data.filter(item => (
    Number.isFinite(item.close)
    && Number.isFinite(item.high)
    && Number.isFinite(item.low)
    && Number.isFinite(item.open)
  ));

  if (safeData.length === 0) {
    return {
      valid: false,
      score: 0,
      stance: '暂无数据',
      stanceTone: 'neutral',
      tradeDate: '--',
      summary: '当前没有可用于生成看板报告的K线数据。',
      nextAction: '等待K线数据加载完成后再判断。',
      metrics: [],
      nextSteps: [],
      risks: [],
      sections: [],
      process: [],
    };
  }

  const latestK = safeData[safeData.length - 1];
  const prevK = safeData[safeData.length - 2];
  const mainSeries = buildOpenEatFishSeries(safeData);
  const anomalySeries = buildVipAnomalySeries(safeData);
  const shortSeries = buildVipShortEnergySeries(safeData);
  const dragonSeries = buildVipFiveDragonSeries(safeData);
  const main = mainSeries.latest;
  const anomaly = anomalySeries.latest;
  const short = shortSeries.latest;
  const dragon = dragonSeries.latest;

  const prevClose = prevK?.close || stock?.preClose || 0;
  const dayPct = pctChange(prevClose, latestK.close);
  const run5Pct = safeData.length > 5 ? pctChange(safeData[safeData.length - 6].close, latestK.close) : 0;
  const run20Pct = safeData.length > 20 ? pctChange(safeData[safeData.length - 21].close, latestK.close) : 0;
  const volumeRatio = formatVolumeRatio(latestK, safeData);
  const bullDims = countBullDimensions(dragon);
  const recentEatFish = countRecent(mainSeries.points, 20, point => point.eatFish);
  const recentOpenEatFish = countRecent(mainSeries.points, 40, point => point.openEatFish);
  const recentAnomaly = countRecent(anomalySeries.points, 20, point => point.anomaly);
  const recentFire = countRecent(anomalySeries.points, 20, point => point.fire);
  const recentStrictAnomaly = countRecent(shortSeries.points, 20, point => point.anomaly);
  const recentRelaxed10 = countRecent(anomalySeries.points, 10, point => point.anomaly) > 0;
  const recentStrict10 = countRecent(shortSeries.points, 10, point => point.anomaly) > 0;
  const recentMainShip = countRecent(shortSeries.points, 10, point => point.mainShip);
  const ma5 = cleanNumber(main?.ma5);
  const ma10 = cleanNumber(main?.ma10);
  const ma20 = cleanNumber(main?.ma20);
  const aboveMa5 = ma5 != null && latestK.close >= ma5;
  const aboveMa10 = ma10 != null && latestK.close >= ma10;
  const maStack = ma5 != null && ma10 != null && ma20 != null && ma5 >= ma10 && ma10 >= ma20;
  const extended = (ma5 != null && latestK.close > ma5 * 1.075) || dayPct >= 7 || run5Pct >= 18;
  const hotButTired = Boolean(main?.takeProfit || short?.mainShip || short?.macdDead || recentMainShip > 0);
  const controlRising = short ? short.controlScaled > 0 : false;
  const score = scoreWaveV1Like(main, anomaly, short, dragon, recentRelaxed10, recentStrict10);
  const weakStructure = !aboveMa10 || bullDims <= 2 || Boolean(!dragon?.buySignal);
  const strongConfirm = Boolean(
    main?.strong
    && (main.eatFish || recentEatFish > 0)
    && (anomaly?.anomaly || anomaly?.fire || recentAnomaly > 0)
    && controlRising
    && bullDims >= 4
  );

  let stance = '谨慎观望';
  let stanceTone: Tone = 'warn';
  let nextAction = '明天先观察承接，暂不主动追入。';
  if (hotButTired || (weakStructure && score < 58)) {
    stance = '先防回落';
    stanceTone = 'bad';
    nextAction = '不建议开盘直接入手，先看能否修复止盈/减仓/死叉风险。';
  } else if (score >= 78 && strongConfirm) {
    stance = extended ? '强势等分歧' : '强势可试错';
    stanceTone = extended ? 'hot' : 'good';
    nextAction = extended
      ? '趋势和资金较强，但短线偏热，明天不追高，等回踩不破关键位再考虑。'
      : '可按小仓试错处理，要求分时承接和四图共振继续保持。';
  } else if (score >= 64) {
    stance = '等确认低吸';
    stanceTone = 'warn';
    nextAction = '只做回踩确认或放量站回，不适合无条件追涨。';
  } else if (score >= 50) {
    stance = '观察为主';
    stanceTone = 'neutral';
    nextAction = '信号不够完整，明天以观察为主，等异动或五维共振补齐。';
  } else {
    stance = '回避追高';
    stanceTone = 'bad';
    nextAction = '当前看板偏弱或风险信号偏多，明天不建议入手。';
  }

  const support1 = ma5 ?? latestK.low;
  const support2 = ma10 ?? prevK?.low ?? latestK.low;
  const previousHigh = prevK?.high ?? latestK.high;
  const resistance = Math.max(previousHigh, latestK.high);
  const hardStop = Math.min(support2, prevK?.low ?? support2);
  const stRisk = /\bST\b|\*ST|ST/.test(`${stock?.name || ''}${stock?.symbol || ''}`);

  const nextSteps = [
    `触发条件：回踩 ${fmtPrice(support1)} 附近不破并重新放量站稳，或放量突破 ${fmtPrice(resistance)} 后不回落。`,
    `仓位纪律：强共振也按小仓试错，确认后再加；若高开超过3%到5%且分时走弱，不追。`,
    `防守线：跌破 ${fmtPrice(hardStop)} 或跌回MA10 ${fmtPrice(ma10)} 下方，先减仓/止损。`,
    `盘中验证：短线能量不能出现“减仓/死叉”，五维至少保持趋势、量能、中期、短期中3项为红。`,
  ];

  const risks = [
    extended ? `短线偏热：最新涨幅 ${fmtPct(dayPct)}，5日涨幅 ${fmtPct(run5Pct)}，开盘追高容易吃分歧。` : '',
    main?.takeProfit ? '主图出现“及时止盈”，说明已有回落/放量跌破风险信号。' : '',
    short?.mainShip ? '短线能量出现“减仓”，控盘仍为正但动能开始降温。' : '',
    short?.macdDead ? 'MACD出现死叉，短线趋势切换要谨慎。' : '',
    dragon && !dragon.buySignal ? '五维擒龙显示“卖”，多因子方向没有站在多头一侧。' : '',
    stRisk ? '股票名称/代码包含ST特征，波动和退市风险需要单独收紧仓位。' : '',
  ].filter(Boolean) as string[];

  const mainStatus = main?.takeProfit
    ? '有止盈风险'
    : main?.openEatFish
      ? '开仓吃鱼'
      : main?.eatFish
        ? '吃鱼身'
        : main?.strong
          ? '趋势偏强'
          : '趋势偏弱';

  const anomalyStatus = anomaly?.fire
    ? '资金点火'
    : anomaly?.anomaly
      ? '异动'
      : anomaly?.eatFish
        ? '吃鱼身同步'
        : '未见当日异动';

  const shortStatus = short?.mainShip
    ? '减仓降温'
    : short?.startControl
      ? '主力拉升'
      : short?.controlScaled && short.controlScaled > 0
        ? '控盘为正'
        : '控盘偏弱';

  const dragonStatus = dragon?.resonance
    ? '五维共振'
    : dragon?.buySignal
      ? '买字成立'
      : '卖字/未共振';

  const sections: ReportSection[] = [
    {
      title: '主图：开仓吃鱼',
      status: mainStatus,
      tone: main?.takeProfit ? 'bad' : main?.openEatFish || main?.eatFish ? 'good' : main?.strong ? 'warn' : 'neutral',
      lines: [
        `趋势带：${main?.strong ? '红色强趋势/持股侧' : '绿色弱趋势/现金侧'}；近20根吃鱼身 ${recentEatFish} 次，近40根开仓吃鱼 ${recentOpenEatFish} 次。`,
        `均线位置：收盘 ${fmtPrice(latestK.close)}，MA5 ${fmtPrice(ma5)}，MA10 ${fmtPrice(ma10)}，MA20 ${fmtPrice(ma20)}；${maStack ? '短中均线多头排列' : '均线排列尚未完全顺畅'}。`,
        `信号含义：吃鱼身代表趋势参与点，开仓吃鱼代表现金转持股后的更强入场点，及时止盈代表短线风险释放。`,
      ],
    },
    {
      title: '1异动监控VIP',
      status: anomalyStatus,
      tone: anomaly?.fire || anomaly?.superSignal ? 'hot' : anomaly?.anomaly || anomaly?.eatFish ? 'good' : 'neutral',
      lines: [
        `异动：${anomaly?.anomaly ? '当日黄色异动柱成立' : '当日未触发'}；点火：${anomaly?.fire ? '成立，涨幅超过2%阈值' : '未触发'}；近20根异动 ${recentAnomaly} 次，点火 ${recentFire} 次。`,
        `波段计数：${anomaly?.superSignal ? `当前为第${anomaly.superCount}次强信号` : '当前不是超强计数日'}；最近异动日 ${lastSignalText(anomalySeries.points, point => point.anomaly)}。`,
        `颜色/形状：青色柱和“◆吃鱼身”同步主图吃鱼身；黄色柱是主力异动；紫色尖峰是异动强弱；转强/主升/超强表示异动后的持续强度。`,
      ],
    },
    {
      title: '2短线能量VIP',
      status: shortStatus,
      tone: short?.mainShip || short?.macdDead ? 'bad' : short?.startControl || short?.strongCondition ? 'good' : short?.controlScaled && short.controlScaled > 0 ? 'warn' : 'neutral',
      lines: [
        `控盘：${fmtPrice(short?.controlScaled)}；${short?.startControl ? '出现“主力★拉升”' : '未见当日主力拉升'}；${short?.mainShip ? '出现“减仓”' : '未见当日减仓'}。`,
        `短线异动：${short?.anomaly ? '严格异动成立' : '未触发严格异动'}；点火：${short?.fire ? '成立' : '未触发'}；近20根严格异动 ${recentStrictAnomaly} 次。`,
        `MACD：${short?.macdGolden ? '金叉' : short?.macdDead ? '死叉' : '无新交叉'}；紫红控盘层为本地筹码近似代理，重点看方向和持续性。`,
      ],
    },
    {
      title: '3五维擒龙VIP',
      status: dragonStatus,
      tone: dragon?.resonance ? 'hot' : dragon?.buySignal && bullDims >= 4 ? 'good' : dragon?.buySignal ? 'warn' : 'bad',
      lines: [
        `控盘度：${fmtPrice(dragon?.controlDegree)}，属于${controlLabel(dragon)}；红色维度 ${bullDims}/5。`,
        `五维：买卖=${dragon?.buySignal ? '买' : '卖'}，趋势=${dragon?.trendBull ? '红' : '绿'}，量能=${dragon?.energyBull ? '红' : '绿'}，中期=${dragon?.midBull ? '红' : '绿'}，短期=${dragon?.shortBull ? '红' : '绿'}。`,
        `共振：${dragon?.resonance ? 'GZ成立，买字+量能+中期+短期同步' : 'GZ未成立，还缺至少一个维度确认'}。`,
      ],
    },
  ];

  const summary = `${stock?.name || '当前股票'} ${stock?.symbol || ''} 最新收盘 ${fmtPrice(latestK.close)}，看板评分 ${score}/100，结论为“${stance}”。`;
  const metrics: ReportMetric[] = [
    { label: '日期', value: latestK.time.slice(0, 10), tone: 'neutral' },
    { label: '收盘', value: fmtPrice(latestK.close), tone: dayPct >= 0 ? 'good' : 'bad' },
    { label: '日涨幅', value: fmtPct(dayPct), tone: dayPct >= 0 ? 'good' : 'bad' },
    { label: '5日涨幅', value: fmtPct(run5Pct), tone: run5Pct >= 0 ? 'good' : 'neutral' },
    { label: '20日涨幅', value: fmtPct(run20Pct), tone: run20Pct >= 0 ? 'good' : 'neutral' },
    { label: '量比5', value: volumeRatio, tone: 'warn' },
    { label: 'MA5', value: fmtPrice(ma5), tone: aboveMa5 ? 'good' : 'bad' },
    { label: 'MA10', value: fmtPrice(ma10), tone: aboveMa10 ? 'good' : 'bad' },
    { label: '五维红数', value: `${bullDims}/5`, tone: bullDims >= 4 ? 'good' : bullDims >= 3 ? 'warn' : 'bad' },
    { label: '压力位', value: fmtPrice(resistance), tone: 'neutral' },
  ];

  const process = [
    '先看主图：判断是否处在红色趋势带、是否出现吃鱼身/开仓吃鱼，以及有没有及时止盈。',
    '再看异动监控VIP：确认资金是否异动、是否点火，异动后有没有转强、主升、超强的持续计数。',
    '再看短线能量VIP：用控盘红白绿柱、主力★拉升、减仓、MACD金叉死叉判断次日动能是否还在。',
    '再看五维擒龙VIP：买/卖文字、趋势/量能/中期/短期四条红绿带和控盘度决定共振质量。',
    '最后落到执行：强势不等于追高，只有回踩不破关键均线或突破后不回落，才进入次日计划。',
  ];

  return {
    valid: true,
    score,
    stance,
    stanceTone,
    tradeDate: latestK.time.slice(0, 10),
    summary,
    nextAction,
    metrics,
    nextSteps,
    risks,
    sections,
    process,
  };
};

const fmtTime = (ts: number): string => new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });

export const BoardReportDialog: React.FC<BoardReportDialogProps> = ({
  isOpen, onClose, stock, data, period,
  dayKLineData, weekKLineData, monthKLineData, marketIndices,
  f10Overview, valuationSnapshot, marketMessage, marketStatusText, marketStatusCode,
  onForceSync, syncing,
}) => {
  const { colors } = useTheme();
  const technicalReport = useMemo(() => buildBoardReport(stock, data), [stock, data]);
  const technicalFallbackText = useMemo(() => {
    if (!technicalReport.valid) return technicalReport.summary;
    const sections = technicalReport.sections
      .map(section => `【${section.title}】\n${section.lines.join('\n')}`)
      .join('\n\n');
    return [
      `股票：${stock?.name || ''} ${stock?.symbol || ''}`.trim(),
      `周期：${PERIOD_LABELS[period]}`,
      `日期：${technicalReport.tradeDate}`,
      `结论：${technicalReport.stance}，评分 ${technicalReport.score}/100`,
      technicalReport.summary,
      technicalReport.nextAction,
      sections,
      `执行条件：${technicalReport.nextSteps.join('；')}`,
      `风险点：${technicalReport.risks.join('；') || '暂无明显风险提示'}`,
    ].filter(Boolean).join('\n\n');
  }, [period, technicalReport, stock]);

  const [question, setQuestion] = useState('');
  const [qaLoading, setQaLoading] = useState(false);
  const [qaHistory, setQaHistory] = useState<QAItem[]>([]);
  // 默认展示本地四图合并判读面板(秒开)；老陈完整报告按需点按钮生成(慢、走AI网关)
  const [showAIReport, setShowAIReport] = useState(false);
  const [reportLoading, setReportLoading] = useState(false);
  const [generatedReport, setGeneratedReport] = useState('');
  const [reportError, setReportError] = useState('');
  const [reportAgentName, setReportAgentName] = useState('老陈');
  const [reportModelName, setReportModelName] = useState('');
  const [reportGeneratedAt, setReportGeneratedAt] = useState('');
  const [reportFromCache, setReportFromCache] = useState(false);
  // 本次会话是否已发起过生成:缓存回填不许覆盖用户主动生成的结果
  const generationStartedRef = useRef(false);

  const reportText = generatedReport.trim();

  const loadReport = async () => {
    if (!stock?.symbol || reportLoading) return;
    generationStartedRef.current = true;
    setShowAIReport(true);
    setReportLoading(true);
    setReportError('');
    setGeneratedReport('');
    setReportGeneratedAt('');
    setReportFromCache(false);

    try {
      const res = await generateBoardReport({
        stockCode: stock.symbol,
        stockName: stock.name || '',
        period: PERIOD_LABELS[period],
      });

      if (!res) {
        setReportError('当前为浏览器预览模式，请从 Wails 开发入口打开后生成老陈完整报告。');
        return;
      }

      if (!res.success || !res.report) {
        setReportError(res.error || '老陈完整报告生成失败');
        return;
      }

      setGeneratedReport(res.report);
      setReportAgentName(res.agentName || '老陈');
      setReportModelName(res.modelName || '');
      setReportGeneratedAt(res.generatedAt || '');
    } catch (err) {
      setReportError(err instanceof Error ? err.message : '老陈完整报告生成失败，请稍后重试');
    } finally {
      setReportLoading(false);
    }
  };

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  useEffect(() => {
    if (!isOpen) return;
    setQuestion('');
    setQaLoading(false);
    setQaHistory([]);
    setReportError('');
    setGeneratedReport('');
    setReportAgentName('老陈');
    setReportModelName('');
    setReportGeneratedAt('');
    setReportFromCache(false);
    generationStartedRef.current = false;
    setShowAIReport(false); // 打开默认回到本地看板，老陈报告按需生成
  }, [isOpen, stock?.symbol]);

  // 打开弹窗后回填后端缓存的老陈报告(同票同周期)：不切换默认四图视图，
  // 只是用户点到老陈视图时直接秒开缓存，而不是"等待生成"再花8分钟
  useEffect(() => {
    if (!isOpen || !stock?.symbol) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await getCachedBoardReport(stock.symbol, PERIOD_LABELS[period]);
        if (cancelled || !res || !res.success || !res.found || !res.report) return;
        if (generationStartedRef.current) return;
        setGeneratedReport(res.report);
        setReportAgentName(res.agentName || '老陈');
        setReportModelName(res.modelName || '');
        setReportGeneratedAt(res.generatedAt || '');
        setReportFromCache(true);
      } catch {
        // 缓存查询失败静默降级为无缓存
      }
    })();
    return () => { cancelled = true; };
  }, [isOpen, stock?.symbol, period]);

  const submitQuestion = async () => {
    const q = question.trim();
    // 没生成老陈报告时，用本地四图口径回答，问答立即可用
    const baseReport = reportText || technicalFallbackText;
    if (!q || qaLoading || !baseReport) return;
    setQuestion('');
    setQaLoading(true);
    const createdAt = Date.now();

    try {
      const res = await askBoardReport({
        stockCode: stock?.symbol || '',
        report: baseReport,
        question: q,
      });

      if (!res) {
        setQaHistory(prev => [...prev, {
          question: q,
          answer: '当前为浏览器预览模式，请从 Wails 开发入口打开后再提问。',
          createdAt,
        }]);
        return;
      }

      setQaHistory(prev => [...prev, {
        question: q,
        answer: res.answer || '不知道。',
        modelName: res.modelName,
        createdAt,
        error: res.success ? undefined : res.error,
      }]);
    } catch (err) {
      setQaHistory(prev => [...prev, {
        question: q,
        answer: '',
        createdAt,
        error: err instanceof Error ? err.message : '问答失败，请稍后重试',
      }]);
    } finally {
      setQaLoading(false);
    }
  };

  if (!isOpen) return null;

  const shellClass = colors.isDark
    ? 'border-slate-700 bg-[#08111f] text-slate-100'
    : 'border-slate-200 bg-white text-slate-900';
  const reportPanelClass = colors.isDark
    ? 'border-slate-700 bg-slate-950/45 text-slate-200'
    : 'border-slate-200 bg-white text-slate-800';
  const mutedTextClass = colors.isDark ? 'text-slate-400' : 'text-slate-500';

  return (
    <div
      className="fixed inset-0 z-[95] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
      style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
      onClick={onClose}
    >
      <div
        className={`flex h-[92vh] w-[1500px] max-w-[98vw] flex-col overflow-hidden rounded-lg border shadow-2xl ${shellClass}`}
        onClick={(event) => event.stopPropagation()}
      >
        <header className={`flex shrink-0 items-center justify-between gap-3 border-b px-4 py-3 ${colors.isDark ? 'border-slate-800 bg-slate-950/95' : 'border-slate-200 bg-white/95'}`}>
          <div className="flex min-w-0 items-center gap-2">
            <FileText className="h-4 w-4 shrink-0 text-amber-300" />
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">
                看板报告 · {stock?.name || '当前股票'} {stock?.symbol || ''}
              </div>
              <div className={`mt-0.5 text-[11px] ${mutedTextClass}`}>
                {showAIReport
                  ? `老陈完整报告 · ${PERIOD_LABELS[period]} · ${reportGeneratedAt ? `${reportGeneratedAt} 生成` : '生成中'}${reportFromCache ? ' · 缓存' : ''}${reportModelName ? ` · ${reportModelName}` : ''}`
                  : `${PERIOD_LABELS[period]} · ${technicalReport.tradeDate || ''} · 四图合并判读`}
              </div>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {!showAIReport && technicalReport.valid && (
              <div className={`inline-flex h-8 items-center rounded border px-2.5 text-[12px] font-semibold ${
                technicalReport.stanceTone === 'good' ? 'border-emerald-400/40 bg-emerald-500/10 text-emerald-200'
                : technicalReport.stanceTone === 'hot' ? 'border-orange-400/40 bg-orange-500/10 text-orange-200'
                : technicalReport.stanceTone === 'bad' ? 'border-rose-400/40 bg-rose-500/10 text-rose-200'
                : 'border-amber-400/40 bg-amber-500/10 text-amber-200'
              }`}>
                {technicalReport.stance}
              </div>
            )}
            {!showAIReport && technicalReport.valid && (
              <div className="inline-flex h-10 flex-col items-center justify-center rounded border border-amber-400/45 bg-amber-500/10 px-3">
                <span className="text-[9px] leading-none text-amber-200/80">评分</span>
                <span className="text-[15px] font-bold leading-tight text-amber-200">{technicalReport.score}</span>
              </div>
            )}
            {showAIReport && (
              <>
                <div className="inline-flex h-8 items-center gap-2 rounded border border-emerald-400/35 bg-emerald-500/10 px-2.5 text-[12px] font-semibold text-emerald-100">
                  {reportAgentName}
                </div>
                <button
                  type="button"
                  onClick={() => setShowAIReport(false)}
                  className="inline-flex h-8 items-center gap-1.5 rounded border border-slate-500/40 bg-slate-500/10 px-2.5 text-xs font-semibold text-slate-200 transition-colors hover:bg-slate-500/20"
                  title="返回本地看板判读"
                >
                  看板
                </button>
              </>
            )}
            <button
              type="button"
              onClick={() => {
                // 有缓存时先秒开缓存视图；老陈视图里再点(重新生成)才真正走AI
                if (!showAIReport && reportText) {
                  setShowAIReport(true);
                  return;
                }
                void loadReport();
              }}
              disabled={reportLoading || !stock?.symbol}
              className="inline-flex h-8 items-center gap-1.5 rounded border border-cyan-400/35 bg-cyan-400/10 px-2.5 text-xs font-semibold text-cyan-100 transition-colors hover:bg-cyan-400/20 disabled:opacity-50"
              title={!showAIReport && reportText ? '查看已缓存的老陈完整报告(秒开)' : '调用老陈生成完整AI报告(较慢)'}
            >
              {reportLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              {showAIReport ? '重新生成' : '老陈完整报告'}
            </button>
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-8 w-8 items-center justify-center rounded border border-slate-700 text-slate-400 transition-colors hover:border-red-400/50 hover:bg-red-500/10 hover:text-red-200"
              title="关闭"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </header>

        <div className="min-h-0 flex-1 grid grid-cols-[1.45fr_0.85fr] gap-0">
          <main className="min-h-0 overflow-auto px-4 py-3">
            {!showAIReport ? (
              /* 本地四图合并判读面板(秒开，默认视图) */
              !technicalReport.valid ? (
                <div className={`rounded-md border p-4 text-sm ${reportPanelClass}`}>{technicalReport.summary}</div>
              ) : (
                <div className="space-y-3">
                  <div className={`rounded-md border p-3.5 ${colors.isDark ? 'border-amber-400/25 bg-amber-500/[0.07]' : 'border-amber-300 bg-amber-50'}`}>
                    <div className="flex items-start gap-2">
                      <span className="mt-0.5 text-amber-300">⚡</span>
                      <div>
                        <div className="text-sm font-bold">{technicalReport.nextAction}</div>
                        <div className={`mt-1 text-xs ${mutedTextClass}`}>{technicalReport.summary}</div>
                      </div>
                    </div>
                  </div>

                  <div className="grid grid-cols-5 gap-2">
                    {technicalReport.metrics.map((m) => (
                      <div key={m.label} className={`rounded-md border px-2 py-2.5 text-center ${
                        m.tone === 'good' ? 'border-emerald-400/30 bg-emerald-500/[0.06]'
                        : m.tone === 'bad' ? 'border-rose-400/30 bg-rose-500/[0.06]'
                        : m.tone === 'warn' ? 'border-amber-400/30 bg-amber-500/[0.06]'
                        : colors.isDark ? 'border-slate-700 bg-slate-900/40' : 'border-slate-200 bg-slate-50'
                      }`}>
                        <div className={`text-[10px] ${mutedTextClass}`}>{m.label}</div>
                        <div className={`mt-1 font-mono text-[15px] font-bold ${
                          m.tone === 'good' ? 'text-emerald-300'
                          : m.tone === 'bad' ? 'text-rose-300'
                          : m.tone === 'warn' ? 'text-amber-300'
                          : colors.isDark ? 'text-slate-200' : 'text-slate-700'
                        }`}>{m.value}</div>
                      </div>
                    ))}
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    {technicalReport.sections.map((s) => (
                      <div key={s.title} className={`rounded-md border p-3.5 ${reportPanelClass}`}>
                        <div className="flex items-center justify-between gap-2">
                          <div className="text-sm font-bold">{s.title}</div>
                          <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold ${
                            s.tone === 'good' ? 'border-emerald-400/40 text-emerald-300'
                            : s.tone === 'hot' ? 'border-orange-400/40 text-orange-300'
                            : s.tone === 'bad' ? 'border-rose-400/40 text-rose-300'
                            : s.tone === 'warn' ? 'border-amber-400/40 text-amber-300'
                            : 'border-slate-500/40 text-slate-400'
                          }`}>{s.status}</span>
                        </div>
                        <div className={`mt-2.5 space-y-2 text-center text-xs leading-relaxed ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>
                          {s.lines.map((line, i) => <div key={i}>{line}</div>)}
                        </div>
                      </div>
                    ))}
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div className={`rounded-md border p-3.5 ${colors.isDark ? 'border-emerald-400/25 bg-emerald-500/[0.05]' : 'border-emerald-300 bg-emerald-50'}`}>
                      <div className="flex items-center gap-1.5 text-sm font-bold text-emerald-300">✓ 次日执行条件</div>
                      <div className={`mt-2 space-y-1.5 text-center text-xs leading-relaxed ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>
                        {technicalReport.nextSteps.map((line, i) => <div key={i}>· {line}</div>)}
                      </div>
                    </div>
                    <div className={`rounded-md border p-3.5 ${colors.isDark ? 'border-rose-400/25 bg-rose-500/[0.05]' : 'border-rose-300 bg-rose-50'}`}>
                      <div className="flex items-center gap-1.5 text-sm font-bold text-rose-300">🛡 风险点</div>
                      <div className={`mt-2 space-y-1.5 text-center text-xs leading-relaxed ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>
                        {technicalReport.risks.length
                          ? technicalReport.risks.map((line, i) => <div key={i}>· {line}</div>)
                          : <div>暂无明显风险提示</div>}
                      </div>
                    </div>
                  </div>

                {/* 波段模型驾驶舱(与主页同源同显):漏斗初筛/多周期共振/量价验证/联动环境/择时仓位 */}
                {stock && (
                  <div className="mt-1">
                    <ModelRadarStrip
                      embed
                      stock={stock}
                      kLineData={data}
                      period={period}
                      panelHeight={430}
                      dayKLineData={dayKLineData}
                      weekKLineData={weekKLineData}
                      monthKLineData={monthKLineData}
                      marketIndices={marketIndices}
                      f10Overview={f10Overview}
                      valuationSnapshot={valuationSnapshot}
                      marketMessage={marketMessage}
                      marketStatusText={marketStatusText}
                      marketStatusCode={marketStatusCode}
                      onForceSync={onForceSync}
                      syncing={syncing}
                    />
                  </div>
                )}
                </div>
              )
            ) : reportLoading ? (
              <div className={`flex min-h-[420px] flex-col items-center justify-center rounded-md border p-6 text-center ${reportPanelClass}`}>
                <Loader2 className="mb-3 h-7 w-7 animate-spin text-cyan-300" />
                <div className="text-sm font-semibold">老陈正在生成完整报告</div>
                <div className={`mt-2 max-w-[520px] text-xs leading-relaxed ${mutedTextClass}`}>
                  正在拉取最新行情、财务、估值、主营业务、研报和同行对比数据。完整报告可能需要3-6分钟，可先点「看板」回本地判读。
                </div>
              </div>
            ) : reportError ? (
              <div className={`rounded-md border border-rose-400/35 bg-rose-500/10 p-4 text-sm text-rose-100`}>
                <div className="font-semibold">老陈完整报告生成失败</div>
                <div className="mt-2 text-xs leading-relaxed text-rose-100/85">{reportError}</div>
                <div className={`mt-3 rounded border p-3 text-xs leading-relaxed ${colors.isDark ? 'border-slate-700 bg-slate-950/45 text-slate-400' : 'border-slate-200 bg-white text-slate-600'}`}>
                  本地技术摘要备用口径：{technicalFallbackText.slice(0, 320)}{technicalFallbackText.length > 320 ? '...' : ''}
                </div>
              </div>
            ) : reportText ? (
              <article className={`agent-message-content rounded-md border p-4 text-sm leading-relaxed ${reportPanelClass}`}>
                {/* final: 内容是一次性完整文本，不加会把长文里未闭合结构当"仍在流入"永远显示骨架占位 */}
                <NodeRenderer content={reportText} final />
              </article>
            ) : (
              <div className={`rounded-md border p-4 text-sm ${reportPanelClass}`}>等待生成老陈完整报告。</div>
            )}
          </main>

          <aside className={`min-h-0 border-l flex flex-col ${colors.isDark ? 'border-slate-800 bg-slate-950/35' : 'border-slate-200 bg-slate-50/80'}`}>
            <div className={`flex items-center justify-between border-b px-4 py-3 ${colors.isDark ? 'border-slate-800' : 'border-slate-200'}`}>
              <div className="flex items-center gap-2 text-sm font-semibold">
                <Bot className="h-4 w-4 text-cyan-300" />
                报告问答
              </div>
              <div className={`text-[11px] ${colors.isDark ? 'text-slate-500' : 'text-slate-500'}`}>用当前报告直接提问</div>
            </div>

            <div className="min-h-0 flex-1 overflow-auto px-4 py-3 space-y-3">
              {qaHistory.length === 0 ? (
                <div className={`rounded-md border p-3 text-xs ${colors.isDark ? 'border-slate-700 bg-slate-900/50 text-slate-400' : 'border-slate-200 bg-white text-slate-500'}`}>
                  你可以问：<br />
                  1. 这只票现在最强的风险是什么？<br />
                  2. 这份报告里最值得盯的支撑位是哪几个？<br />
                  3. 如果要优化下一次判断，优先改哪里？
                </div>
              ) : (
                qaHistory.map((item) => (
                  <div key={`${item.createdAt}-${item.question}`} className={`space-y-2 rounded-md border p-3 ${colors.isDark ? 'border-slate-700 bg-slate-900/50' : 'border-slate-200 bg-white'}`}>
                    <div className="flex items-start gap-2 text-[12px]">
                      <span className="mt-0.5 rounded bg-cyan-500/15 px-1.5 py-0.5 text-[10px] text-cyan-200">问</span>
                      <div className="flex-1">{item.question}</div>
                    </div>
                    {item.error ? (
                      <div className="flex items-start gap-2 text-[12px] text-rose-200">
                        <span className="mt-0.5 rounded bg-rose-500/15 px-1.5 py-0.5 text-[10px] text-rose-200">错</span>
                        <div className="flex-1">{item.error}</div>
                      </div>
                    ) : (
                      <div className="space-y-1 text-[12px] text-slate-300">
                        <div className="flex items-center gap-2 text-slate-500">
                          <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] text-emerald-200">答</span>
                          <span>{item.modelName || '模型'}</span>
                          <span>{fmtTime(item.createdAt)}</span>
                        </div>
                        <div className="agent-message-content leading-relaxed">
                          <NodeRenderer content={item.answer} final />
                        </div>
                      </div>
                    )}
                  </div>
                ))
              )}
            </div>

            <div className={`shrink-0 border-t px-4 py-3 ${colors.isDark ? 'border-slate-800' : 'border-slate-200'}`}>
              <div className={`rounded-md border p-2 ${colors.isDark ? 'border-slate-700 bg-slate-900/70' : 'border-slate-200 bg-white'}`}>
                <textarea
                  value={question}
                  onChange={(e) => setQuestion(e.target.value)}
                  placeholder="围绕这份报告继续追问"
                  className={`min-h-[92px] w-full resize-none rounded border px-3 py-2 text-sm outline-none placeholder:text-slate-500 ${
                    colors.isDark
                      ? 'border-slate-700 bg-slate-950 text-slate-100'
                      : 'border-slate-200 bg-white text-slate-900'
                  }`}
                />
                <div className="mt-2 flex items-center justify-between gap-2">
                  <div className={`text-[11px] ${colors.isDark ? 'text-slate-500' : 'text-slate-500'}`}>
                    只能基于报告本身回答，超出范围会直接说不知道。
                  </div>
                  <button
                    type="button"
                    onClick={submitQuestion}
                    disabled={qaLoading || !question.trim()}
                    className="inline-flex items-center gap-1.5 rounded border border-cyan-400/40 bg-cyan-400/10 px-3 py-1.5 text-xs font-semibold text-cyan-100 hover:bg-cyan-400/20 disabled:opacity-50"
                  >
                    {qaLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
                    {qaLoading ? '回答中' : '发送'}
                  </button>
                </div>
              </div>
            </div>
          </aside>
        </div>
      </div>
    </div>
  );
};

export default BoardReportDialog;
