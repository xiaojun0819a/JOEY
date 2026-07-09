// 历史日K展示范围(本机偏好,localStorage;访客也可用,不占共享配置)
const KEY = 'jcp_kline_history_years';
export const KLINE_RANGE_CHANGED_EVENT = 'jcp:kline-range-changed';

export const KLINE_RANGE_OPTIONS: { years: number; label: string }[] = [
  { years: 1, label: '近 1 年' },
  { years: 3, label: '近 3 年(默认)' },
  { years: 5, label: '近 5 年' },
  { years: 10, label: '近 10 年' },
  { years: 0, label: '全部(上市以来)' },
];

export function getKlineHistoryYears(): number {
  const v = parseInt(localStorage.getItem(KEY) ?? '3', 10);
  return Number.isFinite(v) && [0, 1, 3, 5, 10].includes(v) ? v : 3;
}

export function setKlineHistoryYears(years: number) {
  localStorage.setItem(KEY, String(years));
  window.dispatchEvent(new CustomEvent(KLINE_RANGE_CHANGED_EVENT));
}

// 年数→交易日根数(每年约250交易日;0=全部,给9000覆盖1991年至今)
export function klineHistoryDays(): number {
  const y = getKlineHistoryYears();
  return y <= 0 ? 9000 : Math.round(y * 250);
}
