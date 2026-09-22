import type { Decorator, Preview } from "@storybook/react-vite";
import { useEffect } from "react";

// The real stylesheet, tokens and all. Stories therefore break when a token
// breaks, which is the only way a component gallery stays honest.
import "../src/styles/app.css";

/*
 * Theme switching, driven by the same data-theme attribute the application
 * uses. Nothing is recompiled between the two themes — the attribute changes
 * and the custom properties resolve differently — which is a live check that
 * the mechanism Phase 8's white-label embedding depends on still works.
 */
const withTheme: Decorator = (Story, context) => {
  const theme = context.globals["theme"] === "dark" ? "dark" : "light";

  useEffect(() => {
    document.documentElement.dataset["theme"] = theme;

    return () => {
      delete document.documentElement.dataset["theme"];
    };
  }, [theme]);

  return (
    <div className="bg-canvas text-content p-6">
      <Story />
    </div>
  );
};

const preview: Preview = {
  decorators: [withTheme],

  globalTypes: {
    theme: {
      description: "Color theme",
      toolbar: {
        title: "Theme",
        icon: "circlehollow",
        items: [
          { value: "light", title: "Light" },
          { value: "dark", title: "Dark" },
        ],
        dynamicTitle: true,
      },
    },
  },

  initialGlobals: {
    theme: "light",
  },

  parameters: {
    // A violation fails rather than warns. "todo" is the addon's default and
    // means the results are recorded and ignored, which over a few months is
    // indistinguishable from not having the addon.
    a11y: { test: "error" },

    // Storybook's own background control would paint over --pivot-canvas and
    // make a dark story look broken. The theme toolbar is the control here.
    backgrounds: { disable: true },

    controls: {
      matchers: {
        color: /(background|color)$/i,
      },
    },
  },
};

export default preview;
