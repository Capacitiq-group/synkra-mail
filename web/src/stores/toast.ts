import { create } from 'zustand';

export interface ToastItem {
  id: string;
  kind: 'info' | 'success' | 'warning' | 'error';
  title: string;
  description?: string;
  durationMs: number;
  action?: { label: string; onClick: () => void };
}

interface ToastState {
  items: ToastItem[];
  push: (input: Omit<ToastItem, 'id' | 'durationMs'> & { durationMs?: number }) => string;
  remove: (id: string) => void;
  clear: () => void;
}

let seq = 0;

export const useToast = create<ToastState>((set) => ({
  items: [],
  push: (input) => {
    const id = `t${++seq}`;
    const durationMs = input.durationMs ?? (input.kind === 'error' ? 8000 : 4000);
    set((s) => ({ items: [...s.items, { ...input, id, durationMs }] }));
    if (durationMs > 0) setTimeout(() => set((s) => ({ items: s.items.filter((x) => x.id !== id) })), durationMs);
    return id;
  },
  remove: (id) => set((s) => ({ items: s.items.filter((x) => x.id !== id) })),
  clear: () => set({ items: [] }),
}));

export function toast(kind: ToastItem['kind'], title: string, description?: string, action?: ToastItem['action']) {
  return useToast.getState().push({ kind, title, description, action });
}
