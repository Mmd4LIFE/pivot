import type { paths } from "./schema";

/**
 * The API client.
 *
 * Hand-written over the generated types rather than pulling a client library:
 * it is forty lines, the project keeps its dependency list short on purpose,
 * and the one thing a generic client would not know is the error envelope
 * every Pivot endpoint shares.
 *
 * Credentials are always included. The session lives in an HttpOnly cookie, so
 * there is no token for this code to hold -- which is the point, and why an
 * XSS bug here cannot walk away with a session.
 */

/** The error envelope every non-2xx response uses. */
export interface ApiErrorBody {
  code: string;
  message: string;
  details?: { field?: string; message: string }[];
  requestId?: string;
  docs: string;
}

/**
 * An API failure, carrying the stable code clients branch on.
 *
 * Branch on `code`, never on `message`: the code's meaning is fixed forever,
 * the prose is not.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: { field?: string; message: string }[];
  readonly requestId: string | undefined;
  readonly docs: string | undefined;

  constructor(status: number, body: Partial<ApiErrorBody>) {
    super(body.message ?? `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code ?? "PIVOT-SRV-001";
    this.details = body.details ?? [];
    this.requestId = body.requestId;
    this.docs = body.docs;
  }

  /** Whether this means "you are not signed in". */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }

  /** Whether this means "signed in, but not permitted". */
  get isForbidden(): boolean {
    return this.status === 403;
  }

  /**
   * Whether the answer was unavailable rather than negative.
   *
   * The server draws this distinction deliberately -- 403 is a decision, 503
   * is the absence of one -- so the UI can say "try again" instead of "ask an
   * administrator".
   */
  get isUnavailable(): boolean {
    return this.status === 503;
  }
}

/** Raised when the network failed before any response arrived. */
export class NetworkError extends Error {
  constructor(cause: unknown) {
    super("Could not reach Pivot");
    this.name = "NetworkError";
    this.cause = cause;
  }
}

export const API_PREFIX = "/api/v1";

interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
}

/**
 * Issue a request and decode the response.
 *
 * A 204 returns undefined rather than attempting to parse an empty body, which
 * is what every delete endpoint returns.
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = "GET", body, signal } = options;

  const init: RequestInit = {
    method,
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  };

  if (signal) init.signal = signal;

  if (body !== undefined) {
    init.headers = { ...init.headers, "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }

  let response: Response;

  try {
    response = await fetch(API_PREFIX + path, init);
  } catch (cause) {
    // A cancelled request is not a failure to report.
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new NetworkError(cause);
  }

  if (response.status === 204) return undefined as T;

  const text = await response.text();
  const parsed: unknown = text ? safeParse(text) : undefined;

  if (!response.ok) {
    const envelope =
      isRecord(parsed) && isRecord(parsed.error) ? (parsed.error as Partial<ApiErrorBody>) : {};
    throw new ApiError(response.status, envelope);
  }

  return parsed as T;
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/* --- typed helpers over the generated schema ---------------------------- */

type JSONResponse<T> = T extends { content: { "application/json": infer B } } ? B : never;

/** The body of `GET /api/v1/auth/me`. */
export type SessionEnvelope = JSONResponse<
  paths["/api/v1/auth/me"]["get"]["responses"]["200"]
>;

/** The body of `GET /api/v1/auth/providers`. */
export type AuthProviderList = JSONResponse<
  paths["/api/v1/auth/providers"]["get"]["responses"]["200"]
>;

/** The body of `GET /healthz`. */
export type HealthResponse = JSONResponse<paths["/healthz"]["get"]["responses"]["200"]>;

export const api = {
  me: (signal?: AbortSignal) =>
    request<SessionEnvelope>("/auth/me", signal ? { signal } : {}),

  providers: (signal?: AbortSignal) =>
    request<AuthProviderList>("/auth/providers", signal ? { signal } : {}),

  login: (email: string, password: string, organization?: string) =>
    request<SessionEnvelope>("/auth/login", {
      method: "POST",
      body: organization ? { email, password, organization } : { email, password },
    }),

  logout: () => request<void>("/auth/logout", { method: "POST" }),
};
