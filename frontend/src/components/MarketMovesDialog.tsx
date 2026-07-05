import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Activity, BarChart3, Info, RefreshCw, Route, TrendingUp, X } from 'lucide-react';
import {
  GetBoardFundFlow,
  GetBoardFundFlowOverview,
  GetBoardFundFlowTracking,
  GetBoardLeaders,
  GetMarketChangeDistribution,
  GetStockMoves,
} from '../../wailsjs/go/main/App';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

interface MarketMovesDialogProps {
  isOpen: boolean;
  onClose: () => void;
  marketStatusCode?: string; // 'trading' | 'pre_market' | 'lunch_break' | 'closed'
}

type MoveTab = 'stock' | 'board' | 'fundflow' | 'distribution';
type BoardCategory = 'industry' | 'concept' | 'region';
type StockMoveType = 'surge' | 'drop' | 'change_up' | 'change_down' | 'mainflow' | 'turnover';

interface BoardItem {
  code: string;
  name: string;
  changePercent: number;
  mainNetInflow: number;
  superNetInflow: number;
  largeNetInflow: number;
}

interface BoardFlowResult {
  category: string;
  items: BoardItem[];
  updateTime?: string;
}

interface BoardFundFlowOverview {
  category: string;
  netMainInflow: number;
  strongestInflow?: BoardItem;
  strongestOutflow?: BoardItem;
  inflow: BoardItem[];
  outflow: BoardItem[];
  updateTime?: string;
}

interface FundFlowKLinePoint {
  time: string;
  mainNetInflow: number;
  superNetInflow: number;
  largeNetInflow: number;
  mediumNetInflow: number;
  smallNetInflow: number;
}

interface BoardFundFlowTrackItem {
  rank: number;
  code: string;
  name: string;
  category: string;
  side: 'inflow' | 'outflow' | string;
  changePercent: number;
  mainNetInflow: number;
  latestMainNetInflow: number;
  klines: FundFlowKLinePoint[];
  updateTime?: string;
}

interface BoardFundFlowTracking {
  category: string;
  source?: string;
  tradeDate?: string;
  tradeTime?: string;
  updateTime?: string;
  totalAmount?: number;
  upCount?: number;
  downCount?: number;
  limitUpCount?: number;
  limitDownCount?: number;
  inflow: BoardFundFlowTrackItem[];
  outflow: BoardFundFlowTrackItem[];
  warning?: string;
}

interface LeaderItem {
  rank: number;
  code: string;
  name: string;
  price: number;
  changePercent: number;
  mainNetInflow: number;
  mainNetInflowRatio: number;
  score: number;
}

interface BoardLeaderResult {
  boardCode: string;
  items: LeaderItem[];
  updateTime?: string;
}

interface StockMoveItem {
  rank: number;
  code: string;
  name: string;
  price: number;
  changePercent: number;
  speed: number;
  turnoverRate: number;
  volume: number;
  amount: number;
  mainNetInflow: number;
  mainNetInflowRatio: number;
  updateTime?: string;
}

interface StockMoveResult {
  moveType: string;
  items: StockMoveItem[];
  updateTime?: string;
}

interface MarketChangeBin {
  key: string;
  label: string;
  side: 'up' | 'down' | 'flat' | string;
  count: number;
}

interface MarketChangeDistribution {
  total: number;
  upCount: number;
  downCount: number;
  flatCount: number;
  limitUpCount: number;
  limitDownCount: number;
  bins: MarketChangeBin[];
  updateTime?: string;
  source?: string;
}

// 盘口异动看时效，刷新快；板块资金流变化慢，刷新慢
const STOCK_REFRESH_INTERVAL_MS = 15000; // 盘口异动 15s
const BOARD_REFRESH_INTERVAL_MS = 60000; // 板块资金流 60s
const TRACKER_REFRESH_INTERVAL_MS = 60000; // 实时追踪 60s

// 仅交易时段内自动刷新；收盘/休市日暂停，避免空转
const isMarketActive = (statusCode?: string): boolean => {
  if (!statusCode) return true; // 状态未就绪时先按活跃处理，加载后自动纠正
  return statusCode === 'trading' || statusCode === 'pre_market' || statusCode === 'lunch_break';
};

const CATEGORY_LABELS: Record<BoardCategory, string> = {
  industry: '行业',
  concept: '概念',
  region: '地域',
};

const STOCK_MOVE_LABELS: Record<StockMoveType, string> = {
  surge: '涨速榜',
  drop: '跌速榜',
  change_up: '涨幅榜',
  change_down: '跌幅榜',
  mainflow: '主力净流入',
  turnover: '换手活跃',
};

const TAB_LABELS: Record<MoveTab, string> = {
  board: '板块异动',
  stock: '盘口异动',
  fundflow: '主力净流入',
  distribution: '涨跌分布',
};

const formatAmount = (value: number): string => {
  if (!Number.isFinite(value) || value === 0) return '0';
  const abs = Math.abs(value);
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)}万`;
  return value.toFixed(0);
};

const getTabRefreshName = (tab: MoveTab): string => TAB_LABELS[tab] || '异动中心';

const getRefreshSeconds = (tab: MoveTab): number => (
  tab === 'stock' ? STOCK_REFRESH_INTERVAL_MS / 1000 : BOARD_REFRESH_INTERVAL_MS / 1000
);

const getMaxAbsMainFlow = (overview: BoardFundFlowOverview | null): number => {
  const values = [...(overview?.inflow || []), ...(overview?.outflow || [])].map(item => Math.abs(item.mainNetInflow || 0));
  return Math.max(1, ...values);
};

const getMaxBinCount = (distribution: MarketChangeDistribution | null): number => (
  Math.max(1, ...(distribution?.bins || []).map(bin => bin.count || 0))
);

const getTrackerItems = (data: BoardFundFlowTracking | null): BoardFundFlowTrackItem[] => [
  ...(data?.inflow || []),
  ...(data?.outflow || []),
].filter(item => Array.isArray(item.klines) && item.klines.length > 0);

const getTrackerMaxAbs = (items: BoardFundFlowTrackItem[]): number => {
  const values = items.flatMap(item => item.klines.map(point => Math.abs(Number(point.mainNetInflow) || 0)));
  return Math.max(1, ...values);
};

const parseTrackerMinute = (time: string): number => {
  const match = String(time || '').match(/(\d{2}):(\d{2})/);
  if (!match) return 0;
  return Number(match[1]) * 60 + Number(match[2]);
};

const trackerXFromTime = (time: string, width: number, left: number): number => {
  const minute = parseTrackerMinute(time);
  const morningStart = 9 * 60 + 30;
  const morningEnd = 11 * 60 + 30;
  const afternoonStart = 13 * 60;
  const afternoonEnd = 15 * 60;
  const total = (morningEnd - morningStart) + (afternoonEnd - afternoonStart);
  let progressed = 0;
  if (minute <= morningStart) {
    progressed = 0;
  } else if (minute <= morningEnd) {
    progressed = minute - morningStart;
  } else if (minute < afternoonStart) {
    progressed = morningEnd - morningStart;
  } else {
    progressed = (morningEnd - morningStart) + Math.min(minute, afternoonEnd) - afternoonStart;
  }
  return left + Math.max(0, Math.min(1, progressed / total)) * width;
};

const compactDate = (date?: string): string => {
  if (!date) return '--';
  const parts = date.split('-');
  if (parts.length === 3) return `${Number(parts[1])}月${Number(parts[2])}日`;
  return date;
};

const formatSignedAmount = (value: number): string => {
  const prefix = value > 0 ? '+' : '';
  return `${prefix}${formatAmount(value)}`;
};

interface FundFlowTrackerModalProps {
  data: BoardFundFlowTracking | null;
  category: BoardCategory;
  loading: boolean;
  error: string;
  autoRefreshEnabled: boolean;
  onClose: () => void;
  onRefresh: () => void;
  onCategoryChange: (category: BoardCategory) => void;
}

const FundFlowTrackerChart: React.FC<{ data: BoardFundFlowTracking | null }> = ({ data }) => {
  const items = getTrackerItems(data);
  const maxAbs = getTrackerMaxAbs(items);
  const viewW = 1040;
  const viewH = 560;
  const left = 58;
  const plotW = 680;
  const top = 36;
  const zeroY = 128;
  const bottom = 490;
  const labelX = 782;
  const positiveColors = ['#7f1d1d', '#dc2626', '#f97316', '#fb7185', '#ef4444', '#b91c1c'];
  const negativeColors = ['#047857', '#0f766e', '#0e7490', '#2563eb', '#4338ca', '#065f46', '#0891b2', '#16a34a'];

  const yFromValue = (value: number): number => {
    if (value >= 0) {
      return zeroY - Math.min(1, value / maxAbs) * (zeroY - top);
    }
    return zeroY + Math.min(1, Math.abs(value) / maxAbs) * (bottom - zeroY);
  };

  const sortedItems = [...items].sort((a, b) => (b.latestMainNetInflow || 0) - (a.latestMainNetInflow || 0));
  let nextPositiveLabelY = 52;
  let nextNegativeLabelY = 154;
  const minLabelGap = 25;

  const seriesMeta = sortedItems.map((item) => {
    const isInflow = item.side === 'inflow' || (item.latestMainNetInflow || 0) >= 0;
    const points = item.klines.map(point => ({
      x: trackerXFromTime(point.time, plotW, left),
      y: yFromValue(Number(point.mainNetInflow) || 0),
      value: Number(point.mainNetInflow) || 0,
    }));
    const latest = points[points.length - 1] || { x: left, y: zeroY, value: 0 };
    let labelY = latest.y;
    if (isInflow) {
      labelY = Math.max(30, Math.min(116, labelY));
      labelY = Math.max(labelY, nextPositiveLabelY);
      nextPositiveLabelY = labelY + minLabelGap;
    } else {
      labelY = Math.max(150, Math.min(522, labelY));
      labelY = Math.max(labelY, nextNegativeLabelY);
      nextNegativeLabelY = labelY + minLabelGap;
    }
    const color = isInflow
      ? positiveColors[item.rank % positiveColors.length]
      : negativeColors[item.rank % negativeColors.length];
    return { item, points, latest, labelY, color, isInflow };
  });

  return (
    <svg viewBox={`0 0 ${viewW} ${viewH}`} className="h-full w-full">
      <defs>
        <linearGradient id="tracker-bg" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor="#fff1f2" stopOpacity="0.08" />
          <stop offset="28%" stopColor="#ffffff" stopOpacity="0.04" />
          <stop offset="100%" stopColor="#ecfeff" stopOpacity="0.08" />
        </linearGradient>
      </defs>
      <rect x={left} y={top} width={plotW} height={bottom - top} fill="url(#tracker-bg)" rx="8" />
      {[100, 0, -100, -300, -500, -700].map((label) => {
        const y = label === 0 ? zeroY : yFromValue(label * 1e8);
        return (
          <g key={label}>
            <line x1={left} x2={left + plotW} y1={y} y2={y} stroke={label === 0 ? '#9ca3af' : '#334155'} strokeDasharray={label === 0 ? '7 7' : '3 8'} strokeOpacity={label === 0 ? 0.75 : 0.45} />
            <text x={left - 10} y={y + 4} textAnchor="end" fontSize="16" fill="#9ca3af">{label}亿</text>
          </g>
        );
      })}
      {[
        ['9:30', '2026-01-01 09:30'],
        ['10:30', '2026-01-01 10:30'],
        ['11:30', '2026-01-01 11:30'],
        ['14:00', '2026-01-01 14:00'],
        ['15:00', '2026-01-01 15:00'],
      ].map(([label, time]) => {
        const x = trackerXFromTime(time, plotW, left);
        return (
          <g key={label}>
            <line x1={x} x2={x} y1={top} y2={bottom} stroke="#334155" strokeOpacity="0.25" />
            <text x={x} y={bottom + 34} textAnchor="middle" fontSize="18" fill="#9ca3af">{label}</text>
          </g>
        );
      })}
      {seriesMeta.map(({ item, points, color, isInflow }) => {
        const d = points.map((point, idx) => `${idx === 0 ? 'M' : 'L'}${point.x.toFixed(1)},${point.y.toFixed(1)}`).join(' ');
        return (
          <g key={`${item.side}-${item.code}`}>
            <path d={d} fill="none" stroke={color} strokeWidth={isInflow ? 4.2 : 3.4} strokeOpacity={isInflow ? 0.98 : 0.9} strokeLinecap="round" strokeLinejoin="round" />
          </g>
        );
      })}
      {seriesMeta.map(({ item, latest, labelY, color, isInflow }) => (
        <g key={`label-${item.side}-${item.code}`}>
          <circle cx={latest.x} cy={latest.y} r={4.2} fill={color} />
          <line x1={latest.x + 5} x2={labelX - 8} y1={latest.y} y2={labelY} stroke="#94a3b8" strokeOpacity="0.55" strokeWidth="1.2" />
          <rect
            x={labelX}
            y={labelY - 15}
            width="230"
            height="28"
            rx="7"
            fill={isInflow ? '#fff1f2' : '#ecfdf5'}
            stroke={color}
            strokeWidth="1.4"
            opacity="0.96"
          />
          <text x={labelX + 9} y={labelY + 5} fontSize="15" fontWeight="700" fill="#111827">
            {item.name} {formatSignedAmount(item.latestMainNetInflow / 1e8)}
          </text>
        </g>
      ))}
    </svg>
  );
};

const FundFlowTrackerModal: React.FC<FundFlowTrackerModalProps> = ({
  data,
  category,
  loading,
  error,
  autoRefreshEnabled,
  onClose,
  onRefresh,
  onCategoryChange,
}) => {
  const upCount = Number(data?.upCount || 0);
  const downCount = Number(data?.downCount || 0);
  const breadthTotal = Math.max(1, upCount + downCount);
  const downWidth = `${Math.max(8, downCount / breadthTotal * 100)}%`;
  const upWidth = `${Math.max(8, upCount / breadthTotal * 100)}%`;

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={onClose} />
      <div className="relative h-[820px] max-h-[92vh] w-[1180px] max-w-[94vw] overflow-hidden rounded-xl border border-red-500/25 bg-[#f8fafc] text-slate-950 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-200 bg-gradient-to-r from-red-900 via-red-700 to-orange-600 px-6 py-4 text-white">
          <div className="flex items-center gap-3">
            <Route className="h-5 w-5" />
            <div>
              <div className="text-2xl font-black tracking-normal">主力资金净流入【实时跟踪】</div>
              <div className="mt-1 text-xs text-red-100">数据源：{data?.source || 'eastmoney board fflow/kline'} · 1分钟板块资金曲线</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {(['industry', 'concept', 'region'] as BoardCategory[]).map(item => (
              <button
                key={item}
                onClick={() => onCategoryChange(item)}
                className={`rounded border px-3 py-1.5 text-xs font-semibold ${
                  category === item
                    ? 'border-white bg-white text-red-700'
                    : 'border-white/40 bg-white/10 text-white hover:bg-white/20'
                }`}
              >
                {CATEGORY_LABELS[item]}
              </button>
            ))}
            <button
              onClick={onRefresh}
              className="rounded border border-white/40 bg-white/10 p-2 hover:bg-white/20"
              disabled={loading}
              title="刷新实时追踪"
            >
              <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
            <button onClick={onClose} className="rounded border border-white/40 bg-white/10 p-2 hover:bg-white/20">
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-[190px_1fr_260px] items-center gap-4 px-6 py-3">
          <div className="rounded-lg border border-orange-200 bg-orange-50 px-4 py-2 text-center">
            <span className="text-xl font-black">{compactDate(data?.tradeDate)}</span>
          </div>
          <div className="text-center text-2xl font-black">{data?.tradeTime || '--:--'}</div>
          <div className="text-right text-xl font-black">
            涨停 <span className="text-red-600">{data?.limitUpCount || 0}</span> : 跌停 <span className="text-emerald-700">{data?.limitDownCount || 0}</span>
          </div>
        </div>

        <div className="mx-6 h-9 overflow-hidden rounded border border-slate-300 bg-slate-100">
          <div className="flex h-full text-lg font-black text-white">
            <div className="flex items-center px-3" style={{ width: downWidth, background: '#0f766e' }}>跌 {downCount}</div>
            <div className="flex flex-1 items-center justify-end px-3" style={{ width: upWidth, background: '#dc2626' }}>涨 {upCount}</div>
          </div>
        </div>

        <div className="px-6 pt-3">
          <div className="text-2xl font-black">
            全市场成交 <span>{formatAmount(data?.totalAmount || 0)}</span>
          </div>
        </div>

        <div className="relative mx-6 mt-2 h-[590px] rounded-lg border border-slate-200 bg-white">
          {loading ? (
            <div className="absolute inset-0 flex items-center justify-center bg-white/70">
              <RefreshCw className="h-7 w-7 animate-spin text-slate-500" />
            </div>
          ) : error ? (
            <div className="p-5 text-sm font-semibold text-amber-700">{error}</div>
          ) : getTrackerItems(data).length ? (
            <FundFlowTrackerChart data={data} />
          ) : (
            <div className="p-5 text-sm font-semibold text-amber-700">暂无可展示的主力资金实时曲线</div>
          )}
        </div>

        <div className="flex items-center justify-between px-6 py-2 text-xs text-slate-500">
          <span>{autoRefreshEnabled ? '打开期间每60秒自动刷新' : '当前非连续交易状态，自动刷新暂停'}</span>
          <span>{data?.warning ? `提示：${data.warning}` : `更新：${data?.updateTime || '--'}`}</span>
        </div>
      </div>
    </div>
  );
};

export const MarketMovesDialog: React.FC<MarketMovesDialogProps> = ({ isOpen, onClose, marketStatusCode }) => {
  const wasOpenRef = useRef(false);
  const [activeTab, setActiveTab] = useState<MoveTab>('board');
  const [category, setCategory] = useState<BoardCategory>('industry');
  const [stockMoveType, setStockMoveType] = useState<StockMoveType>('surge');
  const [loading, setLoading] = useState(false);
  const [boardFlow, setBoardFlow] = useState<BoardFlowResult | null>(null);
  const [error, setError] = useState<string>('');
  const [stockLoading, setStockLoading] = useState(false);
  const [stockError, setStockError] = useState('');
  const [stockMoves, setStockMoves] = useState<StockMoveResult | null>(null);
  const [selectedBoard, setSelectedBoard] = useState<BoardItem | null>(null);
  const [leaderLoading, setLeaderLoading] = useState(false);
  const [leaderError, setLeaderError] = useState('');
  const [leaderResult, setLeaderResult] = useState<BoardLeaderResult | null>(null);
  const [fundFlowOverview, setFundFlowOverview] = useState<BoardFundFlowOverview | null>(null);
  const [fundFlowLoading, setFundFlowLoading] = useState(false);
  const [fundFlowError, setFundFlowError] = useState('');
  const [trackerOpen, setTrackerOpen] = useState(false);
  const [trackerData, setTrackerData] = useState<BoardFundFlowTracking | null>(null);
  const [trackerLoading, setTrackerLoading] = useState(false);
  const [trackerError, setTrackerError] = useState('');
  const [distribution, setDistribution] = useState<MarketChangeDistribution | null>(null);
  const [distributionLoading, setDistributionLoading] = useState(false);
  const [distributionError, setDistributionError] = useState('');
  const refreshIntervalMs = activeTab === 'stock' ? STOCK_REFRESH_INTERVAL_MS : BOARD_REFRESH_INTERVAL_MS;
  const refreshIntervalSeconds = refreshIntervalMs / 1000;
  const autoRefreshEnabled = isMarketActive(marketStatusCode);
  const [autoRefreshCountdown, setAutoRefreshCountdown] = useState(refreshIntervalSeconds);

  const resetAutoRefreshCountdown = useCallback((tab: MoveTab = activeTab) => {
    setAutoRefreshCountdown(getRefreshSeconds(tab));
  }, [activeTab]);

  const isCurrentTabLoading = (
    activeTab === 'stock' ? stockLoading :
      activeTab === 'fundflow' ? fundFlowLoading :
        activeTab === 'distribution' ? distributionLoading :
          loading || leaderLoading
  );

  const loadBoardLeaders = useCallback(async (boardCode: string) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('板块龙头', 'go');
      setLeaderResult(null);
      setLeaderError('浏览器预览模式暂不支持该数据源');
      setLeaderLoading(false);
      return;
    }
    setLeaderLoading(true);
    setLeaderError('');
    try {
      const result = await GetBoardLeaders(boardCode, 6);
      const normalized: BoardLeaderResult = {
        boardCode: result?.boardCode || boardCode,
        items: Array.isArray(result?.items) ? result.items : [],
        updateTime: result?.updateTime,
      };
      setLeaderResult(normalized);
      if (!normalized.items.length) {
        setLeaderError('该板块暂未返回可用龙头候选');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取龙头推荐失败';
      setLeaderError(message);
      setLeaderResult(null);
    } finally {
      setLeaderLoading(false);
    }
  }, []);

  const loadBoardFlow = useCallback(async (nextCategory: BoardCategory, preferredBoardCode?: string) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('板块异动', 'go');
      setBoardFlow(null);
      setSelectedBoard(null);
      setLeaderResult(null);
      setError('浏览器预览模式暂不支持该数据源');
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const result = await GetBoardFundFlow(nextCategory, 1, 30);
      const normalized: BoardFlowResult = {
        category: result?.category || nextCategory,
        items: Array.isArray(result?.items) ? result.items : [],
        updateTime: result?.updateTime,
      };
      setBoardFlow(normalized);
      if (normalized.items.length > 0) {
        const nextSelected =
          normalized.items.find(item => item.code === preferredBoardCode) || normalized.items[0];
        setSelectedBoard(nextSelected);
        void loadBoardLeaders(nextSelected.code);
      } else {
        setSelectedBoard(null);
        setLeaderResult(null);
        setLeaderError('');
        setError('暂无可展示的板块异动数据');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取板块异动失败';
      setError(message);
      setBoardFlow(null);
      setSelectedBoard(null);
      setLeaderResult(null);
      setLeaderError('');
    } finally {
      setLoading(false);
    }
  }, [loadBoardLeaders]);

  const loadStockMoves = useCallback(async (nextType: StockMoveType) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('盘口异动', 'go');
      setStockMoves(null);
      setStockError('浏览器预览模式暂不支持该数据源');
      setStockLoading(false);
      return;
    }
    setStockLoading(true);
    setStockError('');
    try {
      const result = await GetStockMoves(nextType, 1, 50);
      const normalized: StockMoveResult = {
        moveType: result?.moveType || nextType,
        items: Array.isArray(result?.items) ? result.items : [],
        updateTime: result?.updateTime,
      };
      setStockMoves(normalized);
      if (!normalized.items.length) {
        setStockError('暂无可展示的盘口异动数据');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取盘口异动失败';
      setStockError(message);
      setStockMoves(null);
    } finally {
      setStockLoading(false);
    }
  }, []);

  const loadFundFlowOverview = useCallback(async (nextCategory: BoardCategory = category) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('主力净流入', 'go');
      setFundFlowOverview(null);
      setFundFlowError('浏览器预览模式暂不支持该数据源');
      setFundFlowLoading(false);
      return;
    }
    setFundFlowLoading(true);
    setFundFlowError('');
    try {
      const result = await GetBoardFundFlowOverview(nextCategory, 10);
      const normalized: BoardFundFlowOverview = {
        category: result?.category || nextCategory,
        netMainInflow: Number(result?.netMainInflow) || 0,
        strongestInflow: result?.strongestInflow,
        strongestOutflow: result?.strongestOutflow,
        inflow: Array.isArray(result?.inflow) ? result.inflow : [],
        outflow: Array.isArray(result?.outflow) ? result.outflow : [],
        updateTime: result?.updateTime,
      };
      setFundFlowOverview(normalized);
      if (!normalized.inflow.length && !normalized.outflow.length) {
        setFundFlowError('暂无可展示的主力净流入数据');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取主力净流入失败';
      setFundFlowError(message);
      setFundFlowOverview(null);
    } finally {
      setFundFlowLoading(false);
    }
  }, [category]);

  const loadFundFlowTracker = useCallback(async (nextCategory: BoardCategory = category) => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('主力资金实时追踪', 'go');
      setTrackerData(null);
      setTrackerError('浏览器预览模式暂不支持该数据源');
      setTrackerLoading(false);
      return;
    }
    setTrackerLoading(true);
    setTrackerError('');
    try {
      const result = await GetBoardFundFlowTracking(nextCategory, 24, '1');
      const normalized: BoardFundFlowTracking = {
        category: result?.category || nextCategory,
        source: result?.source,
        tradeDate: result?.tradeDate,
        tradeTime: result?.tradeTime,
        updateTime: result?.updateTime,
        totalAmount: Number(result?.totalAmount) || 0,
        upCount: Number(result?.upCount) || 0,
        downCount: Number(result?.downCount) || 0,
        limitUpCount: Number(result?.limitUpCount) || 0,
        limitDownCount: Number(result?.limitDownCount) || 0,
        inflow: Array.isArray(result?.inflow) ? result.inflow : [],
        outflow: Array.isArray(result?.outflow) ? result.outflow : [],
        warning: result?.warning,
      };
      setTrackerData(normalized);
      if (!normalized.inflow.length && !normalized.outflow.length) {
        setTrackerError(normalized.warning || '暂无可展示的主力资金实时曲线');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取主力资金实时追踪失败';
      setTrackerError(message);
      setTrackerData(null);
    } finally {
      setTrackerLoading(false);
    }
  }, [category]);

  const loadChangeDistribution = useCallback(async () => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('涨跌分布', 'go');
      setDistribution(null);
      setDistributionError('浏览器预览模式暂不支持该数据源');
      setDistributionLoading(false);
      return;
    }
    setDistributionLoading(true);
    setDistributionError('');
    try {
      const result = await GetMarketChangeDistribution(false);
      const normalized: MarketChangeDistribution = {
        total: Number(result?.total) || 0,
        upCount: Number(result?.upCount) || 0,
        downCount: Number(result?.downCount) || 0,
        flatCount: Number(result?.flatCount) || 0,
        limitUpCount: Number(result?.limitUpCount) || 0,
        limitDownCount: Number(result?.limitDownCount) || 0,
        bins: Array.isArray(result?.bins) ? result.bins : [],
        updateTime: result?.updateTime,
        source: result?.source,
      };
      setDistribution(normalized);
      if (!normalized.total) {
        setDistributionError('暂无可展示的涨跌分布数据');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '获取涨跌分布失败';
      setDistributionError(message);
      setDistribution(null);
    } finally {
      setDistributionLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!isOpen) {
      wasOpenRef.current = false;
      return;
    }
    if (wasOpenRef.current) return;
    wasOpenRef.current = true;
    setActiveTab('board');
    setCategory('industry');
    setStockMoveType('surge');
    resetAutoRefreshCountdown('board');
    void loadBoardFlow('industry');
  }, [isOpen, loadBoardFlow, resetAutoRefreshCountdown]);

  useEffect(() => {
    if (!isOpen || activeTab !== 'stock') return;
    void loadStockMoves(stockMoveType);
  }, [isOpen, activeTab, stockMoveType, loadStockMoves]);

  useEffect(() => {
    if (!isOpen || activeTab !== 'fundflow') return;
    void loadFundFlowOverview(category);
  }, [isOpen, activeTab, category, loadFundFlowOverview]);

  useEffect(() => {
    if (!isOpen || !trackerOpen) return;
    void loadFundFlowTracker(category);
  }, [isOpen, trackerOpen, category, loadFundFlowTracker]);

  useEffect(() => {
    if (!isOpen || activeTab !== 'distribution') return;
    void loadChangeDistribution();
  }, [isOpen, activeTab, loadChangeDistribution]);

  useEffect(() => {
    if (!isOpen || !autoRefreshEnabled) return;
    setAutoRefreshCountdown(refreshIntervalSeconds);
    const timer = window.setInterval(() => {
      setAutoRefreshCountdown(prev => (prev <= 1 ? refreshIntervalSeconds : prev - 1));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [isOpen, autoRefreshEnabled, refreshIntervalSeconds]);

  useEffect(() => {
    if (!isOpen || !autoRefreshEnabled) return;
    const timer = window.setInterval(() => {
      if (activeTab === 'stock') {
        void loadStockMoves(stockMoveType);
      } else if (activeTab === 'fundflow') {
        void loadFundFlowOverview(category);
      } else if (activeTab === 'distribution') {
        void loadChangeDistribution();
      } else {
        void loadBoardFlow(category, selectedBoard?.code);
      }
      resetAutoRefreshCountdown();
    }, refreshIntervalMs);
    return () => window.clearInterval(timer);
  }, [isOpen, autoRefreshEnabled, refreshIntervalMs, activeTab, stockMoveType, category, selectedBoard?.code, loadBoardFlow, loadStockMoves, loadFundFlowOverview, loadChangeDistribution, resetAutoRefreshCountdown]);

  useEffect(() => {
    if (!isOpen || !trackerOpen || !autoRefreshEnabled) return;
    const timer = window.setInterval(() => {
      void loadFundFlowTracker(category);
    }, TRACKER_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [isOpen, trackerOpen, autoRefreshEnabled, category, loadFundFlowTracker]);

  const onChangeCategory = (nextCategory: BoardCategory) => {
    setCategory(nextCategory);
    resetAutoRefreshCountdown();
    void loadBoardFlow(nextCategory);
  };

  const onChangeStockMoveType = (nextType: StockMoveType) => {
    setStockMoveType(nextType);
    resetAutoRefreshCountdown();
    void loadStockMoves(nextType);
  };

  const onChangeTab = (nextTab: MoveTab) => {
    setActiveTab(nextTab);
    resetAutoRefreshCountdown(nextTab);
    if (nextTab === 'board') void loadBoardFlow(category, selectedBoard?.code);
    if (nextTab === 'stock') void loadStockMoves(stockMoveType);
    if (nextTab === 'fundflow') void loadFundFlowOverview(category);
    if (nextTab === 'distribution') void loadChangeDistribution();
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-[1120px] h-[760px] max-w-[92vw] max-h-[88vh] fin-panel border fin-divider rounded-xl shadow-2xl flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4 border-b fin-divider">
          <div className="flex items-center gap-3">
            <Activity className="w-5 h-5 text-accent-2" />
            <div>
              <h2 className="text-lg font-semibold fin-text-primary">异动中心</h2>
              <div className="text-[11px] fin-text-tertiary">
                {autoRefreshEnabled
                  ? `${getTabRefreshName(activeTab)}自动刷新，${autoRefreshCountdown}s 后更新`
                  : '已收盘，自动刷新已暂停（点刷新手动更新）'}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <div className="text-[11px] fin-text-secondary">
              {isCurrentTabLoading ? '刷新中...' : autoRefreshEnabled ? '自动刷新中' : '已暂停'}
            </div>
            <button onClick={onClose} className="p-2 rounded-lg fin-hover transition-colors">
              <X className="w-4 h-4 fin-text-secondary" />
            </button>
          </div>
        </div>

        <div className="px-4 pt-3">
          <div className="flex items-center gap-2">
            {(['board', 'stock', 'fundflow', 'distribution'] as MoveTab[]).map((tab) => (
              <button
                key={tab}
                onClick={() => onChangeTab(tab)}
                className={`px-3 py-1.5 rounded-lg text-sm border transition-colors ${
                  activeTab === tab
                    ? 'bg-accent/20 text-accent border-accent/40'
                    : 'fin-panel fin-text-secondary fin-divider hover:fin-text-primary'
                }`}
              >
                {TAB_LABELS[tab]}
              </button>
            ))}
          </div>
        </div>

        <div className="flex-1 px-4 py-3 overflow-hidden">
          {activeTab === 'fundflow' ? (
            <div className="h-full fin-panel border fin-divider rounded-lg p-4 overflow-hidden flex flex-col">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <TrendingUp className="w-4 h-4 text-accent-2" />
                  <span className="text-sm fin-text-primary font-medium">主力净流入</span>
                  <Info className="w-3.5 h-3.5 fin-text-tertiary" />
                  {fundFlowOverview?.updateTime ? (
                    <span className="text-xs fin-text-tertiary">更新: {fundFlowOverview.updateTime}</span>
                  ) : null}
                </div>
                <div className="flex items-center gap-2">
                  {(['industry', 'concept', 'region'] as BoardCategory[]).map((item) => (
                    <button
                      key={item}
                      onClick={() => {
                        setCategory(item);
                        resetAutoRefreshCountdown();
                        void loadFundFlowOverview(item);
                      }}
                      className={`px-2.5 py-1 rounded text-xs border ${
                        category === item
                          ? 'bg-accent/20 text-accent border-accent/40'
                          : 'fin-panel fin-text-secondary fin-divider hover:fin-text-primary'
                      }`}
                    >
                      {CATEGORY_LABELS[item]}
                    </button>
                  ))}
                  <button
                    onClick={() => {
                      resetAutoRefreshCountdown();
                      void loadFundFlowOverview(category);
                    }}
                    className="p-1.5 rounded fin-hover transition-colors"
                    disabled={fundFlowLoading}
                    title="刷新"
                  >
                    <RefreshCw className={`w-4 h-4 fin-text-secondary ${fundFlowLoading ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              </div>

              {fundFlowLoading ? (
                <div className="flex-1 flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 fin-text-secondary animate-spin" />
                </div>
              ) : fundFlowError ? (
                <div className="text-sm text-amber-300">{fundFlowError}</div>
              ) : (
                <div className="flex-1 min-h-0 grid grid-cols-[1fr_auto_1fr] gap-5 overflow-hidden">
                  <div className="min-w-0 overflow-auto fin-scrollbar rounded-lg border fin-divider p-3">
                    <div className="mb-3 text-sm font-medium text-emerald-300">主力流出最多</div>
                    <div className="space-y-3">
                      {(fundFlowOverview?.outflow || []).map((item) => {
                        const width = `${Math.max(4, Math.min(100, Math.abs(item.mainNetInflow) / getMaxAbsMainFlow(fundFlowOverview) * 100))}%`;
                        return (
                          <div key={item.code} className="grid grid-cols-[92px_1fr_76px] items-center gap-2 text-xs">
                            <div className="min-w-0">
                              <div className="truncate fin-text-primary">{item.name}</div>
                              <div className="font-mono fin-text-tertiary">{item.code}</div>
                            </div>
                            <div className="h-3 rounded-sm bg-slate-700/50 overflow-hidden">
                              <div className="h-full bg-emerald-500" style={{ width }} />
                            </div>
                            <div className="text-right font-mono text-emerald-300">{formatAmount(item.mainNetInflow)}</div>
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  <div className="w-[220px] flex flex-col items-center justify-center text-center">
                    <div className="rounded-lg border fin-divider px-5 py-4 bg-red-500/5">
                      <div className="text-sm fin-text-secondary">主力净流入</div>
                      <div className={`mt-2 font-mono text-3xl font-bold ${Number(fundFlowOverview?.netMainInflow || 0) >= 0 ? 'text-red-400' : 'text-emerald-400'}`}>
                        {formatAmount(fundFlowOverview?.netMainInflow || 0)}
                      </div>
                      <div className="mt-2 text-xs fin-text-tertiary">
                        {fundFlowOverview?.strongestInflow?.name ? `${fundFlowOverview.strongestInflow.name}流入最多` : '按板块主力资金汇总'}
                      </div>
                    </div>
                    <button
                      onClick={() => {
                        setTrackerOpen(true);
                        void loadFundFlowTracker(category);
                      }}
                      className="mt-3 inline-flex items-center gap-2 rounded border border-red-400/70 bg-red-500/15 px-4 py-2 text-sm font-semibold text-red-200 transition-colors hover:bg-red-500/25"
                      title="打开板块主力资金分钟级实时追踪"
                    >
                      <Route className="h-4 w-4" />
                      实时追踪
                    </button>
                  </div>

                  <div className="min-w-0 overflow-auto fin-scrollbar rounded-lg border fin-divider p-3">
                    <div className="mb-3 text-sm font-medium text-red-300">主力流入最多</div>
                    <div className="space-y-3">
                      {(fundFlowOverview?.inflow || []).map((item) => {
                        const width = `${Math.max(4, Math.min(100, Math.abs(item.mainNetInflow) / getMaxAbsMainFlow(fundFlowOverview) * 100))}%`;
                        return (
                          <div key={item.code} className="grid grid-cols-[92px_1fr_76px] items-center gap-2 text-xs">
                            <div className="min-w-0">
                              <div className="truncate fin-text-primary">{item.name}</div>
                              <div className="font-mono fin-text-tertiary">{item.code}</div>
                            </div>
                            <div className="h-3 rounded-sm bg-slate-700/50 overflow-hidden">
                              <div className="h-full bg-red-500" style={{ width }} />
                            </div>
                            <div className="text-right font-mono text-red-300">{formatAmount(item.mainNetInflow)}</div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                </div>
              )}
            </div>
          ) : activeTab === 'distribution' ? (
            <div className="h-full fin-panel border fin-divider rounded-lg p-4 overflow-hidden flex flex-col">
              <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-2">
                  <BarChart3 className="w-4 h-4 text-accent-2" />
                  <span className="text-sm fin-text-primary font-medium">涨跌分布</span>
                  <Info className="w-3.5 h-3.5 fin-text-tertiary" />
                  {distribution?.updateTime ? (
                    <span className="text-xs fin-text-tertiary">更新: {distribution.updateTime}</span>
                  ) : null}
                </div>
                <button
                  onClick={() => {
                    resetAutoRefreshCountdown();
                    void loadChangeDistribution();
                  }}
                  className="p-1.5 rounded fin-hover transition-colors"
                  disabled={distributionLoading}
                  title="刷新"
                >
                  <RefreshCw className={`w-4 h-4 fin-text-secondary ${distributionLoading ? 'animate-spin' : ''}`} />
                </button>
              </div>

              {distributionLoading ? (
                <div className="flex-1 flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 fin-text-secondary animate-spin" />
                </div>
              ) : distributionError ? (
                <div className="text-sm text-amber-300">{distributionError}</div>
              ) : (
                <div className="flex-1 min-h-0 flex flex-col">
                  <div className="grid grid-cols-5 gap-3 text-center">
                    <div className="rounded-lg border fin-divider p-3">
                      <div className="text-xs fin-text-tertiary">上涨</div>
                      <div className="mt-1 text-xl font-bold text-red-400">{distribution?.upCount || 0}家</div>
                    </div>
                    <div className="rounded-lg border fin-divider p-3">
                      <div className="text-xs fin-text-tertiary">下跌</div>
                      <div className="mt-1 text-xl font-bold text-emerald-400">{distribution?.downCount || 0}家</div>
                    </div>
                    <div className="rounded-lg border fin-divider p-3">
                      <div className="text-xs fin-text-tertiary">平盘</div>
                      <div className="mt-1 text-xl font-bold fin-text-primary">{distribution?.flatCount || 0}家</div>
                    </div>
                    <div className="rounded-lg border fin-divider p-3">
                      <div className="text-xs fin-text-tertiary">涨停</div>
                      <div className="mt-1 text-xl font-bold text-red-400">{distribution?.limitUpCount || 0}家</div>
                    </div>
                    <div className="rounded-lg border fin-divider p-3">
                      <div className="text-xs fin-text-tertiary">跌停</div>
                      <div className="mt-1 text-xl font-bold text-emerald-400">{distribution?.limitDownCount || 0}家</div>
                    </div>
                  </div>

                  <div className="mt-5 flex-1 min-h-0 rounded-lg border fin-divider p-4">
                    <div className="flex h-full items-end justify-between gap-2">
                      {(distribution?.bins || []).map((bin) => {
                        const isUp = bin.side === 'up';
                        const isDown = bin.side === 'down';
                        const height = `${Math.max(4, Math.min(100, (bin.count || 0) / getMaxBinCount(distribution) * 100))}%`;
                        return (
                          <div key={bin.key} className="flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-2">
                            <div className={`font-mono text-xs ${isUp ? 'text-red-300' : isDown ? 'text-emerald-300' : 'fin-text-tertiary'}`}>{bin.count}</div>
                            <div className="relative h-[72%] w-full flex items-end justify-center">
                              <div
                                className={`w-full max-w-[48px] rounded-t-sm ${isUp ? 'bg-red-500/85' : isDown ? 'bg-emerald-500/85' : 'bg-slate-500/70'}`}
                                style={{ height }}
                              />
                            </div>
                            <div className="text-[11px] leading-3 fin-text-tertiary text-center whitespace-nowrap">{bin.label}</div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                  <div className="mt-2 text-right text-[11px] fin-text-tertiary">统计范围：沪深全A，不含北交所 · 共 {distribution?.total || 0} 只</div>
                </div>
              )}
            </div>
          ) : activeTab === 'stock' ? (
            <div className="h-full fin-panel border fin-divider rounded-lg p-4 overflow-auto fin-scrollbar">
              <div className="flex items-center justify-between mb-3">
                <div className="text-sm fin-text-primary font-medium">盘口异动</div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      resetAutoRefreshCountdown();
                      void loadStockMoves(stockMoveType);
                    }}
                    className="p-1.5 rounded fin-hover transition-colors"
                    disabled={stockLoading}
                    title="刷新"
                  >
                    <RefreshCw className={`w-4 h-4 fin-text-secondary ${stockLoading ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 mb-3">
                {(Object.keys(STOCK_MOVE_LABELS) as StockMoveType[]).map((item) => (
                  <button
                    key={item}
                    onClick={() => onChangeStockMoveType(item)}
                    className={`px-2.5 py-1 rounded text-xs border ${stockMoveType === item
                      ? 'bg-accent/20 text-accent border-accent/40'
                      : 'fin-panel fin-text-secondary fin-divider hover:fin-text-primary'}`}
                  >
                    {STOCK_MOVE_LABELS[item]}
                  </button>
                ))}
                {stockMoves?.updateTime ? (
                  <span className="text-xs fin-text-tertiary">更新: {stockMoves.updateTime}</span>
                ) : null}
              </div>
              {stockLoading ? (
                <div className="h-[460px] flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 fin-text-secondary animate-spin" />
                </div>
              ) : stockError ? (
                <div className="text-sm text-amber-300">{stockError}</div>
              ) : (
                <div className="overflow-auto fin-scrollbar h-[500px] rounded border fin-divider">
                  <table className="w-full text-sm">
                    <thead className="sticky top-0 fin-panel border-b fin-divider z-10">
                      <tr className="text-left">
                        <th className="px-3 py-2 font-medium fin-text-secondary">股票</th>
                        <th className="px-3 py-2 font-medium fin-text-secondary">现价</th>
                        <th className="px-3 py-2 font-medium fin-text-secondary">涨跌幅</th>
                        <th className="px-3 py-2 font-medium fin-text-secondary">涨速</th>
                        <th className="px-3 py-2 font-medium fin-text-secondary">主力净流入</th>
                        <th className="px-3 py-2 font-medium fin-text-secondary">换手率</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(stockMoves?.items || []).map((item) => (
                        <tr key={item.code} className="border-b fin-divider/40 hover:bg-slate-700/20">
                          <td className="px-3 py-2">
                            <div className="font-medium fin-text-primary">{item.rank}. {item.name}</div>
                            <div className="text-xs fin-text-tertiary font-mono">{item.code}</div>
                          </td>
                          <td className="px-3 py-2 font-mono fin-text-primary">{item.price.toFixed(2)}</td>
                          <td className={`px-3 py-2 font-mono ${item.changePercent >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                            {item.changePercent >= 0 ? '+' : ''}
                            {item.changePercent.toFixed(2)}%
                          </td>
                          <td className={`px-3 py-2 font-mono ${item.speed >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                            {item.speed >= 0 ? '+' : ''}
                            {item.speed.toFixed(2)}%
                          </td>
                          <td className={`px-3 py-2 font-mono ${item.mainNetInflow >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                            {formatAmount(item.mainNetInflow)}
                          </td>
                          <td className="px-3 py-2 font-mono fin-text-primary">{item.turnoverRate.toFixed(2)}%</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          ) : (
            <div className="h-full fin-panel border fin-divider rounded-lg overflow-hidden flex flex-col">
              <div className="px-4 py-3 border-b fin-divider flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <TrendingUp className="w-4 h-4 text-accent-2" />
                  <span className="text-sm fin-text-primary font-medium">板块异动（资金流）</span>
                  {boardFlow?.updateTime ? (
                    <span className="text-xs fin-text-tertiary">更新: {boardFlow.updateTime}</span>
                  ) : null}
                </div>
                <div className="flex items-center gap-2">
                  {(['industry', 'concept', 'region'] as BoardCategory[]).map((item) => (
                    <button
                      key={item}
                      onClick={() => onChangeCategory(item)}
                      className={`px-2.5 py-1 rounded text-xs border ${
                        category === item
                          ? 'bg-accent/20 text-accent border-accent/40'
                          : 'fin-panel fin-text-secondary fin-divider hover:fin-text-primary'
                      }`}
                    >
                      {CATEGORY_LABELS[item]}
                    </button>
                  ))}
                  <button
                    onClick={() => {
                      resetAutoRefreshCountdown();
                      void loadBoardFlow(category, selectedBoard?.code);
                    }}
                    className="p-1.5 rounded fin-hover transition-colors"
                    disabled={loading}
                    title="刷新"
                  >
                    <RefreshCw className={`w-4 h-4 fin-text-secondary ${loading ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              </div>

              {loading ? (
                <div className="flex-1 flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 fin-text-secondary animate-spin" />
                </div>
              ) : error ? (
                <div className="flex-1 px-4 py-3 text-sm text-amber-300">{error}</div>
              ) : (
                <div className="flex-1 min-h-0 flex">
                  <div className="w-[58%] overflow-auto fin-scrollbar border-r fin-divider">
                    <table className="w-full text-sm">
                      <thead className="sticky top-0 fin-panel border-b fin-divider z-10">
                        <tr className="text-left">
                          <th className="px-3 py-2 font-medium fin-text-secondary">板块</th>
                          <th className="px-3 py-2 font-medium fin-text-secondary">涨跌幅</th>
                          <th className="px-3 py-2 font-medium fin-text-secondary">主力净流入</th>
                          <th className="px-3 py-2 font-medium fin-text-secondary">超大单</th>
                          <th className="px-3 py-2 font-medium fin-text-secondary">大单</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(boardFlow?.items || []).map((item) => {
                          const active = selectedBoard?.code === item.code;
                          return (
                            <tr
                              key={item.code}
                              className={`border-b fin-divider/40 cursor-pointer ${
                                active ? 'bg-accent/10' : 'hover:bg-slate-700/20'
                              }`}
                              onClick={() => {
                                setSelectedBoard(item);
                                void loadBoardLeaders(item.code);
                              }}
                            >
                              <td className="px-3 py-2">
                                <div className="font-medium fin-text-primary">{item.name}</div>
                                <div className="text-xs fin-text-tertiary font-mono">{item.code}</div>
                              </td>
                              <td className={`px-3 py-2 font-mono ${item.changePercent >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                                {item.changePercent >= 0 ? '+' : ''}
                                {item.changePercent.toFixed(2)}%
                              </td>
                              <td className={`px-3 py-2 font-mono ${item.mainNetInflow >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                                {formatAmount(item.mainNetInflow)}
                              </td>
                              <td className={`px-3 py-2 font-mono ${item.superNetInflow >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                                {formatAmount(item.superNetInflow)}
                              </td>
                              <td className={`px-3 py-2 font-mono ${item.largeNetInflow >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                                {formatAmount(item.largeNetInflow)}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                  <div className="flex-1 min-w-0 p-3 overflow-auto fin-scrollbar">
                    <div className="flex items-center justify-between mb-2">
                      <div className="text-sm fin-text-primary font-medium">龙头推荐</div>
                      {leaderResult?.updateTime ? (
                        <div className="text-xs fin-text-tertiary">更新: {leaderResult.updateTime}</div>
                      ) : null}
                    </div>
                    <div className="text-xs fin-text-tertiary mb-3">
                      推荐逻辑：涨跌幅 + 主力净流入 + 主力净流入占比综合评分
                    </div>
                    {selectedBoard ? (
                      <div className="text-xs fin-text-secondary mb-3">
                        当前板块：<span className="fin-text-primary">{selectedBoard.name}</span>
                        <span className="ml-1 font-mono fin-text-tertiary">{selectedBoard.code}</span>
                      </div>
                    ) : (
                      <div className="text-sm fin-text-tertiary">请先在左侧选择板块</div>
                    )}
                    {leaderLoading ? (
                      <div className="pt-6 flex items-center justify-center">
                        <RefreshCw className="w-4 h-4 fin-text-secondary animate-spin" />
                      </div>
                    ) : leaderError ? (
                      <div className="text-sm text-amber-300">{leaderError}</div>
                    ) : (
                      <div className="space-y-2">
                        {(leaderResult?.items || []).map((leader) => (
                          <div key={leader.code} className="fin-panel border fin-divider rounded-lg px-3 py-2">
                            <div className="flex items-center justify-between">
                              <div className="text-sm fin-text-primary">
                                {leader.rank}. {leader.name}
                                <span className="ml-1 text-xs font-mono fin-text-tertiary">{leader.code}</span>
                              </div>
                              <div className={`text-sm font-mono ${leader.changePercent >= 0 ? 'text-red-400' : 'text-green-400'}`}>
                                {leader.changePercent >= 0 ? '+' : ''}
                                {leader.changePercent.toFixed(2)}%
                              </div>
                            </div>
                            <div className="mt-1 text-xs fin-text-secondary flex items-center justify-between">
                              <span>主力净流入: {formatAmount(leader.mainNetInflow)}</span>
                              <span>占比: {leader.mainNetInflowRatio.toFixed(2)}%</span>
                              <span>评分: {leader.score.toFixed(2)}</span>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
      {trackerOpen ? (
        <FundFlowTrackerModal
          data={trackerData}
          category={category}
          loading={trackerLoading}
          error={trackerError}
          autoRefreshEnabled={autoRefreshEnabled}
          onClose={() => setTrackerOpen(false)}
          onRefresh={() => {
            void loadFundFlowTracker(category);
          }}
          onCategoryChange={(nextCategory) => {
            setCategory(nextCategory);
            resetAutoRefreshCountdown();
            void loadFundFlowOverview(nextCategory);
            void loadFundFlowTracker(nextCategory);
          }}
        />
      ) : null}
    </div>
  );
};
