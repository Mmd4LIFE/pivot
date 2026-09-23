import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { dirname, resolve } from "node:path";

import { BINARY, DATABASE } from "../playwright.config";

/**
 * The first run, on a Pivot nobody has ever used.
 *
 * Its own instance, on its own port, against its own empty database — because
 * the rest of the suite runs against one that was provisioned by the CLI, and
 * an instance that has been set up cannot be set up again. That is the point
 * of the feature, and it makes this the one spec that has to bring its own
 * server.
 *
 * What it proves that no unit test can: that a binary with no configuration
 * and no database prints a token, serves a browser, and turns somebody with no
 * account into an administrator looking at the application.
 */

const PORT = 8101;
const BASE = `http://127.0.0.1:${PORT}`;
const DB = resolve(dirname(DATABASE), "pivot-first-run.db");

// No cookies. The shared signed-in state is for another instance entirely, and
// cookies are not scoped by port — carrying it here would hand this server a
// session token it has never issued.
test.use({ storageState: { cookies: [], origins: [] } });

// Serial: the second test depends on the first having claimed the instance,
// which is exactly the behaviour being checked.
test.describe.configure({ mode: "serial" });

let server: ChildProcess | undefined;
let token = "";

test.beforeAll(async () => {
  mkdirSync(dirname(DB), { recursive: true });

  for (const path of [DB, `${DB}-wal`, `${DB}-shm`]) {
    rmSync(path, { force: true });
  }

  server = spawn(BINARY, ["serve"], {
    env: {
      ...process.env,
      PIVOT_DATABASE_URL: `sqlite://${DB}`,
      PIVOT_SERVER_PORT: String(PORT),
      PIVOT_LOG_LEVEL: "error",
    },
    stdio: "pipe",
  });

  // The token is read out of the process's own output, the way an operator
  // reads it out of their terminal. Parsing the banner is the test: if the
  // wording moves, somebody has to come and look at this, which is correct --
  // it is the only instruction a first-time user is given.
  const banner = new Promise<string>((resolveToken, rejectToken) => {
    let buffered = "";

    server?.stdout?.on("data", (chunk: Buffer) => {
      buffered += chunk.toString();

      const match = /Token:\s*(\S+)/.exec(buffered);

      if (match?.[1] !== undefined) resolveToken(match[1]);
    });

    setTimeout(() => rejectToken(new Error(`no setup token in:\n${buffered}`)), 20_000);
  });

  token = await banner;

  await waitForHealth();
});

test.afterAll(() => {
  server?.kill("SIGTERM");
  server = undefined;
});

test("a fresh install prints a token and asks to be set up", async ({ page }) => {
  expect(token).not.toBe("");

  // Straight to the root, the way somebody who has just read "open
  // http://localhost:8080" would.
  await page.goto(BASE + "/");

  // Not the login page. A form no password can satisfy is the dead end this
  // whole part exists to remove.
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole("heading", { name: /set up pivot/i })).toBeVisible();
});

test("the whole first run: nothing, to administrator, in one form", async ({ page }) => {
  await page.goto(BASE + "/setup");

  await page.getByLabel(/organization name/i).fill("Acme Analytics");
  await page.getByLabel(/your name/i).fill("Ada Lovelace");
  await page.getByLabel(/email address/i).fill("ada@example.com");
  await page.getByLabel(/^password/i).fill("a-long-enough-password");
  await page.getByLabel(/setup token/i).fill(token);

  await page.getByRole("button", { name: /create administrator/i }).click();

  // Inside the application, signed in. Not at a login form holding a password
  // that was typed thirty seconds ago.
  await expect(page).toHaveURL(BASE + "/");
  await expect(page.getByRole("main")).toBeVisible();
  await expect(page.getByRole("navigation", { name: /main/i })).toBeVisible();
});

test("setup does not reopen", async ({ page }) => {
  // A second visit, with the same token, on the same instance. The endpoint is
  // closed, and the page says so rather than offering a form that cannot work.
  await page.goto(BASE + "/setup");

  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: /sign in to pivot/i })).toBeVisible();
});

test("the account it made can sign in", async ({ page }) => {
  await page.goto(BASE + "/login");

  await page.getByLabel(/email address/i).fill("ada@example.com");
  await page.getByLabel(/^password/i).fill("a-long-enough-password");
  await page.getByRole("button", { name: /^sign in$/i }).click();

  await expect(page).toHaveURL(BASE + "/");
});

/** Poll until the server answers, or give up loudly. */
async function waitForHealth(): Promise<void> {
  const deadline = Date.now() + 20_000;

  for (;;) {
    try {
      const response = await fetch(`${BASE}/healthz`);

      if (response.ok) return;
    } catch {
      // Not listening yet.
    }

    if (Date.now() > deadline) {
      throw new Error(`the first-run instance did not become healthy on ${BASE} within 20s`);
    }

    await new Promise((sleep) => setTimeout(sleep, 200));
  }
}
