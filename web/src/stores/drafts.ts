import { create } from 'zustand';
import type { Draft } from '@/lib/types';

interface DraftsState {
  dirty: Record<string, Partial<Draft>>;
  mark: (mailboxId: string, patch: Partial<Draft>) => void;
  clear: (mailboxId: string) => void;
}

export const useDrafts = create<DraftsState>((set) => ({
  dirty: {},
  mark: (mailboxId, patch) =>
    set((s) => ({ dirty: { ...s.dirty, [mailboxId]: { ...(s.dirty[mailboxId] || {}), ...patch } } })),
  clear: (mailboxId) =>
    set((s) => {
      const next = { ...s.dirty };
      delete next[mailboxId];
      return { dirty: next };
    }),
}));
