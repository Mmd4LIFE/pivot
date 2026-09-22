import { cleanup, render } from "@testing-library/react";
import { composeStories, setProjectAnnotations } from "@storybook/react";
import axe from "axe-core";
import type { ComponentType } from "react";
import { afterEach, describe, expect, test } from "vitest";

import previewAnnotations from "../../.storybook/preview";

import * as Alert from "./Alert.stories";
import * as Avatar from "./Avatar.stories";
import * as Badge from "./Badge.stories";
import * as Button from "./Button.stories";
import * as Card from "./Card.stories";
import * as Checkbox from "./Checkbox.stories";
import * as EmptyState from "./EmptyState.stories";
import * as Field from "./Field.stories";
import * as Input from "./Input.stories";
import * as RadioGroup from "./RadioGroup.stories";
import * as Separator from "./Separator.stories";
import * as Skeleton from "./Skeleton.stories";
import * as Spinner from "./Spinner.stories";
import * as Switch from "./Switch.stories";
import * as Table from "./Table.stories";
import * as Textarea from "./Textarea.stories";

/*
 * Every story, scanned by axe.
 *
 * This is the half of accessibility that a color calculation cannot reach:
 * whether a control has an accessible name, whether a label points at
 * something, whether an aria-* attribute is allowed on the element carrying
 * it. web/tokens_test.go is the other half. Neither substitutes for the other,
 * and neither substitutes for using the thing with a keyboard.
 *
 * It runs in jsdom rather than a browser, which is a real limitation and a
 * deliberate trade: a browser-based run needs Playwright and a few hundred
 * megabytes of downloaded browser, and a check that only runs when someone
 * remembers to install it is a check that does not run. Part 12 can add the
 * browser pass in CI on top of this; the point of this one is that it runs on
 * every developer's machine, every time.
 *
 * The list below is the single place a new component's stories get wired in.
 * web/stories_test.go fails the Go build if a component has no story file, or
 * if a story file is missing from this list — so the list cannot quietly fall
 * behind the directory.
 */
const STORY_MODULES = {
  Alert,
  Avatar,
  Badge,
  Button,
  Card,
  Checkbox,
  EmptyState,
  Field,
  Input,
  RadioGroup,
  Separator,
  Skeleton,
  Spinner,
  Switch,
  Table,
  Textarea,
};

// The preview's decorators and parameters, so a story under test renders the
// way it renders in Storybook. Without this the theme decorator never runs and
// the test would be checking a different tree from the one people review.
setProjectAnnotations([previewAnnotations]);

/** A composed story: a component that takes no props of its own. */
type ComposedStory = ComponentType<Record<string, never>>;

/**
 * composeStories, with the per-module typing dropped.
 *
 * Its return type is keyed to one module's exact exports, which is useful when
 * a test names a story and wrong here, where the point is to treat all sixteen
 * modules identically and never name a story at all.
 */
function compose(storyModule: object): Record<string, ComposedStory> {
  const composed = composeStories(
    storyModule as Parameters<typeof composeStories>[0],
  );

  return composed as unknown as Record<string, ComposedStory>;
}

/*
 * The rules to run.
 *
 * Scoped to the WCAG 2.1 AA tags rather than "everything axe knows", because
 * axe's best-practice rules include checks that are wrong for a component in
 * isolation — `region` wants all content inside a landmark, which is a
 * property of a page, not of a button. Asserting them here would train people
 * to ignore the output.
 *
 * color-contrast is turned off explicitly rather than left to fail quietly.
 * jsdom computes no layout and no cascade and has no canvas, so axe cannot
 * evaluate the rule — it reports "incomplete" and logs a wall of
 * HTMLCanvasElement errors on the way. Turning it off here says where the
 * check actually lives: web/tokens_test.go, in Go, against the tokens.
 */
const AXE_OPTIONS: axe.RunOptions = {
  runOnly: {
    type: "tag",
    values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"],
  },

  rules: {
    "color-contrast": { enabled: false },
  },
};

afterEach(() => {
  cleanup();
});

for (const [component, storyModule] of Object.entries(STORY_MODULES)) {
  const stories = compose(storyModule);

  describe(component, () => {
    const names = Object.keys(stories);

    test("has at least one story", () => {
      expect(names.length).toBeGreaterThan(0);
    });

    for (const name of names) {
      const Story = stories[name];

      test(`${name} has no accessibility violations`, async () => {
        if (Story === undefined) throw new Error(`no story named ${name}`);

        const { container } = render(<Story />);

        const results = await axe.run(container, AXE_OPTIONS);

        expect(results.violations.map(describeViolation)).toEqual([]);
      });
    }
  });
}

/** A violation, rendered as something a person can act on. */
function describeViolation(violation: axe.Result): string {
  const where = violation.nodes.map((node) => node.target.join(" ")).join(", ");

  return `${violation.id}: ${violation.help} (${where}) — ${violation.helpUrl}`;
}
