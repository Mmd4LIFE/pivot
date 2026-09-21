import { Link, Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

/**
 * The root route.
 *
 * Routes are declared in code rather than generated from the filesystem: the
 * route tree is small, and a generated one would add a build step and a
 * watcher for no benefit at this size. Part 11 revisits that if the tree grows
 * past what is readable in one file.
 */
export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
});

function RootLayout() {
  return (
    <div className="min-h-screen bg-canvas text-content">
      <Outlet />
    </div>
  );
}

/**
 * A client-side 404.
 *
 * Reachable because the Go server sends index.html for any unknown path that
 * does not look like a file — the SPA fallback. The router is what decides the
 * path is unknown, so this is where the message belongs.
 */
function NotFound() {
  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-4 p-6 text-center">
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <p className="text-content-muted">
        That address does not match anything in Pivot.
      </p>
      <Link
        to="/"
        className="rounded-token bg-accent px-4 py-2 font-medium text-accent-content hover:bg-accent-hover"
      >
        Go to the start
      </Link>
    </main>
  );
}
