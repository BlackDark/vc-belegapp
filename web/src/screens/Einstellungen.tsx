import { A, useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Button } from "../components/ui";
import { bezugsorte } from "../lib/amtlich";
import { ApiError, client, type Einstellungen as Profil } from "../lib/api";
import { applyTheme, readTheme, type Theme } from "../lib/theme";

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
