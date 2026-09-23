import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createQueryClient } from "../api/queries";
import { initI18n } from "../i18n";
import { routeTree } from "./tree";

/*
 * The account page.
 *
 * The behaviour worth testing is what happens when something is refused. A
 * wrong current password must not look like a session ending, and a failed
 * change must leave the form usable — the alternative is somebody deciding
 * that changing their password broke Pivot and never trying again.
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
    organizationId: "acme",
    email: "ada@example.com",
    name: "Ada Lovelace",
    locale: "en",
    timezone: "UTC",
    isActive: true,
  },
  session: {
    id: "s1",
    issuedAt: "2026-09-23T08:00:00Z",
    expiresAt: "2026-09-23T16:00:00Z",
    absoluteExpiresAt: "2026-10-23T08:00:00Z",
    lastSeenAt: "2026-09-23T09:00:00Z",
    current: true,
  },
  permissions: ["view_content"],
};

const SESSIONS = {
  sessions: [
    { ...SESSION.session, userAgent: "Firefox on Linux", ip: "10.0.0.1" },
    {
      id: "s2",
      issuedAt: "2026-09-20T08:00:00Z",
      expiresAt: "2026-09-23T16:00:00Z",
      absoluteExpiresAt: "2026-10-20T08:00:00Z",
      lastSeenAt: "2026-09-22T09:00:00Z",
      current: false,
      userAgent: "Safari on iPhone",
      ip: "10.0.0.2",
    },
  ],
};

function signedIn() {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: true, tokenRequired: false },
  };
  replies["GET /api/v1/auth/me"] = { status: 200, body: SESSION };
  replies["GET /api/v1/auth/sessions"] = { status: 200, body: SESSIONS };
}

function mountAccount() {
  const queryClient = createQueryClient(() => {});

  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/account"] }),
  });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  return { router, queryClient };
}

async function fillAndSubmit(current: string, next: string) {
  const user = userEvent.setup();

  await user.type(await screen.findByLabelText(/current password/i), current);
  await user.type(screen.getByLabelText(/new password/i), next);
  await user.click(screen.getByRole("button", { name: /change password/i }));
}

describe("changing your password", () => {
  test("says what it did, and clears the form", async () => {
    signedIn();
    replies["POST /api/v1/auth/password"] = { status: 204 };

    mountAccount();

    await fillAndSubmit("current-password-here", "a-brand-new-password");

    expect(await screen.findByText(/other sessions have been signed out/i)).toBeTruthy();

    // Cleared, so the next person at this machine cannot read the password
    // out of the form.
    await waitFor(() => {
      expect((screen.getByLabelText(/new password/i) as HTMLInputElement).value).toBe("");
    });
  });

  /*
   * The reason the server returns 422 rather than 401 for this.
   *
   * A 401 would reach the query client's global session handling, which sends
   * the user to the login page -- so mistyping your own password in the change
   * form would sign you out. The safety feature would be the thing that
   * logged you out.
   */
  test("a wrong current password does not send you to the login page", async () => {
    signedIn();
    replies["POST /api/v1/auth/password"] = {
      status: 422,
      body: {
        error: {
          code: "PIVOT-REQ-002",
          message: "The request body failed validation",
          details: [{ field: "currentPassword", message: "That is not your current password" }],
        },
      },
    };

    const { router } = mountAccount();

    await fillAndSubmit("wrong-password-here", "a-brand-new-password");

    expect(await screen.findByText(/not your current password/i)).toBeTruthy();
    expect(router.state.location.pathname).toBe("/account");

    // And the new password is still in the field, so somebody who mistyped
    // one box does not have to think up another password.
    expect((screen.getByLabelText(/new password/i) as HTMLInputElement).value).toBe(
      "a-brand-new-password",
    );
  });

  test("a password that is too short names the field", async () => {
    signedIn();
    replies["POST /api/v1/auth/password"] = {
      status: 422,
      body: {
        error: {
          code: "PIVOT-REQ-002",
          message: "The request body failed validation",
          details: [{ field: "newPassword", message: "auth: password is too short" }],
        },
      },
    };

    mountAccount();

    await fillAndSubmit("current-password-here", "short");

    expect(await screen.findByText(/use a longer password/i)).toBeTruthy();
  });

  test("being throttled says to wait rather than that the password was wrong", async () => {
    signedIn();
    replies["POST /api/v1/auth/password"] = {
      status: 429,
      body: { error: { code: "PIVOT-RATE-001", message: "Too many requests" } },
    };

    mountAccount();

    await fillAndSubmit("current-password-here", "a-brand-new-password");

    expect(await screen.findByText(/wait a few minutes/i)).toBeTruthy();
  });
});

describe("the session list", () => {
  test("shows every device and marks this one", async () => {
    signedIn();

    mountAccount();

    expect(await screen.findByText(/firefox on linux/i)).toBeTruthy();
    expect(screen.getByText(/safari on iphone/i)).toBeTruthy();
    expect(screen.getByText(/this device/i)).toBeTruthy();
  });

  /*
   * No button on your own row.
   *
   * Ending the session you are using from a list of devices is a logout by
   * surprise -- the same action, in the place people expect to find "sign
   * out", but with none of the warning.
   */
  test("offers to end the others and not this one", async () => {
    signedIn();

    mountAccount();

    await screen.findByText(/safari on iphone/i);

    // One row is the current session, one is not, so exactly one button.
    expect(screen.getAllByRole("button", { name: /end session/i })).toHaveLength(1);
    expect(screen.getByText(/use sign out to end this one/i)).toBeTruthy();
  });

  test("ending one asks the server and refreshes the list", async () => {
    signedIn();
    replies["DELETE /api/v1/auth/sessions/s2"] = { status: 204 };

    mountAccount();

    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: /end session/i }));

    await waitFor(() => {
      expect(calls).toContain("DELETE /api/v1/auth/sessions/s2");
    });

    // Refetched rather than removed from the cache by hand. The server is the
    // one that knows what is still live, and a list edited locally would show
    // a session as gone when the request had actually failed.
    await waitFor(() => {
      expect(calls.filter((call) => call === "GET /api/v1/auth/sessions").length).toBeGreaterThan(
        1,
      );
    });
  });

  test("says so when a session could not be ended", async () => {
    signedIn();
    replies["DELETE /api/v1/auth/sessions/s2"] = {
      status: 503,
      body: { error: { code: "PIVOT-SRV-002", message: "unavailable" } },
    };

    mountAccount();

    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: /end session/i }));

    expect(await screen.findByText(/could not be ended/i)).toBeTruthy();
  });
});
