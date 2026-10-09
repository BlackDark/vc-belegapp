import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { Show } from "solid-js";
import { CaptureInputs, openCaptured } from "../components/CaptureInputs";
import { client } from "../lib/api";
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
        {(item) => (
          <A
            href={`/belege/${item().id}`}
            class="rounded-2xl border border-zinc-200 p-4 dark:border-zinc-800"
          >
            <p class="text-sm text-zinc-500">Beleg vorhanden</p>
            <p class="text-2xl font-semibold">
              {formatCent(item().belegbetrag_cent)}
            </p>
            <p>Erstattung {formatCent(item().berechnung.erstattung_cent)}</p>
            <Show when={item().bilder[0]}>
              <img
                alt="Beleg"
                class="mt-3 h-32 w-full rounded-xl object-cover"
                src={item().bilder[0]?.thumbnail_url}
              />
            </Show>
          </A>
        )}
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
