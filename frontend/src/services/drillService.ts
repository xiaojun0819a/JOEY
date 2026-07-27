// 选股体感练习:取题面(信号日证据,无未来数据)+ 取次日分时磁带(逐点揭示用)。
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export interface DrillPick {
  symbol: string;
  name: string;
  industry: string;
  rank: number;
  score: number;
  price: number;       // 信号日 14:50 入选价(成本)
  signalClose: number; // 信号日收盘 = 次日分时的昨收基准
  changePct: number;
  turnover: number;
  mainPct: number;
  mainNet: number;
  triggers: string[];
  reasons: string[];
  risks: string[];
}

export interface DrillSession {
  strategyId: string;
  strategyName: string;
  signalDate: string;
  nextDate: string;
  picks: DrillPick[];
  availableDates: string[];
  warning: string;
}

export interface TapeTick {
  time: string;
  price: number;
  pct: number;
  volume: number; // 累计量(手)
  amount: number; // 累计额(元)
}

export interface Tape {
  code: string;
  date: string;
  auction: TapeTick[];
  minutes: TapeTick[];
}

// 通达信回补的一分钟线(minute_history):没有 pct/成交额,只有 分钟/价/量
export interface PriceMinute {
  minute: string;
  price: number;
  volume: number;
}

type GoBridge = {
  GetDrillSession?: (strategyId: string, signalDate: string) => Promise<DrillSession>;
  GetStockIntraday?: (code: string, date: string) => Promise<Tape>;
  GetMinuteHistory?: (code: string, date: string) => Promise<PriceMinute[]>;
};

const bridge = (): GoBridge => {
  const win = window as unknown as { go?: { main?: { App?: GoBridge } } };
  return win.go?.main?.App || {};
};

export const getDrillSession = async (strategyId: string, signalDate = ''): Promise<DrillSession | null> => {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('体感练习', 'go');
    return null;
  }
  const b = bridge();
  if (!b.GetDrillSession) throw new Error('当前版本未暴露 GetDrillSession 接口，请重装最新 app');
  return await b.GetDrillSession(strategyId, signalDate);
};

// 取回放磁带。两个源:
//   ① minute_ticks(自采集 15 秒级,2026-07-06 起):含 pct 与累计量额,能画真均价线;
//   ② minute_history(通达信回补的一分钟线):只有价和量,pct 用 prevClose 现算,
//      均价线用 Σ(价×量)/Σ量 自己累(每分钟的 volume 是增量,不是累计)。
// prevClose 由调用方给(= 信号日收盘),回补源没有它。
export const getTape = async (code: string, date: string, prevClose = 0): Promise<TapeTick[]> => {
  if (!isWailsGoReady()) return [];
  const b = bridge();
  if (b.GetStockIntraday) {
    try {
      const tape = await b.GetStockIntraday(code, date);
      const live = (tape?.minutes || []).filter((t) => Number(t.price) > 0);
      if (live.length >= 20) return live;
    } catch {
      /* 自采集没有就退回补库 */
    }
  }
  if (!b.GetMinuteHistory) return [];
  const bars = (await b.GetMinuteHistory(code, date)) || [];
  const out: TapeTick[] = [];
  let cumVol = 0;
  let cumAmt = 0;
  for (const bar of bars) {
    const price = Number(bar.price);
    if (!(price > 0)) continue;
    const vol = Math.max(Number(bar.volume) || 0, 0);
    cumVol += vol;
    cumAmt += price * vol * 100; // 量单位=手,与自采集的 amount(元)对齐
    out.push({
      time: bar.minute.length === 5 ? `${bar.minute}:00` : bar.minute,
      price,
      pct: prevClose > 0 ? (price / prevClose - 1) * 100 : 0,
      volume: cumVol,
      amount: cumAmt,
    });
  }
  return out;
};

// —— 练习成绩本(存本机 localStorage,按策略分档) ——
export interface DrillRecord {
  at: string;
  strategyId: string;
  signalDate: string;
  symbol: string;
  name: string;
  netPct: number;      // 我的净收益(扣0.2%往返)
  holdClosePct: number; // 持到收盘净收益
  maxPct: number;       // 当日最高(理论天花板)净收益
  sellTime: string;
}

const KEY = 'jcp_drill_records';

export const loadDrillRecords = (): DrillRecord[] => {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as DrillRecord[]) : [];
  } catch {
    return [];
  }
};

export const saveDrillRecord = (r: DrillRecord): DrillRecord[] => {
  const list = loadDrillRecords();
  list.unshift(r);
  const trimmed = list.slice(0, 300);
  try {
    localStorage.setItem(KEY, JSON.stringify(trimmed));
  } catch {
    /* 存不下就算了,不影响练习 */
  }
  return trimmed;
};
