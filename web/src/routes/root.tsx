import { Link, Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { Button } from "../ui/Button";

/**
 * The root route.
 *
 * Routes are declared in code rather than generated from the filesystem: the
 * route tree is small, and a generated one would add a build step and a
 * watcher for no benefit at this size. Part 11-b revisits that if the tree
 * grows past what is readable in one file.
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
  const { t } = useTranslation();

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-4 p-6 text-center">
      <h1 className="text-2xl font-semibold">{t("errors.notFoundTitle")}</h1>
      <p className="text-content-muted">{t("errors.notFoundBody")}</p>

      <Button asChild>
        <Link to="/">{t("common.goToStart")}</Link>
      </Button>
    </main>
  );
}
