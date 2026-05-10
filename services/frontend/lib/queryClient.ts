import { QueryClient } from '@tanstack/react-query';

import { STALE_TIME_1_MIN } from '@/lib/constants/cacheTTL';

// Module-scope singleton so non-React modules (like the axios 404 interceptor)
// can call queryClient.invalidateQueries. CONTEXT D-16 + 02-PATTERNS.md
// §`services/frontend/lib/api.ts (add 404 interceptor)`.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: STALE_TIME_1_MIN, // 60s — matches the existing inline default
      retry: 1,
    },
  },
});
