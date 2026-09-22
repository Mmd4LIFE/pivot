import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

/*
 * A separate config from vite.config.ts, on purpose.
 *
 * The application build has a Tailwind plugin, manual chunking and a dev
 * proxy, none of which a test run needs, and the chunking in particular makes
 * the test environment differ from the production one in ways that are hard to
 * reason about. This config is the smallest thing that renders a component.
 *
 * There is no Tailwind here either, which is why the accessibility tests check
 * structure rather than appearance: without the compiled stylesheet, nothing
 * has a color to measure. Color is checked in Go, against the tokens, in
 * web/tokens_test.go. The two halves together are the whole check.
 */
export default defineConfig({
  plugins: [react()],

  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],

    // jsdom has no layout engine, so every overlay in the design system throws
    // on mount without these. See the file for what is stubbed and why that is
    // not cheating.
    setupFiles: ["./vitest.setup.ts"],

    // Stylesheet imports resolve to nothing rather than being processed. The
    // tests do not read styles, and processing Tailwind for each of them would
    // add seconds per file for no signal.
    css: false,
  },
});
