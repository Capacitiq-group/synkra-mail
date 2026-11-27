import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { Button } from '@/design';
import { useDebounce } from '@/hooks';
import { appendToken, parseSearch, removeToken, type ParsedSearch } from './parseSearch';

export interface SearchBarProps {
  /** Placeholder copy depends on whether the folder is a special role. */
  folderName: string;
  onSearchChange: (parsed: ParsedSearch) => void;
  onOpenFilters?: () => void;
}

export function SearchBar({ folderName, onSearchChange, onOpenFilters }: SearchBarProps) {
  const [params, setParams] = useSearchParams();
  const [text, setText] = useState(params.get('q') ?? '');
  const inputRef = useRef<HTMLInputElement>(null);
  const debounced = useDebounce(text, 250);

  // Sync from URL when user navigates (e.g. browser back).
  useEffect(() => {
    setText(params.get('q') ?? '');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.get('q')]);

  // Push debounced value to URL.
  useEffect(() => {
    const current = params.get('q') ?? '';
    if (debounced === current) return;
    const next = new URLSearchParams(params);
    if (debounced) next.set('q', debounced); else next.delete('q');
    setParams(next, { replace: true });
  }, [debounced, params, setParams]);

  const parsed = useMemo(() => parseSearch(text), [text]);
  useEffect(() => { onSearchChange(parsed); }, [parsed, onSearchChange]);

  function setQuery(raw: string) { setText(raw); }

  function removeChip(token: string) { setQuery(removeToken(text, token)); }
  function toggleToken(token: string) {
    const has = text.split(/\s+/).includes(token);
    setQuery(has ? removeToken(text, token) : appendToken(text, token));
  }

  const hasUnread = /\bis:unread\b/i.test(text);
  const hasAttachment = /\bhas:attachment\b/i.test(text);

  return (
    <div className="border-b border-line bg-surface">
      <div className="flex items-center gap-2 px-3 py-2">
        <div className="flex-1 relative">
          <Icon
            name="search"
            size={16}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-mute pointer-events-none"
          />
          <input
            ref={inputRef}
            value={text}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') { setQuery(''); inputRef.current?.blur(); }
            }}
            placeholder={`Search ${folderName} — try from:boss subject:invoice has:attachment`}
            aria-label={`Search ${folderName}`}
            className="w-full pl-8 pr-8 py-1.5 rounded border border-line bg-canvas text-sm focus:outline-none focus:ring-2 focus:ring-accent"
          />
          {text && (
            <button
              type="button"
              onClick={() => setQuery('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded text-ink-mute hover:text-ink"
              aria-label="Clear search"
            >
              <Icon name="close" size={14} />
            </button>
          )}
        </div>
        <div className="flex gap-1">
          <button
            type="button"
            onClick={() => toggleToken('is:unread')}
            className={clsx(
              'px-2 py-1 text-xs rounded border',
              hasUnread ? 'bg-accent text-white border-accent' : 'border-line hover:bg-canvas',
            )}
            aria-pressed={hasUnread}
          >
            Unread
          </button>
          <button
            type="button"
            onClick={() => toggleToken('has:attachment')}
            className={clsx(
              'px-2 py-1 text-xs rounded border',
              hasAttachment ? 'bg-accent text-white border-accent' : 'border-line hover:bg-canvas',
            )}
            aria-pressed={hasAttachment}
          >
            Attachments
          </button>
          {onOpenFilters && (
            <Button size="sm" variant="ghost" onClick={onOpenFilters} aria-label="More filters">
              <Icon name="filter" size={14} />
            </Button>
          )}
        </div>
      </div>
      {parsed.chips.length > 0 && (
        <div className="flex flex-wrap gap-1 px-3 pb-2">
          {parsed.chips.map((c) => (
            <button
              key={c.token}
              type="button"
              onClick={() => removeChip(c.token)}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-full bg-canvas border border-line hover:border-ink-mute"
              aria-label={`Remove filter ${c.label}`}
            >
              <span>{c.label}</span>
              <Icon name="close" size={10} />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
