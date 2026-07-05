// 把专家发言解析为记分卡数据。支持两种格式：
//   1) Battle/量化：【立场】【买点】【卖点】【止损】【仓位】【时效】【核心证据】
//   2) 各专家自有格式：政策：/影响链路：/阶段：/量化：/price-in：…（中文冒号，行首短标签）
// 只要识别到 >=3 个结构化字段就渲染卡片；否则返回 null 回退原始 markdown。

export type StanceSide = 'bull' | 'bear' | 'neutral';

export interface CardSection {
  label: string;
  value: string;
}

export interface ExpertCard {
  hasStance: boolean;
  side: StanceSide;
  stanceText: string;
  confidence: number;
  gaugePos: number;
  confTier: 'high' | 'mid' | 'low';
  confBadge: string;
  // 决策四项（存在才显示）
  buy?: string;
  sell?: string;
  stop?: string;
  stopPrice?: string;
  position?: string;
  hasDecision: boolean;
  // 其余结构化字段（影响链路/阶段/量化/催化/price-in…）
  extras: CardSection[];
  evidence: string[];
  validityDate?: string;
  validityDaysLeft?: number;
  sampleStart?: string;
  sampleEnd?: string;
}

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// 提取有序的 [{label,value}]：优先【】块，否则按行首"短标签：值"
function extractSections(content: string): CardSection[] {
  const out: CardSection[] = [];
  if (/【[^】]+】/.test(content)) {
    const re = /【([^】]+)】([\s\S]*?)(?=【[^】]+】|$)/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(content)) !== null) {
      const label = m[1].trim();
      const value = m[2].trim().replace(/^[：:\s]+/, '');
      if (label) out.push({ label, value });
    }
    return out;
  }
  for (const raw of content.split(/\n+/)) {
    const line = raw.trim();
    // 行首短标签(<=8字，可含英文/-)+中文或英文冒号
    const m = line.match(/^([A-Za-z一-龥][A-Za-z0-9一-龥\-/]{0,9})[：:]\s*(.+)$/);
    if (m) out.push({ label: m[1].trim(), value: m[2].trim() });
  }
  return out;
}

const hit = (label: string, keys: string[]) => keys.some(k => label.includes(k));

function parseDates(text: string): string[] {
  const out: string[] = [];
  const re = /(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) out.push(`${m[1]}-${m[2].padStart(2, '0')}-${m[3].padStart(2, '0')}`);
  return out;
}

function daysBetween(from: Date, to: Date): number {
  return Math.round((to.setHours(0, 0, 0, 0) - from.setHours(0, 0, 0, 0)) / 86400000);
}

export function parseExpertCard(content: string): ExpertCard | null {
  if (!content || content.length < 8) return null;
  const sections = extractSections(content);
  if (sections.length < 3) return null; // 结构化字段太少 → 不是卡片格式

  let hasStance = false;
  let side: StanceSide = 'neutral';
  let stanceText = '中性';
  let confidence = 50;
  let buy: string | undefined, sell: string | undefined, stop: string | undefined, position: string | undefined;
  let evidence: string[] = [];
  let validityRaw = '';
  const extras: CardSection[] = [];

  for (const sec of sections) {
    const { label, value } = sec;
    if (!hasStance && hit(label, ['立场'])) {
      hasStance = true;
      if (/看多|偏多/.test(value)) { side = 'bull'; stanceText = '看多'; }
      else if (/看空|偏空/.test(value)) { side = 'bear'; stanceText = '看空'; }
      else { side = 'neutral'; stanceText = '中性'; }
      const cm = value.match(/(\d{1,3})/);
      if (cm) confidence = clamp(parseInt(cm[1], 10), 0, 100);
    } else if (!buy && hit(label, ['买点'])) buy = value;
    else if (!sell && hit(label, ['卖点'])) sell = value;
    else if (!stop && hit(label, ['止损'])) stop = value;
    else if (!position && hit(label, ['仓位'])) position = value;
    else if (hit(label, ['核心证据', '证据'])) {
      evidence = value.split(/\s*\d+\s*[)）.、]\s*|\n+|；;/).map(s => s.trim()).filter(s => s.length > 1);
    } else if (hit(label, ['时效', '有效期', '有效'])) validityRaw = value;
    else extras.push(sec);
  }

  const hasDecision = !!(buy || sell || stop || position);
  if (!hasStance && !hasDecision && extras.length < 3) return null;

  // 仪表位置
  const gaugePos = side === 'bull' ? clamp(50 + confidence / 2, 50, 98)
    : side === 'bear' ? clamp(50 - confidence / 2, 2, 50) : 50;

  // 置信度档位
  const lowKw = /样本不足|数据不足|低置信|不足/.test(content);
  let confTier: 'high' | 'mid' | 'low' = confidence >= 70 ? 'high' : confidence >= 40 ? 'mid' : 'low';
  if (lowKw) confTier = 'low';
  const sampleDayM = content.match(/样本[^0-9]{0,4}(\d{1,3})\s*日|(\d{1,3})\s*日样本/);
  const sampleDay = sampleDayM ? (sampleDayM[1] || sampleDayM[2]) : '';
  const tierText = confTier === 'high' ? '高置信' : confTier === 'mid' ? '中置信' : '低置信';
  const confBadge = sampleDay ? `样本${sampleDay}日·${tierText}` : tierText;

  let stopPrice: string | undefined;
  if (stop) { const pm = stop.match(/(\d+(?:\.\d+)?)/); if (pm) stopPrice = pm[1]; }

  // 时效/样本
  let validityDate: string | undefined, validityDaysLeft: number | undefined;
  let sampleStart: string | undefined, sampleEnd: string | undefined;
  const vDates = parseDates(validityRaw);
  if (/样本/.test(validityRaw) && vDates.length >= 2) {
    sampleStart = vDates[0]; sampleEnd = vDates[vDates.length - 1];
  } else if (vDates.length >= 1) {
    const d = vDates[vDates.length - 1];
    const left = daysBetween(new Date(), new Date(d));
    if (left >= 0) { validityDate = d; validityDaysLeft = left; }
    else if (vDates.length >= 2) { sampleStart = vDates[0]; sampleEnd = d; }
  }

  return {
    hasStance, side, stanceText, confidence, gaugePos, confTier, confBadge,
    buy, sell, stop, stopPrice, position, hasDecision,
    extras, evidence, validityDate, validityDaysLeft, sampleStart, sampleEnd,
  };
}
