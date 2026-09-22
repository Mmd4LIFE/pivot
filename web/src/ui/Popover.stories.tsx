import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./Button";
import { Field } from "./Field";
import { Input } from "./Input";
import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from "./Popover";

const meta: Meta<typeof PopoverContent> = {
  title: "Overlays/Popover",
  component: PopoverContent,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof PopoverContent>;

export const Closed: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary">Filter</Button>
      </PopoverTrigger>

      <PopoverContent>Nothing yet.</PopoverContent>
    </Popover>
  ),
};

/**
 * Open, with a form inside. This is the case a tooltip cannot do: the input and
 * both buttons are in the tab order.
 */
export const WithAForm: Story = {
  render: () => (
    <Popover defaultOpen>
      <PopoverTrigger asChild>
        <Button variant="secondary">Filter</Button>
      </PopoverTrigger>

      <PopoverContent className="flex flex-col gap-3">
        <Field label="Minimum revenue">
          <Input type="number" defaultValue="1000" />
        </Field>

        <div className="flex justify-end gap-2">
          <PopoverClose asChild>
            <Button variant="ghost" size="sm">
              Cancel
            </Button>
          </PopoverClose>
          <PopoverClose asChild>
            <Button size="sm">Apply</Button>
          </PopoverClose>
        </div>
      </PopoverContent>
    </Popover>
  ),
};

export const Aligned: Story = {
  render: () => (
    <div className="flex justify-end">
      <Popover defaultOpen>
        <PopoverTrigger asChild>
          <Button variant="secondary">Account</Button>
        </PopoverTrigger>

        <PopoverContent align="end" className="w-56 text-sm">
          Signed in as ada@example.com
        </PopoverContent>
      </Popover>
    </div>
  ),
};
