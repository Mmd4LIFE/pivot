import { defineConfig, devices } from "@playwright/test";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));

/**
 * End-to-end, against the real binary.
 *
 * Not against the Vite dev server. The thing that ships is one Go process
 * serving the embedded bundle, and the differences are exactly where bugs hide
 * — the SPA fallback, the asset caching headers, the document CSP, and a
 * same-origin session cookie that a proxied dev setup would handle
 * differently. Part 9 built all four; this is what checks they still hold.
 *
 * `make e2e` builds the frontend, builds the binary, provisions a throwaway
 * database and runs this. Running `npx playwright test` on its own will fail
 * fast if the binary is missing, which is the right failure.
 */

export const PORT = 8099;
export const BASE_URL = `http://127.0.0.1:${PORT}`;

/*
 * Provisioned by globalSetup, thrown away afterwards. Never the developer's
 * own database: an E2E suite that can delete real data is one people stop
 * running.
 *
 * Absolute, and both of those words are load-bearing. `sqlite://./relative`
 * parses as a URL with `.` for a host, which the driver then cannot open --
 * and it fails with "unable to open database file (14)", which reads like a
 * permissions problem rather than a path one.
 */
export const DATABASE = resolve(HERE, ".playwright/pivot-e2e.db");

/** The binary under test, built by `make e2e`. */
export const BINARY = resolve(HERE, "../bin/pivot");

/** Where the signed-in cookie is parked between projects. */
export const STORAGE_STATE = resolve(HERE, ".playwright/state.json");


export const CREDENTIALS = {
  email: "ada@example.com",
  password: "playwright-e2e-password",
  organization: "Acme Analytics",
  name: "Ada Lovelace",
};

export default defineConfig({
  testDir: "./e2e",
  outputDir: ".playwright/results",

  // A failing E2E test is usually a real failure, and a retry that turns it
  // green teaches people to re-run rather than to look. CI gets one retry
  // because a runner genuinely is flakier than a laptop.
  retries: process.env["CI"] === undefined ? 0 : 1,

  // Serialized. The suite signs the same account in and out, and parallel
  // workers would revoke each other's sessions.
  workers: 1,

  reporter: process.env["CI"] === undefined ? "list" : [["list"], ["html", { open: "never" }]],

  /*
   * Provisions the database and starts the server, and returns the function
   * that stops it.
   *
   * Playwright's `webServer` option is deliberately unused: it starts the
   * process *before* globalSetup, which means provisioning afterwards leaves
   * the server reading a database that has since been deleted. See the file
   * for the full trap.
   */
  globalSetup: "./e2e/setup.ts",

  use: {
    baseURL: BASE_URL,

    /*
     * Reduced motion, which the base stylesheet honors by collapsing every
     * transition to 0.01ms.
     *
     * Not a preference here, a correctness fix. Switching theme and scanning
     * immediately measured the colors *mid-transition* -- `transition-colors`
     * on the nav links meant axe saw the light value fading towards the dark
     * one and reported 2.29:1 against a background that had already changed.
     * The product was fine; the suite was reading it too early.
     */
    reducedMotion: "reduce",

    // Kept only for a failure, because a trace per passing test fills a disk
    // and nobody opens them.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    // Signs in once and saves the cookie; see e2e/auth.setup.ts for why that
    // is a correctness matter and not a speed one.
    { name: "setup", testMatch: /auth\.setup\.ts/ },

    {
      name: "chromium",
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], storageState: STORAGE_STATE },
    },
  ],

});
