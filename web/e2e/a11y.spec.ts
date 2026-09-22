import { expect, test, type Page } from "@playwright/test";
import { createRequire } from "node:module";
import type { Result, RunOptions } from "axe-core";

/**
 * axe, in a real browser, with real computed styles.
 *
 * This is the one thing the jsdom suite could never do. `src/ui/a11y.test.tsx`
 * runs the same engine over every story, but jsdom lays nothing out and has no
 * canvas, so it has to disable `color-contrast` outright — the rule silently
 * does not run there. Here it does, against the colors the browser actually
 * painted, which is the only way to catch a token that is legible in isolation
 * and unreadable once something translucent is laid over it.
 *
 * `web/tokens_test.go` checks the token pairs the components are *meant* to
 * produce. This checks what they produced.
 *
 * axe-core is injected from node_modules rather than pulled in as
 * `@axe-core/playwright`: the package is already a dependency of the jsdom
 * suite, and one injected script is cheaper than another wrapper.
 *
 * It goes in with `addInitScript` and not `addScriptTag`, and that is not a
 * style choice. `addScriptTag` appends a real <script> element, which the
 * application's own Content Security Policy refuses — `script-src 'self'`,
 * from Part 9 — so every one of these tests failed at the injection until it
 * moved. `addInitScript` is delivered over the debugging protocol before the
 * page's own scripts run, which is not subject to the policy. The policy stays
 * in force for everything the page itself does, which is the point: a suite
 * that switched the CSP off to make itself easier would stop testing the thing
 * that ships. The last test here proves it is still on.
 */

const require = createRequire(import.meta.url);
const AXE_PATH: string = require.resolve("axe-core/axe.min.js");

const OPTIONS: RunOptions = {
  runOnly: {
    type: "tag",
    values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"],
  },
};

/** Load axe into every document this page opens. Call before navigating. */
async function withAxe(page: Page): Promise<void> {
  await page.addInitScript({ path: AXE_PATH });
}

async function violations(page: Page): Promise<string[]> {
  const found = await page.evaluate(
    async (options) => (await window.axe.run(document, options)).violations,
    OPTIONS,
  );

  // One line per node rather than per rule, and axe's own failureSummary with
  // it: "color-contrast on six links" is not actionable, and "expected 4.5:1,
  // got 3.9:1 against #18181b" is.
  return found.flatMap((violation: Result) =>
    violation.nodes.map(
      (node) =>
        `${violation.id} at ${node.target.join(" ")}\n` +
        `    ${(node.failureSummary ?? violation.help).replace(/\n/g, "\n    ")}`,
    ),
  );
}

declare global {
  interface Window {
    axe: { run: (context: Document, options: RunOptions) => Promise<{ violations: Result[] }> };
  }
}

const PAGES = ["/", "/dashboards", "/questions", "/connections", "/people", "/settings"];

for (const path of PAGES) {
  test(`${path} has no accessibility violations`, async ({ page }) => {
    await withAxe(page);
    await page.goto(path);
    await expect(page.getByRole("main")).toBeVisible();

    expect(await violations(page)).toEqual([]);
  });

  test(`${path} has no accessibility violations in the dark theme`, async ({ page }) => {
    await withAxe(page);
    await page.goto(path);
    await expect(page.getByRole("main")).toBeVisible();

    // Both themes, because contrast is the rule that only runs here and the
    // dark palette is a completely separate set of numbers.
    await page.evaluate(() => {
      document.documentElement.dataset["theme"] = "dark";
    });

    expect(await violations(page)).toEqual([]);
  });
}

test("the login page has no accessibility violations", async ({ page }) => {
  await withAxe(page);

  // Signed out, so this one cannot reuse the saved session.
  await page.context().clearCookies();
  await page.goto("/login");

  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

  expect(await violations(page)).toEqual([]);
});

test("the command palette has no accessibility violations while open", async ({ page }) => {
  await withAxe(page);
  await page.goto("/");
  await expect(page.getByRole("main")).toBeVisible();

  await page.keyboard.press("ControlOrMeta+k");
  await expect(page.getByRole("dialog")).toBeVisible();

  // An open overlay is a different document: a scrim, a focus trap, and
  // everything behind it marked aria-hidden. Scanning only the closed state
  // would miss all of it.
  expect(await violations(page)).toEqual([]);
});

test("the account menu has no accessibility violations while open", async ({ page }) => {
  await withAxe(page);
  await page.goto("/");
  await expect(page.getByRole("main")).toBeVisible();

  await page.getByRole("button", { name: /account/i }).click();
  await expect(page.getByRole("menu")).toBeVisible();

  expect(await violations(page)).toEqual([]);
});

test("the content security policy refuses an injected script", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("main")).toBeVisible();

  // The reason the tests above use addInitScript. `script-src 'self'` means a
  // <script> element with a foreign source never executes, which is the single
  // most valuable line in the policy and the one most likely to be relaxed by
  // somebody debugging something else.
  await expect(
    page.addScriptTag({ content: "window.__injected = true;" }),
  ).rejects.toThrow();

  expect(await page.evaluate(() => "__injected" in window)).toBe(false);
});
