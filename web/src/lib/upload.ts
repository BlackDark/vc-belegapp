import { toast } from "solid-sonner";
import { type Bild, client, fehlermeldung } from "./api";
import { downscale } from "./image";

// uploadBelegbild downscales one captured photo, uploads it as a Belegbild and
// reports a failure with the same German sentence everywhere a photo is added.
// It returns null after showing the toast, so a caller only has to handle the
// success path. recognise starts the recognition run; the first photo of a new
// Beleg asks for it, a later photo still attaches to the same run.
export async function uploadBelegbild(
  file: File,
  recognize = true,
): Promise<Bild | null> {
  try {
    const blob = await downscale(file);
    return await client.upload(blob, recognize);
  } catch (err) {
    toast.error(
      fehlermeldung(err, "Das Bild konnte nicht hochgeladen werden."),
    );
    return null;
  }
}
