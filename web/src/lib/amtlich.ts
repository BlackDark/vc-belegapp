export const amtlich: Record<
  number,
  { fruehstueck: number; mittag: number; abend: number }
> = {
  2025: { fruehstueck: 230, mittag: 440, abend: 440 },
  2026: { fruehstueck: 237, mittag: 457, abend: 457 },
  2027: { fruehstueck: 243, mittag: 470, abend: 470 },
};

export const kistVorschlag: Record<string, number> = {
  BW: 450,
  BY: 700,
  BE: 500,
  BB: 500,
  HB: 700,
  HH: 400,
  HE: 700,
  MV: 500,
  NI: 600,
  NW: 700,
  RP: 700,
  SL: 700,
  SN: 500,
  ST: 500,
  SH: 600,
  TH: 500,
};

export const laender = Object.keys(kistVorschlag);

export const bezugsorte = [
  ["supermarkt", "Supermarkt"],
  ["restaurant", "Restaurant"],
  ["kantine", "Kantine"],
  ["baeckerei", "Bäckerei"],
  ["lieferdienst", "Lieferdienst"],
  ["sonstiges", "Sonstiges"],
] as const;

export const mahlzeitLabel: Record<string, string> = {
  fruehstueck: "Frühstück",
  mittag: "Mittag",
  abend: "Abend",
};
