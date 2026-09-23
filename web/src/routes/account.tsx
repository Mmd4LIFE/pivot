import { createRoute } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { Route as authenticatedRoute } from "./authenticated";
import type { SessionSummary } from "../api/client";
import {
  changePasswordErrorKey,
  useChangePassword,
  useRevokeSession,
  useSession,
  useSessions,
} from "../api/queries";
import { PageHeader } from "../components/shell/AppShell";
import { Alert } from "../ui/Alert";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Card, CardBody, CardDescription, CardHeader, CardTitle } from "../ui/Card";
import { Field } from "../ui/Field";
import { Input } from "../ui/Input";
import { Skeleton } from "../ui/Skeleton";
import { TBody, THead, Table, Td, Th, Tr } from "../ui/Table";

/**
 * Your own account.
 *
 * Three things, in the order somebody needs them: who you are, the password
 * that proves it, and every device that currently claims to be you. The third
 * is the one people come here for after losing a laptop.
 */

export const Route = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/account",
  component: AccountPage,
});

function AccountPage() {
  const { t } = useTranslation();

  return (
    <>
      <PageHeader title={t("account.title")} description={t("account.subtitle")} />

      <div className="flex max-w-2xl flex-col gap-6">
        <Profile />
        <ChangePassword />
        <Sessions />
      </div>
    </>
  );
}

function Profile() {
  const { t } = useTranslation();
  const session = useSession();

  const user = session.data?.user;

  return (
    <Card>
      <CardHeader>
        <CardTitle level={2}>{t("account.profile.heading")}</CardTitle>
        <CardDescription>{t("account.profile.readOnly")}</CardDescription>
      </CardHeader>

      <CardBody>
        {user === undefined ? (
          <Skeleton className="h-16 w-full" />
        ) : (
          /*
            A description list, not a table. These are properties of one thing,
            and `dl` is what says so to a screen reader -- a table would
            announce rows and columns that do not exist.
          */
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
            <dt className="text-content-muted">{t("account.profile.name")}</dt>
            <dd>{user.name}</dd>

            <dt className="text-content-muted">{t("account.profile.email")}</dt>
            <dd>{user.email}</dd>

            <dt className="text-content-muted">{t("account.profile.organization")}</dt>
            <dd>{user.organizationId}</dd>
          </dl>
        )}
      </CardBody>
    </Card>
  );
}

function ChangePassword() {
  const { t } = useTranslation();
  const change = useChangePassword();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    change.mutate(
      { currentPassword: current, newPassword: next },
      {
        // Cleared on success only. Leaving them filled after a failure means
        // somebody who mistyped the current password does not retype the new
        // one they had just chosen.
        onSuccess: () => {
          setCurrent("");
          setNext("");
        },
      },
    );
  }

  const errorKey = change.error === null ? null : changePasswordErrorKey(change.error);

  return (
    <Card>
      <CardHeader>
        <CardTitle level={2}>{t("account.password.heading")}</CardTitle>
        <CardDescription>{t("account.password.description")}</CardDescription>
      </CardHeader>

      <CardBody>
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          {errorKey !== null && (
            <Alert tone="danger" live>
              {t(errorKey)}
            </Alert>
          )}

          {change.isSuccess && change.error === null && (
            // `live`, because the form clears itself on success and a silent
            // reset looks like nothing happened.
            <Alert tone="success" live>
              {t("account.password.changed")}
            </Alert>
          )}

          <Field label={t("account.password.current")} required>
            <Input
              type="password"
              name="currentPassword"
              autoComplete="current-password"
              required
              value={current}
              onChange={(event) => setCurrent(event.target.value)}
            />
          </Field>

          <Field
            label={t("account.password.new")}
            description={t("account.password.newHelp")}
            required
          >
            <Input
              type="password"
              name="newPassword"
              autoComplete="new-password"
              required
              minLength={12}
              value={next}
              onChange={(event) => setNext(event.target.value)}
            />
          </Field>

          <div>
            <Button type="submit" loading={change.isPending}>
              {change.isPending ? t("account.password.submitting") : t("account.password.submit")}
            </Button>
          </div>
        </form>
      </CardBody>
    </Card>
  );
}

function Sessions() {
  const { t } = useTranslation();

  const sessions = useSessions();
  const revoke = useRevokeSession();

  return (
    <Card>
      <CardHeader>
        <CardTitle level={2}>{t("account.sessions.heading")}</CardTitle>
        <CardDescription>{t("account.sessions.description")}</CardDescription>
      </CardHeader>

      <CardBody>
        {revoke.isError && (
          <Alert tone="danger" live className="mb-4">
            {t("account.sessions.endFailed")}
          </Alert>
        )}

        {sessions.data === undefined ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <Table caption={t("account.sessions.description")} captionHidden>
            <THead>
              <Tr>
                <Th>{t("account.sessions.heading")}</Th>
                <Th>{t("account.sessions.lastSeen")}</Th>
                <Th>{t("account.sessions.expires")}</Th>
                <Th>
                  {/* The action column's header is present but unlabeled on
                      purpose: a visible "Actions" heading is noise, and the
                      buttons below name themselves. */}
                  <span className="sr-only">{t("account.sessions.end")}</span>
                </Th>
              </Tr>
            </THead>

            <TBody>
              {sessions.data.sessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  onEnd={() => revoke.mutate(session.id)}
                  ending={revoke.isPending && revoke.variables === session.id}
                />
              ))}
            </TBody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}

function SessionRow({
  session,
  onEnd,
  ending,
}: {
  session: SessionSummary;
  onEnd: () => void;
  ending: boolean;
}) {
  const { t } = useTranslation();

  return (
    <Tr>
      <Td>
        <div className="flex items-center gap-2">
          <span className="truncate">
            {session.userAgent ?? t("account.sessions.unknownDevice")}
          </span>

          {session.current && <Badge tone="accent">{t("account.sessions.current")}</Badge>}
        </div>

        {session.ip !== undefined && (
          <span className="text-xs text-content-muted">{session.ip}</span>
        )}
      </Td>

      <Td className="whitespace-nowrap text-content-muted">
        {formatWhen(session.lastSeenAt)}
      </Td>

      <Td className="whitespace-nowrap text-content-muted">
        {formatWhen(session.absoluteExpiresAt)}
      </Td>

      <Td className="text-end">
        {session.current ? (
          // No button on this row. Ending your own session from a list of
          // devices is a logout by surprise; Sign out is where that lives, and
          // it is one menu away.
          <span className="text-xs text-content-muted">{t("account.sessions.endCurrent")}</span>
        ) : (
          <Button variant="secondary" onClick={onEnd} loading={ending}>
            {ending ? t("account.sessions.ending") : t("account.sessions.end")}
          </Button>
        )}
      </Td>
    </Tr>
  );
}

/**
 * A timestamp somebody can read.
 *
 * The browser's own locale and time zone, because a session list is answering
 * "was that me, an hour ago?" and UTC makes that arithmetic.
 */
function formatWhen(iso: string): string {
  const when = new Date(iso);

  if (Number.isNaN(when.getTime())) return iso;

  return when.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
