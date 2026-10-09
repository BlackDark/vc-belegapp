import { Button as KButton } from "@kobalte/core/button";
import { Dialog } from "@kobalte/core/dialog";
import { TextField as KText } from "@kobalte/core/text-field";
import { createEffect, createSignal, type JSX } from "solid-js";

export function Button(props: {
  type?: "button" | "submit";
  class?: string;
  disabled?: boolean;
  onClick?: () => void;
  children: JSX.Element;
}) {
  return (
    <KButton
      type={props.type ?? "button"}
      disabled={props.disabled}
      onClick={props.onClick}
      class={`inline-flex min-h-12 items-center justify-center rounded-xl px-4 text-base font-medium disabled:opacity-50 ${props.class ?? "bg-emerald-800 text-white dark:bg-emerald-500 dark:text-zinc-950"}`}
    >
      {props.children}
    </KButton>
  );
}

export function TextField(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
  inputmode?: JSX.InputHTMLAttributes<HTMLInputElement>["inputMode"];
  autocomplete?: string;
  name?: string;
}) {
  return (
    <KText class="flex flex-col gap-1">
      <KText.Label class="text-sm font-medium">{props.label}</KText.Label>
      <KText.Input
        name={props.name}
        type={props.type ?? "text"}
        inputMode={props.inputmode}
        autocomplete={props.autocomplete}
        value={props.value}
        onInput={(event) => props.onChange(event.currentTarget.value)}
        class="min-h-12 rounded-xl border border-zinc-300 bg-white px-3 dark:border-zinc-700 dark:bg-zinc-900"
      />
    </KText>
  );
}

export function ReasonDialog(props: {
  open: boolean;
  title: string;
  onOpenChange: (open: boolean) => void;
  onConfirm: (reason: string) => void;
}) {
  const [value, setValue] = createSignal("");
  createEffect(() => {
    if (props.open) {
      setValue("");
    }
  });
  const reason = () => value().trim();
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 bg-black/40" />
        <Dialog.Content class="fixed inset-x-4 top-24 z-20 rounded-2xl bg-white p-4 shadow-xl dark:bg-zinc-900">
          <Dialog.Title class="text-lg font-semibold">
            {props.title}
          </Dialog.Title>
          <Dialog.Description class="mt-2 text-sm text-zinc-600 dark:text-zinc-300">
            Der Monat ist gesperrt. Bitte einen Grund mit mindestens 5 Zeichen
            angeben.
          </Dialog.Description>
          <label class="mt-3 block text-sm font-medium">
            Änderungsgrund
            <textarea
              class="mt-1 min-h-24 w-full rounded-xl border border-zinc-300 p-3 dark:border-zinc-700 dark:bg-zinc-950"
              value={value()}
              onInput={(event) => setValue(event.currentTarget.value)}
            />
          </label>
          <div class="mt-3 flex gap-2">
            <Button
              class="flex-1 bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
              onClick={() => props.onOpenChange(false)}
            >
              Abbrechen
            </Button>
            <Button
              class="flex-1"
              disabled={reason().length < 5}
              onClick={() => props.onConfirm(reason())}
            >
              Bestätigen
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog>
  );
}
