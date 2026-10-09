import { A, useNavigate } from "@solidjs/router";

import { CaptureInputs, openCaptured } from "../components/CaptureInputs";
import { buttonVariants } from "../components/ui/button";
import { cn } from "../lib/utils";

export default function Erfassen() {
  const navigate = useNavigate();
  return (
    <section class="mx-auto flex w-full max-w-lg flex-col gap-4">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Beleg erfassen</h1>
        <p class="text-sm text-muted-foreground">
          Foto aufnehmen oder ein Bild aus der Galerie wählen.
        </p>
      </div>
      <CaptureInputs onFile={(file) => void openCaptured(navigate, file)} />
      <A
        href="/belege/neu"
        class={cn(buttonVariants({ variant: "link" }), "self-center")}
      >
        Ohne Foto erfassen
      </A>
    </section>
  );
}
