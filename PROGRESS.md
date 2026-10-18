# Stand der Umsetzung

Diese Sitzung hat den offenen Rückstand der [ROADMAP](docs/ROADMAP.md) abgearbeitet und die Codebasis
aufgeräumt: die amtlichen Lohnsteuerwerte aus der API statt aus dem Frontend-Bundle (U1), doppelte
Service-Logik zusammengeführt (U2), Dateien und interne Zeichenketten englisch (U3), der komplette
Eingangsweg für Prüfbilder — Liste „Offene Bilder" (U4a), IMAP-Abruf (U4b), Webhook (U8), überwachter
Ordner (U7). Danach die Betriebseigenschaften: die Warteschlange kann von einem festhängenden Eingang
nicht mehr die Belegerkennung aushungern (U9), und die `jobs`-Tabelle wächst nicht mehr unbegrenzt,
weil abgeschlossene Zeilen stündlich entfernt und der Speicher Platz zurückgegeben wird (U10). Zwei
geplante Eingänge wurden bewusst nicht gebaut; die Gründe stehen unten. Jede Einheit ist vor dem Commit geprüft
worden, die Entscheidungen stehen in [SPEC](docs/SPEC.md) und [configuration](docs/configuration.md).

Eine zweite Runde (U11–U19) hatte gemessene statt vermutete Belege: jede verdächtige Abfrage aus
`internal/db/queries/` wurde mit `EXPLAIN QUERY PLAN` gegen das wirklich migrierte Schema gelesen, alle
Migrationen aus `internal/db/migrations/` auf eine frische Datenbank gelegt. Der Durchlauf fand vier neue
Migrationen, drei Taktgeber, ein N+1 und fünf stillschweigend verworfene Fehler. Alles davon ist gelandet.
Die Runde deckte außerdem einen CI-Ausfall auf, der unentdeckt auf `main` stand — siehe [CI](#ci-der-rote-verlauf-auf-und-seine-auflösung).

## Erreichte Einheiten

| Einheit | Was sie getan hat | Commit | Status |
| ------- | ----------------- | ------ | ------ |
| U1  | Amtliche Werte (Sachbezugswerte, Kirchensteuersätze, Mahlzeit- und Bezorts-Labels) aus der API ausliefern; `web/src/lib/amtlich.ts` gelöscht | `a05678e` | erledigt |
| U2  | Refactoring: doppelte Service-Logik zusammengeführt (Monatsparsing, Problemkonstruktoren, Katalog, CSV-Zeilen, Restore-Rollback) | `2c2981e` | erledigt |
| U3  | Sprachlauf: Dateien und Verzeichnisse auf Englisch umbenannt, Kommentare, Logmeldungen, interne Fehlerstrings und Testnamen übersetzt. Domänen-Begriffe und Nutzertext bleiben deutsch | `25d8fc3` | erledigt |
| U4a | `GET /api/v1/belegbilder?offen=true` plus Screen „Offene Bilder" mit Deep-Link auf `/pruefen?bild=<id>` | `a41bb9d` | erledigt |
| U4b | IMAP-Abruf: Belegbilder aus einem Postfach als unzugewiesene Bilder einlesen; Mail wird nie gelöscht | `a629028` | erledigt |
| U8  | Ausgehender Webhook, sobald ein Bild ohne Beleg zur Prüfung liegt; eine Codebahn für alle Eingänge, die Adresse wird nur in redigierter Form protokolliert | `9f219db` | erledigt |
| U7  | Überwachter Ordner: eingelegte Bilder als unzugewiesene Bilder einlesen; die Quelle wandert nach `.processed`, wird nie gelöscht | `6acbb53` | erledigt |
| U9  | Warteschlange gehärtet: Parallelitätsgrenze je Jobtyp, Frist je Hintergrundauftrag, Zurückstellen ohne Versuchsverbrauch. Dazu Konfigurations- und Redaktionskorrekturen: Postfachpasswort wird verlangt, Mailbox-Details landen nicht mehr im Fehlertext | `964f99b`, `bf311ba` | erledigt |
| U10 | Aufräumen der Job-Historie: abgeschlossene Zeilen werden nach `BELEGAPP_JOB_RETENTION` gelöscht, freie Seiten über `auto_vacuum=INCREMENTAL` zurückgegeben | `364c527` | erledigt |
| U11 | Indizes für die zwei heißen Zugriffe: `blob_key`/`thumb_blob_key` auf `belegbilder` und ein partieller Index `jobs(typ,payload) WHERE status IN ('wartend','laeuft')`. Migration `00009_lookup_index.sql`, jeder Query-Plan von einem Test festgeschrieben. Der als redundant verdächtigte Index `belegbilder_beleg` **bleibt**: gemessen brachte sein Wegfall nichts | `5bab499` | erledigt |
| U12 | `BELEGAPP_JOB_RETENTION` wird beim Start gegen die Download-Frist eines Datenexports geprüft (beide lesen dieselbe Konstante, können also nicht auseinanderlaufen). Dazu Jobfristen je Typ: `BELEGAPP_JOB_TIMEOUT_ERKENNUNG`, `_DATAEXPORT`, `_MAIL`, `_NOTIFY`, `_WATCH`; `0` heißt weiterhin „keine Frist". Unbekannter Typ oder unlesbare Dauer wird beim Start abgelehnt, nicht ignoriert | `94fa6d9` | erledigt |
| U13 | `jobs.TypErkennung`, `TypDatenexport`, `TypMail`, `TypNotify`, `TypWatch` als exportierte Konstanten; die etwa zwanzig Stellen, die den Typ als String-Literal ausschrieben, benutzen sie. Ein Test liest die `CHECK`-Bedingung auf `jobs.typ` aus dem `sqlite_master` einer wirklich migrierten Datenbank, damit Go-Liste und Tabelle nicht auseinanderlaufen können | PR #25 (`cf63f31`) | erledigt |
| U14 | Die vier stillschweigend verworfenen Fehlerstellen sind sichtbar: `RunJobs` protokolliert einen fehlgeschlagenen `Reset`/`ResetLaufendeErkennung`/`ListErkennungOffen`, `JobRetry`/`JobFail` einen fehlgeschlagenen Status-Write, `sweepOnce` warnt je Bild und fasst die Fehlschläge zusammen, statt einen teilweisen Durchlauf als Erfolg zu melden | PR #25 (`cf63f31`) | erledigt |
| U15 | Gemeinsame Poll-Schleife `pollLoop` mit `enqueueUnlessOpen` und den Log-Helfern für `mail` und `watch`, die vorher zweimal derselbe Code waren. `Sweep` und `Pruner` bleiben bewusst eigenständig: sie sind keine Eingangs-Taktgeber | PR #25 (`cf63f31`) | erledigt |
| U16 | `ANALYZE` läuft am Ende jedes Aufräumdurchlaufs (begrenzt auf 15 s, Fehler nicht fatal). Ohne Statistiken fielen die Query-Pläne in Migration `00008` und `00009` auf Vollabtastungen zurück. **Mit U11 gelandet**, nicht danach: der Plan-Test legt den Index wieder weg und misst neu, er kann ohne `ANALYZE` also nicht grün werden | `5bab499` | erledigt |
| U17 | `sweepOnce` liest und löscht in Batches von 200 mit einem Keyset-Cursor auf `(erstellt_am, id)` statt in einer unbegrenzten Abfrage auf der einen Schreibverbindung. Ein Offset wäre falsch: das Löschen eines Batches schiebt jede Zeile hinter dem Fenster um genau dessen Länge vor, `LIMIT ... OFFSET n` überspränge also lebende Zeilen | PR #26 (`c487e55`) | erledigt |
| U18 | `Bild.DuplikatVon` kostete eine Duplikatsuche je Bild. Jetzt eine `IN (...)`-Abfrage für die ganze Seite, Paarung und `LIMIT 1` in Go; der Kandidat ist der erste nach `(b.id, i.id)` und damit wiederholbar. `internal/service/warnings.go` profitiert mit | PR #26 (`c487e55`) | erledigt |
| U19 | Ticker-Intervalle werden injiziert (`internal/jobs` von 19,2 s auf 11,0 s), und die zwei Kopien eines Warte-Helfers mit vertauschten Argumenten wurden zu einem `dbtest.WaitFor`, dessen falsche Reihenfolge nicht übersetzt. `internal/service` blieb bei ~20 s: die Kosten liegen in der Fixture (`dbtest.OpenSeeded` spielt neun Migrationen je Test), nicht in den Takten | PR #26 (`c487e55`) | erledigt |

U4 ist über die ursprüngliche Ein-Zeilen-Aufgabe hinausgewachsen und in zwei Commits gelandet (U4a Liste,
U4b Abruf). U7 und U8 sind in dieser Reihenfolge gelandet, nicht in der ursprünglichen — die Absicht war beide
Male, den Prüfweg vor der Benachrichtigung zu haben. Ab U13 geht alles über einen Pull Request und landet als
Squash in der Historie von `main`: `cf63f31` ist der Squash-Stand von PR #25, `c487e55` der von PR #26.

## Höchstzuschuss je Mahlzeitart (14./15.10.2026)

Der Vorschlag für `zuschuss_cent` kam aus `SBW_mittag + 310`, egal welche Mahlzeiten bezuschusst werden. Eine Regel
mit Frühstück bekam damit 7,67 € gegen einen Frühstücks-Sachbezugswert von 2,37 €: über der Grenze,
`W_HOECHSTZUSCHUSS` auf jedem Frühstücksbeleg und der Mehrbetrag als regulärer, damit sozialversicherungspflichtiger
Lohn. `rules.HoechstzuschussCent` leitet die Grenze jetzt aus dem SBW der jeweiligen Mahlzeitart und dem einen
konfigurierbaren Aufschlag ab (2026: 547 Frühstück, 767 Mittag/Abend); der Vorschlag — eine Zahl für alle
bezuschussten Mahlzeitarten — aus dem kleinsten dieser Grenzen. **Mittag und Abend bleiben numerisch unverändert**
(767/767), nur der Frühstücksvorschlag wandert auf 547. Drei Zuschussbeträge je Mahlzeitart bräuchten drei Spalten;
`zuschuss_cent` bleibt eine gemeinsame — wer für Frühstück mehr zahlen will, hebt den Wert und nimmt die Warnung in Kauf.
Bestätigt am **15.10.**, ohne Codeänderung: der gemeinsame Wert bleibt, die daraus folgende Grenze ist bewusst
angenommen. `main` stand dabei auf `12ba364`, der Squash-Stand ist `48b3144` (PR #30).

## Bewusst verworfen

| Einheit | Grund |
| ------- | ----- |
| U5 — PWA Web Share Target | Nur Chromium/Android. iOS Safari kennt kein `share_target`, und iOS teilt HEIC, das die Bildverarbeitung ablehnt. Der CSRF-/Cookie-Risikofaktor lohnt diese Abdeckung nicht. |
| U6 — Webhook-Eingang für Mail | Der IMAP-Abruf aus U4b deckt eingehende Mail bereits ab, ganz ohne öffentlichen Endpunkt. Ein Webhook fügte einer im Internet betriebenen Einzelbenutzer-App einen dauerhaft öffentlichen POST-Endpunkt hinzu, der die Sitzungsprüfung umgeht. |

## CI: der rote Verlauf auf `main` und seine Auflösung

**`main` war über sieben aufeinanderfolgende Commits nicht grün, und das fiel erst auf, als jemand die Historie
nachlas.** Bei vier davon (`6acbb53`, `bf311ba`, `887cc26`, `5bab499`) lief CI an und fiel im Job `go` durch; bei
drei (`964f99b`, `364c527`, `94fa6d9`) ist für den Push kein Lauf verzeichnet. Der Fehler war derselbe,
eingeführt mit U7 (`6acbb53`) und behoben mit `8661dd5` (PR #24).

Die Ursache ist ein Test-Fixture, kein Produktfehler. `TestIngestWatchFolderSkipsNamesItDoesNotRead` legte
zwei Dateinamen an, die sich **nur in der Groß-/Kleinschreibung** unterschieden — `beleg.JPG.PART` und
`beleg.jpg.part`. Auf einem dateisystem, das Groß- und Kleinschreibung nicht unterscheidet (macOS, APFS,
Standard), schreibt das zweite stillschweigend über das erste, und die erwartete Dateizahl hält **aus
Zufall**. Auf Linux überleben beide Einträge, die Zählung stimmt nicht mehr, und die Behauptung schlägt fehl:

```
--- FAIL: TestIngestWatchFolderSkipsNamesItDoesNotRead (0.34s)
    watchfolder_test.go:244: folder holds [... beleg.JPG.PART ... beleg.jpg.part ...], want the ten files, the directory and the link
```

Der Test war also seit U7 kaputt, aber nur auf einer Plattform sichtbar. Das eigentliche Versagen ist ein
zweites: **Niemand hat die CI auf `main` nach jedem Push angesehen.** Die lokale Suite lief auf macOS und war
grün, also wurde viermal weitergearbeitet, bis ein Commit aus der Ferne geprüft wurde.

Behoben in `8661dd5` (PR #24): die erwartete Zahl wird aus der Liste abgeleitet, die den Ordner geschrieben
hat, und die Zugehörigkeit wird geprüft statt einer festen Zahl; ein Helfer lehnt jedes Fixture-Paar ab, das
einmal kleingeschrieben kollidiert; ein eigener Test nagelt das Kleinschreiben selbst fest, weil der Sprungtest
sonst nicht fehlschlagen könnte. Ein Berechtigungstest wird übersprungen, wenn die Suite als `root` läuft.

**Die Lehre, die daraus folgt: nach jedem Push den CI-Status auf `main` prüfen, nicht nur die lokale Suite.**

## CI: Laufzeit (18.10.2026)

**Ein Push kostete 385–409 s Wanduhr, davon 335 s im längsten Job `go` und 264 s im Job `release-dry-run`,
der alles noch einmal baut, was `linux-amd64`, `linux-arm64`, `darwin` und `docker` schon gebaut haben.**

Im `go`-Job war die Ursache eine Cache-Falle, keine zu große Suite. `actions/setup-go` legt Modul- und
Build-Cache unter **einen** Schlüssel je `go.sum`, und `actions/cache` schreibt einen Schlüssel nur
**einmal**. Gewinner ist damit der Job, der zuerst fertig wird — in diesem Workflow `vuln` nach 21 s, mit
nichts kompiliert außer `govulncheck`. Die so entstandenen 233 MB finden danach **alle** Jobs vor, und
keiner kann sie mehr ersetzen. `go generate` baute deshalb sqlc bei jedem Lauf neu (61 s), `sqlc-diff.sh`
das Binary (41 s) und die Testschritte jedes Paket (159 s mit `-race`). Dass daran die Kompilierung hängt
und nicht die Prüfungen, ist lokal gemessen: dieselbe Race-Suite braucht mit kaltem Build-Cache 227 s, mit
warmem 58 s.

Geändert:

- **Ein Build-Cache je Job** ([`.github/actions/go-setup`](.github/actions/go-setup/action.yml)). Der
  Modul-Cache bleibt auf dem geteilten `setup-go`-Schlüssel — er ist eine Funktion von `go.sum` und dort
  bereits vollständig —, der Build-Cache bekommt einen Schlüssel je Job. Der Schlüssel wechselt weiterhin
  nur mit `go.sum`: Abhängigkeiten (`modernc.org/sqlite`), Standardbibliothek und Werkzeuge bleiben gültig,
  und Code aus diesem Repository ändert sich zu oft, als dass sich ein Schlüssel je Lauf lohnte — er würde
  je Lauf einige hundert MB in ein Cache-Budget von 10 GB schreiben und die Einträge verdrängen, die die
  anderen Jobs wiederherstellen.
- **Kein Job wartet mehr auf `web`.** Die Plattform-Jobs kompilieren aus dem committeten `web/dist` — derselben
  Eingabe, die ein Release einbettet —, und der `go`-Job braucht das Bundle gar nicht. Der Abgleich, dass das
  committete `web/dist` zum Build passt, läuft im `web`-Job direkt neben dem Build; er benutzt `git status`
  statt `git diff`, weil `diff` eine hinzugekommene Datei nicht sieht. Vorher lagen Bundle-Erstellung (46 s)
  und Artefakt (3 s je Job) vor jedem Plattform-Job und damit vor `e2e`.
- **`release-dry-run` läuft nur noch, wenn der Push eine Datei berührt hat, die GoReleaser liest**
  (`.goreleaser.yaml`, die Dockerfiles, `LICENSE`, `README.md`, `deploy/`, die Release-Workflows, die
  Composite Action, `scripts/install-typst.sh`). Ein Kompilierfehler bleibt dabei nicht unentdeckt: die
  Plattform-Jobs bauen dieselben vier Binaries aus denselben Quellen mit denselben Flags.
- **`e2e` cached Typst und die Playwright-Browser**, statt sie je Lauf zu laden.

Gemessen nach dem zweiten Lauf auf einem Branch (Run 39127716001, Push ohne Release-Dateien, vorgewärmte
Caches):

| Job | vorher | nachher |
| --- | --- | --- |
| `go` | 335 s | 124 s |
| `web` | 46 s | 41 s |
| `linux-amd64` | 84 s | 33 s |
| `linux-arm64` | 97 s | 32 s |
| `darwin` | 134 s | 41 s |
| `pdf` | 40 s | 43 s |
| `e2e` | 106 s | 66 s |
| `docker` | 40 s | 50 s |
| `vuln` | 21 s | 25 s |
| `release-dry-run` | 264–382 s | nicht gelaufen |

**Wanduhr 385–409 s → 128 s.** Der kritische Pfad ist jetzt der `go`-Job; `e2e` folgt `linux-amd64` (33 s).
Der **erste** Lauf mit neuen Cache-Schlüsseln ist teurer als vorher (`go` 349 s), weil jeder Job seinen Cache
einmal schreibt; ab dem zweiten greift er. `release-dry-run` läuft weiterhin, sobald ein Push eine
Release-Datei berührt — auch `.github/workflows/ci.yml` selbst —, und ist dann mit ~283 s wieder der längste
Job.

## Arbeitsweise mit `main`

`main` ist geschützt. Eine Änderung geht über einen Pull Request, wird per Squash zusammengefasst und
automatisch gemergt, sobald die erforderlichen Checks grün sind; erforderlich sind `go`, `web`, `docker`,
`e2e` und `vuln`, und der Branch muss auf dem Stand von `main` sein (0 Reviews, Single-Maintainer, kein
Force-Push, kein Löschen — die vollständige Regel steht als
[.github/rulesets/main.json](.github/rulesets/main.json) und in [SPEC](docs/SPEC.md) §16.4). `gh` ist über
seinen Anmeldedatenspeicher authentifiziert:

```bash
gh pr create --base main --head <branch> --title "..." --body "..."
gh pr merge --squash --auto <pr-number>
```

Nach dem Push wird **nicht** mehr nachgedrückt: kein Force-Push, kein Amend, kein Rebase. Ein Branch hinter
`main` wird gemeldet und aufgegeben, statt die Historie umzuschreiben.

## Bekannte Einschränkungen

Ungeschönt, weil jede davon jemanden betrifft, der die Anwendung betreibt:

- **Eine Frist kann keinen festhängenden Dateisystemzugriff unterbrechen.** Auf einem hängenden NFS- oder
  SMB-Mount bleibt ein `watch`-Auftrag im Lesen, bis der Mount antwortet. Die Parallelitätsgrenze begrenzt das
  auf einen Worker je Typ, aber **zwei** festhängende Typen unterschiedlicher Art — ein hängender Mount und etwa
  ein Webhook-Server, der Verbindungen annimmt und nie antwortet — füllen den Pool von zwei Workern und halten
  die Belegerkennung bis zum Neustart an. Ein Neustart hilft nicht: der Auftrag wird wieder eingereiht und
  hängt wieder. Die Fristen aus U9/U12 ändern daran nichts, weil eine Frist nur einen Auftrag beendet, der
  wieder im Rechner hängt, nicht einen, der noch darin steckt. Abhilfe: `BELEGAPP_WATCH_FOLDER=`.
- **`auto_vacuum=INCREMENTAL` gilt nur für Datenbanken, die ab hier neu angelegt werden.** SQLite übergeht die
  Einstellung auf einer Datei, die bereits Tabellen hat — stillschweigend, absichtlich. Eine bestehende
  Installation löscht weiterhin korrekt, behält aber ihre Dateigröße, bis ein Betreiber einmalig `PRAGMA
  auto_vacuum=INCREMENTAL; VACUUM;` bei gestopptem Prozess ausführt. Ohne diesen Schritt begrenzt das Aufräumen
  nur das Wachstum — Schritt für Schritt in [configuration](docs/configuration.md#the-jobs-table-and-the-size-of-the-database).
- **Der Webhook ist die einzige Ankunftsanzeige für Postfach- und Ordnerbelege.** Prüft niemand einen Eingang,
  vernichtet ihn der TTL-Lauf für unzugewiesene Bilder (Vorgabe `BELEGAPP_UNASSIGNED_IMAGE_TTL=24h`, stündlicher
  Lauf, also nach 24–25 Stunden). Die Ordnerquelle ist nach `.processed` verschoben und liegt noch da; beim
  Postfach ist die Mail nie gelöscht, es sei denn, sie wandert nach `BELEGAPP_MAIL_DONE_FOLDER`. Das Bild ist der
  einzige Verlust. Dazu: ein dauerhaft fehlschlagender Webhook-Auftrag hat **keine** sichtbare Oberfläche — er
  erscheint nur als Status `fehler` in der `jobs`-Tabelle, seit U14 wenigstens mit Logeintrag.
- **Ein dauerhaft fehlschlagender Eingang schreibt weiterhin eine Zeile je Intervall.** Das Aufräumen begrenzt
  die Tabelle, aber es bremst den Auftrag nicht zurück: ein Postfach mit dauerhaft abgelehnten Anmeldedaten
  erzeugt alle fünf Minuten eine neue Zeile, jeder Endzustand wird nach einer Woche entfernt, und dieselbe Woche
  später steht die nächste da. Es gibt keine Meldeschwelle und keinen Circuit-Breaker.
- **`github.com/emersion/go-imap/v2` ist eine Beta-Abhängigkeit** (`v2.0.0-beta.8`, MIT). Die v1-Linie ist seit
  2022 eingefroren, deshalb wurde die v2 gewählt. Ein API-Wechsel beim nächsten Sprung kann Codeänderungen
  erzwingen.
- **Der überwachte Ordner erkennt bereits gespeicherte Bytes am Inhalts-Hash**, legt sie ohne zweites Bild ab
  und meldet das mit einer eigenen Warnung und einem eigenen Zähler, statt sie wie einen gewöhnlichen Fund zu
  verschieben. Dieselbe Quittung wird also nicht doppelt angelegt, aber es ist auch kein Hinweis darauf, dass
  etwas neu eingegangen ist.

Zwei Einschränkungen aus dem vorigen Stand sind erledigt und stehen deshalb nicht mehr hier: `ANALYZE` läuft
jetzt am Ende jedes Aufräumdurchlaufs (U16), und `BELEGAPP_JOB_RETENTION` wird beim Start gegen die
Download-Frist eines Datenexports geprüft (U12).

## Review-Protokoll

Was die Prüfung je Einheit gefunden hat. Vor jedem Review in diesem Repository lohnt der Blick: sie war
bei Oberflächen-Lügen und bei kaputt gebautem `dist` bisher erfolgreich.

- **U1** — ein Toast meldete Erfolg, obwohl der Abruf fehlschlug; die Kontrollkästchen für Mahlzeiten
  erschienen ohne Beschriftung, wenn die Wertetabelle nicht lud.
- **U3** — ein halbfertiger Rename (`werteResponse`/`werte` deutsch in einer englisch benannten Datei); zehn
  nicht verfolgte `web/dist`-Dateien, die ein `git commit -am` als kaputtes Deployment ausgeliefert hätte; ein
  veralteter Git-Index mit Inhalten vor dem Rename, der einen kaputten Build committet hätte.
- **U4a** — fester Nutzertext „nach 24 Stunden", obwohl die TTL konfigurierbar ist; Bilder jenseits des
  API-Limits von 50 waren nicht erreichbar und wurden danach stillschweigend per TTL gelöscht; der
  Thumbnail-401-Pfad war nur durch die Platzierung der Route korrekt und durch keinen Test abgesichert.
- **U4b** — `Peek: false` beim FETCH setzte bei abgelehnten Absendern ungewollt `\Seen`; ein fehlerhafter
  Anhang hielt den Abruf an und verdoppelte alle vorherigen Bilder bei jedem Poll; der Keyword-STORE-Fallback
  meldete Erfolg, ohne eine Markierung zu schreiben, und holte dieselbe Nachricht endlos; eine halb
  gespeicherte Nachricht hinterließ keine Markierung und verdoppelte ihren gespeicherten Anteil.
- **U9** — ein zurückgestellter Auftrag darf keinen Versuch kosten: `ClaimJob` zählte einen, und eine nur im
  Status zurückgesetzte Übernahme ließ eine Zeile in `laeuft` stehen, die nichts zurücksetzte; die
  Abruf-Poller zählen `laeuft` als offenen Auftrag, also unterdrückte eine solche Zeile jeden folgenden Poll
  und der Eingang war bis zum nächsten Start tot. Mail ohne Passwort startete sauber und erzeugte danach
  dauerhaft einen fehlgeschlagenen Auftrag je Intervall, mit „Zugang verweigert" statt „Passwort fehlt".
  Der Mail-Fehlertext trug Mailbox-Host und rohe Serverantwort in die Auftragszeile und ins Log. Vier
  Tests mit festen `time.Sleep` wurden durch deterministische Wartebedingungen ersetzt.
- **U10** — der Statfilter musste in die Query wandern, damit ein Fehler im Aufrufer keine Zeile löschen kann,
  die noch Arbeit schuldet; der Aufräumvorgang darf nicht in der Warteschlange hängen, weil der Schreibkanal nur
  eine Verbindung hat und ein Block auf den ganzen Bestand jeden Auftrag verzögert hätte (festgehalten durch
  `TestPruneDoesNotDelayJobExecution`); `auto_vacuum` an anderer Stelle als bei der Erstellung zu setzen wäre auf
  einer bestehenden Datenbank wirkungslos; die Dateigrößenmessung musste das WAL erst zurückfalten; und das
  Test-Cleanup schlug mit „directory not empty" fehl, weil `database/sql` die `-wal`-/`-shm`-Dateien nach
  `Close` neu anlegt.
- **U11/U16** — die Query-Pläne wurden **nach** dem Anlegen der Migration gegen die frisch migrierte Datenbank
  gelesen, nicht vorher geschätzt; die Tests legen den Index wieder weg und messen neu, sonst wären sie grün,
  ohne dass SQLite ihn benutzt. Der als redundant verdächtigte Index blieb, weil die Messung keinen Unterschied
  zeigte — die Vermutung war falsch, das Messen hat sie widerlegt.
- **U17/U19** — ein Offset-Cursor wäre still falsch gewesen, nicht nur langsam: das Löschen eines Batches
  verschiebt jede Zeile hinter dem Fenster vor, `LIMIT ... OFFSET n` hätte also lebende Zeilen übersprungen.
  Die Poll-Schleife beansprucht außerdem einen Auftrag, **bevor** sie wartet; fast jeder Test zahlte Sekunden
  für einen Taktgeber, der im Millisekundenbereich feuert, und `TestQueueClaimsAJobWaitingBeforeTheFirstTick`
  gibt ihm eine Stunde und fällt ohne diese erste Beanspruchung.

## Konfiguration aus dieser Sitzung

Maßgeblich ist die Tabelle in [configuration](docs/configuration.md); hier nur die Gruppen, damit eine
Änderung dort gefunden wird:

- `BELEGAPP_MAIL_*` — IMAP-Abruf (Host, Port, TLS, Zugang, Absender-Allowlist, Intervall, Grenzen je Abruf)
- `BELEGAPP_NOTIFY_*` — Webhook-Adresse, Token, Timeout
- `BELEGAPP_WATCH_FOLDER`, `BELEGAPP_WATCH_INTERVAL` — überwachter Ordner
- `BELEGAPP_JOB_TIMEOUT` — Frist für die drei Hintergrundaufträge `mail`, `watch`, `notify`
- `BELEGAPP_JOB_TIMEOUT_ERKENNUNG`, `_DATAEXPORT`, `_MAIL`, `_NOTIFY`, `_WATCH` — Frist je Typ, überschreibt
  `BELEGAPP_JOB_TIMEOUT`; `0` bedeutet „keine Frist" (ab U12)
- `BELEGAPP_JOB_RETENTION` — Aufbewahrungsfenster für abgeschlossene Auftragszeilen; muss mindestens so lang
  sein wie die Download-Frist eines Datenexports, sonst wird die Konfiguration beim Start abgelehnt (ab U12)

## Offene Fragen

Die verbindlichen Listen stehen in [SPEC](docs/SPEC.md) §19, nicht hier. Beides braucht noch eine Antwort
des Eigentümers:

- **§19.1** — beantwortet und mit Datum in [SPEC](docs/SPEC.md) §19.1 festgehalten: **Nr. 1, 2, 6, 7, 8** am 15.10.
  (höchstens eine bezuschusste Mahlzeit je Tag; Höchstzuschuss `SBW + 3,10 €` **als Zusatzbetrag**, nicht als
  Gehaltsumwandlung; Fotos genügen; Homeoffice und Kantine akzeptiert; alle drei Übergabewege in Benutzung),
  **Nr. 4 und 5** am 14.10. (Eigenanteil-Variante `standard`, Monatslimit 15), **Nr. 3** am 18.10. (es bleibt beim
  vereinfachten Verfahren, und `kist_satz_bp = 0` ist der vollwertige Wert für „keine Kirchensteuer"; eine neue
  Jahresregel ohne Vorjahr beginnt bei 0). Damit ist **§19.1 vollständig entschieden.** **§19.3** — vollständig bestätigt: am 15.10.
  **Nr. 1, 3** (kein Entsperren), **Nr. 2** (ein Bundesland, ein Kalender für die ganze App), **Nr. 4**
  (Vorschau sperrt nie), **Nr. 5** (Arbeitnehmererklärung wird nicht erfasst), **Nr. 6** (Umwandlung +
  Pauschalierung nur Warnung), **Nr. 7** (Limit-Modus `warnen`: warnt nur, blockiert nie), **Nr. 9** (Passwort nur per Env,
  Allowlist Pflicht), **Nr. 10** (nur das normalisierte Bild), **Nr. 11** (Import ersetzt alles, mit Sicherung
  davor), **Nr. 12**; am 14.10. **Nr. 8**. Dazu zwei Bestätigungen außerhalb von §19, weil sie nirgends eine
  offene Frage waren: der gemeinsame `zuschuss_cent` (§5.3) und die Vorgabe `mittag` allein bei einer neuen
  Jahresregel (§5.2, §5.3). Alle stehen in [NOTES](docs/NOTES.md).

Ergänzt am 15.10. ist außerdem **Ganzlabel-Platzhalter in der OIDC-Allowlist** (`*@example.de`, `*@*.example.de`,
`*`): eine ganze Domain oder alle zuzulassen, ohne die Allowlist optional zu machen. Siehe
[`docs/configuration.md`](docs/configuration.md#the-oidc-email-allowlist).

Diese Sitzung deckt bereits ab, was ohne Antwort auskam: die Eingangswege (IMAP, überwachter Ordner) und
ihre Anzeige (Liste „Offene Bilder", Webhook) sind umgesetzt und in §19.2 als erledigt vermerkt.

Offen bleiben und eine eigene Entscheidung brauchen (alle in [ROADMAP](docs/ROADMAP.md), nicht terminiert):

- **Die Sachbezugswerte für 2027 müssen vor Januar 2027 aktualisiert werden.** Für 2027 liegt bisher nur ein
  Entwurf vor, die Oberfläche warnt bis zum Release mit dem amtlichen Wert. Ein Termin, keine Frage.
- **WebDAV als Eingang** (ROADMAP-Option 4b) — derselbe Aufbau wie der überwachte Ordner, aber ohne Mount
  und mit einer zweiten Zugangsberechtigung in der Anwendung.
- **PDF-Anhänge als Eingabe** — kein Rasterizer im Bestand; §1.2 schließt PDF-Belege aus, bis das entschieden
  ist. Dazu die Frage aus §19.2, wie sich eine prüfpflichtige Belegzuordnung überhaupt abbilden soll.
- **Mehrbenutzerbetrieb, Rollen, Mandanten** — das Datenmodell ist dafür offengehalten, gebaut ist nichts.
- **Offline-Erfassung mit Sync-Warteschlange** und **Regelzeiträume mitten im Jahr** (Teilbelege gehören dazu).

## Verwandte Dokumente

- [`docs/ROADMAP.md`](docs/ROADMAP.md) — genehmigter Rückstand und mögliche Features
- [`docs/SPEC.md`](docs/SPEC.md) — §16.4 Branch-Schutz, §18 Meilensteine, §19 offene Punkte, §5.2 Warteschlange
- [`docs/FEATURES.md`](docs/FEATURES.md) — Changelog je Version
- [`docs/configuration.md`](docs/configuration.md) — alle `BELEGAPP_*`-Variablen
- [`docs/deployment.md`](docs/deployment.md) — Betrieb, Datenbankgröße
