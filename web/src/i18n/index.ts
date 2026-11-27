import enZA from './en-ZA';

export type Dict = typeof enZA;
const dictionaries: Record<string, Dict> = { 'en-ZA': enZA };

export function t(): Dict {
  const locale = typeof navigator !== 'undefined' ? navigator.language : 'en-ZA';
  return dictionaries[locale] ?? dictionaries['en-ZA'];
}

export const strings = t();
