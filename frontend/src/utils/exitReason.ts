// 平仓原因 → 中文标签(单一真相源)。
//
// 2026-07-27 抽出来的。之前同一张映射表在三个地方各抄了一份
// (paperService / accountService / LowBuyScannerDialog),抄出两类问题:
//
// 1) **写死了错的数字**。原来写「止损-5%」「止盈+15%」「5日<3%」,那是低吸系一档的参数。
//    实际各策略风控档位差很远(硬止损 -4.5%~-15%、止盈 6%~40%、时间止损 1~10 日),
//    对不上档的策略等于被报了错数——看复盘时会以为纪律没执行到位。
//    所以标签只说「触发了哪一条」,不编具体数值;要看数值去各策略的「规则说明」。
// 2) 三份各自漂移(ma10 一处叫「破10线」一处叫「破10日线」),改一处漏两处。
//
// ⚠️空字符串的含义**因语境而异**,所以本模块只给映射表,不给统一的空值默认:
//   - 模拟持仓:空 = 手动平仓(引擎平仓必带理由,见 paperService 里的推导)
//   - 策略账户:空 = 无(手动平在库里就是字面量 "manual")
//   - 复盘/回放:空 = 还没离场
// 各调用方自己决定空值怎么显示。

export const EXIT_REASON_LABEL: Record<string, string> = {
  stop_loss: '止损线',
  ma5: '破5日线',
  ma10: '破10日线',
  ma20: '破20日线',
  signal_low: '破信号日低点',
  prev_low: '破昨日阳线低点',
  struct_stop: '破确认前低',
  turnover: '换手超限',
  time_stop: '时间止损',
  take_profit: '止盈',
  breakeven: '保本离场',
  trail: '移动止损',
  window_end: '到期',
  manual: '手动平仓',
};

// exitReasonLabel 查表并处理 half_ 前缀(减半止盈后剩余仓位的离场)。
// overrides 供调用方覆盖语境相关的词条(如复盘里 window_end 该叫「未到期」)。
export const exitReasonLabel = (
  reason: string,
  overrides?: Record<string, string>,
): string => {
  const half = reason.startsWith('half_');
  const key = half ? reason.slice(5) : reason;
  const base = overrides?.[key] || EXIT_REASON_LABEL[key] || key;
  return half ? `减半·${base}` : base;
};
