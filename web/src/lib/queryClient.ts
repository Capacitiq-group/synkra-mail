import { QueryClient } from '@tanstack/react-query';
import { ApiError } from './errors';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 20_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
      retry: (failures, err) => {
        if (err instanceof ApiError) {
          if (err.status === 401 || err.status === 403) return false;
          if (err.status >= 400 && err.status < 500) return false;
        }
        return failures < 2;
      },
    },
    mutations: { retry: 0 },
  },
});
