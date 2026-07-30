import { QueryClient } from "@tanstack/react-query";
import type { Revision } from "../boundary/contract";

export const queryKeys = {
  snapshot: (revision: Revision) => ["snapshot", revision] as const,
  schema: () => ["schema"] as const,
  peaks: (revision: Revision, voiceId: string) => ["peaks", revision, voiceId] as const,
};

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
        refetchOnMount: false,
        staleTime: Infinity,
        retry: false,
      },
    },
  });
}
