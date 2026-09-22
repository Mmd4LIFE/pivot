import { createRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { useLogout, useSession } from "../api/queries";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Card, CardBody, CardHeader, CardTitle } from "../ui/Card";
import { ThemeToggle } from "../components/ThemeToggle";
import { LocalePicker } from "../components/LocalePicker";

/**
 * The start page.
 *
 * Still thin. Part 11-b replaces it with the application shell — navigation,
 * breadcrumbs, a user menu, the command palette. What it proves today is that
 * the whole chain works end to end: the guard let us in, the session is real,
 * the design system renders, every string comes from the catalog, and sign-out
 * returns to the login page.
 */
export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  component: Home,
});

function Home() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const session = useSession();
  const logout = useLogout();

  function signOut() {
    logout.mutate(undefined, {
      onSettled: () => {
        void navigate({ to: "/login", replace: true });
      },
    });
  }

  const user = session.data?.user;

  return (
    <main className="mx-auto flex min-h-screen max-w-2xl flex-col gap-8 p-6">
      <header className="flex items-start justify-between gap-4 border-b border-line pb-4">
        <div>
          <h1 className="text-2xl font-semibold">{t("app.name")}</h1>
          <p className="text-sm text-content-muted">{t("app.tagline")}</p>
        </div>

        <div className="flex items-center gap-2">
          <LocalePicker />
          <ThemeToggle />
        </div>
      </header>

      <Card>
        <CardHeader>
          <CardTitle level={2}>{t("session.heading")}</CardTitle>
        </CardHeader>

        <CardBody className="flex flex-col gap-3">
          {user === undefined ? (
            <p className="text-content-subtle">{t("session.checking")}</p>
          ) : (
            <>
              <p className="text-sm">{t("auth.signedInAs", { email: user.email })}</p>

              <div className="flex flex-wrap gap-2">
                {(session.data?.permissions ?? []).map((permission) => (
                  <Badge key={permission} tone="neutral">
                    {permission}
                  </Badge>
                ))}
              </div>
            </>
          )}

          <div>
            <Button variant="secondary" loading={logout.isPending} onClick={signOut}>
              {t("auth.signOut")}
            </Button>
          </div>
        </CardBody>
      </Card>
    </main>
  );
}
