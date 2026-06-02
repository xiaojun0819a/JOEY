import { useState, useEffect, useCallback, useRef } from 'react';
import { GetTradingSchedule } from '@wailsjs/go/main/App';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

// 交易时段
interface TradingPeriod {
  status: string;
  text: string;
  startTime: string;
  endTime: string;
}

// 交易时间表
interface TradingSchedule {
  isTradeDay: boolean;
  holidayName: string;
  periods: TradingPeriod[];
}

const FALLBACK_TRADING_SCHEDULE: TradingSchedule = {
  isTradeDay: true,
  holidayName: '',
  periods: [
    { status: 'pre_market', text: '盘前', startTime: '09:15', endTime: '09:30' },
    { status: 'trading', text: '交易中', startTime: '09:30', endTime: '11:30' },
    { status: 'lunch_break', text: '午休', startTime: '11:30', endTime: '13:00' },
    { status: 'trading', text: '交易中', startTime: '13:00', endTime: '15:00' },
  ],
};

// 市场状态
export interface MarketStatus {
  status: string;
  statusText: string;
  isTradeDay: boolean;
  holidayName: string;
}

// 解析时间字符串为分钟数
function parseTimeToMinutes(time: string): number {
  const [hours, minutes] = time.split(':').map(Number);
  return hours * 60 + minutes;
}

// 根据当前时间和时间表计算市场状态
function calculateStatus(schedule: TradingSchedule): MarketStatus {
  if (!schedule.isTradeDay) {
    let statusText = '休市';
    if (schedule.holidayName) {
      statusText = `${schedule.holidayName}休市`;
    }
    return {
      status: 'closed',
      statusText,
      isTradeDay: false,
      holidayName: schedule.holidayName,
    };
  }

  const now = new Date();
  const currentMinutes = now.getHours() * 60 + now.getMinutes();

  for (const period of schedule.periods) {
    const start = parseTimeToMinutes(period.startTime);
    const end = parseTimeToMinutes(period.endTime);

    if (currentMinutes >= start && currentMinutes < end) {
      return {
        status: period.status,
        statusText: period.text,
        isTradeDay: true,
        holidayName: '',
      };
    }
  }

  return {
    status: 'closed',
    statusText: '已收盘',
    isTradeDay: true,
    holidayName: '',
  };
}

/**
 * 市场状态 Hook
 * 应用启动时循环获取交易时间表（直到成功），然后纯前端每秒判断当前状态
 */
export function useMarketStatus() {
  const [schedule, setSchedule] = useState<TradingSchedule | null>(null);
  const [status, setStatus] = useState<MarketStatus | null>(null);

  // 循环获取交易时间表，直到成功
  const fetchScheduleWithRetry = useCallback(async () => {
    if (!isWailsGoReady()) {
      warnWailsUnavailable('市场状态', 'go');
      setSchedule(FALLBACK_TRADING_SCHEDULE);
      return;
    }
    while (true) {
      try {
        const data = await GetTradingSchedule();
        if (data && data.periods && data.periods.length > 0) {
          setSchedule(data);
          return;
        }
      } catch (err) {
        console.error('获取交易时间表失败，500ms后重试:', err);
      }
      // 延迟500ms后重试
      await new Promise(resolve => setTimeout(resolve, 500));
    }
  }, []);

  // 启动时循环获取时间表
  useEffect(() => {
    fetchScheduleWithRetry();
  }, [fetchScheduleWithRetry]);

  // 用 ref 记录上次状态，避免值不变时触发无效渲染
  const lastStatusKeyRef = useRef<string>('');

  // 定时更新状态（值不变则跳过 setState）
  useEffect(() => {
    if (!schedule) return;

    const update = () => {
      const newStatus = calculateStatus(schedule);
      const key = `${newStatus.status}:${newStatus.statusText}`;
      if (key !== lastStatusKeyRef.current) {
        lastStatusKeyRef.current = key;
        setStatus(newStatus);
      }
    };

    // 立即计算一次
    update();

    // 交易时段 1 秒刷新，非交易时段 60 秒刷新
    const getInterval = () => {
      const s = lastStatusKeyRef.current.split(':')[0];
      return (s === 'trading' || s === 'pre_market') ? 1000 : 60000;
    };

    let timer: ReturnType<typeof setTimeout>;
    const tick = () => {
      update();
      timer = setTimeout(tick, getInterval());
    };
    timer = setTimeout(tick, getInterval());

    return () => clearTimeout(timer);
  }, [schedule]);

  // 每天0点刷新时间表（处理跨天）
  useEffect(() => {
    const now = new Date();
    const tomorrow = new Date(now);
    tomorrow.setDate(tomorrow.getDate() + 1);
    tomorrow.setHours(0, 0, 0, 0);

    const msUntilMidnight = tomorrow.getTime() - now.getTime();

    const timer = setTimeout(() => {
      fetchScheduleWithRetry();
    }, msUntilMidnight);

    return () => clearTimeout(timer);
  }, [fetchScheduleWithRetry]);

  return { status, schedule, refresh: fetchScheduleWithRetry };
}
