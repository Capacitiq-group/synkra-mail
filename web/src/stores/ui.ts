import { create } from 'zustand'

export type ToastKind = 'info' | 'success' | 'error'

export interface Toast {
  id: number
  kind: ToastKind
  title: string
  detail?: string
}

interface UIState {
  toasts: Toast[]
  toast: (kind: ToastKind, title: string, detail?: string) => void
  dismiss: (id: number) => void
}

let nextId = 1

export const useUI = create<UIState>((set, get) => ({
  toasts: [],
  toast: (kind, title, detail) => {
    const id = nextId++
    set({ toasts: [...get().toasts, { id, kind, title, detail }] })
  },
  dismiss: (id) => set({ toasts: get().toasts.filter((t) => t.id !== id) }),
}))

/** Convenience: surface an ApiError (or anything) as a toast. */
export function toastError(err: unknown, fallback = 'Something went wrong'): void {
  const e = err as { code?: string; message?: string; hint?: string }
  useUI
    .getState()
    .toast('error', e?.message || fallback, e?.hint || (e?.code ? `code: ${e.code}` : undefined))
}
