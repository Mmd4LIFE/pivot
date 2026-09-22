import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname } from "node:path";

import { BASE_URL, BINARY, CREDENTIALS, DATABASE, PORT, STORAGE_STATE } from "../playwright.config";

/**
 * A throwaway instance: provisioned, served, and torn down.
 *
 * The server is started here rather than through Playwright's `webServer`
 * option, and that is the whole reason this file exists in this shape.
 * Playwright starts `webServer` *before* globalSetup, so provisioning
 * afterwards leaves the server holding an open descriptor on a database that
 * has since been deleted and recreated: it keeps reading the old, empty file
 * while the new one fills up. Every login then fails with "that email address
 * and password do not match" — a completely convincing wrong answer, and one
 * that sends you looking at the login form.
 *
 * Moving the cleanup to the config's module scope does not fix it either.
 * Playwright evaluates the config once per worker process, so the database
 * gets deleted again in the middle of the run.
 *
 * Doing all of it here makes the order unambiguous: wipe, migrate, create the
 * user, then start the server that will read it.
 *
 * The instance is provisioned through the real CLI rather than by writing
 * rows. `pivot admin create-user` is how an operator bootstraps an install, so
 * the documented first-run path is covered by the same suite that covers the
 * login page. Inserting a user directly would test a fixture instead.
 */

let server: ChildProcess | undefined;

export default async function setup(): Promise<() => Promise<void>> {
  if (!existsSync(BINARY)) {
    throw new Error(
      `${BINARY} does not exist. Run \`make e2e\`, which builds it first, ` +
        `or \`make all\` by hand.`,
    );
  }

  // From scratch every run. A suite that inherits yesterday's state passes for
  // reasons nobody can reconstruct.
  mkdirSync(dirname(DATABASE), { recursive: true });

  for (const path of [DATABASE, `${DATABASE}-wal`, `${DATABASE}-shm`, STORAGE_STATE]) {
    rmSync(path, { force: true });
  }

  const env = {
    ...process.env,
    PIVOT_DATABASE_URL: `sqlite://${DATABASE}`,
    PIVOT_SERVER_PORT: String(PORT),
    PIVOT_LOG_LEVEL: "error",
    PIVOT_ADMIN_PASSWORD: CREDENTIALS.password,
  };

  execFileSync(BINARY, ["migrate", "up"], { env, stdio: "pipe" });

  execFileSync(
    BINARY,
    [
      "admin",
      "create-user",
      "--create-org",
      CREDENTIALS.organization,
      "--email",
      CREDENTIALS.email,
      "--name",
      CREDENTIALS.name,
    ],
    { env, stdio: "pipe" },
  );

  server = spawn(BINARY, ["serve"], { env, stdio: "pipe" });

  server.on("exit", (code) => {
    if (code !== 0 && code !== null) {
      console.error(`pivot serve exited with ${code}`);
    }
  });

  await waitForHealth();

  // Returned rather than registered as a separate globalTeardown, so the
  // handle to the process cannot go missing between the two.
  return async () => {
    server?.kill("SIGTERM");
    server = undefined;
  };
}

/** Poll until the server answers, or give up loudly. */
async function waitForHealth(): Promise<void> {
  const deadline = Date.now() + 20_000;

  for (;;) {
    try {
      const response = await fetch(`${BASE_URL}/healthz`);

      if (response.ok) return;
    } catch {
      // Not listening yet.
    }

    if (Date.now() > deadline) {
      throw new Error(`pivot did not become healthy on ${BASE_URL} within 20s`);
    }

    await new Promise((resolve) => setTimeout(resolve, 200));
  }
}
