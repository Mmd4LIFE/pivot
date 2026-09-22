import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import {
  Card,
  CardBody,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "./Card";

// Annotated rather than `satisfies`, unlike the other story files. A Card's
// children are structural -- header, body, footer -- so each story builds its
// own tree in `render` instead of passing them as an arg, and the `satisfies`
// form would insist on an `args.children` that nothing uses.
const meta: Meta<typeof Card> = {
  title: "Layout/Card",
  component: Card,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof Card>;

export const Basic: Story = {
  render: () => (
    <Card className="max-w-md">
      <CardBody>Revenue is up 12% against the same week last year.</CardBody>
    </Card>
  ),
};

/**
 * CardTitle takes an explicit heading level. It has no default, because the
 * right level depends on where the card sits in the page — and a component
 * that guesses produces an outline that skips levels.
 */
export const WithHeader: Story = {
  render: () => (
    <Card className="max-w-md">
      <CardHeader>
        <CardTitle level={2}>Weekly revenue</CardTitle>
        <CardDescription>Updated 4 minutes ago</CardDescription>
      </CardHeader>

      <CardBody>Revenue is up 12% against the same week last year.</CardBody>
    </Card>
  ),
};

export const WithFooter: Story = {
  render: () => (
    <Card className="max-w-md">
      <CardHeader>
        <CardTitle level={2}>Delete this dashboard?</CardTitle>
        <CardDescription>This cannot be undone.</CardDescription>
      </CardHeader>

      <CardBody>
        Seven people have viewed it in the last week. They will lose access
        immediately.
      </CardBody>

      <CardFooter>
        <Button variant="ghost">Cancel</Button>
        <Button variant="danger">Delete</Button>
      </CardFooter>
    </Card>
  ),
};
