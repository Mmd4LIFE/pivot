import {
  MutationCache,
  QueryCache,
  QueryClient,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { ApiError, api, type SessionEnvelope } from "./client";

/**
 * The query client.
 *
 * Three defaults here are decisions rather than taste:
 *
 * A 401 is never retried. The session is gone, and asking three more times
 * cannot bring it back -- it only delays the redirect to the login page and
 * makes the interface feel broken rather than signed out.
 *
 * A 403 is never retried either. It is an answer, not a failure. A 503 *is*
 * retried, because the server draws that distinction deliberately: it means
 * the decision could not be reached, which is exactly the case where trying
 * again is reasonable.
 *
 * And every 401, from any query or mutation anywhere in the application, is
 * reported once to `onUnauthenticated`. Handling it per call site would mean
 * every future feature has to remember to; handling it here means a session
 * that ends mid-visit sends the user to the login page with their destination
 * intact, whatever they happened to be doing.
 */
export function createQueryClient(onUnauthenticated: () => void = () => {}): QueryClient {
  function report(error: unknown): void {
    if (error instanceof ApiError && error.isUnauthenticated) onUnauthenticated();
  }

  return new QueryClient({
    queryCache: new QueryCache({ onError: report }),
    mutationCache: new MutationCache({ onError: report }),

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
 * normal state for this query, and modeling it as an error would make every
 * consumer unwrap one. It also keeps the login page's own session probe from
 * tripping the global 401 handler and redirecting to itself.
 */
export const sessionQuery = {
  queryKey: keys.me,
  queryFn: async ({ signal }: { signal: AbortSignal }): Promise<SessionEnvelope | null> => {
    try {
      return await api.me(signal);
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) return null;
      throw error;
    }
  },
};

export function useSession() {
  return useQuery(sessionQuery);
}

/** The single sign-on buttons to offer. */
export function useAuthProviders() {
  return useQuery({
    queryKey: keys.providers,
    queryFn: ({ signal }: { signal: AbortSignal }) => api.providers(signal),
    staleTime: 5 * 60_000,
  });
}

export interface Credentials {
  email: string;
  password: string;
  organization?: string;
}

/**
 * Sign in.
 *
 * The whole cache is cleared on success, not just the session query. Anything
 * already fetched belongs to whoever was signed in before — on a shared
 * machine, leaving it would show one person another person's data until the
 * next refetch.
 */
export function useLogin() {
  const client = useQueryClient();

  return useMutation({
    mutationFn: ({ email, password, organization }: Credentials) =>
      api.login(email, password, organization),

    onSuccess: (envelope) => {
      client.clear();
      client.setQueryData(keys.me, envelope);
    },
  });
}

/**
 * Sign out.
 *
 * The cache is cleared even when the request fails. A failed logout still
 * means the user asked to leave, and the honest response to "I could not reach
 * the server" is to forget everything locally rather than keep showing data
 * they have asked to stop seeing.
 */
export function useLogout() {
  const client = useQueryClient();

  return useMutation({
    mutationFn: () => api.logout(),

    onSettled: () => {
      client.clear();
      client.setQueryData(keys.me, null);
    },
  });
}

/**
 * The messages a failed login can produce.
 *
 * A union rather than `string`, so adding a branch below without adding the
 * string to the catalog is a compile error instead of a key rendered onto the
 * page.
 */
export type LoginErrorKey =
  | "auth.errors.lockedOut"
  | "auth.errors.unavailable"
  | "auth.errors.invalidCredentials"
  | "auth.errors.unexpected"
  | "connection.offline";

/**
 * Which message a failed login deserves.
 *
 * Branches on the stable code, never on the message: the code's meaning is
 * fixed forever and the prose is not. Returns a translation key rather than a
 * string, so the caller decides the language.
 */
export function loginErrorKey(error: unknown): LoginErrorKey {
  if (error instanceof ApiError) {
    // Both are "too many attempts" from the user's side: one is the per-IP
    // limiter in front of the handler, the other is the per-account lockout
    // inside it. Telling them apart would tell an attacker which one they hit.
    if (error.status === 429) return "auth.errors.lockedOut";

    if (error.isUnavailable) return "auth.errors.unavailable";

    if (error.isUnauthenticated) return "auth.errors.invalidCredentials";

    return "auth.errors.unexpected";
  }

  // A NetworkError, or anything else that never reached the server.
  return "connection.offline";
}
