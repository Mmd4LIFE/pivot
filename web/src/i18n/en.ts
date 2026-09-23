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

  /*
   * The first run.
   *
   * Written for somebody who has downloaded a binary and has no idea what
   * Pivot calls things yet, which is why the words here are plainer than the
   * rest of the product's. "Organization" is explained rather than assumed.
   */
  setup: {
    heading: "Set up Pivot",
    subheading: "Create the first account. It becomes the administrator.",

    organization: "Organization name",
    organizationHelp: "Your company or team. You can change it later.",

    name: "Your name",
    email: "Email address",
    password: "Password",
    passwordHelp: "At least 12 characters. Length matters more than symbols.",

    token: "Setup token",
    tokenHelp: "Printed by the server when it started, under \u201cToken:\u201d.",

    submit: "Create administrator",
    submitting: "Creating",

    // Shown when somebody lands on /setup and there is nothing to set up.
    alreadyDone: "This Pivot is already set up.",
    goToSignIn: "Go to sign in",

    errors: {
      alreadyInitialized:
        "This Pivot already has an administrator. Sign in instead.",

      // Deliberately one message for a missing token and a wrong one: the
      // next step is the same, and an unauthenticated endpoint should not
      // help somebody work out which half they got right.
      badToken:
        "That setup token is not right. Copy it from the output of the server you just started.",

      passwordTooShort: "Use a longer password \u2014 at least 12 characters.",

      duplicate: "That organization or email address is already taken.",

      unexpected: "Something went wrong while setting up Pivot.",
    },
  },

  /*
   * Your own account.
   *
   * The words here are about consequence rather than mechanism: somebody
   * changing a password wants to know what happens to their other devices,
   * and somebody ending a session wants to know what it does to whoever is
   * using it.
   */
  account: {
    title: "Your account",
    subtitle: "Your details, your password, and the devices you are signed in on.",

    profile: {
      heading: "Details",
      name: "Name",
      email: "Email address",
      organization: "Organization",

      // Editing comes with the people-management screens in Phase 0's
      // successor; saying so is better than a disabled field with no reason.
      readOnly: "Ask an administrator to change these for now.",
    },

    password: {
      heading: "Password",
      description:
        "Changing it signs you out everywhere else. You stay signed in here.",

      current: "Current password",
      new: "New password",
      newHelp: "At least 12 characters. Length matters more than symbols.",

      submit: "Change password",
      submitting: "Changing",
      changed: "Password changed. Your other sessions have been signed out.",

      errors: {
        incorrect: "That is not your current password.",
        tooShort: "Use a longer password \u2014 at least 12 characters.",
        same: "The new password must be different from the current one.",
        throttled: "Too many attempts. Wait a few minutes before trying again.",
        unexpected: "Something went wrong while changing your password.",
      },
    },

    sessions: {
      heading: "Signed in on",
      description:
        "Every device with a live session. Ending one takes effect immediately.",

      current: "This device",
      lastSeen: "Last used",
      signedIn: "Signed in",
      expires: "Expires",
      unknownDevice: "Unknown device",

      end: "End session",
      ending: "Ending",
      ended: "That session has been ended.",
      endFailed: "That session could not be ended.",

      // The row for the session you are using says this instead of offering a
      // button that would sign you out by surprise.
      endCurrent: "Use Sign out to end this one.",

      empty: "No other devices are signed in.",
    },
  },

  nav: {
    // The landmark labels. A screen reader user navigates by these, so they
    // are names rather than descriptions.
    primary: "Main",
    breadcrumbs: "Breadcrumb",
    skipToContent: "Skip to content",

    home: "Home",
    dashboards: "Dashboards",
    questions: "Questions",
    connections: "Connections",
    people: "People",
    settings: "Settings",

    search: "Search",
    // The hotkey, shown on the search button. Decorative -- the button has a
    // real name -- but it is how anyone learns the shortcut exists.
    searchHint: "Ctrl K",

    account: "Account",
    openMenu: "Open menu",
  },

  pages: {
    home: {
      title: "Home",
      welcome: "Welcome back, {{name}}.",
      permissionsHeading: "What you can do here",
      noPermissions: "No permissions have been granted to you yet.",
      sessionHeading: "This session",
      signedInAs: "Signed in as {{email}}",
      expires: "Ends {{when}}",
    },

    dashboards: {
      title: "Dashboards",
      emptyTitle: "No dashboards yet",
      emptyBody:
        "A dashboard collects questions onto one page. Phase 1 builds them, once a connection exists.",
    },

    questions: {
      title: "Questions",
      emptyTitle: "No questions yet",
      emptyBody:
        "A question is a query you can save, share and put on a dashboard. Phase 1 builds the editor.",
    },

    connections: {
      title: "Connections",
      emptyTitle: "No connections yet",
      emptyBody:
        "A connection points Pivot at a database. Phase 1 adds PostgreSQL, MySQL, ClickHouse and the rest.",
    },

    people: {
      title: "People",
      emptyTitle: "Nothing to show yet",
      emptyBody:
        "Members and their roles. The API is live; Phase 0 Part 14 puts an interface on it.",
    },

    settings: {
      title: "Settings",
      emptyTitle: "Nothing to configure yet",
      emptyBody:
        "Identity providers, retention and branding land here as the phases that own them arrive.",
    },
  },

  command: {
    label: "Search Pivot",
    placeholder: "Search or jump to...",
    empty: "Nothing matches that.",
    goTo: "Go to",
    actions: "Actions",
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
