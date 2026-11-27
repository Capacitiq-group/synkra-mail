import { create } from 'zustand';
import { StorageKeys, getItem, setItem } from '@/lib/storage';

interface LayoutState {
  sidebarCollapsed: boolean;
  mobileNavOpen: boolean;
  readingPane: 'split' | 'full';
  toggleSidebar: () => void;
  setMobileNavOpen: (open: boolean) => void;
  setReadingPane: (mode: 'split' | 'full') => void;
}

export const useLayout = create<LayoutState>((set, get) => ({
  sidebarCollapsed: getItem(StorageKeys.sidebar, false),
  mobileNavOpen: false,
  readingPane: getItem<LayoutState['readingPane']>(StorageKeys.listPrefs + '.pane', 'split'),
  toggleSidebar: () => {
    const next = !get().sidebarCollapsed;
    setItem(StorageKeys.sidebar, next);
    set({ sidebarCollapsed: next });
  },
  setMobileNavOpen: (open) => set({ mobileNavOpen: open }),
  setReadingPane: (mode) => {
    setItem(StorageKeys.listPrefs + '.pane', mode);
    set({ readingPane: mode });
  },
}));
