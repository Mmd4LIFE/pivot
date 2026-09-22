import type { Meta, StoryObj } from "@storybook/react-vite";
import { Field } from "./Field";
import {
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectRoot,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "./Select";

const meta: Meta<typeof SelectTrigger> = {
  title: "Overlays/Select",
  component: SelectTrigger,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof SelectTrigger>;

/** Closed, which is how it spends almost all of its life. */
export const Closed: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Default role" description="Applied to everyone who joins by SSO.">
        <SelectRoot defaultValue="viewer">
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="viewer">Viewer</SelectItem>
            <SelectItem value="editor">Editor</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};

export const Open: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Default role">
        <SelectRoot defaultValue="viewer" defaultOpen>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="viewer">Viewer</SelectItem>
            <SelectItem value="editor">Editor</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};

/** Nothing chosen. The placeholder is muted, and it is not a label. */
export const Placeholder: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Connection">
        <SelectRoot>
          <SelectTrigger>
            <SelectValue placeholder="Choose a connection" />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="warehouse">Warehouse</SelectItem>
            <SelectItem value="events">Events</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};

export const Grouped: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Connection">
        <SelectRoot defaultValue="warehouse" defaultOpen>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>

          <SelectContent>
            <SelectGroup>
              <SelectLabel>Production</SelectLabel>
              <SelectItem value="warehouse">Warehouse</SelectItem>
              <SelectItem value="events">Events</SelectItem>
            </SelectGroup>

            <SelectSeparator />

            <SelectGroup>
              <SelectLabel>Staging</SelectLabel>
              <SelectItem value="warehouse-staging">Warehouse (staging)</SelectItem>
            </SelectGroup>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};

export const WithError: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Connection" error="Choose a connection before saving.">
        <SelectRoot>
          <SelectTrigger>
            <SelectValue placeholder="Choose a connection" />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="warehouse">Warehouse</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};

export const Disabled: Story = {
  render: () => (
    <div className="max-w-xs">
      <Field label="Connection" description="Ask an administrator to add one.">
        <SelectRoot disabled>
          <SelectTrigger>
            <SelectValue placeholder="No connections yet" />
          </SelectTrigger>

          <SelectContent>
            <SelectItem value="none">None</SelectItem>
          </SelectContent>
        </SelectRoot>
      </Field>
    </div>
  ),
};
