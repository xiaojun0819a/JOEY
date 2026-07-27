// 标签与顶部策略菜单同名(2026-07-21 用户定:模拟持仓/策略账户所见即菜单里的策略)。
// 2026-07-27 去掉数字编号:停用三个策略后编号出现 1、10 的缺口,而重编会让每个号指向另一个策略
// (捉妖9→捉妖8 之类),旧截图/手册/学习日报里的编号全部错位。用名字不会撞号,新增策略也不用再纠结编号。
export const STRATEGY_SOURCE_LABELS: Record<string, string> = {
  'lowbuy-v1': '低吸选股',
  'limit-pullback-v1': '涨停回调低吸',
  'triple-volume-v5': '三倍量',
  'tail-buy-v6': '尾盘买入',
  'hot-money-v7': '游资突破',
  'dip-entry-v8': '低吸入场',
  'monster-v9': '捉妖',
  'monster-v10': '捉妖v10·停',
  'oversold-ignite-v1': '超跌起爆',
  'limitup-retrace-v12': '涨停回踩·停',
  'taillazy-v2': '低吸尾盘',
  'latechase-v3': '尾盘强势',
  'caoyuan-standard4a': '草元标准',
  'caoyuan-standard4a-strict': '草元标准',
  'caoyuan-zhuang4b': '草元抓庄',
  'caoyuan-zhuang4b-strict': '草元抓庄',
  'wave-v1': '波段',
  'fund-value': '基本面价值',
  'fund-boom': '基本面景气',
  // 外部荐股跟单验证(2026-07-27):**不是自研策略**,是订阅的荐股服务每天早盘推的票,
  // 按固定金额跟单进模拟盘,攒不被卖方挑选的真实前向样本。风控引擎对它一律不接管
  // (要测的是用户自己"冲高就卖"的手,引擎替他卖了就测不出来了)。详见 app_tip_picks.go
  'tip-tianding': '天鼎早盘推荐',
  lowbuy: '低吸旧口径',
  taillazy: '低吸尾盘旧口径',
  latechase: '尾盘强势旧口径',
  wave: '波段旧口径',
  manual: '手动',
};

// 已停用/已证伪的策略(2026-07-27):不再进 14:50 自动扫描、菜单里也摘掉了,
// 但**历史持仓与留痕还在**,所以筛选标签保留(否则那些仓位只能在「全部」里大海捞针),
// 只是排到最后并置灰,且当持仓数为 0 时由 UI 自动隐藏。
export const RETIRED_STRATEGY_KEYS = new Set<string>([
  'lowbuy-v1',          // 扣费超额 −0.52%/笔 n=273
  'monster-v10',        // 扣费超额 −0.61%/笔 n=239
  'limitup-retrace-v12',// 两年回测证伪
]);

// 顺序:在用的策略在前,已停用的排到最后(2026-07-27)
export const STRATEGY_SOURCE_FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'taillazy-v2', label: STRATEGY_SOURCE_LABELS['taillazy-v2'] },
  { key: 'latechase-v3', label: STRATEGY_SOURCE_LABELS['latechase-v3'] },
  { key: 'limit-pullback-v1', label: STRATEGY_SOURCE_LABELS['limit-pullback-v1'] },
  { key: 'triple-volume-v5', label: STRATEGY_SOURCE_LABELS['triple-volume-v5'] },
  { key: 'tail-buy-v6', label: STRATEGY_SOURCE_LABELS['tail-buy-v6'] },
  { key: 'hot-money-v7', label: STRATEGY_SOURCE_LABELS['hot-money-v7'] },
  { key: 'dip-entry-v8', label: STRATEGY_SOURCE_LABELS['dip-entry-v8'] },
  { key: 'monster-v9', label: STRATEGY_SOURCE_LABELS['monster-v9'] },
  { key: 'oversold-ignite-v1', label: STRATEGY_SOURCE_LABELS['oversold-ignite-v1'] },
  { key: 'wave-v1', label: STRATEGY_SOURCE_LABELS['wave-v1'] },
  // 外部荐股独立成一档:它和自研策略不是一回事,计分卡必须分得开,
  // 否则别人推的票的成绩会混进自己策略的胜率里
  { key: 'tip-tianding', label: STRATEGY_SOURCE_LABELS['tip-tianding'] },
  { key: 'manual', label: STRATEGY_SOURCE_LABELS.manual },
  // 已停用/已证伪的策略(低吸选股 / 捉妖v10 / 涨停回踩)**不再出现在来源筛选与策略账户标签里**
  // (2026-07-27 用户定,与草元系同款处理)。它们的历史持仓不会消失,仍在「全部」里可见,
  // 未平仓的那几笔也照常由风控引擎按各自纪律托管,只是不再单独占一个标签。
] as const;

export const getStrategySourceLabel = (source?: string): string => {
  const key = (source || 'manual').trim() || 'manual';
  return STRATEGY_SOURCE_LABELS[key] || key;
};

export const shouldAutoTagStrategySource = (source?: string): boolean => {
  const key = (source || '').trim();
  return !!key && key !== 'manual';
};

// 来源别名匹配:一笔持仓的原始 source 是否属于某个策展策略键(兼容历史别名)。
// 模拟持仓筛选 与 策略账户标签 共用,保证两边"有哪些策略"口径一致。
export const sourceMatchesStrategyKey = (source: string | undefined, key: string): boolean => {
  const src = (source || 'manual').trim() || 'manual';
  if (key === 'all') return true;
  const aliases: Record<string, string[]> = {
    'lowbuy-v1': ['lowbuy'],
    'triple-volume-v5': ['triple-volume'],
    'tail-buy-v6': ['tail-buy'],
    'hot-money-v7': ['hot-money'],
    'dip-entry-v8': ['dip-entry'],
    'monster-v9': ['monster', '捉妖策略6'],
    'monster-v10': ['捉妖策略10'],
    'taillazy-v2': ['taillazy'],
    'latechase-v3': ['latechase'],
    'wave-v1': ['wave'],
    'caoyuan-standard4a': ['caoyuan-standard4a-strict'],
    'caoyuan-zhuang4b': ['caoyuan-zhuang4b-strict'],
  };
  return src === key || (aliases[key] || []).includes(src);
};
