import { create } from 'zustand'
import { mailApi } from '@/api/endpoints'
import type { Folder, ItemWithMessage, MessageDetail } from '@/types/api'

interface FolderState {
  items: ItemWithMessage[]
  folder: Folder | null
  hasMore: boolean
  nextBeforeUid?: number
  loading: boolean
  error: string | null
}

interface MailState {
  folders: Folder[]
  foldersLoading: boolean
  foldersError: string | null
  currentFolderId: string | null
  pages: Record<string, FolderState>
  detail: MessageDetail | null
  detailLoading: boolean
  searchResults: ItemWithMessage[] | null
  searchLoading: boolean

  loadFolders: () => Promise<void>
  selectFolder: (folderId: string | null) => void
  loadFolder: (folderId: string, opts?: { reset?: boolean }) => Promise<void>
  loadOlder: () => Promise<void>
  openMessage: (id: string) => Promise<MessageDetail | null>
  clearMessage: () => void
  runSearch: (q: string) => Promise<void>
  clearSearch: () => void
  // optimistic flag helpers used by list interactions
  setItemFlags: (itemId: string, flags: string[]) => void
  removeItem: (itemId: string) => void
}

function emptyState(): FolderState {
  return { items: [], folder: null, hasMore: false, loading: false, error: null }
}

function pageOf(get: () => MailState, folderId: string | null): FolderState {
  return (folderId && get().pages[folderId]) || emptyState()
}

export const useMail = create<MailState>((set, get) => ({
  folders: [],
  foldersLoading: false,
  foldersError: null,
  currentFolderId: null,
  pages: {},
  detail: null,
  detailLoading: false,
  searchResults: null,
  searchLoading: false,

  loadFolders: async () => {
    set({ foldersLoading: true, foldersError: null })
    try {
      const { folders } = await mailApi.folders()
      set({ folders, foldersLoading: false })
    } catch (err) {
      set({ foldersLoading: false, foldersError: (err as Error).message })
    }
  },

  selectFolder: (folderId) => {
    set({ currentFolderId: folderId, searchResults: null })
  },

  loadFolder: async (folderId, opts) => {
    const reset = opts?.reset ?? false
    const prev = pageOf(get, folderId)
    set((s) => ({
      pages: {
        ...s.pages,
        [folderId]: { ...prev, loading: true, error: null },
      },
    }))
    try {
      const page = await mailApi.messages(folderId, {
        limit: 50,
        beforeUid: reset ? undefined : prev.nextBeforeUid,
      })
      set((s) => {
        const merged = reset
          ? page.items
          : dedupeAppend(prev.items, page.items)
        return {
          pages: {
            ...s.pages,
            [folderId]: {
              items: merged,
              folder: page.folder,
              hasMore: page.has_more,
              nextBeforeUid: page.next_before_uid,
              loading: false,
              error: null,
            },
          },
        }
      })
    } catch (err) {
      set((s) => ({
        pages: {
          ...s.pages,
          [folderId]: { ...prev, loading: false, error: (err as Error).message },
        },
      }))
    }
  },

  loadOlder: async () => {
    const id = get().currentFolderId
    if (!id) return
    const st = pageOf(get, id)
    if (!st.hasMore || st.loading) return
    await get().loadFolder(id)
  },

  openMessage: async (id) => {
    set({ detailLoading: true, detail: null })
    try {
      const detail = await mailApi.message(id)
      set({ detail, detailLoading: false })
      // Optimistic \Seen on the list entry (server flags update too).
      if (!detail.item.flags.includes('\\Seen')) {
        const flags = [...detail.item.flags, '\\Seen']
        setItemFlagsEverywhere(set, get, detail.item.id, flags)
        try {
          await mailApi.setFlags(id, flags)
        } catch {
          /* flag write is best-effort; the read still happened */
        }
      }
      return detail
    } catch {
      set({ detailLoading: false })
      return null
    }
  },

  clearMessage: () => set({ detail: null, detailLoading: false }),

  runSearch: async (q) => {
    if (!q.trim()) {
      set({ searchResults: null })
      return
    }
    set({ searchLoading: true })
    try {
      const { results } = await mailApi.search(q.trim())
      set({ searchResults: results, searchLoading: false })
    } catch {
      set({ searchResults: [], searchLoading: false })
    }
  },

  clearSearch: () => set({ searchResults: null, searchLoading: false }),

  setItemFlags: (itemId, flags) => setItemFlagsEverywhere(set, get, itemId, flags),

  removeItem: (itemId) =>
    set((s) => {
      const pages: Record<string, FolderState> = {}
      for (const [fid, st] of Object.entries(s.pages)) {
        pages[fid] = { ...st, items: st.items.filter((i) => i.item.id !== itemId) }
      }
      return { pages }
    }),
}))

function dedupeAppend(base: ItemWithMessage[], next: ItemWithMessage[]): ItemWithMessage[] {
  const seen = new Set(base.map((i) => i.item.id))
  return [...base, ...next.filter((i) => !seen.has(i.item.id))]
}

function setItemFlagsEverywhere(
  set: (fn: (s: MailState) => Partial<MailState>) => void,
  get: () => MailState,
  itemId: string,
  flags: string[]
): void {
  void get
  set((s) => {
    const pages: Record<string, FolderState> = {}
    for (const [fid, st] of Object.entries(s.pages)) {
      pages[fid] = {
        ...st,
        items: st.items.map((i) =>
          i.item.id === itemId ? { ...i, item: { ...i.item, flags } } : i
        ),
      }
    }
    return { pages }
  })
}
