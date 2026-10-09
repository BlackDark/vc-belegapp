import { Dialog } from "@kobalte/core/dialog";
import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { createSignal, Show } from "solid-js";
import { CaptureInputs, openCaptured } from "../components/CaptureInputs";
import { Button } from "../components/ui";
import { type Beleg, client } from "../lib/api";
import { currentMonth, todayISO } from "../lib/dates";
import { formatCent } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";

export default function Heute() {
  const navigate = useNavigate();
  const today = todayISO();
  const month = currentMonth();
  const monat = useQuery(() => ({
    queryKey: queryKeys.monat(month),
    queryFn: () => client.monat(month),
    retry: false,
  }));

  const beleg = () => monat.data?.belege.find((item) => item.datum === today);
  const limit = () => monat.data?.jahresregel?.monatslimit ?? 15;

  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">Heute</h1>
      <Show when={monat.error}>
        <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
          Für dieses Jahr fehlt die Jahresregel.{" "}
          <A
            class="underline"
            href={`/einstellungen/jahre/${today.slice(0, 4)}`}
          >
            Regel anlegen
          </A>
        </p>
      </Show>
      <Show
        when={beleg()}
        fallback={
          <CaptureInputs onFile={(file) => void openCaptured(navigate, file)} />
        }
      >
        {(item) => <Vorhanden item={item()} />}
      </Show>
      <div class="grid grid-cols-2 gap-3">
        <article class="rounded-2xl bg-zinc-100 p-4 dark:bg-zinc-900">
          <p class="text-sm text-zinc-500">Monat</p>
          <p class="text-xl font-semibold">
            {monat.data?.summen.anzahl ?? 0} / {limit()}
          </p>
        </article>
        <article class="rounded-2xl bg-zinc-100 p-4 dark:bg-zinc-900">
          <p class="text-sm text-zinc-500">Erstattung Σ</p>
          <p class="text-xl font-semibold">
            {formatCent(monat.data?.summen.erstattung_cent ?? 0)}
          </p>
        </article>
      </div>
    </section>
  );
}

function Vorhanden(props: { item: Beleg }) {
  const [offen, setOffen] = createSignal(false);
  const bild = () => props.item.bilder[0];
  return (
    <article class="flex items-center gap-4 rounded-2xl border border-zinc-200 p-4 dark:border-zinc-800">
      <A href={`/belege/${props.item.id}`} class="min-w-0 flex-1">
        <p class="text-sm text-zinc-500">Beleg vorhanden</p>
        <p class="text-2xl font-semibold">
          {formatCent(props.item.belegbetrag_cent)}
        </p>
        <p>Erstattung {formatCent(props.item.berechnung.erstattung_cent)}</p>
      </A>
      <Show when={bild()}>
        {(row) => (
          <>
            <button
              type="button"
              class="shrink-0 cursor-pointer rounded-xl bg-zinc-100 p-1 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800 dark:bg-zinc-900"
              aria-label="Belegbild öffnen"
              onClick={() => setOffen(true)}
            >
              {/* Fixed portrait frame. object-contain keeps the whole receipt visible. */}
              <img
                alt="Beleg"
                class="h-40 w-28 object-contain"
                src={row().thumbnail_url}
              />
            </button>
            <Dialog open={offen()} onOpenChange={setOffen}>
              <Dialog.Portal>
                <Dialog.Overlay class="fixed inset-0 z-20 bg-black/40" />
                <Dialog.Content class="fixed inset-x-4 top-8 z-20 mx-auto flex max-h-[calc(100dvh-7rem)] w-full max-w-lg flex-col gap-3 overflow-y-auto rounded-2xl bg-white p-4 shadow-xl dark:bg-zinc-900">
                  <Dialog.Title class="text-lg font-semibold">
                    Belegbild
                  </Dialog.Title>
                  <img
                    alt="Belegbild"
                    class="max-h-[65vh] w-full rounded-xl bg-zinc-100 object-contain dark:bg-zinc-950"
                    src={row().url}
                  />
                  <Button
                    class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
                    onClick={() => setOffen(false)}
                  >
                    Schließen
                  </Button>
                </Dialog.Content>
              </Dialog.Portal>
            </Dialog>
          </>
        )}
      </Show>
    </article>
  );
}
