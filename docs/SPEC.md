# vc-belegapp – Spezifikation v1.0

| | |
|---|---|
| Repo | `git@github.com:BlackDark/vc-belegapp.git` (Image: `ghcr.io/blackdark/vc-belegapp`) |
| Stand | 2026-10-08, Interview abgeschlossen |
| Quellen | `NOTES.md`, `GLOSSARY.md`, `docs/adr/0001–0006`, `research/steuer.md`, `research/stack.md` |
| Sprache | UI und Dokumente auf Deutsch; Code, Bezeichner und Commits auf Englisch, fachliche Begriffe im Code als deutsche Domänenwörter (z. B. `Beleg`, `Erstattung`) |

Fachbegriffe sind in `GLOSSARY.md` definiert und werden hier **genau so** verwendet. Steuerliche Herleitung: `research/steuer.md`. Die App ist eine Dokumentations- und Vorberechnungshilfe; die verbindliche Lohnabrechnung macht der Arbeitgeber.

---

## 1. Ziel und Nicht-Ziele

### 1.1 Ziel
Eine selbst gehostete, mobil optimierte Web-App (PWA) für **einen Nutzer** (Eduard), mit der er
1. pro Belegtag einen Beleg fotografiert oder hochlädt,
2. Belegbetrag, Datum, Händler per Belegerkennung vorausgefüllt bekommt, prüft und korrigiert,
3. pro Monat Erstattung, Geldwerten Vorteil, Steuerfreien Anteil und Pauschalsteuer nach konfigurierbaren Jahresregeln berechnet sieht,
4. einen prüfungssicheren Monatsexport (PDF/A-2b, optional ZIP/CSV) für den Arbeitgeber erzeugt, wonach der Monat ein Gesperrter Monat wird,
5. alle Daten per Datenexport sichern und per Datenimport wiederherstellen kann.

### 1.2 Nicht-Ziele (v1)
- Mehrbenutzer, Rollen, Mandanten (Datenmodell bleibt aber erweiterbar, siehe 5.1).
- Verbindliche Lohnsteuer-/SV-Berechnung, individuelle Lohnsteuer, Lohnabrechnung.
- Automatischer Versand an den Arbeitgeber (Mail, HR-API). Der Export wird heruntergeladen.
- Arbeitszeit-, Urlaubs- oder Krankheitserfassung (Belegtag zählt; nur Warnungen).
- Reisekosten/Verpflegungsmehraufwand.
- Mehrere Belege oder mehrere bezuschusste Mahlzeiten pro Tag (genau ein Beleg pro Belegtag; ein Beleg darf bis zu 3 Belegbilder haben, siehe 5.2).
- PDF- oder E-Mail-Belege als Eingabe (nur Bilder, siehe Offene Punkte).
- Offline-Erfassung mit Sync-Queue (PWA nur installierbar + App-Shell-Cache).
- Native Apps, englische UI.

### 1.3 Nicht-funktionale Anforderungen
| ID | Anforderung |
|---|---|
| NF-1 | Upload bis vorausgefüllte Felder ≤ 15 s (p95) mit Cloud-Modell; manuelle Erfassung jederzeit ohne Wartezeit möglich. |
| NF-2 | Monats-PDF mit 23 Belegen ≤ 20 s, Dateigröße ≤ 15 MB. |
| NF-3 | Image ≤ 120 MB (inkl. Typst), RAM im Leerlauf ≤ 64 MiB, Limit 512 MiB. |
| NF-4 | Läuft als UID 65532, read-only Root-FS, keine Capabilities. |
| NF-5 | Browser: iOS Safari ≥ 17, Chrome Android aktuell, Desktop-Evergreen. |
| NF-6 | Barrierearm: Kobalte-Primitives, Touch-Ziele ≥ 44 px, Kontrast WCAG AA in Hell und Dunkel. |
| NF-7 | Beträge intern ausschließlich als Integer-Cent; Prozentsätze als Basispunkte (bp, 1 bp = 0,01 %). |
| NF-8 | Zeitzone fachlich `Europe/Berlin` (konfigurierbar); Datumswerte `YYYY-MM-DD`, Zeitstempel RFC 3339 UTC in der DB. |

---

## 2. Glossar
Verbindlich ist `GLOSSARY.md`. Für diese Spezifikation neu eingeführt und dort ergänzt: Mahlzeitart, Bezugsort, Arbeitsort, Belegbild, Erkennungsergebnis, Korrekturvorschlag, Höchstzuschuss, Regulär steuerpflichtiger Anteil (R), Pauschalsteuer, Eigenanteil-Variante, Gehaltsumwandlung, Monatslimit, Feiertagskalender, Warnung, Prüfpunkt, Monatsstatus, Vorschau, Exportversion, Änderungsprotokoll, Änderungsgrund, Arbeitnehmererklärung, Datenimport, Sitzung.

---

## 3. Architektur (Kurz)
Gemäß ADR 0001–0006:

```
PWA (SolidJS, eingebettet) ─HTTPS─ Reverse Proxy ─ belegapp (Go, :8080, UID 65532)
                                                   ├─ SQLite /data/belegapp.db (WAL)
                                                   ├─ BlobStore: fs (/data/blobs) | s3
                                                   ├─ Job-Worker (Belegerkennung, Datenexport)
                                                   ├─ typst (PDF/A-2b)
                                                   ├─ LLM (OpenAI-kompatibel, extern/lokal)
                                                   └─ OIDC-Provider (optional, primärer Login)
```

Repo-Layout:
```
cmd/belegapp/            main, CLI-Subcommands
internal/config          Env-Parsing, Validierung
internal/server          HTTP-Server, Middleware, Security-Header, SPA-Handler
internal/api             Handler /api/v1
internal/auth            Passwort, Sitzungen, OIDC, CSRF
internal/db              goose-Migrationen (embed), sqlc-Queries, generierter Code
internal/calc            Berechnungsregeln (reine Funktionen)
internal/rules           Jahresregeln, Validierung, amtliche Voreinstellungen
internal/holidays        Feiertagskalender (Wrapper um rickar/cal/v2/de + Eigene Feiertage)
internal/storage         BlobStore-Interface, fs, s3
internal/erkennung       ReceiptExtractor-Interface, openaicompat, fake
internal/imaging         Decode, Orientierung, Resize, JPEG-Encode, Thumbnail
internal/pdf             PDFRenderer-Interface, typstcli, Templates + Fonts (embed)
internal/export          Monatsexport (PDF/CSV/ZIP), Datenexport/-import
internal/audit           Änderungsprotokoll (Hash-Kette)
internal/jobs            SQLite-Jobqueue + Worker
web/                     Vite/Solid-App; web/dist via go:embed
deploy/                  docker-compose.yml, k8s/ (Kustomize)
docs/adr, docs/SPEC.md
```

---

## 4. User Stories und Flows

### 4.1 User Stories
| ID | Als Eduard möchte ich … | damit … |
|---|---|---|
| US-1 | mich per OIDC (primär) oder Passwort anmelden | nur ich Zugriff habe |
| US-2 | auf dem Handy mit einem Tipp den Kassenzettel fotografieren | die Erfassung < 30 s dauert |
| US-3 | Datum, Händler, Belegbetrag vorausgefüllt bekommen | ich wenig tippen muss |
| US-4 | einen Korrigierten Betrag mit Grund eintragen oder den Korrekturvorschlag übernehmen | nur Mahlzeit-Artikel zählen |
| US-5 | Mahlzeitart, Bezugsort und Arbeitsort setzen (Standard aus Einstellungen/Jahresregeln) | die Dokumentation vollständig ist |
| US-6 | sofort Erstattung, Eigenanteil, Geldwerten Vorteil, Steuerfreien Anteil sehen | ich die Wirkung verstehe |
| US-7 | Warnungen bei Wochenende, Feiertag, Monatslimit, Duplikat, Datumsabweichung sehen | ich Fehler vor dem Export bemerke |
| US-8 | eine Monatsansicht (Kalender + Liste + Summen) | ich den Stand jederzeit kenne |
| US-9 | eine Vorschau und einen finalen Monatsexport (PDF, optional ZIP/CSV) erzeugen | ich ihn dem Arbeitgeber schicken und archivieren kann |
| US-10 | Belege in Gesperrten Monaten nur mit Änderungsgrund ändern | die Nachweise prüfungssicher bleiben |
| US-11 | Jahresregeln pro Jahr pflegen (mit amtlichen SBW-Vorschlägen, „aus Vorjahr übernehmen“) | Arbeitgeber-Regeln abgebildet sind |
| US-12 | einen Datenexport herunterladen und per Datenimport wiederherstellen | ich nichts verliere und Server/Storage wechseln kann |
| US-13 | das Änderungsprotokoll einsehen und prüfen | Manipulationen auffallen |

### 4.2 Flow A – Erfassen (mobil)
1. Startseite „Heute“ → Button **„Beleg fotografieren“** (`<input type=file accept="image/*" capture="environment">`) oder **„Aus Galerie“** (ohne `capture`).
2. Client: `createImageBitmap(file, {imageOrientation: "from-image"})` → Canvas, lange Kante ≤ 2000 px, JPEG q=0,85. Fehler beim Dekodieren (z. B. HEIC in Nicht-Safari) → Meldung „Format nicht unterstützt, bitte als JPEG aufnehmen“.
3. `POST /api/v1/belegbilder` (multipart) → Server normalisiert (11.2), speichert Belegbild (noch keinem Beleg zugeordnet), startet Job `erkennung` (falls aktiv), antwortet `201` mit `erkennung.status = "ausstehend"`.
4. Client navigiert sofort zu **Prüfen**: Bild oben, Formular darunter, vorbelegt mit Datum = heute, Mahlzeitart = `standard_mahlzeit` der Jahresregel, Bezugsort/Arbeitsort = Einstellungen. Polling `GET /api/v1/belegbilder/{id}` alle 1 s bis `fertig|fehler` (max. 90 s).
5. Bei `fertig`: Felder, die der Nutzer noch nicht angefasst hat, werden mit dem Erkennungsergebnis gefüllt (Datum, Händler, Ort, Belegbetrag, Bezugsort-Vorschlag). Positionen werden aufklappbar angezeigt; ein abweichender Korrekturvorschlag erscheint als Hinweis mit Button „Übernehmen“ (setzt Korrigierten Betrag + Grund „Automatisch: ohne Pfand/Alkohol/Tabak/Non-Food“).
6. Optional weitere Seite fotografieren („+ Seite“, max. 3 Belegbilder; Erkennung nur auf Seite 1, außer Nutzer startet sie manuell).
7. Jede Feldänderung → debounced (300 ms) `POST /api/v1/belege/vorschau` → Live-Anzeige Berechnung + Warnungen.
8. **Speichern** → `POST /api/v1/belege`. Fehler `422` werden feldbezogen angezeigt. Im Gesperrten Monat öffnet sich vorher der Dialog **Änderungsgrund** (Pflicht, ≥ 5 Zeichen).
9. Erfolg → Toast „Beleg gespeichert“, zurück zu „Heute“ bzw. Monatsansicht.

Bei `erkennung.status = fehler` oder deaktivierter Belegerkennung: Hinweis „Bitte manuell ausfüllen“ + Button „Erneut erkennen“; Speichern ist nie von der Erkennung abhängig.

### 4.3 Flow B – Monatsansicht
`GET /api/v1/monate/{YYYY-MM}`: Kalender (Mo–So), je Tag Status: Beleg (Betrag), Wochenende (gedimmt), Feiertag (Name), Warnsymbol. Darunter Summenkarte und Liste. Monatsstatus-Badge (`offen`/`gesperrt`/`geändert`). Aktionen: „Vorschau“, „Exportieren“, Monatswechsel (Swipe/Pfeile). Tippen auf Tag ohne Beleg → Flow A mit vorbelegtem Datum.

### 4.4 Flow C – Monatsexport
1. „Exportieren“ → Dialog zeigt Prüfpunkte (✓/⚠/✗), Liste aller Warnungen, Optionen „ZIP mit Originalbildern“ / „CSV“ (Standard aus Einstellungen).
2. Pflicht-Checkbox Arbeitnehmererklärung (Text 12.4). Wenn Warnungen existieren: zusätzliche Checkbox „Warnungen geprüft“.
3. „Vorschau“ → `POST …/vorschau` liefert PDF mit Wasserzeichen „ENTWURF“, sperrt nicht.
4. „Final exportieren“ → `POST /api/v1/monate/{m}/exporte` → neue Exportversion n, Monatsstatus `gesperrt`, Downloads PDF/ZIP/CSV.
5. Ändert der Nutzer danach einen Beleg (mit Änderungsgrund) oder die Jahresregel des Jahres, wird der Monatsstatus `geaendert`; Banner „Seit Export Version n geändert – bitte neu exportieren“. Neuer Export → Version n+1 mit Abschnitt „Änderungen gegenüber Version n“.

### 4.5 Flow D – Einstellungen
Profil (Name, Personalnummer, Arbeitgeber), Standardwerte, Jahresregeln (Liste + Editor), Belegerkennung (Status, „Verbindung testen“), Speicher (Backend, read-only), Sitzungen, Datenexport/Datenimport, Änderungsprotokoll, Darstellung (Hell/Dunkel/System), Über (Version, Commit).

### 4.6 Flow E – Datenexport/Datenimport
- Export: „Datenexport erstellen“ → Job → Download ZIP (11.3). Zusätzlich per CLI `belegapp backup --out <datei>`.
- Import: Datei wählen → Server validiert Manifest/Prüfsummen → Dialog zeigt Inhalt (Zeitraum, Anzahl Belege, App-/Schema-Version) → Eingabe des Worts `ERSETZEN` → Server erstellt automatisch Sicherheits-Datenexport des aktuellen Stands, ersetzt alle Daten, beendet alle Sitzungen → Login. CLI: `belegapp restore <datei> --yes`.

---

## 5. Datenmodell (SQLite)

### 5.1 Konventionen
- IDs: ULID (TEXT, 26 Zeichen), außer `jahresregeln.jahr`, `monate.monat`, `aenderungsprotokoll.id` (INTEGER).
- Geld: `*_cent INTEGER`; Sätze: `*_bp INTEGER`; Booleans `INTEGER 0/1`; Datum `TEXT 'YYYY-MM-DD'`; Zeitstempel `TEXT` RFC 3339 UTC.
- Pragmas: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`. Eine Schreib-Connection, Lese-Pool.
- Kein `user_id` in v1 (Single-User); Erweiterung später per Migration.
- Migrationen: goose (eingebettet), beim Start automatisch; `belegapp migrate` für manuellen Lauf.

### 5.2 Tabellen

**`einstellungen`** (genau eine Zeile, `id = 1`)
| Spalte | Typ | Constraint/Default |
|---|---|---|
| id | INTEGER PK | CHECK (id = 1) |
| arbeitnehmer_name | TEXT NOT NULL | '' |
| personalnummer | TEXT NOT NULL | '' |
| arbeitgeber_name | TEXT NOT NULL | '' |
| standard_bezugsort | TEXT NOT NULL | 'supermarkt', CHECK in Bezugsorte |
| standard_arbeitsort | TEXT NOT NULL | 'betrieb', CHECK in ('betrieb','homeoffice') |
| erkennung_aktiv | INTEGER NOT NULL | 1 |
| export_zip_standard | INTEGER NOT NULL | 0 |
| export_csv_standard | INTEGER NOT NULL | 1 |
| geaendert_am | TEXT NOT NULL | |

**`jahresregeln`**
| Spalte | Typ | Constraint/Default |
|---|---|---|
| jahr | INTEGER PK | CHECK 2020–2100 |
| zuschuss_cent | INTEGER NOT NULL | CHECK > 0 |
| mahlzeiten | TEXT NOT NULL | JSON-Array ⊆ {fruehstueck, mittag, abend}, nicht leer; Default `["mittag"]` |
| standard_mahlzeit | TEXT NOT NULL | 'mittag'; muss in `mahlzeiten` enthalten sein |
| sbw_fruehstueck_cent | INTEGER NOT NULL | CHECK > 0 |
| sbw_mittag_cent | INTEGER NOT NULL | CHECK > 0 |
| sbw_abend_cent | INTEGER NOT NULL | CHECK > 0 |
| hoechstzuschuss_aufschlag_cent | INTEGER NOT NULL | 310 |
| pauschalierung | INTEGER NOT NULL | 1 |
| pauschsteuersatz_bp | INTEGER NOT NULL | 2500 |
| soli_satz_bp | INTEGER NOT NULL | 550 |
| gehaltsumwandlung | INTEGER NOT NULL | 0 |
| bundesland | TEXT NOT NULL | CHECK in (BW,BY,BE,BB,HB,HH,HE,MV,NI,NW,RP,SL,SN,ST,SH,TH) |
| kist_satz_bp | INTEGER NOT NULL | CHECK 0–900; Vorschlag aus Bundesland (5.4) |
| eigenanteil_variante | TEXT NOT NULL | 'standard', CHECK in ('standard','vorsichtig') |
| monatslimit | INTEGER NOT NULL | 15, CHECK 1–31 |
| limit_modus | TEXT NOT NULL | 'warnen', CHECK in ('warnen','blockieren') |
| eigene_feiertage | TEXT NOT NULL | '[]' – JSON `[{datum, name}]`, Datum im selben Jahr |
| notiz | TEXT NOT NULL | '' |
| erstellt_am, geaendert_am | TEXT NOT NULL | |

**`belegbilder`**
| Spalte | Typ | Constraint/Default |
|---|---|---|
| id | TEXT PK | ULID |
| beleg_id | TEXT NULL | FK → belege(id); NULL = noch nicht zugeordnet |
| seite | INTEGER NULL | 1–3; UNIQUE(beleg_id, seite) |
| blob_key | TEXT NOT NULL | `bilder/{sha[0:2]}/{sha256}.jpg` |
| thumb_blob_key | TEXT NOT NULL | `thumbs/{sha[0:2]}/{sha256}.jpg` |
| sha256 | TEXT NOT NULL | Hash der gespeicherten Datei; INDEX |
| upload_sha256 | TEXT NOT NULL | Hash der hochgeladenen Bytes; INDEX |
| mime | TEXT NOT NULL | 'image/jpeg' |
| bytes, breite, hoehe | INTEGER NOT NULL | |
| erkennung_status | TEXT NOT NULL | CHECK in ('keine','ausstehend','laeuft','fertig','fehler') |
| erkennung_ergebnis | TEXT NULL | JSON (Schema 10.3), validiert |
| erkennung_roh | TEXT NULL | Rohantwort, ≤ 64 KiB |
| erkennung_fehler | TEXT NULL | |
| erkennung_modell | TEXT NULL | |
| erkennung_dauer_ms | INTEGER NULL | |
| erstellt_am | TEXT NOT NULL | |

Nicht zugeordnete Belegbilder werden nach `BELEGAPP_UNASSIGNED_IMAGE_TTL` gelöscht (Zeile + Blobs, sofern der Blob weder von einem anderen Belegbild noch von einem Monatsexport referenziert wird). Soft-Delete eines Belegs, der in keinem `monatsexporte.beleg_ids` steht, hebt die Zuordnung auf; die Bilder fallen danach unter dieselbe Frist, gerechnet ab `erstellt_am`. Bilder eines exportierten Belegs bleiben zugeordnet. Die Belegzeile und das Änderungsprotokoll bleiben. Blobs unter `bilder/` und `thumbs/`, die keine Zeile haben und älter als die Frist sind, werden entfernt. Neue Bilder liegen inhaltsadressiert (`bilder/{sha[0:2]}/{sha256}.jpg`, `thumbs/{sha[0:2]}/{sha256}.jpg`). Bestehende Schlüssel `bilder/{id}.jpg` und `bilder/{id}.thumb.jpg` werden beim Start und nach einem Datenimport dorthin kopiert; der alte Blob wird gelöscht, wenn keine Zeile und kein Monatsexport ihn noch nennt. Die Kopie ist idempotent. Monatsexport-Blobs werden nie gelöscht.

**`belege`**
| Spalte | Typ | Constraint/Default |
|---|---|---|
| id | TEXT PK | ULID |
| datum | TEXT NOT NULL | Belegtag; partieller UNIQUE-Index `ON belege(datum) WHERE geloescht_am IS NULL` |
| mahlzeit | TEXT NOT NULL | CHECK in ('fruehstueck','mittag','abend') |
| bezugsort | TEXT NOT NULL | CHECK in ('supermarkt','restaurant','kantine','baeckerei','lieferdienst','sonstiges') |
| arbeitsort | TEXT NOT NULL | CHECK in ('betrieb','homeoffice') |
| haendler_name | TEXT NOT NULL | 1–120 Zeichen |
| haendler_ort | TEXT NOT NULL | '' |
| belegbetrag_cent | INTEGER NOT NULL | CHECK 1–100000 |
| korrigierter_betrag_cent | INTEGER NULL | CHECK ≥ 0 AND ≤ belegbetrag_cent |
| korrektur_grund | TEXT NULL | Pflicht (≥ 3 Zeichen), wenn korrigierter_betrag_cent NOT NULL |
| notiz | TEXT NOT NULL | '' (≤ 500) |
| quelle | TEXT NOT NULL | CHECK in ('manuell','ki','ki_korrigiert') |
| version | INTEGER NOT NULL | 1; +1 bei jeder Änderung (optimistic locking) |
| erstellt_am, geaendert_am | TEXT NOT NULL | |
| geloescht_am | TEXT NULL | Soft-Delete |
| loesch_grund | TEXT NULL | |

`quelle`: `ki`, wenn Datum/Händler/Belegbetrag unverändert aus Erkennungsergebnis; `ki_korrigiert`, wenn mindestens eines davon geändert; `manuell` ohne Erkennungsergebnis.

**`monate`**
| Spalte | Typ | Constraint |
|---|---|---|
| monat | TEXT PK | 'YYYY-MM' |
| status | TEXT NOT NULL | CHECK in ('offen','gesperrt','geaendert'); Default 'offen' |
| gesperrt_am | TEXT NULL | |
| letzte_exportversion | INTEGER NOT NULL | 0 |

Zeile wird bei Bedarf angelegt (fehlend = `offen`).

**`monatsexporte`**
| Spalte | Typ | Constraint |
|---|---|---|
| id | TEXT PK | ULID |
| monat | TEXT NOT NULL | FK → monate; UNIQUE(monat, version) |
| version | INTEGER NOT NULL | ≥ 1 |
| erstellt_am | TEXT NOT NULL | |
| pdf_blob_key, pdf_sha256 | TEXT NOT NULL | `exporte/{YYYY-MM}/v{n}/nachweis-{YYYY-MM}-v{n}.pdf` |
| csv_blob_key, csv_sha256 | TEXT NULL | |
| zip_blob_key, zip_sha256 | TEXT NULL | |
| regeln_snapshot | TEXT NOT NULL | JSON der Jahresregel |
| einstellungen_snapshot | TEXT NOT NULL | JSON |
| summen | TEXT NOT NULL | JSON (Format 6.4) |
| beleg_ids | TEXT NOT NULL | JSON-Array mit `{id, version}` |
| erklaerung_bestaetigt_am | TEXT NOT NULL | |
| warnungen_bestaetigt | INTEGER NOT NULL | |
| protokoll_hash | TEXT NOT NULL | Hash des letzten Änderungsprotokoll-Eintrags zum Exportzeitpunkt |

Monatsexporte und ihre Blobs werden nie automatisch gelöscht. `BELEGAPP_EXPORT_RETENTION_YEARS` (Default 10) setzt nur die Aufbewahrungsuhr: `aufbewahrung_bis` ist das Ende des Kalenderjahres `Jahr(erstellt_am) + N` in `BELEGAPP_TZ` (ein Export aus 2026 mit N=6 endet am 31.12.2032, analog § 41 Abs. 1 EStG; der App-Default folgt der Empfehlung „mindestens 10 Jahre“ in `docs/research/steuer.md`). `aufbewahrung_abgelaufen` steht in der Monatsansicht und an den Exportversionen, löst aber keine Löschung aus.

**`aenderungsprotokoll`** (append-only)
| Spalte | Typ | Inhalt |
|---|---|---|
| id | INTEGER PK AUTOINCREMENT | |
| zeitpunkt | TEXT NOT NULL | |
| akteur | TEXT NOT NULL | `passwort` \| `oidc:{sub}` \| `system` \| `cli` |
| aktion | TEXT NOT NULL | `beleg_erstellt`, `beleg_geaendert`, `beleg_geloescht`, `belegbild_hinzugefuegt`, `belegbild_entfernt`, `jahresregel_erstellt`, `jahresregel_geaendert`, `einstellungen_geaendert`, `monatsexport_erstellt`, `datenexport_erstellt`, `datenimport` |
| entitaet, entitaet_id | TEXT NOT NULL | z. B. `beleg`, ULID |
| monat | TEXT NULL | betroffener Monat |
| vorher, nachher | TEXT NULL | JSON (vollständige Entität) |
| diff | TEXT NULL | JSON `[{feld, alt, neu}]` |
| grund | TEXT NULL | Änderungsgrund; Pflicht, wenn Monat zum Zeitpunkt `gesperrt`/`geaendert` war |
| request_id | TEXT NOT NULL | |
| prev_hash | TEXT NOT NULL | Hash des Vorgängers; `0`×64 beim ersten Eintrag |
| hash | TEXT NOT NULL | `hex(sha256(prev_hash ‖ "\n" ‖ kanonisches_json(eintrag ohne hash)))` |

Trigger `BEFORE UPDATE` und `BEFORE DELETE` → `RAISE(ABORT, 'append-only')`. Kanonisches JSON: Schlüssel sortiert, keine Leerzeichen, UTF-8. Einträge werden in derselben Transaktion wie die fachliche Änderung geschrieben.

**`sitzungen`**
| Spalte | Typ | |
|---|---|---|
| token_hash | TEXT PK | sha256(Token) |
| akteur | TEXT NOT NULL | wie oben |
| erstellt_am, zuletzt_aktiv_am, laeuft_ab_am | TEXT NOT NULL | |
| user_agent, ip | TEXT NOT NULL | |

**`jobs`**
| Spalte | Typ | |
|---|---|---|
| id | TEXT PK | |
| typ | TEXT NOT NULL | 'erkennung' \| 'datenexport' |
| payload | TEXT NOT NULL | JSON |
| status | TEXT NOT NULL | 'wartend','laeuft','fertig','fehler' |
| versuche | INTEGER NOT NULL | 0 |
| naechster_versuch_am | TEXT NOT NULL | |
| ergebnis, fehler | TEXT NULL | |
| erstellt_am, geaendert_am | TEXT NOT NULL | |

Beim Start werden Jobs im Status `laeuft` auf `wartend` zurückgesetzt. Worker-Parallelität: `BELEGAPP_JOB_WORKERS`.

**`system`** (Key/Value): `secret_key` (32 Byte, beim ersten Start erzeugt, falls `BELEGAPP_SECRET_KEY` leer), `instanz_id`.

### 5.3 Amtliche Voreinstellungen (im Binary)
| Jahr | SBW Frühstück | SBW Mittag/Abend | Status |
|---|---|---|---|
| 2025 | 230 | 440 | amtlich |
| 2026 | 237 | 457 | amtlich (SvEV 19.12.2025, BMF 29.12.2025) |
| 2027 | 243 | 470 | **Entwurf** (03.09.2026) – UI-Warnung bis Release mit amtlichem Wert |

Neue Jahresregel: Vorschlag = Vorjahresregel (alle Arbeitgeber-Felder) + SBW aus dieser Tabelle; `zuschuss_cent`-Vorschlag = min(Vorjahr, SBW_mittag + 310).

### 5.4 Kirchensteuer-Vorschläge (vereinfachtes Verfahren, 2026, im Binary)
| Land | bp | Land | bp | Land | bp | Land | bp |
|---|---|---|---|---|---|---|---|
| BW | 450 | BY | 700 | BE | 500 | BB | 500 |
| HB | 700 | HH | 400 | HE | 700 | MV | 500 |
| NI | 600 | NW | 700 | RP | 700 | SL | 700 |
| SN | 500 | ST | 500 | SH | 600 | TH | 500 |

Nur Vorschlag beim Wählen des Bundeslands; der gespeicherte Wert ist `kist_satz_bp` (Nachweisverfahren: 0 oder 800/900 manuell). ⚠️ Gegen Ländererlasse verifizieren (research/steuer.md 3.5).

---

## 6. Berechnungsregeln

### 6.1 Eingaben je Beleg
`B` = belegbetrag_cent · `K` = korrigierter_betrag_cent (optional) · `m` = mahlzeit · Jahresregel `J` = jahresregeln[jahr(datum)] · `Z` = J.zuschuss_cent · `S` = J.sbw_{m}_cent · `H` = S + J.hoechstzuschuss_aufschlag_cent (Höchstzuschuss).

### 6.2 Tagesformeln (Integer-Cent)
```
A = K falls gesetzt, sonst B                    // Anerkannter Betrag
falls m ∉ J.mahlzeiten:  E = U = G = F = R = 0; Warnung W_MAHLZEIT_NICHT_BEZUSCHUSST; Ende
E = min(Z, A)                                   // Erstattung
U = A − E                                       // Eigenanteil
falls Z > H:                                    // Höchstzuschuss überschritten → alles Barlohn
    G = 0; F = 0; R = E; Warnung W_HOECHSTZUSCHUSS
sonst falls J.eigenanteil_variante = 'standard':
    G = max(0, min(E, S − U)); F = E − G; R = 0
sonst ('vorsichtig'):
    G = min(E, S); F = E − G; R = 0
```
Invarianten (Property-Tests): `E ≤ Z`, `E ≤ A`, `E = G + F + R`, `0 ≤ G ≤ S`, alle Werte ≥ 0.

### 6.3 Monatsformeln
Belege des Monats (nicht gelöscht), aufsteigend nach `datum`:
```
N  = Anzahl Belege mit E > 0
ΣB, ΣA, ΣE, ΣU, ΣG, ΣF, ΣR = Summen
falls J.pauschalierung:
    LSt  = (ΣG · J.pauschsteuersatz_bp + 5000) div 10000   // kaufmännisch auf Cent
    Soli = (LSt · J.soli_satz_bp) div 10000                // abrunden
    KiSt = (LSt · J.kist_satz_bp) div 10000                // abrunden
    AN_pflichtig = ΣR
sonst:
    LSt = Soli = KiSt = 0
    AN_pflichtig = ΣG + ΣR                                 // regulär lohnsteuer- und SV-pflichtig
Pauschal_gesamt = LSt + Soli + KiSt
AG_Kosten       = ΣE + Pauschal_gesamt
```
Monatslimit: Der k-te Beleg (chronologisch) mit k > J.monatslimit erhält `W_LIMIT_UEBERSCHRITTEN`; bei `limit_modus = blockieren` wird das Speichern eines solchen Belegs abgelehnt (`E_LIMIT_ERREICHT`). Berechnet wird immer normal.

Wenn `J.gehaltsumwandlung = 1` und `J.pauschalierung = 1`: Regel-Warnung `W_REGEL_GEHALTSUMWANDLUNG` (§ 40 Abs. 2 Satz 1 Nr. 1 Satz 2 EStG); Berechnung folgt der Konfiguration, PDF zeigt den Hinweis.

Alle Zahlen sind indikativ; PDF und UI tragen den Hinweis „Vorberechnung – maßgeblich ist die Lohnabrechnung des Arbeitgebers“.

### 6.4 Summen-JSON (API, Export-Snapshot)
```json
{ "anzahl": 5, "belegbetrag_cent": 4097, "anerkannt_cent": 3857, "erstattung_cent": 3301,
  "eigenanteil_cent": 556, "gv_cent": 1678, "steuerfrei_cent": 1623, "regulaer_cent": 0,
  "pauschalsteuer_cent": 420, "soli_cent": 23, "kist_cent": 29, "pauschal_gesamt_cent": 472,
  "an_pflichtig_cent": 0, "ag_kosten_cent": 3773 }
```

### 6.5 Testvektoren (verbindlich, `internal/calc/testdata/vektoren.json`)
Tagesvektoren (Werte in Cent; Spalten: A, Z, S, Variante → E, U, G, F, R):

| ID | A | Z | S | Variante | E | U | G | F | R | Bemerkung |
|---|---|---|---|---|---|---|---|---|---|---|
| T01 | 840 | 767 | 457 | standard | 767 | 73 | 384 | 383 | 0 | Beispiel Mo |
| T01v | 840 | 767 | 457 | vorsichtig | 767 | 73 | 457 | 310 | 0 | |
| T02 | 620 | 767 | 457 | standard | 620 | 0 | 457 | 163 | 0 | Beleg < Zuschuss |
| T03 | 1250 | 767 | 457 | standard | 767 | 483 | 0 | 767 | 0 | K=1250 bei B=1490 |
| T03v | 1250 | 767 | 457 | vorsichtig | 767 | 483 | 457 | 310 | 0 | |
| T04 | 767 | 767 | 457 | standard | 767 | 0 | 457 | 310 | 0 | |
| T05 | 380 | 767 | 457 | standard | 380 | 0 | 380 | 0 | 0 | |
| T06 | 1000 | 310 | 457 | standard | 310 | 690 | 0 | 310 | 0 | U ≥ S |
| T06v | 1000 | 310 | 457 | vorsichtig | 310 | 690 | 310 | 0 | 0 | |
| T07 | 1000 | 500 | 457 | standard | 500 | 500 | 0 | 500 | 0 | |
| T08 | 600 | 800 | 457 | standard | 600 | 0 | 0 | 0 | 600 | Z > H (757) |
| T09 | 600 | 767 | 237 | standard | 600 | 0 | 0 | 0 | 600 | Frühstück, Z > H (547) |
| T10 | 500 | 547 | 237 | standard | 500 | 0 | 237 | 263 | 0 | Frühstück an der Grenze |
| T11 | 967 | 767 | 457 | standard | 767 | 200 | 257 | 510 | 0 | Haufe-Beispiel |
| T11v | 967 | 767 | 457 | vorsichtig | 767 | 200 | 457 | 310 | 0 | |
| T12 | 800 | 750 | 440 | standard | 750 | 50 | 390 | 360 | 0 | Werte 2025 |
| T13 | 840 | 767 | 457 | standard, m ∉ mahlzeiten | 0 | 0 | 0 | 0 | 0 | W_MAHLZEIT_NICHT_BEZUSCHUSST |

Monatsvektoren:

| ID | Eingabe | ΣE | ΣU | ΣG | ΣF | ΣR | LSt | Soli | KiSt | AG_Kosten | AN_pflichtig |
|---|---|---|---|---|---|---|---|---|---|---|---|
| M1 | T01,T02,T03,T04,T05; pauschal, kist 700 | 3301 | 556 | 1678 | 1623 | 0 | 420 | 23 | 29 | 3773 | 0 |
| M2 | wie M1, vorsichtig | 3301 | 556 | 2208 | 1093 | 0 | 552 | 30 | 38 | 3921 | 0 |
| M3 | 15 × (A=767, Z=767, S=457), kist 700 | 11505 | 0 | 6855 | 4650 | 0 | 1714 | 94 | 119 | 13432 | 0 |
| M4 | wie M3, kist 450 (BW) | 11505 | 0 | 6855 | 4650 | 0 | 1714 | 94 | 77 | 13390 | 0 |
| M5 | wie M3, kist 0 | 11505 | 0 | 6855 | 4650 | 0 | 1714 | 94 | 0 | 13313 | 0 |
| M6 | ΣG = 2 (Rundung) | – | – | 2 | – | 0 | 1 | 0 | 0 | ΣE+1 | 0 |
| M7 | wie M1, pauschalierung = 0 | 3301 | 556 | 1678 | 1623 | 0 | 0 | 0 | 0 | 3301 | 1678 |
| M8 | 16 × T04, Limit 15, warnen, kist 700 → 16. Beleg mit W_LIMIT_UEBERSCHRITTEN | 12272 | 0 | 7312 | 4960 | 0 | 1828 | 100 | 127 | 14327 | 0 |

Beispielmonat M1 (Oktober 2026, NW) mit Belegen: Mo 05.10. REWE 8,40 · Di 06.10. Bäckerei 6,20 · Mi 07.10. Edeka 14,90 (korrigiert 12,50, Grund „Pfand/Drogerie“) · Do 08.10. Lidl 7,67 · Fr 09.10. Kiosk 3,80. Dieser Monat ist zugleich die Fixture für PDF-, API- und E2E-Tests.

---

## 7. Validierungen, Warnungen, Prüfpunkte

### 7.1 Harte Fehler (HTTP 422, Problem-JSON mit `code` und `felder`)
| Code | Bedingung |
|---|---|
| E_DATUM_BELEGT | Für `datum` existiert bereits ein nicht gelöschter Beleg |
| E_DATUM_ZUKUNFT | `datum` > heute (Zeitzone) |
| E_JAHRESREGEL_FEHLT | Keine Jahresregel für das Jahr |
| E_MAHLZEIT_NICHT_ERLAUBT | `mahlzeit` ∉ J.mahlzeiten (beim Speichern) |
| E_BETRAG_UNGUELTIG | Belegbetrag ∉ [1, 100000] |
| E_KORREKTUR_ZU_HOCH | korrigiert > Belegbetrag |
| E_KORREKTUR_GRUND_FEHLT | korrigiert gesetzt, Grund fehlt |
| E_BILDER_ANZAHL | 0 oder > 3 Belegbilder |
| E_BILD_UNBEKANNT | Bild-ID existiert nicht oder gehört zu anderem Beleg |
| E_BILD_FORMAT / E_BILD_ZU_GROSS | Upload kein JPEG/PNG/WebP bzw. > Limit |
| E_LIMIT_ERREICHT | `limit_modus = blockieren` und Monatslimit erreicht |
| E_AENDERUNGSGRUND_FEHLT | Änderung/Löschung in Monat mit Status `gesperrt`/`geaendert` ohne `aenderungsgrund` (≥ 5 Zeichen) |
| E_VERSION_KONFLIKT (409) | `version` passt nicht |
| E_ERKLAERUNG_FEHLT | Finaler Export ohne `erklaerung_bestaetigt` |
| E_WARNUNGEN_UNBESTAETIGT | Finaler Export mit Warnungen ohne `warnungen_bestaetigt` |
| E_REGEL_UNGUELTIG | Jahresregel verletzt Constraints (5.2) |
| E_PRUEFPUNKT_FEHLGESCHLAGEN | Finaler Export bei Prüfpunkt-Ergebnis ✗ (7.3) |
| E_EXPORT_LAEUFT (409) | Export desselben Monats läuft bereits |
| E_IMPORT_UNGUELTIG / E_IMPORT_ZU_NEU | Datenimport: Manifest/Prüfsumme falsch bzw. Schema neuer als App |
| E_PDF_FEHLER (500) | Typst-Aufruf fehlgeschlagen oder Timeout |

### 7.2 Warnungen (nicht blockierend, in Beleg- und Monatsantwort)
| Code | Bedingung | Text (UI) |
|---|---|---|
| W_WOCHENENDE | Sa/So | „Belegtag ist ein Samstag/Sonntag.“ |
| W_FEIERTAG | Datum im Feiertagskalender | „Belegtag ist ein Feiertag: {name}.“ |
| W_LIMIT_UEBERSCHRITTEN | siehe 6.3 | „{k}. Beleg im Monat – Monatslimit {limit} überschritten.“ |
| W_DATUM_ABWEICHUNG | Erkennungsergebnis.datum ≠ datum | „Datum laut Beleg ({d}) weicht vom Belegtag ab (Vorratskauf ist nicht zulässig).“ |
| W_DUPLIKAT_BILD | gleicher `sha256` oder `upload_sha256` bei anderem Beleg | „Dieses Bild wurde bereits für {datum} verwendet.“ |
| W_DUPLIKAT_INHALT | anderer Beleg mit gleichem erkanntem Datum + Belegbetrag + Händler | „Möglicherweise derselbe Kassenzettel wie {datum}.“ |
| W_HOECHSTZUSCHUSS | Z > H | „Zuschuss übersteigt Sachbezugswert + 3,10 € – Erstattung ist voll steuerpflichtig.“ |
| W_MAHLZEIT_NICHT_BEZUSCHUSST | m ∉ J.mahlzeiten (nachträglich geänderte Regel) | „Mahlzeitart wird laut Jahresregel nicht bezuschusst.“ |
| W_KI_UNSICHER | Konfidenz < 0,6 oder Σ Positionen weicht > 5 ct vom Gesamtbetrag ab | „Erkennung unsicher – Werte bitte prüfen.“ |
| W_BEZUGSORT_KANTINE | bezugsort = kantine | „Nur zulässig, wenn die Kantine nicht vom Arbeitgeber betrieben oder bezuschusst wird.“ |
| W_REGEL_GEHALTSUMWANDLUNG | siehe 6.3 | „Pauschalierung ist bei Gehaltsumwandlung i. d. R. nicht zulässig – mit Arbeitgeber klären.“ |
| W_SBW_ENTWURF | SBW-Werte stammen aus Entwurf (5.3) | „Sachbezugswerte {jahr} sind noch nicht amtlich.“ |
| W_GEAENDERT_NACH_EXPORT | Beleg nach letztem Export geändert | „Nach Export Version {n} geändert.“ |

### 7.3 Prüfpunkte (Monatsexport, im PDF mit ✓/⚠/✗)
| Code | Prüfung | Ergebnis bei Verstoß |
|---|---|---|
| P_EIN_BELEG_PRO_TAG | höchstens ein Beleg je Datum | ✗ (durch Constraint ausgeschlossen) |
| P_ERSTATTUNG_LE_BELEG | E ≤ A für alle | ✗ |
| P_HOECHSTZUSCHUSS | Z ≤ H für alle verwendeten Mahlzeitarten | ⚠ |
| P_MONATSLIMIT | N ≤ monatslimit | ⚠ |
| P_KEINE_DUPLIKATE | keine W_DUPLIKAT_* | ⚠ |
| P_BILDER_VOLLSTAENDIG | jeder Beleg ≥ 1 Belegbild, Blob vorhanden, Hash stimmt | ✗ |
| P_ARBEITSTAGE | keine Belege an Wochenende/Feiertag | ⚠ |
| P_DATUM_KONSISTENT | keine W_DATUM_ABWEICHUNG | ⚠ |
| P_PROTOKOLL_INTAKT | Hash-Kette des Änderungsprotokolls gültig | ✗ |

✗ blockiert den finalen Export (`422 E_PRUEFPUNKT_FEHLGESCHLAGEN`), ⚠ erfordert `warnungen_bestaetigt`.

Die Monatsansicht prüft `P_BILDER_VOLLSTAENDIG` per Stat (Blob vorhanden, Größe = `bytes`) und `P_PROTOKOLL_INTAKT` nur am letzten Eintrag. Finaler Export und PDF-Vorschau hashen jedes Bild und prüfen die gesamte Hash-Kette. Schlägt der finale Export mit `E_PRUEFPUNKT_FEHLGESCHLAGEN` fehl, lädt der Export-Dialog `GET /monate/{monat}/pruefpunkte` und ersetzt die angezeigten Häkchen durch dieses Ergebnis.

### 7.4 Feiertagskalender
- Quelle: `github.com/rickar/cal/v2/de` (gesetzliche Feiertage je Bundesland, landesweit) + `jahresregeln.eigene_feiertage`.
- Bundesland = `jahresregeln.bundesland` (dasselbe Feld liefert den Vorschlag für `kist_satz_bp`).
- Nicht abgedeckt: nur regional geltende Feiertage (z. B. Augsburger Friedensfest, Fronleichnam in Teilen von SN/TH, Mariä Himmelfahrt in Teilen von BY) → über `eigene_feiertage` pflegen.
- Implementierung hinter Interface `holidays.Provider { Feiertage(jahr int, land string) []Feiertag }`, Tests für alle 16 Länder 2026.

---

## 8. API (REST, JSON)

### 8.1 Allgemein
- Basis `/api/v1`; JSON UTF-8; Felder `snake_case`; Geld in Cent; Datum `YYYY-MM-DD`; Monat `YYYY-MM`.
- Authentifizierung per Sitzungs-Cookie; alle Endpunkte außer `auth/config`, `auth/login`, `auth/oidc/*`, `/healthz`, `/readyz` erfordern eine Sitzung (`401` sonst).
- Zustandsändernde Requests: CSRF-Schutz (9.4); Content-Type `application/json` oder `multipart/form-data`.
- Fehler: RFC 9457 `application/problem+json`:
  ```json
  { "type": "about:blank", "title": "Validierung fehlgeschlagen", "status": 422,
    "code": "E_KORREKTUR_GRUND_FEHLT", "detail": "…",
    "felder": [{ "feld": "korrektur_grund", "code": "E_KORREKTUR_GRUND_FEHLT", "text": "Bitte Grund angeben." }],
    "request_id": "01J…" }
  ```
- Jede Antwort trägt `X-Request-Id`. Optimistic Locking über Feld `version` im Body.

### 8.2 Endpunkte
| Methode | Pfad | Zweck |
|---|---|---|
| GET | `/auth/config` | `{passwort: bool, oidc: bool, oidc_label: string}` |
| POST | `/auth/login` | `{passwort}` → `204` + Cookie; `401` falsch; `429` Rate-Limit |
| POST | `/auth/logout` | `204`, löscht Sitzung |
| GET | `/auth/oidc/start` | `302` zum IdP (PKCE, state, nonce) |
| GET | `/auth/oidc/callback` | Code-Austausch → `302 /` mit Cookie; Fehler → `302 /login?fehler=…` |
| GET | `/auth/me` | `{akteur, methode, sitzung_seit}` |
| GET | `/auth/sitzungen` | Liste eigener Sitzungen (ohne Token) |
| DELETE | `/auth/sitzungen` | alle anderen Sitzungen beenden |
| GET/PUT | `/einstellungen` | Objekt 5.2 `einstellungen` |
| GET | `/jahresregeln` | Liste |
| GET | `/jahresregeln/{jahr}` | Jahresregel oder `404` |
| GET | `/jahresregeln/{jahr}/vorschlag` | Vorschlag nach 5.3 (nicht gespeichert) |
| PUT | `/jahresregeln/{jahr}` | anlegen/ändern; Body = Jahresregel + optional `aenderungsgrund` (Pflicht, wenn ein Monat des Jahres `gesperrt`/`geaendert` ist; betroffene `gesperrt`-Monate → `geaendert`) |
| GET | `/feiertage?jahr=2026&bundesland=NW` | `[{datum, name}]` inkl. eigener Feiertage |
| POST | `/belegbilder` | multipart `datei`, optional `erkennung=false` → `201` Belegbild |
| GET | `/belegbilder/{id}` | Belegbild inkl. Erkennungsstatus/-ergebnis |
| POST | `/belegbilder/{id}/erkennung` | Erkennung (erneut) starten → `202` |
| GET | `/belegbilder/{id}/datei` · `/thumbnail` | Bild (`Cache-Control: private, max-age=31536000, immutable`) |
| DELETE | `/belegbilder/{id}` | nur nicht zugeordnete Bilder → `204` |
| POST | `/belege/vorschau` | Beleg-Body ohne Speichern → `{berechnung, warnungen}` |
| POST | `/belege` | anlegen → `201` Beleg |
| GET | `/belege/{id}` | Beleg |
| PATCH | `/belege/{id}` | Teiländerung, Pflicht `version`; `aenderungsgrund` falls Monat gesperrt/geändert |
| DELETE | `/belege/{id}` | Soft-Delete; Body `{version, aenderungsgrund?}` |
| GET | `/monate/{monat}` | Monatsansicht (8.3) |
| GET | `/monate/{monat}/pruefpunkte` | Export-Prüfung `{pruefpunkte}` (Hash je Bild, volle Hash-Kette) |
| POST | `/monate/{monat}/vorschau` | `application/pdf` mit Wasserzeichen ENTWURF |
| POST | `/monate/{monat}/exporte` | finaler Monatsexport → `201` Monatsexport |
| GET | `/monate/{monat}/exporte` | Liste der Exportversionen |
| GET | `/exporte/{id}/pdf` · `/csv` · `/zip` | Download (`Content-Disposition: attachment`) |
| GET | `/protokoll?monat=&entitaet_id=&limit=100&vor_id=` | Änderungsprotokoll (absteigend, Cursor `vor_id`) |
| GET | `/protokoll/pruefen` | `{ok, anzahl, erster_fehler_id}` |
| POST | `/datenexport` | Job starten → `202 {job_id}` |
| GET | `/jobs/{id}` | `{status, fehler, ergebnis}`; bei Datenexport `ergebnis.download_url` |
| GET | `/datenexport/{job_id}/datei` | ZIP-Download (24 h verfügbar, danach gelöscht) |
| POST | `/datenimport/pruefen` | multipart `datei` → Manifest-Zusammenfassung + `import_token` (gültig 30 min) |
| POST | `/datenimport` | `{import_token, bestaetigung: "ERSETZEN"}` → `200`, alle Sitzungen beendet |
| POST | `/erkennung/test` | Testaufruf mit eingebautem Beispielbild → `{ok, modell, dauer_ms, fehler?}` |
| GET | `/system/info` | `{version, commit, build_datum, storage_backend, erkennung_konfiguriert, oidc, typst_version}` |

### 8.3 Ressourcen (Beispiele)
**Belegbild**
```json
{ "id": "01JA…", "beleg_id": null, "seite": null, "sha256": "…", "bytes": 412331,
  "breite": 1500, "hoehe": 2000, "url": "/api/v1/belegbilder/01JA…/datei",
  "thumbnail_url": "/api/v1/belegbilder/01JA…/thumbnail",
  "erkennung": { "status": "fertig", "modell": "gpt-5-mini", "dauer_ms": 4210,
                 "ergebnis": { …Schema 10.3… }, "korrekturvorschlag_cent": 1250, "fehler": null },
  "duplikat_von": null }
```

**Beleg – Request (POST/PATCH)**
```json
{ "datum": "2026-10-07", "mahlzeit": "mittag", "bezugsort": "supermarkt", "arbeitsort": "betrieb",
  "haendler_name": "Edeka", "haendler_ort": "Köln", "belegbetrag_cent": 1490,
  "korrigierter_betrag_cent": 1250, "korrektur_grund": "Pfand 0,25 €, Drogerie 2,15 €",
  "notiz": "", "bild_ids": ["01JA…"], "version": 1, "aenderungsgrund": null }
```

**Beleg – Response**
```json
{ "id": "01JB…", "datum": "2026-10-07", "mahlzeit": "mittag", "bezugsort": "supermarkt",
  "arbeitsort": "betrieb", "haendler_name": "Edeka", "haendler_ort": "Köln",
  "belegbetrag_cent": 1490, "korrigierter_betrag_cent": 1250, "korrektur_grund": "…",
  "notiz": "", "quelle": "ki_korrigiert", "bilder": [ {…Belegbild…} ],
  "berechnung": { "jahr": 2026, "zuschuss_cent": 767, "sbw_cent": 457, "hoechstzuschuss_cent": 767,
                  "anerkannt_cent": 1250, "erstattung_cent": 767, "eigenanteil_cent": 483,
                  "gv_cent": 0, "steuerfrei_cent": 767, "regulaer_cent": 0 },
  "warnungen": [ { "code": "W_DATUM_ABWEICHUNG", "text": "…", "details": { "datum_beleg": "2026-10-06" } } ],
  "monat_status": "offen", "version": 2, "erstellt_am": "…", "geaendert_am": "…" }
```

**Monat**
```json
{ "monat": "2026-10", "status": "gesperrt", "letzte_exportversion": 1,
  "jahresregel": { …5.2… },
  "tage": [ { "datum": "2026-10-03", "wochentag": 6, "wochenende": true,
              "feiertag": "Tag der Deutschen Einheit", "beleg_id": null } ],
  "belege": [ {…Beleg…} ], "summen": { …6.4… },
  "pruefpunkte": [ { "code": "P_MONATSLIMIT", "ergebnis": "ok" } ],
  "warnungen": [ { "code": "W_FEIERTAG", "beleg_id": "…", "text": "…" } ],
  "exporte": [ { "id": "…", "version": 1, "erstellt_am": "…", "pdf": true, "csv": true, "zip": false,
                 "aufbewahrung_bis": "2036-12-31T22:59:59Z", "aufbewahrung_abgelaufen": false } ] }
```

**Monatsexport – Request**
```json
{ "erklaerung_bestaetigt": true, "warnungen_bestaetigt": true, "csv": true, "zip": false }
```
Antwort `201`: `{id, monat, version, erstellt_am, pdf_url, csv_url, zip_url, pdf_sha256}`. Läuft synchron (Timeout `BELEGAPP_PDF_TIMEOUT`); paralleler Export desselben Monats → `409 E_EXPORT_LAEUFT`.

---

## 9. Authentifizierung und Sicherheit

### 9.1 Login-Methoden
- Beide Methoden sind gleichzeitig nutzbar. Passwort aktiv, wenn `BELEGAPP_AUTH_PASSWORD_HASH(_FILE)` gesetzt; OIDC aktiv, wenn `BELEGAPP_OIDC_ISSUER_URL` gesetzt. Start bricht ab, wenn keine Methode konfiguriert ist.
- Login-Seite: OIDC-Button primär (oben, groß), Passwortfeld darunter (falls aktiv). Verstecktes Feld `username` (Wert „belegapp“, `autocomplete=username`) für Passwortmanager.

### 9.2 Passwort
- Hash: argon2id, PHC-String `$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>`; Erzeugung `belegapp hash-password` (liest von stdin, kein Echo). Vergleich konstantzeitig.
- Kein Passwortwechsel in der UI (Konfiguration per Env/Secret).
- Rate-Limit: 5 Fehlversuche je IP / 15 min → `429` mit `Retry-After`; zusätzlich global 20 / 15 min. Client-IP nur aus `X-Forwarded-For`, wenn Remote-Adresse in `BELEGAPP_TRUSTED_PROXIES`.

### 9.3 Sitzungen
- Token: 32 Byte `crypto/rand`, base64url; Cookie `__Host-belegapp_session` (`Secure; HttpOnly; SameSite=Lax; Path=/`). Bei `BELEGAPP_COOKIE_SECURE=false` (nur lokal) Name `belegapp_session` ohne `Secure`.
- DB speichert nur `sha256(token)`. Idle-Timeout `BELEGAPP_SESSION_IDLE_TIMEOUT` (720 h), absolut `BELEGAPP_SESSION_ABSOLUTE_TIMEOUT` (2160 h); `zuletzt_aktiv_am` höchstens alle 5 min aktualisieren. Neues Token bei jedem Login; abgelaufene Sitzungen stündlich löschen.

### 9.4 CSRF und Header
- `http.CrossOriginProtection` (Go ≥ 1.25) für alle Nicht-GET/HEAD/OPTIONS; `AddTrustedOrigin(BELEGAPP_BASE_URL)`. Zusätzlich `SameSite=Lax`.
- Security-Header: `Content-Security-Policy: default-src 'self'; img-src 'self' blob: data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `Permissions-Policy: camera=(self), geolocation=()`. HSTS setzt der Reverse Proxy.
- Request-Body-Limits: JSON 1 MiB, Bild `BELEGAPP_UPLOAD_MAX_BYTES`, Import `BELEGAPP_IMPORT_MAX_BYTES`.

### 9.5 OIDC
- Authorization Code Flow mit PKCE (S256), `coreos/go-oidc/v3` + `golang.org/x/oauth2`. Redirect-URI: `{BELEGAPP_BASE_URL}/api/v1/auth/oidc/callback`.
- `state`, `nonce`, `code_verifier` in kurzlebigem Cookie `belegapp_oidc` (10 min, HMAC-SHA256 mit `secret_key`, `SameSite=Lax`).
- Callback prüft: `state`, ID-Token-Signatur, `iss`, `aud`, `exp`, `nonce`. Autorisierung: `sub ∈ BELEGAPP_OIDC_ALLOWED_SUBJECTS` **oder** (`email ∈ BELEGAPP_OIDC_ALLOWED_EMAILS` **und** `email_verified = true`). Mindestens eine Liste muss gesetzt sein (sonst Startfehler). Abgelehnt → `302 /login?fehler=nicht_berechtigt` + Log-Eintrag (warn).
- Akteur im Protokoll: `oidc:{sub}`. Discovery beim Start mit Retry (5 × exponentiell); schlägt sie fehl, startet die App trotzdem, `/auth/config` meldet `oidc: false` bis zur erfolgreichen Discovery (Retry alle 60 s).
- Logout: lokal; wenn `BELEGAPP_OIDC_RP_LOGOUT=true` und `end_session_endpoint` vorhanden → Redirect dorthin mit `id_token_hint` und `post_logout_redirect_uri = BASE_URL/login`.

---

## 10. Belegerkennung

### 10.1 Interface
```go
type ReceiptExtractor interface {
    Extract(ctx context.Context, img []byte, mime string) (Erkennungsergebnis, Meta, error)
}
type Meta struct { Modell string; DauerMS int64; Roh []byte }
```
Implementierungen: `openaicompat` (Standard), `fake` (Tests/Dev, liefert Fixture je Bild-Hash), `aus` (liefert `ErrDeaktiviert`).

### 10.2 openaicompat
- `POST {BELEGAPP_LLM_BASE_URL}/chat/completions` über `openai-go/v3` mit `option.WithBaseURL`, `Authorization: Bearer {API_KEY}` (leer erlaubt für lokale Server).
- Standard: OpenAI `gpt-5-mini`. Alternativen nur per Env: Gemini (`BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/`, `MODEL=gemini-…-flash`), Ollama (`http://ollama:11434/v1`, z. B. `qwen3-vl:8b`), vLLM, LM Studio.
- Bild: auf lange Kante `BELEGAPP_LLM_MAX_IMAGE_PX` (1600) skaliert, JPEG q80, als `data:image/jpeg;base64,…` in einer `image_url`-Content-Part; Text-Part mit Prompt (10.4).
- `response_format`: `BELEGAPP_LLM_RESPONSE_FORMAT` = `json_schema` (Standard, `strict: true`, Schema 10.3) | `json_object` (Schema im Prompt) | `auto` (erst `json_schema`, bei HTTP 400 mit Hinweis auf `response_format` einmalig `json_object`).
- Optional `reasoning_effort` (`BELEGAPP_LLM_REASONING_EFFORT`, z. B. `low`), nur gesendet, wenn gesetzt. `temperature` wird nicht gesendet (von Reasoning-Modellen nicht unterstützt).
- Timeout `BELEGAPP_LLM_TIMEOUT` (60 s). Keine Bilddaten und keine API-Keys in Logs.

### 10.3 JSON-Schema (Erkennungsergebnis)
```json
{
  "name": "kassenbeleg",
  "strict": true,
  "schema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["ist_kassenbeleg","datum","uhrzeit","haendler_name","haendler_ort",
                 "gesamtbetrag_cent","waehrung","positionen","bezugsort_vorschlag","konfidenz","hinweise"],
    "properties": {
      "ist_kassenbeleg":   { "type": "boolean" },
      "datum":             { "type": ["string","null"], "description": "YYYY-MM-DD" },
      "uhrzeit":           { "type": ["string","null"], "description": "HH:MM" },
      "haendler_name":     { "type": ["string","null"] },
      "haendler_ort":      { "type": ["string","null"] },
      "gesamtbetrag_cent": { "type": ["integer","null"] },
      "waehrung":          { "type": ["string","null"], "description": "ISO 4217, z. B. EUR" },
      "positionen": {
        "type": "array",
        "items": {
          "type": "object", "additionalProperties": false,
          "required": ["bezeichnung","betrag_cent","mwst_satz_prozent","kategorie"],
          "properties": {
            "bezeichnung":       { "type": "string" },
            "betrag_cent":       { "type": "integer", "description": "negativ für Rabatte/Pfandrückgabe" },
            "mwst_satz_prozent": { "type": ["number","null"] },
            "kategorie": { "type": "string",
              "enum": ["lebensmittel","getraenk_alkoholfrei","alkohol","tabak","pfand","nonfood","rabatt","sonstiges"] }
          }
        }
      },
      "bezugsort_vorschlag": { "type": ["string","null"],
        "enum": ["supermarkt","restaurant","kantine","baeckerei","lieferdienst","sonstiges", null] },
      "konfidenz": { "type": "number", "description": "0..1" },
      "hinweise":  { "type": ["string","null"] }
    }
  }
}
```

### 10.4 Prompt (versioniert, `internal/erkennung/prompt_v1.txt`)
> Du liest deutsche Kassenbelege. Antworte ausschließlich im vorgegebenen JSON-Schema. Beträge in Cent als Ganzzahl. `gesamtbetrag_cent` ist der zu zahlende Endbetrag (SUMME/ZU ZAHLEN). Datum im Format YYYY-MM-DD. Ordne jede Position einer Kategorie zu: Speisen und Lebensmittel → lebensmittel; alkoholfreie Getränke → getraenk_alkoholfrei; Bier, Wein, Spirituosen → alkohol; Tabakwaren → tabak; Pfand und Pfandrückgabe → pfand; Drogerie-, Haushalts- und andere Nicht-Lebensmittel → nonfood; Rabatte → rabatt. Unleserliche Felder setzt du auf null. Ist das Bild kein Kassenbeleg, setze ist_kassenbeleg=false. konfidenz ist deine Gesamtsicherheit von 0 bis 1.

### 10.5 Nachverarbeitung
1. JSON gegen Schema validieren (Go-seitig, auch bei `json_schema`-Modus).
2. Plausibilität: `datum` ≤ heute und ≥ heute − 400 Tage, sonst `datum = null` + Hinweis; `gesamtbetrag_cent` ∈ [1, 100000], sonst `null`; `waehrung ≠ EUR` → Hinweis.
3. Korrekturvorschlag: `max(0, gesamtbetrag − Σ max(0, betrag_cent) über Kategorien {alkohol, tabak, pfand, nonfood})`; nur angeboten, wenn ≠ gesamtbetrag. Rabatte und Pfandrückgaben bleiben im Gesamtbetrag (konservativ).
4. `W_KI_UNSICHER`, wenn `konfidenz < 0,6` oder `|Σ positionen − gesamtbetrag| > 5` oder `ist_kassenbeleg = false`.

### 10.6 Fehlerverhalten
| Fall | Verhalten |
|---|---|
| Timeout, HTTP 429/5xx, Netzwerkfehler | bis 2 Wiederholungen (nach 5 s, 20 s), dann `fehler` |
| HTTP 400 `response_format` nicht unterstützt | bei `auto` einmal mit `json_object`; sonst `fehler` |
| HTTP 401/403 | `fehler` „API-Key ungültig“, keine Wiederholung; `/system/info` meldet Problem |
| Ungültiges JSON / Schemaverstoß | eine Wiederholung, dann `fehler` |
| Erkennung deaktiviert / kein Key bei OpenAI-Basis-URL | Status `keine` |
In allen Fällen bleibt manuelle Erfassung möglich; Fehler werden am Belegbild gespeichert und in der UI angezeigt („Erneut erkennen“).

---

## 11. Storage, Bildverarbeitung, Datenexport

### 11.1 BlobStore-Interface
```go
type BlobStore interface {
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
    Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)   // ErrNotFound
    Delete(ctx context.Context, key string) error               // idempotent
    List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
    Ping(ctx context.Context) error
}
type ObjectInfo struct { Key string; Size int64; ContentType string; ModTime time.Time }
```
- Keys: nur `[a-z0-9/._-]`, kein `..`, kein führendes `/`. Bilder inhaltsadressiert (`bilder/{sha[0:2]}/{sha256}.jpg`, Thumbnail `thumbs/{sha[0:2]}/{sha256}.jpg`) → unveränderlich, Dedup. Gleicher Inhalt teilt sich Blob und Thumbnail. Beim Start und nach Datenimport werden Alt-Schlüssel `bilder/{id}.jpg` / `bilder/{id}.thumb.jpg` umkopiert (idempotent, Referenzzähler vor dem Löschen). Ein Blob, dessen Hash nicht zur Zeile passt, bleibt auf dem alten Schlüssel.
- **fs**: `os.Root` auf `BELEGAPP_STORAGE_FS_DIR`; Schreiben in Temp-Datei, `fsync`, `rename`; Dateien 0640, Verzeichnisse 0750.
- **s3**: `minio-go/v7`; Endpoint, Region, Bucket, Prefix, Path-Style, TLS konfigurierbar; `Ping` = `BucketExists` (Ergebnis 30 s gecacht). Optional SSE-S3 (`BELEGAPP_S3_SSE=true`).
- Contract-Tests laufen gegen beide Implementierungen (fs mit TempDir, s3 mit `gofakes3` in-process; optional MinIO/Garage per Testcontainers).
- Backend-Wechsel: Datenexport erstellen → Backend umkonfigurieren → Datenimport (lädt alle Blobs in das neue Backend).

### 11.2 Bildverarbeitung (Server)
Upload → MIME-Sniffing (`gabriel-vasile/mimetype`): JPEG/PNG/WebP erlaubt → Dekodieren → EXIF-Orientierung anwenden (falls vorhanden) → lange Kante ≤ 2400 px → JPEG q85 ohne Metadaten (entfernt GPS/EXIF) → `sha256` → Thumbnail 400 px JPEG q75. Dekodier-Limit 50 MP (Schutz vor Decompression Bombs).

### 11.3 Datenexport (ZIP)
```
manifest.json            { "format": "vc-belegapp-datenexport", "format_version": 1,
                           "app_version", "schema_version", "erstellt_am", "instanz_id",
                           "zeitraum": {"von","bis"}, "anzahl_belege",
                           "dateien": [ {"pfad","sha256","bytes"} ] }
db/belegapp.sqlite       Snapshot per VACUUM INTO (inkl. Änderungsprotokoll, ohne Sitzungen/Jobs)
blobs/<key>              alle referenzierten Blobs (Bilder, Thumbnails, Monatsexporte)
csv/belege.csv           alle Belege, Format 12.5
```
Erstellung als Job in Temp-Datei, dann Ablage unter `datenexporte/<zeitstempel>.zip` im BlobStore (Download 24 h, danach gelöscht; CLI schreibt direkt in Datei). `format_version` bleibt 1: die Schlüssel sind Daten in `blobs/<key>` und in `belegbilder`, kein zweites Archivlayout. Import liest Alt- und Neu-Schlüssel und migriert danach auf die inhaltsadressierte Form.

### 11.4 Datenimport
1. ZIP-Struktur, Manifest, alle Prüfsummen validieren; `format_version` bekannt; `schema_version ≤` aktuell (sonst `422 E_IMPORT_ZU_NEU`).
2. Sicherheits-Datenexport des aktuellen Stands erzeugen (Blob `datenexporte/vor-import-<ts>.zip`).
3. Job-Worker anhalten, Schreibsperre setzen.
4. Blobs ins aktuelle Backend schreiben (bestehende Keys mit gleichem Hash überspringen).
5. SQLite-Datei ersetzen (Temp-Datei + `rename`), goose-Migrationen ausführen, Hash-Kette prüfen (Fehler → Abbruch und Rollback auf Sicherheitsstand).
6. Alle Sitzungen löschen, Protokolleintrag `datenimport` (Akteur, Manifest-Hash), Worker starten.

---

## 12. Monatsexport (PDF, CSV, ZIP)

### 12.1 Erzeugung
- `PDFRenderer`-Interface (`Render(ctx, MonatsDaten) ([]byte, error)`), Implementierung `typstcli` (ADR 0002).
- Temp-Verzeichnis unter `/tmp/belegapp-pdf-<ulid>`: `main.typ`, `daten.json`, `bilder/<sha>.jpg` (lange Kante 1600 px, JPEG q80), `fonts/` (Inter Regular/Medium/SemiBold, OFL, per `go:embed`).
- Aufruf: `typst compile --root <tmp> --font-path <tmp>/fonts --ignore-system-fonts --pdf-standard a-2b --input entwurf=<true|false> main.typ out.pdf` mit `SOURCE_DATE_EPOCH` = Exportzeitpunkt, `XDG_CACHE_HOME=/tmp`; Timeout `BELEGAPP_PDF_TIMEOUT`; Stderr bei Fehler ins Log und als `500 E_PDF_FEHLER`.
- Template liest `json("daten.json")`; keine Typst-Packages (kein Netzwerk). Typst-Version im Image fest (0.15.1), beim Start `typst --version` geprüft (Readiness).
- PDF-Metadaten: Titel „Nachweis Essenszuschuss {Monat Jahr}“, Autor = arbeitnehmer_name, Keywords = Dokument-ID.

### 12.2 Seitenaufbau
**Teil 1 – Übersicht (A4 quer)**
1. Kopf: Titel „Nachweis arbeitstäglicher Zuschüsse zu Mahlzeiten – Oktober 2026“; rechts Dokument-ID (`{YYYY-MM}-v{n}`), Erstellungszeitpunkt (Europe/Berlin), bei Vorschau Wasserzeichen „ENTWURF“ diagonal auf allen Seiten.
2. Stammdaten: Arbeitnehmer, Personalnummer, Arbeitgeber.
3. Angewendete Jahresregel: Zuschuss/Tag, bezuschusste Mahlzeitarten, SBW je Mahlzeitart, Höchstzuschuss, Pauschalierung ja/nein, Pauschsteuersatz, Soli, Kirchensteuersatz + Bundesland, Gehaltsumwandlung ja/nein, Eigenanteil-Variante, Monatslimit; Hinweis bei SBW-Entwurf.
4. Tabelle (eine Zeile je Beleg): Nr. · Datum · Wt · Mahlzeit · Bezugsort · Arbeitsort · Händler, Ort · Belegbetrag · Anerkannt · Erstattung · Eigenanteil · Geldwerter Vorteil · Steuerfrei · Regulär · Hinweise (Warnungs-Kurzcodes + Fußnoten mit Korrekturgrund) · Anhang-Seite. Beträge rechtsbündig, deutsches Format „7,67 €“.
5. Summenzeile + Summenblock: Anzahl, ΣB, ΣA, ΣE, ΣU, ΣG, ΣF, ΣR; bei Pauschalierung: Pauschalsteuer, Soli, Kirchensteuer, Pauschal gesamt, Arbeitgeberkosten; ohne Pauschalierung: „ΣG regulär lohnsteuer- und SV-pflichtig“.
6. Prüfpunkte mit ✓/⚠/✗ und Kurztext.
7. Nur ab Version 2: „Änderungen gegenüber Version {n−1}“ – Tabelle aus Änderungsprotokoll (Zeitpunkt, Beleg/Datum, Feld, alt → neu, Änderungsgrund).
8. Arbeitnehmererklärung (12.4) + digitale Bestätigung („bestätigt am {Zeitpunkt} durch {akteur}“) + Zeile „Ort, Datum, Unterschrift Arbeitnehmer“ + Feld „Geprüft (Arbeitgeber): Datum/Kürzel“.
9. Hinweis: „Vorberechnung nach R 8.1 Abs. 7 Nr. 4 LStR / § 40 Abs. 2 Satz 1 Nr. 1 EStG; maßgeblich ist die Lohnabrechnung des Arbeitgebers.“

**Teil 2 – Anhang (A4 hoch, eine Seite je Beleg, Folgeseiten für Seite 2–3 eines Belegs)**
- Kopfzeile: Nr., Datum, Wochentag, Händler, Ort, Mahlzeit, Bezugsort, Arbeitsort.
- Belegbild(er) maximal groß, Seitenverhältnis erhalten.
- Faktenblock: Belegbetrag, Korrigierter Betrag + Grund, Erstattung, Quelle (KI/KI korrigiert/manuell), erkannter Händler/Datum, SHA-256 je Belegbild, Upload-Zeitpunkt, letzte Änderung.
- Fußzeile aller Seiten: „{Dokument-ID} · Seite x von y · vc-belegapp {version}“.

### 12.3 Darstellung
Schrift Inter, 9 pt Tabelle / 10 pt Text; neutrale Graustufen + eine Akzentfarbe; Tabellenkopf auf Folgeseiten wiederholt; Zebra-Streifen; keine Transparenzeffekte außer Wasserzeichen.

### 12.4 Arbeitnehmererklärung (Text, fest)
> Ich versichere, dass jeder aufgeführte Beleg eine Mahlzeit betrifft, die ich an dem angegebenen Tag als Arbeitstag (kein Urlaub, keine Krankheit, keine Auswärtstätigkeit) selbst erworben habe und die zum Verzehr an diesem Tag bestimmt war. Nicht erstattungsfähige Artikel (z. B. Alkohol, Tabak, Pfand, Non-Food, Vorratskäufe) habe ich herausgerechnet. Jeder Beleg wird nur einmal eingereicht.

### 12.5 CSV
UTF-8 mit BOM, Trennzeichen `;`, Dezimalkomma, Zeilenende CRLF, Kopfzeile:
`nr;datum;wochentag;mahlzeit;bezugsort;arbeitsort;haendler;ort;belegbetrag;anerkannt;korrektur_grund;erstattung;eigenanteil;geldwerter_vorteil;steuerfrei;regulaer;warnungen;bild_sha256`
Letzte Zeile `summe;…` mit Summen. Für den Datenexport zusätzlich Spalte `beleg_id` und alle Jahre.

### 12.6 ZIP (optional)
`nachweis-2026-10-v1.pdf`, `nachweis-2026-10-v1.csv`, `bilder/2026-10-05_REWE_s1.jpg` (gespeicherte Belegbilder, Dateiname `datum_händler-slug_s{seite}`), `manifest.json` mit SHA-256 aller Dateien.

---

## 13. UI

### 13.1 Grundsätze
- Mobile-first, eine Spalte < 768 px; ab `md` Sidebar-Layout. Untere Navigation (mobil): **Heute**, **Monat**, **Einstellungen**; zentraler Kamera-FAB auf „Heute“ und „Monat“.
- Dark Mode: `Hell | Dunkel | System` (Standard System), gespeichert in `localStorage`, Klasse `dark` auf `<html>`; Tailwind-v4-Tokens (shadcn-Variablen) für beide Themes.
- Komponenten: solid-ui (Kobalte/corvu) im Repo unter `web/src/components/ui`; Icons lucide-solid; Toasts solid-sonner; Daten TanStack Query; Formulare TanStack Form.
- Zahlen/Datum: `Intl.NumberFormat("de-DE", {style:"currency", currency:"EUR"})`, `Intl.DateTimeFormat("de-DE")`; Betragseingabe akzeptiert „7,67“ und „7.67“, `inputmode="decimal"`.
- Leere Zustände, Ladeskelette, Fehlermeldungen in Klartext (Problem-JSON `felder[].text`).

### 13.2 Screens
| Screen | Inhalt |
|---|---|
| Login | App-Name, OIDC-Button (primär), Passwortformular (falls aktiv), Fehlerhinweis aus `?fehler=` |
| Heute | Status heute (Beleg vorhanden: Karte mit Thumbnail/Betrag/Erstattung; sonst Kamera- und Galerie-Button groß), Monatsfortschritt „n / Limit“, ΣE des Monats, Hinweis bei fehlender Jahresregel |
| Prüfen/Bearbeiten | Bild (Pinch-Zoom, Seitenwechsel, „+ Seite“), Erkennungsstatus (Skeleton/Fehler/„Erneut erkennen“), Felder: Datum, Mahlzeitart (Segmented, nur erlaubte), Bezugsort (Select), Arbeitsort (Toggle), Händler, Ort, Belegbetrag, Korrigierter Betrag + Grund (aufklappbar), Notiz; Positionsliste mit Kategorie-Badges; Korrekturvorschlag-Hinweis; Berechnungskarte (E, U, G, F, R); Warnungsliste; Speichern/Löschen; Änderungsgrund-Dialog im gesperrten Monat |
| Monat | Monatsauswahl, Status-Badge, Kalender (Tageszellen mit Betrag/Wochenende/Feiertag/Warnung), Summenkarte, Belegliste, Buttons Vorschau/Exportieren, Banner bei `geaendert` |
| Export-Dialog | Prüfpunkte, Warnungen, Optionen CSV/ZIP, Arbeitnehmererklärung (Checkbox, Volltext), „Warnungen geprüft“, Vorschau, Final exportieren; bei `E_PRUEFPUNKT_FEHLGESCHLAGEN` die Export-Prüfpunkte neu laden; Liste der Exportversionen mit Downloads und Aufbewahrungsuhr |
| Einstellungen | Profil; Monatsexport-Standards `export_csv_standard` / `export_zip_standard`; Jahresregeln (Liste; Editor mit Abschnitten Zuschuss, Sachbezugswerte (Button „amtliche Werte übernehmen“), Steuer (Pauschalierung, Gehaltsumwandlung, Bundesland → Kirchensteuer-Vorschlag, Satz), Berechnung (Eigenanteil-Variante mit Erklärung), Monatslimit + Modus, Eigene Feiertage); Belegerkennung (an/aus, Modell, Basis-URL read-only, „Verbindung testen“); Speicher (Backend read-only); Sitzungen; Datenexport/-import; Änderungsprotokoll (Liste, „Kette prüfen“); Darstellung; Über |

### 13.3 PWA
- `vite-plugin-pwa`: `registerType: "prompt"` (Toast „Neue Version – neu laden“), Manifest `name: "Belegapp"`, `short_name: "Belege"`, `lang: "de"`, `display: "standalone"`, `start_url: "/"`, Theme-Farben hell/dunkel, Icons 192/512 + maskable (generiert mit `@vite-pwa/assets-generator`), iOS-`apple-touch-icon`.
- Workbox: Precache App-Shell; `/api/*` `NetworkOnly`; Navigations-Fallback `index.html`.
- Kein Web Share Target in v1 (siehe Offene Punkte).

---

## 14. Konfiguration (Umgebungsvariablen)
Alle Variablen mit Präfix `BELEGAPP_`; für Geheimnisse zusätzlich `<NAME>_FILE` (Pfad, Inhalt getrimmt). Ungültige Konfiguration → Start mit Exit-Code 2 und klarer Meldung.

| Variable | Default | Beschreibung |
|---|---|---|
| `BELEGAPP_LISTEN_ADDR` | `:8080` | HTTP-Listener |
| `BELEGAPP_BASE_URL` | – (Pflicht bei OIDC) | Externe URL, z. B. `https://belege.example.de`; Basis für Redirect und Trusted Origin |
| `BELEGAPP_DATA_DIR` | `/data` | Datenverzeichnis |
| `BELEGAPP_DB_PATH` | `${DATA_DIR}/belegapp.db` | SQLite-Datei |
| `BELEGAPP_TZ` | `Europe/Berlin` | Fachliche Zeitzone (tzdata per `time/tzdata` eingebettet) |
| `BELEGAPP_LOG_LEVEL` | `info` | `debug\|info\|warn\|error` |
| `BELEGAPP_LOG_FORMAT` | `json` | `json\|text` |
| `BELEGAPP_TRUSTED_PROXIES` | `` | CIDR-Liste für `X-Forwarded-For`/`-Proto` |
| `BELEGAPP_SECRET_KEY(_FILE)` | `` (auto, in DB) | 32 Byte base64; HMAC für OIDC-Cookie |
| `BELEGAPP_COOKIE_SECURE` | `true` | nur lokal `false` |
| `BELEGAPP_SESSION_IDLE_TIMEOUT` | `720h` | |
| `BELEGAPP_SESSION_ABSOLUTE_TIMEOUT` | `2160h` | |
| `BELEGAPP_AUTH_PASSWORD_HASH(_FILE)` | `` | argon2id-PHC; leer = Passwort-Login aus |
| `BELEGAPP_OIDC_ISSUER_URL` | `` | leer = OIDC aus |
| `BELEGAPP_OIDC_CLIENT_ID` | `` | |
| `BELEGAPP_OIDC_CLIENT_SECRET(_FILE)` | `` | leer = Public Client (nur PKCE) |
| `BELEGAPP_OIDC_SCOPES` | `openid profile email` | |
| `BELEGAPP_OIDC_ALLOWED_SUBJECTS` | `` | Komma-Liste |
| `BELEGAPP_OIDC_ALLOWED_EMAILS` | `` | Komma-Liste (nur mit `email_verified`) |
| `BELEGAPP_OIDC_BUTTON_LABEL` | `Mit SSO anmelden` | |
| `BELEGAPP_OIDC_RP_LOGOUT` | `false` | RP-initiated Logout |
| `BELEGAPP_STORAGE_BACKEND` | `fs` | `fs\|s3` |
| `BELEGAPP_STORAGE_FS_DIR` | `${DATA_DIR}/blobs` | |
| `BELEGAPP_S3_ENDPOINT` | `` | z. B. `s3.eu-central-1.amazonaws.com`, `minio:9000` |
| `BELEGAPP_S3_REGION` | `us-east-1` | |
| `BELEGAPP_S3_BUCKET` | `` | Pflicht bei s3 |
| `BELEGAPP_S3_PREFIX` | `belegapp/` | |
| `BELEGAPP_S3_ACCESS_KEY_ID(_FILE)` | `` | |
| `BELEGAPP_S3_SECRET_ACCESS_KEY(_FILE)` | `` | |
| `BELEGAPP_S3_USE_TLS` | `true` | |
| `BELEGAPP_S3_FORCE_PATH_STYLE` | `true` | |
| `BELEGAPP_S3_SSE` | `false` | SSE-S3 |
| `BELEGAPP_LLM_ENABLED` | `true` | globaler Schalter (zusätzlich `einstellungen.erkennung_aktiv`) |
| `BELEGAPP_LLM_BASE_URL` | `https://api.openai.com/v1` | OpenAI-kompatibel |
| `BELEGAPP_LLM_API_KEY(_FILE)` | `` | bei OpenAI-Basis-URL Pflicht, sonst optional |
| `BELEGAPP_LLM_MODEL` | `gpt-5-mini` | |
| `BELEGAPP_LLM_RESPONSE_FORMAT` | `auto` | `json_schema\|json_object\|auto` |
| `BELEGAPP_LLM_REASONING_EFFORT` | `` | z. B. `low`; leer = nicht senden |
| `BELEGAPP_LLM_TIMEOUT` | `60s` | |
| `BELEGAPP_LLM_MAX_IMAGE_PX` | `1600` | |
| `BELEGAPP_JOB_WORKERS` | `2` | |
| `BELEGAPP_UPLOAD_MAX_BYTES` | `15728640` | 15 MiB |
| `BELEGAPP_IMPORT_MAX_BYTES` | `4294967296` | 4 GiB (Datenimport) |
| `BELEGAPP_UNASSIGNED_IMAGE_TTL` | `24h` | |
| `BELEGAPP_EXPORT_RETENTION_YEARS` | `10` | Kalenderjahre der Aufbewahrungsuhr; keine automatische Löschung |
| `BELEGAPP_TYPST_BIN` | `typst` | Pfad zum Typst-Binary |
| `BELEGAPP_PDF_TIMEOUT` | `120s` | |
| `BELEGAPP_METRICS_ADDR` | `` | z. B. `:9090`; leer = aus |

CLI: `belegapp serve` (Default) · `hash-password` · `healthcheck [--url]` · `migrate` · `backup --out <zip>` · `restore <zip> --yes` · `verify-audit` · `version`.

---

## 15. Betrieb

### 15.1 Image
- Multi-Stage-`Dockerfile` (lokal) und `Dockerfile.goreleaser` (Release): Stage `typst` lädt `typst-{x86_64|aarch64}-unknown-linux-musl.tar.xz` (Version + SHA-256 als `ARG`, Renovate-Custom-Manager), Final-Stage `gcr.io/distroless/static-debian13:nonroot` (Digest gepinnt): `/belegapp`, `/usr/local/bin/typst`, `USER 65532:65532`, `EXPOSE 8080`, `ENTRYPOINT ["/belegapp"]`, `CMD ["serve"]`. OCI-Labels (`source`, `version`, `revision`, `licenses`).
- Plattformen: `linux/amd64`, `linux/arm64`.
- Healthcheck über `belegapp healthcheck` (kein curl im Image).

### 15.2 Endpunkte
- `GET /healthz` → `200 ok` (Prozess lebt).
- `GET /readyz` → `200` wenn DB-Ping, Migrationen aktuell, `BlobStore.Ping`, `typst --version` (beim Start gecacht) ok; sonst `503` mit JSON der fehlgeschlagenen Checks. OIDC und LLM beeinflussen Readiness nicht.
- Metriken (optional, `BELEGAPP_METRICS_ADDR`, eigener Port, kein Auth): `belegapp_http_requests_total{route,method,code}`, `belegapp_http_request_duration_seconds`, `belegapp_erkennung_total{ergebnis}`, `belegapp_erkennung_dauer_seconds`, `belegapp_pdf_dauer_seconds`, `belegapp_belege_total`, `belegapp_jobs_wartend` + Go-Runtime-Metriken.

### 15.3 Docker Compose (Beispiel, `deploy/docker-compose.yml`)
Pflichtwerte kommen aus `deploy/.env` (Vorlage `deploy/.env.example`). `${VAR:?}` bricht `docker compose` ab, solange der Wert leer ist. Der Prozess selbst startet mit Passwort-Hash oder OIDC; das Beispiel setzt beides und den LLM-Key.
```yaml
services:
  belegapp:
    image: ghcr.io/blackdark/vc-belegapp:1
    restart: unless-stopped
    user: "65532:65532"
    read_only: true
    cap_drop: [ALL]
    # Seccomp bleibt das Engine-Default (entspricht Kubernetes RuntimeDefault).
    security_opt:
      - no-new-privileges:true
    tmpfs: ["/tmp:size=512m,mode=1777"]
    mem_limit: 512m
    mem_reservation: 64m
    cpus: 1
    ports: ["127.0.0.1:8080:8080"]
    volumes: ["./data:/data"]          # vorher: chown -R 65532:65532 ./data
    environment:
      BELEGAPP_BASE_URL: ${BELEGAPP_BASE_URL:?set BELEGAPP_BASE_URL}
      BELEGAPP_TRUSTED_PROXIES: ${BELEGAPP_TRUSTED_PROXIES:-172.16.0.0/12}
      BELEGAPP_OIDC_ISSUER_URL: ${BELEGAPP_OIDC_ISSUER_URL:?set BELEGAPP_OIDC_ISSUER_URL}
      BELEGAPP_OIDC_CLIENT_ID: ${BELEGAPP_OIDC_CLIENT_ID:?set BELEGAPP_OIDC_CLIENT_ID}
      BELEGAPP_OIDC_CLIENT_SECRET_FILE: /run/secrets/oidc_secret
      BELEGAPP_OIDC_ALLOWED_EMAILS: ${BELEGAPP_OIDC_ALLOWED_EMAILS:?set BELEGAPP_OIDC_ALLOWED_EMAILS}
      BELEGAPP_AUTH_PASSWORD_HASH_FILE: /run/secrets/pw_hash
      BELEGAPP_LLM_API_KEY_FILE: /run/secrets/openai_key
    secrets: [oidc_secret, pw_hash, openai_key]
    healthcheck:
      test: ["CMD", "/belegapp", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
secrets:
  oidc_secret:
    file: ${OIDC_SECRET_FILE:?path to the OIDC client secret}
  pw_hash:
    file: ${PASSWORD_HASH_FILE:?path to the argon2id password hash}
  openai_key:
    file: ${LLM_API_KEY_FILE:?path to the LLM API key}
```

### 15.4 Kubernetes (`deploy/k8s/`, Kustomize)
- `Deployment`: `replicas: 1`, `strategy: Recreate` (SQLite); Pod-`securityContext`: `runAsNonRoot: true`, `runAsUser/Group: 65532`, `fsGroup: 65532`, `seccompProfile: RuntimeDefault`; Container: `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`.
- Volumes: PVC `ReadWriteOnce` für `/data` (kein NFS/CIFS), `emptyDir` (`sizeLimit: 1Gi`) für `/tmp`.
- Probes: liveness `/healthz` (period 20 s), readiness `/readyz` (period 10 s), startup `/readyz` (failureThreshold 30).
- Ressourcen: requests `50m`/`64Mi`, limits `1` CPU / `512Mi` (Deployment und Backup-CronJob).
- `Secret` für Hash/OIDC/LLM/S3; `ConfigMap` für übrige Env; `Service` + `Ingress` (TLS via cert-manager, Body-Limit ≥ Import-Größe, z. B. `nginx.ingress.kubernetes.io/proxy-body-size: 4g`).
- Optional `NetworkPolicy`: Egress nur DNS, IdP, LLM, S3.
- Optional Litestream-Sidecar für SQLite-Replikation (dokumentiert, nicht Teil von v1).

### 15.5 Backups
Empfohlen: regelmäßiger `belegapp backup` per CronJob/Host-Cron **oder** Volume-Snapshots; Datenexport ist das einzige garantiert konsistente Format inkl. Blobs.

---

## 16. CI/CD (GitHub Actions)

### 16.1 Workflows
| Datei | Trigger | Jobs |
|---|---|---|
| `ci.yml` | PR, Push `main`, `workflow_dispatch` | **web**: pnpm install (frozen), `biome ci`, `tsc --noEmit`, `vitest run`, `vite build`, Artefakt `web-dist` · **linux-amd64 / linux-arm64 / darwin** (needs web, je ein Runner): `CGO_ENABLED=0` Cross-Compile · **go** (needs web): `go mod verify`, `sqlc diff` + `sqlc vet`, `golangci-lint run`, `go test -race -coverprofile ./...`, `govulncheck ./...` · **pdf**: Typst, Golden-Test M1, veraPDF PDF/A-2b · **e2e** (needs linux-amd64): Playwright gegen das fertige Binary, Fake-LLM, Mock-OIDC · **docker** (needs linux-amd64 und linux-arm64): `Dockerfile.goreleaser` kopiert die Binaries nach distroless, Buildx ohne QEMU und ohne Push, GHA-Cache, Trivy, Smoke (read-only, UID 65532) |
| `release-please.yml` | Push `main` | release-please (Conventional Commits) erstellt Release-PR, Changelog, Tag `vX.Y.Z`; `workflow_dispatch` von `ci.yml` auf dem Release-Branch und von `release.yml` auf dem Tag |
| `release.yml` | Tag `v*` | `ci.yml`, Smoke des Copy-Images, dann GoReleaser v2 (`release --clean`) |
| `codeql.yml` | PR, wöchentlich | CodeQL `go`, `javascript-typescript` |

Actions per Commit-SHA gepinnt; `permissions: contents: read` als Default, Release-Job: `contents: write`, `packages: write`, `id-token: write`, `attestations: write`. Go-Version aus `go.mod` (`toolchain`), Node über `.nvmrc`, pnpm über `packageManager`.

### 16.2 GoReleaser (`.goreleaser.yaml`, v2)
- `before.hooks`: `pnpm -C web install --frozen-lockfile`, `pnpm -C web build`, `go generate ./...`.
- `builds`: `CGO_ENABLED=0`, `goos: [linux, darwin]`, `goarch: [amd64, arm64]`, `-trimpath`, `ldflags: -s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}`.
- `archives`: tar.gz mit `LICENSE`, `README.md`, `deploy/`; `checksum`; `sboms` (syft); `signs` (cosign keyless) für Checksums.
- `dockers_v2`: `images: [ghcr.io/blackdark/vc-belegapp]`, `tags: ["{{.Version}}", "{{.Major}}.{{.Minor}}", "{{.Major}}", "latest"]`, `platforms: [linux/amd64, linux/arm64]`, `dockerfile: Dockerfile.goreleaser`; `docker_signs` (cosign keyless); Provenance-Attestation via `actions/attest-build-provenance`.
- `changelog.disable: true` (release-please führt Changelog).

### 16.3 Renovate (`renovate.json`)
`extends: ["config:best-practices", ":semanticCommits"]`, Zeitplan wöchentlich, `postUpdateOptions: ["gomodTidy"]`, Gruppen: `go`, `web (prod)`, `web (dev)`, `github-actions`, `docker`; Automerge für Patch/Minor von Dev-Dependencies und Actions nach grüner CI; Custom-Regex-Manager für `TYPST_VERSION`/SHA in Dockerfiles; Solid 2.x und Kobalte-Major nicht automatisch (`matchUpdateTypes: major` → manuelles Review).

### 16.4 Branch-Schutz (Vorschlag, Ruleset auf `main`)
PR erforderlich (0 Reviews, Single-Maintainer), Status-Checks `web`, `go`, `pdf`, `docker` erforderlich, lineare Historie, kein Force-Push/Löschen, Konversationen aufgelöst; Tag-Ruleset `v*` nur durch release-please/Maintainer; Secret Scanning + Push Protection, Dependabot Security Alerts an.

---

## 17. Tests
| Ebene | Inhalt |
|---|---|
| Unit `calc` | alle Testvektoren 6.5 (aus `testdata/vektoren.json`), Property-Tests der Invarianten (`testing/quick` o. ä., ≥ 10 000 Fälle), Rundung |
| Unit sonstige | Jahresregel-Validierung, Vorschlagslogik 5.3, Feiertage 16 Länder 2026 + eigene Feiertage, Warnungen/Prüfpunkte, Korrekturvorschlag, Erkennungs-Nachverarbeitung, argon2id, Session-Timeouts, OIDC-Allowlist, Key-Validierung Storage, Hash-Kette |
| Contract | BlobStore (fs + gofakes3), ReceiptExtractor (Fake-HTTP-Server: Erfolg, 429→Erfolg, 400 `response_format`→Fallback, ungültiges JSON, 401, Timeout) |
| Integration API | `httptest` mit Temp-SQLite, fs-Storage, Fake-LLM, Mock-OIDC (`oauth2-proxy/mockoidc`): Login beide Methoden, CSRF-Ablehnung Cross-Origin, Flow A komplett, E_DATUM_BELEGT, Sperre + Änderungsgrund, Exportversion 2 mit Änderungsabschnitt, Datenexport → Datenimport in leere Instanz (Roundtrip identische Summen + Hash-Kette gültig), Backend-Wechsel fs → s3 per Import |
| PDF | Golden-Test M1: PDF erzeugen, Text enthält Summen/Erklärung, Übersicht ≤ 2 Seiten + 5 Anhangseiten, optional veraPDF |
| Frontend | Vitest: Betragsparser, Formatierung, Formular-Logik; Komponenten-Tests für Prüfen-Screen |
| E2E (Playwright, in CI) | Eigenes Server-Fixture je Test (parallele Worker): Login Passwort und OIDC (Mock), Jahresregel, Upload, Fake-Erkennung, korrigierter Betrag, Monatsansicht, Prüfpunkte, Entwurf und finaler Export inkl. PDF, Sperre mit Änderungsgrund, Datenexport/Import, Logout; Screenshot-Spec Desktop und iPhone 15 |
| Manuell/Eval | `make eval-erkennung`: 20 anonymisierte echte Belege gegen konfiguriertes Modell, Report Trefferquote Datum/Betrag/Händler (Ziel ≥ 90 % Betrag) |

Coverage-Ziel: `internal/calc` 100 %, Backend gesamt ≥ 70 %.

---

## 18. Meilensteine

| M | Umfang | Abnahmekriterien |
|---|---|---|
| **M0 Fundament** | Repo, Go-Modul, Vite/Solid-Skeleton eingebettet, Config, Logging, `/healthz`/`/readyz`, goose + sqlc, CI (`web`, `go`, `docker`), Renovate, Dockerfile, Ruleset | CI grün; `docker run` startet als UID 65532 mit read-only FS und liefert SPA; multi-arch Build läuft |
| **M1 Erfassen (MVP)** | Auth Passwort **und** OIDC, Sitzungen, CSRF; BlobStore fs **und** s3; Einstellungen; Jahresregeln inkl. Vorschläge; Belegbilder-Upload + Normalisierung; Belege CRUD (1 pro Tag, Soft-Delete, Version); Berechnung + Warnungen + Feiertage; Monatsansicht; Änderungsprotokoll (Hash-Kette, alle Änderungen); UI Heute/Prüfen/Monat/Einstellungen mobil + Dark Mode | Alle Vektoren 6.5 grün; Login per OIDC gegen Eduards IdP und per Passwort; Beleg auf iPhone und Android fotografiert und gespeichert; zweiter Beleg am selben Tag → E_DATUM_BELEGT; Wochenende/Feiertag-Warnung sichtbar; Storage-Contract-Tests fs+s3 grün |
| **M2 Belegerkennung** | Job-Queue, `openaicompat` + `fake`, Schema, Nachverarbeitung, Korrekturvorschlag, Retry/Fallback, UI-Polling, „Verbindung testen“ | Mit `gpt-5-mini` werden Datum/Händler/Betrag vorausgefüllt (NF-1); Fehlerfälle 10.6 per Contract-Test; manuelle Erfassung bei LLM-Ausfall unverändert möglich; Wechsel auf Ollama nur per Env |
| **M3 Monatsexport & Sperre** | Typst-Template, PDFRenderer, Vorschau (ENTWURF), finaler Export, Prüfpunkte, Arbeitnehmererklärung, CSV/ZIP, Monatsstatus, Änderungsgrund, Exportversionen mit Änderungsabschnitt | Golden-PDF M1 entspricht 12.2; PDF/A-2b-Validierung bestanden; nach Export ist Bearbeiten ohne Änderungsgrund unmöglich; Version 2 listet Änderungen; NF-2 erfüllt |
| **M4 Datenexport/-import & Release v1.0** | Datenexport/-import (UI + CLI), Backend-Wechsel per Import, PWA (Manifest, Icons, Update-Prompt), GoReleaser + GHCR + Signatur + SBOM, release-please, Compose- und K8s-Beispiele, README/Betriebsdoku | Roundtrip-Test grün; Restore auf frischem Host ergibt identische Summen und gültige Hash-Kette; `v1.0.0`-Release mit Binaries und multi-arch Image (signiert); App auf iOS/Android installierbar; Kustomize-Beispiel läuft in Kind |
| M5 (optional) | Web Share Target, Litestream-Doku, geplanter automatischer Datenexport, PDF-Belege als Eingabe, Regelperioden innerhalb eines Jahres | nach Bedarf |

---

## 19. Offene Punkte

### 19.1 Beim Arbeitgeber zu klären (Konfiguration der Jahresregel)
1. Tageszuschuss, bezuschusste Mahlzeitarten, arbeitsvertragliche Grundlage.
2. Zusätzlich oder Gehaltsumwandlung; pauschaliert der Arbeitgeber (25 %)?
3. Kirchensteuer: vereinfachtes Verfahren (Satz nach Bundesland) oder Nachweisverfahren (0 % bzw. 8/9 %); Bundesland der Betriebsstätte.
4. Eigenanteil-Variante `standard` (Eigenanteil angerechnet) oder `vorsichtig`.
5. Monatslimit 15 oder alle Arbeitstage (dann Abwesenheitserfassung beim Arbeitgeber).
6. Reicht das PDF mit Fotos oder werden Papieroriginale verlangt; Frist; Unterschrift nötig?
7. Homeoffice-Tage akzeptiert? Kantine (fremd betrieben) akzeptiert?
8. Übergabeweg/Format (PDF per Mail, HR-Portal, CSV-Bedarf).

### 19.2 Fachlich/technisch
- ⚠️ Pauschale Kirchensteuersätze 2026 je Bundesland gegen Ländererlasse prüfen (Vorschlagstabelle 5.4).
- ⚠️ 2027-Sachbezugswerte nach Bundesrat-Beschluss aktualisieren (Release vor Januar 2027).
- ⚠️ Rundung der pauschalen Lohnsteuer (kaufmännisch angenommen) mit Lohnabrechnung abgleichen.
- Teilbelege (mehrere Kassenzettel für eine Mahlzeit, steuerlich zulässig) – in v1 bewusst nicht unterstützt.
- PDF-/E-Mail-Belege (Lieferdienste) als Eingabe.
- Unterjährige Regeländerungen (z. B. Zuschusserhöhung zum 1.7.) – v1 nur pro Kalenderjahr.
- Modellnamen/Preise (`gpt-5-mini`, Gemini Flash) vor M2 verifizieren; Eval-Set aufbauen.
- solid-ui-Tokens auf Tailwind v4 portieren; Solid-2.0-Migration nach stabilem Release und Kobalte-Unterstützung.
- typst-go-wasm als echtes Single-Binary beobachten (Bild-Einbettung klären).
- Domain, Reverse Proxy, IdP-Details (Issuer, Client), S3-Anbieter für Eduards Umgebung.

### 19.3 Eigene Annahmen dieser Spezifikation (von Eduard zu bestätigen)
1. Ein Beleg pro Belegtag, aber bis zu **3 Belegbilder** (lange Kassenzettel).
2. Feiertagskalender = gesetzliche Feiertage des in der Jahresregel gewählten **Bundeslands** (gleiches Feld wie Kirchensteuer-Vorschlag) + „Eigene Feiertage“; regionale Feiertage nur manuell.
3. Gesperrter Monat: Änderungen bleiben möglich, erfordern aber einen **Änderungsgrund**, setzen den Monatsstatus auf `geaendert` und verlangen einen neuen Export (Version n+1 mit Änderungsliste). Kein „Entsperren“.
4. **Vorschau** (Wasserzeichen ENTWURF) sperrt nicht; nur der finale Export sperrt.
5. Arbeitnehmererklärung wird beim finalen Export digital bestätigt; das PDF enthält zusätzlich eine Unterschriftszeile.
6. Gehaltsumwandlung + Pauschalierung gleichzeitig: nur Warnung, Berechnung folgt der Konfiguration.
7. Monatslimit standardmäßig **warnen** (nicht blockieren).
8. Standard-Eigenanteil-Variante `standard` (Eigenanteil wird angerechnet).
9. Passwort nur per Env/Secret (kein Setup-Assistent, kein Passwortwechsel in der UI); OIDC verlangt Allowlist (`sub` oder verifizierte E-Mail).
10. Gespeichert wird das normalisierte Bild (≤ 2400 px, ohne EXIF/GPS), nicht das Original.
11. Datenimport ersetzt immer den gesamten Bestand (kein Zusammenführen) und legt vorher automatisch eine Sicherung an.
12. Bezugsorte: Supermarkt, Restaurant, Kantine, Bäckerei, Lieferdienst, Sonstiges; Arbeitsort Betrieb/Homeoffice als zusätzliches Feld.

### 19.4 Intentional deviations

- Monatsexport blobs are never deleted automatically. `BELEGAPP_EXPORT_RETENTION_YEARS` (default 10, the app recommendation in `docs/research/steuer.md` §2.6; 6 matches the Lohnkonto window in § 41 Abs. 1 EStG) only sets `aufbewahrung_bis` / `aufbewahrung_abgelaufen`. Reclaim after the window is not implemented.
- Datenexport `format_version` stays 1. Per-id keys (`bilder/{id}.jpg`, `bilder/{id}.thumb.jpg`) and content-addressed keys are both valid archive members. Import and process start copy legacy keys onto `bilder/{sha[0:2]}/{sha256}.jpg` and `thumbs/{sha[0:2]}/{sha256}.jpg`.
- The month screen still uses the cheap image and audit checks. Export, PDF preview, and `GET /monate/{monat}/pruefpunkte` hash images and walk the full chain. The export dialog reloads that result when final export returns `E_PRUEFPUNKT_FEHLGESCHLAGEN`.
- Job rows store `err.Error()` for the operator (`safeText`, truncated). That text is not part of the public problem JSON.
- LLM and OIDC base URLs are operator configuration. Private hosts stay allowed so a local Ollama endpoint works. There is no switch to block them.
- An image whose stored bytes do not match `belegbilder.sha256` is left on its old key and the process still starts. The export-time hash check then fails closed.
