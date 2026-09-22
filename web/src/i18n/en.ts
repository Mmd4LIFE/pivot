/**
 * English, complete.
 *
 * This file is the source of truth for every string a user can read, and it is
 * the reason `t("auth.signIn")` is a compile error when the key is wrong: the
 * type augmentation in ./index.ts is built from this object, so a typo fails
 * `tsc` rather than rendering the key itself into the interface.
 *
 * Written as TypeScript rather than JSON on purpose. JSON cannot carry a
 * comment, and a translator needs to know that "sign in" here is a verb.
 */
export const en = {
  app: {
    name: "Pivot",
    tagline: "The open business intelligence platform for the AI era.",
  },

  common: {
    loading: "Loading",
    retry: "Try again",
    cancel: "Cancel",
    dismiss: "Dismiss",
    goToStart: "Go to the start",
  },

  auth: {
    // The verb, not the noun. "Sign in" is the action; a "sign-in" is not a
    // word this product uses.
    signIn: "Sign in",
    signingIn: "Signing in",
    signOut: "Sign out",

    heading: "Sign in to Pivot",
    subheading: "Use your email address and password, or your organization's provider.",

    email: "Email address",
    emailPlaceholder: "you@example.com",
    password: "Password",

    organization: "Organization",
    organizationHelp:
      "Only needed if your email address belongs to more than one organization.",

    // Separates the password form from the single sign-on buttons.
    orContinueWith: "Or continue with",
    continueWith: "Continue with {{provider}}",

    signedInAs: "Signed in as {{email}}",

    errors: {
      // Deliberately the same message for a wrong password and an unknown
      // address. Saying which one was wrong turns the login form into a way to
      // find out who has an account here.
      invalidCredentials: "That email address and password do not match.",

      lockedOut:
        "Too many attempts. Wait a few minutes before trying again.",

      // 503 from the server means no decision could be reached, which is an
      // outage and not a rejection -- so the message says "try again" rather
      // than "ask an administrator".
      unavailable: "Pivot could not check your credentials just now. Try again shortly.",

      unexpected: "Something went wrong while signing you in.",

      emailRequired: "Enter your email address.",
      passwordRequired: "Enter your password.",

      // This instance hosts more than one organization, so the email address
      // alone is ambiguous. The server is what decides that, which is why the
      // field stays hidden until it says so.
      organizationRequired:
        "This Pivot hosts more than one organization. Name yours to continue.",
    },

    // Shown when a session ends while the user is still looking at the page.
    expired: "Your session ended. Sign in again to carry on where you left off.",
  },

  connection: {
    offline: "Pivot cannot reach the server.",
    offlineDetail: "Your changes are not being saved. This page will recover on its own.",
    restored: "Connection restored.",
  },

  errors: {
    notFoundTitle: "Page not found",
    notFoundBody: "That address does not match anything in Pivot.",

    crashTitle: "Something went wrong",
    crashBody: "Pivot hit an error it could not recover from. Reloading usually helps.",
    reload: "Reload",
  },

  locale: {
    label: "Language",
  },

  theme: {
    label: "Theme",
    light: "Light",
    dark: "Dark",
    system: "System",
  },

  session: {
    heading: "Session",
    checking: "Checking",
    signedOut: "Not signed in.",
  },
} as const;
