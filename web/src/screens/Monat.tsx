import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { client } from "../lib/api";
import { currentMonth } from "../lib/dates";
import { formatCent } from "../lib/money";

export default function Monat() {
  const [monat, setMonat] = createSignal(currentMonth());
  const query = useQuery(() => ({
    queryKey: ["monat", monat()],
    queryFn: () => client.monat(monat()),
    retry: false,
  }));

  return (
    <section class="flex flex-col gap-4">
      <div class="flex items-center justify-between gap-3">
        <h1 class="text-2xl font-semibold">Monat</h1>
        <input
          aria-label="Monat"
          type="month"
          class="min-h-12 rounded-xl border border-zinc-300 bg-white px-3 dark:border-zinc-700 dark:bg-zinc-900"
          value={monat()}
          onInput={(event) => setMonat(event.currentTarget.value)}
        />
      </div>
      <Show
        when={query.data}
        fallback={
          <p>{query.isPending ? "Lädt …" : "Keine Daten für diesen Monat."}</p>
        }
      >
        {(data) => (
          <>
            <p class="w-fit rounded-full bg-zinc-200 px-3 py-1 text-sm dark:bg-zinc-800">
              Status {data().status}
            </p>
            <Show when={data().status === "geaendert"}>
              <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
                Nach dem letzten Export geändert.
              </p>
            </Show>
            <article class="rounded-2xl bg-zinc-100 p-4 dark:bg-zinc-900">
              <p>Belege {data().summen.anzahl}</p>
              <p>Erstattung {formatCent(data().summen.erstattung_cent)}</p>
              <p>Eigenanteil {formatCent(data().summen.eigenanteil_cent)}</p>
              <p>AG-Kosten {formatCent(data().summen.ag_kosten_cent)}</p>
            </article>
            <div class="grid grid-cols-7 gap-1 text-center text-xs">
              <For each={["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"]}>
                {(day) => <span class="text-zinc-500">{day}</span>}
              </For>
              <For
                each={Array.from({
                  length: (data().tage[0]?.wochentag ?? 1) - 1,
                })}
              >
                {() => <span />}
              </For>
              <For each={data().tage}>
                {(tag) => (
                  <A
                    href={
                      tag.beleg_id ? `/belege/${tag.beleg_id}` : "/erfassen"
                    }
                    class={`rounded-lg px-1 py-2 ${tag.wochenende || tag.feiertag ? "bg-amber-100 dark:bg-amber-950" : "bg-zinc-100 dark:bg-zinc-900"} ${tag.beleg_id ? "font-semibold" : ""}`}
                  >
                    {Number(tag.datum.slice(8))}
                  </A>
                )}
              </For>
            </div>
            <ul class="flex flex-col gap-2">
              <For each={data().warnungen}>
                {(warn) => (
                  <li class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
                    {warn.text}
                  </li>
                )}
              </For>
            </ul>
            <ul class="flex flex-col gap-2">
              <For each={data().belege}>
                {(beleg) => (
                  <li>
                    <A
                      href={`/belege/${beleg.id}`}
                      class="flex items-center justify-between rounded-xl border border-zinc-200 px-3 py-3 dark:border-zinc-800"
                    >
                      <span>
                        {beleg.datum.slice(8, 10)}.{beleg.datum.slice(5, 7)}.{" "}
                        {beleg.haendler_name}
                      </span>
                      <span>
                        {formatCent(beleg.berechnung.erstattung_cent)}
                      </span>
                    </A>
                  </li>
                )}
              </For>
            </ul>
            <button
              type="button"
              class="min-h-12 rounded-xl border border-zinc-300 dark:border-zinc-700"
              onClick={() => toast("PDF-Export folgt im nächsten Meilenstein.")}
            >
              Vorschau / Export
            </button>
          </>
        )}
      </Show>
    </section>
  );
}
