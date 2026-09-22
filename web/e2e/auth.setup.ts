import { expect, test as setup } from "@playwright/test";

import { CREDENTIALS, STORAGE_STATE } from "../playwright.config";

/**
 * Sign in once, and save the cookie for everyone else.
 *
 * Not an optimization. Signing in through the form in every `beforeEach`
 * burns the login rate limiter — Part 5 throttles `/auth/login` per IP, and
 * every test in this suite arrives from 127.0.0.1 — so after a dozen tests the
 * server starts answering 429 and the failures read as "too many attempts",
 * which looks like a product bug rather than a suite that is hammering its own
 * front door.
 *
 * The tests that are *about* signing in opt back out with an empty storage
 * state. They are the only ones that should touch the form.
 */
setup("sign in once and save the session", async ({ page }) => {
  await page.goto("/login");

  await page.getByLabel(/email/i).fill(CREDENTIALS.email);
  await page.getByLabel(/password/i).fill(CREDENTIALS.password);
  await page.getByRole("button", { name: /^sign in$/i }).click();

  await expect(page.getByRole("heading", { level: 1, name: /home/i })).toBeVisible();

  await page.context().storageState({ path: STORAGE_STATE });
});
