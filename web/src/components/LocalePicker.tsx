import { useTranslation } from "react-i18next";

import { availableLocales, changeLocale, directionOf, RTL_PSEUDO_LOCALE } from "../i18n";

/**
 * Switches language, and with it the direction of the whole interface.
 *
 * Visible only where there is more than one locale to pick, which today means
 * development: the second entry is a pseudo-locale carrying the English
 * strings and declaring itself right-to-left, so the mirrored layout can be
 * checked without inventing translations nobody here can read.
 *
 * It exists in Phase 0 because `dir="rtl"` has to be one attribute away from
 * the first day. Retrofitting it is a multi-week job once four hundred strings
 * and a stylesheet full of `margin-left` have been written.
 */
export function LocalePicker() {
  const { i18n, t } = useTranslation();

  const locales = availableLocales();

  if (locales.length < 2) return null;

  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="sr-only">{t("locale.label")}</span>

      <select
        value={i18n.language}
        onChange={(event) => {
          void changeLocale(event.target.value);
        }}
        className="h-9 rounded-token border border-line-strong bg-surface px-2 text-sm text-content"
      >
        {locales.map((locale) => (
          <option key={locale} value={locale}>
            {locale === RTL_PSEUDO_LOCALE
              ? `${locale} (${directionOf(locale)})`
              : locale}
          </option>
        ))}
      </select>
    </label>
  );
}
