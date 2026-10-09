import type { JSX } from "solid-js";

import {
  TextField,
  TextFieldInput,
  TextFieldLabel,
  TextFieldTextArea,
} from "./ui/text-field";

export function LabeledField(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: "text" | "password" | "date" | "month" | "number";
  inputmode?: JSX.InputHTMLAttributes<HTMLInputElement>["inputMode"];
  autocomplete?: string;
  name?: string;
  readOnly?: boolean;
}) {
  return (
    <TextField class="gap-1.5" value={props.value} onChange={props.onChange}>
      <TextFieldLabel>{props.label}</TextFieldLabel>
      <TextFieldInput
        type={props.type ?? "text"}
        name={props.name}
        autocomplete={props.autocomplete}
        inputMode={props.inputmode}
        readOnly={props.readOnly}
      />
    </TextField>
  );
}

export function LabeledArea(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <TextField class="gap-1.5" value={props.value} onChange={props.onChange}>
      <TextFieldLabel>{props.label}</TextFieldLabel>
      <TextFieldTextArea />
    </TextField>
  );
}
