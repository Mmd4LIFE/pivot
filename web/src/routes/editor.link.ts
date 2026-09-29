/**
 * A question, carried in a link.
 *
 * Paste the URL to somebody and they open the same source with the same
 * statement in front of them. It is the cheapest form of sharing there is:
 * nothing is saved, nothing is named, and nothing has to be cleaned up
 * afterwards -- which is what makes it the one people actually use. Saved
 * questions are Phase 2's and are a different thing with a different
 * lifecycle.
 *
 * **In the fragment, never the query string.** A URL fragment is not sent to
 * the server: it stays out of access logs, out of proxy logs, and out of the
 * `Referer` header of every request the page subsequently makes. A statement
 * can name tables, columns and filter values that are themselves sensitive, so
 * a query string would sprinkle it across infrastructure that has no business
 * holding it. Metabase does the same, and this is the reason.
 *
 * **A link carries the question, not the answer.** Whoever opens it still has
 * to be signed in, still needs `native_query`, and still needs access to that
 * connection -- the pipeline decides, exactly as it does for anything typed by
 * hand. Sharing a link is not sharing data.
 */

/** What a link carries. */
export interface SharedQuestion {
  /**
   * The format version.
   *
   * Present from the first release so that a later change has something to
   * branch on. A link somebody bookmarked or pasted into a chat outlives the
   * code that made it, and the alternative to a version is guessing.
   */
  v: 1;

  connectionId: string;
  sql: string;
  title?: string;
}

/** Encode a question for the fragment. */
export function encodeQuestion(question: Omit<SharedQuestion, "v">): string {
  const payload: SharedQuestion = { v: 1, ...question };

  return base64urlEncode(JSON.stringify(payload));
}

/**
 * Decode a fragment, or return null.
 *
 * Null for anything that is not a question this version understands: a
 * truncated paste, a link from a later release, somebody's own hash. A URL is
 * the least trustworthy input a frontend takes -- it arrives from chat clients
 * that wrap lines, from email that rewrites links, and from people editing it
 * by hand -- so nothing here throws, and the editor falls back to a normal
 * empty tab.
 */
export function decodeQuestion(fragment: string): SharedQuestion | null {
  const cleaned = fragment.replace(/^#/, "").trim();
  if (cleaned === "") return null;

  try {
    const parsed: unknown = JSON.parse(base64urlDecode(cleaned));

    if (typeof parsed !== "object" || parsed === null) return null;

    const question = parsed as Partial<SharedQuestion>;

    if (question.v !== 1) return null;
    if (typeof question.connectionId !== "string" || question.connectionId === "") return null;
    if (typeof question.sql !== "string") return null;

    return {
      v: 1,
      connectionId: question.connectionId,
      sql: question.sql,
      ...(typeof question.title === "string" ? { title: question.title } : {}),
    };
  } catch {
    return null;
  }
}

/**
 * Base64, in the URL-safe alphabet and through UTF-8.
 *
 * `btoa` takes a Latin-1 string and throws on anything above U+00FF, so
 * encoding a statement with an accented identifier or a non-Latin string
 * literal in it would fail -- and a share button that works until somebody
 * queries a column named `città` is worse than none. The text is encoded to
 * bytes first.
 *
 * `+/` become `-_` and the padding goes, because the result lives in a URL
 * that people paste into chat clients, and `+` in particular is the character
 * most likely to come back as a space.
 */
function base64urlEncode(text: string): string {
  const bytes = new TextEncoder().encode(text);

  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);

  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function base64urlDecode(encoded: string): string {
  const padded = encoded.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(padded);

  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);

  return new TextDecoder().decode(bytes);
}

/**
 * The link for a question, absolute so it can be pasted anywhere.
 *
 * Built from the page's own origin rather than anything configured: whatever
 * host somebody is using is the host their colleague can reach, and a
 * configured base URL is exactly the setting that is wrong on the instance
 * running behind a proxy nobody documented.
 */
export function questionLink(question: Omit<SharedQuestion, "v">, origin: string): string {
  return `${origin}/editor#${encodeQuestion(question)}`;
}

/**
 * How long a link may get before it is not one.
 *
 * Browsers handle far more than this, but a link is for pasting into chat and
 * email, and those truncate. Past this the share button says so rather than
 * handing somebody a URL that arrives broken at the other end.
 */
export const MAX_LINK_LENGTH = 8000;
