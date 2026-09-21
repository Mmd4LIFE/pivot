import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import { createQueryClient } from "./api/queries";
import { routeTree } from "./routes/tree";
import { ErrorBoundary } from "./components/ErrorBoundary";
import "./styles/app.css";

const queryClient = createQueryClient();

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

createRoot(container).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
