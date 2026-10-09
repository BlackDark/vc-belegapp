import { A, useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Choice } from "../components/choice";
import { LabeledField } from "../components/field";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Button } from "../components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../components/ui/card";
import { Checkbox } from "../components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Separator } from "../components/ui/separator";
import { Skeleton } from "../components/ui/skeleton";
import {
  Switch,
  SwitchControl,
  SwitchLabel,
  SwitchThumb,
} from "../components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "../components/ui/tabs";
import { bezugsorte } from "../lib/amtlich";
import { ApiError, client, type Einstellungen as Profil } from "../lib/api";
import { queryKeys } from "../lib/queryKeys";
import { applyTheme, currentTheme } from "../lib/theme";

type ImportPreview = {
  import_token: string;
  gueltig_bis: string;
  app_version: string;
  schema_version: number;
  zeitraum_von: string;
  zeitraum_bis: string;
  anzahl_belege: number;
};

type ImportResult = {
  ok: boolean;
  anzahl_belege: number;
  zeitraum_von: string;
  zeitraum_bis: string;
  schema_version: number;
  app_version: string;
};

function uploadImport(
  file: File,
  onProgress: (percent: number) => void,
): Promise<ImportPreview> {
  return new Promise((resolve, reject) => {
    const data = new FormData();
    data.set("datei", file);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/datenimport/pruefen");
    xhr.withCredentials = true;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onProgress(Math.round((event.loaded / event.total) * 100));
      }
    };
    xhr.onload = () => {
      let body: (ImportPreview & { detail?: string; code?: string }) | null =
        null;
      if (xhr.responseText) {
        try {
          body = JSON.parse(xhr.responseText) as ImportPreview & {
            detail?: string;
            code?: string;
          };
        } catch {
          reject(
            new ApiError(xhr.status, { detail: "Die Antwort war kein JSON." }),
          );
          return;
        }
      }
      if (xhr.status >= 200 && xhr.status < 300 && body) {
        resolve(body);
        return;
      }
      reject(
        new ApiError(xhr.status, {
          detail: body?.detail,
          code: body?.code,
        }),
      );
    };
    xhr.onerror = () => reject(new Error("Upload fehlgeschlagen"));
    xhr.send(data);
  });
}

export default function Einstellungen() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const profil = useQuery(() => ({
    queryKey: queryKeys.einstellungen,
    queryFn: () => client.einstellungen(),
  }));
  const regeln = useQuery(() => ({
    queryKey: queryKeys.regeln,
    queryFn: () => client.regeln(),
  }));
  const info = useQuery(() => ({
    queryKey: queryKeys.info,
    queryFn: () => client.info(),
  }));
  const sitzungen = useQuery(() => ({
    queryKey: queryKeys.sitzungen,
    queryFn: () => client.sitzungen(),
  }));
  const [name, setName] = createSignal("");
  const [personal, setPersonal] = createSignal("");
  const [ag, setAg] = createSignal("");
  const [bezug, setBezug] = createSignal("supermarkt");
  const [arbeit, setArbeit] = createSignal("betrieb");
  const [aktiv, setAktiv] = createSignal(true);
  const [csvStd, setCsvStd] = createSignal(true);
  const [zipStd, setZipStd] = createSignal(false);
  const [testLaeuft, setTestLaeuft] = createSignal(false);
  const [exportLaeuft, setExportLaeuft] = createSignal(false);
  const [exportStatus, setExportStatus] = createSignal("");
  const [downloadUrl, setDownloadUrl] = createSignal("");
  const [importStatus, setImportStatus] = createSignal("");
  const [preview, setPreview] = createSignal<ImportPreview | null>(null);
  const [wort, setWort] = createSignal("");
  const [importLaeuft, setImportLaeuft] = createSignal(false);
  const [importResult, setImportResult] = createSignal<ImportResult | null>(
    null,
  );
  const [seeded, setSeeded] = createSignal(false);

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
      toast.error(
        err instanceof ApiError ? err.message : "Datenimport fehlgeschlagen",
      );
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
      toast.error(
        err instanceof ApiError ? err.message : "Datenimport fehlgeschlagen",
      );
    } finally {
      setImportLaeuft(false);
    }
  };
  createEffect(() => {
    const row = profil.data;
    if (!row || seeded()) {
      return;
    }
    setName(row.arbeitnehmer_name);
    setPersonal(row.personalnummer);
    setAg(row.arbeitgeber_name);
    setBezug(row.standard_bezugsort);
    setArbeit(row.standard_arbeitsort);
    setAktiv(row.erkennung_aktiv);
    setCsvStd(row.export_csv_standard);
    setZipStd(row.export_zip_standard);
    setSeeded(true);
  });

  const save = async (event?: Event) => {
    event?.preventDefault();
    const current = profil.data;
    if (!current) {
      return;
    }
    const next: Profil = {
      ...current,
      arbeitnehmer_name: name(),
      personalnummer: personal(),
      arbeitgeber_name: ag(),
      standard_bezugsort: bezug(),
      standard_arbeitsort: arbeit(),
      erkennung_aktiv: aktiv(),
      export_csv_standard: csvStd(),
      export_zip_standard: zipStd(),
    };
    try {
      await client.putEinstellungen(next);
      toast.success("Gespeichert");
      await queryClient.invalidateQueries({
        queryKey: queryKeys.einstellungen,
      });
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Speichern fehlgeschlagen",
      );
    }
  };

  return (
    <section class="flex flex-col gap-6">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Einstellungen</h1>
        <p class="text-sm text-muted-foreground">
          Profil, Jahresregeln, Darstellung und Sicherung.
        </p>
      </div>
      <Show when={profil.isPending}>
        <div class="flex flex-col gap-3">
          <Skeleton height={40} radius={8} />
          <Skeleton height={160} radius={12} />
        </div>
      </Show>
      <Show when={profil.isError}>
        <Alert>
          <AlertDescription>
            Die Einstellungen konnten nicht geladen werden.
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={profil.data}>
        <form
          class="flex flex-col gap-4"
          onSubmit={(event) => void save(event)}
        >
          <Card class="rounded-xl">
            <CardHeader>
              <CardTitle>Profil</CardTitle>
              <CardDescription>
                Name und Standardwerte für neue Belege.
              </CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-3">
              <LabeledField
                label="Arbeitnehmer"
                value={name()}
                onChange={setName}
              />
              <LabeledField
                label="Personalnummer"
                value={personal()}
                onChange={setPersonal}
              />
              <LabeledField label="Arbeitgeber" value={ag()} onChange={setAg} />
              <Choice
                label="Standard-Bezugsort"
                value={bezug()}
                options={bezugsorte}
                onChange={setBezug}
              />
              <Choice
                label="Standard-Arbeitsort"
                value={arbeit()}
                options={[
                  ["betrieb", "Betrieb"],
                  ["homeoffice", "Homeoffice"],
                ]}
                onChange={setArbeit}
              />
              <Separator />
              <h2 class="text-lg font-medium">Monatsexport</h2>
              <Checkbox checked={csvStd()} onChange={setCsvStd}>
                CSV im Monatsexport vorauswählen
              </Checkbox>
              <Checkbox checked={zipStd()} onChange={setZipStd}>
                ZIP mit Originalbildern im Monatsexport vorauswählen
              </Checkbox>
              <Button type="submit">Profil speichern</Button>
            </CardContent>
          </Card>
        </form>
      </Show>
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Jahresregeln</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-2">
          <ul class="flex flex-col gap-2">
            <For each={regeln.data ?? []}>
              {(regel) => (
                <li>
                  <A
                    class="underline underline-offset-4"
                    href={`/einstellungen/jahre/${regel.jahr}`}
                  >
                    {regel.jahr}
                  </A>
                </li>
              )}
            </For>
          </ul>
          <A
            class="inline-flex text-sm font-medium underline underline-offset-4"
            href={`/einstellungen/jahre/${new Date().getFullYear()}`}
          >
            Jahr bearbeiten
          </A>
        </CardContent>
      </Card>
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Darstellung</CardTitle>
          <CardDescription>
            Hell, Dunkel oder die Systemeinstellung.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs
            value={currentTheme()}
            onChange={(value) => {
              if (
                value === "hell" ||
                value === "dunkel" ||
                value === "system"
              ) {
                applyTheme(value);
              }
            }}
          >
            <TabsList aria-label="Darstellung">
              <TabsTrigger value="hell">Hell</TabsTrigger>
              <TabsTrigger value="dunkel">Dunkel</TabsTrigger>
              <TabsTrigger value="system">System</TabsTrigger>
            </TabsList>
          </Tabs>
        </CardContent>
      </Card>
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Belegerkennung</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <Switch
            class="flex items-center gap-3"
            checked={aktiv()}
            onChange={(value) => {
              setAktiv(value);
              void save();
            }}
          >
            <SwitchControl>
              <SwitchThumb />
            </SwitchControl>
            <SwitchLabel>Belegerkennung aktiv</SwitchLabel>
          </Switch>
          <LabeledField
            label="Modell"
            readOnly
            value={info.data?.llm_model ?? ""}
            onChange={() => undefined}
          />
          <LabeledField
            label="Basis-URL"
            readOnly
            value={info.data?.llm_base_url ?? ""}
            onChange={() => undefined}
          />
          <Show when={info.data?.erkennung_problem}>
            <p class="text-sm text-warning-foreground">
              {info.data?.erkennung_problem}
            </p>
          </Show>
          <Show when={info.data && !info.data.erkennung_konfiguriert}>
            <p class="text-sm text-muted-foreground">
              Nicht konfiguriert. Ohne API-Key bei der OpenAI-Basis-URL oder mit
              BELEGAPP_LLM_ENABLED=false bleibt die Erfassung manuell.
            </p>
          </Show>
          <Button
            variant="outline"
            disabled={testLaeuft()}
            onClick={() => {
              setTestLaeuft(true);
              void client
                .testErkennung()
                .then((result) => {
                  if (result.ok) {
                    toast.success(
                      `Verbindung ok (${result.modell}, ${result.dauer_ms} ms)`,
                    );
                  } else {
                    toast.error(result.fehler || "Verbindung fehlgeschlagen");
                  }
                })
                .catch((err: unknown) => {
                  toast.error(
                    err instanceof ApiError
                      ? err.message
                      : "Verbindung fehlgeschlagen",
                  );
                })
                .finally(() => {
                  setTestLaeuft(false);
                  void queryClient.invalidateQueries({
                    queryKey: queryKeys.info,
                  });
                });
            }}
          >
            Verbindung testen
          </Button>
        </CardContent>
      </Card>
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Speicher</CardTitle>
          <CardDescription>{info.data?.storage_backend}</CardDescription>
        </CardHeader>
      </Card>
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
      <Card class="rounded-xl">
        <CardHeader>
          <CardTitle>Sitzungen</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <ul class="text-sm">
            <For each={sitzungen.data ?? []}>
              {(row) => (
                <li>
                  {row.aktuell ? "Diese Sitzung" : "Weitere Sitzung"} · {row.ip}
                </li>
              )}
            </For>
          </ul>
          <Button
            variant="outline"
            onClick={() =>
              void client.deleteOtherSessions().then(() =>
                queryClient.invalidateQueries({
                  queryKey: queryKeys.sitzungen,
                }),
              )
            }
          >
            Andere Sitzungen beenden
          </Button>
        </CardContent>
      </Card>
      <div class="flex flex-col gap-2">
        <Button
          variant="outline"
          onClick={() =>
            void client
              .protokoll()
              .then((report) =>
                toast(
                  report.ok
                    ? `Kette in Ordnung (${report.anzahl})`
                    : "Kette fehlerhaft",
                ),
              )
          }
        >
          Kette prüfen
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            void (async () => {
              try {
                await client.logout();
                queryClient.clear();
                navigate("/login");
              } catch (err) {
                toast.error(
                  err instanceof ApiError
                    ? err.message
                    : "Abmelden fehlgeschlagen",
                );
              }
            })();
          }}
        >
          Abmelden
        </Button>
      </div>
      <p class="text-sm text-muted-foreground">Belegapp {info.data?.version}</p>
    </section>
  );
}
