import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createQueryClient } from "../api/queries";
import { initI18n } from "../i18n";
import { routeTree } from "./tree";

/*
 * The first run, in the browser.
 *
 * The behaviour worth testing here is the routing, not the form: a Pivot with
 * no accounts must not show a login page that no password can satisfy, and a
 * Pivot that has been set up must not show a setup page that cannot do
 * anything. Both are dead ends somebody hits on their first five minutes with
 * the product.
 *
 * The router and the API client are real; only `fetch` is replaced.
 */

interface Reply {
  status: number;
  body?: unknown;
}

let replies: Record<string, Reply> = {};
let calls: string[] = [];

beforeEach(async () => {
  replies = {};
  calls = [];

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const key = `${init?.method ?? "GET"} ${url}`;

      calls.push(key);

      const reply = replies[key] ?? { status: 404, body: { error: { code: "PIVOT-REQ-003" } } };

      // `null`, not `""`. A 204 is a null-body status and the Response
      // constructor throws when given one -- which surfaces as a NetworkError
      // from the client and makes every successful 204 look like the server
      // being unreachable. Cost an hour once.
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

const SESSION = {
  user: {
    id: "u1",
    organizationId: "o1",
    email: "ada@example.com",
    name: "Ada Lovelace",
    locale: "en",
    timezone: "UTC",
    isActive: true,
  },
  session: {
    id: "s1",
    issuedAt: "2026-09-23T00:00:00Z",
    expiresAt: "2026-09-24T00:00:00Z",
    absoluteExpiresAt: "2026-09-30T00:00:00Z",
    lastSeenAt: "2026-09-23T00:00:00Z",
    current: true,
  },
  permissions: ["view_content"],
};

/** An unclaimed Pivot: no accounts, and it wants the token. */
function unclaimed() {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: false, tokenRequired: true },
  };
  replies["GET /api/v1/auth/me"] = { status: 401, body: { error: { code: "PIVOT-AUTH-001" } } };
  replies["GET /api/v1/auth/providers"] = { status: 200, body: { providers: [] } };
}

function claimed() {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: true, tokenRequired: false },
  };
  replies["GET /api/v1/auth/me"] = { status: 401, body: { error: { code: "PIVOT-AUTH-001" } } };
  replies["GET /api/v1/auth/providers"] = { status: 200, body: { providers: [] } };
}

function mountAt(path: string) {
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

  return { router, queryClient };
}

describe("an instance nobody has claimed", () => {
  /*
   * The dead end this exists to prevent. Somebody downloads Pivot, runs it,
   * opens a browser, and is shown a login form for an instance with no
   * accounts -- with nothing on the page saying so.
   */
  test("sends somebody at the login page to setup instead", async () => {
    unclaimed();

    const { router } = mountAt("/login");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/setup");
    });

    expect(await screen.findByRole("heading", { name: /set up pivot/i })).toBeTruthy();
  });

  test("asks for the token when the server says it needs one", async () => {
    unclaimed();

    mountAt("/setup");

    expect(await screen.findByLabelText(/setup token/i)).toBeTruthy();
  });

  test("does not ask for a token when the server does not need one", async () => {
    unclaimed();
    replies["GET /api/v1/setup/status"] = {
      status: 200,
      body: { initialized: false, tokenRequired: false },
    };

    mountAt("/setup");

    await screen.findByRole("heading", { name: /set up pivot/i });

    expect(screen.queryByLabelText(/setup token/i)).toBeNull();
  });

  test("claiming it lands in the application, signed in", async () => {
    unclaimed();
    replies["POST /api/v1/setup"] = { status: 201, body: SESSION };

    const { router } = mountAt("/setup");

    const user = userEvent.setup();

    await user.type(await screen.findByLabelText(/organization name/i), "Acme Analytics");
    await user.type(screen.getByLabelText(/your name/i), "Ada Lovelace");
    await user.type(screen.getByLabelText(/email address/i), "ada@example.com");
    await user.type(screen.getByLabelText(/^password/i), "a-long-enough-password");
    await user.type(screen.getByLabelText(/setup token/i), "the-token");

    await user.click(screen.getByRole("button", { name: /create administrator/i }));

    // Not the login page. The password was just typed twice; asking for it a
    // third time is a step that should not exist.
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });

    expect(calls).toContain("POST /api/v1/setup");
    expect(calls).not.toContain("POST /api/v1/auth/login");
  });

  test("explains a refused token rather than failing silently", async () => {
    unclaimed();
    replies["POST /api/v1/setup"] = {
      status: 403,
      body: { error: { code: "PIVOT-SETUP-002", message: "nope" } },
    };

    mountAt("/setup");

    const user = userEvent.setup();

    await user.type(await screen.findByLabelText(/organization name/i), "Acme");
    await user.type(screen.getByLabelText(/email address/i), "ada@example.com");
    await user.type(screen.getByLabelText(/^password/i), "a-long-enough-password");
    await user.type(screen.getByLabelText(/setup token/i), "wrong");

    await user.click(screen.getByRole("button", { name: /create administrator/i }));

    expect(await screen.findByText(/setup token is not right/i)).toBeTruthy();
  });

  test("says so when somebody else claimed it first", async () => {
    unclaimed();
    replies["POST /api/v1/setup"] = {
      status: 409,
      body: { error: { code: "PIVOT-SETUP-001", message: "nope" } },
    };

    mountAt("/setup");

    const user = userEvent.setup();

    await user.type(await screen.findByLabelText(/organization name/i), "Acme");
    await user.type(screen.getByLabelText(/email address/i), "ada@example.com");
    await user.type(screen.getByLabelText(/^password/i), "a-long-enough-password");
    await user.type(screen.getByLabelText(/setup token/i), "the-token");

    await user.click(screen.getByRole("button", { name: /create administrator/i }));

    expect(await screen.findByText(/already has an administrator/i)).toBeTruthy();
  });
});

describe("an instance that is already set up", () => {
  // The other dead end: a form that cannot do anything, on a page somebody
  // reached from a bookmark or a stale tab.
  test("sends somebody at the setup page to the login page", async () => {
    claimed();

    const { router } = mountAt("/setup");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });
  });

  test("leaves the login page alone", async () => {
    claimed();

    const { router } = mountAt("/login");

    expect(await screen.findByRole("heading", { name: /sign in to pivot/i })).toBeTruthy();
    expect(router.state.location.pathname).toBe("/login");
  });
});

/*
 * A probe that fails must not take the login page with it.
 *
 * "We could not check whether this instance is set up" is not a reason to stop
 * somebody signing in, and a guard that treats every error as "not set up"
 * would send an entire company to a setup page during a blip.
 */
test("a failed status probe leaves the login page working", async () => {
  replies["GET /api/v1/setup/status"] = { status: 503, body: { error: { code: "PIVOT-SRV-002" } } };
  replies["GET /api/v1/auth/me"] = { status: 401, body: { error: { code: "PIVOT-AUTH-001" } } };
  replies["GET /api/v1/auth/providers"] = { status: 200, body: { providers: [] } };

  const { router } = mountAt("/login");

  expect(await screen.findByRole("heading", { name: /sign in to pivot/i })).toBeTruthy();
  expect(router.state.location.pathname).toBe("/login");
});
