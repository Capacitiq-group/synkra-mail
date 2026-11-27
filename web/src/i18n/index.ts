// Minimal typed i18n. Ships en (South African business English) only for now;
// the structure is here so additional locales are a dictionary file + a
// locale switch, not a refactor.

import { en } from './en'

export type Locale = 'en'
export type Dict = typeof en
export type TKey = keyof Dict

const dictionaries: Record<Locale, Dict> = { en }

let locale: Locale = 'en'

export function setLocale(l: Locale): void {
  locale = l
}

/** t('folders.unread', { n: 3 }) → "3 unread" */
export function t(key: TKey, vars?: Record<string, string | number>): string {
  let s: string = dictionaries[locale][key] ?? (key as string)
  if (vars) {
    for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v))
  }
  return s
}
