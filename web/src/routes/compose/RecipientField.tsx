import { useEffect, useId, useRef, useState } from 'react';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { isEmail } from '@/lib/validators';
import type { Address } from '@/lib/types';
import { useRecipientAutocomplete, type AutocompleteEntry } from './useRecipientAutocomplete';

interface Props {
  label: string;
  values: Address[];
  onChange: (list: Address[]) => void;
  placeholder?: string;
  autoFocus?: boolean;
  id?: string;
}

export function RecipientField({ label, values, onChange, placeholder, autoFocus, id }: Props) {
  const fieldId = useId();
  const inputId = id ?? fieldId;
  const [text, setText] = useState('');
  const [focused, setFocused] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [showInvalid, setShowInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  const { results, loading } = useRecipientAutocomplete(text);

  useEffect(() => { if (autoFocus) inputRef.current?.focus(); }, [autoFocus]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setFocused(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  function commit(raw: string) {
    const trimmed = raw.trim().replace(/[,;]$/, '');
    if (!trimmed) return;
    if (!isEmail(trimmed)) {
      setShowInvalid(true);
      return;
    }
    if (values.some((v) => v.email.toLowerCase() === trimmed.toLowerCase())) {
      setText('');
      return;
    }
    onChange([...values, { email: trimmed }]);
    setText('');
    setShowInvalid(false);
    setActiveIndex(-1);
  }

  function commitEntry(e: AutocompleteEntry) {
    if (values.some((v) => v.email.toLowerCase() === e.email.toLowerCase())) {
      setText('');
      return;
    }
    onChange([...values, { email: e.email, name: e.name }]);
    setText('');
    setActiveIndex(-1);
  }

  function removeAt(index: number) {
    const next = values.slice();
    next.splice(index, 1);
    onChange(next);
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === 'Tab' || e.key === ',' || e.key === ';') {
      if (results[activeIndex]) {
        e.preventDefault();
        commitEntry(results[activeIndex]);
        return;
      }
      if (text.trim()) {
        e.preventDefault();
        commit(text);
      }
      return;
    }
    if (e.key === 'Backspace' && text === '' && values.length > 0) {
      e.preventDefault();
      removeAt(values.length - 1);
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIndex((i) => Math.min(results.length - 1, i + 1));
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIndex((i) => Math.max(-1, i - 1));
    }
  }

  const dropdownOpen = focused && (results.length > 0 || loading);

  return (
    <div ref={rootRef} className="flex items-start gap-2 px-3 py-1.5 border-b border-line last:border-b-0">
      <label htmlFor={inputId} className="text-sm text-ink-soft pt-1 w-10 shrink-0">
        {label}
      </label>
      <div className="flex-1 min-w-0 relative">
        <div
          className={clsx(
            'flex flex-wrap items-center gap-1 min-h-[28px] px-1 py-0.5 rounded border',
            showInvalid ? 'border-danger' : 'border-transparent',
            focused && 'border-accent',
          )}
          onClick={() => inputRef.current?.focus()}
        >
          {values.map((v, i) => (
            <span
              key={`${v.email}-${i}`}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-canvas border border-line text-xs max-w-[280px]"
            >
              <span className="truncate">{v.name ? `${v.name} <${v.email}>` : v.email}</span>
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); removeAt(i); }}
                className="text-ink-mute hover:text-ink"
                aria-label={`Remove ${v.email}`}
              >
                <Icon name="close" size={10} />
              </button>
            </span>
          ))}
          <input
            id={inputId}
            ref={inputRef}
            type="text"
            value={text}
            onChange={(e) => { setText(e.target.value); setActiveIndex(-1); setShowInvalid(false); }}
            onKeyDown={onKeyDown}
            onFocus={() => setFocused(true)}
            onBlur={() => { if (text.trim()) commit(text); }}
            placeholder={values.length === 0 ? placeholder : undefined}
            className="flex-1 min-w-[120px] px-1 py-0.5 text-sm bg-transparent focus:outline-none"
            aria-autocomplete="list"
            aria-expanded={dropdownOpen}
            aria-controls={`${inputId}-listbox`}
          />
        </div>

        {dropdownOpen && (
          <ul
            id={`${inputId}-listbox`}
            role="listbox"
            className="absolute z-30 left-0 right-0 mt-1 max-h-64 overflow-y-auto scroll-thin bg-surface border border-line rounded shadow-md py-1"
          >
            {loading && <li className="px-3 py-1.5 text-xs text-ink-soft">Searching…</li>}
            {results.map((r, i) => (
              <li
                key={r.email}
                role="option"
                aria-selected={i === activeIndex}
                onMouseDown={(e) => { e.preventDefault(); commitEntry(r); }}
                className={clsx(
                  'px-3 py-1.5 cursor-pointer text-sm flex items-center gap-2',
                  i === activeIndex ? 'bg-canvas' : 'hover:bg-canvas',
                )}
              >
                <span className="flex-1 truncate">
                  {r.name ? <><strong>{r.name}</strong> <span className="text-ink-soft">&lt;{r.email}&gt;</span></> : r.email}
                </span>
                <span className="text-xs text-ink-mute">{r.source}</span>
              </li>
            ))}
          </ul>
        )}

        {showInvalid && (
          <p className="text-xs text-danger mt-0.5">Not a valid email address</p>
        )}
      </div>
    </div>
  );
}
