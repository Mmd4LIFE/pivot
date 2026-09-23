import { createRoute, redirect, useNavigate, useSearch } from "@tanstack/react-router";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { Route as rootRoute } from "./root";
import { ApiError } from "../api/client";
import {
  loginErrorKey,
  sessionQuery,
  setupStatusQuery,
  useAuthProviders,
  useLogin,
  type LoginErrorKey,
} from "../api/queries";
import { DEFAULT_DESTINATION, safeDestination } from "../lib/redirect";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Field } from "../ui/Field";
import { Input } from "../ui/Input";
import { Separator } from "../ui/Separator";
import { ThemeToggle } from "../components/ThemeToggle";

/**
 * The login page.
 *
 * It is a plain `<form>` with a submit button, not a div with a click handler,
 * which is what makes Enter submit it and a password manager fill it. Both
 * come free from the element and neither is worth reimplementing.
 */

interface LoginSearch {
  /** Where to go once signed in. Never trusted; see safeDestination. */
  redirect?: string;

  /** Set when the user arrived because their session ended. */
  expired?: boolean;
}

export const Route = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",

  validateSearch: (search: Record<string, unknown>): LoginSearch => {
    const out: LoginSearch = {};

    if (typeof search["redirect"] === "string") out.redirect = search["redirect"];
    if (search["expired"] === true || search["expired"] === "true") out.expired = true;

    return out;
  },

  // Two people have no business on the login page: somebody already signed in,
  // and somebody whose Pivot has no accounts at all. The second is the one
  // that matters on a fresh install -- without this check the first thing a
  // new operator sees is a form that no password can satisfy, with nothing on
  // it saying why.
  //
  // The rule lives here rather than in every route, because every guard that
  // turns somebody away sends them to this page.
  beforeLoad: async ({ context, search }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery);

    if (session !== null) {
      throw redirect({ to: safeDestination(search.redirect), replace: true });
    }

    // Fails open. If the probe itself fails -- the server is briefly
    // unreachable, a proxy ate it -- the login page still renders, because
    // "we could not check" is not a reason to stop somebody signing in. Only a
    // definite "there are no accounts" sends them to setup.
    const status = await context.queryClient
      .ensureQueryData(setupStatusQuery)
      .catch(() => null);

    if (status !== null && !status.initialized) {
      throw redirect({ to: "/setup", replace: true });
    }
  },

  component: LoginPage,
});

function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const search = useSearch({ from: Route.id });

  const login = useLogin();
  const providers = useAuthProviders();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [organization, setOrganization] = useState("");

  // Revealed by the server, not guessed: a single-organization instance never
  // shows this field, and a multi-organization one shows it exactly once the
  // first attempt comes back asking for it.
  const [needsOrganization, setNeedsOrganization] = useState(false);
  const organizationRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (needsOrganization) organizationRef.current?.focus();
  }, [needsOrganization]);

  const destination = safeDestination(search.redirect);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    login.mutate(
      organization === "" ? { email, password } : { email, password, organization },
      {
        onSuccess: () => {
          // `replace`, so Back does not return to the login page of a session
          // that is now over.
          void navigate({ to: destination, replace: true });
        },

        onError: (error) => {
          if (namesOrganization(error)) setNeedsOrganization(true);
        },
      },
    );
  }

  const errorKey = login.error === null ? null : messageFor(login.error);

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold">{t("auth.heading")}</h1>
        <p className="text-sm text-content-muted">{t("auth.subheading")}</p>
      </header>

      {search.expired === true && login.error === null && (
        <Alert tone="info" live>
          {t("auth.expired")}
        </Alert>
      )}

      {errorKey !== null && (
        // `live`, because it appears in response to something the user just
        // did and they may not be looking at this corner of the page.
        <Alert tone="danger" live>
          {t(errorKey)}
        </Alert>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Field label={t("auth.email")}>
          <Input
            type="email"
            name="email"
            // The browser's own autofill, which is also a password manager's
            // hook. Getting these two attributes wrong is why some login
            // forms never offer to fill.
            autoComplete="username"
            required
            autoFocus
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder={t("auth.emailPlaceholder")}
          />
        </Field>

        <Field label={t("auth.password")}>
          <Input
            type="password"
            name="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Field>

        {needsOrganization && (
          <Field label={t("auth.organization")} description={t("auth.organizationHelp")} required>
            <Input
              ref={organizationRef}
              name="organization"
              autoComplete="organization"
              value={organization}
              onChange={(event) => setOrganization(event.target.value)}
            />
          </Field>
        )}

        <Button type="submit" loading={login.isPending} className="w-full">
          {login.isPending ? t("auth.signingIn") : t("auth.signIn")}
        </Button>
      </form>

      {providers.data !== undefined && providers.data.providers.length > 0 && (
        <>
          <div className="flex items-center gap-3">
            <Separator className="flex-1" />
            <span className="text-xs text-content-muted">{t("auth.orContinueWith")}</span>
            <Separator className="flex-1" />
          </div>

          <div className="flex flex-col gap-2">
            {providers.data.providers.map((provider) => (
              <Button key={provider.slug} variant="secondary" asChild className="w-full">
                {/*
                  A real link, not a fetch. The single sign-on flow is a
                  top-level navigation to the identity provider and back; doing
                  it with XHR would land the redirect inside a request nobody
                  can follow.

                  The destination rides along as `return`, which the server
                  vets again with the same rule safeDestination applies here.
                */}
                <a href={startUrl(provider.slug, destination)}>
                  {t("auth.continueWith", { provider: provider.name })}
                </a>
              </Button>
            ))}
          </div>
        </>
      )}

      <footer className="flex justify-center">
        <ThemeToggle />
      </footer>
    </main>
  );
}

/** The server endpoint that begins a single sign-on flow. */
function startUrl(slug: string, destination: string): string {
  const path = `/api/v1/auth/oidc/${encodeURIComponent(slug)}/start`;

  return destination === DEFAULT_DESTINATION
    ? path
    : `${path}?return=${encodeURIComponent(destination)}`;
}

/** Whether the server refused because it needs an organization named. */
function namesOrganization(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.details.some((detail) => detail.field === "organization")
  );
}

/** The translation key for a failed attempt. */
function messageFor(
  error: unknown,
): LoginErrorKey | "auth.errors.organizationRequired" {
  if (namesOrganization(error)) return "auth.errors.organizationRequired";

  return loginErrorKey(error);
}
