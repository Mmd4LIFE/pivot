import { expect, test } from "@playwright/test";

/**
 * A browser error reaching the real server.
 *
 * `src/lib/report.test.ts` checks the reporter's behaviour in jsdom, and
 * `internal/api/telemetry_test.go` checks the endpoint's. Neither can check
 * the join: that the listeners are actually installed by `main.tsx`, that a
 * real browser's error event carries what the reporter expects, and that the
 * request survives the Content-Security-Policy the application serves itself
 * under. Those are the three places this has broken before in other projects,
 * and all three are only visible here.
 */

const REPORT_URL = "**/api/v1/telemetry/errors";

test.describe("browser error reporting", () => {
  test("an uncaught error is posted to the server", async ({ page }) => {
    await page.goto("/");

    const reported = page.waitForRequest(REPORT_URL);

    // Thrown from a timeout so it escapes `evaluate` and reaches the page's
    // own error event, which is the path a real bug takes. Throwing inside
    // evaluate would be caught by Playwright and never reach the listener.
    await page.evaluate(() => {
      setTimeout(() => {
        throw new Error("thrown from a real browser");
      }, 0);
    });

    const request = await reported;
    const body = request.postDataJSON() as Record<string, unknown>;

    expect(request.method()).toBe("POST");
    expect(body.kind).toBe("error");
    expect(body.message).toBe("thrown from a real browser");
    expect(body.url).toContain("/");

    // And the server accepted it. A report the endpoint rejects is a report
    // nobody reads, and the jsdom tests cannot see the status at all.
    const response = await request.response();

    expect(response?.status()).toBe(204);
  });

  test("a rejected promise nobody caught is posted too", async ({ page }) => {
    await page.goto("/");

    const reported = page.waitForRequest(REPORT_URL);

    await page.evaluate(() => {
      void Promise.reject(new Error("a promise nobody caught"));
    });

    const body = (await reported).postDataJSON() as Record<string, unknown>;

    expect(body.kind).toBe("unhandledrejection");
    expect(body.message).toBe("a promise nobody caught");
  });

  /*
   * The same error, over and over, is one report.
   *
   * A component that throws on every render throws every frame. Without the
   * dedupe, one bug becomes a flood against the server that is trying to
   * record it -- from every open tab at once.
   */
  test("the same error is reported once", async ({ page }) => {
    await page.goto("/");

    let reports = 0;

    page.on("request", (request) => {
      if (request.url().includes("/api/v1/telemetry/errors")) reports += 1;
    });

    await page.evaluate(() => {
      for (let i = 0; i < 5; i += 1) {
        setTimeout(() => {
          throw new Error("the same thing, again");
        }, 0);
      }
    });

    // Long enough for five timeouts and five reports, if they were coming.
    await page.waitForTimeout(500);

    expect(reports).toBe(1);
  });
});
