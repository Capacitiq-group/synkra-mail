import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Calendar, CalendarEvent, SchedulingLink } from '@/lib/types';

export function useCalendars() {
  return useQuery({ queryKey: ['calendars'], queryFn: api.listCalendars, staleTime: 60_000 });
}

export function useCreateCalendar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: Omit<Calendar, 'id' | 'url'>) => api.createCalendar(c),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['calendars'] }),
  });
}

export function useUpdateCalendar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Calendar> }) => api.updateCalendar(id, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['calendars'] }),
  });
}

export function useDeleteCalendar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteCalendar(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['calendars'] }),
  });
}

/** Fetch events across every visible calendar for a date range. */
export function useEventsForRange(calendarIds: string[], from: string, to: string) {
  const enabled = calendarIds.length > 0 && !!from && !!to;
  return useQuery({
    queryKey: ['events', calendarIds.slice().sort(), from, to],
    queryFn: async () => {
      const results = await Promise.all(
        calendarIds.map((id) => api.listEvents(id, from, to).catch(() => [] as CalendarEvent[])),
      );
      return results.flat();
    },
    enabled,
    staleTime: 15_000,
  });
}

export function useCreateEvent(calendarId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ev: Omit<CalendarEvent, 'id' | 'organizer' | 'rsvp' | 'status'>) => api.createEvent(calendarId, ev),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['events'] }),
  });
}

export function useUpdateEvent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<CalendarEvent> }) => api.updateEvent(id, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['events'] }),
  });
}

export function useDeleteEvent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteEvent(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['events'] }),
  });
}

export function useRsvpEvent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, response, message }: { id: string; response: 'accepted' | 'declined' | 'tentative'; message?: string }) =>
      api.rsvpEvent(id, response, message),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['events'] }),
  });
}

export function useSchedulingLinks() {
  return useQuery({ queryKey: ['scheduling-links'], queryFn: api.schedulingLinks, staleTime: 60_000 });
}

export function useCreateSchedulingLink() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<SchedulingLink, 'id' | 'slug'>) => api.createSchedulingLink(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['scheduling-links'] }),
  });
}

export function useDeleteSchedulingLink() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteSchedulingLink(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['scheduling-links'] }),
  });
}

export function useMessageCalendarEvent(messageId: string | undefined) {
  return useQuery({
    queryKey: ['message-calendar-event', messageId ?? ''],
    queryFn: () => api.getMessageCalendarEvent(messageId!),
    enabled: !!messageId,
    staleTime: 60_000,
  });
}
