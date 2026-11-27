import type { ComposeSnapshot } from './types';

const KEY = 'synkra.compose.autosave';
const MAX_AGE_MS = 24 * 60 * 60 * 1000; // 1 day

export interface AutosaveRecord {
  snapshot: ComposeSnapshot;
  savedAt: number;
  sessionId: string;
}

export function writeLocalAutosave(sessionId: string, snapshot: ComposeSnapshot): void {
  try {
    const rec: AutosaveRecord = { snapshot, savedAt: Date.now(), sessionId };
    localStorage.setItem(KEY, JSON.stringify(rec));
  } catch {
    /* quota or disabled */
  }
}

export function readLocalAutosave(): AutosaveRecord | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const rec = JSON.parse(raw) as AutosaveRecord;
    if (Date.now() - rec.savedAt > MAX_AGE_MS) { localStorage.removeItem(KEY); return null; }
    return rec;
  } catch {
    return null;
  }
}

export function clearLocalAutosave(): void {
  try { localStorage.removeItem(KEY); } catch { /* noop */ }
}
