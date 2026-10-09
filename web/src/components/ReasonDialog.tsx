import { createEffect, createSignal } from "solid-js";

import { LabeledArea } from "./field";
import { Button } from "./ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog";

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
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{props.title}</DialogTitle>
          <DialogDescription>
            Der Monat ist gesperrt. Bitte einen Grund mit mindestens 5 Zeichen
            angeben.
          </DialogDescription>
        </DialogHeader>
        <LabeledArea
          label="Änderungsgrund"
          value={value()}
          onChange={setValue}
        />
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>
            Abbrechen
          </Button>
          <Button
            disabled={reason().length < 5}
            onClick={() => props.onConfirm(reason())}
          >
            Bestätigen
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
