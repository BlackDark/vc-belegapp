import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { Camera, ImagePlus } from "lucide-solid";
import { Show } from "solid-js";
import { client } from "../lib/api";
import { currentMonth, todayISO } from "../lib/dates";
import { downscale } from "../lib/image";
import { formatCent } from "../lib/money";

export function CaptureInputs(props: { onFile: (file: File) => void }) {
  const pick = (event: Event) => {
    const file = (event.currentTarget as HTMLInputElement).files?.[0];
    if (file) {
      props.onFile(file);
    }
  };
  return (
    <div class="grid gap-3">
      <label class="flex min-h-28 cursor-pointer flex-col items-center justify-center gap-2 rounded-2xl bg-emerald-800 text-white dark:bg-emerald-500 dark:text-zinc-950">
        <Camera size={28} />
        <span class="text-lg font-medium">Kamera</span>
        <input
          class="sr-only"
          aria-label="Kamera"
          type="file"
          accept="image/*"
          capture="environment"
          onChange={pick}
        />
      </label>
      <label class="flex min-h-16 cursor-pointer items-center justify-center gap-2 rounded-2xl border border-zinc-300 dark:border-zinc-700">
        <ImagePlus size={20} />
        Galerie
        <input
          class="sr-only"
          aria-label="Galerie"
          type="file"
          accept="image/*"
          onChange={pick}
        />
      </label>
    </div>
  );
}

export default function Heute() {
  const navigate = useNavigate();
  const today = todayISO();
  const month = currentMonth();
  const monat = useQuery(() => ({
    queryKey: ["monat", month],
    queryFn: () => client.monat(month),
    retry: false,
  }));

  const upload = async (file: File) => {
    const blob = await downscale(file);
    const bild = await client.upload(blob);
    navigate(`/belege/neu?bild=${bild.id}`);
  };

  const beleg = () => monat.data?.belege.find((item) => item.datum === today);
  const limit = () => monat.data?.jahresregel?.monatslimit ?? 15;

  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">Heute</h1>
      <Show when={monat.error}>
        <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
          Für dieses Jahr fehlt die Jahresregel.{" "}
          <a
            class="underline"
            href={`/einstellungen/jahre/${today.slice(0, 4)}`}
          >
            Regel anlegen
          </a>
        </p>
      </Show>
      <Show
        when={beleg()}
        fallback={<CaptureInputs onFile={(file) => void upload(file)} />}
      >
        {(item) => (
          <a
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
          </a>
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
