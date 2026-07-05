export const STRATEGY_SOURCE_LABELS: Record<string, string> = {
  'lowbuy-v1': '低吸1',
  'limit-pullback-v1': '涨停回调',
  'triple-volume-v5': '三倍量',
  'tail-buy-v6': '尾盘买入',
  'hot-money-v7': '游资突破',
  'dip-entry-v8': '低吸入场',
  'monster-v9': '捉妖',
  'monster-v10': '捉妖10',
  'taillazy-v2': '低吸2',
  'latechase-v3': '尾盘3',
  'caoyuan-standard4a': '草元标准',
  'caoyuan-standard4a-strict': '草元标准',
  'caoyuan-zhuang4b': '草元抓庄',
  'caoyuan-zhuang4b-strict': '草元抓庄',
  'wave-v1': '波段',
  'fund-value': '基本面价值',
  'fund-boom': '基本面景气',
  lowbuy: '低吸旧口径',
  taillazy: '低吸尾盘旧口径',
  latechase: '尾盘强势旧口径',
  wave: '波段旧口径',
  manual: '手动',
};

export const STRATEGY_SOURCE_FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'lowbuy-v1', label: STRATEGY_SOURCE_LABELS['lowbuy-v1'] },
  { key: 'limit-pullback-v1', label: STRATEGY_SOURCE_LABELS['limit-pullback-v1'] },
  { key: 'triple-volume-v5', label: STRATEGY_SOURCE_LABELS['triple-volume-v5'] },
  { key: 'tail-buy-v6', label: STRATEGY_SOURCE_LABELS['tail-buy-v6'] },
  { key: 'hot-money-v7', label: STRATEGY_SOURCE_LABELS['hot-money-v7'] },
  { key: 'dip-entry-v8', label: STRATEGY_SOURCE_LABELS['dip-entry-v8'] },
  { key: 'monster-v9', label: STRATEGY_SOURCE_LABELS['monster-v9'] },
  { key: 'monster-v10', label: STRATEGY_SOURCE_LABELS['monster-v10'] },
  { key: 'taillazy-v2', label: STRATEGY_SOURCE_LABELS['taillazy-v2'] },
  { key: 'latechase-v3', label: STRATEGY_SOURCE_LABELS['latechase-v3'] },
  // 草元标准4A / 草元抓庄4B 已暂停隐藏（用户停用），不在来源筛选标签中显示
  { key: 'fund-value', label: STRATEGY_SOURCE_LABELS['fund-value'] },
  { key: 'fund-boom', label: STRATEGY_SOURCE_LABELS['fund-boom'] },
  { key: 'wave-v1', label: STRATEGY_SOURCE_LABELS['wave-v1'] },
  { key: 'manual', label: STRATEGY_SOURCE_LABELS.manual },
] as const;

export const getStrategySourceLabel = (source?: string): string => {
  const key = (source || 'manual').trim() || 'manual';
  return STRATEGY_SOURCE_LABELS[key] || key;
};

export const shouldAutoTagStrategySource = (source?: string): boolean => {
  const key = (source || '').trim();
  return !!key && key !== 'manual';
};
