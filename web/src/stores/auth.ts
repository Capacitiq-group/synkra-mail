import { create } from 'zustand';
import type { Me } from '@/lib/types';

interface AuthState {
  me: Me | null;
  status: 'idle' | 'loading' | 'authenticated' | 'unauthenticated' | 'error';
  setMe: (me: Me | null) => void;
  setStatus: (s: AuthState['status']) => void;
}

export const useAuth = create<AuthState>((set) => ({
  me: null,
  status: 'idle',
  setMe: (me) => set({ me, status: me ? 'authenticated' : 'unauthenticated' }),
  setStatus: (status) => set({ status }),
}));
