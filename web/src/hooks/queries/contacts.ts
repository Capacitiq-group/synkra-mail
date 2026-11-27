import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { useDebounce } from '@/hooks';
import type { Contact, ContactGroup } from '@/lib/types';

const contactsKey = (q?: string) => ['contacts', q ?? ''] as const;
const groupsKey = () => ['contact-groups'] as const;

export function useContacts(query = '') {
  const debounced = useDebounce(query.trim(), 250);
  return useQuery({
    queryKey: contactsKey(debounced),
    queryFn: () => api.listContacts(debounced || undefined),
    staleTime: 30_000,
  });
}

export function useContact(id: string) {
  return useQuery({
    queryKey: ['contact', id],
    queryFn: () => api.getContact(id),
    enabled: !!id,
  });
}

export function useCreateContact() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: Omit<Contact, 'id' | 'created_at' | 'updated_at'>) => api.createContact(c),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['contacts'] });
      qc.invalidateQueries({ queryKey: ['recipient-autocomplete'] });
    },
  });
}

export function useUpdateContact() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Contact> }) => api.updateContact(id, patch),
    onSuccess: (_d, vars) => {
      qc.invalidateQueries({ queryKey: ['contacts'] });
      qc.invalidateQueries({ queryKey: ['contact', vars.id] });
      qc.invalidateQueries({ queryKey: ['recipient-autocomplete'] });
    },
  });
}

export function useDeleteContact() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteContact(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['contacts'] });
      qc.invalidateQueries({ queryKey: ['recipient-autocomplete'] });
    },
  });
}

export function useImportContacts() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => api.importContacts(file),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['contacts'] }),
  });
}

export function useContactGroups() {
  return useQuery({ queryKey: groupsKey(), queryFn: api.listContactGroups, staleTime: 60_000 });
}

export function useCreateContactGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.createContactGroup(name),
    onSuccess: () => qc.invalidateQueries({ queryKey: groupsKey() }),
  });
}

export function useDeleteContactGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteContactGroup(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: groupsKey() });
      qc.invalidateQueries({ queryKey: ['contacts'] });
    },
  });
}

export type { Contact, ContactGroup };
