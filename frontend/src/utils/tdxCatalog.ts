// 通达信公式目录:构建期把 assets/tdx/*.txt 全量内嵌,按文件名分类主图/副图,
// 并按逻辑内核归入 8 大类;重复贴牌的公式只保留一份。
export type TdxCategory =
  | '均线趋势交叉'
  | 'KDJ·RSI摆动'
  | '主力资金推断'
  | '涨停连板龙头'
  | 'K线形态反转'
  | '通道箱体撑压'
  | '筹码与画图'
  | '分时日内'
  | '通用面板';

export const TDX_CATEGORIES: TdxCategory[] = [
  '通用面板',
  '均线趋势交叉',
  'KDJ·RSI摆动',
  '主力资金推断',
  '涨停连板龙头',
  'K线形态反转',
  '通道箱体撑压',
  '筹码与画图',
  '分时日内',
];

export interface TdxFormula {
  id: string;        // 'tdx:<文件名>'
  name: string;      // 清理后的显示名
  kind: 'main' | 'sub';
  category: TdxCategory;
  source: string;
}

const raw = import.meta.glob('../assets/tdx/*.txt', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;

// 重复贴牌(逻辑同源)只保留一份,右注为保留的版本
const DUPLICATE_DROP = new Set([
  '九尾跟庄主图指标公式源码',     // = 九尾狐跟庄
  '短线擒妖主图指标公式源码',     // = 九尾狐跟庄
  '金蝶探底主图指标公式源码',     // = 金蛇寻底
  '游资资金主图指标公式源码',     // = 主力资金入场
  '寻龙成妖主图指标公式源码',     // = 寻妖成龙
  '龙行主图指标公式源码',         // = 战赢趋势
  '分时捕手主图指标公式源码',     // = AI分时
  '吸筹雷达分时主图指标公式源码', // = AI分时
]);

// 按文件名前缀归类(前缀取到足以区分同前缀公式为止)
const CATEGORY_BY_PREFIX: [string, TdxCategory][] = [
  // 〇、诚实版通用面板(只标注可验证事实,不画买卖点)
  ['通用面板', '通用面板'],
  // 一、纯均线/趋势线交叉
  ['接力涨', '均线趋势交叉'],
  ['照抄主力', '均线趋势交叉'],
  ['点火题材共振', '均线趋势交叉'],
  ['三线合一来钱', '均线趋势交叉'],
  ['主力资金流向', '均线趋势交叉'],
  ['风起', '均线趋势交叉'],
  ['神奇战法', '均线趋势交叉'],
  ['出手必杀', '均线趋势交叉'],
  ['聚力突破', '均线趋势交叉'],
  ['关键起爆点', '均线趋势交叉'],
  ['平空开多', '均线趋势交叉'],
  ['涨停跳空拉升', '均线趋势交叉'],
  ['跋山涉水', '均线趋势交叉'],
  ['主力跟踪', '均线趋势交叉'],
  ['波段擒牛', '均线趋势交叉'],
  ['操盘密码机构专用', '均线趋势交叉'],
  ['唐能通精准买卖', '均线趋势交叉'],
  // 二、KDJ/RSI/威廉换皮
  ['九尾狐跟庄', 'KDJ·RSI摆动'],
  ['多重逃顶', 'KDJ·RSI摆动'],
  ['金蛇寻底', 'KDJ·RSI摆动'],
  ['趋势拉涨爆发', 'KDJ·RSI摆动'],
  ['经典建仓点', 'KDJ·RSI摆动'],
  ['强势突破分时', 'KDJ·RSI摆动'],
  ['波段加速', 'KDJ·RSI摆动'],
  ['量价拐点', 'KDJ·RSI摆动'],
  ['左膀右臂', 'KDJ·RSI摆动'],
  ['介入时机', 'KDJ·RSI摆动'],
  ['一个绝顶高手', 'KDJ·RSI摆动'],
  ['抓住黑马', 'KDJ·RSI摆动'],
  ['资金起量', 'KDJ·RSI摆动'],
  ['多彩共振', 'KDJ·RSI摆动'],
  ['超级组合顶底', 'KDJ·RSI摆动'],
  ['动感底部', 'KDJ·RSI摆动'],
  ['三阶动量背离', 'KDJ·RSI摆动'],
  // 三、"主力/机构/游资资金"(量价代理估算)
  ['主力资金入场', '主力资金推断'],
  ['游资主力进场加仓信号', '主力资金推断'],
  ['主力盘口异动', '主力资金推断'],
  ['主力控盘分析', '主力资金推断'],
  ['游资龙头', '主力资金推断'],
  ['神秘机构组合', '主力资金推断'],
  ['抓牛分时', '主力资金推断'],
  ['单笔资金分时', '主力资金推断'],
  ['监控资金波动', '主力资金推断'],
  ['狙击启动', '主力资金推断'],
  ['爆量上穿出妖', '主力资金推断'],
  ['横盘突破起爆', '主力资金推断'],
  ['放量起飞', '主力资金推断'],
  // 四、涨停/连板/妖股/龙头
  ['连板王', '涨停连板龙头'],
  ['擒龙决策', '涨停连板龙头'],
  ['寻妖成龙', '涨停连板龙头'],
  ['回踩高升', '涨停连板龙头'],
  ['龙头主升', '涨停连板龙头'],
  ['拉涨捉妖', '涨停连板龙头'],
  ['阳穿地底', '涨停连板龙头'],
  ['反包出击', '涨停连板龙头'],
  ['超跌破位反转', '涨停连板龙头'],
  ['黑马显形', '涨停连板龙头'],
  ['神龙突破', '涨停连板龙头'],
  ['有肉吃', '涨停连板龙头'],
  ['试盘K线', '涨停连板龙头'],
  ['妖股炸裂', '涨停连板龙头'],
  ['定龙', '涨停连板龙头'],
  ['红色主升箱体', '涨停连板龙头'],
  // 五、K线形态/反包拐点
  ['神奇九转', 'K线形态反转'],
  ['上升柱', 'K线形态反转'],
  ['梨花剑', 'K线形态反转'],
  ['M峰卖点', 'K线形态反转'],
  ['绝杀之刃', 'K线形态反转'],
  ['精准踩点', 'K线形态反转'],
  ['筹码先知', 'K线形态反转'],
  ['大牛飞天', 'K线形态反转'],
  ['双底构筑', 'K线形态反转'],
  ['暴利圆弧底', 'K线形态反转'],
  ['擒牛', 'K线形态反转'],
  ['回档买入', 'K线形态反转'],
  // 六、通道/箱体/支撑压力
  ['非常准的趋势买卖', '通道箱体撑压'],
  ['分歧低吸', '通道箱体撑压'],
  ['战赢趋势', '通道箱体撑压'],
  ['强势启动', '通道箱体撑压'],
  // 七、筹码(COST/WINNER)与画图工具
  ['可视化筹码线涨停标记', '筹码与画图'],
  ['筹码动脉', '筹码与画图'],
  ['连板斗黑马', '筹码与画图'],
  ['可调甘氏角', '筹码与画图'],
  ['波浪理论分析', '筹码与画图'],
  ['短中长线撑压', '筹码与画图'],
  ['分时抓板起爆', '筹码与画图'],
  // 八、分时(日内)
  ['AI分时', '分时日内'],
];

// 已删除(2026-07-07 审读后确认):内核造假/营销贴牌/重绘观赏品,或依赖引擎没有的数据核心已死。
// txt 文件仍在 assets/tdx,想恢复某个就从这里移除对应行。
const REMOVED_BY_PREFIX: [string, string][] = [
  // —— 营销贴牌/内核造假 ——
  ['波段擒牛', '营销水印+有效期开关,内核为加权均线'],
  ['黑马显形', '含失效日期开关的大杂烩'],
  ['神秘机构组合', '口诀营销,威廉通道换皮'],
  ['照抄主力', 'EMA(1.5)双金叉纯换皮'],
  ['主力资金流向', '"主力资金"=MA(C,1)-MA(C,6),假资金'],
  ['抓牛分时', '分时均线冒充主力/大户/散户'],
  ['主力控盘分析', '涨跌幅×1000改名"控盘",无资金数据'],
  ['资金起量', '名带资金,实为34日随机值,无资金变量'],
  // —— 重绘/未来函数观赏品 ——
  ['双底构筑', 'BACKSET重绘,历史信号事后补画'],
  ['可调甘氏角', '重绘+江恩角度线玄学'],
  ['波浪理论分析', 'ZIG全篇必重绘,只能观赏'],
  ['游资龙头', 'BACKSET找前高,右侧突破重绘'],
  ['龙头主升', '筹码线无数据+WH系列重绘'],
  // —— 过拟合堆料 ——
  ['擒牛', '十几个AND条件,过拟合典型'],
  ['神龙突破', '全包最长条件流,过拟合堆料'],
  ['定龙', '十几个模块大杂烩'],
  ['妖股炸裂', '纯视觉盛宴,无可执行信息'],
  // —— 依赖引擎没有的数据,核心已死 ——
  // (2026-07-07 晚筹码/股本估算上线后,金蛇寻底/狙击启动/筹码动脉/可视化筹码线已复活移出本名单)
  ['连板斗黑马', '资产负债表细项未接入,Z值核心仍死'],
  ['分时抓板起爆', '分时口径错配日K+FINANCE(46)未接入'],
  ['单笔资金分时', '分时单笔口径,日K上无意义'],
];

function removedOf(base: string): boolean {
  return REMOVED_BY_PREFIX.some(([prefix]) => base.startsWith(prefix));
}

function categoryOf(base: string): TdxCategory {
  for (const [prefix, cat] of CATEGORY_BY_PREFIX) {
    if (base.startsWith(prefix)) return cat;
  }
  return '均线趋势交叉';
}

function cleanName(base: string): string {
  return base
    .replace(/(主图|副图)?指标?公式源码$/u, '')
    .replace(/(主图|副图)?指标公式$/u, '')
    .replace(/公式源码$/u, '')
    .replace(/源码$/u, '')
    .replace(/指标$/u, '')
    .replace(/主图$/u, '')
    .replace(/副图$/u, '')
    .replace(/\(\d+\)$/u, '')
    .replace(/_/gu, '·')
    .trim() || base;
}

function build(): TdxFormula[] {
  const out: TdxFormula[] = [];
  for (const [path, source] of Object.entries(raw)) {
    const base = (path.split('/').pop() || '').replace(/\.txt$/u, '');
    if (!source || source.trim().length < 10) continue;
    if (DUPLICATE_DROP.has(base)) continue;
    if (removedOf(base)) continue;
    const kind: 'main' | 'sub' = base.includes('主图') ? 'main' : 'sub';
    out.push({ id: `tdx:${base}`, name: cleanName(base), kind, category: categoryOf(base), source });
  }
  out.sort((a, b) => a.name.localeCompare(b.name, 'zh'));
  return out;
}

export const TDX_FORMULAS: TdxFormula[] = build();
export const TDX_MAIN: TdxFormula[] = TDX_FORMULAS.filter(f => f.kind === 'main');
export const TDX_SUB: TdxFormula[] = TDX_FORMULAS.filter(f => f.kind === 'sub');

export interface TdxGroup {
  category: TdxCategory;
  items: TdxFormula[];
}

function groupByCategory(list: TdxFormula[]): TdxGroup[] {
  return TDX_CATEGORIES
    .map(category => ({ category, items: list.filter(f => f.category === category) }))
    .filter(g => g.items.length > 0);
}

export const TDX_MAIN_GROUPS: TdxGroup[] = groupByCategory(TDX_MAIN);
export const TDX_SUB_GROUPS: TdxGroup[] = groupByCategory(TDX_SUB);

const byId = new Map(TDX_FORMULAS.map(f => [f.id, f]));
export function getTdxFormula(id: string): TdxFormula | undefined {
  return byId.get(id);
}
