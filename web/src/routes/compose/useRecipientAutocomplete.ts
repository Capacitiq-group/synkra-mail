import { useQuery } from '@tanstack/react-query';
import { useDebounce } from '@/hooks';
import { api } from '@/lib/api';

export interface AutocompleteEntry {
  email: string;
  name?: string;
  source: 'personal' | 'org' | 'recent';
}

export function useRecipientAutocomplete(query: string): {
  results: AutocompleteEntry[];
  loading: boolean;
} {
  const debounced = useDebounce(query.trim(), 200);
  const q = useQuery({
    queryKey: ['recipient-autocomplete', debounced],
    queryFn: () => api.autocompleteContacts(debounced),
    enabled: debounced.length >= 2,
    staleTime: 60_000,
  });
  return { results: q.data ?? [], loading: q.isLoading };
}
