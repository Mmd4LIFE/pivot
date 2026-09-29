import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createQueryClient } from "../api/queries";
import { initI18n } from "../i18n";
import { locate } from "./editor";
import { clearWorkspace } from "./editor.workspace";
import { routeTree } from "./tree";

/*
 * The SQL editor.
 *
 * Three things are worth testing here, and none of them is that a table
 * renders. A result that met the row cap must say so, because showing a
 * partial answer as a whole one is the failure the whole truncation signal
 * exists to prevent. A rejected statement must show the source's own words,
 * because "no such column: nope" is the entire answer and anything else sends
 * somebody looking in the wrong place. And a NULL must not look like an empty
 * string, which the connectors went to real trouble to keep apart.
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

      return new Response(reply.body === undefined ? null : JSON.stringify(reply.body), {
        status: reply.status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );

  clearWorkspace();

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
    issuedAt: "2026-09-29T08:00:00Z",
    expiresAt: "2026-09-29T16:00:00Z",
    absoluteExpiresAt: "2026-10-29T08:00:00Z",
    lastSeenAt: "2026-09-29T09:00:00Z",
    current: true,
  },
  permissions: ["native_query"],
};

function signedIn(connections: unknown[] = [{ id: "c1", slug: "sales", name: "Sales", kind: "sqlite" }]) {
  replies["GET /api/v1/setup/status"] = {
    status: 200,
    body: { initialized: true, tokenRequired: false },
  };
  replies["GET /api/v1/auth/me"] = { status: 200, body: SESSION };
  replies["GET /api/v1/connections"] = { status: 200, body: { connections } };
}

function mountEditor() {
  const queryClient = createQueryClient(() => {});

  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/editor"] }),
  });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function result(overrides: Record<string, unknown> = {}) {
  return {
    queryId: "q1",
    columns: [
      { name: "id", type: "integer", sourceType: "INTEGER" },
      { name: "manager", type: "string", sourceType: "TEXT" },
    ],
    rows: [[1, "Ada"]],
    rowCount: 1,
    truncated: false,
    cacheStatus: "miss",
    durationMs: 3,
    ...overrides,
  };
}

async function run() {
  await userEvent.click(await screen.findByRole("button", { name: /run/i }));
}

describe("running a statement", () => {
  test("shows the rows it got back", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = { status: 200, body: result() };

    mountEditor();
    await run();

    expect(await screen.findByText("Ada")).toBeInTheDocument();

    // The source's own spelling is shown beside the column, because a person
    // debugging wants it and the canonical kind is what the grid formats on.
    expect(screen.getByText("INTEGER")).toBeInTheDocument();
  });

  /*
   * A result cut at the row cap says so.
   *
   * Without this the grid presents a partial answer as a complete one, which
   * is exactly what the truncation flag was added to prevent — it travels all
   * the way from the connector, and the last step is the easiest place to drop
   * it.
   */
  test("says when the result was truncated", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = {
      status: 200,
      body: result({ truncated: true, rowCount: 5 }),
    };

    mountEditor();
    await run();

    expect(await screen.findByText(/truncated/i)).toBeInTheDocument();
  });

  // A cached answer says so, which is why the second run was instant.
  test("says when the answer came from cache", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = { status: 200, body: result({ cacheStatus: "hit" }) };

    mountEditor();
    await run();

    expect(await screen.findByText(/from cache/i)).toBeInTheDocument();
  });

  /*
   * A NULL is not an empty string.
   *
   * An empty cell is what both look like, and the connectors have a
   * conformance property keeping them apart across four databases. Throwing
   * that away in the last five pixels would be a waste of the whole exercise.
   */
  test("shows a null as a null", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = {
      status: 200,
      body: result({ rows: [[1, null]] }),
    };

    mountEditor();
    await run();

    expect(await screen.findByText("NULL")).toBeInTheDocument();
  });
});

describe("when the source says no", () => {
  /*
   * The source's own words, not a paraphrase.
   *
   * "no such column: nope" is the whole answer. A generic "the query failed"
   * sends somebody to check their connection, their permissions and their
   * network before they find the typo.
   */
  test("shows the message the source sent", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = {
      status: 502,
      body: {
        error: {
          code: "PIVOT-QUERY-002",
          message: "SQLite rejected the query: no such column: nope",
          docs: "https://docs.pivot.dev/errors/PIVOT-QUERY-002",
        },
      },
    };

    mountEditor();
    await run();

    expect(await screen.findByText(/no such column: nope/i)).toBeInTheDocument();
  });

  // A failed query leaves the editor usable, so the typo can be fixed in place.
  test("leaves the statement where it was", async () => {
    signedIn();
    replies["POST /api/v1/queries"] = {
      status: 502,
      body: { error: { code: "PIVOT-QUERY-002", message: "nope", docs: "" } },
    };

    mountEditor();
    await run();

    await waitFor(() => expect(screen.getByText(/nope/)).toBeInTheDocument());

    expect(screen.getByRole("button", { name: /run/i })).toBeEnabled();
  });
});

/*
 * An instance with nothing connected explains itself.
 *
 * A picker with no options and a Run button that cannot work is a page that
 * looks broken. Saying what to do instead is the difference between an empty
 * state and an accident.
 */
test("explains an instance with no sources", async () => {
  signedIn([]);

  mountEditor();

  expect(await screen.findByText(/no sources are connected/i)).toBeInTheDocument();
  expect(calls).not.toContain("POST /api/v1/queries");
});

/*
 * Turning the source's byte offset into a place in the text.
 *
 * Counted in bytes because that is what the source reports. Doing it in
 * JavaScript string indices lands on the wrong character in any statement
 * containing a non-ASCII identifier -- and a marker one place to the left of
 * the problem is worse than no marker, because it is confidently wrong.
 */
describe("locating an error in the statement", () => {
  test("finds the line and column of a byte offset", () => {
    const sql = "SELECT 1\nFROM nope\nWHERE x";

    // 1-based, pointing at the "n" of nope: 9 bytes of the first line and its
    // newline, then 5 more.
    const at = locate(sql, 15);

    expect(at).toEqual({ line: 2, column: 6, text: "FROM nope" });
  });

  test("points at the first character when the offset is 1", () => {
    expect(locate("SELECT", 1)).toEqual({ line: 1, column: 1, text: "SELECT" });
  });

  /*
   * The case that makes bytes rather than characters the right unit.
   *
   * "SELECT ä, nope" is 15 bytes but 14 characters, because ä takes two. An
   * offset counted in string indices would point one character to the left of
   * the word that is actually wrong.
   */
  test("counts bytes, not characters", () => {
    const sql = "SELECT ä, nope";

    // Byte 11 is the "n" of nope: 7 for "SELECT ", 2 for ä, then ", ".
    expect(locate(sql, 11)?.column).toBe(10);
  });

  test("has nothing to say without a position", () => {
    expect(locate("SELECT 1", undefined)).toBeUndefined();
    expect(locate("SELECT 1", 0)).toBeUndefined();
  });

  // A position past the end is a source disagreeing with itself. Showing a
  // marker somewhere arbitrary would be worse than showing none.
  test("refuses a position past the end", () => {
    expect(locate("SELECT 1", 999)).toBeUndefined();
  });
});
