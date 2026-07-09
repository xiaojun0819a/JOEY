import React, { useCallback, useEffect, useState } from 'react';
import { X, Brain, Plus, Trash2, Loader2, Sparkles, RefreshCw } from 'lucide-react';
import { NodeRenderer } from 'markstream-react';
import { useTheme } from '../contexts/ThemeContext';
import { addIntelNote, listIntelNotes, deleteIntelNote, generateIntelDigest, IntelNote } from '../services/intelService';
import { getHeldPositions } from '../services/sessionService';

interface Props {
  isOpen: boolean;
  onClose: () => void;
}

const SOURCE_LABEL: Record<string, string> = { manual: '手记', news: '快讯', url: '链接' };

export const IntelDialog: React.FC<Props> = ({ isOpen, onClose }) => {
  const { colors } = useTheme();
  const dark = colors.isDark;
  const [notes, setNotes] = useState<IntelNote[]>([]);
  const [draft, setDraft] = useState('');
  const [codes, setCodes] = useState('');
  const [saving, setSaving] = useState(false);
  const [digest, setDigest] = useState('');
  const [digestMeta, setDigestMeta] = useState('');
  const [genLoading, setGenLoading] = useState(false);
  const [genError, setGenError] = useState('');

  const load = useCallback(async () => {
    setNotes(await listIntelNotes('', 200));
  }, []);

  useEffect(() => { if (isOpen) load(); }, [isOpen, load]);

  const save = async () => {
    const t = draft.trim();
    if (!t) return;
    setSaving(true);
    try {
      const codeList = codes.split(/[\s,，]+/).map(s => s.trim()).filter(Boolean);
      await addIntelNote(t, codeList, 'manual');
      setDraft(''); setCodes('');
      await load();
    } catch (e) {
      // 错误已在 service 打日志
    }
    setSaving(false);
  };

  const remove = async (id: number) => {
    await deleteIntelNote(id);
    await load();
  };

  const genDigest = async () => {
    setGenLoading(true); setGenError(''); setDigest('');
    try {
      const positions = await getHeldPositions().catch(() => []);
      const res = await generateIntelDigest(positions || []);
      if (!res) { setGenError('未就绪'); }
      else if (!res.success) { setGenError(res.error || '生成失败'); }
      else {
        setDigest(res.digest);
        setDigestMeta(`${res.generatedAt} · 分析 ${res.noteCount} 条笔记 / ${res.holdCount} 个持仓${res.modelName ? ' · ' + res.modelName : ''}`);
      }
    } catch (e: any) {
      setGenError(e?.message || '生成失败');
    }
    setGenLoading(false);
  };

  if (!isOpen) return null;

  const card = dark ? 'bg-slate-900/60 border-slate-700/50' : 'bg-white border-slate-200';
  const inputCls = `w-full box-border px-3 py-2 rounded-lg border text-sm outline-none resize-none ${dark ? 'bg-slate-950 border-slate-700 text-slate-100' : 'bg-white border-slate-300 text-slate-800'}`;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className={`w-[880px] max-w-[94vw] h-[80vh] rounded-xl border shadow-2xl flex flex-col overflow-hidden ${dark ? 'bg-slate-900 border-slate-700' : 'bg-slate-50 border-slate-200'}`}>
        {/* 头 */}
        <div className={`flex items-center justify-between px-5 py-3 border-b ${dark ? 'border-slate-800' : 'border-slate-200'}`}>
          <div className="flex items-center gap-2">
            <Brain className="h-5 w-5 text-accent-2" />
            <div>
              <div className={`font-semibold ${dark ? 'text-white' : 'text-slate-800'}`}>交易情报库 · 第二大脑</div>
              <div className="text-[11px] text-slate-400">只做两件事:把信息接起来 · 主动送反证(不预测、不喊单)</div>
            </div>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200"><X className="h-5 w-5" /></button>
        </div>

        <div className="flex-1 min-h-0 flex">
          {/* 左:入库 + 笔记列表 */}
          <div className={`w-[42%] min-w-0 border-r flex flex-col ${dark ? 'border-slate-800' : 'border-slate-200'}`}>
            <div className={`p-4 border-b ${dark ? 'border-slate-800' : 'border-slate-200'}`}>
              <div className="text-xs font-medium mb-2 flex items-center gap-1.5"><Plus className="h-3.5 w-3.5 text-accent-2" />记一条情报</div>
              <textarea value={draft} onChange={e => setDraft(e.target.value)} rows={3}
                placeholder="看到的观点、听到的、临时想到的…粘进来就行" className={inputCls} />
              <input value={codes} onChange={e => setCodes(e.target.value)}
                placeholder="关联股票代码(可空,逗号分隔,如 sh600519)" className={`mt-2 ${inputCls}`} />
              <button disabled={saving || !draft.trim()} onClick={save}
                className="mt-2 w-full py-2 rounded-lg text-sm font-medium text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-2)] disabled:opacity-40">
                {saving ? '保存中…' : '入库'}
              </button>
            </div>
            <div className="flex-1 min-h-0 overflow-auto p-3 space-y-2">
              <div className="text-[11px] text-slate-500 px-1">已沉淀 {notes.length} 条</div>
              {notes.map(n => (
                <div key={n.id} className={`group rounded-lg border p-2.5 ${card}`}>
                  <div className="flex items-start justify-between gap-2">
                    <div className="text-[13px] leading-relaxed whitespace-pre-wrap break-words min-w-0">{n.text}</div>
                    <button onClick={() => remove(n.id)} className="shrink-0 text-slate-500 hover:text-red-400 opacity-0 group-hover:opacity-100"><Trash2 className="h-3.5 w-3.5" /></button>
                  </div>
                  <div className="mt-1.5 flex items-center gap-1.5 flex-wrap text-[10px]">
                    <span className="text-slate-500">{n.createdAt?.slice(5, 16)}</span>
                    <span className="rounded bg-slate-500/15 px-1.5 py-0.5 text-slate-400">{SOURCE_LABEL[n.source] || n.source}</span>
                    {n.codes?.map(c => <span key={c} className="rounded bg-accent/15 px-1.5 py-0.5 text-accent">{c}</span>)}
                  </div>
                </div>
              ))}
              {notes.length === 0 && <div className="text-center text-xs text-slate-500 py-8">还没有情报。先记几条,再生成晨报。</div>}
            </div>
          </div>

          {/* 右:反证晨报 */}
          <div className="flex-1 min-w-0 flex flex-col">
            <div className={`p-4 border-b flex items-center justify-between ${dark ? 'border-slate-800' : 'border-slate-200'}`}>
              <div className="text-xs font-medium flex items-center gap-1.5"><Sparkles className="h-3.5 w-3.5 text-accent-2" />反证晨报</div>
              <button disabled={genLoading} onClick={genDigest}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-2)] disabled:opacity-50">
                {genLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                {digest ? '重新生成' : '生成晨报'}
              </button>
            </div>
            <div className="flex-1 min-h-0 overflow-auto p-4">
              {genLoading ? (
                <div className="flex flex-col items-center justify-center h-full text-center gap-3">
                  <Loader2 className="h-7 w-7 animate-spin text-accent-2" />
                  <div className="text-sm">正在把你的持仓、情报和快讯接起来,找反证…</div>
                  <div className="text-xs text-slate-500 max-w-[420px]">只找矛盾和该核实的问题,不会给买卖建议。可能需要 1-2 分钟。</div>
                </div>
              ) : genError ? (
                <div className="rounded-lg border border-rose-400/35 bg-rose-500/10 p-3 text-sm text-rose-200">{genError}</div>
              ) : digest ? (
                <div>
                  <div className="text-[11px] text-slate-500 mb-2">{digestMeta}</div>
                  <article className="agent-message-content text-sm leading-relaxed">
                    <NodeRenderer content={digest} final />
                  </article>
                </div>
              ) : (
                <div className="text-center text-sm text-slate-500 py-10 px-6 leading-relaxed">
                  点「生成晨报」——我会拿你当前的持仓、情报库笔记和近期快讯,<br />
                  给你一份<b className="text-slate-300">「哪些新信息正在打脸你的持仓 + 今天该核实什么」</b>,<br />
                  每条都标出处,不预测涨跌、不给买卖建议。
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default IntelDialog;
