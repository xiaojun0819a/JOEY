import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { StockList } from './components/StockList';
import { StockChartLW, type MainChartTemplate } from './components/StockChartLW';
import { OrderBook as OrderBookComponent } from './components/OrderBook';
import { F10Panel } from './components/F10Panel';
import { AgentRoom } from './components/AgentRoom';
import { SettingsDialog } from './components/SettingsDialog';
import { PositionDialog } from './components/PositionDialog';
import { TradeJournalDialog } from './components/TradeJournalDialog';
import { HotTrendDialog } from './components/HotTrendDialog';
import { LongHuBangDialog } from './components/LongHuBangDialog';
import { MarketMovesDialog } from './components/MarketMovesDialog';
import LowBuyScannerDialog, {
  type HistoryAutoCollectRequest,
  type HistoryAutoCollectStatus,
  type HistoryCollectRequest,
  type HistoryCollectResult,
  type LowBuyScannerResult,
  type LowBuyScannerRequest,
} from './components/LowBuyScannerDialog';
import LateDayChaseScannerDialog from './components/LateDayChaseScannerDialog';
import ModelRadarStrip from './components/ModelRadarStrip';
import WaveScannerDialog from './components/WaveScannerDialog';
import BoardReportDialog from './components/BoardReportDialog';
import { ResearchReportDialog } from './components/ResearchReportDialog';
import { MarketRegimeBadge } from './components/MarketRegimeBadge';
import { PaperPortfolioDialog } from './components/PaperPortfolioDialog';
import FundamentalScanDialog from './components/FundamentalScanDialog';
import { AddToGroupButton } from './components/AddToGroupButton';
import SafeBoundary from './components/SafeBoundary';
import { WelcomePage } from './components/WelcomePage';
import { useTheme } from './contexts/ThemeContext';
import { useCandleColor } from './contexts/CandleColorContext';
import { ResizeHandle } from './components/ResizeHandle';
import { isWailsGoReady, isWailsRuntimeReady, warnWailsUnavailable } from './utils/wailsEnv';
import { getWatchlist, addToWatchlist, removeFromWatchlist, syncTradeJournalWatchGroup } from './services/watchlistService';
import { getKLineData, getOrderBook, getStockRealTimeData } from './services/stockService';
import { getF10Overview, getF10Valuation } from './services/f10Service';
import {
  collectDailyHistory,
  getHistoryAutoCollectStatus,
  runCaoYuanStandardScanner4A,
  runCaoYuanZhuangScanner4B,
  runDipEntryScannerV8,
  runHotMoneyBreakoutScannerV7,
  runLimitPullbackScanner,
  runLowBuyScannerV1,
  runMonsterScannerV9,
  runMonsterScannerV10,
  runTailLazyScannerV2,
  runTailBuyScannerV6,
  runTripleVolumeScannerV5,
  runTailLazyReplayOnDate,
  runLowBuyReplayOnDate,
  updateHistoryAutoCollect,
} from './services/scannerService';
import { getOrCreateSession, StockSession, updateStockPosition } from './services/sessionService';
import { sellStockPosition } from './services/journalService';
import { getConfig, updateConfig } from './services/configService';
import { checkForUpdate } from './services/updateService';
import { useMarketEvents } from './hooks/useMarketEvents';
import { useMarketStatus } from './hooks/useMarketStatus';
import { Stock, KLineData, OrderBook, TimePeriod, Telegraph, MarketIndex, F10Overview, StockValuation } from './types';
import { Radio, Settings, List, Minus, Square, X, Copy, Briefcase, TrendingUp, BarChart3, Activity, RefreshCw, Search, Gauge, FileText, Wallet, ChevronDown, Maximize2 } from 'lucide-react';
import logo from './assets/images/logo.png';
import { GetTelegraphList, OpenURL, WindowMinimize, WindowMaximize, WindowClose } from '../wailsjs/go/main/App';
import { WindowIsMaximised, WindowGetSize } from '../wailsjs/runtime/runtime';

// 布局配置常量
const LAYOUT_DEFAULTS = {
  leftPanelWidth: 280,
  rightPanelWidth: 384,
  bottomPanelHeight: 132,
};

type LowBuyStrategyMode = 'lowbuy' | 'limit-pullback' | 'triple-volume' | 'tail-buy' | 'hot-money' | 'dip-entry' | 'monster' | 'monster-v10' | 'taillazy' | 'caoyuan-standard4a' | 'caoyuan-zhuang4b';
type ChartFullscreenMode = 'normal' | 'strategy';
const LAYOUT_MIN = {
  leftPanelWidth: 180,
  rightPanelWidth: 260,
  bottomPanelHeight: 96,
};
const LAYOUT_MAX = {
  leftPanelWidth: 760,
  rightPanelWidth: 860,
  bottomPanelHeight: 320,
};
const CENTER_PANEL_MIN_WIDTH = 640;
const RESIZE_HANDLES_TOTAL_WIDTH = 2;
const ORDERBOOK_COMPACT_CAP = 150;
const BOOTSTRAP_TIMEOUT_MS = 8000;
const RADAR_PANEL_MIN_HEIGHT = 72;
const RADAR_PANEL_MAX_HEIGHT = 560;
const RADAR_PANEL_DEFAULT_HEIGHT = 138;

type PricePanelState = {
  trendLabel: string;
  zoneLabel: string;
  zoneTone: 'high' | 'mid' | 'low';
};

type StockIdentityState = {
  boardLabel: string;
  volatilityLabel: string;
  volatilityTone: 'hot' | 'warm' | 'calm';
};

const clampValue = (value: number, min: number, max: number): number => (
  Math.max(min, Math.min(max, value))
);

const getLeftPanelMax = (viewportWidth: number, rightWidth: number): number => {
  const byViewport = viewportWidth - rightWidth - CENTER_PANEL_MIN_WIDTH - RESIZE_HANDLES_TOTAL_WIDTH;
  return Math.max(
    LAYOUT_MIN.leftPanelWidth,
    Math.min(LAYOUT_MAX.leftPanelWidth, byViewport),
  );
};

const getRightPanelMax = (viewportWidth: number, leftWidth: number): number => {
  const byViewport = viewportWidth - leftWidth - CENTER_PANEL_MIN_WIDTH - RESIZE_HANDLES_TOTAL_WIDTH;
  return Math.max(
    LAYOUT_MIN.rightPanelWidth,
    Math.min(LAYOUT_MAX.rightPanelWidth, byViewport),
  );
};

const normalizeSidePanelWidths = (
  viewportWidth: number,
  leftWidth: number,
  rightWidth: number,
): { left: number; right: number } => {
  let left = clampValue(leftWidth, LAYOUT_MIN.leftPanelWidth, getLeftPanelMax(viewportWidth, rightWidth));
  let right = clampValue(rightWidth, LAYOUT_MIN.rightPanelWidth, getRightPanelMax(viewportWidth, left));
  left = clampValue(left, LAYOUT_MIN.leftPanelWidth, getLeftPanelMax(viewportWidth, right));
  return { left, right };
};

const getPricePanelState = (stock: Stock): PricePanelState => {
  const trendLabel = stock.change > 0 ? '上涨' : stock.change < 0 ? '下跌' : '平盘';
  const range = stock.high - stock.low;
  if (range <= 0 || !Number.isFinite(range)) {
    return { trendLabel, zoneLabel: '区间中性', zoneTone: 'mid' };
  }
  const position = (stock.price - stock.low) / range;
  if (position >= 0.72) return { trendLabel, zoneLabel: '高位区', zoneTone: 'high' };
  if (position <= 0.28) return { trendLabel, zoneLabel: '低位区', zoneTone: 'low' };
  return { trendLabel, zoneLabel: '中位区', zoneTone: 'mid' };
};

const getPricePanelSizeClass = (priceText: string): string => {
  const length = priceText.length;
  if (length <= 5) return 'text-[36px]';
  if (length <= 6) return 'text-[clamp(30px,2.65vw,36px)]';
  return 'text-[clamp(26px,2.35vw,32px)]';
};

const getStockIdentityState = (stock: Stock): StockIdentityState => {
  const symbol = stock.symbol.toLowerCase();
  let boardLabel = '主板';
  if (symbol.includes('688') || symbol.includes('689')) boardLabel = '科创板';
  else if (symbol.includes('300') || symbol.includes('301')) boardLabel = '创业板';
  else if (symbol.includes('bj')) boardLabel = '北交所';

  const absChange = Math.abs(stock.changePercent);
  if (absChange >= 10) return { boardLabel, volatilityLabel: '高波动', volatilityTone: 'hot' };
  if (absChange >= 5) return { boardLabel, volatilityLabel: '强波动', volatilityTone: 'warm' };
  return { boardLabel, volatilityLabel: '低波动', volatilityTone: 'calm' };
};

type KLineUpdateMode = 'full' | 'incremental' | 'refresh';
type MultiCycleKLines = {
  daily: KLineData[];
  weekly: KLineData[];
  monthly: KLineData[];
};

const isMinuteTrendPeriod = (period: TimePeriod) => period === '1m' || period === '5d';

const getKLineRequestLength = (period: TimePeriod) => {
  if (period === '5d') return 1250;
  if (period === '30m' || period === '60m') return 320; // 分钟K:约40-80个交易日
  return period === '1m' ? 250 : 240;
};

const App: React.FC = () => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const [watchlist, setWatchlist] = useState<Stock[]>([]);
  const [selectedSymbol, setSelectedSymbol] = useState<string>('');
  const [, setPriceQuoteUpdatedAt] = useState<Date>(() => new Date());
  const [clockNow, setClockNow] = useState<Date>(() => new Date());
  const [currentSession, setCurrentSession] = useState<StockSession | null>(null);
  const [timePeriod, setTimePeriod] = useState<TimePeriod>('1m');
  const [kLineData, setKLineData] = useState<KLineData[]>([]);
  const [kLineUpdateMode, setKLineUpdateMode] = useState<KLineUpdateMode>('full');
  const [orderBook, setOrderBook] = useState<OrderBook>({ bids: [], asks: [] });
  const [marketMessage, setMarketMessage] = useState<string>('市场数据加载中...');
  const [telegraphList, setTelegraphList] = useState<Telegraph[]>([]);
  const [showTelegraphList, setShowTelegraphList] = useState(false);
  const [telegraphLoading, setTelegraphLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [showSettings, setShowSettings] = useState(false);
  const [settingsInitialTab, setSettingsInitialTab] = useState<'update' | undefined>(undefined);
  const [updateAvailable, setUpdateAvailable] = useState<string>('');
  const [showPosition, setShowPosition] = useState(false);
  const [showHotTrend, setShowHotTrend] = useState(false);
  const [showLongHuBang, setShowLongHuBang] = useState(false);
  const [showMarketMoves, setShowMarketMoves] = useState(false);
  const [showLowBuyScanner, setShowLowBuyScanner] = useState(false);
  const [lowBuyStrategyMode, setLowBuyStrategyMode] = useState<LowBuyStrategyMode>('lowbuy');
  const [lowBuyStrategyTitle, setLowBuyStrategyTitle] = useState('低吸选股策略1');
  const [lowBuyStrategySubtitle, setLowBuyStrategySubtitle] = useState('V1.2 高胜率短线规则（全A · 回踩偏好 · Top3）');
  const [showLowBuyStrategyMenu, setShowLowBuyStrategyMenu] = useState(false);
  const [showFundamental, setShowFundamental] = useState(false);
  const [showLateDayChaseScanner, setShowLateDayChaseScanner] = useState(false);
  const [showWaveScanner, setShowWaveScanner] = useState(false);
  const [showPaper, setShowPaper] = useState(false);
  const [showWaveModel, setShowWaveModel] = useState(false);
  const [showJournal, setShowJournal] = useState(false);
  const [showF10, setShowF10] = useState(false);
  const [showChartFullscreen, setShowChartFullscreen] = useState(false);
  const [showBoardReport, setShowBoardReport] = useState(false);
  const [showResearchReport, setShowResearchReport] = useState(false);
  const [chartFullscreenMode, setChartFullscreenMode] = useState<ChartFullscreenMode>('normal');
  const [mainChartTemplate, setMainChartTemplate] = useState<MainChartTemplate>('standard');
  const [marketIndices, setMarketIndices] = useState<MarketIndex[]>([]);
  const [isMaximized, setIsMaximized] = useState(false);
  const [f10Overview, setF10Overview] = useState<F10Overview | null>(null);
  const [valuationSnapshot, setValuationSnapshot] = useState<StockValuation | null>(null);
  const [multiCycleKLines, setMultiCycleKLines] = useState<MultiCycleKLines>({
    daily: [],
    weekly: [],
    monthly: [],
  });
  const [f10Loading, setF10Loading] = useState(false);
  const [f10Error, setF10Error] = useState('');
  const [syncingRadar, setSyncingRadar] = useState(false);
  const [loadingHint, setLoadingHint] = useState('');
  const [scannerLoading, setScannerLoading] = useState(false);
  const [scannerError, setScannerError] = useState('');
  const [scannerResult, setScannerResult] = useState<LowBuyScannerResult | null>(null);
  const [scannerResultsByMode, setScannerResultsByMode] = useState<Partial<Record<LowBuyStrategyMode, LowBuyScannerResult>>>({});
  const [scannerErrorsByMode, setScannerErrorsByMode] = useState<Partial<Record<LowBuyStrategyMode, string>>>({});
  const [previewStock, setPreviewStock] = useState<Stock | null>(null);
  const klineRequestIdRef = useRef(0);
  const valuationRequestIdRef = useRef(0);
  const multiCycleRequestIdRef = useRef(0);
  const lowBuyScanRequestIdRef = useRef(0);
  const lowBuyStrategyMenuRef = useRef<HTMLDivElement>(null);

  const withTimeoutOr = useCallback(async <T,>(promise: Promise<T>, timeoutMs: number, fallback: T): Promise<T> => {
    return await new Promise<T>((resolve) => {
      const timer = setTimeout(() => resolve(fallback), timeoutMs);
      promise
        .then(value => {
          clearTimeout(timer);
          resolve(value);
        })
        .catch(() => {
          clearTimeout(timer);
          resolve(fallback);
        });
    });
  }, []);

  // 使用纯前端市场状态判断
  const { status: marketStatus } = useMarketStatus();

  // 布局状态
  const [leftPanelWidth, setLeftPanelWidth] = useState(LAYOUT_DEFAULTS.leftPanelWidth);
  const [rightPanelWidth, setRightPanelWidth] = useState(LAYOUT_DEFAULTS.rightPanelWidth);
  const [bottomPanelHeight, setBottomPanelHeight] = useState(LAYOUT_DEFAULTS.bottomPanelHeight);
  const [radarPanelHeight, setRadarPanelHeight] = useState(RADAR_PANEL_DEFAULT_HEIGHT);
  const saveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clampSidePanelsToViewport = useCallback((left: number, right: number) => {
    const viewportWidth = typeof window !== 'undefined' ? window.innerWidth : 1920;
    return normalizeSidePanelWidths(viewportWidth, left, right);
  }, []);

  const selectedStock = useMemo(() => {
    const normalizedSelected = String(selectedSymbol || '').trim().toLowerCase();
    const watched = watchlist.find(s => s.symbol.toLowerCase() === normalizedSelected);
    if (watched) return watched;
    if (previewStock && previewStock.symbol.toLowerCase() === normalizedSelected) return previewStock;
    return watchlist[0] || previewStock;
  }, [selectedSymbol, watchlist, previewStock]);
  const watchlistSubscriptionKey = useMemo(() => (
    Array.from(new Set(watchlist.map(stock => String(stock.symbol || '').trim()).filter(Boolean))).join(',')
  ), [watchlist]);

  const safeKLineData = useMemo(() => (
    (kLineData || []).filter((item): item is KLineData => (
      !!item
      && typeof item.time === 'string'
      && Number.isFinite(item.open)
      && Number.isFinite(item.high)
      && Number.isFinite(item.low)
      && Number.isFinite(item.close)
      && Number.isFinite(item.volume)
    ))
  ), [kLineData]);

  const pricePanelState = useMemo(
    () => selectedStock ? getPricePanelState(selectedStock) : { trendLabel: '平盘', zoneLabel: '区间中性', zoneTone: 'mid' as const },
    [selectedStock],
  );
  const stockIdentityState = useMemo(
    () => selectedStock ? getStockIdentityState(selectedStock) : { boardLabel: '主板', volatilityLabel: '低波动', volatilityTone: 'calm' as const },
    [selectedStock],
  );
  const positionSummary = useMemo(() => {
    const pos = currentSession?.position;
    if (!selectedStock || !pos || pos.shares <= 0) return null;
    const marketValue = pos.shares * selectedStock.price;
    const costAmount = pos.shares * pos.costPrice;
    const profitLoss = marketValue - costAmount;
    const profitPercent = costAmount > 0 ? (profitLoss / costAmount) * 100 : 0;
    return {
      shares: pos.shares,
      profitLoss,
      profitPercent,
      isProfit: profitLoss >= 0,
    };
  }, [currentSession?.position, selectedStock]);
  const liveClockTime = useMemo(
    () => clockNow.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
    [clockNow],
  );
  const selectedPriceText = useMemo(
    () => selectedStock ? selectedStock.price.toFixed(2) : '--',
    [selectedStock?.price],
  );
  const selectedPriceSizeClass = useMemo(
    () => getPricePanelSizeClass(selectedPriceText),
    [selectedPriceText],
  );

  const refreshCurrentSession = useCallback(async () => {
    if (!selectedStock?.symbol) return;
    const session = await getOrCreateSession(selectedStock.symbol, selectedStock.name);
    setCurrentSession(session);
  }, [selectedStock?.symbol, selectedStock?.name]);

  const refreshTradeJournalWatchGroup = useCallback(async () => {
    await syncTradeJournalWatchGroup();
    const list = await withTimeoutOr(getWatchlist(), 3500, [] as Stock[]);
    setWatchlist(list);
    window.dispatchEvent(new CustomEvent('watchlist-groups-changed'));
    const normalizedSelected = String(selectedSymbol || '').trim().toLowerCase();
    if ((!normalizedSelected || !list.some(stock => stock.symbol.toLowerCase() === normalizedSelected)) && list.length > 0) {
      setSelectedSymbol(list[0].symbol);
    }
  }, [selectedSymbol, withTimeoutOr]);

  const handleTradeJournalChanged = useCallback(async () => {
    await refreshTradeJournalWatchGroup();
    await refreshCurrentSession();
  }, [refreshTradeJournalWatchGroup, refreshCurrentSession]);

  useEffect(() => {
    const timer = window.setInterval(() => setClockNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  // 启动后静默检查更新(CheckForUpdate 在 remoteBridge 白名单里,始终走本地绑定查 GitHub Release);
  // dev 本地构建时后端会带 error(版本号不可解析),不弹提醒
  useEffect(() => {
    const timer = window.setTimeout(async () => {
      try {
        const info = await checkForUpdate();
        if (info.hasUpdate && !info.error) setUpdateAvailable(info.latestVersion);
      } catch {
        // 静默失败:离线/GitHub 不可达时不打扰
      }
    }, 8000);
    return () => window.clearTimeout(timer);
  }, []);

  // 处理股票数据更新（来自后端推送）
  const handleStockUpdate = useCallback((stocks: Stock[]) => {
    if (!stocks || !Array.isArray(stocks)) return;
    const currentSymbol = selectedSymbol.trim().toLowerCase();
    if (currentSymbol && stocks.some(stock => String(stock.symbol || '').trim().toLowerCase() === currentSymbol)) {
      setPriceQuoteUpdatedAt(new Date());
    }
    setWatchlist(prev => {
      // 更新已有股票的数据
      return prev.map(stock => {
        const symbol = String(stock.symbol || '').trim().toLowerCase();
        const updated = stocks.find(s => String(s.symbol || '').trim().toLowerCase() === symbol);
        return updated || stock;
      });
    });
  }, [selectedSymbol]);

  // 处理盘口数据更新（来自后端推送）
  const handleOrderBookUpdate = useCallback((data: OrderBook) => {
    setOrderBook(data);
  }, []);

  // 处理快讯数据更新（来自后端推送）
  const handleTelegraphUpdate = useCallback((data: Telegraph) => {
    if (data && data.content) {
      setMarketMessage(`[${data.time}] ${data.content}`);
    }
  }, []);

  // 开机即拉最新一条快讯，避免长时间停在"市场数据加载中..."
  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!isWailsGoReady()) return;
      try {
        const list = await GetTelegraphList();
        if (!cancelled && list && list.length > 0) {
          const t = list[0];
          setMarketMessage(t.time ? `[${t.time}] ${t.content}` : t.content);
        }
      } catch { /* ignore */ }
    })();
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (!showLowBuyStrategyMenu) return;
    const handleClickOutside = (event: MouseEvent) => {
      if (lowBuyStrategyMenuRef.current && !lowBuyStrategyMenuRef.current.contains(event.target as Node)) {
        setShowLowBuyStrategyMenu(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showLowBuyStrategyMenu]);

  useEffect(() => {
    if (!showChartFullscreen) return;
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowChartFullscreen(false);
      }
    };
    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [showChartFullscreen]);

  // 处理大盘指数更新（来自后端推送）
  const handleMarketIndicesUpdate = useCallback((indices: MarketIndex[]) => {
    if (indices) {
      setMarketIndices(indices);
    }
  }, []);

  // 处理K线数据更新（来自后端推送，支持增量）
  const handleKLineUpdate = useCallback((data: { code: string; period: string; data: KLineData[]; incremental?: boolean }) => {
    if (!data || data.code !== selectedSymbol || data.period !== timePeriod) return;

    if (data.incremental && data.data.length > 0) {
      setKLineUpdateMode('incremental');
      // 增量更新：合并最新K线
      setKLineData(prev => {
        if (prev.length === 0) return data.data;
        const newBar = data.data[0];
        const lastIdx = prev.length - 1;
        // 同一时间戳则更新，否则追加
        if (prev[lastIdx].time === newBar.time) {
          const updated = [...prev];
          updated[lastIdx] = newBar;
          return updated;
        }
        return [...prev.slice(-(getKLineRequestLength(timePeriod) - 1)), newBar];
      });
    } else {
      // 后端定时推送：用 refresh 模式更新数据但保留用户缩放状态
      if (Array.isArray(data.data) && data.data.length > 0) {
        setKLineUpdateMode('refresh');
        setKLineData(data.data);
      }
    }
  }, [selectedSymbol, timePeriod]);

  const syncWindowMaximizedState = useCallback(async () => {
    if (!isWailsRuntimeReady()) return;
    try {
      const maximized = await WindowIsMaximised();
      setIsMaximized(maximized);
    } catch {
      // ignore runtime query failures and keep current UI state
    }
  }, []);

  const toggleWindowMaximize = useCallback(async () => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('窗口最大化', 'go');
      return;
    }
    try {
      await WindowMaximize();
      await syncWindowMaximizedState();
    } catch {
      // fallback to optimistic toggle when runtime query is unavailable
      setIsMaximized(prev => !prev);
    }
  }, [syncWindowMaximizedState]);

  // 保存布局配置（防抖）
  const saveLayoutConfig = useCallback(async (
    left: number, right: number, bottom: number,
    winWidth?: number, winHeight?: number
  ) => {
    if (!isWailsGoReady() || !isWailsRuntimeReady()) return;
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
    }
    saveTimeoutRef.current = setTimeout(async () => {
      try {
        const config = await getConfig();
        const size = await WindowGetSize();
        config.layout = {
          leftPanelWidth: left,
          rightPanelWidth: right,
          bottomPanelHeight: bottom,
          windowWidth: winWidth ?? size.w,
          windowHeight: winHeight ?? size.h,
        };
        await updateConfig(config);
      } catch (err) {
        console.error('Failed to save layout config:', err);
      }
    }, 500);
  }, []);

  // 左侧面板 resize
  const handleLeftResize = useCallback((delta: number) => {
    setLeftPanelWidth(prev => {
      const next = clampSidePanelsToViewport(prev + delta, rightPanelWidth);
      return next.left;
    });
  }, [rightPanelWidth, clampSidePanelsToViewport]);

  // 右侧面板 resize
  const handleRightResize = useCallback((delta: number) => {
    setRightPanelWidth(prev => {
      const next = clampSidePanelsToViewport(leftPanelWidth, prev - delta);
      return next.right;
    });
  }, [leftPanelWidth, clampSidePanelsToViewport]);

  // 模型驾驶舱高度 resize（向上拖动会收缩，向下拖动会展开）
  const handleRadarResize = useCallback((delta: number) => {
    setRadarPanelHeight(prev => {
      const viewportHeight = typeof window !== 'undefined' ? window.innerHeight : 1200;
      const dynamicMax = Math.max(
        RADAR_PANEL_MIN_HEIGHT,
        Math.min(RADAR_PANEL_MAX_HEIGHT, Math.round(viewportHeight * 0.58)),
      );
      return clampValue(prev + delta, RADAR_PANEL_MIN_HEIGHT, dynamicMax);
    });
  }, []);

  // resize 结束时保存配置
  const handleResizeEnd = useCallback(() => {
    saveLayoutConfig(leftPanelWidth, rightPanelWidth, bottomPanelHeight);
  }, [leftPanelWidth, rightPanelWidth, bottomPanelHeight, saveLayoutConfig]);

  // 监听窗口 resize 事件
  useEffect(() => {
    const windowResizeTimeoutRef = { current: null as ReturnType<typeof setTimeout> | null };
    const handleWindowResize = () => {
      if (windowResizeTimeoutRef.current) {
        clearTimeout(windowResizeTimeoutRef.current);
      }
      windowResizeTimeoutRef.current = setTimeout(() => {
        saveLayoutConfig(leftPanelWidth, rightPanelWidth, bottomPanelHeight);
      }, 500);
    };
    window.addEventListener('resize', handleWindowResize);
    return () => {
      window.removeEventListener('resize', handleWindowResize);
      if (windowResizeTimeoutRef.current) {
        clearTimeout(windowResizeTimeoutRef.current);
      }
    };
  }, [leftPanelWidth, rightPanelWidth, bottomPanelHeight, saveLayoutConfig]);

  // Keep orderbook compact so the middle chart has more visual space.
  useEffect(() => {
    if (bottomPanelHeight > ORDERBOOK_COMPACT_CAP) {
      setBottomPanelHeight(ORDERBOOK_COMPACT_CAP);
    }
  }, [bottomPanelHeight]);

  // Prevent an all-black startup if any bridge call gets stuck.
  useEffect(() => {
    if (!loading) return;
    const timer = setTimeout(() => {
      setLoadingHint('初始化超时，已切换到降级界面');
      setLoading(false);
    }, BOOTSTRAP_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [loading]);

  // 获取快讯列表
  const handleShowTelegraphList = async () => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('快讯列表', 'go');
      setShowTelegraphList(prev => !prev);
      setTelegraphLoading(false);
      if (!showTelegraphList) {
        setTelegraphList([]);
      }
      return;
    }
    if (!showTelegraphList) {
      setShowTelegraphList(true);
      setTelegraphLoading(true);
      try {
        const list = await GetTelegraphList();
        setTelegraphList(list || []);
      } finally {
        setTelegraphLoading(false);
      }
    } else {
      setShowTelegraphList(false);
    }
  };

  // 打开快讯链接
  const handleOpenTelegraph = (telegraph: Telegraph) => {
    if (telegraph.url) {
      if (isWailsGoReady()) {
        OpenURL(telegraph.url);
      } else {
        window.open(telegraph.url, '_blank', 'noopener,noreferrer');
      }
    }
    setShowTelegraphList(false);
  };

  const handleWindowMinimize = useCallback(() => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('窗口最小化', 'go');
      return;
    }
    void WindowMinimize();
  }, []);

  const handleWindowClose = useCallback(() => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('关闭窗口', 'go');
      return;
    }
    void WindowClose();
  }, []);

  const fetchSelectedF10 = useCallback(async (symbol: string, options?: { silent?: boolean }) => {
    if (!symbol) return;
    const silent = options?.silent === true;
    if (!silent) {
      setF10Loading(true);
      setF10Error('');
    }
    try {
      const overview = await getF10Overview(symbol);
      setF10Overview(overview);
    } catch (err) {
      console.error('Failed to load F10 overview:', err);
      if (!silent) {
        setF10Error(err instanceof Error ? err.message : '获取F10数据失败');
      }
    } finally {
      if (!silent) {
        setF10Loading(false);
      }
    }
  }, []);

  const fetchSelectedValuation = useCallback(async (symbol: string) => {
    if (!symbol) return;
    const requestId = ++valuationRequestIdRef.current;
    try {
      const valuation = await getF10Valuation(symbol);
      if (requestId !== valuationRequestIdRef.current) return;
      if (valuation && (valuation.totalMarketCap || valuation.totalShares || valuation.price)) {
        setValuationSnapshot(valuation);
      }
    } catch (err) {
      console.warn('Failed to load valuation snapshot:', err);
    }
  }, []);

  const fetchMultiCycleKLines = useCallback(async (symbol: string) => {
    if (!symbol) return;
    const requestId = ++multiCycleRequestIdRef.current;
    const [daily, weekly, monthly] = await Promise.all([
      withTimeoutOr(getKLineData(symbol, '1d', 260), 5000, [] as KLineData[]),
      withTimeoutOr(getKLineData(symbol, '1w', 220), 5000, [] as KLineData[]),
      withTimeoutOr(getKLineData(symbol, '1mo', 180), 5000, [] as KLineData[]),
    ]);
    if (requestId !== multiCycleRequestIdRef.current) return;
    setMultiCycleKLines({
      daily: Array.isArray(daily) ? daily : [],
      weekly: Array.isArray(weekly) ? weekly : [],
      monthly: Array.isArray(monthly) ? monthly : [],
    });
  }, [withTimeoutOr]);

  const forceSyncSelectedStock = useCallback(async () => {
    if (!selectedSymbol) return;
    setSyncingRadar(true);
    try {
      const dataLen = getKLineRequestLength(timePeriod);
      const [latestKline] = await Promise.all([
        withTimeoutOr(getKLineData(selectedSymbol, timePeriod, dataLen), 4000, [] as KLineData[]),
        withTimeoutOr(fetchSelectedF10(selectedSymbol, { silent: true }), 5000, undefined as void | undefined),
        withTimeoutOr(fetchSelectedValuation(selectedSymbol), 5000, undefined as void | undefined),
        withTimeoutOr(fetchMultiCycleKLines(selectedSymbol), 5500, undefined as void | undefined),
      ]);
      if (Array.isArray(latestKline) && latestKline.length > 0) {
        setKLineUpdateMode('refresh');
        setKLineData(latestKline);
      }
    } finally {
      setSyncingRadar(false);
    }
  }, [selectedSymbol, timePeriod, withTimeoutOr, fetchSelectedF10, fetchSelectedValuation, fetchMultiCycleKLines]);

  const handleShowTrend = useCallback(() => {
    setShowF10(false);
  }, []);

  const handleShowF10 = useCallback(() => {
    setShowF10(true);
  }, []);

  const handleMainChartTemplateChange = useCallback((template: MainChartTemplate) => {
    setMainChartTemplate(template);
    if (template === 'openEatFish') {
      setShowF10(false);
      if (isMinuteTrendPeriod(timePeriod)) {
        setTimePeriod('1d');
      }
    }
  }, [timePeriod]);

  const handleOpenChartFullscreen = useCallback(() => {
    setShowF10(false);
    setChartFullscreenMode('normal');
    setShowChartFullscreen(true);
  }, []);

  const handlePageRefresh = useCallback(() => {
    window.location.reload();
  }, []);

  const handleOpenLowBuyScanner = useCallback((title = '低吸选股策略1', subtitle = 'V1.2 高胜率短线规则（全A · 回踩偏好 · Top3）', mode: LowBuyStrategyMode = 'lowbuy') => {
    lowBuyScanRequestIdRef.current += 1;
    setLowBuyStrategyMode(mode);
    setLowBuyStrategyTitle(title);
    setLowBuyStrategySubtitle(subtitle);
    setShowLowBuyScanner(true);
    setShowLowBuyStrategyMenu(false);
    setScannerLoading(false);
    setScannerError(scannerErrorsByMode[mode] || '');
    setScannerResult(scannerResultsByMode[mode] || null);
  }, [scannerErrorsByMode, scannerResultsByMode]);

  const handleOpenLowBuyTailStrategy = useCallback(() => {
    handleOpenLowBuyScanner('低吸尾盘策略2', '尾盘懒人V2（量比1-2.5 · 涨幅3-6% · 换手5-10% · 多头排列/新高/形态）', 'taillazy');
  }, [handleOpenLowBuyScanner]);

  const handleOpenLimitPullbackStrategy = useCallback(() => {
    handleOpenLowBuyScanner('涨停回调低吸4', '近期涨停强启动 · 缩量回踩 · 站稳5/10日线后低吸', 'limit-pullback');
  }, [handleOpenLowBuyScanner]);

  const handleOpenTripleVolumeStrategy = useCallback(() => {
    handleOpenLowBuyScanner('三倍量策略5', '未涨停阳线 · 成交量≥前一日3倍 · 一阳穿MA5/10/20/30', 'triple-volume');
  }, [handleOpenLowBuyScanner]);

  const handleOpenTailBuyStrategy = useCallback(() => {
    handleOpenLowBuyScanner('尾盘买入策略6', '昨日资金强势触发 · 今日阴线回踩 · 尾盘确认承接', 'tail-buy');
  }, [handleOpenLowBuyScanner]);

  const handleOpenHotMoneyStrategy = useCallback(() => {
    handleOpenLowBuyScanner('游资突破策略7', '游资涨停结构 · 量能倍率1-5 · 流通股本分档', 'hot-money');
  }, [handleOpenLowBuyScanner]);

  const handleOpenDipEntryStrategy = useCallback(() => {
    handleOpenLowBuyScanner('低吸入场策略8', 'RSI短线反转 · 快速RSI过线 · 动能底背离三选二', 'dip-entry');
  }, [handleOpenLowBuyScanner]);

  const handleOpenMonsterStrategy = useCallback(() => {
    handleOpenLowBuyScanner('捉妖策略9', '原“捉妖选股”可落地复刻 · 妖股初启/突破/布林爆发/低点反抽', 'monster');
  }, [handleOpenLowBuyScanner]);

  const handleOpenMonsterV10Strategy = useCallback(() => {
    handleOpenLowBuyScanner('捉妖策略10', '通达信公式严格复刻 · GGZY_ZS=FILTER(GGZY_IG=1,3)', 'monster-v10');
  }, [handleOpenLowBuyScanner]);

  const handleOpenLateDayStrengthStrategy = useCallback(() => {
    setShowLateDayChaseScanner(true);
    setShowLowBuyStrategyMenu(false);
  }, []);

  const handleRunLowBuyScanner = useCallback(async (req: LowBuyScannerRequest) => {
    const requestId = ++lowBuyScanRequestIdRef.current;
    const mode = lowBuyStrategyMode;
    setScannerLoading(true);
    setScannerError('');
    setScannerErrorsByMode(prev => ({ ...prev, [mode]: '' }));
    try {
      const result = mode === 'taillazy'
        ? await runTailLazyScannerV2(req)
        : mode === 'limit-pullback'
          ? await runLimitPullbackScanner(req)
        : mode === 'triple-volume'
          ? await runTripleVolumeScannerV5(req)
        : mode === 'tail-buy'
          ? await runTailBuyScannerV6(req)
        : mode === 'hot-money'
          ? await runHotMoneyBreakoutScannerV7(req)
        : mode === 'dip-entry'
          ? await runDipEntryScannerV8(req)
        : mode === 'monster'
          ? await runMonsterScannerV9(req)
        : mode === 'monster-v10'
          ? await runMonsterScannerV10(req)
        : mode === 'caoyuan-standard4a'
          ? await runCaoYuanStandardScanner4A(req)
          : mode === 'caoyuan-zhuang4b'
            ? await runCaoYuanZhuangScanner4B(req)
            : await runLowBuyScannerV1(req);
      if (requestId !== lowBuyScanRequestIdRef.current) return;
      if (!result) {
        const message = '当前为浏览器预览模式，请从 Wails 开发入口打开后再扫描。';
        setScannerError(message);
        setScannerErrorsByMode(prev => ({ ...prev, [mode]: message }));
        return;
      }
      setScannerResult(result);
      setScannerResultsByMode(prev => ({ ...prev, [mode]: result }));
    } catch (err) {
      if (requestId !== lowBuyScanRequestIdRef.current) return;
      const message = err instanceof Error ? err.message : '扫描失败，请稍后重试';
      setScannerError(message);
      setScannerErrorsByMode(prev => ({ ...prev, [mode]: message }));
    } finally {
      if (requestId === lowBuyScanRequestIdRef.current) {
        setScannerLoading(false);
      }
    }
  }, [lowBuyStrategyMode]);

  const handleRunTailLazyReplay = useCallback(async (date: string) => {
    setScannerLoading(true);
    setScannerError('');
    try {
      const result = lowBuyStrategyMode === 'taillazy'
        ? await runTailLazyReplayOnDate(date, 30)
        : await runLowBuyReplayOnDate(date, 30, 1.5);
      if (!result) {
        setScannerError('当前为浏览器预览模式，请从 Wails 开发入口打开后再复盘。');
        setScannerResult(null);
        return;
      }
      setScannerResult(result);
    } catch (err) {
      setScannerError(err instanceof Error ? err.message : '历史复盘失败，请稍后重试');
      setScannerResult(null);
    } finally {
      setScannerLoading(false);
    }
  }, [lowBuyStrategyMode]);

  const handleCollectDailyHistory = useCallback(async (req: HistoryCollectRequest): Promise<HistoryCollectResult | null> => {
    return await collectDailyHistory(req);
  }, []);

  const handleGetHistoryAutoCollectStatus = useCallback(async (): Promise<HistoryAutoCollectStatus | null> => {
    return await getHistoryAutoCollectStatus();
  }, []);

  const handleUpdateHistoryAutoCollect = useCallback(async (req: HistoryAutoCollectRequest): Promise<HistoryAutoCollectStatus | null> => {
    return await updateHistoryAutoCollect(req);
  }, []);

  // 使用市场事件 Hook
  const { subscribe, subscribeOrderBook, subscribeKLine } = useMarketEvents({
    onStockUpdate: handleStockUpdate,
    onOrderBookUpdate: handleOrderBookUpdate,
    onTelegraphUpdate: handleTelegraphUpdate,
    onMarketIndicesUpdate: handleMarketIndicesUpdate,
    onKLineUpdate: handleKLineUpdate,
  });

  useEffect(() => {
    const codes = watchlistSubscriptionKey.split(',').filter(Boolean);
    if (codes.length === 0) return;
    subscribe(codes);
  }, [watchlistSubscriptionKey, subscribe]);

  // Handle Adding Stock
  const handleAddStock = async (newStock: Stock) => {
    if (!watchlist.find(s => s.symbol === newStock.symbol)) {
      await addToWatchlist(newStock);
      setWatchlist(prev => [...prev, newStock]);
      // 添加后自动选中新股票并加载数据
      setPreviewStock(null);
      setSelectedSymbol(newStock.symbol);
      setPriceQuoteUpdatedAt(new Date());
      // 先清空 session，避免显示旧股票的消息
      setCurrentSession(null);
      subscribeOrderBook(newStock.symbol);
      // 加载 Session 和盘口数据
      const [session, orderBookData] = await Promise.all([
        getOrCreateSession(newStock.symbol, newStock.name),
        getOrderBook(newStock.symbol)
      ]);
      setCurrentSession(session);
      setOrderBook(orderBookData);
    }
  };

  const handleAddFromLongHuBang = useCallback(async (newStock: Stock): Promise<boolean> => {
    const normalizedSymbol = String(newStock.symbol || '').trim().toLowerCase();
    if (!normalizedSymbol) return false;
    if (watchlist.some(s => s.symbol.toLowerCase() === normalizedSymbol)) return false;

    const stockToAdd: Stock = { ...newStock, symbol: normalizedSymbol };
    await addToWatchlist(stockToAdd);
    setWatchlist(prev => (
      prev.some(s => s.symbol.toLowerCase() === normalizedSymbol)
        ? prev
        : [...prev, stockToAdd]
    ));
    return true;
  }, [watchlist]);

  const handleOpenStockFromStrategy = useCallback(async (stock: Stock) => {
    const normalizedSymbol = String(stock.symbol || '').trim().toLowerCase();
    if (!normalizedSymbol) return;
    const existing = watchlist.find(s => s.symbol.toLowerCase() === normalizedSymbol);
    const targetStock: Stock = existing || { ...stock, symbol: normalizedSymbol };

    setShowF10(false);
    setMainChartTemplate('openEatFish');
    setTimePeriod('1d');
    setChartFullscreenMode('strategy');
    setShowChartFullscreen(true);
    setSelectedSymbol(normalizedSymbol);
    setPriceQuoteUpdatedAt(new Date());
    setCurrentSession(null);
    if (!existing) {
      setPreviewStock(targetStock);
    } else {
      setPreviewStock(null);
    }
    subscribeOrderBook(normalizedSymbol);

    const [session, orderBookData, realtime] = await Promise.all([
      getOrCreateSession(normalizedSymbol, targetStock.name),
      getOrderBook(normalizedSymbol),
      getStockRealTimeData([normalizedSymbol]).catch(() => [] as Stock[]),
    ]);
    const realtimeStock = Array.isArray(realtime) ? realtime.find(s => s.symbol.toLowerCase() === normalizedSymbol) : null;
    if (!existing && realtimeStock) {
      setPreviewStock(realtimeStock);
      setPriceQuoteUpdatedAt(new Date());
    }
    setCurrentSession(session);
    setOrderBook(orderBookData);
  }, [watchlist, subscribeOrderBook]);

  // Handle Removing Stock
  const handleRemoveStock = async (symbol: string) => {
    await removeFromWatchlist(symbol);
    setWatchlist(prev => prev.filter(s => s.symbol !== symbol));
    // 如果删除的是当前选中的股票，切换到第一个
    if (symbol === selectedSymbol) {
      const remaining = watchlist.filter(s => s.symbol !== symbol);
      if (remaining.length > 0) {
        handleSelectStock(remaining[0].symbol);
      }
    }
  };

  // Handle Stock Selection - Load Session and sync data
  const handleSelectStock = async (symbol: string) => {
    setPreviewStock(null);
    setSelectedSymbol(symbol);
    setPriceQuoteUpdatedAt(new Date());
    // 订阅该股票的盘口推送
    subscribeOrderBook(symbol);
    const stock = watchlist.find(s => s.symbol === symbol);
    if (stock) {
      // 并行加载 Session 和盘口数据
      const [session, orderBookData] = await Promise.all([
        getOrCreateSession(symbol, stock.name),
        getOrderBook(symbol)
      ]);
      setCurrentSession(session);
      setOrderBook(orderBookData);
    }
  };

  // Load watchlist on mount
  useEffect(() => {
    const loadWatchlist = async () => {
      try {
        // 加载布局配置
        const config = await withTimeoutOr(getConfig(), 3500, null as any);
        if (config.layout) {
          const layoutLeft = config.layout.leftPanelWidth > 0 ? config.layout.leftPanelWidth : LAYOUT_DEFAULTS.leftPanelWidth;
          const layoutRight = config.layout.rightPanelWidth > 0 ? config.layout.rightPanelWidth : LAYOUT_DEFAULTS.rightPanelWidth;
          const normalized = clampSidePanelsToViewport(layoutLeft, layoutRight);
          setLeftPanelWidth(normalized.left);
          setRightPanelWidth(normalized.right);
          if (config.layout.bottomPanelHeight > 0) {
            // Keep the orderbook compact to prioritize chart visibility.
            setBottomPanelHeight(Math.min(config.layout.bottomPanelHeight, 150));
          }
        }

        const list = await withTimeoutOr(getWatchlist(), 3500, [] as Stock[]);
        setWatchlist(list);
        if (list.length > 0) {
          setSelectedSymbol(list[0].symbol);
          setPriceQuoteUpdatedAt(new Date());
          // 订阅第一个股票的盘口推送
          subscribeOrderBook(list[0].symbol);
          // 加载第一个股票的Session
          const session = await withTimeoutOr(getOrCreateSession(list[0].symbol, list[0].name), 3500, null as StockSession | null);
          if (session) setCurrentSession(session);
        }
        // 主动获取一次快讯数据（解决启动时后端推送早于前端监听注册的时序问题）
        if (isWailsGoReady()) {
          const telegraphs = await withTimeoutOr(GetTelegraphList(), 2500, [] as Telegraph[]);
          if (telegraphs && telegraphs.length > 0) {
            const latest = telegraphs[0];
            setMarketMessage(`[${latest.time}] ${latest.content}`);
          }
        }
      } catch (err) {
        console.error('Failed to load watchlist:', err);
        setLoadingHint('初始化异常，已进入降级模式');
      } finally {
        setLoading(false);
      }
    };
    loadWatchlist();
  }, [subscribeOrderBook, withTimeoutOr, clampSidePanelsToViewport]);

  // Load K-line data when symbol or period changes
  useEffect(() => {
    if (!selectedSymbol) return;
    const requestId = ++klineRequestIdRef.current;
    // 切换时先切回 full 模式，等待全量数据到达
    setKLineUpdateMode('full');
    // 清空旧数据，避免切换期间出现“新股票 + 旧K线”错配
    setKLineData([]);
    // 订阅K线推送
    subscribeKLine(selectedSymbol, timePeriod);

    const loadKLineData = async () => {
      // 与后端推送统一数据长度，降低周/月K空响应概率
      const dataLen = getKLineRequestLength(timePeriod);
      const maxRetries = 2;
      for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
        if (requestId !== klineRequestIdRef.current) return;
        try {
          const data = await getKLineData(selectedSymbol, timePeriod, dataLen);
          if (requestId !== klineRequestIdRef.current) return;
          if (Array.isArray(data) && data.length > 0) {
            setKLineUpdateMode('full');
            setKLineData(data);
            return;
          }
          console.warn(`[kline] empty data for ${selectedSymbol} ${timePeriod}, attempt=${attempt + 1}`);
        } catch (err) {
          if (requestId !== klineRequestIdRef.current) return;
          console.error(`[kline] load failed for ${selectedSymbol} ${timePeriod}, attempt=${attempt + 1}`, err);
        }
        if (attempt < maxRetries) {
          await new Promise(resolve => setTimeout(resolve, 600));
        }
      }
    };

    void loadKLineData();
  }, [selectedSymbol, timePeriod, subscribeKLine]);

  useEffect(() => {
    setShowF10(false);
    setF10Error('');
    setValuationSnapshot(null);
    setMultiCycleKLines({ daily: [], weekly: [], monthly: [] });
    if (selectedSymbol) {
      void fetchSelectedF10(selectedSymbol, { silent: true });
      void fetchSelectedValuation(selectedSymbol);
      void fetchMultiCycleKLines(selectedSymbol);
    }
  }, [selectedSymbol, fetchSelectedF10, fetchSelectedValuation, fetchMultiCycleKLines]);

  useEffect(() => {
    if (!showF10 || !selectedSymbol) return;
    void fetchSelectedF10(selectedSymbol, { silent: false });
  }, [showF10, selectedSymbol, fetchSelectedF10]);

  // Periodically pull latest K-line to avoid stale data when push is delayed.
  useEffect(() => {
    if (!selectedSymbol) return;
    let cancelled = false;
    let inFlight = false;
    const intervalMs = isMinuteTrendPeriod(timePeriod) ? 45_000 : 180_000;

    const syncLatest = async () => {
      if (inFlight || cancelled) return;
      inFlight = true;
      try {
        const dataLen = getKLineRequestLength(timePeriod);
        const latest = await withTimeoutOr(getKLineData(selectedSymbol, timePeriod, dataLen), 4000, [] as KLineData[]);
        if (!cancelled && Array.isArray(latest) && latest.length > 0) {
          setKLineUpdateMode('refresh');
          setKLineData(latest);
        }
      } finally {
        inFlight = false;
      }
    };

    const bootstrapTimer = setTimeout(syncLatest, 1600);
    const intervalTimer = setInterval(syncLatest, intervalMs);
    return () => {
      cancelled = true;
      clearTimeout(bootstrapTimer);
      clearInterval(intervalTimer);
    };
  }, [selectedSymbol, timePeriod, withTimeoutOr]);

  // Slow-cycle K data for daily/weekly/monthly resonance validation.
  useEffect(() => {
    if (!selectedSymbol) return;
    let cancelled = false;
    let inFlight = false;

    const syncMultiCycle = async () => {
      if (cancelled || inFlight) return;
      inFlight = true;
      try {
        await fetchMultiCycleKLines(selectedSymbol);
      } finally {
        inFlight = false;
      }
    };

    const bootstrapTimer = setTimeout(syncMultiCycle, 1800);
    const intervalTimer = setInterval(syncMultiCycle, 6 * 60_000);
    return () => {
      cancelled = true;
      clearTimeout(bootstrapTimer);
      clearInterval(intervalTimer);
    };
  }, [selectedSymbol, fetchMultiCycleKLines]);

  // 初始化窗口最大化状态
  useEffect(() => {
    void syncWindowMaximizedState();
  }, [syncWindowMaximizedState]);

  // Keep side panels valid when the window is resized.
  useEffect(() => {
    const applyViewportClamp = () => {
      const normalized = clampSidePanelsToViewport(leftPanelWidth, rightPanelWidth);
      if (normalized.left !== leftPanelWidth) {
        setLeftPanelWidth(normalized.left);
      }
      if (normalized.right !== rightPanelWidth) {
        setRightPanelWidth(normalized.right);
      }
    };
    applyViewportClamp();
    window.addEventListener('resize', applyViewportClamp);
    return () => window.removeEventListener('resize', applyViewportClamp);
  }, [leftPanelWidth, rightPanelWidth, clampSidePanelsToViewport]);

  // Keep radar panel height in a usable range.
  useEffect(() => {
    const clampRadarHeight = () => {
      const viewportHeight = typeof window !== 'undefined' ? window.innerHeight : 1200;
      const dynamicMax = Math.max(
        RADAR_PANEL_MIN_HEIGHT,
        Math.min(RADAR_PANEL_MAX_HEIGHT, Math.round(viewportHeight * 0.58)),
      );
      setRadarPanelHeight(prev => clampValue(prev, RADAR_PANEL_MIN_HEIGHT, dynamicMax));
    };
    clampRadarHeight();
    window.addEventListener('resize', clampRadarHeight);
    return () => window.removeEventListener('resize', clampRadarHeight);
  }, []);

  if (loading) {
    return (
      <div className="h-screen w-screen flex items-center justify-center fin-app text-white">
        <div className="text-center">
          <div>加载中...</div>
          {loadingHint && <div className="text-xs mt-2 text-amber-300">{loadingHint}</div>}
        </div>
      </div>
    );
  }

  // 没有自选股时显示欢迎页面
  if (watchlist.length === 0) {
    return <WelcomePage onAddStock={handleAddStock} />;
  }

  if (!selectedStock) return <div className="h-screen w-screen flex items-center justify-center fin-app text-white">请添加自选股</div>;

  return (
    <div className="flex flex-col h-screen text-slate-100 font-sans fin-app">
      {/* Top Navbar */}
      <header
        className="h-14 fin-panel border-b fin-divider flex items-center px-4 justify-between shrink-0 z-20"
        style={{ '--wails-draggable': 'drag' } as React.CSSProperties}
        onDoubleClick={(e) => {
          // 排除 no-drag 区域的双击
          const target = e.target as HTMLElement;
          if (target.closest('[style*="no-drag"]') || target.closest('button') || target.closest('input')) return;
          void toggleWindowMaximize();
        }}
      >
        <div className="flex items-center gap-2" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
          <img src={logo} alt="logo" className="h-8 w-8 rounded-lg" />
          <span className={`font-bold text-lg tracking-tight ${colors.isDark ? 'text-white' : 'text-slate-800'}`}>JOEY <span className="text-accent-2">AI</span></span>
        </div>
        
        <div className="flex items-center gap-1.5 fin-panel-soft px-2.5 py-1.5 rounded-full border fin-divider relative w-[160px] lg:w-[200px] xl:w-[240px] max-w-[18vw] shrink-0" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties} title={marketMessage}>
          <Radio className="h-3 w-3 shrink-0 animate-pulse text-accent-2" />
          <span className={`text-xs flex-1 min-w-0 truncate ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>
            {marketMessage}
          </span>
          <button
            onClick={handleShowTelegraphList}
            className={`p-1 rounded transition-colors ${colors.isDark ? 'hover:bg-slate-700/50 text-slate-400' : 'hover:bg-slate-200/50 text-slate-500'} hover:text-accent-2`}
            title="查看快讯列表"
          >
            <List className="h-4 w-4" />
          </button>

          {/* 快讯下拉列表 */}
          {showTelegraphList && (
            <div
              className="absolute top-full left-0 right-0 mt-2 fin-panel border fin-divider rounded-lg shadow-xl z-50 max-h-96 overflow-y-auto fin-scrollbar text-left"
              onMouseLeave={() => setShowTelegraphList(false)}
            >
              <div className={`p-2 border-b fin-divider text-xs font-medium ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
                财联社快讯
              </div>
              {telegraphLoading ? (
                <div className={`p-4 text-center text-sm ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>加载中...</div>
              ) : telegraphList.length === 0 ? (
                <div className={`p-4 text-center text-sm ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>暂无快讯</div>
              ) : (
                telegraphList.map((tg, idx) => (
                  <div
                    key={idx}
                    onClick={() => handleOpenTelegraph(tg)}
                    className={`p-3 border-b fin-divider last:border-b-0 cursor-pointer transition-colors ${colors.isDark ? 'hover:bg-slate-800/50' : 'hover:bg-slate-100/80'}`}
                  >
                    <div className="flex items-start gap-2">
                      <span className="text-xs text-accent-2 font-mono shrink-0">{tg.time}</span>
                      <span className={`text-xs line-clamp-2 ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{tg.content}</span>
                    </div>
                  </div>
                ))
              )}
            </div>
          )}
        </div>

        <div className="flex items-center gap-3" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
          <button
            onClick={() => setShowJournal(true)}
            className={`flex items-center gap-1.5 px-2.5 py-2 rounded-lg fin-panel border fin-divider transition-colors text-xs font-medium ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-emerald-400/40`}
            title="交易台账（实盘复盘，验证真实胜率）"
          >
            <FileText className="h-3.5 w-3.5" />
            <span>交易台账</span>
          </button>
          <button
            onClick={() => setShowPaper(true)}
            className={`flex items-center gap-1.5 px-2.5 py-2 rounded-lg fin-panel border fin-divider transition-colors text-xs font-medium ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-amber-400/40`}
            title="模拟持仓（纸上交易，验证各筛选系统胜率）"
          >
            <Wallet className="h-3.5 w-3.5" />
            <span>模拟持仓</span>
          </button>
          <div ref={lowBuyStrategyMenuRef} className="relative">
            <button
              onClick={() => setShowLowBuyStrategyMenu(prev => !prev)}
              className={`flex items-center gap-1.5 px-2.5 py-2 rounded-lg fin-panel border fin-divider transition-colors text-xs font-medium ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-fuchsia-400/40`}
              title="低吸策略：选择低吸/尾盘策略入口"
            >
              <Search className="h-3.5 w-3.5" />
              <span>低吸策略</span>
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${showLowBuyStrategyMenu ? 'rotate-180' : ''}`} />
            </button>
            {showLowBuyStrategyMenu && (
              <div className="absolute right-0 top-full mt-2 z-50 w-48 fin-panel border fin-divider rounded-lg shadow-xl p-1.5">
                <button
                  onClick={() => { setShowFundamental(true); setShowLowBuyStrategyMenu(false); }}
                  className="w-full text-left px-3 py-2 rounded-md text-xs font-medium text-indigo-300 hover:bg-indigo-500/15 transition-colors"
                >
                  📊 基本面选股(价值/景气)
                </button>
                <div className="my-1 border-t fin-divider-soft" />
                <button
                  onClick={() => handleOpenLowBuyScanner('低吸选股策略1', 'V1.2 高胜率短线规则（全A · 回踩偏好 · Top3）')}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  低吸选股策略1
                </button>
                <button
                  onClick={handleOpenLowBuyTailStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  低吸尾盘策略2
                </button>
                <button
                  onClick={handleOpenLateDayStrengthStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  尾盘强势策略3
                </button>
                <button
                  onClick={handleOpenLimitPullbackStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  涨停回调低吸4
                </button>
                <button
                  onClick={handleOpenTripleVolumeStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  三倍量策略5
                </button>
                <button
                  onClick={handleOpenTailBuyStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  尾盘买入策略6
                </button>
                <button
                  onClick={handleOpenHotMoneyStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  游资突破策略7
                </button>
                <button
                  onClick={handleOpenDipEntryStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  低吸入场策略8
                </button>
                <button
                  onClick={handleOpenMonsterStrategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  捉妖策略9
                </button>
                <button
                  onClick={handleOpenMonsterV10Strategy}
                  className={`w-full text-left px-3 py-2 rounded-md text-xs transition-colors ${colors.isDark ? 'text-slate-200 hover:bg-slate-700/60' : 'text-slate-700 hover:bg-slate-100'}`}
                >
                  捉妖策略10
                </button>
                {/* 草元标准4A / 草元抓庄4B 已暂停隐藏（用户停用） */}
              </div>
            )}
          </div>
          <button
            onClick={() => setShowWaveScanner(true)}
            className={`flex items-center gap-1.5 px-2.5 py-2 rounded-lg fin-panel border fin-divider transition-colors text-xs font-medium ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-amber-400/40`}
            title="波段策略1.0（吃鱼身/异动点火 + 短线能量 + 五灯共振）"
          >
            <Activity className="h-3.5 w-3.5" />
            <span>波段选股</span>
          </button>
          <button
            onClick={() => setShowLongHuBang(true)}
            className={`p-2 rounded-lg fin-panel border fin-divider transition-colors ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-red-400/40`}
            title="龙虎榜"
          >
            <BarChart3 className="h-4 w-4" />
          </button>
          <button
            onClick={() => setShowHotTrend(true)}
            className={`p-2 rounded-lg fin-panel border fin-divider transition-colors ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-orange-400/40`}
            title="全网热点"
          >
            <TrendingUp className="h-4 w-4" />
          </button>
          <button
            onClick={() => setShowMarketMoves(true)}
            className={`p-2 rounded-lg fin-panel border fin-divider transition-colors ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-cyan-400/40`}
            title="异动中心"
          >
            <Activity className="h-4 w-4" />
          </button>
          <button
            onClick={handlePageRefresh}
            className={`p-2 rounded-lg fin-panel border fin-divider transition-colors ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-emerald-400/40`}
            title="刷新页面"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
          <button
            onClick={() => setShowSettings(true)}
            className={`p-2 rounded-lg fin-panel border fin-divider transition-colors ${colors.isDark ? 'text-slate-300 hover:text-white' : 'text-slate-600 hover:text-slate-900'} hover:border-accent/40`}
          >
            <Settings className="h-4 w-4" />
          </button>
          <div className="text-xs text-right hidden md:block">
            <div className={colors.isDark ? 'text-slate-400' : 'text-slate-500'}>市场状态</div>
            <div className={`font-bold ${
              marketStatus?.status === 'trading' ? 'text-green-500' :
              marketStatus?.status === 'pre_market' ? 'text-yellow-500' :
              marketStatus?.status === 'lunch_break' ? 'text-orange-500' :
              colors.isDark ? 'text-slate-500' : 'text-slate-400'
            }`}>
              {marketStatus?.statusText || '加载中...'}
            </div>
          </div>
          <MarketRegimeBadge />
          {/* 窗口控制按钮 */}
          <div className="flex items-center ml-2 border-l fin-divider pl-3">
            <button
              onClick={handleWindowMinimize}
              className={`p-1.5 rounded transition-colors ${colors.isDark ? 'hover:bg-slate-700/50 text-slate-400 hover:text-white' : 'hover:bg-slate-200/50 text-slate-500 hover:text-slate-900'}`}
              title="最小化"
            >
              <Minus className="h-4 w-4" />
            </button>
            <button
              onClick={() => { void toggleWindowMaximize(); }}
              className={`p-1.5 rounded transition-colors ${colors.isDark ? 'hover:bg-slate-700/50 text-slate-400 hover:text-white' : 'hover:bg-slate-200/50 text-slate-500 hover:text-slate-900'}`}
              title={isMaximized ? "还原" : "最大化"}
            >
              {isMaximized ? <Copy className="h-3.5 w-3.5" /> : <Square className="h-3.5 w-3.5" />}
            </button>
            <button
              onClick={handleWindowClose}
              className={`p-1.5 rounded hover:bg-red-500/80 hover:text-white transition-colors ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}
              title="关闭"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>
      </header>

      {/* Main Content Grid */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left Sidebar: Watchlist */}
        <div style={{ width: leftPanelWidth }} className="shrink-0 fin-panel overflow-hidden">
          <StockList
            stocks={watchlist}
            selectedSymbol={selectedSymbol}
            onSelect={handleSelectStock}
            onAddStock={handleAddStock}
            onRemoveStock={handleRemoveStock}
            marketIndices={marketIndices}
          />
        </div>

        {/* Left Resize Handle */}
        <ResizeHandle direction="horizontal" onResize={handleLeftResize} onResizeEnd={handleResizeEnd} />

        {/* Center Panel: Charts & Data */}
        <div className="flex-1 flex flex-col min-w-0 fin-panel-center">
          {/* Stock Header - A股风格 */}
          <div className="px-6 py-2 shrink-0 border-b fin-divider-soft">
            <div className="grid grid-cols-[auto_1fr_auto] items-start gap-3">
              <div
                className={`relative min-w-[250px] max-w-[520px] h-[108px] rounded-lg border px-3 py-2.5 overflow-visible ${
                  colors.isDark ? 'bg-slate-950/28 border-slate-700/70' : 'bg-slate-50/80 border-slate-200'
                }`}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex items-baseline gap-2">
                    <span className={`truncate font-black leading-none tracking-normal text-[clamp(18px,1.45vw,24px)] ${colors.isDark ? 'text-slate-50' : 'text-slate-900'}`}>
                      {selectedStock.name}
                    </span>
                    <span className={`shrink-0 text-[clamp(12px,1vw,15px)] font-mono ${colors.isDark ? 'text-slate-500' : 'text-slate-500'}`}>
                      {selectedStock.symbol}
                    </span>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <span className={`rounded-md border px-1.5 py-0.5 text-[10px] font-semibold ${
                      colors.isDark ? 'border-slate-600 bg-slate-800/45 text-slate-300' : 'border-slate-300 bg-white/70 text-slate-600'
                    }`}>
                      {stockIdentityState.boardLabel}
                    </span>
                    <span className={`rounded-md border px-1.5 py-0.5 text-[10px] font-semibold ${
                      stockIdentityState.volatilityTone === 'hot'
                        ? 'border-red-500/35 bg-red-500/10 text-red-300'
                        : stockIdentityState.volatilityTone === 'warm'
                          ? 'border-amber-500/40 bg-amber-500/10 text-amber-300'
                          : 'border-slate-500/35 bg-slate-500/10 text-slate-300'
                    }`}>
                      {stockIdentityState.volatilityLabel}
                    </span>
                  </div>
                </div>
                <div className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-[12px] leading-5">
                  <div className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>涨跌</div>
                  <div className="flex min-w-0 items-center gap-3">
                    <span className={`font-mono text-[clamp(14px,1.15vw,18px)] font-bold ${cc.getColorClass(selectedStock.change >= 0)}`}>
                      {selectedStock.change >= 0 ? '+' : ''}{selectedStock.change.toFixed(2)}
                    </span>
                    <span className={`font-mono text-[clamp(14px,1.15vw,18px)] font-bold ${cc.getColorClass(selectedStock.change >= 0)}`}>
                      {selectedStock.change >= 0 ? '+' : ''}{selectedStock.changePercent.toFixed(2)}%
                    </span>
                  </div>
                  <div className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>持仓</div>
                  <button
                    onClick={() => setShowPosition(true)}
                    className={`min-w-0 inline-flex items-center gap-1.5 rounded-md text-left leading-5 transition-colors ${
                      colors.isDark ? 'text-slate-300 hover:text-accent-2' : 'text-slate-600 hover:text-slate-900'
                    }`}
                    title="持仓设置"
                  >
                    <Briefcase className="h-3 w-3 shrink-0" />
                    {positionSummary ? (
                      <>
                        <span className={`shrink-0 font-mono text-[clamp(12px,0.95vw,14px)] font-semibold ${colors.isDark ? 'text-slate-200' : 'text-slate-700'}`}>
                          {positionSummary.shares}股
                        </span>
                        <span className={`font-mono text-[clamp(12px,0.95vw,14px)] font-bold ${positionSummary.isProfit ? cc.upClass : cc.downClass}`}>
                          {positionSummary.isProfit ? '+' : ''}{positionSummary.profitLoss.toFixed(0)}
                        </span>
                        <span className={`font-mono text-[clamp(12px,0.95vw,14px)] font-bold ${positionSummary.isProfit ? cc.upClass : cc.downClass}`}>
                          {positionSummary.isProfit ? '+' : ''}{positionSummary.profitPercent.toFixed(2)}%
                        </span>
                      </>
                    ) : (
                      <span className="text-sm font-semibold">设置持仓</span>
                    )}
	                  </button>
	                </div>
	                <AddToGroupButton
	                  stock={selectedStock}
	                  source="manual"
	                  inWatch={watchlist.some(stock => stock.symbol.toLowerCase() === selectedStock.symbol.toLowerCase())}
		                  onAddToWatchlist={handleAddFromLongHuBang}
		                  variant="tagIcon"
		                  menuAlign="right"
		                  className="absolute right-1.5 bottom-1.5 z-30"
		                />
	              </div>

              <div className="min-w-0 w-full max-w-[860px]">
                <OrderBookComponent
                  data={orderBook}
                  compact
                  levels={5}
                  className="h-[108px]"
                />
              </div>

              <div
                className={`relative w-[206px] h-[108px] rounded-lg border px-3 pt-1.5 pb-5 overflow-hidden ${
                  colors.isDark ? 'bg-[#1b120d]/80 border-amber-900/55' : 'bg-amber-50/80 border-amber-200'
                }`}
              >
                <div className="flex h-full min-w-0 flex-col items-center text-center">
                  <div className="min-w-0 overflow-hidden">
                    <div className={`text-center text-[12px] font-semibold leading-4 ${colors.isDark ? 'text-slate-500' : 'text-slate-500'}`}>现价</div>
                    <div className={`mt-0.5 max-w-full whitespace-nowrap text-center font-mono ${selectedPriceSizeClass} leading-none font-black tracking-normal ${cc.getColorClass(selectedStock.change >= 0)}`}>
                      {selectedPriceText}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center justify-center gap-1">
                      <span className={`rounded-md border px-1.5 py-[1px] text-[11px] font-semibold leading-4 ${
                        selectedStock.change >= 0
                          ? 'border-red-500/35 bg-red-500/10 text-red-300'
                          : 'border-emerald-500/35 bg-emerald-500/10 text-emerald-300'
                      }`}>
                        {pricePanelState.trendLabel}
                      </span>
                      <span className={`rounded-md border px-1.5 py-[1px] text-[11px] font-semibold leading-4 ${
                        pricePanelState.zoneTone === 'high'
                          ? 'border-amber-500/45 bg-amber-500/10 text-amber-300'
                          : pricePanelState.zoneTone === 'low'
                            ? 'border-emerald-500/35 bg-emerald-500/10 text-emerald-300'
                            : 'border-slate-500/35 bg-slate-500/10 text-slate-300'
                      }`}>
                        {pricePanelState.zoneLabel}
                      </span>
                    </div>
                  </div>
                  <div className="pointer-events-none absolute inset-x-0 bottom-1 flex justify-center">
                    <div className={`font-mono text-[12px] font-bold leading-none ${colors.isDark ? 'text-slate-300' : 'text-slate-700'}`}>
                      {liveClockTime}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* A股传统行情数据（单行展示） */}
          <div className="border-b fin-divider-soft shrink-0 text-xs overflow-x-auto fin-scrollbar">
            <div className="grid grid-cols-7 gap-1 px-2 py-1.5 min-w-[860px]">
              <AStockStatItem label="今开" value={selectedStock.open} preClose={selectedStock.preClose} isDark={colors.isDark} />
              <AStockStatItem label="最高" value={selectedStock.high} preClose={selectedStock.preClose} isDark={colors.isDark} />
              <AStockStatItem label="成交量" value={formatVolume(selectedStock.volume)} isPlain isDark={colors.isDark} />
              <AStockStatItem label="昨收" value={selectedStock.preClose} isPlain isDark={colors.isDark} />
              <AStockStatItem label="最低" value={selectedStock.low} preClose={selectedStock.preClose} isDark={colors.isDark} />
              <AStockStatItem label="成交额" value={formatAmount(selectedStock.amount)} isPlain isDark={colors.isDark} />
              <AStockStatItem label="振幅" value={selectedStock.preClose > 0 ? ((selectedStock.high - selectedStock.low) / selectedStock.preClose * 100).toFixed(2) + '%' : '--'} isPlain isDark={colors.isDark} />
            </div>
          </div>

          <div className="px-4 py-1 border-b fin-divider-soft shrink-0">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={handleShowTrend}
                  className={`text-xs px-2.5 py-0.5 rounded border transition-colors ${
                    !showF10
                      ? 'border-accent text-accent-2 bg-accent/10'
                      : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
                  }`}
                >
                  趋势图
                </button>
                <button
                  type="button"
                  onClick={handleShowF10}
                  className={`text-xs px-2.5 py-0.5 rounded border transition-colors ${
                    showF10
                      ? 'border-accent text-accent-2 bg-accent/10'
                      : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
                  }`}
                >
                  F10 全景
                </button>
                <select
                  value={mainChartTemplate}
                  onChange={(event) => handleMainChartTemplateChange(event.target.value as MainChartTemplate)}
                  className={`h-[23px] rounded border px-2 text-xs font-semibold outline-none transition-colors ${
                    colors.isDark
                      ? 'border-slate-700 bg-slate-900/70 text-slate-200 hover:border-accent/60'
                      : 'border-slate-300 bg-white/80 text-slate-700 hover:border-accent/60'
                  }`}
                  style={{ colorScheme: colors.isDark ? 'dark' : 'light' }}
                  title="主图模板；开仓吃鱼会自动切到日K"
                >
                  <option value="standard">主图：标准</option>
                  <option value="openEatFish">主图：开仓吃鱼</option>
                </select>
                {mainChartTemplate === 'openEatFish' && (
                  <span className={`text-[11px] ${colors.isDark ? 'text-cyan-300' : 'text-cyan-700'}`}>
                    日K主图生效
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={handleOpenChartFullscreen}
                className={`inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs transition-colors ${
                  colors.isDark
                    ? 'border-slate-700 bg-slate-900/35 text-slate-300 hover:border-accent/60 hover:text-accent-2 hover:bg-accent/10'
                    : 'border-slate-300 bg-white/60 text-slate-600 hover:border-accent/60 hover:text-accent-2 hover:bg-accent/10'
                }`}
                title="全屏查看趋势图"
              >
                <Maximize2 className="h-3 w-3" />
                全屏
              </button>
            </div>
          </div>

          <div className="flex-1 flex flex-col min-h-0">
            {!showF10 ? (
              <>
                <div className="flex-1 p-1 relative min-h-0">
                  <SafeBoundary title="图表渲染异常" resetKey={`${selectedSymbol}:${timePeriod}`}>
                    <StockChartLW
                      data={safeKLineData}
                      updateMode={kLineUpdateMode}
                      period={timePeriod}
                      onPeriodChange={setTimePeriod}
                      stock={selectedStock}
                      dayKData={multiCycleKLines.daily}
                      mainChartTemplate={mainChartTemplate}
                      onMainChartTemplateChange={handleMainChartTemplateChange}
                    />
                  </SafeBoundary>
                </div>
              </>
            ) : (
              <div className="flex-1 min-h-0 border-t fin-divider-soft overflow-hidden">
                <F10Panel
                  overview={f10Overview}
                  loading={f10Loading}
                  error={f10Error}
                  onRefresh={() => void fetchSelectedF10(selectedStock.symbol)}
                  onCollapse={() => setShowF10(false)}
                />
              </div>
            )}
          </div>
        </div>

        {/* Right Resize Handle */}
        <ResizeHandle direction="horizontal" onResize={handleRightResize} onResizeEnd={handleResizeEnd} />

        {/* Right Panel: AI Agents */}
        <div style={{ width: rightPanelWidth }} className="shrink-0 fin-panel overflow-hidden">
          <AgentRoom
            stock={selectedStock}
            kLineData={safeKLineData}
            session={currentSession}
            onSessionUpdate={setCurrentSession}
            marketStatusCode={marketStatus?.status}
          />
        </div>
      </div>

      {updateAvailable && (
        <div className="fixed top-14 right-4 z-[9999] fin-panel border border-[var(--accent)]/50 rounded-lg shadow-lg px-4 py-3 flex items-center gap-3 animate-slide-in">
          <RefreshCw className="h-4 w-4 text-[var(--accent)]" />
          <span className="text-sm">发现新版本 v{updateAvailable}</span>
          <button
            onClick={() => {
              setSettingsInitialTab('update');
              setShowSettings(true);
              setUpdateAvailable('');
            }}
            className="px-3 py-1 rounded-md text-sm text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-2)]"
          >
            去更新
          </button>
          <button
            onClick={() => setUpdateAvailable('')}
            className="p-1 rounded hover:bg-white/10"
            title="忽略"
          >
            <X className="h-4 w-4 opacity-60" />
          </button>
        </div>
      )}

      <SettingsDialog
        isOpen={showSettings}
        onClose={() => {
          setShowSettings(false);
          setSettingsInitialTab(undefined);
        }}
        initialTab={settingsInitialTab}
      />
      <PositionDialog
        isOpen={showPosition}
        onClose={() => setShowPosition(false)}
        stockCode={selectedStock.symbol}
        stockName={selectedStock.name}
        currentPrice={selectedStock.price}
        position={currentSession?.position}
	        onSave={async (shares, costPrice, buyDate) => {
	          await updateStockPosition(selectedStock.symbol, shares, costPrice, buyDate);
	          await handleTradeJournalChanged();
	        }}
	        onSell={async (sellPrice) => {
	          const today = new Date().toISOString().slice(0, 10);
	          await sellStockPosition(selectedStock.symbol, sellPrice, today);
	          await handleTradeJournalChanged();
	        }}
      />
      <HotTrendDialog isOpen={showHotTrend} onClose={() => setShowHotTrend(false)} />
      <LongHuBangDialog
        isOpen={showLongHuBang}
        onClose={() => setShowLongHuBang(false)}
        watchlistSymbols={watchlist.map(stock => stock.symbol)}
        onAddToWatchlist={handleAddFromLongHuBang}
      />
      <MarketMovesDialog isOpen={showMarketMoves} onClose={() => setShowMarketMoves(false)} marketStatusCode={marketStatus?.status} />
      <LowBuyScannerDialog
        isOpen={showLowBuyScanner}
        strategyMode={lowBuyStrategyMode}
        title={lowBuyStrategyTitle}
        subtitle={lowBuyStrategySubtitle}
        loading={scannerLoading}
        result={scannerResult}
        error={scannerError}
        onClose={() => setShowLowBuyScanner(false)}
        onScan={handleRunLowBuyScanner}
        onReplay={handleRunTailLazyReplay}
        onCollectHistory={handleCollectDailyHistory}
        onGetHistoryAutoCollectStatus={handleGetHistoryAutoCollectStatus}
        onUpdateHistoryAutoCollect={handleUpdateHistoryAutoCollect}
        onAddToWatchlist={handleAddFromLongHuBang}
        onOpenStock={handleOpenStockFromStrategy}
        watchlistSymbols={watchlist.map(stock => stock.symbol)}
        onOpenLateDayChase={() => setShowLateDayChaseScanner(true)}
      />
      <LateDayChaseScannerDialog
        isOpen={showLateDayChaseScanner}
        onClose={() => setShowLateDayChaseScanner(false)}
        onAddToWatchlist={handleAddFromLongHuBang}
        onOpenStock={handleOpenStockFromStrategy}
        watchlistSymbols={watchlist.map(stock => stock.symbol)}
      />
	      <TradeJournalDialog
	        isOpen={showJournal}
	        onClose={() => setShowJournal(false)}
	        onChanged={handleTradeJournalChanged}
	      />
      <PaperPortfolioDialog
        isOpen={showPaper}
        onClose={() => setShowPaper(false)}
        onOpenStock={handleOpenStockFromStrategy}
      />
      <FundamentalScanDialog isOpen={showFundamental} onClose={() => setShowFundamental(false)} />
      <WaveScannerDialog
        isOpen={showWaveScanner}
        onClose={() => setShowWaveScanner(false)}
        onAddToWatchlist={handleAddFromLongHuBang}
        onOpenStock={handleOpenStockFromStrategy}
        watchlistSymbols={watchlist.map(stock => stock.symbol)}
      />
      {showChartFullscreen && selectedStock && (
        <div
          className="fixed inset-0 z-[70] flex flex-col bg-[#070b12]"
          style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
        >
          <div className="shrink-0 border-b border-slate-800/90 bg-slate-950/96 px-3 py-2 shadow-lg">
            <div className="flex items-center justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2">
                <Maximize2 className="h-4 w-4 shrink-0 text-accent-2" />
                <span className="truncate text-sm font-semibold text-slate-100">
                  {selectedStock.name} {selectedStock.symbol}
                </span>
                <span className="shrink-0 rounded border border-accent/30 bg-accent/10 px-2 py-0.5 text-[11px] font-medium text-accent-2">
                  {chartFullscreenMode === 'strategy' ? '全屏四图K线' : '全屏趋势图'}
                </span>
                {chartFullscreenMode === 'strategy' && (
                  <span className="hidden sm:inline shrink-0 rounded border border-cyan-400/20 bg-cyan-400/10 px-2 py-0.5 text-[11px] font-medium text-cyan-200">
                    日K · 近6个月 · 主图开仓吃鱼
                  </span>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <button
                  type="button"
                  onClick={() => setShowResearchReport(true)}
                  className="inline-flex h-8 items-center gap-1.5 rounded border border-cyan-400/40 bg-cyan-400/10 px-3 text-xs font-semibold text-cyan-100 transition-colors hover:bg-cyan-400/20"
                  title="机构级深度诊断报告V5.0(16章完整框架,生成约15-25分钟,可下载Word)"
                >
                  <FileText className="h-3.5 w-3.5" />
                  <span>投研报告</span>
                </button>
                <button
                  type="button"
                  onClick={() => {
                    if (chartFullscreenMode !== 'strategy') {
                      setChartFullscreenMode('strategy');
                      setMainChartTemplate('openEatFish');
                      setTimePeriod('1d');
                    }
                    setShowBoardReport(true);
                  }}
                  className="inline-flex h-8 items-center gap-1.5 rounded border border-amber-400/40 bg-amber-400/10 px-3 text-xs font-semibold text-amber-100 transition-colors hover:bg-amber-400/20"
                  title={chartFullscreenMode === 'strategy' ? '根据四图生成看板报告' : '切到四图K线并生成看板报告'}
                >
                  <FileText className="h-3.5 w-3.5" />
                  <span>看板报告</span>
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setShowBoardReport(false);
                    setShowChartFullscreen(false);
                  }}
                  className="inline-flex h-8 w-8 items-center justify-center rounded border border-slate-700 text-slate-400 transition-colors hover:border-red-400/50 hover:bg-red-500/10 hover:text-red-200"
                  title="关闭全屏"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
          <div className="min-h-0 flex-1 p-2">
            <SafeBoundary title="全屏图表渲染异常" resetKey={`fullscreen:${chartFullscreenMode}:${selectedSymbol}:${timePeriod}`}>
              <StockChartLW
                data={safeKLineData}
                updateMode={kLineUpdateMode}
                period={timePeriod}
                onPeriodChange={setTimePeriod}
                stock={selectedStock}
                dayKData={multiCycleKLines.daily}
                mainChartTemplate={mainChartTemplate}
                onMainChartTemplateChange={handleMainChartTemplateChange}
                initialGridMode={chartFullscreenMode === 'strategy' ? true : undefined}
                initialSubChartType={chartFullscreenMode === 'strategy' ? 'vipAnomaly' : undefined}
                initialSubType2={chartFullscreenMode === 'strategy' ? 'vipShortEnergy' : undefined}
                initialSubType3={chartFullscreenMode === 'strategy' ? 'vipFiveDragon' : undefined}
                visibleRangeBars={chartFullscreenMode === 'strategy' ? 126 : undefined}
              />
            </SafeBoundary>
          </div>
          <BoardReportDialog
            isOpen={showBoardReport}
            onClose={() => setShowBoardReport(false)}
            stock={selectedStock}
            data={safeKLineData}
            period={timePeriod}
            dayKLineData={multiCycleKLines.daily}
            weekKLineData={multiCycleKLines.weekly}
            monthKLineData={multiCycleKLines.monthly}
            marketIndices={marketIndices}
            f10Overview={f10Overview}
            valuationSnapshot={valuationSnapshot}
            marketMessage={marketMessage}
            marketStatusText={marketStatus?.statusText}
            marketStatusCode={marketStatus?.status}
            onForceSync={forceSyncSelectedStock}
            syncing={syncingRadar}
          />
          <ResearchReportDialog
            isOpen={showResearchReport}
            onClose={() => setShowResearchReport(false)}
            stock={selectedStock}
          />
        </div>
      )}
      {showWaveModel && selectedStock && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setShowWaveModel(false)} />
          <div className={`relative w-[1120px] max-w-[94vw] max-h-[88vh] overflow-auto rounded-xl border fin-divider shadow-2xl ${colors.isDark ? 'bg-[#0f1722]' : 'bg-white'}`}>
            <div className="flex items-center justify-between px-4 py-3 border-b fin-divider sticky top-0 z-10 fin-panel-strong">
              <div className="flex items-center gap-2">
                <Gauge className="h-4 w-4 text-amber-400" />
                <span className={`font-semibold ${colors.isDark ? 'text-white' : 'text-slate-800'}`}>波段模型驾驶舱</span>
                <span className={`text-sm font-mono ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>{selectedStock.name} {selectedStock.symbol}</span>
              </div>
              <button
                onClick={() => setShowWaveModel(false)}
                className={`p-1 rounded transition-colors ${colors.isDark ? 'hover:bg-slate-700 text-slate-400 hover:text-white' : 'hover:bg-slate-200 text-slate-500 hover:text-slate-700'}`}
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="p-3">
              <SafeBoundary title="模型驾驶舱渲染异常" resetKey={`${selectedSymbol}:${timePeriod}`}>
                <ModelRadarStrip
                  stock={selectedStock}
                  kLineData={safeKLineData}
                  period={timePeriod}
                  panelHeight={Math.max(radarPanelHeight, 480)}
                  dayKLineData={multiCycleKLines.daily}
                  weekKLineData={multiCycleKLines.weekly}
                  monthKLineData={multiCycleKLines.monthly}
                  marketIndices={marketIndices}
                  f10Overview={f10Overview}
                  valuationSnapshot={valuationSnapshot}
                  marketMessage={marketMessage}
                  marketStatusText={marketStatus?.statusText}
                  marketStatusCode={marketStatus?.status}
                  onForceSync={forceSyncSelectedStock}
                  syncing={syncingRadar}
                />
              </SafeBoundary>
              <ResizeHandle
                direction="vertical"
                onResize={handleRadarResize}
                className="h-3"
                showLine
                lineClassName={colors.isDark ? 'bg-accent/45 group-hover:bg-accent/80' : 'bg-accent/40 group-hover:bg-accent/70'}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

// A股行情数据项组件
interface AStockStatItemProps {
  label: string;
  value: number | string;
  preClose?: number;
  isPlain?: boolean;
  isDark?: boolean;
}

const AStockStatItem: React.FC<AStockStatItemProps> = ({ label, value, preClose, isPlain, isDark = true }) => {
  const cc = useCandleColor();
  let colorClass = isDark ? 'text-slate-100' : 'text-slate-700';
  let displayValue = typeof value === 'string' ? value : value.toFixed(2);

  if (!isPlain && typeof value === 'number' && preClose) {
    if (value > preClose) colorClass = cc.upClass;
    else if (value < preClose) colorClass = cc.downClass;
  }

  return (
    <div className="flex items-center gap-2 px-1.5 py-1 whitespace-nowrap">
      <span className={`shrink-0 ${isDark ? 'text-slate-500' : 'text-slate-400'}`}>{label}</span>
      <span className={`font-mono ${colorClass}`}>{displayValue}</span>
    </div>
  );
};

// 格式化成交量
const formatVolume = (vol: number): string => {
  if (vol >= 100000000) return (vol / 100000000).toFixed(2) + '亿';
  if (vol >= 10000) return (vol / 10000).toFixed(2) + '万';
  return vol.toString();
};

// 格式化成交额
const formatAmount = (amount: number): string => {
  if (amount >= 100000000) return (amount / 100000000).toFixed(2) + '亿';
  if (amount >= 10000) return (amount / 10000).toFixed(2) + '万';
  return amount.toFixed(2);
};

export default App;
