import { useParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Button } from "../components/ui";
import { amtlich, kistVorschlag, laender, mahlzeitLabel } from "../lib/amtlich";
import { ApiError, client, type Jahresregel } from "../lib/api";

export default function JahresregelPage() {
  const params = useParams();
  const jahr = () => Number(params.jahr);
  const queryClient = useQueryClient();
  const existing = useQuery(() => ({
    queryKey: ["regel", jahr()],
    queryFn: () => client.regel(jahr()),
    retry: false,
  }));
  const suggest = useQuery(() => ({
    queryKey: ["vorschlag", jahr()],
    queryFn: () => client.vorschlag(jahr()),
    enabled: existing.isError,
  }));
  const [draft, setDraft] = createSignal<Jahresregel | null>(null);
  createEffect(() => {
    const row = existing.data ?? suggest.data;
    if (row && !draft()) {
      setDraft(row);
    }
  });

  const update = (patch: Partial<Jahresregel>) => {
    const current = draft();
    if (current) {
      setDraft({ ...current, ...patch });
    }
  };

  const save = async () => {
    const current = draft();
    if (!current) {
      return;
    }
    try {
      await client.putRegel({ ...current, jahr: jahr() });
      toast.success("Jahresregel gespeichert");
      await queryClient.invalidateQueries({ queryKey: ["regel", jahr()] });
      await queryClient.invalidateQueries({ queryKey: ["regeln"] });
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Speichern fehlgeschlagen",
      );
    }
  };

  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">Jahresregel {params.jahr}</h1>
      <Show when={draft()} fallback={<p>Lädt …</p>}>
        {(row) => (
          <div class="flex flex-col gap-3">
            <label class="text-sm">
              Zuschuss (Cent)
              <input
                class="mt-1 min-h-12 w-full rounded-xl border px-3 dark:border-zinc-700 dark:bg-zinc-900"
                inputmode="numeric"
                value={row().zuschuss_cent}
                onInput={(event) =>
                  update({ zuschuss_cent: Number(event.currentTarget.value) })
                }
              />
            </label>
            <fieldset>
              <legend class="text-sm font-medium">Mahlzeiten</legend>
              <For each={["fruehstueck", "mittag", "abend"]}>
                {(meal) => (
                  <label class="mr-3">
                    <input
                      type="checkbox"
                      checked={row().mahlzeiten.includes(meal)}
                      onChange={(event) => {
                        const set = new Set(row().mahlzeiten);
                        if (event.currentTarget.checked) {
                          set.add(meal);
                        } else {
                          set.delete(meal);
                        }
                        const mahlzeiten = [
                          "fruehstueck",
                          "mittag",
                          "abend",
                        ].filter((item) => set.has(item));
                        update({
                          mahlzeiten,
                          standard_mahlzeit: mahlzeiten.includes(
                            row().standard_mahlzeit,
                          )
                            ? row().standard_mahlzeit
                            : (mahlzeiten[0] ?? "mittag"),
                        });
                      }}
                    />{" "}
                    {mahlzeitLabel[meal]}
                  </label>
                )}
              </For>
            </fieldset>
            <div class="grid grid-cols-3 gap-2">
              <Cent
                label="SBW Frühstück"
                value={row().sbw_fruehstueck_cent}
                onChange={(value) => update({ sbw_fruehstueck_cent: value })}
              />
              <Cent
                label="SBW Mittag"
                value={row().sbw_mittag_cent}
                onChange={(value) => update({ sbw_mittag_cent: value })}
              />
              <Cent
                label="SBW Abend"
                value={row().sbw_abend_cent}
                onChange={(value) => update({ sbw_abend_cent: value })}
              />
            </div>
            <Button
              class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
              onClick={() => {
                const official = amtlich[jahr()];
                if (!official) {
                  toast("Für dieses Jahr liegen keine amtlichen Werte vor.");
                  return;
                }
                update({
                  sbw_fruehstueck_cent: official.fruehstueck,
                  sbw_mittag_cent: official.mittag,
                  sbw_abend_cent: official.abend,
                });
              }}
            >
              Amtliche Werte übernehmen
            </Button>
            <label class="text-sm">
              <input
                type="checkbox"
                checked={row().pauschalierung}
                onChange={(event) =>
                  update({ pauschalierung: event.currentTarget.checked })
                }
              />{" "}
              Pauschalierung
            </label>
            <label class="text-sm">
              <input
                type="checkbox"
                checked={row().gehaltsumwandlung}
                onChange={(event) =>
                  update({ gehaltsumwandlung: event.currentTarget.checked })
                }
              />{" "}
              Gehaltsumwandlung
            </label>
            <label class="text-sm">
              Bundesland
              <select
                class="mt-1 min-h-12 w-full rounded-xl border px-3 dark:border-zinc-700 dark:bg-zinc-900"
                value={row().bundesland}
                onChange={(event) => {
                  const bundesland = event.currentTarget.value;
                  update({
                    bundesland,
                    kist_satz_bp:
                      kistVorschlag[bundesland] ?? row().kist_satz_bp,
                  });
                }}
              >
                <For each={laender}>
                  {(land) => <option value={land}>{land}</option>}
                </For>
              </select>
            </label>
            <Cent
              label="Kirchensteuer (Basispunkte)"
              value={row().kist_satz_bp}
              onChange={(value) => update({ kist_satz_bp: value })}
            />
            <label class="text-sm">
              Eigenanteil
              <select
                class="mt-1 min-h-12 w-full rounded-xl border px-3 dark:border-zinc-700 dark:bg-zinc-900"
                value={row().eigenanteil_variante}
                onChange={(event) =>
                  update({ eigenanteil_variante: event.currentTarget.value })
                }
              >
                <option value="standard">
                  Standard: Eigenanteil mindert den Sachbezug
                </option>
                <option value="vorsichtig">
                  Vorsichtig: Erstattung bis zum Sachbezugswert
                </option>
              </select>
            </label>
            <Cent
              label="Monatslimit"
              value={row().monatslimit}
              onChange={(value) => update({ monatslimit: value })}
            />
            <label class="text-sm">
              Limit-Modus
              <select
                class="mt-1 min-h-12 w-full rounded-xl border px-3 dark:border-zinc-700 dark:bg-zinc-900"
                value={row().limit_modus}
                onChange={(event) =>
                  update({ limit_modus: event.currentTarget.value })
                }
              >
                <option value="warnen">Warnen</option>
                <option value="blockieren">Blockieren</option>
              </select>
            </label>
            <Button onClick={() => void save()}>Speichern</Button>
          </div>
        )}
      </Show>
    </section>
  );
}

function Cent(props: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <label class="text-sm">
      {props.label}
      <input
        class="mt-1 min-h-12 w-full rounded-xl border px-3 dark:border-zinc-700 dark:bg-zinc-900"
        inputmode="numeric"
        value={props.value}
        onInput={(event) => props.onChange(Number(event.currentTarget.value))}
      />
    </label>
  );
}
