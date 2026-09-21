import { useState } from "react";
import { applyTheme, storedTheme, type Theme } from "../lib/theme";

const OPTIONS: { value: Theme; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

/**
 * Theme switcher.
 *
 * It exists in Part 9 rather than Part 10 for one reason: it is the smallest
 * thing that proves the token indirection actually works at runtime. If
 * switching required a rebuild, Phase 8's white-label embedding would be
 * impossible, and this catches that immediately rather than two phases later.
 */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => storedTheme());

  function choose(next: Theme) {
    setTheme(next);
    applyTheme(next);
  }

  return (
    <fieldset className="flex items-center gap-1 rounded-token border border-line p-1">
      <legend className="sr-only">Theme</legend>

      {OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={theme === option.value}
          onClick={() => choose(option.value)}
          className={
            "rounded-token-sm px-2 py-1 text-sm " +
            (theme === option.value
              ? "bg-accent text-accent-content"
              : "text-content-muted hover:bg-surface-sunken")
          }
        >
          {option.label}
        </button>
      ))}
    </fieldset>
  );
}
