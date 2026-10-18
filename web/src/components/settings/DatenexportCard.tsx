import { createSignal, Show } from "solid-js";
import { toast } from "solid-sonner";
import { ApiError, client } from "../../lib/api";
import { Button } from "../ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";

export function DatenexportCard() {
  const [exportLaeuft, setExportLaeuft] = createSignal(false);
  const [exportStatus, setExportStatus] = createSignal("");
  const [downloadUrl, setDownloadUrl] = createSignal("");

  const startExport = async () => {
    setExportLaeuft(true);
    setDownloadUrl("");
    setExportStatus("Wird erstellt…");
    try {
      const started = await client.datenexport();
      for (;;) {
        const job = await client.job(started.job_id);
        if (job.status === "fertig" && job.ergebnis?.download_url) {
          setDownloadUrl(job.ergebnis.download_url);
          setExportStatus("Datenexport ist bereit.");
          return;
        }
        if (job.status === "fehler") {
          throw new Error("Datenexport fehlgeschlagen");
        }
        await new Promise((resolve) => setTimeout(resolve, 400));
      }
    } catch (err) {
      setExportStatus("");
      toast.error(
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "Datenexport fehlgeschlagen",
      );
    } finally {
      setExportLaeuft(false);
    }
  };

  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Datenexport</CardTitle>
        <CardDescription>
          Sicherung der Datenbank, Belege und Bilder. Der Download ist 24
          Stunden gültig.
        </CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <Button disabled={exportLaeuft()} onClick={() => void startExport()}>
          Datenexport erstellen
        </Button>
        <Show when={exportStatus()}>
          <p aria-live="polite" class="text-sm">
            {exportStatus()}
          </p>
        </Show>
        <Show when={downloadUrl()}>
          <a
            class="text-sm font-medium underline underline-offset-4"
            href={downloadUrl()}
            download="vc-belegapp-datenexport.zip"
          >
            Datenexport herunterladen
          </a>
        </Show>
      </CardContent>
    </Card>
  );
}
