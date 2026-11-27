import { create } from 'zustand';
import { StorageKeys, getItem, setItem } from '@/lib/storage';

export type ThemeMode = 'light' | 'dark' | 'system';

interface ThemeState {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  resolved: 'light' | 'dark';
  apply: () => void;
}

function resolveSystem(): 'light' | 'dark' {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function resolveTheme(mode: ThemeMode): 'light' | 'dark' {
  return mode === 'system' ? resolveSystem() : mode;
}

function applyClass(t: 'light' | 'dark') {
  document.documentElement.classList.toggle('dark', t === 'dark');
}

export const useTheme = create<ThemeState>((set, get) => ({
  mode: getItem<ThemeMode>(StorageKeys.theme, 'system'),
  resolved: resolveTheme(getItem<ThemeMode>(StorageKeys.theme, 'system')),
  setMode: (mode) => {
    setItem(StorageKeys.theme, mode);
    const resolved = resolveTheme(mode);
    applyClass(resolved);
    set({ mode, resolved });
  },
  apply: () => {
    const resolved = resolveTheme(get().mode);
    applyClass(resolved);
    set({ resolved });
  },
}));
