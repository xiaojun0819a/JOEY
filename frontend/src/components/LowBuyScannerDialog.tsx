import React, { useEffect, useState } from 'react';
import { Database, Loader2, RefreshCw, Search, X, AlertTriangle, CheckCircle2, SlidersHorizontal, BarChart3, Info, Check } from 'lucide-react';
import type { Stock } from '../types';
import { AddToGroupButton } from './AddToGroupButton';
import { BatchAddToGroupButton } from './BatchAddToGroupButton';
import { StrategyScorecard } from './StrategyScorecard';
import { StrategyReviewDialog } from './StrategyReviewDialog';
import { runBacktest, type BacktestResult } from '../services/backtestService';
import { runTailLazyBatchReplay, type TailLazyBatchResult, runLowBuyBatchReplay, type LowBuyBatchResult } from '../services/scannerService';

export interface LowBuyScannerRequest {
  limit: number;
  includeBeijing: boolean;
  historyFilterEnabled?: boolean;
  historyTurnoverDays?: number;
  historyTurnoverMax?: number;
  historyMainFlowDays?: number;
  historyMainFlowPositiveDays?: number;
  historyMAPeriod?: number;
  maxChangePct?: number;
  caoYuanStrict?: boolean;
  historyPickDate?: string;
}

export interface HistoryCollectRequest {
  tradeDate?: string;
  includeBeijing: boolean;
}

export interface HistoryCollectResult {
  tradeDate: string;
  startedAt: string;
  finishedAt: string;
  dbPath: string;
  totalCount: number;
  savedCount: number;
  failedCount: number;
  maUpdated: boolean;
  status: string;
  message: string;
}

export interface HistoryAutoCollectRequest {
  enabled: boolean;
  collectStart?: string;
  collectEnd?: string;
  includeBeijing: boolean;
}

export interface HistoryAutoCollectStatus {
  enabled: boolean;
  collectStart: string;
  collectEnd: string;
  includeBeijing: boolean;
  lastCollectDate: string;
  dbPath: string;
  message: string;
}

export interface LowBuyScannerMarketOverview {
  shPrice: number;
  shMA20: number;
  limitUpCount: number;
  limitDownCount: number;
  totalAmount: number;
}

export interface LowBuyScannerItem {
  symbol: string;
  name: string;
  price: number;
  changePercent: number;
  amount: number;
  turnoverRate: number;
  mainNetInflow: number;
  mainNetInflowRatio: number;
  mainFlowSource: string;
  totalMarketCap: number;
  floatMarketCap: number;
  capBucket: string;
  industry: string;
  score: number;
  triggerCount: number;
  triggers: string[];
  reasons: string[];
  riskFlags: string[];
  buyPointHint: string;
  sellPointHint: string;
  stopLossHint: string;
  ma10: number;
  ma10Status: string; // hold | broke | ''
  nextDate?: string;
  nextOpenGainPct?: number;
  nextHighGainPct?: number;
  nextCloseGainPct?: number;
  replayExitDate?: string;
  replayHoldDays?: number;
  replayReturnPct?: number;
  replayExitReason?: string;
  updatedAt: string;
}

export interface LowBuyScannerResult {
  asOf: string;
  ruleVersion: string;
  universeCount: number;
  candidateCount: number;
  selectedCount: number;
  marketGatePassed: boolean;
  marketGateReasons: string[];
  marketOverview: LowBuyScannerMarketOverview;
  items: LowBuyScannerItem[];
  warning?: string;
}

interface LowBuyScannerDialogProps {
  isOpen: boolean;
  title?: string;
  subtitle?: string;
  loading: boolean;
  result: LowBuyScannerResult | null;
  error: string;
  onClose: () => void;
  onScan: (req: LowBuyScannerRequest) => void;
  onCollectHistory: (req: HistoryCollectRequest) => Promise<HistoryCollectResult | null>;
  onGetHistoryAutoCollectStatus: () => Promise<HistoryAutoCollectStatus | null>;
  onUpdateHistoryAutoCollect: (req: HistoryAutoCollectRequest) => Promise<HistoryAutoCollectStatus | null>;
  onAddToWatchlist: (stock: Stock) => Promise<boolean>;
  watchlistSymbols: string[];
  onOpenStock?: (stock: Stock) => void | Promise<void>;
  onOpenLateDayChase: () => void;
  strategyMode?: 'lowbuy' | 'limit-pullback' | 'triple-volume' | 'tail-buy' | 'hot-money' | 'dip-entry' | 'monster' | 'monster-v10' | 'taillazy' | 'caoyuan-standard4a' | 'caoyuan-zhuang4b';
  onReplay?: (date: string) => void;
}

// 低吸规则 V1.2 要点浮层
const RulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[460px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">低吸规则 V1.2 · 要点</div>
          <div className="text-[10px] fin-text-tertiary">小盘主板 + 资金确认 + 偏好微跌回踩 + 只打前3 + 机械止损止盈</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>

      <Sec title="② 硬过滤（全满足）">
        <div>· 总市值 <b>20~100亿</b>；剔除 300/688、ST、北交所</div>
        <div>· 行业黑名单：地产/农业/林业/建筑/零售/百货</div>
        <div>· 当日涨幅 <b>−3% ~ +1.5%</b>；换手 <b>≤8%</b></div>
        <div>· 主力真实源 + 强度<b>≥1%</b> + 净流入<b>≥800万</b></div>
      </Sec>

      <Sec title="③ 触发信号（4选3）">
        <div>① 换手≤2.5% · ② 主力净流入&gt;0 · ③ 主力强度≥3% · ④ 涨幅≥−2%</div>
      </Sec>

      <Sec title="④ 评分·涨跌结构（V1.2 核心改动）">
        <div>· <span className="text-red-400">[−1%,0) 微跌回踩 +12</span>（置顶）</div>
        <div>· [−2%,−1%) +4 · <span className="text-green-400">[0,1%) 已翻红 −2</span> · ≥1% −5</div>
        <div className="fin-text-tertiary">→ 让"微跌回踩"名次置顶，追涨压低</div>
      </Sec>

      <Sec title="⑤ 操作 & 推送">
        <div>· <b>只取前3名</b>（集中优于摊薄）；尾盘自动推送前3</div>
        <div>· 进场：尾盘14:30–15:00分批 或 次日开盘</div>
      </Sec>

      <Sec title="⑥ 退出纪律（机械）">
        <div>1. 跌破买入价 <b>−5%</b> 止损</div>
        <div>2. 跌破<b>10日线次日不收回</b> 离场</div>
        <div>3. 单日<b>换手&gt;12%</b> 走</div>
        <div>4. <b>+15% 减半</b>（余仓续跑）</div>
        <div>5. 持有<b>满5日且涨幅&lt;3%</b> 清仓</div>
      </Sec>

      <Sec title="⑦ 可选叠加（自定维度筛选）">
        <div>连续N日换手&lt;X% · 近N日主力≥M日净流入正 · <b>站上MA10（不破均线）</b></div>
      </Sec>

      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        定性：防守型低波 beta 底仓——绝对赚钱、平/弱市抗跌，长期 alpha≈0。成本口径：佣金万2.5双边+印花税千1.0+滑点千1.0≈来回0.35%。
      </div>
    </div>
  );
};

// 尾盘懒人策略2 要点浮层
const TailLazyRulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[470px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">尾盘懒人策略 V2 · 要点</div>
          <div className="text-[10px] fin-text-tertiary">打强：尾盘买入 → 次日上午冲高止盈（"睡后收入"）</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>

      <Sec title="① 量价粗筛（快照）">
        <div>· 量比 <b>1 ~ 2.5</b>（交投活跃但不过热）</div>
        <div>· 当日涨幅 <b>3% ~ 6%</b>（惯性足、不追高）</div>
        <div>· 换手 <b>5% ~ 10%</b>（不吸筹期、不尾期）</div>
        <div>· 剔除创业板/科创/北交所、ST</div>
      </Sec>

      <Sec title="② K线技术（日K验证）">
        <div>· <b>多头排列</b>：MA10 &gt; MA20</div>
        <div>· 当日最高 = <b>近5日新高</b></div>
        <div>· 近20日 <b>≥1次涨幅&gt;8%</b>（股性激活）</div>
        <div>· 近20日 <b>无单日跌幅&lt;−8%</b>（抗跌）</div>
        <div>· <b>前7日无涨停</b>（不过热）</div>
        <div>· <b>上影线 ≤ 实体</b>（无冲高回落抛压）</div>
      </Sec>

      <Sec title="③ 形态二选一（保留）">
        <div>· <span className="text-red-400">强</span>：完整站上 10/20 日线</div>
        <div>· <span className="text-amber-400">中</span>：破10线但下影回踩20线、收在20线上反弹</div>
        <div className="fin-text-tertiary">→ 破10线又没碰20线 / 跌破20线 = 弱，剔除</div>
      </Sec>

      <Sec title="④ 操作">
        <div>· 买点：<b>尾盘14:30-15:00分批</b>买入</div>
        <div>· 卖点：<b>次日上午冲高5-6点止盈</b>；乏力/平开保本走</div>
        <div>· 止损：次日走弱或 <b>−3%</b> 离场</div>
      </Sec>

      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        定位：短线动量打板式，适合上班族尾盘挂单、次日上午了结。错亏2-3点、对赚5-6点起。形态那条为系统近似，仍建议人工瞄一眼K线。
      </div>
    </div>
  );
};

const LimitPullbackRulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[500px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">涨停回调低吸 · 规则要点</div>
          <div className="text-[10px] fin-text-tertiary">先看强启动，再等缩量回踩，放量站稳均线后参与</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>
      <Sec title="① 强启动">
        <div>· 近 <b>2-8</b> 个交易日出现涨停/准涨停</div>
        <div>· 启动日成交额至少为前5日均值 <b>1.25倍</b>，优先 ≥1.5倍</div>
      </Sec>
      <Sec title="② 趋势闸门">
        <div>· 剔除 <b>MA5&lt;MA10&lt;MA20</b> 的空头排列</div>
        <div>· 剔除收盘压在 <b>MA20/MA30</b> 下方的反抽票</div>
        <div>· 剔除 <b>MA20&lt;MA30&lt;MA60</b>、MA20斜率向下的下降通道</div>
        <div>· 只做强势股回调，不做下跌趋势中的涨停反抽</div>
      </Sec>
      <Sec title="③ 缩量洗盘">
        <div>· 涨停后不追高，等待横盘或回调</div>
        <div>· 回调均额低于启动日成交额，缩得越充分越好</div>
        <div>· 回撤不宜过深，优先不破涨停阳线半分位/低点</div>
      </Sec>
      <Sec title="④ 低吸确认">
        <div>· 收盘站稳 <b>MA5/MA10</b>，且贴近均线更适合低吸</div>
        <div>· 或者放量突破回调区间高点，再等回踩确认</div>
      </Sec>
      <Sec title="⑤ 操作纪律">
        <div>· 买点：尾盘14:30后分批，回踩5/10日线或涨停半分位不破</div>
        <div>· 止损：跌破低吸位或5日线先走；放量跌破涨停阳线低点，模型失效</div>
        <div>· 止盈：冲高放量滞涨、长上影、跌破5日线先减</div>
      </Sec>
      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        定位：右侧强势股回调买点，不做连续一字板接力，也不做无涨停记忆的普通反抽。
      </div>
    </div>
  );
};

const TripleVolumeRulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[500px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">三倍量策略5 · 规则要点</div>
          <div className="text-[10px] fin-text-tertiary">主板10cm口径：放量突破多条均线，次日缩量回踩再确认</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>
      <Sec title="① 当天选股公式">
        <div>· 阳线未涨停：<b>收盘价 &gt; 开盘价</b>，且收盘价 &lt; 昨收 × <b>1.095</b></div>
        <div>· 三倍量：今日成交量 ≥ 昨日成交量 × <b>3</b></div>
        <div>· 一阳穿线：收盘同时上穿 <b>MA5 / MA10 / MA20 / MA30</b></div>
      </Sec>
      <Sec title="② 板块口径">
        <div>· 剔除 ST、北交所、创业板300/301、科创板688/689</div>
        <div>· 原因：公式里的未涨停阈值按主板10cm设计，20cm股票会改变信号含义</div>
      </Sec>
      <Sec title="③ 评分">
        <div>· 核心分：完整穿越四条均线 + 三倍量强度</div>
        <div>· 加分：实体阳线、突破后离均线不远、成交额足够、市值弹性更好</div>
        <div>· 扣分：上影线长、接近涨停、量能过度放大、突破后偏离均线太远</div>
      </Sec>
      <Sec title="④ 买点纪律">
        <div>· 不追当天突破长阳；重点看 <b>第二天缩量回调</b></div>
        <div>· 回调不破突破日成本线/阳线低点，并且分时承接稳定，再考虑低吸</div>
      </Sec>
      <Sec title="⑤ 风控">
        <div>· 跌破突破阳线低点或买入价-5%，策略失效</div>
        <div>· 次日放量滞涨、冲高长上影、跌回均线下方，先减仓观察</div>
      </Sec>
      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        定位：突破启动后的次日回踩策略。当天扫描只负责找“疑似启动”，真正交易要等次日缩量承接。
      </div>
    </div>
  );
};

const FormulaKLineRulesPopover: React.FC<{ kind: string; onClose: () => void }> = ({ kind, onClose }) => {
  const profiles: Record<string, { title: string; subtitle: string; sections: Array<{ title: string; lines: string[] }> }> = {
    'tail-buy': {
      title: '尾盘买入策略6 · 规则要点',
      subtitle: '昨日资金强势触发，今日阴线回踩，尾盘确认承接',
      sections: [
        { title: '① 选股节奏', lines: ['昨日 WPZY_NP 触发：强势位置、换手>5%、站上强势均线代理', '今日 C<O：不追昨日强阳，只看尾盘回踩承接'] },
        { title: '② 本地代理', lines: ['CAPITAL 用流通市值/价格估算，换手优先用成交额/流通市值', 'FILTER(...,30) 保留，避免短期重复信号'] },
        { title: '③ 交易纪律', lines: ['尾盘14:30后不破昨日成本线再考虑', '跌破昨日强势阳线低点或买入价-5%止损'] },
      ],
    },
    'hot-money': {
      title: '游资突破策略7 · 规则要点',
      subtitle: '涨停/准涨停结构 + 量能倍率 + 流通股本分档',
      sections: [
        { title: '① 结构信号', lines: ['LZY_11 / LZY_L8 / LZY_1W：涨停、连阳、隔日再启动结构', '收盘贴近最高价，代表游资突破强度'] },
        { title: '② 量能和股本', lines: ['LZY_AW 要求 1~5，避免量能太弱或过度爆量', 'DYNAINFO(58) 用流通股本万股代理，按价格分档'] },
        { title: '③ 风控', lines: ['只做回踩承接，不追一字和秒板', '次日高开冲板失败或放量长上影先走'] },
      ],
    },
    'dip-entry': {
      title: '低吸入场策略8 · 规则要点',
      subtitle: '三类短线反转信号至少2类共振',
      sections: [
        { title: '① 三类信号', lines: ['GQZY_CD：RSI5上穿20，同时上穿RSI8且低于50', 'GQZY_KK：快速RSI上穿11', 'GQZY_8J：动能线低位反转并通过FILTER去重'] },
        { title: '② 入场定位', lines: ['这是反转低吸，不是突破追涨', '尾盘不再破日内低点，或次日回踩不破信号日低点再进'] },
        { title: '③ 退出', lines: ['反弹到MA10/MA20或放量滞涨分批止盈', '跌破信号日低点或买入价-5%止损'] },
      ],
    },
    monster: {
      title: '捉妖策略9 · 规则要点',
      subtitle: '原“捉妖选股”公式的可落地复刻',
      sections: [
        { title: '① 信号来源', lines: ['长期沉寂后突然转强', '放量突破前高或突破后第2日确认', '布林收敛后爆发', '60日低点反抽只做辅助，不单独出票'] },
        { title: '② 本地代理', lines: ['MACD.GGZY_A8 用标准MACD柱代理', 'BOLL.UB 用20日布林上轨代理，CCI用标准14日CCI', 'CURRBARSCOUNT/CONST 用“距上次突破天数”代理'] },
        { title: '③ 入池门槛', lines: ['必须有核心信号：妖股启动 / 强放量突破 / 布林收敛爆发', '至少2个触发信号共振；普通低点反抽会被过滤'] },
        { title: '④ 风控', lines: ['只在突破后回踩不破或尾盘站稳前高时试错', '封板失败、放量长上影或跌破信号日低点先走'] },
      ],
    },
    'monster-v10': {
      title: '捉妖策略10 · 规则要点',
      subtitle: '通达信公式严格复刻，不沿用策略9代理放宽',
      sections: [
        { title: '① 最终入选', lines: ['只看原公式最后一行：捉妖选股 = GGZY_ZS', 'GGZY_ZS = FILTER(GGZY_IG=1,3)，评分只用于排序展示'] },
        { title: '② 公式链路', lines: ['保留 GGZY_YI / GGZY_0M / GGZY_DD / GGZY_IR / GGZY_4Z / GGZY_IG 等变量链', 'BARSLAST、BARSSINCEN、HHVBARS、EVERY、RANGE、FILTER 按序列计算'] },
        { title: '③ 本地口径', lines: ['MACD.MACD*100 按标准MACD柱计算', 'MACD.GGZY_A8 按同一标准MACD柱承接，不额外放宽', 'CAPITAL 用流通市值/价格估算'] },
        { title: '④ 风控', lines: ['信号日后只看承接，不追高', '跌破信号日低点或买入价-5%止损'] },
      ],
    },
  };
  const profile = profiles[kind] || profiles['dip-entry'];
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[500px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">{profile.title}</div>
          <div className="text-[10px] fin-text-tertiary">{profile.subtitle}</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>
      {profile.sections.map((section) => (
        <div key={section.title} className="mb-2.5">
          <div className="text-[11px] font-semibold text-accent-2 mb-1">{section.title}</div>
          <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">
            {section.lines.map((line) => <div key={line}>· {line}</div>)}
          </div>
        </div>
      ))}
      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        说明：这些是公式扫描信号，只负责“入池/观察”，最终买点仍看当日尾盘或次日分时承接。
      </div>
    </div>
  );
};

const CaoYuanStandardRulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[480px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">草元标准 4A · 规则要点</div>
          <div className="text-[10px] fin-text-tertiary">normal结果反推：深度超跌 + 贴近地板 + 当日止跌</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>
      <Sec title="① 硬过滤">
        <div>· 沪深主板/创业板；剔除 ST、科创688、北交所</div>
        <div>· 总市值与流通市值均为 <b>20~100亿</b></div>
        <div>· 当日止跌翻红，换手 <b>≤3%</b>，量比 <b>≤1.5</b></div>
      </Sec>
      <Sec title="② 日K反推">
        <div>· 10日涨幅 <b>≤ -1.5%</b>；20日涨幅 <b>≤ -8%</b></div>
        <div>· 收盘在 MA20 下方，优先 MA5/MA10/MA20 全压制</div>
        <div>· 距20日低点 <b>≤8%</b>，越贴近地板评分越高</div>
      </Sec>
      <Sec title="③ 标准 / 严选">
        <div>· <b>标准</b>：按 normal 正格口径，覆盖更多超跌止跌候选</div>
        <div>· <b>严选</b>：二次收紧为翻红更明显、量比/换手放大、20/30日跌幅更深</div>
        <div>· 参考阈值：10日≤-3%、20日≤-12%、30日≤-14%、量比0.8~3.2</div>
      </Sec>
      <Sec title="④ 买卖纪律">
        <div>· 买点：尾盘确认不再破低后分批，定位超跌止跌低吸</div>
        <div>· 卖点：反抽3-8%先落袋；触及MA10/MA20或放量滞涨先走</div>
        <div>· 止损：跌破20日低点或买入价-5%离场</div>
      </Sec>
      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        说明：这是对草元 normal 接口入选结果的本地反推，不接入草元原站账号接口，也不使用抓包 token。
      </div>
    </div>
  );
};

const CaoYuanZhuangRulesPopover: React.FC<{ onClose: () => void }> = ({ onClose }) => {
  const Sec: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
    <div className="mb-2.5">
      <div className="text-[11px] font-semibold text-accent-2 mb-1">{title}</div>
      <div className="text-[11px] fin-text-secondary leading-relaxed space-y-0.5">{children}</div>
    </div>
  );
  return (
    <div className="absolute left-0 top-[calc(100%+8px)] z-[80] w-[500px] max-h-[64vh] overflow-auto fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3.5">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <div className="text-sm font-semibold fin-text-primary">草元抓庄 4B · 规则要点</div>
          <div className="text-[10px] fin-text-tertiary">ZZ结果反推：90日涨停记忆 + 深跌企稳 + 高控盘代理</div>
        </div>
        <button onClick={onClose} className="p-1 rounded fin-hover" title="关闭"><X className="h-3.5 w-3.5 fin-text-tertiary" /></button>
      </div>
      <Sec title="① 独立硬过滤">
        <div>· 板块/市值/ST过滤同 4A，但不复用 4A 的评分和触发</div>
        <div>· 10日涨幅 <b>≤ -3%</b>；20日涨幅 <b>≤ -10%</b></div>
        <div>· 必须有 <b>90日涨停记忆</b>（由本地日K推导）</div>
      </Sec>
      <Sec title="② 控盘代理">
        <div>· 高控盘没有原站真字段时，只用本地代理：涨停记忆 + 深回撤 + 贴近低点 + 缩量</div>
        <div>· 量能主区间：换手 ≤3% 更优，量比 ≤1.5 更优；过热降分</div>
      </Sec>
      <Sec title="③ 标准 / 严选">
        <div>· <b>标准</b>：ZZ 口径，必须有90日涨停记忆，适合找疑似庄股低位企稳</div>
        <div>· <b>严选</b>：二次收紧为高控盘代理强确认、20/30日深跌、当日翻红确认</div>
        <div>· 参考阈值：20日≤-14%、30日≤-14%、量比0.8~2.5、必须命中控盘代理</div>
      </Sec>
      <Sec title="④ 买卖纪律">
        <div>· 买点：尾盘看承接，缩量不破当日均价/前低才试错</div>
        <div>· 卖点：反抽到MA10/MA20、放量滞涨、长上影先减仓</div>
        <div>· 止损：跌破20日低点或买入价-5%，次日不修复离场</div>
      </Sec>
      <div className="mt-1 pt-2 border-t fin-divider text-[10px] fin-text-tertiary leading-relaxed">
        注意：4B 的“高控盘代理”不等同草元原站 is_gao_kong 原值，界面会明确标注，不把代理数据当真实字段。
      </div>
    </div>
  );
};

const formatAmount = (value: number): string => {
  if (!Number.isFinite(value)) return '--';
  if (value === 0) return '0';
  if (value < 0) {
    const abs = Math.abs(value);
    if (abs >= 1e8) return `-${(abs / 1e8).toFixed(2)}亿`;
    if (abs >= 1e4) return `-${(abs / 1e4).toFixed(2)}万`;
    return value.toFixed(0);
  }
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
};

const formatMainFlowSource = (source?: string): string => {
  switch ((source || '').toLowerCase()) {
    case 'eastmoney':
      return '东财';
    case 'tencent-fundflow':
      return '腾讯资金流';
    case 'tencent-pk-proxy':
      return '腾讯盘口代理';
    case 'caoyuan-not-required':
      return '草元非硬门槛';
    default:
      return '未知';
  }
};

const isGateReasonPassed = (reason: string): boolean => {
  if (!reason) return false;
  return !reason.includes('未达') && !reason.includes('未站上') && !reason.includes('异常');
};

const toWatchStock = (item: LowBuyScannerItem): Stock => {
  const preClose = item.price / (1 + item.changePercent / 100);
  return {
    symbol: item.symbol,
    name: item.name,
    price: item.price,
    change: item.price - preClose,
    changePercent: item.changePercent,
    volume: 0,
    amount: item.amount,
    marketCap: item.totalMarketCap > 0 ? `${(item.totalMarketCap / 1e8).toFixed(1)}亿` : '',
    sector: item.industry,
    open: 0,
    high: 0,
    low: 0,
    preClose: Number.isFinite(preClose) ? preClose : 0,
  };
};

export const LowBuyScannerDialog: React.FC<LowBuyScannerDialogProps> = ({
  isOpen,
  title = '低吸选股',
  loading,
  result,
  error,
  onClose,
  onScan,
  onCollectHistory,
  onGetHistoryAutoCollectStatus,
  onUpdateHistoryAutoCollect,
  onAddToWatchlist,
  onOpenStock,
  watchlistSymbols,
  strategyMode = 'lowbuy',
  onReplay,
}) => {
  const isTailLazy = strategyMode === 'taillazy';
  const isLimitPullback = strategyMode === 'limit-pullback';
  const isTripleVolume = strategyMode === 'triple-volume';
  const isTailBuy = strategyMode === 'tail-buy';
  const isHotMoney = strategyMode === 'hot-money';
  const isDipEntry = strategyMode === 'dip-entry';
  const isMonster = strategyMode === 'monster';
  const isMonsterV10 = strategyMode === 'monster-v10';
  const isFormulaKLine = isTripleVolume || isTailBuy || isHotMoney || isDipEntry || isMonster || isMonsterV10;
  const isCaoYuanStandard = strategyMode === 'caoyuan-standard4a';
  const isCaoYuanZhuang = strategyMode === 'caoyuan-zhuang4b';
  const isCaoYuan = isCaoYuanStandard || isCaoYuanZhuang;
  const [replayDate, setReplayDate] = useState('');
  const isReplay = !!result?.items?.[0]?.nextDate;
  const isLbReplay = !!result?.items?.[0]?.replayExitReason;
  const exitReasonCN = (r?: string): string => {
    if (!r) return '';
    const half = r.startsWith('half_');
    const key = half ? r.slice(5) : r;
    const m: Record<string, string> = { stop_loss: '止损-5%', ma10: '破10线', turnover: '换手>12%', time_stop: '5日<3%', take_profit: '止盈+15%', window_end: '未到期' };
    const base = m[key] || key;
    return half ? `减半·${base}` : base;
  };
  const [showBatch, setShowBatch] = useState(false);
  const [batchStart, setBatchStart] = useState('2024-01-01');
  const [batchEnd, setBatchEnd] = useState(new Date().toISOString().slice(0, 10));
  const [batchLoading, setBatchLoading] = useState(false);
  const [batchResult, setBatchResult] = useState<TailLazyBatchResult | null>(null);
  const [lbBatchResult, setLbBatchResult] = useState<LowBuyBatchResult | null>(null);
  const runBatch = async () => {
    setBatchLoading(true);
    try {
      if (isTailLazy) {
        setBatchResult(await runTailLazyBatchReplay(batchStart, batchEnd));
      } else {
        setLbBatchResult(await runLowBuyBatchReplay(batchStart, batchEnd, 3, maxChangePct));
      }
    } catch {
      setBatchResult(null);
      setLbBatchResult(null);
    } finally {
      setBatchLoading(false);
    }
  };
  const watchSet = new Set((watchlistSymbols || []).map(s => String(s).toLowerCase()));
  const [limit, setLimit] = useState(30);
  const [includeBeijing, setIncludeBeijing] = useState(false);
  const [selected, setSelected] = useState<LowBuyScannerItem | null>(null);
  const [collectingHistory, setCollectingHistory] = useState(false);
  const [historyCollectResult, setHistoryCollectResult] = useState<HistoryCollectResult | null>(null);
  const [historyCollectError, setHistoryCollectError] = useState('');
  const [autoCollectStatus, setAutoCollectStatus] = useState<HistoryAutoCollectStatus | null>(null);
  const [autoCollectSaving, setAutoCollectSaving] = useState(false);
  const [showHistoryPanel, setShowHistoryPanel] = useState(false);
  const [historyFilterEnabled, setHistoryFilterEnabled] = useState(false);
  const [historyTurnoverDays, setHistoryTurnoverDays] = useState(3);
  const [historyTurnoverMax, setHistoryTurnoverMax] = useState(3);
  const [historyMainFlowDays, setHistoryMainFlowDays] = useState(3);
  const [historyMainFlowPositiveDays, setHistoryMainFlowPositiveDays] = useState(2);
  const [historyMAPeriod, setHistoryMAPeriod] = useState(10);
  const [maxChangePct, setMaxChangePct] = useState(1.5); // 当日涨幅上限，回测最优≈0
  const [caoYuanStrict, setCaoYuanStrict] = useState(false);
  const [monsterHistoryDate, setMonsterHistoryDate] = useState('');
  const [btResult, setBtResult] = useState<BacktestResult | null>(null);
  const [btLoading, setBtLoading] = useState(false);
  const [showRules, setShowRules] = useState(false);
  const [showNextDayReview, setShowNextDayReview] = useState(false);
  const [batchSelectMode, setBatchSelectMode] = useState(false);
  const [batchSelectedSymbols, setBatchSelectedSymbols] = useState<string[]>([]);
  const strategySource = isTailLazy
    ? 'taillazy-v2'
    : isLimitPullback
      ? 'limit-pullback-v1'
      : isTripleVolume
        ? 'triple-volume-v5'
      : isTailBuy
        ? 'tail-buy-v6'
      : isHotMoney
        ? 'hot-money-v7'
      : isDipEntry
        ? 'dip-entry-v8'
      : isMonster
        ? 'monster-v9'
      : isMonsterV10
        ? 'monster-v10'
      : isCaoYuanStandard
        ? (caoYuanStrict ? 'caoyuan-standard4a-strict' : 'caoyuan-standard4a')
        : isCaoYuanZhuang
          ? (caoYuanStrict ? 'caoyuan-zhuang4b-strict' : 'caoyuan-zhuang4b')
          : 'lowbuy-v1';
  const reviewStrategyName = isCaoYuanStandard
    ? `草元标准 4A${caoYuanStrict ? ' · 严选' : ' · 标准'}`
    : isCaoYuanZhuang
      ? `草元抓庄 4B${caoYuanStrict ? ' · 严选' : ' · 标准'}`
      : title;

  const buildScanRequest = (overrides: Partial<LowBuyScannerRequest> = {}): LowBuyScannerRequest => ({
    limit,
    includeBeijing,
    historyFilterEnabled: isCaoYuan || isLimitPullback || isFormulaKLine ? false : historyFilterEnabled,
    historyTurnoverDays,
    historyTurnoverMax,
    historyMainFlowDays,
    historyMainFlowPositiveDays,
    historyMAPeriod,
    maxChangePct: isCaoYuan || isLimitPullback || isFormulaKLine ? undefined : maxChangePct,
    caoYuanStrict: isCaoYuan ? caoYuanStrict : undefined,
    ...overrides,
  });

  const runParamBacktest = async () => {
    setBtLoading(true);
    try {
      const res = await runBacktest({ days: 250, topN: 5, entryRule: 'next_open', sellRule: 'fast', maxChangePct });
      setBtResult(res);
    } finally {
      setBtLoading(false);
    }
  };

  useEffect(() => {
    if (!isOpen) return;
    setShowHistoryPanel(false);
    setBatchSelectMode(false);
    setBatchSelectedSymbols([]);
    if (result && result.items.length > 0) {
      setSelected(result.items[0]);
    } else {
      setSelected(null);
    }
  }, [isOpen, strategyMode]);

  useEffect(() => {
    if (!isOpen) return;
    if (result && result.items.length > 0) {
      setSelected(result.items[0]);
    } else {
      setSelected(null);
    }
  }, [isOpen, result]);

  useEffect(() => {
    if (!isOpen) return;
    void onGetHistoryAutoCollectStatus()
      .then(status => {
        if (status) setAutoCollectStatus(status);
      })
      .catch(() => undefined);
  }, [isOpen, onGetHistoryAutoCollectStatus]);

  if (!isOpen) return null;

  const batchSelectedSet = new Set(batchSelectedSymbols);
  const batchStocks = (result?.items || [])
    .filter(item => batchSelectedSet.has(item.symbol))
    .map(toWatchStock);
  const toggleBatchStock = (symbol: string) => {
    setBatchSelectedSymbols(prev => (
      prev.includes(symbol) ? prev.filter(item => item !== symbol) : [...prev, symbol]
    ));
  };
  const exitBatchMode = () => {
    setBatchSelectMode(false);
    setBatchSelectedSymbols([]);
  };

  const triggerTotal = isTailLazy
    ? 7
    : isCaoYuanStandard
      ? (caoYuanStrict ? 9 : 7)
      : isCaoYuanZhuang
        ? (caoYuanStrict ? 10 : 8)
        : isLimitPullback
          ? 8
          : isTripleVolume || isDipEntry
            ? 3
          : isTailBuy
            ? 4
          : isHotMoney || isMonster || isMonsterV10
            ? 5
          : (historyFilterEnabled ? 7 : 4);

  const runScan = () => {
    onScan(buildScanRequest());
  };

  const runMonsterHistoryScan = () => {
    if (!monsterHistoryDate) return;
    onScan(buildScanRequest({ historyPickDate: monsterHistoryDate }));
  };

  const runCaoYuanWithLevel = (strict: boolean) => {
    setCaoYuanStrict(strict);
    onScan({
      limit,
      includeBeijing,
      historyFilterEnabled: false,
      historyTurnoverDays,
      historyTurnoverMax,
      historyMainFlowDays,
      historyMainFlowPositiveDays,
      historyMAPeriod,
      caoYuanStrict: strict,
    });
  };

  const toggleHistoryPanel = () => {
    setShowHistoryPanel(prev => !prev);
  };

  const confirmHistoryFilter = () => {
    setHistoryFilterEnabled(true);
    setShowHistoryPanel(false);
    onScan({
      limit,
      includeBeijing,
      historyFilterEnabled: true,
      historyTurnoverDays,
      historyTurnoverMax,
      historyMainFlowDays,
      historyMainFlowPositiveDays,
      historyMAPeriod,
      maxChangePct,
    });
  };

  const handleCollectHistory = async () => {
    if (collectingHistory) return;
    setCollectingHistory(true);
    setHistoryCollectError('');
    try {
      const collectResult = await onCollectHistory({ includeBeijing });
      if (!collectResult) {
        setHistoryCollectError('当前为浏览器预览模式，请从 Wails 开发入口打开后再采集。');
        setHistoryCollectResult(null);
        return;
      }
      setHistoryCollectResult(collectResult);
      if (collectResult.status === 'failed') {
        setHistoryCollectError(collectResult.message || '历史采集失败');
      }
    } catch (err) {
      setHistoryCollectError(err instanceof Error ? err.message : '历史采集失败');
      setHistoryCollectResult(null);
    } finally {
      setCollectingHistory(false);
    }
  };

  const handleToggleAutoCollect = async (enabled: boolean) => {
    setAutoCollectSaving(true);
    setHistoryCollectError('');
    try {
      const next = await onUpdateHistoryAutoCollect({
        enabled,
        collectStart: autoCollectStatus?.collectStart || '16:00',
        collectEnd: autoCollectStatus?.collectEnd || '17:00',
        includeBeijing,
      });
      if (next) {
        setAutoCollectStatus(next);
      }
    } catch (err) {
      setHistoryCollectError(err instanceof Error ? err.message : '自动采集配置保存失败');
    } finally {
      setAutoCollectSaving(false);
    }
  };

  return (
    <>
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1320px] h-[840px] max-w-[96vw] max-h-[92vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        {showBatch && (
          <div className="absolute inset-0 z-[90] flex items-center justify-center bg-black/60 backdrop-blur-sm">
            <div className="w-[820px] max-w-[92%] fin-panel-strong border fin-divider rounded-xl shadow-2xl p-4">
              <div className="flex items-center justify-between mb-3">
                <div>
                  <div className="text-sm font-semibold fin-text-primary">{isTailLazy ? '尾盘懒人 · 批量历史复盘' : '低吸 · 批量历史复盘'}</div>
                  <div className="text-[11px] fin-text-tertiary">{isTailLazy ? '区间内逐日跑筛选，统计次日表现的整体真实命中率/期望（扣成本0.35%）' : '区间内逐日选Top3、按机械纪律持有，统计整体胜率/期望/超额alpha（T+1开盘进、扣成本0.35%）'}</div>
                </div>
                <button onClick={() => setShowBatch(false)} className="p-1 rounded fin-hover"><X className="h-4 w-4 fin-text-tertiary" /></button>
              </div>
              <div className="flex items-center gap-2 mb-3 text-xs">
                <span className="fin-text-tertiary">起</span>
                <input type="date" value={batchStart} onChange={(e) => setBatchStart(e.target.value)} className="px-2 py-1 rounded fin-input text-xs" />
                <span className="fin-text-tertiary">至</span>
                <input type="date" value={batchEnd} onChange={(e) => setBatchEnd(e.target.value)} className="px-2 py-1 rounded fin-input text-xs" />
                <button onClick={runBatch} disabled={batchLoading} className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-accent/55 text-accent-2 bg-accent/10 hover:bg-accent/15 disabled:opacity-50">
                  {batchLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <BarChart3 className="h-3.5 w-3.5" />}
                  <span>{batchLoading ? '统计中(约10-40秒)' : '开始批量复盘'}</span>
                </button>
                <span className="fin-text-tertiary">区间越长越慢，库自 2023-03 起</span>
              </div>
              {isTailLazy && batchResult && batchResult.warning && !batchResult.rows?.length && (
                <div className="text-xs text-amber-300 px-2 py-3">{batchResult.warning}</div>
              )}
              {isTailLazy && batchResult && batchResult.rows && batchResult.rows.length > 0 && (
                <>
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="fin-text-tertiary border-b fin-divider-soft">
                        <th className="text-left px-2 py-1.5">区间</th>
                        <th className="text-right px-2 py-1.5">信号数</th>
                        <th className="text-right px-2 py-1.5" title="次日最高≥3%（理想止盈可及）的占比">次日高≥3%</th>
                        <th className="text-right px-2 py-1.5">≥5%</th>
                        <th className="text-right px-2 py-1.5" title="次日开盘卖的平均收益">次日开</th>
                        <th className="text-right px-2 py-1.5" title="次日收盘卖的平均收益">次日收</th>
                        <th className="text-right px-2 py-1.5" title="冲到3%就止盈、否则次日收盘卖；扣成本">止盈3%胜率</th>
                        <th className="text-right px-2 py-1.5">期望/笔</th>
                      </tr>
                    </thead>
                    <tbody>
                      {batchResult.rows.map((r) => {
                        const isTotal = r.label === '合计';
                        return (
                          <tr key={r.label} className={`border-b fin-divider-soft ${isTotal ? 'font-semibold' : ''}`}>
                            <td className={`px-2 py-1.5 ${isTotal ? 'fin-text-primary' : 'fin-text-secondary'}`}>{r.label}</td>
                            <td className="px-2 py-1.5 text-right fin-text-secondary">{r.samples}</td>
                            <td className={`px-2 py-1.5 text-right ${r.hit3Rate >= 50 ? 'text-rose-300' : 'text-amber-300'}`}>{r.hit3Rate.toFixed(1)}%</td>
                            <td className="px-2 py-1.5 text-right fin-text-tertiary">{r.hit5Rate.toFixed(1)}%</td>
                            <td className={`px-2 py-1.5 text-right ${r.avgOpen >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.avgOpen >= 0 ? '+' : ''}{r.avgOpen.toFixed(2)}%</td>
                            <td className={`px-2 py-1.5 text-right ${r.avgClose >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.avgClose >= 0 ? '+' : ''}{r.avgClose.toFixed(2)}%</td>
                            <td className="px-2 py-1.5 text-right fin-text-secondary">{r.tpWinRate.toFixed(1)}%</td>
                            <td className={`px-2 py-1.5 text-right font-mono ${r.tpExpectancy >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.tpExpectancy >= 0 ? '+' : ''}{r.tpExpectancy.toFixed(2)}%</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                  <div className="mt-2 text-[11px] fin-text-tertiary leading-relaxed">
                    判读：盯<b className="fin-text-secondary">期望/笔</b>和<b className="fin-text-secondary">次日开/收</b>——为正才真赚钱。"次日高"是盘中最高点(难精准卖在那)，仅供参考。
                  </div>
                </>
              )}

              {!isTailLazy && lbBatchResult && lbBatchResult.warning && !lbBatchResult.rows?.length && (
                <div className="text-xs text-amber-300 px-2 py-3">{lbBatchResult.warning}</div>
              )}
              {!isTailLazy && lbBatchResult && lbBatchResult.rows && lbBatchResult.rows.length > 0 && (
                <>
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="fin-text-tertiary border-b fin-divider-soft">
                        <th className="text-left px-2 py-1.5">区间</th>
                        <th className="text-right px-2 py-1.5">交易</th>
                        <th className="text-right px-2 py-1.5">胜率</th>
                        <th className="text-right px-2 py-1.5" title="每笔扣成本净收益均值">期望值/笔</th>
                        <th className="text-right px-2 py-1.5" title="盈利单均值/亏损单均值">赔率</th>
                        <th className="text-right px-2 py-1.5">盈利因子</th>
                        <th className="text-right px-2 py-1.5">单笔最大亏</th>
                        <th className="text-right px-2 py-1.5" title="相对同期等权全A的超额，>0才算真本事">超额alpha</th>
                        <th className="text-right px-2 py-1.5">持有</th>
                      </tr>
                    </thead>
                    <tbody>
                      {lbBatchResult.rows.map((r) => {
                        const isTotal = r.label === '合计';
                        return (
                          <tr key={r.label} className={`border-b fin-divider-soft ${isTotal ? 'font-semibold' : ''}`}>
                            <td className={`px-2 py-1.5 ${isTotal ? 'fin-text-primary' : 'fin-text-secondary'}`}>{r.label}</td>
                            <td className="px-2 py-1.5 text-right fin-text-secondary">{r.trades}</td>
                            <td className={`px-2 py-1.5 text-right ${r.winRate >= 50 ? 'text-rose-300' : 'fin-text-secondary'}`}>{r.winRate.toFixed(1)}%</td>
                            <td className={`px-2 py-1.5 text-right font-mono ${r.expectancy >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.expectancy >= 0 ? '+' : ''}{r.expectancy.toFixed(2)}%</td>
                            <td className={`px-2 py-1.5 text-right ${r.payoffRatio >= 1 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.payoffRatio.toFixed(2)}</td>
                            <td className={`px-2 py-1.5 text-right ${r.profitFactor >= 1 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.profitFactor.toFixed(2)}</td>
                            <td className="px-2 py-1.5 text-right text-emerald-300">{r.maxLoss.toFixed(2)}%</td>
                            <td className={`px-2 py-1.5 text-right font-mono ${r.excess >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{r.excess >= 0 ? '+' : ''}{r.excess.toFixed(2)}%</td>
                            <td className="px-2 py-1.5 text-right fin-text-tertiary">{r.avgHold.toFixed(1)}天</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                  <div className="mt-2 text-[11px] fin-text-tertiary leading-relaxed">
                    判读：<b className="fin-text-secondary">期望值/笔</b>正=绝对赚钱；<b className="fin-text-secondary">超额alpha</b>正=真跑赢市场。低吸通常期望微正、alpha≈0(防守型beta)。
                  </div>
                </>
              )}

              {((isTailLazy && !batchResult) || (!isTailLazy && !lbBatchResult)) && !batchLoading && (
                <div className="text-xs fin-text-tertiary px-2 py-6 text-center">选好区间，点「开始批量复盘」</div>
              )}
            </div>
          </div>
        )}
        <div className="flex items-center justify-between px-5 py-3 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Search className="h-5 w-5 text-accent-2" />
            <div className="text-left">
              <div className="text-sm font-semibold fin-text-primary">{title}</div>
            </div>
            <div className="relative">
              <button
                onClick={() => setShowRules(v => !v)}
                className={`inline-flex items-center gap-1 px-2 py-1 ml-1 text-[11px] rounded-lg border transition-colors ${
                  showRules ? 'border-accent/60 text-accent-2 bg-accent/10' : 'fin-divider fin-text-tertiary hover:border-accent/50'
                }`}
                title="查看 V1.2 低吸规则要点"
              >
                <Info className="h-3.5 w-3.5" />
                <span>规则说明</span>
              </button>
              {showRules && (isTailLazy
                ? <TailLazyRulesPopover onClose={() => setShowRules(false)} />
                : isLimitPullback
                  ? <LimitPullbackRulesPopover onClose={() => setShowRules(false)} />
                  : isTripleVolume
                    ? <TripleVolumeRulesPopover onClose={() => setShowRules(false)} />
                  : isTailBuy
                    ? <FormulaKLineRulesPopover kind="tail-buy" onClose={() => setShowRules(false)} />
                  : isHotMoney
                    ? <FormulaKLineRulesPopover kind="hot-money" onClose={() => setShowRules(false)} />
                  : isDipEntry
                    ? <FormulaKLineRulesPopover kind="dip-entry" onClose={() => setShowRules(false)} />
                  : isMonster
                    ? <FormulaKLineRulesPopover kind="monster" onClose={() => setShowRules(false)} />
                  : isMonsterV10
                    ? <FormulaKLineRulesPopover kind="monster-v10" onClose={() => setShowRules(false)} />
                : isCaoYuanStandard
                  ? <CaoYuanStandardRulesPopover onClose={() => setShowRules(false)} />
                  : isCaoYuanZhuang
                    ? <CaoYuanZhuangRulesPopover onClose={() => setShowRules(false)} />
                    : <RulesPopover onClose={() => setShowRules(false)} />)}
            </div>
          </div>
          <div className="flex items-center gap-2">
            {isCaoYuan && (
              <div
                className="inline-flex items-center rounded-lg border fin-divider overflow-hidden text-xs"
                title="标准=原策略口径；严选=在当前策略内按严格档做二次筛选，信号更少更集中"
              >
                <button
                  type="button"
                  onClick={() => caoYuanStrict && runCaoYuanWithLevel(false)}
                  disabled={loading}
                  className={`px-3 py-1.5 transition-colors ${!caoYuanStrict ? 'text-accent-2 bg-accent/10' : 'fin-text-secondary hover:bg-slate-500/10'} disabled:opacity-50`}
                >
                  标准
                </button>
                <button
                  type="button"
                  onClick={() => !caoYuanStrict && runCaoYuanWithLevel(true)}
                  disabled={loading}
                  className={`px-3 py-1.5 border-l fin-divider-soft transition-colors ${caoYuanStrict ? 'text-accent-2 bg-accent/10' : 'fin-text-secondary hover:bg-slate-500/10'} disabled:opacity-50`}
                >
                  严选
                </button>
              </div>
            )}
            {onReplay && !isCaoYuan && !isLimitPullback && !isFormulaKLine && (
              <div className="flex items-center gap-1.5 mr-1">
                <span className="text-[11px] fin-text-tertiary">复盘日</span>
                <input
                  type="date"
                  value={replayDate}
                  onChange={(e) => setReplayDate(e.target.value)}
                  max={new Date().toISOString().slice(0, 10)}
                  className="px-2 py-1 rounded fin-input text-xs"
                />
                <button
                  onClick={() => replayDate && onReplay(replayDate)}
                  disabled={loading || !replayDate}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded-lg border border-accent/55 text-accent-2 bg-accent/10 hover:bg-accent/15 transition-colors disabled:opacity-50"
                  title="用该交易日的历史数据跑筛选，并带出次日表现"
                >
                  {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                  <span>复盘此日</span>
                </button>
                <button
                  onClick={() => setShowBatch(true)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded-lg border border-amber-400/50 text-amber-300 bg-amber-500/10 hover:bg-amber-500/15 transition-colors"
                  title="选区间，统计整体真实命中率/期望（验证策略到底灵不灵）"
                >
                  <BarChart3 className="h-3.5 w-3.5" />
                  <span>批量复盘</span>
                </button>
              </div>
            )}
            <div className="relative">
              {!isTailLazy && !isCaoYuan && !isLimitPullback && !isFormulaKLine && (
              <button
                onClick={toggleHistoryPanel}
                disabled={loading}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border transition-colors ${
                  historyFilterEnabled
                    ? 'border-accent/60 text-accent-2 bg-accent/10'
                    : 'fin-divider hover:border-accent/50'
                }`}
                title="打开历史质量筛选设置，确认后才会追加历史维度筛选"
              >
                <SlidersHorizontal className="h-3.5 w-3.5" />
                <span>调节历史维度</span>
              </button>
              )}
              {!isTailLazy && !isCaoYuan && !isLimitPullback && !isFormulaKLine && showHistoryPanel && (
                <div className="absolute right-0 top-[calc(100%+8px)] z-[70] w-[420px] fin-panel-strong border fin-divider rounded-lg shadow-2xl p-3 text-xs">
                  <div className="flex items-start justify-between gap-3 mb-3">
                    <div>
                      <div className="text-sm font-semibold fin-text-primary">自定维度筛选</div>
                      <div className="mt-0.5 text-[11px] fin-text-tertiary">涨幅上限始终生效；其余维度确认后在 V1.1 候选上继续收紧。</div>
                    </div>
                    <button
                      onClick={() => setShowHistoryPanel(false)}
                      className="p-1 rounded fin-hover"
                      title="关闭"
                    >
                      <X className="h-3.5 w-3.5 fin-text-tertiary" />
                    </button>
                  </div>

                  <div className="space-y-2.5">
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">涨幅上限</span>
                      <input
                        type="number"
                        min={-3}
                        max={3}
                        step={0.5}
                        value={maxChangePct}
                        onChange={(e) => setMaxChangePct(Math.min(3, Math.max(-3, Number(e.target.value))))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[0, 0.5, 1.5].map(v => (
                          <button
                            key={`maxchg-${v}`}
                            type="button"
                            onClick={() => setMaxChangePct(v)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              maxChangePct === v
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {v === 0 ? '0%' : `+${v}%`}
                          </button>
                        ))}
                        <span className="text-[10px] fin-text-tertiary ml-0.5">最优≈0</span>
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">连续天数</span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        value={historyTurnoverDays}
                        onChange={(e) => setHistoryTurnoverDays(Math.min(20, Math.max(1, Number(e.target.value) || 3)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`turnover-day-${day}`}
                            type="button"
                            onClick={() => setHistoryTurnoverDays(day)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyTurnoverDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">换手低于</span>
                      <input
                        type="number"
                        min={0.1}
                        max={20}
                        step={0.1}
                        value={historyTurnoverMax}
                        onChange={(e) => setHistoryTurnoverMax(Math.min(20, Math.max(0.1, Number(e.target.value) || 3)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[2, 3, 5].map(rate => (
                          <button
                            key={`turnover-rate-${rate}`}
                            type="button"
                            onClick={() => setHistoryTurnoverMax(rate)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyTurnoverMax === rate
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {rate}%
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">主力观察</span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        value={historyMainFlowDays}
                        onChange={(e) => {
                          const next = Math.min(20, Math.max(1, Number(e.target.value) || 3));
                          setHistoryMainFlowDays(next);
                          setHistoryMainFlowPositiveDays(prev => Math.min(prev, next));
                        }}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`flow-day-${day}`}
                            type="button"
                            onClick={() => {
                              setHistoryMainFlowDays(day);
                              setHistoryMainFlowPositiveDays(prev => Math.min(prev, day));
                            }}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMainFlowDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">至少为正</span>
                      <input
                        type="number"
                        min={1}
                        max={historyMainFlowDays}
                        value={historyMainFlowPositiveDays}
                        onChange={(e) => setHistoryMainFlowPositiveDays(Math.min(historyMainFlowDays, Math.max(1, Number(e.target.value) || 2)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[1, 2, 3].map(day => (
                          <button
                            key={`positive-day-${day}`}
                            type="button"
                            disabled={day > historyMainFlowDays}
                            onClick={() => setHistoryMainFlowPositiveDays(day)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMainFlowPositiveDays === day
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            } ${day > historyMainFlowDays ? 'opacity-40 cursor-not-allowed' : ''}`}
                          >
                            {day}日
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="grid grid-cols-[88px_96px_1fr] items-center gap-2">
                      <span className="fin-text-secondary">站上 MA</span>
                      <input
                        type="number"
                        min={3}
                        max={60}
                        value={historyMAPeriod}
                        onChange={(e) => setHistoryMAPeriod(Math.min(60, Math.max(3, Number(e.target.value) || 10)))}
                        className="w-full px-2 py-1.5 rounded fin-input text-xs"
                      />
                      <div className="flex items-center gap-1">
                        {[5, 10, 20].map(period => (
                          <button
                            key={`ma-${period}`}
                            type="button"
                            onClick={() => setHistoryMAPeriod(period)}
                            className={`px-2 py-1 rounded border transition-colors ${
                              historyMAPeriod === period
                                ? 'border-accent/60 text-accent-2 bg-accent/10'
                                : 'fin-divider fin-text-secondary hover:border-accent/45'
                            }`}
                          >
                            MA{period}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 rounded border border-amber-500/25 bg-amber-500/8 px-2 py-1.5 text-[11px] text-amber-200">
                    默认：连续3日换手&lt;3%，近3日主力至少2日净流入为正，站上MA10。
                  </div>

                  {/* 回测此参数：用诚实引擎(T+1开盘/减半/扣成本)在 250 日历史上验证当前涨幅上限 */}
                  <div className="mt-3 pt-3 border-t fin-divider">
                    <div className="flex items-center justify-between gap-2 mb-2">
                      <div className="text-[11px] fin-text-tertiary">
                        回测当前涨幅上限 <span className="text-accent-2 font-medium">{maxChangePct === 0 ? '0%' : `${maxChangePct > 0 ? '+' : ''}${maxChangePct}%`}</span> · 250日 · T+1开盘/减半/扣成本
                      </div>
                      <button
                        onClick={runParamBacktest}
                        disabled={btLoading}
                        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border border-accent/55 text-accent-2 bg-accent/10 hover:bg-accent/15 transition-colors disabled:opacity-50"
                      >
                        {btLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <BarChart3 className="h-3.5 w-3.5" />}
                        <span>{btLoading ? '回测中(约5-15秒)' : '回测此参数'}</span>
                      </button>
                    </div>
                    {btResult && btResult.totalTrades > 0 && (
                      <StrategyScorecard
                        dense
                        metrics={{
                          trades: btResult.totalTrades,
                          winRate: btResult.winRate,
                          expectancy: btResult.avgReturn,
                          payoffRatio: btResult.payoffRatio,
                          profitFactor: btResult.profitFactor,
                          maxLoss: btResult.maxLossPct,
                          excess: btResult.excessPct,
                          benchmark: btResult.benchmarkPct,
                          avgHold: btResult.avgHoldDays,
                        }}
                      />
                    )}
                    {btResult && btResult.totalTrades > 0 && (
                      <div className={`mt-1.5 text-[11px] ${btResult.excessPct >= 0 ? 'text-red-300' : 'text-green-300'}`}>
                        {btResult.excessPct >= 0
                          ? `扣成本后跑赢同期市场 +${btResult.excessPct.toFixed(2)}%/笔，该上限有效。`
                          : `扣成本后跑输同期市场 ${btResult.excessPct.toFixed(2)}%/笔(alpha为负)，建议调整上限再测。`}
                      </div>
                    )}
                    {btResult && btResult.totalTrades === 0 && (
                      <div className="text-[11px] text-amber-300">该窗口无交易（历史字段可能未补齐，先点"补齐回测数据"）。</div>
                    )}
                  </div>

                  <div className="mt-3 flex items-center justify-between gap-2">
                    <button
                      onClick={() => {
                        setHistoryFilterEnabled(false);
                        setShowHistoryPanel(false);
                        onScan({
                          limit,
                          includeBeijing,
                          historyFilterEnabled: false,
                          historyTurnoverDays,
                          historyTurnoverMax,
                          historyMainFlowDays,
                          historyMainFlowPositiveDays,
                          historyMAPeriod,
                          maxChangePct,
                        });
                      }}
                      disabled={loading}
                      className="px-2.5 py-1.5 rounded-lg border fin-divider fin-text-secondary hover:border-accent/45 transition-colors"
                    >
                      仅筛涨幅
                    </button>
                    <button
                      onClick={confirmHistoryFilter}
                      disabled={loading}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-accent/55 text-accent-2 bg-accent/10 hover:bg-accent/15 transition-colors"
                    >
                      {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SlidersHorizontal className="h-3.5 w-3.5" />}
                      <span>确认筛选</span>
                    </button>
                  </div>
                </div>
              )}
            </div>
            {isMonster && (
              <div
                className="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg border border-amber-400/35 bg-amber-500/8"
                title="按所选交易日做历史时间选股：只使用该日及以前数据，不看后续K线"
              >
                <span className="text-[11px] text-amber-200">历史时间</span>
                <input
                  type="date"
                  value={monsterHistoryDate}
                  onChange={(e) => setMonsterHistoryDate(e.target.value)}
                  max={new Date().toISOString().slice(0, 10)}
                  className="w-[126px] px-2 py-1 rounded fin-input text-xs"
                />
                <button
                  type="button"
                  onClick={runMonsterHistoryScan}
                  disabled={loading || !monsterHistoryDate}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded-md border border-amber-400/45 text-amber-200 bg-amber-500/10 hover:bg-amber-500/15 transition-colors disabled:opacity-50"
                >
                  {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                  <span>选股</span>
                </button>
              </div>
            )}
            <button
              onClick={runScan}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/50 transition-colors"
              title="开始扫描"
            >
              {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              <span>{loading ? '扫描中...' : '重新扫描'}</span>
            </button>
            <button
              type="button"
              onClick={() => setShowNextDayReview(true)}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-amber-400/45 text-amber-200 bg-amber-500/8 hover:bg-amber-500/15 transition-colors"
              title="对该策略上一交易日选出的股票做次日收盘复盘"
            >
              <BarChart3 className="h-3.5 w-3.5" />
              <span>次日复盘</span>
            </button>
            <button
              onClick={handleCollectHistory}
              disabled={loading || collectingHistory}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/50 transition-colors"
              title="采集今日全A快照到本地 history.db，用于后续回踩测试"
            >
              {collectingHistory ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Database className="h-3.5 w-3.5" />}
              <span>{collectingHistory ? '采集中...' : '采集历史'}</span>
            </button>
            <label
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-lg border fin-divider hover:border-accent/45 transition-colors"
              title={`每日盘后自动采集全A快照，默认 ${autoCollectStatus?.collectStart || '16:00'}-${autoCollectStatus?.collectEnd || '17:00'}，同一天只采一次`}
            >
              {autoCollectSaving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Database className="h-3.5 w-3.5 fin-text-tertiary" />}
              <input
                type="checkbox"
                checked={Boolean(autoCollectStatus?.enabled)}
                disabled={autoCollectSaving}
                onChange={(e) => void handleToggleAutoCollect(e.target.checked)}
              />
              <span className={autoCollectStatus?.enabled ? 'text-accent-2' : 'fin-text-secondary'}>盘后自动</span>
            </label>
            <button onClick={onClose} className="p-2 rounded-lg fin-hover transition-colors">
              <X className="h-4 w-4 fin-text-secondary" />
            </button>
          </div>
        </div>

        <div className="px-4 py-2 border-b fin-divider-soft flex items-center gap-4 text-xs">
          <label className="flex items-center gap-2">
            <span className="fin-text-secondary">返回数量</span>
            <input
              type="number"
              min={10}
              max={200}
              value={limit}
              onChange={(e) => setLimit(Math.min(200, Math.max(10, Number(e.target.value) || 30)))}
              className="w-20 px-2 py-1 rounded fin-input text-xs"
            />
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={includeBeijing}
              disabled={isFormulaKLine}
              onChange={(e) => setIncludeBeijing(e.target.checked)}
            />
            <span className="fin-text-secondary">{isFormulaKLine ? '主板10cm口径（自动剔除北交所）' : '包含北交所'}</span>
          </label>
          {result && (
            <div className="ml-auto flex items-center gap-3 fin-text-tertiary">
              <span>全市场 {result.universeCount}</span>
              <span>候选 {result.candidateCount}</span>
              <span>入选 {result.selectedCount}</span>
              <span>{result.asOf}</span>
            </div>
          )}
        </div>

        {result && isReplay && (() => {
          const picks = result.items.filter(i => i.nextDate);
          const n = picks.length;
          const wins = picks.filter(i => (i.nextHighGainPct ?? 0) >= 3).length;
          const avgHigh = n ? picks.reduce((s, i) => s + (i.nextHighGainPct ?? 0), 0) / n : 0;
          const avgClose = n ? picks.reduce((s, i) => s + (i.nextCloseGainPct ?? 0), 0) / n : 0;
          const winRate = n ? (wins * 100 / n) : 0;
          return (
            <div className="px-4 py-2 border-b fin-divider-soft text-xs flex items-center gap-4 flex-wrap bg-amber-500/5">
              <span className="fin-text-secondary">复盘 <b className="fin-text-primary">{result.asOf}</b> · 次日表现({n}只)</span>
              <span className="fin-text-secondary">次日最高≥3%命中 <b className={winRate >= 50 ? 'text-rose-300' : 'text-amber-300'}>{winRate.toFixed(0)}%</b> ({wins}/{n})</span>
              <span className="fin-text-secondary">次日最高均值 <b className={avgHigh >= 0 ? 'text-rose-300' : 'text-emerald-300'}>{avgHigh >= 0 ? '+' : ''}{avgHigh.toFixed(2)}%</b></span>
              <span className="fin-text-secondary">次日收盘均值 <b className={avgClose >= 0 ? 'text-rose-300' : 'text-emerald-300'}>{avgClose >= 0 ? '+' : ''}{avgClose.toFixed(2)}%</b></span>
            </div>
          );
        })()}

        {result && isLbReplay && (() => {
          const picks = result.items.filter(i => i.replayExitReason);
          const n = picks.length;
          const wins = picks.filter(i => (i.replayReturnPct ?? 0) > 0).length;
          const avg = n ? picks.reduce((s, i) => s + (i.replayReturnPct ?? 0), 0) / n : 0;
          const winRate = n ? (wins * 100 / n) : 0;
          return (
            <div className="px-4 py-2 border-b fin-divider-soft text-xs flex items-center gap-4 flex-wrap bg-amber-500/5">
              <span className="fin-text-secondary">复盘 <b className="fin-text-primary">{result.asOf}</b> · 机械纪律持有结果({n}只)</span>
              <span className="fin-text-secondary">胜率 <b className={winRate >= 50 ? 'text-rose-300' : 'text-amber-300'}>{winRate.toFixed(0)}%</b> ({wins}/{n})</span>
              <span className="fin-text-secondary">平均净收益 <b className={avg >= 0 ? 'text-rose-300' : 'text-emerald-300'}>{avg >= 0 ? '+' : ''}{avg.toFixed(2)}%</b></span>
              <span className="fin-text-tertiary">T+1开盘进 · 扣成本0.35%</span>
            </div>
          );
        })()}

        {result && !isTailLazy && !isCaoYuan && !isFormulaKLine && !isLbReplay && (
          <div className="px-4 py-2 border-b fin-divider-soft text-xs">
            <div className="flex items-center gap-4 flex-wrap">
            <div className={`inline-flex items-center gap-1 ${result.marketGatePassed ? 'text-emerald-300' : 'text-amber-300'}`}>
              {result.marketGatePassed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
              <span>{result.marketGatePassed ? '大盘过滤通过' : '大盘过滤未通过（仅供观察）'}</span>
            </div>
            <span className="fin-text-tertiary">上证 {result.marketOverview.shPrice.toFixed(2)} / MA20 {result.marketOverview.shMA20.toFixed(2)}</span>
            <span className="fin-text-tertiary">涨停 {result.marketOverview.limitUpCount}</span>
            <span className="fin-text-tertiary">跌停 {result.marketOverview.limitDownCount}</span>
            <span className="fin-text-tertiary">两市成交额 {formatAmount(result.marketOverview.totalAmount)}</span>
            </div>
            {result.marketGateReasons && result.marketGateReasons.length > 0 && (
              <div className="mt-2 grid grid-cols-1 md:grid-cols-2 gap-1.5">
                {result.marketGateReasons.map((reason, idx) => {
                  const passed = isGateReasonPassed(reason);
                  return (
                    <div
                      key={`gate-${idx}`}
                      className={`inline-flex items-center gap-1.5 px-2 py-1 rounded border ${
                        passed
                          ? 'border-emerald-500/30 text-emerald-300 bg-emerald-500/8'
                          : 'border-amber-500/30 text-amber-300 bg-amber-500/8'
                      }`}
                    >
                      {passed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
                      <span>{reason}</span>
                    </div>
                  );
                })}
              </div>
            )}
            {result.warning && (
              <div className="mt-2 inline-flex items-center gap-1.5 px-2 py-1 rounded border border-amber-500/35 text-amber-300 bg-amber-500/8">
                <AlertTriangle className="h-3.5 w-3.5" />
                <span>{result.warning}</span>
              </div>
            )}
          </div>
        )}

        {(historyCollectResult || historyCollectError) && (
          <div className="px-4 py-2 border-b fin-divider-soft text-xs">
            {historyCollectResult && (
              <div className={`inline-flex items-center gap-1.5 px-2 py-1 rounded border ${
                historyCollectResult.status === 'success'
                  ? 'border-emerald-500/35 text-emerald-300 bg-emerald-500/8'
                  : 'border-amber-500/35 text-amber-300 bg-amber-500/8'
              }`}>
                {historyCollectResult.status === 'success' ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
                <span>
                  历史采集 {historyCollectResult.tradeDate}：全市场 {historyCollectResult.totalCount}，写入 {historyCollectResult.savedCount}，失败 {historyCollectResult.failedCount}，MA{historyCollectResult.maUpdated ? '已回写' : '未回写'}
                </span>
              </div>
            )}
            {historyCollectError && (
              <div className="mt-1 inline-flex items-center gap-1.5 px-2 py-1 rounded border border-red-500/35 text-red-300 bg-red-500/8">
                <AlertTriangle className="h-3.5 w-3.5" />
                <span>{historyCollectError}</span>
              </div>
            )}
          </div>
        )}

        <div className="flex-1 flex overflow-hidden">
          <div className="w-[56%] border-r fin-divider-soft overflow-auto fin-scrollbar">
            {loading && (
              <div className="h-full flex items-center justify-center text-sm fin-text-secondary">
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                正在扫描全A股票...
              </div>
            )}
            {!loading && error && (
              <div className="p-4 text-sm text-red-300">{error}</div>
            )}
            {!loading && !error && result && result.items.length === 0 && (
              <div className="p-4 text-sm fin-text-secondary">未找到符合规则的标的。</div>
            )}
            {!loading && !error && result && result.items.length > 0 && (
              <>
              <div className="flex items-center justify-end gap-2 border-b fin-divider-soft px-2 py-1.5 text-xs fin-panel-strong">
                {batchSelectMode ? (
                  <>
                    <span className="mr-auto fin-text-tertiary">批量模式 · 已选 {batchStocks.length} 只</span>
                    <BatchAddToGroupButton
                      stocks={batchStocks}
                      source={strategySource}
                      onAddToWatchlist={onAddToWatchlist}
                      onDone={exitBatchMode}
                    />
                    <button
                      type="button"
                      onClick={exitBatchMode}
                      className="rounded border border-slate-600/70 px-2.5 py-1.5 text-[11px] font-semibold fin-text-tertiary hover:bg-slate-500/10"
                    >
                      取消
                    </button>
                  </>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setBatchSelectedSymbols([]);
                      setBatchSelectMode(true);
                    }}
                    className="rounded border border-amber-400/45 bg-amber-500/10 px-2.5 py-1.5 text-[11px] font-semibold text-amber-200 hover:bg-amber-500/18"
                  >
                    批量操作
                  </button>
                )}
              </div>
              <table className="w-full text-xs">
                <thead className="sticky top-0 fin-panel-strong border-b fin-divider-soft">
                  <tr>
                    <th className="text-left px-2 py-2">股票</th>
                    <th className="text-right px-2 py-2">评分</th>
                    <th className="text-right px-2 py-2">涨跌</th>
                    <th className="text-right px-2 py-2">换手</th>
                    <th className="text-right px-2 py-2" title="东财主力净流入不可用时，优先补齐腾讯资金流，其次腾讯盘口代理">主力</th>
                    <th className="text-right px-2 py-2">市值</th>
                    {isReplay && <th className="text-right px-2 py-2" title="以当日收盘为买入价，次日 开盘/最高/收盘 涨幅">次日 开/高/收</th>}
                    {isLbReplay && <th className="text-right px-2 py-2" title="按机械纪律(T+1开盘进、扣成本)的持有结果">持有结果</th>}
                    <th className="text-center px-2 py-2">操作</th>
                    {batchSelectMode && <th className="w-8 px-1 py-2" aria-label="批量勾选" />}
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((item) => {
                    const selectedRow = selected?.symbol === item.symbol;
                    return (
                      <tr
                        key={item.symbol}
                        className={`cursor-pointer border-b fin-divider-soft ${selectedRow ? 'bg-accent/10' : 'hover:bg-slate-500/5'}`}
                        onClick={() => setSelected(item)}
                      >
                        <td className="px-2 py-2 text-left">
                          <div className="flex items-center gap-1.5">
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                setSelected(item);
                                void onOpenStock?.(toWatchStock(item));
                              }}
                              className="font-medium fin-text-primary hover:text-accent-2 hover:underline underline-offset-2 text-left"
                              title="打开全屏四图K线"
                            >
                              {item.name}
                            </button>
                            {item.ma10Status === 'hold' && (
                              <span
                                className="inline-flex items-center px-1 py-0.5 rounded text-[9px] leading-none text-rose-300 bg-rose-500/10 border border-rose-400/30 shrink-0"
                                title={`收盘 ≥ MA10(${item.ma10?.toFixed(2)})，回踩未破均线 ✓`}
                              >↑未破MA10</span>
                            )}
                            {item.ma10Status === 'broke' && (
                              <span
                                className="inline-flex items-center px-1 py-0.5 rounded text-[9px] leading-none text-emerald-300/70 bg-emerald-500/10 border border-emerald-400/25 shrink-0"
                                title={`收盘 < MA10(${item.ma10?.toFixed(2)})，已跌破均线`}
                              >↓破MA10</span>
                            )}
                          </div>
                          <div className="fin-text-tertiary">{item.symbol} · {item.capBucket}</div>
                        </td>
                        <td className="px-2 py-2 text-right text-accent-2 font-semibold">{item.score.toFixed(1)}</td>
                        <td className={`px-2 py-2 text-right ${item.changePercent >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                          {item.changePercent >= 0 ? '+' : ''}{item.changePercent.toFixed(2)}%
                        </td>
                        <td className="px-2 py-2 text-right">{item.turnoverRate.toFixed(2)}%</td>
                        <td className={`px-2 py-2 text-right ${item.mainNetInflow >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                          <div>{Number.isFinite(item.mainNetInflow) ? formatAmount(item.mainNetInflow) : '--'}</div>
                          <div className="text-[10px] fin-text-tertiary">{formatMainFlowSource(item.mainFlowSource)}</div>
                        </td>
                        <td className="px-2 py-2 text-right">{(item.totalMarketCap / 1e8).toFixed(1)}亿</td>
                        {isReplay && (
                          <td className="px-2 py-2 text-right font-mono text-[11px]">
                            {item.nextDate ? (
                              <span>
                                <span className="fin-text-tertiary">{(item.nextOpenGainPct ?? 0) >= 0 ? '+' : ''}{(item.nextOpenGainPct ?? 0).toFixed(1)}</span>
                                <span className="fin-text-tertiary"> / </span>
                                <span className={`font-semibold ${(item.nextHighGainPct ?? 0) >= 3 ? 'text-rose-300' : (item.nextHighGainPct ?? 0) >= 0 ? 'text-amber-300' : 'text-emerald-300'}`}>{(item.nextHighGainPct ?? 0) >= 0 ? '+' : ''}{(item.nextHighGainPct ?? 0).toFixed(1)}</span>
                                <span className="fin-text-tertiary"> / </span>
                                <span className={`${(item.nextCloseGainPct ?? 0) >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{(item.nextCloseGainPct ?? 0) >= 0 ? '+' : ''}{(item.nextCloseGainPct ?? 0).toFixed(1)}</span>
                              </span>
                            ) : <span className="fin-text-tertiary">无次日</span>}
                          </td>
                        )}
                        {isLbReplay && (
                          <td className="px-2 py-2 text-right text-[11px]">
                            {item.replayExitReason ? (
                              <span className="inline-flex items-center gap-1 justify-end">
                                <span className={`font-mono font-semibold ${(item.replayReturnPct ?? 0) >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{(item.replayReturnPct ?? 0) >= 0 ? '+' : ''}{(item.replayReturnPct ?? 0).toFixed(2)}%</span>
                                <span className="fin-text-tertiary">·{item.replayHoldDays}天</span>
                                <span className="px-1 py-0.5 rounded bg-slate-500/15 fin-text-tertiary">{exitReasonCN(item.replayExitReason)}</span>
                              </span>
                            ) : <span className="fin-text-tertiary">—</span>}
                          </td>
                        )}
                        <td className="px-2 py-2 text-center" onClick={(e) => e.stopPropagation()}>
                          {batchSelectMode ? (
                            <span className="text-[11px] fin-text-tertiary">勾选</span>
                          ) : (
                            <div className="inline-flex justify-center">
                              <AddToGroupButton stock={toWatchStock(item)} source={strategySource} inWatch={watchSet.has(item.symbol.toLowerCase())} onAddToWatchlist={onAddToWatchlist} />
                            </div>
                          )}
                        </td>
                        {batchSelectMode && (
                          <td className="px-1 py-2 text-center" onClick={(e) => e.stopPropagation()}>
                            <button
                              type="button"
                              onClick={() => toggleBatchStock(item.symbol)}
                              className={`inline-flex h-4 w-4 items-center justify-center rounded-[3px] border transition-colors ${
                                batchSelectedSet.has(item.symbol)
                                  ? 'border-amber-300 bg-amber-400 text-slate-950'
                                  : 'border-slate-500/80 bg-slate-950/20 hover:border-amber-300'
                              }`}
                              title={batchSelectedSet.has(item.symbol) ? '取消勾选' : '勾选批量添加'}
                            >
                              {batchSelectedSet.has(item.symbol) && <Check className="h-3 w-3" />}
                            </button>
                          </td>
                        )}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              </>
            )}
          </div>

          <div className="flex-1 overflow-auto fin-scrollbar p-3 text-left">
            {!selected ? (
              <div className="text-sm fin-text-secondary">请选择左侧标的查看详情。</div>
            ) : (
              <div className="space-y-3">
                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="text-sm font-semibold fin-text-primary">{selected.name} <span className="font-normal fin-text-tertiary">({selected.symbol})</span></div>
                      <div className="text-xs fin-text-tertiary">{selected.industry} · {selected.capBucket}</div>
                    </div>
                    <div className="text-right">
                      <div className="text-lg font-mono fin-text-primary">{selected.price.toFixed(2)}</div>
                      <div className={`text-xs ${selected.changePercent >= 0 ? 'text-rose-300' : 'text-emerald-300'}`}>
                        {selected.changePercent >= 0 ? '+' : ''}{selected.changePercent.toFixed(2)}%
                      </div>
                    </div>
                  </div>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">触发信号（{selected.triggerCount}/{triggerTotal}）</div>
                  <div className="flex flex-wrap gap-1.5">
                    {(selected.triggers || []).map((t) => (
                      <span key={t} className="text-[11px] px-2 py-0.5 rounded border border-accent/35 text-accent-2">{t}</span>
                    ))}
                  </div>
                  {(selected.riskFlags?.length ?? 0) > 0 && (
                    <>
                      <div className="text-xs font-semibold fin-text-primary mt-3 mb-1">风险提示</div>
                      <div className="flex flex-wrap gap-1.5">
                        {(selected.riskFlags || []).map((rf) => (
                          <span key={rf} className="text-[11px] px-2 py-0.5 rounded border border-amber-500/35 text-amber-300">{rf}</span>
                        ))}
                      </div>
                    </>
                  )}
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">逻辑说明</div>
                  <ul className="text-xs fin-text-secondary space-y-1 list-disc pl-4">
                    {(selected.reasons || []).map((r, i) => (
                      <li key={`${selected.symbol}-reason-${i}`}>{r}</li>
                    ))}
                  </ul>
                </div>

                <div className="fin-panel-soft border fin-divider rounded-lg p-3">
                  <div className="text-xs font-semibold fin-text-primary mb-2">买卖纪律</div>
                  <div className="text-xs fin-text-secondary space-y-1">
                    <div>买点：{selected.buyPointHint}</div>
                    <div>卖点：{selected.sellPointHint}</div>
                    <div>止损：{selected.stopLossHint}</div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
    <StrategyReviewDialog
      isOpen={showNextDayReview}
      onClose={() => setShowNextDayReview(false)}
      strategyId={strategySource}
      strategyName={reviewStrategyName}
      signalDate={result?.asOf}
    />
    </>
  );
};

export default LowBuyScannerDialog;
