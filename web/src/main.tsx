import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import { createQueryClient, keys } from "./api/queries";
import { routeTree } from "./routes/tree";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { initI18n } from "./i18n";
import { currentDestination } from "./lib/redirect";
import "./styles/app.css";

/*
 * A session that ends mid-visit.
 *
 * Every 401 from anywhere in the application arrives here, because the query
 * client reports them centrally rather than leaving each call site to
 * remember. Three things happen, in order:
 *
 *   the cached session becomes null, so the route guard agrees with the server
 *   the user goes to the login page, carrying where they were
 *   `expired` says why, so the page explains rather than just appearing
 *
 * Without the first step the guard would read a stale session, let the user
 * back in, and bounce them again on the next request.
 */
function handleSessionEnded(): void {
  const location = router.state.location;

  if (location.pathname === "/login") return;

  queryClient.setQueryData(keys.me, null);

  void router.navigate({
    to: "/login",
    search: { redirect: currentDestination(location), expired: true },
    replace: true,
  });
}

const queryClient = createQueryClient(handleSessionEnded);

const router = createRouter({
  routeTree,
  context: { queryClient },

  // The Go server already sent index.html for this path (the SPA fallback), so
  // the router is what decides whether the route exists.
  defaultPreload: "intent",
});

// Type-safe routes: this is what makes a typo in a `to=` prop a compile error
// rather than a broken link found in production.
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const container = document.getElementById("root");

if (container === null) {
  throw new Error("index.html has no #root element to mount into");
}

/*
 * Translations are loaded before the first render rather than suspended on.
 * The alternative is a frame of untranslated keys, and a page that visibly
 * rewrites itself on load looks broken in a way a few milliseconds does not.
 * The catalog is bundled, so this resolves in a microtask.
 *
 * A `.then` and not a top-level `await`: the build targets es2020, where
 * top-level await does not exist, and raising the target to get one line of
 * syntax would quietly drop browsers the NFRs still name.
 */
void initI18n().then(() => {
  createRoot(container).render(
    <StrictMode>
      <ErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </ErrorBoundary>
    </StrictMode>,
  );
});
