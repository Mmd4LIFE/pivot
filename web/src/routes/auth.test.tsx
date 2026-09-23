import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createQueryClient, keys } from "../api/queries";
import { initI18n } from "../i18n";
import { currentDestination, safeDestination } from "../lib/redirect";
import { routeTree } from "./tree";

/*
 * The session boundary.
 *
 * Three things are checked here, and each one is a real defect if it breaks:
 * that a signed-out visitor cannot see a protected page, that they come back
 * to where they were going, and that "where they were going" cannot be turned
 * into somebody else's website.
 *
 * The router and the API client are the real ones; only `fetch` is replaced.
 * Stubbing the client instead would test a mock's idea of a 401 rather than
 * the envelope the Go server actually sends.
 */

interface Reply {
  status: number;
  body?: unknown;
}

/** The canned responses, by "METHOD /path". */
let replies: Record<string, Reply> = {};

/** Every request the application made, in order. */
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

      const reply = replies[key] ?? { status: 404, body: { error: { code: "PIVOT-SRV-404" } } };

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
    issuedAt: "2026-09-22T00:00:00Z",
    expiresAt: "2026-09-23T00:00:00Z",
    absoluteExpiresAt: "2026-09-29T00:00:00Z",
    lastSeenAt: "2026-09-22T00:00:00Z",
    current: true,
  },
  permissions: ["view_content"],
};

/** Mount the real application at a given URL. */
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

/** An instance that has been set up. Every test here assumes one. */
function initialized() {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: true, tokenRequired: false },
  };
}

/** The signed-out case, which is most of these tests. */
function signedOut() {
  initialized();
  replies["GET /api/v1/auth/me"] = { status: 401, body: { error: { code: "PIVOT-AUTH-001" } } };
  replies["GET /api/v1/auth/providers"] = { status: 200, body: { providers: [] } };
}

function signedIn() {
  initialized();
  replies["GET /api/v1/auth/me"] = { status: 200, body: SESSION };
  replies["GET /api/v1/auth/providers"] = { status: 200, body: { providers: [] } };
}

describe("safeDestination", () => {
  test("keeps a path inside Pivot", () => {
    expect(safeDestination("/dashboards/7?tab=sql")).toBe("/dashboards/7?tab=sql");
    expect(safeDestination("/a#b")).toBe("/a#b");
  });

  test("refuses anything that leaves the origin", () => {
    // The whole point. Each of these is a working off-site redirect in some
    // browser if only the leading slash is checked.
    for (const hostile of [
      "https://evil.example",
      "//evil.example",
      "/\\evil.example",
      "javascript:alert(1)",
      "evil.example",
      "/ok\r\nLocation: https://evil.example",
    ]) {
      expect(safeDestination(hostile)).toBe("/");
    }
  });

  test("refuses to bounce back to the login page", () => {
    expect(safeDestination("/login")).toBe("/");
    expect(safeDestination("/login?redirect=%2F")).toBe("/");
  });

  test("falls back for anything that is not a string", () => {
    expect(safeDestination(undefined)).toBe("/");
    expect(safeDestination(42)).toBe("/");
    expect(safeDestination("")).toBe("/");
  });
});

describe("currentDestination", () => {
  test("keeps the query and the fragment", () => {
    // A user sent to sign in from a filtered table comes back to the same
    // filtered table, not its unfiltered default.
    expect(
      currentDestination({ pathname: "/dashboards", searchStr: "?owner=me", hash: "#chart" }),
    ).toBe("/dashboards?owner=me#chart");
  });

  test("takes the fragment with or without its hash", () => {
    // The router's parsed location omits the leading `#` and
    // `window.location.hash` includes it. Concatenating the first one blindly
    // produced `/?owner=mechart` -- a destination that still navigates, and
    // quietly goes somewhere else.
    expect(currentDestination({ pathname: "/", hash: "chart" })).toBe("/#chart");
    expect(currentDestination({ pathname: "/", hash: "#chart" })).toBe("/#chart");
  });

  test("survives a location with neither", () => {
    expect(currentDestination({ pathname: "/" })).toBe("/");
    expect(currentDestination({ pathname: "/", hash: "" })).toBe("/");
  });
});

describe("the guard", () => {
  test("sends a signed-out visitor to the login page", async () => {
    signedOut();

    const { router } = mountAt("/");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });

    expect(await screen.findByRole("heading", { name: /sign in/i })).toBeTruthy();
  });

  test("remembers where they were going, query and fragment included", async () => {
    signedOut();

    const { router } = mountAt("/?owner=me#chart");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });

    // Not just the path. Somebody sent to sign in from a filtered view comes
    // back to the same filtered view.
    expect(router.state.location.search).toMatchObject({
      redirect: "/?owner=me#chart",
    });
  });

  test("a path that does not exist is a 404, signed in or not", async () => {
    signedOut();

    mountAt("/nothing-here");

    // Deliberate: the guard hangs off a layout route, and an address that
    // matches no route never reaches it. Answering "sign in" for a URL that
    // will still not exist afterwards wastes the user's time, and the 404 page
    // reveals nothing -- it is the same page either way.
    expect(await screen.findByRole("heading", { name: /page not found/i })).toBeTruthy();
  });

  test("lets a signed-in user through, and asks the server once", async () => {
    signedIn();

    const { router } = mountAt("/");

    expect(await screen.findByText(/ada@example.com/)).toBeTruthy();
    expect(router.state.location.pathname).toBe("/");

    // The guard resolves the session through the query cache, so the page
    // underneath reuses it rather than asking again.
    expect(calls.filter((c) => c === "GET /api/v1/auth/me")).toHaveLength(1);
  });

  test("keeps a signed-in user off the login page", async () => {
    signedIn();

    const { router } = mountAt("/login?redirect=%2F");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  test("will not honor a hostile destination even from the login page", async () => {
    signedIn();

    const { router } = mountAt("/login?redirect=https%3A%2F%2Fevil.example");

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });

    expect(router.state.location.href).not.toContain("evil.example");
  });
});

describe("signing in", () => {
  test("submits the form and lands on the remembered destination", async () => {
    const user = userEvent.setup();

    signedOut();
    replies["POST /api/v1/auth/login"] = { status: 200, body: SESSION };

    const { router } = mountAt("/login?redirect=%2F");

    await screen.findByRole("heading", { name: /sign in/i });

    await user.type(screen.getByLabelText(/email/i), "ada@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2hunter2");

    // The button, not a synthetic submit: this also proves the form is a form.
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  test("says the same thing for a wrong password and an unknown address", async () => {
    const user = userEvent.setup();

    signedOut();
    replies["POST /api/v1/auth/login"] = {
      status: 401,
      body: { error: { code: "PIVOT-AUTH-001", message: "Incorrect email or password" } },
    };

    mountAt("/login");

    await screen.findByRole("heading", { name: /sign in/i });

    await user.type(screen.getByLabelText(/email/i), "nobody@example.com");
    await user.type(screen.getByLabelText(/password/i), "wrong");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    // Announced, because the user has just acted and may not be looking here.
    const alert = await screen.findByRole("alert");

    expect(alert.textContent).toContain("do not match");
  });

  test("tells the user to wait after too many attempts", async () => {
    const user = userEvent.setup();

    signedOut();
    replies["POST /api/v1/auth/login"] = {
      status: 429,
      body: { error: { code: "PIVOT-AUTH-005" } },
    };

    mountAt("/login");

    await screen.findByRole("heading", { name: /sign in/i });

    await user.type(screen.getByLabelText(/email/i), "ada@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2hunter2");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    const alert = await screen.findByRole("alert");

    expect(alert.textContent).toContain("Too many attempts");
  });

  test("distinguishes an outage from a rejection", async () => {
    const user = userEvent.setup();

    signedOut();
    replies["POST /api/v1/auth/login"] = {
      status: 503,
      body: { error: { code: "PIVOT-SRV-503" } },
    };

    mountAt("/login");

    await screen.findByRole("heading", { name: /sign in/i });

    await user.type(screen.getByLabelText(/email/i), "ada@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2hunter2");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    const alert = await screen.findByRole("alert");

    // 503 is "no decision could be reached", so the advice is to retry rather
    // than to go and argue with an administrator.
    expect(alert.textContent).toContain("Try again shortly");
  });

  test("reveals the organization field only when the server asks for it", async () => {
    const user = userEvent.setup();

    signedOut();
    replies["POST /api/v1/auth/login"] = {
      status: 422,
      body: {
        error: {
          code: "PIVOT-VAL-001",
          details: [{ field: "organization", message: "is required" }],
        },
      },
    };

    mountAt("/login");

    await screen.findByRole("heading", { name: /sign in/i });

    // A single-organization instance never shows it, so it is absent until the
    // first attempt comes back.
    expect(screen.queryByLabelText(/organization/i)).toBeNull();

    await user.type(screen.getByLabelText(/email/i), "ada@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2hunter2");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    const organization = await screen.findByLabelText(/organization/i);

    await waitFor(() => {
      expect(organization).toHaveFocus();
    });
  });
});

describe("signing out", () => {
  test("returns to the login page and forgets the session", async () => {
    const user = userEvent.setup();

    signedIn();
    replies["POST /api/v1/auth/logout"] = { status: 204 };

    const { router, queryClient } = mountAt("/");

    await screen.findByText(/ada@example.com/);

    // Sign out lives in the account menu now, so the route out has to be
    // opened first. Asserting it through the menu rather than reaching for a
    // hidden button is the point: if the menu stops opening, there is no way
    // to sign out, and that is a bug the old test would not have seen.
    await user.click(screen.getByRole("button", { name: /account/i }));

    await user.click(await screen.findByRole("menuitem", { name: /sign out/i }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });

    // Cleared locally as well as on the server. Leaving it would show the next
    // person at this machine the last one's data until a refetch.
    expect(queryClient.getQueryData(keys.me)).toBeNull();
  });
});
