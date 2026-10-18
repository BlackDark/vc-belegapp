import { useNavigate } from "@solidjs/router";
import { createSignal, Show } from "solid-js";
import { toast } from "solid-sonner";
import { client, fehlermeldung } from "../../lib/api";
import {
  type ImportPreview,
  type ImportResult,
  uploadImport,
} from "../../lib/datenimport";
import { LabeledField } from "../field";
import { Button } from "../ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

export function DatenimportCard() {
  const navigate = useNavigate();
  const [importStatus, setImportStatus] = createSignal("");
  const [preview, setPreview] = createSignal<ImportPreview | null>(null);
  const [wort, setWort] = createSignal("");
  const [importLaeuft, setImportLaeuft] = createSignal(false);
  const [importResult, setImportResult] = createSignal<ImportResult | null>(
    null,
  );

  const stageImport = async (file: File) => {
    setImportResult(null);
    setWort("");
    setImportStatus("Wird hochgeladen…");
    try {
      const next = await uploadImport(file, (percent) => {
        setImportStatus(`Wird hochgeladen… ${percent} %`);
      });
      setPreview(next);
      setImportStatus("");
    } catch (err) {
      setImportStatus("");
      toast.error(fehlermeldung(err, "Datenimport fehlgeschlagen"));
    }
  };

  const commitImport = async () => {
    const current = preview();
    if (!current) {
      return;
    }
    setImportLaeuft(true);
    setImportStatus("Wird wiederhergestellt…");
    try {
      const result = await client.datenimport(current.import_token, wort());
      setPreview(null);
      setImportResult(result);
      setImportStatus("");
    } catch (err) {
      setImportStatus("");
      toast.error(fehlermeldung(err, "Datenimport fehlgeschlagen"));
    } finally {
      setImportLaeuft(false);
    }
  };

  return (
    <>
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Datenimport</CardTitle>
          <CardDescription>
            Ersetzt den gesamten Bestand. Vorher wird automatisch eine Sicherung
            angelegt.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <div class="flex flex-col gap-1.5">
            <Label for="datenimport">ZIP-Datei</Label>
            <Input
              id="datenimport"
              type="file"
              accept=".zip,application/zip"
              aria-label="Datenimport"
              onChange={(event) => {
                const file = event.currentTarget.files?.item(0);
                if (file) void stageImport(file);
              }}
            />
          </div>
          <Show when={importStatus()}>
            <p aria-live="polite" class="text-sm">
              {importStatus()}
            </p>
          </Show>
          <Show when={importResult()}>
            <div class="rounded-xl border p-3 text-sm">
              <p>Datenimport abgeschlossen. Alle Sitzungen wurden beendet.</p>
              <p class="mt-1">
                {importResult()?.anzahl_belege} Belege
                <Show when={importResult()?.zeitraum_von}>
                  {" "}
                  ({importResult()?.zeitraum_von} bis{" "}
                  {importResult()?.zeitraum_bis})
                </Show>
              </p>
              <p>
                App {importResult()?.app_version}, Schema{" "}
                {importResult()?.schema_version}
              </p>
              <Button class="mt-3" onClick={() => navigate("/login")}>
                Zur Anmeldung
              </Button>
            </div>
          </Show>
        </CardContent>
      </Card>
      <Dialog
        open={preview() !== null}
        onOpenChange={(open) => {
          if (!open) setPreview(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Datenimport</DialogTitle>
          </DialogHeader>
          <div class="text-sm">
            <p>
              Zeitraum {preview()?.zeitraum_von || "–"} bis{" "}
              {preview()?.zeitraum_bis || "–"}
            </p>
            <p>Anzahl Belege {preview()?.anzahl_belege}</p>
            <p>App-Version {preview()?.app_version}</p>
            <p>Schema-Version {preview()?.schema_version}</p>
          </div>
          <p class="text-sm text-muted-foreground">
            Zum Ersetzen ERSETZEN eingeben
          </p>
          <LabeledField label="Bestätigung" value={wort()} onChange={setWort} />
          <DialogFooter>
            <Button variant="outline" onClick={() => setPreview(null)}>
              Abbrechen
            </Button>
            <Button
              disabled={importLaeuft() || wort() !== "ERSETZEN"}
              onClick={() => void commitImport()}
            >
              Wiederherstellen
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
