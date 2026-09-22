import { beforeEach, describe, expect, test } from "vitest";

import { en } from "./en";
import {
  DEFAULT_LOCALE,
  RTL_PSEUDO_LOCALE,
  applyDirection,
  changeLocale,
  directionOf,
  i18next,
  initI18n,
} from "./index";

/*
 * Translation, and the direction that comes with it.
 *
 * The direction tests are the load-bearing ones. `dir` on <html> is the single
 * attribute every logical property in the stylesheet resolves against, so if
 * it stops being written the entire right-to-left layout silently reverts —
 * and reverts to something that still looks fine to anyone reading English.
 */

beforeEach(async () => {
  await initI18n(DEFAULT_LOCALE);
});

describe("direction", () => {
  test("knows the right-to-left languages, with or without a region", () => {
    for (const locale of ["ar", "ar-EG", "he", "he-IL", "fa", "ur", "ckb"]) {
      expect(directionOf(locale)).toBe("rtl");
    }
  });

  test("everything else reads left to right", () => {
    for (const locale of ["en", "en-GB", "de", "ja", "tr"]) {
      expect(directionOf(locale)).toBe("ltr");
    }
  });

  test("the pseudo-locale is right to left", () => {
    // Its entire job: prove the mirrored layout without inventing
    // translations nobody here can read.
    expect(directionOf(RTL_PSEUDO_LOCALE)).toBe("rtl");
  });
});

describe("the document", () => {
  test("carries the language and the direction", () => {
    applyDirection("ar");

    expect(document.documentElement.lang).toBe("ar");
    expect(document.documentElement.dir).toBe("rtl");

    applyDirection("en");

    expect(document.documentElement.lang).toBe("en");
    expect(document.documentElement.dir).toBe("ltr");
  });

  test("the pseudo-locale reads as English but draws right to left", () => {
    applyDirection(RTL_PSEUDO_LOCALE);

    // `lang` tells a screen reader which voice to use. Claiming this is
    // anything but English would have it read English strings in the wrong
    // voice, which is unintelligible.
    expect(document.documentElement.lang).toBe(DEFAULT_LOCALE);
    expect(document.documentElement.dir).toBe("rtl");
  });

  test("changing locale moves both", async () => {
    await changeLocale(RTL_PSEUDO_LOCALE);
    expect(document.documentElement.dir).toBe("rtl");

    await changeLocale(DEFAULT_LOCALE);
    expect(document.documentElement.dir).toBe("ltr");
  });
});

describe("the catalog", () => {
  test("resolves a nested key", () => {
    expect(i18next.t("auth.signIn")).toBe(en.auth.signIn);
  });

  test("interpolates without escaping", () => {
    // React escapes what it renders, so i18next escaping too would turn an
    // apostrophe into `&#39;` on the page.
    expect(i18next.t("auth.signedInAs", { email: "a&b@example.com" })).toContain(
      "a&b@example.com",
    );
  });

  test("every string is reachable, and none is empty", () => {
    // Catches a key added to the catalog as `""` -- which renders as nothing
    // and looks like a layout bug rather than a missing string.
    for (const [path, value] of flatten(en)) {
      expect(value, `${path} is empty`).not.toBe("");
      expect(i18next.t(path as "auth.signIn")).toBe(value);
    }
  });
});

/** Every leaf in the catalog, as [dotted.path, string]. */
function flatten(node: unknown, prefix = ""): [string, string][] {
  if (typeof node === "string") return [[prefix, node]];

  if (typeof node !== "object" || node === null) return [];

  return Object.entries(node).flatMap(([key, value]) =>
    flatten(value, prefix === "" ? key : `${prefix}.${key}`),
  );
}
