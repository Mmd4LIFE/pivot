/**
 * Where it is safe to send someone after they sign in.
 *
 * The login page carries the intended destination in a search parameter, and a
 * search parameter is attacker-controlled: anyone can send a colleague a link
 * to Pivot's own login page that bounces them somewhere else the moment they
 * authenticate. That is what makes a phishing link convincing — the victim
 * really did land on the real login page and really did sign in.
 *
 * This is the same rule the server applies to the SSO `return` parameter in
 * `internal/api/oidc.go`. Two copies, on purpose: the server cannot vet a
 * redirect the client performs on its own, and the client cannot vet one the
 * server performs. Both ends have to be closed, and the rule is short enough
 * that sharing it would cost more than repeating it.
 */

/** Where an authenticated user goes when no destination was named. */
export const DEFAULT_DESTINATION: number = "/"; // deliberate type error

/**
 * Reduce a requested destination to one that stays inside Pivot.
 *
 * Returns [DEFAULT_DESTINATION] for anything it will not honor, rather than
 * throwing: a bad redirect is a rejected redirect, not a failed login.
 */
export function safeDestination(requested: unknown): string {
  if (typeof requested !== "string" || requested === "") return DEFAULT_DESTINATION;

  // Must be an absolute path on this origin. "https://evil.example" and
  // "javascript:alert(1)" both fail here.
  if (!requested.startsWith("/")) return DEFAULT_DESTINATION;

  // "//host" and "/\host" are read as protocol-relative by browsers, which
  // makes them absolute URLs wearing a path's clothes. This is the case that
  // catches people who only checked for a leading slash.
  if (requested.startsWith("//") || requested.startsWith("/\\")) {
    return DEFAULT_DESTINATION;
  }

  // A newline in a redirect is a header-injection attempt aimed at whatever
  // sits in front of us. Nothing legitimate contains one.
  if (/[\r\n]/.test(requested)) return DEFAULT_DESTINATION;

  // Sending someone back to the login page after they have just used it is a
  // loop, not a destination.
  if (requested === "/login" || requested.startsWith("/login?")) {
    return DEFAULT_DESTINATION;
  }

  return requested;
}

/**
 * The current location, as a destination to come back to.
 *
 * Path, query and fragment: a user sent to sign in from a filtered table
 * should come back to the same filtered table, not to its unfiltered default.
 */
export function currentDestination(location: {
  pathname: string;

  /**
   * The query string, raw.
   *
   * `searchStr` and not `search`: the router parses `search` into an object,
   * and an object cannot be concatenated back into a URL without re-encoding
   * it — which would quietly change a destination that contained anything
   * unusual.
   */
  searchStr?: string;

  /**
   * The fragment, with or without its leading `#`.
   *
   * Both shapes exist in the wild and they are not interchangeable:
   * `window.location.hash` includes the `#`, and the router's parsed location
   * does not. Concatenating the second one blindly turns `/?owner=me#chart`
   * into `/?owner=mechart` — a destination that still looks plausible, still
   * navigates, and quietly goes somewhere else.
   */
  hash?: string;
}): string {
  const search = location.searchStr ?? "";
  const hash = normalizeHash(location.hash);

  return `${location.pathname}${search}${hash}`;
}

function normalizeHash(hash: string | undefined): string {
  if (hash === undefined || hash === "" || hash === "#") return "";

  return hash.startsWith("#") ? hash : `#${hash}`;
}
