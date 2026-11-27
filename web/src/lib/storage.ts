export const StorageKeys = {
  theme: 'synkra.theme',
  sidebar: 'synkra.sidebar',
  drafts: 'synkra.drafts',
  compose: 'synkra.compose',
  lastMailbox: 'synkra.lastMailbox',
  listPrefs: 'synkra.listPrefs',
  readImages: 'synkra.readImages',
  notificationsSeen: 'synkra.notificationsSeen',
  offlineMessages: 'synkra.offlineMessages',
} as const;

export function getItem<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return fallback;
    return JSON.parse(raw) as T;
  } catch { return fallback; }
}

export function setItem<T>(key: string, value: T): void {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* quota */ }
}

export function removeItem(key: string): void {
  try { localStorage.removeItem(key); } catch { /* noop */ }
}

export function sessionGet<T>(key: string, fallback: T): T {
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) return fallback;
    return JSON.parse(raw) as T;
  } catch { return fallback; }
}

export function sessionSet<T>(key: string, value: T): void {
  try { sessionStorage.setItem(key, JSON.stringify(value)); } catch { /* quota */ }
}
