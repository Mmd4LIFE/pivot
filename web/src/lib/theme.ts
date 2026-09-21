/**
 * Theme selection.
 *
 * The theme is an attribute on <html>, and the tokens in styles/tokens.css key
 * off it. Switching is therefore a single attribute write with no rebuild and
 * no re-render -- which is the same mechanism Phase 8's white-label embedding
 * uses, exercised here so it cannot quietly stop working.
 */

export type Theme = "light" | "dark" | "system";

const STORAGE_KEY = "pivot-theme";

/** Read the stored preference. Absent or unreadable storage means "system". */
export function storedTheme(): Theme {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "light" || value === "dark") return value;
  } catch {
    // Private mode, or storage blocked by policy. The system preference
    // applies, which is a reasonable answer rather than a failure.
  }

  return "system";
}

/** Apply a theme and remember it. */
export function applyTheme(theme: Theme): void {
  const resolved = theme === "system" ? systemTheme() : theme;

  document.documentElement.dataset.theme = resolved;

  try {
    if (theme === "system") {
      localStorage.removeItem(STORAGE_KEY);
    } else {
      localStorage.setItem(STORAGE_KEY, theme);
    }
  } catch {
    // The attribute is already set, so the theme applies for this page even
    // when the preference cannot be persisted.
  }
}

function systemTheme(): "light" | "dark" {
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}
