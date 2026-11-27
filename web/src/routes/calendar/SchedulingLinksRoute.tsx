import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSchedulingLinks, useCreateSchedulingLink, useDeleteSchedulingLink, useCalendars } from '@/hooks/queries/calendar';
import { Button, ConfirmDialog, EmptyState, Field, Input, Modal, Select, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { useAuth } from '@/stores/auth';
import { usePageTitle } from '@/hooks';
import { WEEKDAY_LABELS } from '@/lib/dates';
import type { SchedulingLink } from '@/lib/types';

type DayAvail = { start: string; end: string };
type Availability = Record<string, DayAvail[]>;

const DAY_KEYS = ['1', '2', '3', '4', '5', '6', '0']; // Mon..Sun (JS day numbers as strings)

export function SchedulingLinksRoute() {
  usePageTitle('Scheduling links');
  const navigate = useNavigate();
  const me = useAuth((s) => s.me);
  const q = useSchedulingLinks();
  const del = useDeleteSchedulingLink();
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SchedulingLink | null>(null);

  const baseUrl = `${window.location.origin}/book/`;

  return (
    <div className="h-full flex flex-col bg-surface">
      <header className="px-4 py-3 border-b border-line flex items-center gap-2">
        <Button size="icon" variant="ghost" onClick={() => navigate('/calendar')} aria-label="Back to calendar">
          <Icon name="chevron-left" size={18} />
        </Button>
        <h1 className="text-lg font-semibold flex-1">Scheduling links</h1>
        <Button variant="primary" onClick={() => setCreateOpen(true)}>
          <Icon name="plus" size={14} /> New link
        </Button>
      </header>

      <div className="flex-1 overflow-y-auto scroll-thin p-4">
        <p className="text-sm text-ink-soft mb-4 max-w-2xl">
          Share a link so others can book a slot on your calendar without the back-and-forth.
          Availability is defined per weekday; bookings create events in the linked calendar.
        </p>

        {q.isLoading && (
          <div className="space-y-2">
            {Array.from({ length: 2 }).map((_, i) => <Skeleton key={i} className="h-20 w-full" />)}
          </div>
        )}

        {!q.isLoading && (q.data?.length ?? 0) === 0 && (
          <EmptyState
            icon={<Icon name="clock" size={28} />}
            title="No scheduling links yet"
            description="Create a link to let others book time with you."
            action={<Button variant="primary" onClick={() => setCreateOpen(true)}>New link</Button>}
          />
        )}

        <ul className="space-y-2 max-w-3xl">
          {(q.data ?? []).map((l) => (
            <li key={l.id} className="p-3 border border-line rounded-lg">
              <div className="flex items-start gap-3">
                <div className="flex-1 min-w-0">
                  <p className="font-medium">/book/{l.slug}</p>
                  <p className="text-xs text-ink-soft">
                    {l.duration_minutes} min · {Object.keys(l.availability).length} day{Object.keys(l.availability).length === 1 ? '' : 's'} · {l.timezone}
                    {!l.active && ' · inactive'}
                  </p>
                  <button
                    onClick={() => { navigator.clipboard.writeText(baseUrl + l.slug); toast('success', 'Link copied'); }}
                    className="text-xs text-accent hover:underline mt-1"
                  >
                    Copy link
                  </button>
                </div>
                <Button size="sm" variant="ghost" onClick={() => setDeleteTarget(l)}>Delete</Button>
              </div>
            </li>
          ))}
        </ul>
      </div>

      <CreateLinkModal open={createOpen} onClose={() => setCreateOpen(false)} timezone={me?.timezone ?? 'Africa/Johannesburg'} />

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Link deleted');
            setDeleteTarget(null);
          } catch (e) {
            toast('error', 'Could not delete', errorMessage(e));
          }
        }}
        title="Delete this scheduling link?"
        description="Anyone with the link will get a 404. Existing bookings are unaffected."
        confirmLabel="Delete"
        danger
        busy={del.isPending}
      />
    </div>
  );
}

function CreateLinkModal({ open, onClose, timezone }: { open: boolean; onClose: () => void; timezone: string }) {
  const create = useCreateSchedulingLink();
  const calendarsQ = useCalendars();
  const [duration, setDuration] = useState(30);
  const [calendarId, setCalendarId] = useState(calendarsQ.data?.find((c) => c.kind === 'personal')?.id ?? '');
  const [availability, setAvailability] = useState<Availability>({
    '1': [{ start: '09:00', end: '12:00' }, { start: '13:00', end: '17:00' }],
    '2': [{ start: '09:00', end: '12:00' }, { start: '13:00', end: '17:00' }],
    '3': [{ start: '09:00', end: '12:00' }, { start: '13:00', end: '17:00' }],
    '4': [{ start: '09:00', end: '12:00' }, { start: '13:00', end: '17:00' }],
    '5': [{ start: '09:00', end: '12:00' }, { start: '13:00', end: '17:00' }],
  });
  const [tz, setTz] = useState(timezone);
  const [err, setErr] = useState<string | null>(null);

  function toggleDay(day: string) {
    const next = { ...availability };
    if (next[day]) delete next[day];
    else next[day] = [{ start: '09:00', end: '17:00' }];
    setAvailability(next);
  }

  async function submit() {
    setErr(null);
    if (!calendarId) { setErr('Pick a calendar'); return; }
    if (Object.keys(availability).length === 0) { setErr('Enable at least one day'); return; }
    try {
      await create.mutateAsync({
        duration_minutes: duration,
        availability,
        timezone: tz,
        calendar_id: calendarId,
        active: true,
      });
      toast('success', 'Scheduling link created');
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New scheduling link"
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={create.isPending}>Create link</Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Duration">
            <Select value={duration} onChange={(e) => setDuration(Number(e.target.value))}>
              <option value={15}>15 minutes</option>
              <option value={30}>30 minutes</option>
              <option value={45}>45 minutes</option>
              <option value={60}>60 minutes</option>
            </Select>
          </Field>
          <Field label="Calendar">
            <Select value={calendarId} onChange={(e) => setCalendarId(e.target.value)}>
              {calendarsQ.data?.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </Select>
          </Field>
        </div>

        <Field label="Timezone">
          <Input value={tz} onChange={(e) => setTz(e.target.value)} />
        </Field>

        <div>
          <p className="text-xs font-medium text-ink-soft mb-2">Availability</p>
          <ul className="space-y-1">
            {DAY_KEYS.map((day, idx) => {
              const active = !!availability[day];
              return (
                <li key={day} className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={active} onChange={() => toggleDay(day)} />
                  <span className="w-10 text-ink-soft">{WEEKDAY_LABELS[idx]}</span>
                  {active && availability[day].map((slot, si) => (
                    <span key={si} className="flex items-center gap-1 text-xs">
                      <Input
                        type="time"
                        value={slot.start}
                        onChange={(e) => {
                          const next = { ...availability };
                          next[day] = next[day].map((s, i) => i === si ? { ...s, start: e.target.value } : s);
                          setAvailability(next);
                        }}
                        className="w-[100px]"
                      />
                      –
                      <Input
                        type="time"
                        value={slot.end}
                        onChange={(e) => {
                          const next = { ...availability };
                          next[day] = next[day].map((s, i) => i === si ? { ...s, end: e.target.value } : s);
                          setAvailability(next);
                        }}
                        className="w-[100px]"
                      />
                    </span>
                  ))}
                </li>
              );
            })}
          </ul>
        </div>

        {err && <p className="text-sm text-danger">{err}</p>}
      </div>
    </Modal>
  );
}

