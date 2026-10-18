import { useQuery } from "@tanstack/solid-query";
import { client, type Sachbezugswert, type Werte } from "./api";
import { queryKeys } from "./queryKeys";

// Reference data compiled into the binary and served by GET /werte: the
// official Sachbezugswerte, the simplified church-tax rates, and the German
// labels for meals and purchase places. The app shell starts the request, so
// the screens below usually read it straight from the cache.

export function useWerte() {
  return useQuery(() => ({
    queryKey: queryKeys.werte,
    queryFn: () => client.werte(),
    retry: false,
    staleTime: Infinity,
  }));
}

// useEinstellungen reads the single Einstellungen row. The screens that need it
// either poll it whenever it is mounted or, like the review and export dialogs,
// only while a dialog is open, so the caller may pass an enabled predicate. One
// query key and one query function keep every copy of that row in sync.
export function useEinstellungen(enabled?: () => boolean) {
  return useQuery(() => ({
    queryKey: queryKeys.einstellungen,
    queryFn: () => client.einstellungen(),
    enabled: enabled?.(),
  }));
}

// The helpers take the query data, which is undefined until the request
// resolves. They never hand back a raw code while loading, so a label is
// either the right German text or nothing at all. Every list is read through an
// optional chain, because a nil array from the server must not throw. A screen
// that already left its loading state falls back to the raw code itself, so a
// failed request never leaves a control without a label.

// mahlzeitLabelOf returns the label of a meal code, or undefined when the
// reference table is still loading. Once loaded, an unknown code falls back to
// the code itself, matching how the PDF export renders it.
export function mahlzeitLabelOf(
  werte: Werte | undefined,
  code: string,
): string | undefined {
  if (!werte) {
    return undefined;
  }
  return (
    werte.mahlzeiten?.find((mahlzeit) => mahlzeit.code === code)?.label ?? code
  );
}

// mealCodesOf returns the meal codes in the order the API lists them, so no
// screen keeps its own copy of the list.
export function mealCodesOf(werte: Werte | undefined): string[] {
  return (werte?.mahlzeiten ?? []).map((mahlzeit) => mahlzeit.code);
}

export function bezugsorteOptions(
  werte: Werte | undefined,
): readonly (readonly [string, string])[] {
  return (werte?.bezugsorte ?? []).map((bezugsort) => [
    bezugsort.code,
    bezugsort.label,
  ]);
}

export function bundeslaenderOf(werte: Werte | undefined): string[] {
  return werte?.bundeslaender ?? [];
}

// kistSatzFor returns the suggested simplified church-tax rate in basis
// points for a Bundesland, or the caller's own value when the table has none.
export function kistSatzFor(
  werte: Werte | undefined,
  land: string,
  fallback: number,
): number {
  return (
    werte?.kirchensteuer?.find((satz) => satz.land === land)?.satz_bp ??
    fallback
  );
}

export function sbwFor(
  werte: Werte | undefined,
  jahr: number,
): Sachbezugswert | undefined {
  return werte?.sachbezugswerte?.find((sbw) => sbw.jahr === jahr);
}
