import { expect, test } from "@playwright/test";

import { BASE_URL, CREDENTIALS } from "../playwright.config";

/**
 * Your own account, against the real server.
 *
 * The jsdom tests drive the page against canned replies. What only this layer
 * can show is the whole round trip: that the password really changes, that the
 * other device really stops working, and that this one does not — three facts
 * that live in three different processes.
 *
 * It changes the password and changes it back, because every other spec in
 * this suite signs in with the same credentials and a shared instance is a
 * shared state. The restore is what keeps this from being the test that breaks
 * the others.
 */

const NEW_PASSWORD = "a-temporary-e2e-password";

test.describe.configure({ mode: "serial" });

test("the account page shows who you are and where you are signed in", async ({ page }) => {
  await page.goto("/account");

  await expect(page.getByRole("heading", { name: /your account/i })).toBeVisible();
  await expect(page.getByText(CREDENTIALS.email)).toBeVisible();

  // The session doing the looking is in the list, and marked.
  await expect(page.getByText(/this device/i)).toBeVisible();

  // And it offers no way to end itself from here. That is Sign out's job, and
  // a button in a device list that logs you out is a surprise.
  await expect(page.getByText(/use sign out to end this one/i)).toBeVisible();
});

test("a wrong current password is refused without signing you out", async ({ page }) => {
  await page.goto("/account");

  await page.getByLabel(/current password/i).fill("not-the-password");
  await page.getByLabel(/new password/i).fill(NEW_PASSWORD);
  await page.getByRole("button", { name: /change password/i }).click();

  await expect(page.getByText(/not your current password/i)).toBeVisible();

  // Still here, still signed in. A 401 would have sent the browser to the
  // login page, which is why the server answers this with a 422.
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole("heading", { name: /your account/i })).toBeVisible();
});

test("changing it ends the other sessions and keeps this one", async ({ browser, page }) => {
  // A second device, and every part of that is spelled out. A context made
  // from the browser fixture picks up this project's `storageState`, which is
  // the suite's already-signed-in cookie -- so the "other device" arrives
  // holding the same session as the first and the test proves nothing. Empty
  // cookies, explicit baseURL.
  const second = await browser.newContext({
    baseURL: BASE_URL,
    storageState: { cookies: [], origins: [] },
  });
  const elsewhere = await second.newPage();

  await elsewhere.goto("/login");
  await elsewhere.getByLabel(/email address/i).fill(CREDENTIALS.email);
  await elsewhere.getByLabel(/^password/i).fill(CREDENTIALS.password);
  await elsewhere.getByRole("button", { name: /^sign in$/i }).click();
  await expect(elsewhere).toHaveURL(/\/$/);

  await page.goto("/account");

  await page.getByLabel(/current password/i).fill(CREDENTIALS.password);
  await page.getByLabel(/new password/i).fill(NEW_PASSWORD);
  await page.getByRole("button", { name: /change password/i }).click();

  await expect(page.getByText(/other sessions have been signed out/i)).toBeVisible();

  // This one survives: reload, and the page is still there rather than the
  // login form.
  await page.reload();
  await expect(page.getByRole("heading", { name: /your account/i })).toBeVisible();

  // The other one does not. The next thing it asks for is refused, and the
  // application sends it to the login page -- which is the whole point of
  // changing a password after losing a device.
  await elsewhere.goto("/account");
  await expect(elsewhere).toHaveURL(/\/login/);

  await second.close();
});

test("the new password is the one that works now", async ({ page }) => {
  // Put it back, so the rest of the suite's credentials still mean something.
  await page.goto("/account");

  await page.getByLabel(/current password/i).fill(NEW_PASSWORD);
  await page.getByLabel(/new password/i).fill(CREDENTIALS.password);
  await page.getByRole("button", { name: /change password/i }).click();

  await expect(page.getByText(/other sessions have been signed out/i)).toBeVisible();
});
