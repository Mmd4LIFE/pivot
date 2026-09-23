import { API_PREFIX, ApiError } from "../api/client";

/**
 * Browser error reporting.
 *
 * Deliberately not Sentry. Their browser SDK is around 30 KB gzipped against
 * an initial-bundle budget of 200 KB that is already 93% spent, and a
 * self-hosted product should not need an account somewhere else to see its own
 * errors. This posts to Pivot's own endpoint, costs a few hundred bytes, and
 * puts the error in the same log as the request that caused it.
 *
 * What it gives up is real: no source-map resolution, no grouping, no release
 * tracking, no offline queue. Those are the reasons to reach for a service,
 * and an operator who wants one can point their own at this page. What this
 * guarantees instead is that a Pivot with no internet access still reports.
 */

/** How the error surfaced. They are usually different bugs. */
export type ErrorKind = "render" | "error" | "unhandledrejection";

interface ErrorReport {
  kind: ErrorKind;
  message: string;
  stack?: string;
  url?: string;
  traceId?: string;
  requestId?: string;
}

/**
 * The per-page-load budget.
 *
 * A render error in a component that re-renders can throw every frame, and a
 * reporter without a cap turns one bug into a denial of service against the
 * server it is reporting to — from every open tab at once. Ten is enough to
 * see the error and the two that followed it.
 *
 * The server rate-limits as well, because a client-side limit protects a
 * server only from clients that run this code.
 */
const MAX_REPORTS = 10;

/** Field caps, mirroring the server's. Truncating here saves the round trip. */
const MAX_MESSAGE = 1024;
const MAX_STACK = 8192;

let sent = 0;

/**
 * Messages already reported, so a loop reports once.
 *
 * Keyed by message and the first stack frame rather than the whole stack:
 * the same bug from the same place is one bug, however many times React
 * retries the render.
 */
const seen = new Set<string>();

/**
 * Guards against reporting the reporter.
 *
 * A throw inside `reportError` itself propagates out of whichever listener
 * called it, reaches `window.onerror`, and calls `reportError` again — which
 * throws in the same place. That is unbounded recursion, and this flag is what
 * bounds it. The failing report is *dropped*, not retried, because a reporter
 * that cannot report is not an emergency worth compounding.
 *
 * The separate `.catch` below handles the other case: the request itself
 * rejecting, asynchronously, long after this flag is back to false. An
 * unhandled rejection there would arrive at the listener as a fresh error and
 * start the same loop by a different route.
 */
let reporting = false;

/** Resets module state. Exported for tests, which each want a fresh budget. */
export function resetReporting(): void {
  sent = 0;
  seen.clear();
  reporting = false;
}

/**
 * Report an error.
 *
 * Never throws and never rejects: a reporter that can fail loudly is a second
 * source of errors on a page that already has one. Every failure path here is
 * silent on purpose.
 */
export function reportError(error: unknown, kind: ErrorKind, stack?: string): void {
  if (reporting || sent >= MAX_REPORTS) return;

  reporting = true;

  try {
    send(error, kind, stack);
  } catch {
    // Swallowed, so a bug in here cannot become the error the user sees. The
    // flag above already stops it recursing; this stops it escaping into the
    // event handler that called us and being reported as something else.
  } finally {
    // In a finally, so a throw anywhere above still clears the flag. Leaving
    // it set would silently switch reporting off for the rest of the page's
    // life, which is the failure mode nobody notices.
    reporting = false;
  }
}

function send(error: unknown, kind: ErrorKind, stack?: string): void {
  const message = messageOf(error);

  if (message === "") return;

  const trace = stack ?? stackOf(error);
  const key = message + "\n" + trace.slice(0, 200);

  if (seen.has(key)) return;

  seen.add(key);
  sent += 1;

  const report: ErrorReport = {
    kind,
    message: message.slice(0, MAX_MESSAGE),
    url: window.location.href,
  };

  if (trace !== "") report.stack = trace.slice(0, MAX_STACK);

  /*
   * The identifiers of the API call that failed -- but only when the thing
   * being reported *is* that failure.
   *
   * The tempting version remembers the last failed request and attaches it to
   * whatever is reported next. That is a guess, and a guess that is wrong
   * points an operator at an unrelated trace, which costs more time than
   * having no trace at all. So: exact or absent.
   *
   * The browser does not invent either identifier. The trace comes from the
   * failed response's `traceresponse` header and the request ID from its
   * envelope, so both name something the server can find. Inventing a trace ID
   * would also mean inventing the sampling decision, which is the server's.
   */
  if (error instanceof ApiError) {
    if (error.traceId !== undefined) report.traceId = error.traceId;
    if (error.requestId !== undefined) report.requestId = error.requestId;
  }

  try {
    void fetch(API_PREFIX + "/telemetry/errors", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(report),

      // So a report fired as the page is unloading still leaves. Without it,
      // the navigation cancels the request and the error that caused the user
      // to close the tab is the one nobody hears about.
      keepalive: true,
    }).catch(() => {
      // Swallowed. The server being unreachable is not something this page
      // can do anything useful about, and a rejected promise here would be
      // reported as a new error.
    });
  } catch {
    // fetch can throw synchronously -- jsdom does, and a browser will for a
    // malformed request. Same reasoning as the catch above.
  }
}

/**
 * Listen for the errors React's boundary never sees.
 *
 * An error boundary catches render errors and nothing else: a throw in an
 * event handler, a timeout or a rejected promise unmounts nothing and reaches
 * no boundary. Those are the two listeners below, and they are why a page can
 * look fine while being broken.
 *
 * Returns a function that removes them, which is what a test needs and what
 * nothing in the application does.
 */
export function installErrorReporting(): () => void {
  const onError = (event: ErrorEvent): void => {
    reportError(event.error ?? event.message, "error");
  };

  const onRejection = (event: PromiseRejectionEvent): void => {
    reportError(event.reason, "unhandledrejection");
  };

  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);

  return () => {
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  };
}

/**
 * The message, whatever was thrown.
 *
 * `throw "boom"` and `throw {code: 1}` are both legal and both happen, usually
 * from a library. A reporter that assumed an Error would report nothing for
 * exactly the throws that are hardest to track down.
 */
function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;

  if (error !== null && typeof error === "object") {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") return message;
  }

  try {
    return String(error);
  } catch {
    // A thrown object with a hostile toString. It has happened.
    return "Unserializable error";
  }
}

function stackOf(error: unknown): string {
  return error instanceof Error && typeof error.stack === "string" ? error.stack : "";
}
