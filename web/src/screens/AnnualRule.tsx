import { useParams } from "@solidjs/router";
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
  CardHeader,
  CardTitle,
} from "../components/ui/card";
import { Checkbox } from "../components/ui/checkbox";
import { Skeleton } from "../components/ui/skeleton";
import { ValuesAlert } from "../components/values-alert";
import { ApiError, client, type Jahresregel } from "../lib/api";
import { queryKeys } from "../lib/queryKeys";
import {
  bundeslaenderOf,
  kistSatzFor,
  mahlzeitLabelOf,
  mealCodesOf,
  sbwFor,
  useWerte,
} from "../lib/referenceData";

export default function AnnualRulePage() {
  const params = useParams();
  const jahr = () => Number(params.jahr);
  const queryClient = useQueryClient();
  const werte = useWerte();
  const existing = useQuery(() => ({
    queryKey: queryKeys.regel(jahr()),
    queryFn: () => client.regel(jahr()),
    retry: false,
  }));
  const suggest = useQuery(() => ({
    queryKey: queryKeys.vorschlag(jahr()),
    queryFn: () => client.vorschlag(jahr()),
    enabled: existing.isError,
  }));
  const [draft, setDraft] = createSignal<Jahresregel | null>(null);
  // The meal list is server data too: it comes from GET /werte and falls back
  // to the meals the rule was seeded with. The seed is captured once so that
  // unchecking a meal cannot shrink the list away while the table is missing.
  const [seedMeals, setSeedMeals] = createSignal<string[]>([]);
  createEffect(() => {
    const row = existing.data ?? suggest.data;
    if (row && !draft()) {
      setDraft(row);
      setSeedMeals(row.mahlzeiten);
    }
  });
  const meals = () => {
    const codes = mealCodesOf(werte.data);
    return codes.length > 0 ? codes : seedMeals();
  };

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
      await queryClient.invalidateQueries({
        queryKey: queryKeys.regel(jahr()),
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.regeln });
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Speichern fehlgeschlagen",
      );
    }
  };

  return (
    <section class="flex flex-col gap-4">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">
          Jahresregel {params.jahr}
        </h1>
        <p class="text-sm text-muted-foreground">
          Zuschuss, Sachbezugswerte und Pauschalierung für dieses Jahr.
        </p>
      </div>
      <Show
        when={draft()}
        fallback={
          <Show
            when={existing.isError && suggest.isError}
            fallback={<Skeleton height={240} radius={12} />}
          >
            <Alert>
              <AlertDescription>
                Die Jahresregel konnte nicht geladen werden.
              </AlertDescription>
            </Alert>
            <Button
              onClick={() => {
                void existing.refetch();
                void suggest.refetch();
              }}
            >
              Erneut versuchen
            </Button>
          </Show>
        }
      >
        {(row) => (
          <Card class="rounded-xl">
            <CardHeader>
              <CardTitle>Werte</CardTitle>
            </CardHeader>
            <CardContent class="flex flex-col gap-3">
              <ValuesAlert error={werte.isError} />
              <Cent
                label="Zuschuss (Cent)"
                value={row().zuschuss_cent}
                onChange={(value) => update({ zuschuss_cent: value })}
              />
              <Show
                when={!werte.isPending}
                fallback={<Skeleton height={96} radius={8} />}
              >
                <fieldset class="flex flex-col gap-2">
                  <legend class="text-sm font-medium">Mahlzeiten</legend>
                  <For each={meals()}>
                    {(meal) => (
                      <Checkbox
                        checked={row().mahlzeiten.includes(meal)}
                        onChange={(checked) => {
                          const set = new Set(row().mahlzeiten);
                          if (checked) {
                            set.add(meal);
                          } else {
                            set.delete(meal);
                          }
                          const mahlzeiten = meals().filter((item) =>
                            set.has(item),
                          );
                          update({
                            mahlzeiten,
                            standard_mahlzeit: mahlzeiten.includes(
                              row().standard_mahlzeit,
                            )
                              ? row().standard_mahlzeit
                              : (mahlzeiten[0] ?? "mittag"),
                          });
                        }}
                      >
                        {mahlzeitLabelOf(werte.data, meal) ?? meal}
                      </Checkbox>
                    )}
                  </For>
                </fieldset>
              </Show>
              <div class="grid gap-2 sm:grid-cols-3">
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
                variant="outline"
                disabled={werte.isPending || werte.isError}
                onClick={() => {
                  // A failed request and a year without official values are
                  // different facts and must not share one message: the first
                  // is an error, the second is a plain "nothing to copy".
                  if (werte.isError) {
                    toast.error(
                      "Die amtlichen Werte konnten nicht geladen werden.",
                    );
                    return;
                  }
                  const official = sbwFor(werte.data, jahr());
                  if (!official) {
                    toast("Für dieses Jahr liegen keine amtlichen Werte vor.");
                    return;
                  }
                  update({
                    sbw_fruehstueck_cent: official.fruehstueck_cent,
                    sbw_mittag_cent: official.mittag_cent,
                    sbw_abend_cent: official.abend_cent,
                  });
                }}
              >
                Amtliche Werte übernehmen
              </Button>
              <Checkbox
                checked={row().pauschalierung}
                onChange={(checked) => update({ pauschalierung: checked })}
              >
                Pauschalierung
              </Checkbox>
              <Checkbox
                checked={row().gehaltsumwandlung}
                onChange={(checked) => update({ gehaltsumwandlung: checked })}
              >
                Gehaltsumwandlung
              </Checkbox>
              <Show
                when={!werte.isPending}
                fallback={<Skeleton height={40} radius={8} />}
              >
                <Choice
                  label="Bundesland"
                  value={row().bundesland}
                  options={bundeslaenderOf(werte.data).map((land) => [
                    land,
                    land,
                  ])}
                  onChange={(bundesland) =>
                    update({
                      bundesland,
                      kist_satz_bp: kistSatzFor(
                        werte.data,
                        bundesland,
                        row().kist_satz_bp,
                      ),
                    })
                  }
                />
              </Show>
              <Cent
                label="Kirchensteuer (Basispunkte)"
                value={row().kist_satz_bp}
                onChange={(value) => update({ kist_satz_bp: value })}
              />
              <Choice
                label="Eigenanteil"
                value={row().eigenanteil_variante}
                options={[
                  ["standard", "Standard: Eigenanteil mindert den Sachbezug"],
                  [
                    "vorsichtig",
                    "Vorsichtig: Erstattung bis zum Sachbezugswert",
                  ],
                ]}
                onChange={(value) => update({ eigenanteil_variante: value })}
              />
              <Cent
                label="Monatslimit"
                value={row().monatslimit}
                onChange={(value) => update({ monatslimit: value })}
              />
              <Choice
                label="Limit-Modus"
                value={row().limit_modus}
                options={[
                  ["warnen", "Warnen"],
                  ["blockieren", "Blockieren"],
                ]}
                onChange={(value) => update({ limit_modus: value })}
              />
              <Button onClick={() => void save()}>Speichern</Button>
            </CardContent>
          </Card>
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
    <LabeledField
      label={props.label}
      inputmode="numeric"
      value={String(props.value)}
      onChange={(value) => props.onChange(Number(value))}
    />
  );
}
