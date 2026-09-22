import i18next from "i18next";
import { initReactI18next } from "react-i18next";

import { en } from "./en";

/**
 * Translation, and the direction that comes with it.
 *
 * Set up in Phase 0 rather than "when we internationalize", because the
 * expensive part of internationalization is never the library — it is the four
 * hundred strings already written inline, and the layout built on `margin-left`
 * that has to be rewritten for Arabic. Both are cheap now and neither is later.
 *
 * Direction is part of the locale, not a separate setting. A user whose locale
 * is Arabic gets a right-to-left layout because that is what Arabic is, and
 * `dir` on <html> is what makes every logical property in the stylesheet
 * resolve the other way round. That is why the components use `ms-*` and
 * `ps-*` rather than `ml-*` and `pl-*` — the whole mirror is this one
 * attribute.
 */

export const DEFAULT_LOCALE = "en";

/**
 * A development-only pseudo-locale.
 *
 * It carries the English strings and declares itself right-to-left, so the
 * layout can be checked without inventing translations nobody here can read —
 * fabricated Arabic would be worse than none, because it looks finished.
 *
 * `import.meta.env.DEV` keeps it out of the production bundle entirely.
 */
export const RTL_PSEUDO_LOCALE = "en-RTL";

/** The locales offered, in the order a picker should list them. */
export function availableLocales(): readonly string[] {
  return import.meta.env.DEV ? [DEFAULT_LOCALE, RTL_PSEUDO_LOCALE] : [DEFAULT_LOCALE];
}

/**
 * The scripts that run right to left.
 *
 * Matched on the language subtag, so `ar-EG` and `ar` both resolve — a locale
 * is a language plus a region, and the direction belongs to the language.
 */
const RTL_LANGUAGES = new Set(["ar", "he", "fa", "ur", "yi", "dv", "ps", "ckb"]);

export type Direction = "ltr" | "rtl";

/** Which way a locale reads. */
export function directionOf(locale: string): Direction {
  if (locale === RTL_PSEUDO_LOCALE) return "rtl";

  const language = locale.toLowerCase().split("-")[0] ?? "";

  return RTL_LANGUAGES.has(language) ? "rtl" : "ltr";
}

const resources = {
  [DEFAULT_LOCALE]: { translation: en },
  [RTL_PSEUDO_LOCALE]: { translation: en },
};

/** Start i18next. Safe to call more than once; the second call is a no-op. */
export async function initI18n(locale: string = DEFAULT_LOCALE): Promise<void> {
  if (i18next.isInitialized) {
    await changeLocale(locale);

    return;
  }

  await i18next.use(initReactI18next).init({
    resources,
    lng: locale,
    fallbackLng: DEFAULT_LOCALE,
    defaultNS: "translation",

    interpolation: {
      // React escapes everything it renders already, and i18next's own
      // escaping would double-encode an apostrophe into `&#39;` on the page.
      escapeValue: false,
    },

    // A missing key should be loud in development and invisible in
    // production: a user should never read `auth.signIn` off the screen.
    returnNull: false,
    parseMissingKeyHandler: (key) => (import.meta.env.DEV ? `⟦${key}⟧` : ""),
  });

  applyDirection(locale);
}

/** Switch locale, and the document's language and direction with it. */
export async function changeLocale(locale: string): Promise<void> {
  await i18next.changeLanguage(locale);

  applyDirection(locale);
}

/**
 * Write `lang` and `dir` onto <html>.
 *
 * `lang` is not decoration: it tells a screen reader which voice to use, and
 * a page of English read in a Spanish voice is unintelligible. `dir` is what
 * flips the layout.
 */
export function applyDirection(locale: string): void {
  const root = document.documentElement;

  // The pseudo-locale is still English to a screen reader, whatever direction
  // it is drawn in. Claiming otherwise would make it read the strings in the
  // wrong voice.
  root.lang = locale === RTL_PSEUDO_LOCALE ? DEFAULT_LOCALE : locale;
  root.dir = directionOf(locale);
}

export { i18next };

/**
 * Typed keys.
 *
 * This is what turns `t("auth.singIn")` from a string that renders as itself
 * into a compile error. The catalog is the type.
 */
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: { translation: typeof en };
  }
}
