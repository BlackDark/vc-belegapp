import { Camera, ImagePlus } from "lucide-solid";
import { toast } from "solid-sonner";
import { ApiError, client } from "../lib/api";
import { downscale } from "../lib/image";

export function CaptureInputs(props: { onFile: (file: File) => void }) {
  const pick = (event: Event) => {
    const file = (event.currentTarget as HTMLInputElement).files?.[0];
    if (file) {
      props.onFile(file);
    }
  };
  return (
    <div class="grid gap-3">
      <label class="flex min-h-28 cursor-pointer flex-col items-center justify-center gap-2 rounded-2xl bg-emerald-800 text-white dark:bg-emerald-500 dark:text-zinc-950">
        <Camera size={28} />
        <span class="text-lg font-medium">Kamera</span>
        <input
          class="sr-only"
          aria-label="Kamera"
          type="file"
          accept="image/*"
          capture="environment"
          onChange={pick}
        />
      </label>
      <label class="flex min-h-16 cursor-pointer items-center justify-center gap-2 rounded-2xl border border-zinc-300 dark:border-zinc-700">
        <ImagePlus size={20} />
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
