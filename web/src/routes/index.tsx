import { createRoute } from "@tanstack/react-router";
import { Route as rootRoute } from "./root";
import { useAuthProviders, useSession } from "../api/queries";
import { ThemeToggle } from "../components/ThemeToggle";

/**
 * The start page.
 *
 * Deliberately thin. Part 11 replaces it with the real login page and the
 * application shell; what it proves today is that the whole chain works —
 * bundle loads, router mounts, query layer reaches the Go API, tokens style
 * it, and the session endpoint answers.
 */
export const Route = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Home,
});

function Home() {
  const session = useSession();
  const providers = useAuthProviders();

  return (
    <main className="mx-auto flex min-h-screen max-w-2xl flex-col gap-8 p-6">
      <header className="flex items-center justify-between border-b border-line pb-4">
        <div>
          <h1 className="text-2xl font-semibold">Pivot</h1>
          <p className="text-sm text-content-muted">
            The open business intelligence platform for the AI era.
          </p>
        </div>
        <ThemeToggle />
      </header>

      <section className="rounded-token border border-line bg-surface p-4 shadow-token-sm">
        <h2 className="mb-2 text-sm font-medium text-content-muted">Session</h2>

        {session.isPending && <p className="text-content-subtle">Checking…</p>}

        {session.isError && (
          <p className="text-danger">
            Could not reach the API: {String(session.error)}
          </p>
        )}

        {session.isSuccess && session.data === null && (
          <p className="text-content-muted">Not signed in.</p>
        )}

        {session.isSuccess && session.data !== null && (
          <div className="flex flex-col gap-1">
            <p className="font-medium">{session.data.user.name || session.data.user.email}</p>
            <p className="text-sm text-content-muted">{session.data.user.email}</p>
            {session.data.permissions && session.data.permissions.length > 0 && (
              <p className="mt-2 font-mono text-xs text-content-subtle">
                {session.data.permissions.join(" · ")}
              </p>
            )}
          </div>
        )}
      </section>

      <section className="rounded-token border border-line bg-surface p-4 shadow-token-sm">
        <h2 className="mb-2 text-sm font-medium text-content-muted">Single sign-on</h2>

        {providers.isSuccess && providers.data.providers.length === 0 && (
          <p className="text-content-muted">No identity providers are configured.</p>
        )}

        {providers.isSuccess && providers.data.providers.length > 0 && (
          <ul className="flex flex-col gap-2">
            {providers.data.providers.map((provider) => (
              <li key={provider.slug}>
                <a
                  className="inline-block rounded-token border border-line-strong px-3 py-2 hover:bg-surface-sunken"
                  href={`/api/v1/auth/oidc/${encodeURIComponent(provider.slug)}/start`}
                >
                  Continue with {provider.name}
                </a>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
