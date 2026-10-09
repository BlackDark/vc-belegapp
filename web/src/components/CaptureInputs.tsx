import { Camera, ImagePlus } from "lucide-solid";
import { toast } from "solid-sonner";

import { ApiError, client } from "../lib/api";
import { downscale } from "../lib/image";
import { cn } from "../lib/utils";
import { buttonVariants } from "./ui/button";

export function CaptureInputs(props: { onFile: (file: File) => void }) {
  const pick = (event: Event) => {
    const file = (event.currentTarget as HTMLInputElement).files?.[0];
    if (file) {
      props.onFile(file);
    }
  };
  return (
    <div class="grid gap-3">
      <label
        class={cn(
          buttonVariants({ size: "lg" }),
          "h-28 cursor-pointer flex-col",
        )}
      >
        <Camera />
        <span class="text-base">Kamera</span>
        <input
          class="sr-only"
          aria-label="Kamera"
          type="file"
          accept="image/*"
          capture="environment"
          onChange={pick}
        />
      </label>
      <label
        class={cn(
          buttonVariants({ variant: "outline", size: "lg" }),
          "cursor-pointer",
        )}
      >
        <ImagePlus />
        Galerie
        <input
          class="sr-only"
          aria-label="Galerie"
          type="file"
          accept="image/*"
          onChange={pick}
        />
      </label>
    </div>
  );
}

export async function openCaptured(
  navigate: (path: string) => void,
  file: File,
) {
  try {
    const blob = await downscale(file);
    const bild = await client.upload(blob);
    navigate(`/belege/neu?bild=${bild.id}`);
  } catch (err) {
    toast.error(
      err instanceof ApiError
        ? err.message
        : "Das Bild konnte nicht hochgeladen werden.",
    );
  }
}
