import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "../api/client";
import { installErrorReporting, reportError, resetReporting } from "./report";

/*
 * The error reporter.
 *
 * What is actually being tested here is restraint. A reporter that sends
 * everything it sees is a denial of service against the server it reports to,
 * and one that can fail loudly is a second source of errors on a page that
 * already has one. Almost every test below is about something the reporter
 * does *not* do.
 */

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  resetReporting();

  fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** The JSON body of the nth report. */
function reportBody(call = 0): Record<string, unknown> {
  const [, init] = fetchMock.mock.calls[call] as [string, RequestInit];

  return JSON.parse(String(init.body)) as Record<string, unknown>;
}

describe("reportError", () => {
  it("posts the error to Pivot's own endpoint", () => {
    reportError(new Error("the dashboard exploded"), "render");

    expect(fetchMock).toHaveBeenCalledTimes(1);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];

    expect(url).toBe("/api/v1/telemetry/errors");
    expect(init.method).toBe("POST");

    // So a report fired as the page unloads still leaves. Without it the
    // navigation cancels the request, and an error bad enough to make someone
    // close the tab is exactly the one nobody would hear about.
    expect(init.keepalive).toBe(true);

    const body = reportBody();

    expect(body.kind).toBe("render");
    expect(body.message).toBe("the dashboard exploded");
    expect(body.url).toBe(window.location.href);
  });

  /*
   * The identifiers are only attached when the thing being reported *is* the
   * failed request. The tempting alternative -- remember the last failure and
   * staple it to whatever comes next -- points an operator at an unrelated
   * trace, which costs more time than having no trace at all.
   */
  it("carries the trace of the API call that failed", () => {
    reportError(
      new ApiError(
        500,
        { message: "server error", requestId: "req-1" },
        "4bf92f3577b34da6a3ce929d0e0e4736",
      ),
      "unhandledrejection",
    );

    const body = reportBody();

    expect(body.traceId).toBe("4bf92f3577b34da6a3ce929d0e0e4736");
    expect(body.requestId).toBe("req-1");
  });

  it("attaches no trace to an error that did not come from a request", () => {
    reportError(new Error("render failed"), "render");

    const body = reportBody();

    expect(body.traceId).toBeUndefined();
    expect(body.requestId).toBeUndefined();
  });

  it("sends the component stack the boundary gives it", () => {
    reportError(new Error("boom"), "render", "\n    at Dashboard\n    at App");

    expect(reportBody().stack).toContain("at Dashboard");
  });

  /*
   * `throw "boom"` and `throw {code: 1}` are both legal and both happen,
   * usually from a library. A reporter that assumed an Error would report
   * nothing for exactly the throws that are hardest to track down.
   */
  it.each([
    ["a string", "just a string", "just a string"],
    ["an object with a message", { message: "from an object" }, "from an object"],
    ["a number", 42, "42"],
  ])("reports %s", (_name, thrown, expected) => {
    reportError(thrown, "error");

    expect(reportBody().message).toBe(expected);
  });

  it("says nothing when there is nothing to say", () => {
    reportError("", "error");

    expect(fetchMock).not.toHaveBeenCalled();
  });

  /*
   * A component that throws on every render throws every frame. Without a
   * cap, one bug becomes a flood -- from every open tab at once.
   */
  it("reports the same error once", () => {
    const error = new Error("the same thing, again");

    reportError(error, "render");
    reportError(error, "render");
    reportError(error, "render");

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("stops after ten distinct errors", () => {
    for (let i = 0; i < 25; i += 1) {
      reportError(new Error(`error ${String(i)}`), "error");
    }

    expect(fetchMock).toHaveBeenCalledTimes(10);
  });

  /*
   * The recursion this guards against is real: a throw inside the reporter
   * reaches window.onerror, which calls the reporter, which throws in the same
   * place. Unbounded, on a page that is already broken.
   */
  it("does not throw when the reporter itself fails", () => {
    fetchMock.mockImplementation(() => {
      throw new Error("fetch is unavailable");
    });

    expect(() => {
      reportError(new Error("original"), "error");
    }).not.toThrow();
  });

  it("keeps working after a failed report", () => {
    fetchMock.mockImplementationOnce(() => {
      throw new Error("fetch is unavailable");
    });

    reportError(new Error("first"), "error");
    reportError(new Error("second"), "error");

    // The guard flag is cleared in a finally. Leaving it set would switch
    // reporting off for the rest of the page's life, silently.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("swallows a rejected report rather than making it a new error", async () => {
    fetchMock.mockImplementation(() => Promise.reject(new Error("offline")));

    const unhandled = vi.fn();
    window.addEventListener("unhandledrejection", unhandled);

    reportError(new Error("original"), "error");

    await new Promise((resolve) => setTimeout(resolve, 0));

    window.removeEventListener("unhandledrejection", unhandled);

    expect(unhandled).not.toHaveBeenCalled();
  });
});

/*
 * An error boundary catches render errors and nothing else. A throw in an
 * event handler, a timeout, or a rejected promise unmounts nothing and reaches
 * no boundary -- which is how a page can look fine while being broken.
 */
describe("installErrorReporting", () => {
  it("reports an error the boundary would never see", () => {
    const uninstall = installErrorReporting();

    window.dispatchEvent(
      new ErrorEvent("error", { error: new Error("from a click handler") }),
    );

    uninstall();

    expect(fetchMock).toHaveBeenCalledTimes(1);

    const body = reportBody();

    expect(body.kind).toBe("error");
    expect(body.message).toBe("from a click handler");
  });

  it("reports a rejected promise nobody caught", () => {
    const uninstall = installErrorReporting();

    // Constructed rather than caused: an actual unhandled rejection in jsdom
    // is reported asynchronously and inconsistently across versions, and this
    // test is about the listener, not about jsdom.
    const event = new Event("unhandledrejection") as Event & { reason: unknown };
    event.reason = new Error("a promise nobody caught");

    window.dispatchEvent(event);

    uninstall();

    expect(reportBody().kind).toBe("unhandledrejection");
    expect(reportBody().message).toBe("a promise nobody caught");
  });

  it("stops listening once uninstalled", () => {
    installErrorReporting()();

    // A rejection rather than an error event, for a reason worth keeping: an
    // `error` event on window with nothing left to handle it is an uncaught
    // exception, and the test runner is right to fail the run over one. The
    // assertion here is that the listener is gone, and either listener proves
    // it -- they are installed and removed together.
    const event = new Event("unhandledrejection") as Event & { reason: unknown };
    event.reason = new Error("after");

    window.dispatchEvent(event);

    expect(fetchMock).not.toHaveBeenCalled();
  });
});
