import { create } from 'zustand';

interface SseState {
  connected: boolean;
  lastEventAt: number | null;
  setConnected: (v: boolean) => void;
  touch: () => void;
}

export const useSse = create<SseState>((set) => ({
  connected: false,
  lastEventAt: null,
  setConnected: (connected) => set({ connected }),
  touch: () => set({ lastEventAt: Date.now() }),
}));
