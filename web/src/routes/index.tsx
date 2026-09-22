import { createRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import { useSession } from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Badge } from "../ui/Badge";
import { Card, CardBody, CardHeader, CardTitle } from "../ui/Card";
import { Separator } from "../ui/Separator";

/**
 * The start page.
 *
 * What it shows is the only thing Phase 0 actually knows about you: who you
 * are, what you are allowed to do, and when this session ends. That is not a
 * placeholder — the effective permission list comes from the same checker the
 * middleware asks, so an administrator can see at a glance what a role really
 * grants rather than reading the model file.
 */
export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  component: Home,
});

function Home() {
  const { t } = useTranslation();

  const session = useSession();

  const user = session.data?.user;
  const permissions = session.data?.permissions ?? [];
  const expires = session.data?.session.absoluteExpiresAt;

  return (
    <>
      <PageHeader
        title={t("pages.home.title")}
        description={user === undefined ? undefined : t("pages.home.welcome", { name: user.name })}
      />

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle level={2}>{t("pages.home.permissionsHeading")}</CardTitle>
          </CardHeader>

          <CardBody>
            {permissions.length === 0 ? (
              <p className="text-sm text-content-muted">{t("pages.home.noPermissions")}</p>
            ) : (
              <ul className="flex flex-wrap gap-2">
                {permissions.map((permission) => (
                  <li key={permission}>
                    <Badge tone="accent">{permission}</Badge>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle level={2}>{t("pages.home.sessionHeading")}</CardTitle>
          </CardHeader>

          <CardBody className="flex flex-col gap-2 text-sm">
            {user !== undefined && (
              <p>{t("pages.home.signedInAs", { email: user.email })}</p>
            )}

            {expires !== undefined && (
              <>
                <Separator />
                {/*
                  The absolute cap, not the sliding idle timeout: this is the
                  one that is never extended, so it is the one that answers
                  "when will I have to sign in again".
                */}
                <p className="text-content-muted">
                  {t("pages.home.expires", { when: formatWhen(expires) })}
                </p>
              </>
            )}
          </CardBody>
        </Card>
      </div>
    </>
  );
}

/** A timestamp in the reader's own locale and time zone. */
function formatWhen(iso: string): string {
  const at = new Date(iso);

  if (Number.isNaN(at.getTime())) return iso;

  return at.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
