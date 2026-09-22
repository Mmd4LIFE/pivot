import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useLogout } from "../../api/queries";
import {
  CommandAction,
  CommandGroupItems,
  CommandPalette,
  useCommandPaletteHotkey,
} from "../../ui/CommandPalette";
import { NAVIGATION } from "./navigation";
import { Search, SignOut } from "./glyphs";
import { Button } from "../../ui/Button";

/**
 * Search, and jump.
 *
 * The palette is a shortcut and never the only route: everything in it is also
 * in the sidebar. Someone who does not know it exists must still be able to
 * use the product, and it does not exist at all on a touch device.
 *
 * Which is why the button is here too. A hotkey nobody can see is a feature
 * for the people who already knew about it.
 */
export function CommandMenu() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const logout = useLogout();

  const [open, setOpen] = useState(false);

  useCommandPaletteHotkey(() => setOpen(true));

  function go(to: (typeof NAVIGATION)[number]["to"]) {
    setOpen(false);
    void navigate({ to });
  }

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => setOpen(true)}
        className="gap-2 text-content-muted"
      >
        <Search />
        <span className="hidden sm:inline">{t("nav.search")}</span>

        {/*
          Decorative: the button already has a name, and "Search Ctrl K" read
          aloud is worse than "Search". Sighted users are the ones who need to
          be told the shortcut exists.
        */}
        <kbd
          aria-hidden="true"
          className="hidden rounded-token-sm border border-line px-1.5 py-0.5 text-xs md:inline"
        >
          {t("nav.searchHint")}
        </kbd>
      </Button>

      <CommandPalette
        open={open}
        onOpenChange={setOpen}
        label={t("command.label")}
        placeholder={t("command.placeholder")}
        empty={t("command.empty")}
      >
        <CommandGroupItems heading={t("command.goTo")} className={HEADING}>
          {NAVIGATION.map((item) => (
            <CommandAction
              key={item.to}
              // The keywords ride along in the searchable value, so "warehouse"
              // finds Connections. cmdk matches on this string, not on what is
              // rendered.
              value={`${t(item.titleKey)} ${item.keywords ?? ""}`}
              onSelect={() => go(item.to)}
            >
              <item.icon />
              {t(item.titleKey)}
            </CommandAction>
          ))}
        </CommandGroupItems>

        <CommandGroupItems heading={t("command.actions")} className={HEADING}>
          <CommandAction
            value={t("auth.signOut")}
            onSelect={() => {
              setOpen(false);
              logout.mutate(undefined, {
                onSettled: () => {
                  void navigate({ to: "/login", replace: true });
                },
              });
            }}
          >
            <SignOut />
            {t("auth.signOut")}
          </CommandAction>
        </CommandGroupItems>
      </CommandPalette>
    </>
  );
}

// cmdk names its group heading with a data attribute rather than a class, so
// the heading is styled through it.
const HEADING =
  "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 " +
  "[&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium " +
  "[&_[cmdk-group-heading]]:text-content-muted";
