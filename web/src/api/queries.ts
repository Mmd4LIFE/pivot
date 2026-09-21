import { QueryClient, useQuery } from "@tanstack/react-query";
import { ApiError, api } from "./client";

/**
 * The query client.
 *
 * Two defaults here are decisions rather than taste:
 *
 * A 401 is never retried. The session is gone, and asking three more times
 * cannot bring it back -- it only delays the redirect to the login page and
 * makes the interface feel broken rather than signed out.
 *
 * A 403 is never retried either. It is an answer, not a failure. A 503 *is*
 * retried, because the server draws that distinction deliberately: it means
 * the decision could not be reached, which is exactly the case where trying
 * again is reasonable.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failureCount, error) => {
          if (error instanceof ApiError) {
            if (error.isUnauthenticated || error.isForbidden) return false;
          }

          return failureCount < 2;
        },
        staleTime: 30_000,
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

/** Query keys, in one place so an invalidation cannot miss one by typo. */
export const keys = {
  me: ["auth", "me"] as const,
  providers: ["auth", "providers"] as const,
};

/**
 * The signed-in user, or null.
 *
 * A 401 resolves to null rather than throwing: "nobody is signed in" is a
 * normal state for this query, and modelling it as an error would make every
 * consumer unwrap one.
 */
export function useSession() {
  return useQuery({
    queryKey: keys.me,
    queryFn: async ({ signal }) => {
      try {
        return await api.me(signal);
      } catch (error) {
        if (error instanceof ApiError && error.isUnauthenticated) return null;
        throw error;
      }
    },
  });
}

/** The single sign-on buttons to offer. */
export function useAuthProviders() {
  return useQuery({
    queryKey: keys.providers,
    queryFn: ({ signal }) => api.providers(signal),
    staleTime: 5 * 60_000,
  });
}
