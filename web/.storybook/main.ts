import type { StorybookConfig } from "@storybook/react-vite";

/*
 * Storybook is a development and review tool. It is never built into the Go
 * binary and never shipped: `make web-build` does not run it, and web/dist
 * contains only the application. Keeping that boundary explicit matters
 * because the single-binary promise in ADR-0001 is easy to erode one build
 * artifact at a time.
 *
 * It reads vite.config.ts, so the Tailwind plugin and the token pipeline are
 * exactly the ones the application uses. A design system whose stories are
 * built differently from the product is a design system that lies.
 */
const config: StorybookConfig = {
  stories: ["../src/**/*.stories.@(ts|tsx)"],

  addons: [
    // Runs axe against every story as it renders, in the panel. The Go token
    // harness covers color contrast without a browser; this covers the
    // structural half — labels, roles, names, focus order — which is the half
    // a color calculation cannot see.
    "@storybook/addon-a11y",
  ],

  framework: {
    name: "@storybook/react-vite",
    options: {},
  },

  core: {
    // No telemetry. This is a self-hosted product for people who care about
    // where their data goes; the development tooling should behave the same way.
    disableTelemetry: true,
  },
};

export default config;
