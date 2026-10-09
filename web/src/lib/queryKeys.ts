export const queryKeys = {
  me: ["me"] as const,
  authConfig: ["auth-config"] as const,
  einstellungen: ["einstellungen"] as const,
  regeln: ["regeln"] as const,
  regel: (jahr: number) => ["regel", jahr] as const,
  vorschlag: (jahr: number) => ["vorschlag", jahr] as const,
  monatAll: ["monat"] as const,
  monat: (monat: string) => ["monat", monat] as const,
  beleg: (id: string) => ["beleg", id] as const,
  bild: (id: string) => ["bild", id] as const,
  info: ["info"] as const,
  sitzungen: ["sitzungen"] as const,
};
