import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useLogout, useSession } from "../../api/queries";
import { availableLocales, changeLocale, directionOf, RTL_PSEUDO_LOCALE } from "../../i18n";
import { applyTheme, storedTheme, type Theme } from "../../lib/theme";
import { Avatar } from "../../ui/Avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../ui/DropdownMenu";
import { Person, SignOut } from "./glyphs";

/**
 * The account menu.
 *
 * Theme and language live here rather than in the page, because they are
 * properties of the person and not of what they are looking at. Both take
 * effect immediately and neither rebuilds anything — which is the same runtime
 * token mechanism Phase 8's white-label embedding depends on, exercised where
 * someone will notice if it stops working.
 */
export function UserMenu() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();

  const session = useSession();
  const logout = useLogout();

  const user = session.data?.user;
  const locales = availableLocales();

  function signOut() {
    logout.mutate(undefined, {
      onSettled: () => {
        void navigate({ to: "/login", replace: true });
      },
    });
  }

  return (
    /*
      Not modal. Radix's modal menu marks everything outside it `aria-hidden`,
      which leaves the skip link and the whole sidebar focusable inside an
      aria-hidden subtree -- axe fails that as `aria-hidden-focus`, and it is
      right to: the markup says "nothing here" about elements that still exist.
      Focus is trapped in practice, so nobody could reach them, but a menu is
      not a dialog and does not need the page behind it hidden at all.
    */
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        className="flex items-center gap-2 rounded-token p-1 hover:bg-surface-sunken"
        // The trigger is an avatar, so it needs a name of its own. Without
        // this a screen reader announces "button" and nothing else.
        aria-label={t("nav.account")}
      >
        <Avatar name={user?.name ?? "?"} src={user?.avatarUrl} className="size-7" />
      </DropdownMenuTrigger>

      <DropdownMenuContent align="end" className="min-w-56">
        {user !== undefined && (
          <>
            <div className="px-2 py-1.5">
              <p className="truncate text-sm font-medium text-content">{user.name}</p>
              <p className="truncate text-xs text-content-muted">{user.email}</p>
            </div>

            <DropdownMenuSeparator />
          </>
        )}

        <DropdownMenuLabel>{t("theme.label")}</DropdownMenuLabel>
        <ThemeChoices />

        {locales.length > 1 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>{t("locale.label")}</DropdownMenuLabel>

            <DropdownMenuRadioGroup
              value={i18n.language}
              onValueChange={(value) => {
                void changeLocale(value);
              }}
            >
              {locales.map((locale) => (
                <DropdownMenuRadioItem key={locale} value={locale}>
                  {locale === RTL_PSEUDO_LOCALE
                    ? `${locale} (${directionOf(locale)})`
                    : locale}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </>
        )}

        <DropdownMenuSeparator />

        <DropdownMenuItem
          onSelect={() => {
            void navigate({ to: "/account" });
          }}
          className="gap-2"
        >
          <Person />
          {t("nav.account")}
        </DropdownMenuItem>

        <DropdownMenuItem onSelect={signOut} className="gap-2">
          <SignOut />
          {t("auth.signOut")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const THEMES: { value: Theme; labelKey: "theme.light" | "theme.dark" | "theme.system" }[] = [
  { value: "light", labelKey: "theme.light" },
  { value: "dark", labelKey: "theme.dark" },
  { value: "system", labelKey: "theme.system" },
];

function ThemeChoices() {
  const { t } = useTranslation();

  // Seeded from the stored preference and held here after that. The attribute
  // on <html> is what actually styles the page; this is only the radio's idea
  // of which row is checked, and without it the tick never moves.
  const [current, setCurrent] = useState<Theme>(() => storedTheme());

  return (
    <DropdownMenuRadioGroup
      value={current}
      onValueChange={(value) => {
        const theme = value as Theme;

        setCurrent(theme);
        applyTheme(theme);
      }}
    >
      {THEMES.map((theme) => (
        <DropdownMenuRadioItem key={theme.value} value={theme.value}>
          {t(theme.labelKey)}
        </DropdownMenuRadioItem>
      ))}
    </DropdownMenuRadioGroup>
  );
}
