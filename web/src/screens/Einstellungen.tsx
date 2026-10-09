import { Dialog } from "@kobalte/core/dialog";
import { A, useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Button } from "../components/ui";
import { bezugsorte } from "../lib/amtlich";
import { ApiError, client, type Einstellungen as Profil } from "../lib/api";
import { applyTheme, readTheme, type Theme } from "../lib/theme";

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
      const body = xhr.responseText
        ? (JSON.parse(xhr.responseText) as ImportPreview & {
            detail?: string;
            code?: string;
          })
        : null;
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
    queryKey: ["einstellungen"],
    queryFn: () => client.einstellungen(),
  }));
  const regeln = useQuery(() => ({
    queryKey: ["regeln"],
    queryFn: () => client.regeln(),
  }));
  const info = useQuery(() => ({
    queryKey: ["info"],
    queryFn: () => client.info(),
  }));
  const sitzungen = useQuery(() => ({
    queryKey: ["sitzungen"],
    queryFn: () => client.sitzungen(),
  }));
  const [theme, setTheme] = createSignal<Theme>(readTheme());
  const [name, setName] = createSignal("");
  const [personal, setPersonal] = createSignal("");
  const [ag, setAg] = createSignal("");
  const [bezug, setBezug] = createSignal("supermarkt");
  const [arbeit, setArbeit] = createSignal("betrieb");
  const [aktiv, setAktiv] = createSignal(true);
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
    if (!row) {
      return;
    }
    setName(row.arbeitnehmer_name);
    setPersonal(row.personalnummer);
    setAg(row.arbeitgeber_name);
    setBezug(row.standard_bezugsort);
    setArbeit(row.standard_arbeitsort);
    setAktiv(row.erkennung_aktiv);
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
    };
    try {
      await client.putEinstellungen(next);
      toast.success("Gespeichert");
      await queryClient.invalidateQueries({ queryKey: ["einstellungen"] });
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Speichern fehlgeschlagen",
      );
    }
  };

  return (
    <section class="flex flex-col gap-6">
      <h1 class="text-2xl font-semibold">Einstellungen</h1>
      <Show when={profil.data}>
        <form
          class="flex flex-col gap-3"
          onSubmit={(event) => void save(event)}
        >
          <h2 class="text-lg font-medium">Profil</h2>
          <label class="text-sm">
            Arbeitnehmer
            <input
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
              class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
            />
          </label>
          <label class="text-sm">
            Personalnummer
            <input
              value={personal()}
              onInput={(event) => setPersonal(event.currentTarget.value)}
              class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
            />
          </label>
          <label class="text-sm">
            Arbeitgeber
            <input
              value={ag()}
              onInput={(event) => setAg(event.currentTarget.value)}
              class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
            />
          </label>
          <label class="text-sm">
            Standard-Bezugsort
            <select
              value={bezug()}
              onChange={(event) => setBezug(event.currentTarget.value)}
              class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
            >
              <For each={bezugsorte}>
                {(item) => <option value={item[0]}>{item[1]}</option>}
              </For>
            </select>
          </label>
          <label class="text-sm">
            Standard-Arbeitsort
            <select
              value={arbeit()}
              onChange={(event) => setArbeit(event.currentTarget.value)}
              class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
            >
              <option value="betrieb">Betrieb</option>
              <option value="homeoffice">Homeoffice</option>
            </select>
          </label>
          <Button type="submit">Profil speichern</Button>
        </form>
      </Show>
      <div>
        <h2 class="text-lg font-medium">Jahresregeln</h2>
        <ul class="mt-2 flex flex-col gap-2">
          <For each={regeln.data ?? []}>
            {(regel) => (
              <li>
                <A
                  class="underline"
                  href={`/einstellungen/jahre/${regel.jahr}`}
                >
                  {regel.jahr}
                </A>
              </li>
            )}
          </For>
        </ul>
        <A
          class="mt-3 inline-flex underline"
          href={`/einstellungen/jahre/${new Date().getFullYear()}`}
        >
          Jahr bearbeiten
        </A>
      </div>
      <div>
        <h2 class="text-lg font-medium">Darstellung</h2>
        <select
          aria-label="Darstellung"
          class="mt-2 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-900"
          value={theme()}
          onChange={(event) => {
            const next = event.currentTarget.value as Theme;
            setTheme(next);
            applyTheme(next);
          }}
        >
          <option value="hell">Hell</option>
          <option value="dunkel">Dunkel</option>
          <option value="system">System</option>
        </select>
      </div>
      <div class="flex flex-col gap-3">
        <h2 class="text-lg font-medium">Belegerkennung</h2>
        <label class="flex min-h-12 items-center gap-3 text-sm">
          <input
            type="checkbox"
            checked={aktiv()}
            onChange={(event) => {
              setAktiv(event.currentTarget.checked);
              void save();
            }}
          />
          Belegerkennung aktiv
        </label>
        <label class="text-sm">
          Modell
          <input
            readOnly
            aria-label="Modell"
            value={info.data?.llm_model ?? ""}
            class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 bg-zinc-50 px-3 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>
        <label class="text-sm">
          Basis-URL
          <input
            readOnly
            aria-label="Basis-URL"
            value={info.data?.llm_base_url ?? ""}
            class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 bg-zinc-50 px-3 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>
        <Show when={info.data?.erkennung_problem}>
          <p class="text-sm text-amber-800 dark:text-amber-200">
            {info.data?.erkennung_problem}
          </p>
        </Show>
        <Show when={info.data && !info.data.erkennung_konfiguriert}>
          <p class="text-sm text-zinc-500">
            Nicht konfiguriert. Ohne API-Key bei der OpenAI-Basis-URL oder mit
            BELEGAPP_LLM_ENABLED=false bleibt die Erfassung manuell.
          </p>
        </Show>
        <Button
          class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
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
                void queryClient.invalidateQueries({ queryKey: ["info"] });
              });
          }}
        >
          Verbindung testen
        </Button>
      </div>
      <div>
        <h2 class="text-lg font-medium">Speicher</h2>
        <p>{info.data?.storage_backend}</p>
      </div>
      <div class="flex flex-col gap-3">
        <h2 class="text-lg font-medium">Datenexport</h2>
        <p class="text-sm text-zinc-600 dark:text-zinc-300">
          Sicherung der Datenbank, Belege und Bilder. Der Download ist 24
          Stunden gültig.
        </p>
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
            class="underline"
            href={downloadUrl()}
            download="vc-belegapp-datenexport.zip"
          >
            Datenexport herunterladen
          </a>
        </Show>
      </div>
      <div class="flex flex-col gap-3">
        <h2 class="text-lg font-medium">Datenimport</h2>
        <p class="text-sm text-zinc-600 dark:text-zinc-300">
          Ersetzt den gesamten Bestand. Vorher wird automatisch eine Sicherung
          angelegt.
        </p>
        <label class="text-sm">
          ZIP-Datei
          <input
            type="file"
            accept=".zip,application/zip"
            aria-label="Datenimport"
            class="mt-1 block w-full text-sm"
            onChange={(event) => {
              const file = event.currentTarget.files?.item(0);
              if (file) {
                void stageImport(file);
              }
            }}
          />
        </label>
        <Show when={importStatus()}>
          <p aria-live="polite" class="text-sm">
            {importStatus()}
          </p>
        </Show>
        <Show when={importResult()}>
          <div class="rounded-xl border border-zinc-300 p-3 text-sm dark:border-zinc-700">
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
      </div>
      <Dialog
        open={preview() !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPreview(null);
          }
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay class="fixed inset-0 bg-black/40" />
          <Dialog.Content class="fixed inset-x-4 top-24 z-20 rounded-2xl bg-white p-4 shadow-xl dark:bg-zinc-900">
            <Dialog.Title class="text-lg font-semibold">
              Datenimport
            </Dialog.Title>
            <div class="mt-2 text-sm">
              <p>
                Zeitraum {preview()?.zeitraum_von || "–"} bis{" "}
                {preview()?.zeitraum_bis || "–"}
              </p>
              <p>Anzahl Belege {preview()?.anzahl_belege}</p>
              <p>App-Version {preview()?.app_version}</p>
              <p>Schema-Version {preview()?.schema_version}</p>
            </div>
            <label class="mt-3 block text-sm">
              Zum Ersetzen ERSETZEN eingeben
              <input
                aria-label="Bestätigung"
                value={wort()}
                onInput={(event) => setWort(event.currentTarget.value)}
                class="mt-1 min-h-12 w-full rounded-xl border border-zinc-300 px-3 dark:border-zinc-700 dark:bg-zinc-950"
              />
            </label>
            <div class="mt-3 flex gap-2">
              <Button
                class="flex-1 bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
                onClick={() => setPreview(null)}
              >
                Abbrechen
              </Button>
              <Button
                class="flex-1"
                disabled={importLaeuft() || wort() !== "ERSETZEN"}
                onClick={() => void commitImport()}
              >
                Wiederherstellen
              </Button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog>
      <div>
        <h2 class="text-lg font-medium">Sitzungen</h2>
        <ul class="mt-2 text-sm">
          <For each={sitzungen.data ?? []}>
            {(row) => (
              <li>
                {row.aktuell ? "Diese Sitzung" : "Weitere Sitzung"} · {row.ip}
              </li>
            )}
          </For>
        </ul>
        <Button
          class="mt-2 bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
          onClick={() =>
            void client
              .deleteOtherSessions()
              .then(() =>
                queryClient.invalidateQueries({ queryKey: ["sitzungen"] }),
              )
          }
        >
          Andere Sitzungen beenden
        </Button>
      </div>
      <Button
        class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
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
        class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
        onClick={() => {
          void client.logout().then(() => {
            void queryClient.invalidateQueries({ queryKey: ["me"] });
            navigate("/login");
          });
        }}
      >
        Abmelden
      </Button>
      <p class="text-sm text-zinc-500">Belegapp {info.data?.version}</p>
    </section>
  );
}
