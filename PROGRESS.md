# Stand der Umsetzung

Diese Sitzung hat den offenen Rückstand der [ROADMAP](docs/ROADMAP.md) abgearbeitet und die Codebasis
aufgeräumt. Neun Einheiten sind gelandet: die amtlichen Lohnsteuerwerte kommen jetzt aus der API statt aus
dem Frontend-Bundle (U1), doppelte Service-Logik ist zusammengeführt (U2), Dateinamen und interne
Zeichenketten sind englisch (U3). Danach kam der komplette Eingangsweg für Prüfbilder: die Liste
„Offene Bilder" (U4a), der IMAP-Abruf (U4b), die Webhook-Benachrichtigung (U8) und der überwachte Ordner
(U7). Zuletzt wurden die Betriebseigenschaften gehärtet — die Job-Warteschlange kann von einem festhängenden
Eingang nicht mehr die Belegerkennung aushungern (U9), und die `jobs`-Tabelle wächst nicht mehr unbegrenzt,
weil abgeschlossene Zeilen stündlich entfernt und der Speicher Platz zurückgegeben wird (U10). Zwei geplante
Eingänge wurden bewusst nicht gebaut; die Gründe stehen unten. Jede Einheit ist vor dem Commit geprüft
worden, die Entscheidungen sind in [SPEC](docs/SPEC.md) und [configuration](docs/configuration.md)
dokumentiert, nicht in dieser Datei.

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

U4 ist über die ursprüngliche Ein-Zeilen-Aufgabe hinausgewachsen und in zwei Commits gelandet (U4a Liste,
U4b Abruf). U7 und U8 sind in dieser Reihenfolge gelandet, nicht in der ursprünglichen — die Absicht war
beide Male, den Prüfweg vor der Benachrichtigung zu haben.

## Bewusst verworfen

| Einheit | Grund |
| ------- | ----- |
| U5 — PWA Web Share Target | Nur Chromium/Android. iOS Safari kennt kein `share_target`, und iOS teilt HEIC, das die Bildverarbeitung ablehnt. Der CSRF-/Cookie-Risikofaktor lohnt diese Abdeckung nicht. |
| U6 — Webhook-Eingang für Mail | Der IMAP-Abruf aus U4b deckt eingehende Mail bereits ab, ganz ohne öffentlichen Endpunkt. Ein Webhook fügte einer im Internet betriebenen Einzelbenutzer-App einen dauerhaft öffentlichen POST-Endpunkt hinzu, der die Sitzungsprüfung umgeht. |

## Bekannte Einschränkungen

Ungeschönt, weil jede davon jemanden betrifft, der die Anwendung betreibt:

- **Eine Frist kann keinen festhängenden Dateisystemzugriff unterbrechen.** Auf einem hängenden NFS- oder
  SMB-Mount bleibt ein `watch`-Auftrag im Lesen, bis der Mount antwortet. Die Parallelitätsgrenze begrenzt
  das auf einen Worker je Typ, aber **zwei** festhängende Typen unterschiedlicher Art — ein hängender Mount und
  etwa ein Webhook-Server, der Verbindungen annimmt und nie antwortet — füllen den Pool von zwei Workern und
  halten die Belegerkennung bis zum Neustart an. Ein Neustart hilft nicht: der Auftrag wird wieder eingereiht
  und hängt wieder. Abhilfe: den Eingang abschalten (`BELEGAPP_WATCH_FOLDER=`), bis der Mount zurück ist.
- **`auto_vacuum=INCREMENTAL` gilt nur für Datenbanken, die ab hier neu angelegt werden.** SQLite übergeht die
  Einstellung auf einer Datei, die bereits Tabellen hat — stillschweigend, absichtlich. Eine bestehende
  Installation löscht also weiterhin korrekt, behält aber ihre Dateigröße, bis ein Betreiber einmalig
  `PRAGMA auto_vacuum=INCREMENTAL; VACUUM;` bei gestopptem Prozess ausführt. Ohne diesen Schritt begrenzt das
  Aufräumen nur das Wachstum, es schrumpft nichts. Schritt für Schritt in
  [configuration](docs/configuration.md#the-jobs-table-and-the-size-of-the-database).
- **Der Webhook ist die einzige Ankunftsanzeige für Postfach- und Ordnerbelege.** Prüft niemand einen Eingang,
  vernichtet ihn der TTL-Lauf für unzugewiesene Bilder (Vorgabe `BELEGAPP_UNASSIGNED_IMAGE_TTL=24h`, stündlicher
  Lauf, also nach 24–25 Stunden). Die Ordnerquelle ist nach `.processed` verschoben und liegt noch da; beim
  Postfach ist die Mail nie gelöscht, es sei denn, sie wird nach `BELEGAPP_MAIL_DONE_FOLDER` verschoben. Das
  Bild ist der einzige Verlust. Dazu kommt: ein dauerhaft fehlschlagender Webhook-Auftrag hat **keine**
  sichtbare Oberfläche für den Benutzer — er erscheint nur als Zeile mit Status `fehler` in der
  `jobs`-Tabelle.
- **Ein dauerhaft fehlschlagender Eingang schreibt weiterhin eine Zeile je Intervall.** Das Aufräumen begrenzt
  die Tabelle, aber es bremst den Auftrag nicht zurück: ein Postfach mit dauerhaft abgelehnten Anmeldedaten
  erzeugt alle fünf Minuten eine neue Zeile, jeder Endzustand wird nach einer Woche entfernt, und dieselbe
  Woche später steht die nächste da. Es gibt keine Meldeschwelle und keinen Circuit-Breaker.
- **`github.com/emersion/go-imap/v2` ist eine Beta-Abhängigkeit** (`v2.0.0-beta.8`, MIT). Die v1-Linie ist seit
  2022 eingefroren, deshalb wurde die v2 gewählt. Ein API-Wechsel beim nächsten Sprung kann Codeänderungen
  erzwingen.
- **Der überwachte Ordner erkennt bereits gespeicherte Bytes am Inhalts-Hash**, legt sie ohne zweites Bild ab
  und meldet das mit einer eigenen Warnung und einem eigenen Zähler, statt sie wie einen gewöhnlichen
  Fund zu verschieben. Praktisch: dieselbe Quittung wird nicht doppelt angelegt, aber sie ist auch kein
  Hinweis darauf, dass etwas neu eingegangen ist.

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
- **U10** — der Statfilter musste in die Query wandern, damit ein Fehler im Aufrufer keine Zeile löschen
  kann, die noch Arbeit schuldet; der Aufräumvorgang darf nicht in der Warteschlange hängen, weil der
  Schreibkanal nur eine Verbindung hat und ein Block auf den ganzen Bestand jeden Auftrag verzögert hätte
  (festgehalten durch `TestPruneDoesNotDelayJobExecution`); `auto_vacuum` an anderer Stelle als bei der
  Erstellung zu setzen wäre auf einer bestehenden Datenbank wirkungslos; die Dateigrößenmessung musste das
  WAL erst zurückfalten, sonst misst sie nichts; und das Test-Cleanup schlug mit „directory not empty"
  fehl, weil `database/sql` die `-wal`-/`-shm`-Dateien nach `Close` neu anlegt.

## Konfiguration aus dieser Sitzung

Maßgeblich ist die Tabelle in [configuration](docs/configuration.md); hier nur die Gruppen, damit eine
Änderung dort gefunden wird:

- `BELEGAPP_MAIL_*` — IMAP-Abruf (Host, Port, TLS, Zugang, Absender-Allowlist, Intervall, Grenzen je Abruf)
- `BELEGAPP_NOTIFY_*` — Webhook-Adresse, Token, Timeout
- `BELEGAPP_WATCH_FOLDER`, `BELEGAPP_WATCH_INTERVAL` — überwachter Ordner
- `BELEGAPP_JOB_TIMEOUT` — Frist für die drei Hintergrundaufträge `mail`, `watch`, `notify`
- `BELEGAPP_JOB_RETENTION` — Aufbewahrungsfenster für abgeschlossene Auftragszeilen

## Offene Fragen

Die verbindlichen Listen stehen in [SPEC](docs/SPEC.md) §19, nicht hier. Beides braucht noch eine Antwort
des Eigentümers:

- **§19.1** — die Konfigurationsfragen zur Jahresregel, die nur der Arbeitgeber beantworten kann
  (Tageszuschuss, bezuschusste Mahlzeiten, Kirchensteuerverfahren, Eigenanteil-Variante, Monatslimit,
  Übergabeweg). U1 beantwortet davon nur die Beschaffungsfrage: Sachbezugswerte und Kirchensteuersätze
  kommen jetzt aus der API, nicht mehr aus einer Liste im Bundle.
- **§19.3** — die Annahmen, die sich die Spezifikation selbst gegeben hat und die bestätigt werden müssen.

Diese Sitzung deckt bereits ab, was ohne Antwort auskam: die Eingangswege (IMAP, überwachter Ordner) und
ihre Anzeige (Liste „Offene Bilder", Webhook) sind umgesetzt und in §19.2 als erledigt vermerkt. Offen
bleiben darin PDF-Anhänge als Eingabe (kein Rasterizer), WebDAV als Eingang, und die Frage, wie sich
eine prüfpflichtige Belegzuordnung überhaupt abbilden soll.

## Verwandte Dokumente

- [`docs/ROADMAP.md`](docs/ROADMAP.md) — genehmigter Rückstand und mögliche Features
- [`docs/SPEC.md`](docs/SPEC.md) — §18 Meilensteine, §19 offene Punkte, §5.2 Warteschlange
- [`docs/FEATURES.md`](docs/FEATURES.md) — Changelog je Version
- [`docs/configuration.md`](docs/configuration.md) — alle `BELEGAPP_*`-Variablen
- [`docs/deployment.md`](docs/deployment.md) — Betrieb, Datenbankgröße