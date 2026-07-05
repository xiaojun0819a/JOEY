// 从一段会议消息里浓缩出"上次诊断摘要"：综合立场 + 三方分歧比分 + 置信度 + 一句话结论。
import type { ChatMessage } from '../services/sessionService';
import { parseExpertCard, type StanceSide } from './expertCard';

export interface DiagnosisSummary {
  stance: StanceSide; // 综合立场
  stanceText: string;
  bull: number;
  bear: number;
  neutral: number;
  confidence: number; // 平均置信度
  conclusion: string; // 一句话结论
  divergent: boolean; // 存在多空对立
  highDivergence: boolean; // 分歧大，谨慎参考
  asOf: number; // 最后一条相关消息时间
}

const isExpertMsg = (m: ChatMessage) =>
  m.agentId !== 'user' && m.agentId !== 'system' && m.agentId !== 'moderator' && !m.error && !!m.content?.trim();

// 从主持人总结/裁决里抽一句话结论
function extractConclusion(content: string): string {
  const pick = (label: string): string | null => {
    const idx = content.indexOf(label);
    if (idx < 0) return null;
    let seg = content.slice(idx + label.length);
    const nl = seg.search(/[\n\r]/);
    if (nl > 0) seg = seg.slice(0, nl);
    return seg.replace(/^[：:·\s]+/, '').trim() || null;
  };
  return (
    pick('【综合结论】') ||
    pick('【裁决】') ||
    pick('【结论】') ||
    content.split(/[\n\r]/).map(s => s.trim()).find(s => s.length > 4) ||
    ''
  ).slice(0, 80);
}

export function summarizeDiagnosis(messages: ChatMessage[]): DiagnosisSummary | null {
  if (!messages || messages.length === 0) return null;

  // 每位专家取其最新一次发言的立场
  const latestByAgent = new Map<string, ChatMessage>();
  for (const m of messages) {
    if (isExpertMsg(m)) latestByAgent.set(m.agentId, m);
  }
  if (latestByAgent.size === 0) return null;

  let bull = 0, bear = 0, neutral = 0;
  const confs: number[] = [];
  let asOf = 0;
  for (const m of latestByAgent.values()) {
    asOf = Math.max(asOf, m.timestamp || 0);
    const card = parseExpertCard(m.content);
    // 仅在确有【立场】时计入多空分歧与置信度，分析型发言(无立场)不污染统计
    const side: StanceSide = card?.hasStance ? card.side : 'neutral';
    if (side === 'bull') bull++; else if (side === 'bear') bear++; else neutral++;
    if (card?.hasStance && card.confidence > 0) confs.push(card.confidence);
  }

  const total = bull + bear + neutral;
  const confidence = confs.length ? Math.round(confs.reduce((a, b) => a + b, 0) / confs.length) : 0;

  // 综合立场：多空相消后的净方向；持平按中性
  let stance: StanceSide = 'neutral';
  if (bull > bear && bull >= neutral) stance = 'bull';
  else if (bear > bull && bear >= neutral) stance = 'bear';
  const stanceText = stance === 'bull' ? '看多' : stance === 'bear' ? '看空' : '中性';

  const divergent = bull > 0 && bear > 0;
  const maxSide = Math.max(bull, bear, neutral);
  const highDivergence = divergent && maxSide <= Math.ceil(total / 2);

  // 一句话结论：优先最后一条主持人总结/裁决
  let conclusion = '';
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.agentId === 'moderator' && m.content?.trim()) {
      conclusion = extractConclusion(m.content);
      asOf = Math.max(asOf, m.timestamp || 0);
      break;
    }
  }

  return { stance, stanceText, bull, bear, neutral, confidence, conclusion, divergent, highDivergence, asOf };
}
