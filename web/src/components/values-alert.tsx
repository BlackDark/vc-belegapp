import { Show } from "solid-js";
import { Alert, AlertDescription } from "./ui/alert";

// ValuesAlert is the non-blocking notice for a failed GET /werte. The screens
// stay usable on a raw code fallback; this only explains why the labels and
// suggestions are missing.
export function ValuesAlert(props: { error: boolean }) {
  return (
    <Show when={props.error}>
      <Alert>
        <AlertDescription>
          Die Referenzwerte konnten nicht geladen werden. Codes werden
          stattdessen angezeigt.
        </AlertDescription>
      </Alert>
    </Show>
  );
}
