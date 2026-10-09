import * as CheckboxPrimitive from "@kobalte/core/checkbox";
import type { PolymorphicProps } from "@kobalte/core/polymorphic";
import type { JSX, ValidComponent } from "solid-js";
import { Show, splitProps } from "solid-js";

import { cn } from "../../lib/utils";

type CheckboxRootProps<T extends ValidComponent = "div"> =
  CheckboxPrimitive.CheckboxRootProps<T> & {
    class?: string | undefined;
    children?: JSX.Element;
  };

// Kobalte hides the native input with a 1px clip. Playwright's check/uncheck
// uses element.checkVisibility() and retries until that box is hit-testable,
// so the input has to be a real control sitting on top of the visual box.
const hitTarget: JSX.CSSProperties = {
  position: "absolute",
  inset: "0",
  width: "1rem",
  height: "1rem",
  margin: "0",
  padding: "0",
  border: "0",
  background: "transparent",
  clip: "auto",
  "clip-path": "none",
  overflow: "visible",
  "white-space": "normal",
  opacity: "1",
  "z-index": "1",
  cursor: "pointer",
};

const Checkbox = <T extends ValidComponent = "div">(
  props: PolymorphicProps<T, CheckboxRootProps<T>>,
) => {
  const [local, others] = splitProps(props as CheckboxRootProps, [
    "class",
    "children",
  ]);
  return (
    <CheckboxPrimitive.Root
      class={cn("group flex items-start gap-3", local.class)}
      {...others}
    >
      <span class="relative mt-0.5 size-4 shrink-0">
        <CheckboxPrimitive.Input
          class="peer appearance-none"
          style={hitTarget}
        />
        <CheckboxPrimitive.Control class="pointer-events-none absolute inset-0 size-4 rounded-sm border border-primary bg-background text-primary-foreground ring-offset-background peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-2 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50 data-[checked]:border-primary data-[checked]:bg-primary data-[indeterminate]:border-primary data-[indeterminate]:bg-primary">
          <CheckboxPrimitive.Indicator>
            <svg
              aria-hidden="true"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              class="size-4"
            >
              <path d="M5 12l5 5l10 -10" />
            </svg>
          </CheckboxPrimitive.Indicator>
        </CheckboxPrimitive.Control>
      </span>
      <Show when={local.children}>
        <CheckboxPrimitive.Label class="text-sm leading-snug peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
          {local.children}
        </CheckboxPrimitive.Label>
      </Show>
    </CheckboxPrimitive.Root>
  );
};

export { Checkbox };
