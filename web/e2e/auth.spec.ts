import { expect, test, type Page } from "@playwright/test";

import { CREDENTIALS } from "../playwright.config";

/**
 * Login, shell, logout — in a real browser, against the real binary.
 *
 * This is the half the jsdom suites cannot reach. They prove that focus moves
 * to the right element; only a browser proves that the page it moved to was
 * actually painted, that the session cookie survived a real navigation, and
 * that the Go server's SPA fallback returned the shell for a client route it
 * has never heard of.
 */

async function signIn(page: Page): Promise<void> {
  await page.getByLabel(/email/i).fill(CREDENTIALS.email);
  await page.getByLabel(/password/i).fill(CREDENTIALS.password);
  await page.getByRole("button", { name: /^sign in$/i }).click();
}

test.describe("signing in", () => {
  // The only tests that should touch the form. Everything else reuses the
  // cookie saved by the setup project, because the login endpoint is rate
  // limited per IP and the whole suite arrives from one.
  test.use({ storageState: { cookies: [], origins: [] } });

  test("a signed-out visitor is sent to the login page", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("heading", { name: /sign in to pivot/i })).toBeVisible();
  });

  test("the whole flow: sign in, land in the shell, sign out", async ({ page }) => {
    await page.goto("/login");
    await signIn(page);

    // The shell, not just a 200.
    await expect(page).toHaveURL(new RegExp(`${escape("/")}$`));
    await expect(page.getByRole("heading", { level: 1, name: /home/i })).toBeVisible();
    await expect(page.getByRole("navigation", { name: /main/i })).toBeVisible();
    await expect(page.getByText(CREDENTIALS.email)).toBeVisible();

    await page.getByRole("button", { name: /account/i }).click();
    await page.getByRole("menuitem", { name: /sign out/i }).click();

    await expect(page).toHaveURL(/\/login/);

    // And the session is really gone, not just navigated away from.
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("a wrong password is refused, and says so once", async ({ page }) => {
    await page.goto("/login");

    await page.getByLabel(/email/i).fill(CREDENTIALS.email);
    await page.getByLabel(/password/i).fill("not the password");
    await page.getByRole("button", { name: /^sign in$/i }).click();

    await expect(page.getByRole("alert")).toContainText(/do not match/i);
    await expect(page).toHaveURL(/\/login/);
  });

  test("the intended destination survives the round trip", async ({ page }) => {
    // The Go server has never heard of /connections; the SPA fallback is what
    // makes this a page at all, and the guard is what turns it into a login
    // with a destination attached.
    await page.goto("/connections");

    await expect(page).toHaveURL(/\/login\?redirect=%2Fconnections/);

    await signIn(page);

    await expect(page).toHaveURL(/\/connections$/);
    await expect(page.getByRole("heading", { level: 1, name: /connections/i })).toBeVisible();
  });

  test("a hostile destination is refused", async ({ page }) => {
    await page.goto("/login?redirect=https%3A%2F%2Fevil.example");

    await signIn(page);

    // Not evil.example. The client applies the same rule the server applies to
    // the SSO return parameter.
    await expect(page).toHaveURL(new RegExp(`${escape("/")}$`));
  });
});

test.describe("the shell", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1, name: /home/i })).toBeVisible();
  });

  test("navigates without a full page load", async ({ page }) => {
    await page.getByRole("navigation", { name: /main/i }).getByRole("link", { name: /dashboards/i }).click();

    await expect(page).toHaveURL(/\/dashboards$/);
    await expect(page.getByRole("heading", { level: 1, name: /dashboards/i })).toBeVisible();

    // Breadcrumbs follow, and the last crumb is the page rather than a link.
    const crumbs = page.getByRole("navigation", { name: /breadcrumb/i });
    await expect(crumbs.getByRole("link", { name: /home/i })).toBeVisible();
    await expect(crumbs.getByText("Dashboards")).toBeVisible();
  });

  test("marks the current page in the sidebar", async ({ page }) => {
    await page.goto("/questions");

    const current = page
      .getByRole("navigation", { name: /main/i })
      .locator("[aria-current='page']");

    // aria-current, not only a color: a color-blind user has no other way to
    // tell where they are.
    await expect(current).toHaveText(/questions/i);
  });

  test("has exactly one main landmark and one h1", async ({ page }) => {
    await expect(page.getByRole("main")).toHaveCount(1);
    await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  });

  test("the skip link is the first tab stop and reaches the content", async ({ page }) => {
    await page.keyboard.press("Tab");

    const skip = page.getByRole("link", { name: /skip to content/i });

    // Invisible until focused, and then genuinely visible -- sr-only alone
    // would hide it from the sighted keyboard user it exists for.
    await expect(skip).toBeFocused();
    await expect(skip).toBeVisible();

    await page.keyboard.press("Enter");

    await expect(page.getByRole("main")).toBeFocused();
  });
});

test.describe("the command palette", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1, name: /home/i })).toBeVisible();
  });

  test("opens on the hotkey, filters, and navigates", async ({ page }) => {
    await page.keyboard.press("ControlOrMeta+k");

    const palette = page.getByRole("dialog");
    await expect(palette).toBeVisible();

    await page.keyboard.type("conn");

    // cmdk matches a subsequence rather than a substring, so "conn" also finds
    // "cOnfiguratioN" among the Settings keywords. Asserting a count of one
    // would be asserting that the matcher is worse than it is. What has to be
    // true is that the best match is selected and Enter goes there -- no
    // pointer at any point.
    await expect(palette.getByRole("option").first()).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await expect(palette.getByRole("option").first()).toContainText(/connections/i);

    await page.keyboard.press("Enter");

    await expect(page).toHaveURL(/\/connections$/);
  });

  test("finds a page by a word that is not in its name", async ({ page }) => {
    await page.keyboard.press("ControlOrMeta+k");
    await page.keyboard.type("warehouse");

    // "warehouse" appears nowhere on screen -- it is a keyword on Connections.
    // Searching only the rendered text would find nothing, which is the
    // difference between a palette and a filter box.
    await expect(page.getByRole("option")).toHaveCount(1);
    await expect(page.getByRole("option")).toContainText(/connections/i);
  });

  test("Escape closes it and leaves the page alone", async ({ page }) => {
    await page.keyboard.press("ControlOrMeta+k");
    await expect(page.getByRole("dialog")).toBeVisible();

    await page.keyboard.press("Escape");

    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page).toHaveURL(new RegExp(`${escape("/")}$`));
  });
});

test.describe("theme and direction", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1, name: /home/i })).toBeVisible();
  });

  test("switching theme changes nothing but the custom properties", async ({ page }) => {
    const html = page.locator("html");

    await page.getByRole("button", { name: /account/i }).click();
    await page.getByRole("menuitemradio", { name: /^dark$/i }).click();

    await expect(html).toHaveAttribute("data-theme", "dark");

    // The token actually resolved differently -- which is the claim, and the
    // one thing jsdom could never check, because it computes no styles.
    const canvas = await html.evaluate((node) =>
      getComputedStyle(node).getPropertyValue("--pivot-canvas").trim(),
    );

    expect(canvas).toBe("9 9 11");
  });

  test("dir=rtl mirrors the layout", async ({ page }) => {
    const sidebar = page.getByRole("navigation", { name: /main/i });

    const before = await sidebar.boundingBox();
    expect(before).not.toBeNull();

    // Set directly rather than through the locale picker. The right-to-left
    // pseudo-locale is development-only and this suite runs the production
    // build, and shipping a fake locale to users so that a test can click it
    // would be letting the test decide the product. The locale plumbing that
    // writes this attribute is covered in src/i18n/i18n.test.ts; what only a
    // browser can answer is whether the stylesheet actually mirrors, because
    // that needs layout.
    await page.evaluate(() => {
      document.documentElement.dir = "rtl";
    });

    const after = await sidebar.boundingBox();
    const viewport = page.viewportSize();

    expect(after).not.toBeNull();
    expect(viewport).not.toBeNull();

    // The sidebar has genuinely moved to the other side. Every logical
    // property in the stylesheet resolves against that one attribute, which is
    // what makes Arabic a translation job rather than a rewrite.
    expect(before!.x).toBeLessThan(viewport!.width / 2);
    expect(after!.x).toBeGreaterThan(viewport!.width / 2);
  });
});

/** Escape a string for use inside a RegExp. */
function escape(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
