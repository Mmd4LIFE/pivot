import { Link, createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { Route as rootRoute } from "./root";
import { setupErrorKey, setupStatusQuery, useSetup, useSetupStatus } from "../api/queries";
import { DEFAULT_DESTINATION } from "../lib/redirect";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Field } from "../ui/Field";
import { Input } from "../ui/Input";
import { ThemeToggle } from "../components/ThemeToggle";

/**
 * The first run.
 *
 * This is the page a person sees before Pivot has any accounts at all, and the
 * only one that can be reached without one. It asks for four things and a
 * token, creates the organization and the administrator, and signs them in —
 * so the last step of installing Pivot is being inside it, not being at a
 * login form with an account you just made.
 */

export const Route = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",

  // An instance that is already claimed has nothing to set up, and leaving the
  // form reachable would mean somebody filling it in to be told no. The guard
  // is not a security control -- the server refuses regardless, which is where
  // that has to live -- it is there so the page is never a dead end.
  beforeLoad: async ({ context }) => {
    const status = await context.queryClient.ensureQueryData(setupStatusQuery);

    if (status.initialized) {
      throw redirect({ to: "/login", replace: true });
    }
  },

  component: SetupPage,
});

function SetupPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const status = useSetupStatus();
  const setup = useSetup();

  const [organization, setOrganization] = useState("");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");

  // Already claimed while this tab had the page open -- somebody else got
  // there first, or the same person in another window. Rendered rather than
  // redirected, because a page that disappears under somebody mid-typing is
  // worse than one that explains itself.
  if (status.data?.initialized === true) {
    return (
      <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-6 p-6">
        <Alert tone="info">{t("setup.alreadyDone")}</Alert>

        <Button asChild className="w-full">
          <Link to="/login">{t("setup.goToSignIn")}</Link>
        </Button>
      </main>
    );
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    setup.mutate(
      { organization, name, email, password, token },
      {
        // `replace`, so Back does not return to a setup page that can no
        // longer do anything.
        onSuccess: () => void navigate({ to: DEFAULT_DESTINATION, replace: true }),
      },
    );
  }

  const errorKey = setup.error === null ? null : setupErrorKey(setup.error);

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold">{t("setup.heading")}</h1>
        <p className="text-sm text-content-muted">{t("setup.subheading")}</p>
      </header>

      {errorKey !== null && (
        <Alert tone="danger" live>
          {t(errorKey)}
        </Alert>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Field label={t("setup.organization")} description={t("setup.organizationHelp")} required>
          <Input
            name="organization"
            autoComplete="organization"
            required
            autoFocus
            value={organization}
            onChange={(event) => setOrganization(event.target.value)}
          />
        </Field>

        <Field label={t("setup.name")}>
          <Input
            name="name"
            autoComplete="name"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>

        <Field label={t("setup.email")} required>
          <Input
            type="email"
            name="email"
            // `username`, not `email`: this is the field a password manager
            // needs to pair with the new password below in order to offer to
            // save both.
            autoComplete="username"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </Field>

        <Field label={t("setup.password")} description={t("setup.passwordHelp")} required>
          <Input
            type="password"
            name="password"
            autoComplete="new-password"
            required
            minLength={12}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Field>

        {/*
          Only when the server says it wants one. An instance provisioned with
          PIVOT_SETUP_TOKEN unset still generates a token, so this is almost
          always shown -- but a deployment that deliberately runs without one
          should not be asked for something it does not have.
        */}
        {status.data?.tokenRequired === true && (
          <Field label={t("setup.token")} description={t("setup.tokenHelp")} required>
            <Input
              name="token"
              // Not a password field. It is meant to be copied from a terminal
              // and checked against what is on screen, and masking it only
              // makes a paste error impossible to see.
              autoComplete="off"
              spellCheck={false}
              required
              value={token}
              onChange={(event) => setToken(event.target.value)}
            />
          </Field>
        )}

        <Button type="submit" loading={setup.isPending} className="w-full">
          {setup.isPending ? t("setup.submitting") : t("setup.submit")}
        </Button>
      </form>

      <footer className="flex justify-center">
        <ThemeToggle />
      </footer>
    </main>
  );
}
