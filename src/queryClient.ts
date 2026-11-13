import { QueryClient } from "@tanstack/react-query";

/** createQueryClient returns the app's QueryClient. Data comes from the local Go process, not a network: refetching on focus would only repeat work, and mutations (running maintenance, changing the system) must never be retried behind the user's back. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
}
