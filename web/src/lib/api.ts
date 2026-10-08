type ProblemBody = {
  code?: string;
  detail?: string;
  title?: string;
  felder?: { feld: string; code: string; text: string }[];
};

export class ApiError extends Error {
  status: number;
  code: string;
  felder: { feld: string; code: string; text: string }[];

  constructor(status: number, body: ProblemBody | null) {
    super(body?.detail || body?.title || "Anfrage fehlgeschlagen");
    this.status = status;
    this.code = body?.code ?? "";
    this.felder = body?.felder ?? [];
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, { ...init, headers, credentials: "include" });
  if (res.status === 204) {
    return undefined as T;
  }
  const text = await res.text();
  const data = text ? (JSON.parse(text) as unknown) : null;
  if (!res.ok) {
    throw new ApiError(res.status, (data ?? null) as ProblemBody | null);
  }
  return data as T;
}

export type Warnung = { code: string; text: string; beleg_id?: string };
export type Berechnung = {
  jahr: number;
  zuschuss_cent: number;
  sbw_cent: number;
  hoechstzuschuss_cent: number;
  anerkannt_cent: number;
  erstattung_cent: number;
  eigenanteil_cent: number;
  gv_cent: number;
  steuerfrei_cent: number;
  regulaer_cent: number;
};
export type Bild = {
  id: string;
  seite: number | null;
  url: string;
  thumbnail_url: string;
  erkennung: { status: string };
};
export type Beleg = {
  id: string;
  datum: string;
  mahlzeit: string;
  bezugsort: string;
  arbeitsort: string;
  haendler_name: string;
  haendler_ort: string;
  belegbetrag_cent: number;
  korrigierter_betrag_cent: number | null;
  korrektur_grund: string | null;
  notiz: string;
  quelle: string;
  bilder: Bild[];
  berechnung: Berechnung;
  warnungen: Warnung[];
  monat_status: string;
  version: number;
};
export type Jahresregel = {
  jahr: number;
  zuschuss_cent: number;
  mahlzeiten: string[];
  standard_mahlzeit: string;
  sbw_fruehstueck_cent: number;
  sbw_mittag_cent: number;
  sbw_abend_cent: number;
  hoechstzuschuss_aufschlag_cent: number;
  pauschalierung: boolean;
  pauschsteuersatz_bp: number;
  soli_satz_bp: number;
  gehaltsumwandlung: boolean;
  bundesland: string;
  kist_satz_bp: number;
  eigenanteil_variante: string;
  monatslimit: number;
  limit_modus: string;
  eigene_feiertage: { datum: string; name: string }[];
  notiz: string;
  sbw_status?: string;
  aenderungsgrund?: string | null;
};
export type Einstellungen = {
  arbeitnehmer_name: string;
  personalnummer: string;
  arbeitgeber_name: string;
  standard_bezugsort: string;
  standard_arbeitsort: string;
  erkennung_aktiv: boolean;
  export_zip_standard: boolean;
  export_csv_standard: boolean;
  geaendert_am: string;
};
export type Monat = {
  monat: string;
  status: string;
  letzte_exportversion: number;
  jahresregel: Jahresregel | null;
  tage: {
    datum: string;
    wochentag: number;
    wochenende: boolean;
    feiertag: string | null;
    beleg_id: string | null;
  }[];
  belege: Beleg[];
  summen: {
    anzahl: number;
    erstattung_cent: number;
    eigenanteil_cent: number;
    gv_cent: number;
    ag_kosten_cent: number;
    pauschal_gesamt_cent: number;
  };
  pruefpunkte: { code: string; ergebnis: string }[];
  warnungen: Warnung[];
};
export type AuthConfig = {
  passwort: boolean;
  oidc: boolean;
  oidc_label: string;
};
export type SystemInfo = {
  version: string;
  commit: string;
  storage_backend: string;
  erkennung_konfiguriert: boolean;
  llm_model: string;
  llm_base_url: string;
  typst_version: string;
};

const json = (body: unknown) => JSON.stringify(body);

export const client = {
  config: () => api<AuthConfig>("/api/v1/auth/config"),
  login: (passwort: string) =>
    api<void>("/api/v1/auth/login", {
      method: "POST",
      body: json({ passwort }),
    }),
  logout: () => api<void>("/api/v1/auth/logout", { method: "POST" }),
  me: () =>
    api<{ akteur: string; methode: string; sitzung_seit: string }>(
      "/api/v1/auth/me",
    ),
  einstellungen: () => api<Einstellungen>("/api/v1/einstellungen"),
  putEinstellungen: (body: Einstellungen) =>
    api<Einstellungen>("/api/v1/einstellungen", {
      method: "PUT",
      body: json(body),
    }),
  regeln: () => api<Jahresregel[]>("/api/v1/jahresregeln"),
  regel: (jahr: number) => api<Jahresregel>(`/api/v1/jahresregeln/${jahr}`),
  vorschlag: (jahr: number) =>
    api<Jahresregel>(`/api/v1/jahresregeln/${jahr}/vorschlag`),
  putRegel: (body: Jahresregel) =>
    api<Jahresregel>(`/api/v1/jahresregeln/${body.jahr}`, {
      method: "PUT",
      body: json(body),
    }),
  beleg: (id: string) => api<Beleg>(`/api/v1/belege/${id}`),
  preview: (body: unknown) =>
    api<{
      berechnung: Berechnung;
      warnungen: Warnung[];
      monat_status?: string;
    }>("/api/v1/belege/vorschau", { method: "POST", body: json(body) }),
  createBeleg: (body: unknown) =>
    api<Beleg>("/api/v1/belege", { method: "POST", body: json(body) }),
  patchBeleg: (id: string, body: unknown) =>
    api<Beleg>(`/api/v1/belege/${id}`, { method: "PATCH", body: json(body) }),
  deleteBeleg: (id: string, version: number, aenderungsgrund?: string) =>
    api<void>(`/api/v1/belege/${id}`, {
      method: "DELETE",
      body: json({ version, aenderungsgrund }),
    }),
  monat: (monat: string) => api<Monat>(`/api/v1/monate/${monat}`),
  info: () => api<SystemInfo>("/api/v1/system/info"),
  sitzungen: () =>
    api<
      {
        erstellt_am: string;
        user_agent: string;
        ip: string;
        aktuell: boolean;
      }[]
    >("/api/v1/auth/sitzungen"),
  deleteOtherSessions: () =>
    api<void>("/api/v1/auth/sitzungen", { method: "DELETE" }),
  protokoll: () =>
    api<{ ok: boolean; anzahl: number }>("/api/v1/protokoll/pruefen"),
  async upload(file: Blob): Promise<Bild> {
    const data = new FormData();
    data.set("datei", file, "beleg.jpg");
    const res = await fetch("/api/v1/belegbilder", {
      method: "POST",
      body: data,
      credentials: "include",
    });
    const body = (await res.json()) as Bild & { detail?: string };
    if (!res.ok) {
      throw new ApiError(res.status, body);
    }
    return body;
  },
};
