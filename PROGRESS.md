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
- **Der `darwin`-Job ist entfallen.** Er baute nur macOS-Tarballs; mit dem neuen Release-Profil (siehe unten) gibt es keinen Abnehmer, und die CI hat einen Job weniger.

Gemessen nach dem zweiten Lauf auf einem Branch (Run 39127716001, Push ohne Release-Dateien, vorgewärmte
Caches):

| Job | vorher | nachher |
| --- | --- | --- |
| `go` | 335 s | 124 s |
| `web` | 46 s | 41 s |
| `linux-amd64` | 84 s | 33 s |
| `linux-arm64` | 97 s | 32 s |
| `pdf` | 40 s | 43 s |
| `e2e` | 106 s | 66 s |
| `docker` | 40 s | 50 s |
| `vuln` | 21 s | 25 s |
| `release-dry-run` | 264–382 s | nicht gelaufen |

**Wanduhr 385–409 s → 128 s.** Der kritische Pfad ist jetzt der `go`-Job; `e2e` folgt `linux-amd64` (33 s).
Derselbe Wert auf `main` selbst (Run 39128949622, 129 s, `go` 126 s), also nicht nur auf einem Branch, der
seinen Cache schon zweimal gefüllt hat.

Der **erste** Lauf mit neuen Cache-Schlüsseln ist teurer als vorher (`go` 349 s), weil jeder Job seinen Cache
einmal schreibt; ab dem zweiten greift er. Caches gelten zwar je Branch, GitHub reicht einem Lauf aber auch
die Caches von `main` weiter — ein neuer Branch findet den Schlüssel also vor, sobald `main` ihn einmal
gefüllt hat, und zahlt den kalten Lauf nur, wenn er `main` zuvorkommt. `release-dry-run` läuft weiterhin,
sobald ein Push eine Release-Datei berührt — auch `.github/workflows/ci.yml` selbst —, und ist dann mit
~283 s wieder der längste Job.

## Release-Profil (18.10.2026)

**Veröffentlicht wird nur noch das Container-Image.** Das Release trug vorher vier `tar.gz` (linux und darwin,
je amd64/arm64) mit `LICENSE`, `README.md` und `deploy/`, dazu `checksums.txt` mit cosign-Blob-Signatur, ein
Syft-SBOM je Archiv und eine `actions/attest-build-provenance`-Attestation auf `checksums.txt`. Deployt wurde
von all dem immer nur das Image. Ein Image wird über seinen Digest adressiert und über diesen verifiziert;
ein Checksum daneben fügt nichts hinzu, und die beiden Binaries im Image sind dieselben, die die CI baut.

Jetzt: `linux/amd64` und `linux/arm64`, das Multi-Arch-Image `ghcr.io/blackdark/vc-belegapp`, die
Digest-Signatur, die Buildx-SBOM-Attestierung und die Provenance-Attestation. Das GitHub-Release selbst
besteht aus den release-please-Notes plus dem Image-Footer. Aus `.goreleaser.yaml` sind `archives`,
`checksum`, `signs` und `sboms` entfallen, aus der Release-Action das Syft-Install (die Image-SBOM kommt von
Buildx, `dockers_v2.sbom`), aus `release.yml` die Attestation auf `checksums.txt`, aus der CI der
`darwin`-Job und aus `build-binaries.sh` die darwin-Ziele. Einzelheiten: [releases](docs/releases.md),
[SPEC](docs/SPEC.md) §16.2.

**Nachtrag (18.10.2026): der erste Anlauf war unvollständig.** Die Schlüssel wegzulassen schaltet bei
GoReleaser nichts ab. Der Trockenlauf baute weiterhin
`dist/vc-belegapp_1.1.1-snapshot_linux_amd64.tar.gz`, `…_arm64.tar.gz` und `…_checksums.txt` — im Log als
`• archives` und `• calculating checksums`, und bei einem echten Release wären alle drei **hochgeladen**
worden. Nachgelesen im Quellcode von v2.18.2: `archive.Default` ersetzt eine fehlende **oder leere**
`archives`-Liste durch ein Standardarchiv, und `archive.Skip` fragt ausschließlich `--skip=archive` ab;
`UploadableBinary` entsteht nur in diesem Pipe-Zweig, die `Binary`-Artefakte des Builds sind **nicht**
uploadbar. `checksum` hat gar keinen Skip-Schalter, aber ein `disable`. Der Fix: `--skip=archive` an beiden
Aufrufstellen (`release.yml` und der Trockenlauf) und `checksum: {disable: true}` in der Konfiguration.
Lokal mit `goreleaser release --snapshot --clean --skip=before,docker,archive` nachgestellt: `dist/` enthält
nur die zwei Binaries und die Metadaten, `artifacts.json` nur `Binary` ×2 und `Metadata` — nichts, was der
GitHub-Publisher hochladen könnte (`release.include_meta` ist aus). Damit stimmt die Aussage „das Release
trägt keine Assets“ auch wirklich.

Zwei Irrtümer aus derselben Runde, korrigiert: Der Trockenlauf **baut** die Images sehr wohl lokal
(`--load`, ca. 17 s, „snapshot build: will not push any images“); `--skip=publish` unterbindet nur das
Pushen, nicht das Bauen. Und „die Datei im Trockenlauf prüfen“ war der einzige Weg, das hier zu finden: die
CI prüft `goreleaser check` und den sauberen Checkout, aber ob die richtigen Artefakte entstehen, sieht man
nur im Log.

## Betriebshandbuch nachgezogen (18.10.2026)

`docs/deployment.md` beschrieb den Container, aber nicht fünf Dinge, die im Betrieb tatsächlich beißen. Nachgetragen:

- **`Writable space`.** Der Anlass war ein Widerspruch in den eigenen Unterlagen: die Ingress-Vorlage wirbt mit
  `proxy-body-size: 4g`, passend zu `BELEGAPP_IMPORT_MAX_BYTES` (4 GiB), während `/tmp` in den Vorlagen 64 MiB
  (`docker run`), 512 MiB (Compose — und das als tmpfs, also **RAM**, gegen `mem_limit: 512m`) und `emptyDir
  sizeLimit: 1Gi` (Kubernetes, dort auch im Backup-CronJob) bekommt. Ein Datenimport wird aber **vollständig** nach
  `$TMPDIR` geschrieben, bevor er überhaupt geprüft wird; beim Bestätigen kommen die Sicherungskopie und die
  entpackten Mitglieder dazu. Passt es nicht, antwortet der Server `500 E_INTERN`, und der Grund steht nur im Log —
  ein kurzer Schreibvorgang ist kein zu großer Request, also greift die 413-Behandlung nicht. Die neue Tabelle nennt
  jede Datei (`belegapp-import-*`, `-fetch-*`, `-safety-*`, `-member-*`, `-snap-*`, `-export-*`, `-pdf-*`), wer sie
  schreibt und wie groß sie wird, dazu die drei Zahlen der Vorlagen und den Ausweg `TMPDIR=/data/tmp`, mit dem alles
  auf dem Datenvolume landet. Das Verzeichnis muss vorher existieren und UID 65532 gehören: `prepareDirs` legt
  `/data`, das Blob-Verzeichnis und das Verzeichnis von `BELEGAPP_DB_PATH` an, sonst nichts.
- **`Blob storage`.** `fs` gegen `s3`, und was der Store außer Belegbildern noch trägt (`datenexporte/`, `importe/`).
  Ausdrücklich festgehalten: **nichts löscht je einen Export**, `BELEGAPP_EXPORT_RETENTION_YEARS` schreibt nur ein
  Datum — wer monatlich exportiert, sammelt ein Archiv je Export —, und mit `s3` bleibt die Datenbank auf dem
  Datenvolume, weshalb ein kopierter Bucket allein keine Sicherung ist.
- **`Egress`.** Die fünf Gegenstellen (IdP, LLM, S3, IMAP, Webhook), passend zur `networkpolicy.yaml`, die bisher nur
  „DNS erlauben“ sagte. Der LLM-Eintrag spricht aus, dass der Beleg selbst den Host verlässt, und der Aufruf trägt
  außer Prompt und JPEG nichts.
- **`Metrics`.** Der zweite Listener hat keine Authentifizierung; die Vorlagen veröffentlichen ihn nirgends, der
  Service kennt nur 8080.
- **`Upgrading`.** `serve` migriert vor dem Zuhören, es gibt **keine Down-Migration**, also ist der Datenexport der
  einzige Rückweg; und ein Container, der nur gepullt und nie gestartet wurde, hat die Datenbank nicht angefasst.

`docs/configuration.md` hatte genau eine Lücke: die Variablentabelle wird von
[`internal/config/readme_test.go`](internal/config/readme_test.go) gegen den Code erzwungen, aber `TMPDIR` ist keine
`BELEGAPP_*`-Variable und stand nirgends. Dafür gibt es jetzt den kurzen Abschnitt *Temporary files*, der auf die
Betriebsseite zeigt.

**Nicht geändert:** die Werte in `deploy/`. Sie zu erhöhen wäre eine Verhaltensänderung an RAM-Limit und Volume-Größe
und gehört nicht in eine Dokumentationsrunde; die neue Tabelle nennt sie stattdessen beim Namen und macht die
Entscheidung sichtbar.

## Instanzpasswort in der Datenbank (18.10.2026)

Anlass war [Issue #47](https://github.com/BlackDark/vc-belegapp/issues/47), Frage 1: *„why argon2password hash via env var?
not user friendly?"* Die Frage war berechtigt. Der Betreiber musste den Hash mit `belegapp hash-password` erzeugen, ihn in
ein Secret legen, dieses in Compose und Kubernetes einhängen — und konnte ihn danach nie wieder ändern, ohne den Dienst
neu mit einem neuen Secret zu starten. **Entschieden: die Datenbank ist die einzige Quelle.** Der Hash liegt als Zeile
`password_hash` in `system`, also dort, wo die Belege liegen: auf dem Datenvolume, in jeder Sicherung, ohne Datei und
ohne Env-Variable.

- **`serve` prüft nach der Migration**, ob ein Passwort oder `BELEGAPP_OIDC_ISSUER_URL` existiert, sonst Exit 2 wie
  vorher. Die Prüfung kann nicht mehr vor dem Öffnen der Datenbank laufen, weil die Antwort eine Zeile ist.
- **`belegapp set-password`** ersetzt `hash-password`: Schema anwenden, Passwort lesen (am Terminal zweimal verdeckt,
  sonst die erste Zeile von stdin — Pipe und Container-Setup), hashen, schreiben. Mindestlänge 12 Zeichen, darunter Exit
  2 ohne Schreibvorgang. Kein laufender Server nötig, deshalb ist `docker compose run --rm belegapp set-password` der
  Bootstrap-Schritt.
- **Wechsel in der UI:** `Einstellungen → Passwort`, `PUT /api/v1/auth/passwort` mit `{aktuell, neu}`. Das aktuelle
  Passwort ist Pflicht — eine Sitzung sagt, wer sich einmal angemeldet hat, nicht wer gerade davorsitzt. Bei Erfolg
  fallen alle anderen Sitzungen weg, und die Änderung steht als `zugang/passwort_geaendert` im Protokoll: die Entität
  `zugang` betrifft keinen Monat und taucht damit **nicht** im Monatsexport auf, und weder Passwort noch Hash stehen im
  Eintrag. Hash und Protokollzeile entstehen in derselben Transaktion, damit die Kette keine Änderung behaupten kann,
  die es nicht gab.
- **Entfallen:** `BELEGAPP_AUTH_PASSWORD_HASH`, `BELEGAPP_AUTH_PASSWORD_HASH_FILE`, `belegapp hash-password`,
  `config.AuthConfigured()`, die PHC-Prüfung beim Start und der Hash-Eintrag in `applySecretFiles`.
- **Vorlagen:** Compose und Kubernetes halten kein Passwort mehr. Die OIDC-Zeilen im Compose-Beispiel sind
  auskommentiert, damit ein Passwort-only-Start nicht an `OIDC_SECRET_FILE` scheitert; der `oidc_secret`-Mount zeigt
  dann auf `/dev/null`. SPEC §5.2 (`system`), §9.1/§9.2, §12.1 (`PUT /auth/passwort`), §13.2 (Passwortkarte), §15.3,
  §15.4 und §19.1 Nr. 9 tragen die Umstellung samt Datum; `docs/configuration.md` hat den neuen Abschnitt *The instance
  password*, `docs/deployment.md`, `README.md` und `docs/FEATURES.md` den neuen Ablauf.

**Geprüft:** `internal/db/system_test.go` (Round-trip, Upsert, leerer Hash wird verweigert),
`internal/auth/password_test.go` (Längengrenze, auch mit Mehrbyte-Zeichen), `internal/service/credentials_test.go`
(Wechsel, falsches aktuelles Passwort, zu kurzes neues, keine Protokollspur ohne Änderung),
`internal/api/password_test.go` (HTTP-Ablauf einschließlich Ende der anderen Sitzung) und `cmd/belegapp/cli_test.go`
(`set-password` über eine Pipe, verweigert zu Kurzes, der geschriebene Hash verifiziert das Passwort). Die API-Suiten
setzen ihr Passwort über dieselbe Zeile statt über das frühere Feld, `scripts/container-smoke.sh` und `e2e/fixtures.ts`
rufen `set-password` auf, statt eine Variable zu setzen.

**Nicht geändert:** die OIDC-Seite. Die Allowlist bleibt Pflicht, `serve` akzeptiert weiter beide Methoden gleichzeitig. **Nachtrag:** mit der zweiten Frage aus Issue #47 (unten) akzeptiert dieselbe Prüfung auch `BELEGAPP_AUTH_HEADER_ENABLED` — es sind damit drei Methoden.

## Vertrauenswürdige Header statt zweiter Anmeldung (18.10.2026)

Zweite Frage aus [Issue #47](https://github.com/BlackDark/vc-belegapp/issues/47): *support trusted-header authentication
(`Remote-User`/`Remote-Email`, only from `BELEGAPP_TRUSTED_PROXIES`), like vc-reisekostenabrechnung (`HEADER_AUTH_*`).*
Hinter einer Forward-Auth (Traefik gatepost, Envoy Gateway, oauth2-proxy, Authelia) ist der Browser schon angemeldet,
belegapp verlangte aber trotzdem sein eigenes Passwort oder einen zweiten OIDC-Rundlauf. **Der Header wird zur Identität,
aber nur aus einem vertrauenswürdigen Proxy-Netz und nur für Werte auf einer Liste.**

- **Vier Variablen:** `BELEGAPP_AUTH_HEADER_ENABLED` (aus), `BELEGAPP_AUTH_HEADER_NAME` (`Remote-Email`),
  `BELEGAPP_AUTH_HEADER_ALLOWED` (leer) und `BELEGAPP_AUTH_HEADER_LOGOUT_URL` (leer). Der Name ist die Wahl des
  Betreibers, und der Wert darin ist genau das, was in der Liste steht: `Remote-Email` schreibt eine Adresse hinein,
  `Remote-User` oder `X-Forwarded-User` einen Anmeldenamen. Deshalb versteht die Liste beides — dieselben
  Ganzlabel-Platzhalter wie `BELEGAPP_OIDC_ALLOWED_EMAILS` (`*`, `*@example.de`, `*@*.example.de`, eine genaue Adresse)
  **und** einen Wert ohne `@`, der literal verglichen wird (`eduard`). `*` ist die ausdrückliche Schreibweise für „alle,
  die der Proxy authentifiziert".
- **Zwei Pflichtangaben, beide Startfehler:** `BELEGAPP_TRUSTED_PROXIES` (ohne die Liste wäre der Header eine
  Behauptung, die jeder abgeben kann) und `BELEGAPP_AUTH_HEADER_ALLOWED` (leer = niemand). Der Headername muss ein
  gültiger Feldname sein, die Logout-Adresse eine absolute http(s)-URL.
- **Ein doppelter Header ist wie kein Header.** Ein Client kann ihn selbst mitsenden; ein Proxy, der anfügt statt zu
  ersetzen, ließe sonst die Wahl zwischen beiden Werten offen. Diese Stelle darf nicht raten, also antwortet sie `401`.
- **Ablauf:** Cookie zuerst; ohne gültige Sitzung entscheidet der Header, und es entsteht eine gewöhnliche Sitzung mit
  Akteur `header:{identität}`. Die Liste entscheidet, **wer** eine Sitzung eröffnen darf, nicht was bei jeder Anfrage
  neu geprüft wird — ein gestrichener Eintrag endet mit der nächsten Anmeldung. Keine Identität `401 E_UNANGEMELDET`,
  Identität ohne Listeneintrag `403 E_NICHT_BERECHTIGT`.
- **UI:** `/auth/config` meldet `header` und `header_logout_url`; `/auth/me` meldet `methode: "header"`. Die
  Einstellungen zeigen für so eine Sitzung **kein** `Abmelden` (das Löschen der Sitzung brächte nur beim nächsten
  Aufruf eine neue vom Proxy) und stattdessen den Link auf die Proxy-Logout-Adresse, wenn eine konfiguriert ist. Die
  Login-Seite erklärt den Proxy, wenn der Proxy die Anfrage ohne Identität durchgelassen hat. `403` auf `/auth/me`
  landet mit Begründung auf der Login-Seite statt in einer leeren Shell.
- **Nebenbei repariert:** die Passwortkarte erscheint nur noch, wenn überhaupt ein Passwort existiert. Auf einer
  Instanz, die nur OIDC oder nur Header kennt, hätte sie nach einem aktuellen Passwort gefragt, das niemand je gesetzt
  hat.
- **Vorlagen und Doku:** Compose, `.env.example` und die Kubernetes-ConfigMap halten die Zeilen auskommentiert; im
  Compose-Beispiel steht jetzt auch, dass `OIDC_SECRET_FILE` bei einem **public client** (nur PKCE) leer bleibt. SPEC
  §5.1 (Akteur, Aktion), §8.2 (`/auth/config`, `/auth/me`), §9.1, §9.6 (neu), §13.2, §14, §15.3, §15.4 und §19.1 Nr. 9
  tragen die Methode; `docs/configuration.md` hat den Abschnitt *Trusted-header authentication*,
  `docs/deployment.md` den Abschnitt *Trusted headers (forward auth)*.

**Geprüft:** `internal/allowlist` (`ParseValue`: barer Wert literal, `*`/Domänenmuster wie bisher, `edu*` weiter
Startfehler), `internal/auth/header_test.go` (nur aus dem Proxy-Netz, fehlender/leerer/doppelter Header, leere
Proxy-Liste, nil-Resolver), `internal/config/config_test.go` (Default `Remote-Email`, fehlende Proxies, fehlende oder
kaputte Liste, kaputter Headername, kaputte Logout-Adresse), `internal/api/header_auth_test.go` (Anmeldung per Header,
Sitzung trägt danach, Muster, 401/403, `auth/config`, Methode aus = Header wirkungslos) und
`e2e/tests/header-auth.spec.ts` (zwei Browser-Kontexte gegen einen Stack, der `127.0.0.1/32` als Proxy-Netz führt: einer
mit Header landet ohne Anmeldung auf *Heute*, einer ohne sieht den Proxy-Hinweis, einer mit fremder Identität die
Berechtigungsmeldung). Dazu `web/src/screens/Login.test.tsx` (Proxy-Hinweis), `Settings.test.tsx` (Proxy-Logout-Link,
kein `Abmelden` ohne URL, keine Passwortkarte ohne Passwort) und die Shell-Weiterleitung für `403`.

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
- **`auto_vacuum=INCREMENTAL` wird auf einer bestehenden Datenbank beim ersten Start umgestellt.** SQLite übergeht die
  Einstellung auf einer Datei, die bereits Tabellen hat — stillschweigend, absichtlich. Deshalb liest der Start
  `PRAGMA auto_vacuum` und führt bei Bedarf einmalig `PRAGMA auto_vacuum=INCREMENTAL; VACUUM;` aus. Das ist ein
  vollständiger Neuschrieb der Datei: ein langsamerer Start und vorübergehend freier Platz in der Größe der
  Datenbank. Ein Fehlschlag ist nicht fatal; `BELEGAPP_DB_AUTO_VACUUM_MIGRATE=false` überspringt den Schritt, dann
  behält die Datei ihre Größe und das Aufräumen begrenzt nur das Wachstum — Schritt für Schritt in
  [configuration](docs/configuration.md#the-jobs-table-and-the-size-of-the-database).
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
  `TestPruneDoesNotDelayJobExecution`); `auto_vacuum` ohne das anschließende `VACUUM` wäre auf einer bestehenden
  Datenbank wirkungslos — die einmalige Umstellung beim Start kam später dazu; die Dateigrößenmessung musste das
  WAL erst zurückfalten; und das Test-Cleanup schlug mit „directory not empty" fehl, weil `database/sql` die
  `-wal`-/`-shm`-Dateien nach `Close` neu anlegt.
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
  Pauschalierung nur Warnung), **Nr. 7** (Limit-Modus `warnen`: warnt nur, blockiert nie), **Nr. 9** (Passwort: am 18.10.
  überholt — es liegt als Zeile in der Datenbank und wird in der UI gewechselt, die Allowlist bleibt Pflicht), **Nr. 10** (nur das normalisierte Bild), **Nr. 11** (Import ersetzt alles, mit Sicherung
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
- **Die `/tmp`-Größen in `deploy/` passen nicht zu `BELEGAPP_IMPORT_MAX_BYTES`.** Ein 4-GiB-Import, den die
  Ingress-Vorlage bewirbt, passt weder in 64 MiB (`docker run`), noch in 512 MiB tmpfs (Compose), noch in die
  1 GiB `emptyDir` (Kubernetes). [deployment.md](docs/deployment.md#writable-space) nennt die Zahlen jetzt
  ausdrücklich; die Vorlagen selbst anzupassen ist eine Entscheidung über RAM- und Volume-Grenzen.

## Verwandte Dokumente

- [`docs/ROADMAP.md`](docs/ROADMAP.md) — genehmigter Rückstand und mögliche Features
- [`docs/SPEC.md`](docs/SPEC.md) — §16.4 Branch-Schutz, §18 Meilensteine, §19 offene Punkte, §5.2 Warteschlange
- [`docs/FEATURES.md`](docs/FEATURES.md) — Changelog je Version
- [`docs/configuration.md`](docs/configuration.md) — alle `BELEGAPP_*`-Variablen
- [`docs/deployment.md`](docs/deployment.md) — Betrieb, Datenbankgröße, temporärer Speicher, Objektspeicher, Upgrade
