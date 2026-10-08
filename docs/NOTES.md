# Entscheidungen (Interview, 2026-10-08)
- Single-User mit Login.
- Ein Beleg pro Tag; Regeln pro Jahr konfigurierbar; App berechnet Zuschuss/Steueranteile, steuerlich konform (Recherche).
- Gesamtbetrag + optional korrigierter Betrag.
- Belegerkennung: Cloud-Modell hinter austauschbarer Schnittstelle (lokal möglich, z. B. OpenAI-kompatibel).
- Monatsexport: PDF (Pflicht), ZIP/CSV optional.
- Belegtag zählt; Warnungen Wochenende/Feiertag.
- Betrieb: Container (Docker/K8s), non-root, Single-Binary ok; Bilder im Dateisystem oder S3.
- Datenexport/Backup.
- Repo: git@github.com:BlackDark/vc-belegapp.git, mit CI/CD.
- Frontend: SolidJS + shadcn-artige Komponenten (Recherche), PWA mit Kamera-Upload.
- Runde 3: Zuschussregeln (Höhe, Mahlzeit, Pauschalierung, Zusätzlichkeit/Umwandlung, Kirchensteuer/Bundesland, Eigenanteil-Variante, 15er-Limit) alle konfigurierbar pro Jahr.
- Feld "Art der Mahlzeit" (Supermarkt/Restaurant/Kantine; Mahlzeit Frühstück/Mittag/Abend, Standard Mittag).
- Monat nach Export gesperrt, Änderungen nur mit Protokoll.
- KI-Standard: günstiges Vision-Modell (OpenAI gpt-5-mini o. Gemini Flash), Key per Env, Base-URL konfigurierbar.
- Login: Passwort UND OIDC (OIDC primär für Eduard).
- Speicher: Dateisystem UND S3 von Anfang an.
SPEC bestätigt durch Eduard 2026-10-08; Start M0.
