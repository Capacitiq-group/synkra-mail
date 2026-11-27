import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useCreateEvent, useDeleteEvent, useUpdateEvent } from '@/hooks/queries/calendar';
import { Button, Checkbox, ConfirmDialog, Field, Input, Modal, Select, Textarea } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { combineDateTime, toDateInputValue, toTimeInputValue } from '@/lib/dates';
import type { Address, Calendar, CalendarEvent } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  event?: CalendarEvent;
  defaults?: { start: Date; end: Date };
  calendars: Calendar[];
  defaultCalendarId: string;
}

export function EventModal({ open, onClose, event, defaults, calendars, defaultCalendarId }: Props) {
  const qc = useQueryClient();
  const createMut = useCreateEvent(defaultCalendarId);
  const updateMut = useUpdateEvent();
  const deleteMut = useDeleteEvent();

  const initial = event ?? null;
  const start = initial ? new Date(initial.starts_at) : (defaults?.start ?? new Date());
  const end = initial ? new Date(initial.ends_at) : (defaults?.end ?? new Date(Date.now() + 3600_000));

  const [calendarId, setCalendarId] = useState(initial?.calendar_id ?? defaultCalendarId);
  const [title, setTitle] = useState(initial?.title ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [location, setLocation] = useState(initial?.location ?? '');
  const [dateStr, setDateStr] = useState(toDateInputValue(start));
  const [startTime, setStartTime] = useState(toTimeInputValue(start));
  const [endTime, setEndTime] = useState(toTimeInputValue(end));
  const [endDateStr, setEndDateStr] = useState(toDateInputValue(end));
  const [allDay, setAllDay] = useState(initial?.all_day ?? false);
  const [attendeesText, setAttendeesText] = useState((initial?.attendees ?? []).map((a) => a.email).join(', '));
  const [reminders, setReminders] = useState<number[]>(initial?.reminders_minutes ?? [15]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (open && event) {
      setCalendarId(event.calendar_id);
      setTitle(event.title);
      setDescription(event.description ?? '');
      setLocation(event.location ?? '');
      setDateStr(toDateInputValue(event.starts_at));
      setStartTime(toTimeInputValue(event.starts_at));
      setEndTime(toTimeInputValue(event.ends_at));
      setEndDateStr(toDateInputValue(event.ends_at));
      setAllDay(event.all_day);
      setAttendeesText(event.attendees.map((a) => a.email).join(', '));
      setReminders(event.reminders_minutes);
    } else if (open) {
      setCalendarId(defaultCalendarId);
      setTitle('');
      setDescription('');
      setLocation('');
      const s = defaults?.start ?? new Date();
      const e = defaults?.end ?? new Date(Date.now() + 3600_000);
      setDateStr(toDateInputValue(s));
      setStartTime(toTimeInputValue(s));
      setEndTime(toTimeInputValue(e));
      setEndDateStr(toDateInputValue(e));
      setAllDay(false);
      setAttendeesText('');
      setReminders([15]);
    }
    setErr(null);
    setConfirmDelete(false);
  }, [open, event?.id, defaults?.start?.toISOString(), defaults?.end?.toISOString(), defaultCalendarId]); // eslint-disable-line

  async function save() {
    setErr(null);
    if (!title.trim()) { setErr('Title is required'); return; }
    const startsAt = allDay
      ? combineDateTime(dateStr, '00:00')
      : combineDateTime(dateStr, startTime);
    const endsAt = allDay
      ? combineDateTime(endDateStr, '23:59')
      : combineDateTime(endDateStr, endTime);
    if (new Date(endsAt) <= new Date(startsAt)) { setErr('End must be after start'); return; }

    const attendees: Address[] = attendeesText
      .split(/[,;\s]+/)
      .map((s) => s.trim())
      .filter(Boolean)
      .map((email) => ({ email }));

    const payload = {
      calendar_id: calendarId,
      title: title.trim(),
      description: description.trim() || undefined,
      location: location.trim() || undefined,
      starts_at: startsAt,
      ends_at: endsAt,
      all_day: allDay,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      attendees,
      reminders_minutes: reminders,
      organizer: initial?.organizer ?? { email: '', name: '' },
      rrule: initial?.rrule,
    };

    try {
      if (initial) {
        await updateMut.mutateAsync({ id: initial.id, patch: payload as Partial<CalendarEvent> });
        toast('success', 'Event updated');
      } else {
        await createMut.mutateAsync(payload as Omit<CalendarEvent, 'id' | 'organizer' | 'rsvp' | 'status'>);
        toast('success', 'Event created');
      }
      qc.invalidateQueries({ queryKey: ['events'] });
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    }
  }

  async function doDelete() {
    if (!initial) return;
    try {
      await deleteMut.mutateAsync(initial.id);
      qc.invalidateQueries({ queryKey: ['events'] });
      toast('success', 'Event deleted');
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={initial ? 'Edit event' : 'New event'}
      size="lg"
      footer={
        <>
          {initial && (
            <Button variant="ghost" onClick={() => setConfirmDelete(true)} className="mr-auto text-danger">
              <Icon name="trash" size={14} /> Delete
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={save} loading={createMut.isPending || updateMut.isPending}>
            {initial ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Title" required>
          <Input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus placeholder="Event title" />
        </Field>

        <Field label="Calendar">
          <Select value={calendarId} onChange={(e) => setCalendarId(e.target.value)}>
            {calendars.map((c) => (
              <option key={c.id} value={c.id}>{c.name}{c.read_only ? ' (read-only)' : ''}</option>
            ))}
          </Select>
        </Field>

        <Checkbox label="All day" checked={allDay} onChange={(e) => setAllDay(e.target.checked)} />

        <div className="grid grid-cols-2 gap-2">
          <Field label="Start date">
            <Input type="date" value={dateStr} onChange={(e) => setDateStr(e.target.value)} />
          </Field>
          {!allDay && (
            <Field label="Start time">
              <Input type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
            </Field>
          )}
        </div>
        <div className="grid grid-cols-2 gap-2">
          <Field label="End date">
            <Input type="date" value={endDateStr} onChange={(e) => setEndDateStr(e.target.value)} />
          </Field>
          {!allDay && (
            <Field label="End time">
              <Input type="time" value={endTime} onChange={(e) => setEndTime(e.target.value)} />
            </Field>
          )}
        </div>

        <Field label="Location">
          <Input value={location} onChange={(e) => setLocation(e.target.value)} placeholder="Office, address, or video link" />
        </Field>

        <Field label="Attendees" hint="Comma-separated email addresses. They receive an invitation.">
          <Input value={attendeesText} onChange={(e) => setAttendeesText(e.target.value)} placeholder="alice@example.com, bob@example.com" />
        </Field>

        <Field label="Description">
          <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </Field>

        <Field label="Reminders">
          <div className="flex flex-wrap gap-2">
            {[0, 5, 15, 30, 60, 1440].map((m) => {
              const active = reminders.includes(m);
              return (
                <button
                  key={m}
                  type="button"
                  onClick={() => setReminders(active ? reminders.filter((x) => x !== m) : [...reminders, m])}
                  className={`px-2 py-1 text-xs rounded border ${
                    active ? 'bg-accent-soft border-accent text-accent' : 'border-line'
                  }`}
                >
                  {m === 0 ? 'At start' : m === 1440 ? '1 day' : m < 60 ? `${m} min` : `${m / 60}h`}
                </button>
              );
            })}
          </div>
        </Field>

        {err && <p className="text-sm text-danger">{err}</p>}
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onCancel={() => setConfirmDelete(false)}
        onConfirm={doDelete}
        title="Delete this event?"
        description="Attendees will be notified of the cancellation."
        confirmLabel="Delete event"
        danger
        busy={deleteMut.isPending}
      />
    </Modal>
  );
}
