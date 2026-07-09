// 交易情报库(第二大脑)前端服务。用 window.go.main.App 直调(新方法,走本地路由)。
import { isWailsGoReady } from '../utils/wailsEnv';

export interface IntelNote {
  id: number;
  createdAt: string;
  text: string;
  source: string;   // manual | news | url
  codes: string[];
}

export interface IntelDigest {
  success: boolean;
  digest: string;
  noteCount: number;
  holdCount: number;
  generatedAt: string;
  modelName?: string;
  error?: string;
}

const app = () => (window as any).go?.main?.App;

export const addIntelNote = async (text: string, codes: string[], source = 'manual'): Promise<IntelNote | null> => {
  if (!isWailsGoReady()) return null;
  try {
    return await app()?.AddIntelNote(text, codes ?? [], source);
  } catch (e) {
    console.error('[intel] addNote', e);
    throw e;
  }
};

export const listIntelNotes = async (codeFilter = '', limit = 200): Promise<IntelNote[]> => {
  if (!isWailsGoReady()) return [];
  try {
    const r = await app()?.ListIntelNotes(codeFilter, limit);
    return Array.isArray(r) ? r : [];
  } catch (e) {
    console.error('[intel] listNotes', e);
    return [];
  }
};

export const deleteIntelNote = async (id: number): Promise<string> => {
  if (!isWailsGoReady()) return 'not-ready';
  return await app()?.DeleteIntelNote(id);
};

// positions:前端从(NAS)取到的持仓,传进后端参与"反证检查"。
export const generateIntelDigest = async (positions: any[]): Promise<IntelDigest | null> => {
  if (!isWailsGoReady()) return null;
  try {
    return await app()?.GenerateIntelDigest(positions ?? []);
  } catch (e) {
    console.error('[intel] digest', e);
    return { success: false, digest: '', noteCount: 0, holdCount: 0, generatedAt: '', error: (e as Error)?.message || '生成失败' };
  }
};
