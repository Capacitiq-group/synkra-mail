import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useMailboxes } from '@/hooks/queries/mail';
import { useDigestPrefs, useNewsletterSenders, useSetDigestPrefs, useSetNewsletterPref } from '@/hooks/queries/newsletters';
import { Button, EmptyState, Input, Select, Skeleton, Switch, Tabs, Field } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { UnsubscribeModal } from './UnsubscribeModal';
import { formatDateTime, relativeTime, toLocalTimezone } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { DigestPreferences, NewsletterSender } from '@/lib/types';

const TABS = [
  { id: 'senders', label: 'Senders' },
  { id: 'digest', label: 'Digest' },
];

export function NewslettersRoute() {
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') ?? 'senders';
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Newsletters"
        description="Newsletters, promotional emails and recurring subscriptions recognised in your mailbox."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <div className="px-4 md:px-8 border-b border-line">
        <Tabs items={TABS} value={tab} onChange={(id) => {
          const next = new URLSearchParams(params);
          next.set('tab', id);
          setParams(next, { replace: true });
        }} />
      </div>
      <SettingsContent>
        {tab === 'senders' && <SendersTab mailboxId={mailboxId} />}
        {tab === 'digest' && <DigestTab mailboxId={mailboxId} />}
      </SettingsContent>
    </SettingsPage>
  );
}

function SendersTab({ mailboxId }: { mailboxId: string }) {
  const q = useNewsletterSenders(mailboxId);
  const setPref = useSetNewsletterPref(mailboxId);
  const [filter, setFilter] = useState('');
  const [unsubTarget, setUnsubTarget] = useState<NewsletterSender | null>(null);

  const filtered = useMemo(() => {
    const all = q.data ?? [];
    const f = filter.trim().toLowerCase();
    if (!f) return all;
    return all.filter((s) =>
      s.sender.email.toLowerCase().includes(f) ||
      (s.sender.name ?? '').toLowerCase().includes(f),
    );
  }, [q.data, filter]);

  const stats = useMemo(() => {
    const all = q.data ?? [];
    return {
      total: all.length,
      muted: all.filter((s) => s.muted).length,
      unread: all.reduce((sum, s) => sum + s.unread, 0),
    };
  }, [q.data]);

  return (
    <>
      <div className="grid grid-cols-3 gap-3 mb-4">
        <Stat label="Senders" value={stats.total} />
        <Stat label="Muted" value={stats.muted} />
        <Stat label="Unread newsletters" value={stats.unread} />
      </div>

      <div className="flex items-center gap-2 mb-3">
        <div className="flex-1 relative">
          <Icon name="search" size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-mute" />
          <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Search senders" className="pl-8" />
        </div>
      </div>

      {q.isLoading && <div className="space-y-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-20 w-full" />)}</div>}

      {!q.isLoading && filtered.length === 0 && (
        <EmptyState
          icon={<Icon name="mail-open" size={28} />}
          title={filter ? 'No matches' : 'No newsletters detected'}
          description={filter ? 'Try a different search.' : 'We have not identified any recurring newsletters in this mailbox yet.'}
        />
      )}

      <ul className="space-y-2">
        {filtered.map((s) => (
          <li key={s.sender.email} className="flex items-start gap-3 p-3 border border-line rounded-lg">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <p className="text-sm font-medium truncate">{s.sender.name || s.sender.email}</p>
                {s.sender.name && <span className="text-xs text-ink-soft truncate">&lt;{s.sender.email}&gt;</span>}
                {s.unsubscribe === 'one_click' && <span className="text-xs px-1.5 py-0.5 rounded bg-success-soft text-success">1-click</span>}
                {s.muted && <span className="text-xs px-1.5 py-0.5 rounded bg-canvas border border-line text-ink-soft">Muted</span>}
              </div>
              <p className="text-xs text-ink-soft mt-0.5">
                {s.message_count} message{s.message_count === 1 ? '' : 's'} · {s.frequency}
                {s.unread > 0 ? ` · ${s.unread} unread` : ''} · last received {relativeTime(s.last_received_at)}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="secondary" onClick={() => setUnsubTarget(s)}>
                {s.unsubscribe === 'one_click' ? 'Unsubscribe' : 'Manage'}
              </Button>
              <Switch
                checked={!s.muted}
                onChange={(on) => setPref.mutate({ sender: s.sender.email, pref: { muted: !on } })}
              />
            </div>
          </li>
        ))}
      </ul>

      <UnsubscribeModal
        open={!!unsubTarget}
        onClose={() => setUnsubTarget(null)}
        sender={unsubTarget}
      />
    </>
  );
}

function DigestTab({ mailboxId }: { mailboxId: string }) {
  const q = useDigestPrefs(mailboxId);
  const save = useSetDigestPrefs(mailboxId);

  if (q.isLoading || !q.data) return <Skeleton className="h-40 w-full" />;

  async function update(patch: Partial<DigestPreferences>) {
    try {
      await save.mutateAsync({ ...q.data!, ...patch });
      toast('success', 'Preferences updated');
    } catch (e) {
      toast('error', 'Could not save', errorMessage(e));
    }
  }

  return (
    <div className="space-y-4 max-w-2xl">
      <p className="text-sm text-ink-soft">
        A single email summarising newsletters at a time you choose. Individual messages stay in your mailbox —
        the digest is additive.
      </p>

      <div className="space-y-3">
        <Field label="Digest mode">
          <Select
            value={q.data.mode}
            onChange={(e) => update({ mode: e.target.value as DigestPreferences['mode'] })}
          >
            <option value="off">Off</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
          </Select>
        </Field>

        {q.data.mode !== 'off' && (
          <Field label="Time of day">
            <Input
              type="time"
              value={q.data.time_of_day}
              onChange={(e) => update({ time_of_day: e.target.value })}
            />
          </Field>
        )}

        <Field label="Timezone">
          <Select value={q.data.timezone} onChange={(e) => update({ timezone: e.target.value })}>
            <option value="Africa/Johannesburg">Africa/Johannesburg</option>
            <option value="UTC">UTC</option>
            <option value={toLocalTimezone()}>{toLocalTimezone()}</option>
          </Select>
        </Field>

        <div className="bg-canvas border border-line rounded p-3 text-xs text-ink-soft">
          <strong>How it works.</strong> When the digest fires, we include every newsletter received since the last
          digest. Excluded senders are skipped. Original messages are not moved or deleted.
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="border border-line rounded p-3">
      <p className="text-xs text-ink-mute">{label}</p>
      <p className="text-lg font-semibold">{value}</p>
    </div>
  );
}

// Suppress unused import warning.
void formatDateTime;
