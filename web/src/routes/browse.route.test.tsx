import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { createQueryClient } from "../api/queries";
import { initI18n } from "../i18n";
import { routeTree } from "./tree";

/*
 * Browsing a connection renders at all.
 *
 * This exists because it did not: the page threw and the shell's error
 * boundary caught it, so the only thing a person saw was "Something went
 * wrong". A route with no test can be dead on arrival and every other test in
 * the suite still passes, which is exactly what happened.
 */

let replies: Record<string, { status: number; body?: unknown }> = {};

beforeEach(async () => {
  replies = {};

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const key = `${init?.method ?? "GET"} ${url}`;
      const reply = replies[key] ?? { status: 404, body: { error: { code: "PIVOT-REQ-003" } } };

      return new Response(reply.body === undefined ? null : JSON.stringify(reply.body), {
        status: reply.status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );

  await initI18n();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function signedIn() {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: true, tokenRequired: false },
  };

  replies["GET /api/v1/auth/me"] = {
    status: 200,
    body: {
      user: {
        id: "u1",
        organizationId: "acme",
        email: "a@b.c",
        name: "A",
        locale: "en",
        timezone: "UTC",
        isActive: true,
      },
      session: {
        id: "s1",
        issuedAt: "2026-09-29T08:00:00Z",
        expiresAt: "2026-09-29T16:00:00Z",
        absoluteExpiresAt: "2026-10-29T08:00:00Z",
        lastSeenAt: "2026-09-29T09:00:00Z",
        current: true,
      },
      permissions: ["native_query"],
    },
  };

  replies["GET /api/v1/connections"] = {
    status: 200,
    body: { connections: [{ id: "c1", slug: "w", name: "Warehouse", kind: "postgres" }] },
  };
}

function mount(path: string) {
  const queryClient = createQueryClient(() => {});

  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

test("the source list renders", async () => {
  signedIn();

  mount("/browse");

  expect(await screen.findByText("Warehouse")).toBeInTheDocument();
});

test("a connection's tables render", async () => {
  signedIn();

  replies["GET /api/v1/connections/c1/schema"] = {
    status: 200,
    body: {
      synced: true,
      tables: [{ schema: "demo", name: "invoices", columns: ["id", "amount"] }],
    },
  };

  mount("/browse/c1");

  expect(await screen.findByText("invoices")).toBeInTheDocument();
  expect(screen.getByText("demo")).toBeInTheDocument();
});

/*
 * An unsynced connection explains itself.
 *
 * An empty catalog and an empty database produce the same empty list, and only
 * one of them is worth telling somebody about.
 */
test("an unread connection names the command", async () => {
  signedIn();

  replies["GET /api/v1/connections/c1/schema"] = {
    status: 200,
    body: { synced: false, tables: [] },
  };

  mount("/browse/c1");

  expect(await screen.findByText(/has not been read yet/i)).toBeInTheDocument();
  expect(screen.getByText(/sync-catalog/)).toBeInTheDocument();
});
