# Tech-Stack-Empfehlung Beleg-App (Stand 08.10.2026)

Versionen am 08.10.2026 geprüft über npm-Registry, proxy.golang.org und GitHub-Releases.

## 1. Empfehlung auf einen Blick

| Schicht | Wahl | Version (08.10.2026) | Begründung |
|---|---|---|---|
| Sprache Backend | **Go** | 1.27.2 (1.26.x noch unterstützt) | Statisches Binary ohne CGO, kleines Image, schneller Start, gutes Ops-Tooling (goreleaser) |
| HTTP | `net/http` + **chi v5** | v5.3.2 (08/2026) | Kompatibel mit der Standardbibliothek, Middleware ohne Framework-Lock-in. CSRF-Schutz über `http.CrossOriginProtection` (seit Go 1.25) |
| DB | **SQLite** über **modernc.org/sqlite** (reines Go) | v1.60.1 (09/2026) | Keine CGO-Abhängigkeit, damit Cross-Compile und `distroless/static` möglich. WAL-Modus |
| Queries | **sqlc** (Codegen aus SQL) | v1.31.1 (04/2026) | Typsicher, kein ORM; der erzeugte `database/sql`-Code funktioniert mit modernc |
| Migrationen | **goose v3** (embed FS) | v3.28.0 (09/2026) | Migrationen werden ins Binary eingebettet und beim Start ausgeführt |
| Dateien | Interface `BlobStore`: **lokales FS** (`os.Root`) **oder S3** (minio-go v7) | minio-go v7.3.0 (08/2026) | Läuft mit jedem S3-kompatiblen Speicher (AWS, Hetzner, Garage, SeaweedFS, MinIO) |
| PDF | **Typst-CLI** (Template + JSON-Daten), PDF/A-2b | Typst 0.15.1 (07/2026) | Sehr gutes Satzbild, Template bleibt außerhalb des Codes änderbar, Archivformat. Details in Abschnitt 5 |
| Belegerkennung | OpenAI-kompatible **Chat Completions** mit Bild und `response_format: json_schema`, Go-SDK **openai-go v3** | v3.74.0 (10/2026) | Basis-URL frei einstellbar, damit gehen OpenAI, Gemini (OpenAI-Endpoint), Ollama, vLLM und LM Studio |
| Auth | Einzelnutzer: **argon2id-Passwort-Hash + Session-Cookie** (in SQLite), optional **OIDC** (coreos/go-oidc v3) | go-oidc v3.21.0 (09/2026), x/crypto v0.57.0 | Einfach und sicher. OIDC für Authelia, Authentik, Pocket-ID usw. |
| Frontend | **SolidJS 1.9** + **Vite 8** + **@solidjs/router** | solid-js 1.9.17 (07.10.2026), vite 8.3.4 | Klein und schnell. Solid 2.0 ist erst RC, und Kobalte setzt `solid-js ^1.9.8` voraus |
| UI-Primitives | **Kobalte** (zugänglich, headless) | @kobalte/core 0.13.14 (09/2026) | Basis von solid-ui und shadcn-solid |
| Komponenten | **solid-ui** (shadcn-Port, Copy-Paste, Kobalte + corvu) | Repo aktiv, 1,5k Sterne, letzter Push 02/2026 | Gleiche Optik und gleiches Vorgehen wie shadcn/ui, der Code liegt im eigenen Repo, dadurch geringes Abhängigkeitsrisiko |
| Styling | **Tailwind CSS v4** (`@tailwindcss/vite`) | 4.3.3 (07/2026) | CSS-first-Konfiguration über `@theme`, schnell |
| Daten/Forms | @tanstack/solid-query 5, @tanstack/solid-form 1 | 5.104.1 / 1.33.5 | Caching, Upload-Status, Validierung |
| Icons/Toasts | lucide-solid, solid-sonner | 1.53.0 / 0.3.2 | |
| PWA | **vite-plugin-pwa 2.0** (Workbox) | 2.0.0 (03.10.2026, Peer: Vite ^8) | Manifest, installierbar, App-Shell-Cache |
| Lint/Format | Biome 2 (TS), golangci-lint v2 (Go) | 2.5.15 / v2.14.0 | |
| Tests | `go test -race`, Vitest 5, Playwright (E2E, optional) | vitest 5.0.3, playwright 1.64.0 | |
| Paketmanager | pnpm | 12.10.1 | |
| Release | **GoReleaser v2** (`dockers_v2`, multi-arch) | v2.18.2 (09/2026) | Binaries, GHCR-Images, SBOM, Signatur |
| Base-Image | `gcr.io/distroless/static-debian13:nonroot` | – | UID 65532, keine Shell |
| Updates | Renovate | 44.x | |

**Kurz begründet:** Go plus eingebettetes Solid-SPA ergibt **ein einzelnes statisches Binary** (`go:embed` für `dist/`) und damit ein Image von etwa 20–30 MB (dazu Typst, siehe 5). Es läuft ohne Root, mit read-only Root-FS und einem `/data`-Volume, unter Docker und Kubernetes gleich. SQLite und das Dateisystem bzw. S3 decken einen Einzelnutzer mit vielen Jahren Belegen leicht ab.

## 2. Architektur

```
[Handy-PWA (Solid)] --HTTPS--> [Reverse Proxy (Traefik/Caddy/Ingress)] --> [belegapp (Go, :8080, UID 65532)]
                                                                        ├─ SQLite  /data/belegapp.db (WAL)
                                                                        ├─ BlobStore: /data/blobs | S3-Bucket
                                                                        ├─ Typst-CLI (PDF-Export)
                                                                        └─ Vision-LLM (OpenAI-kompatibel, extern oder lokal)
```
- **API**: JSON-REST unter `/api/v1`, das SPA unter `/`, Fallback auf `index.html`. Optional: OpenAPI mit **huma v2** (v2.39.1) und daraus TS-Typen über `openapi-typescript` (7.13.0). Das ist nett, aber kein Muss.
- **Hintergrundjobs** (OCR, PDF): eine Tabelle `jobs` in SQLite und ein Worker als Goroutine. Kein Redis.
- **Domänenmodell (Entwurf)**: `year_rules` (Jahr, SBW je Mahlzeitart, Tageszuschuss, Monatslimit, Pauschalierung, Variante exakt/konservativ, KiSt-Satz), `days` (Datum, Mahlzeitart, Status Arbeitstag/Homeoffice/Urlaub/Krank/Dienstreise), `receipts` (Tag, Blob-Key, SHA-256, Händler, Betrag, korrigierter Betrag und Grund, OCR-Rohdaten, Quelle ki/manuell), `exports` (Monat, Hash, gesperrt), `audit_log`.
- **Berechnung** als reine Go-Funktionen mit Tabellentests. Die Formeln stehen in `steuer.md` Abschnitt 3.3/3.4. Beträge immer als **Integer-Cent**, nie als Float.
- **Backup/Export**: Ein „Alles exportieren“ erzeugt ein ZIP mit SQLite-Snapshot (`VACUUM INTO`), allen Bildern, CSV und PDFs. Optional **Litestream** (v0.5.17) als Sidecar für laufende Replikation nach S3.

## 3. Frontend im Detail

### 3.1 Bewertung der UI-Bibliotheken
| Option | Basis | Styling | Status | Urteil |
|---|---|---|---|---|
| **solid-ui** (stefan-karger) | Kobalte + corvu | Tailwind, shadcn-Optik, Copy-Paste/CLI | 1,5k ★, aktiv, Push 02/2026 | **Empfohlen.** ⚠️ Die Doku zeigt noch eine Tailwind-v3-Konfiguration. Unter v4 werden die Tokens nach `@theme` portiert, das ist überschaubar und beim Setup zu prüfen |
| **shadcn-solid** (hngngn) | Kobalte (+ Ark-Bezüge) | Tailwind oder UnoCSS, nutzt `cva`-Beta | 768 ★, Push 06/2026 | Gleichwertige Alternative, weniger verbreitet |
| **Kobalte** direkt | – | headless | 0.13.14 (09/2026), noch 0.x | Ist ohnehin die Grundlage. Eigene Komponenten nur bei Bedarf |
| **Ark UI (Solid)** | Zag.js-State-Machines | headless | 5.39.3 (10/2026), sehr aktiv (Chakra-Team) | Ausweichoption, falls Kobalte stagniert. Breite Komponentenauswahl |
| **Park UI** | Ark UI | **Panda CSS** | Push 04/2026 | Verworfen, weil kein Tailwind (Panda statt Tailwind) |
| **SolidStart** (SSR) | – | – | 2.0.6 | Verworfen. SSR bringt für eine Einzelnutzer-PWA nichts und braucht eine Node-Runtime statt eines einzelnen Go-Binarys |
| **Solid 2.0** | – | – | 2.0.0-rc.14 | Noch nicht. Kobalte verlangt 1.9. Upgrade, sobald 2.0 stabil ist und das Ökosystem nachgezogen hat |

### 3.2 PWA und Kamera
- **Kamera**: `<input type="file" accept="image/*" capture="environment">` ist die robusteste Lösung auf iOS und Android, öffnet direkt die Rückkamera und erlaubt auch Galerie-Uploads. Eine Live-Vorschau mit `getUserMedia` ist optional und nicht nötig.
- **Clientseitig verkleinern und komprimieren** vor dem Upload: `createImageBitmap(file, {imageOrientation:"from-image"})`, dann Canvas/OffscreenCanvas auf ca. 2000 px lange Kante und als JPEG mit q≈0,85 speichern. Das spart Upload-Volumen, korrigiert die EXIF-Rotation und umgeht HEIC. ⚠️ iOS Safari liefert über ein File-Input normalerweise JPEG. Ob zusätzlich das Original aufbewahrt wird, ist offen (Speicher gegen Beweiswert).
- **vite-plugin-pwa**: Manifest, Icons (`@vite-pwa/assets-generator`), Precache der App-Shell, `registerType: "prompt"` für Updates. **API-Aufrufe nicht cachen.** Mehr Offline-Fähigkeit ist laut Anforderung nicht nötig.
- Nice-to-have: **Web Share Target** (Android), damit Fotos aus der Galerie „an Belegapp teilen“ gehen.
- Seiten: Heute/Erfassen (großer Kamera-Button), Monatskalender mit Status pro Tag, Belegdetail mit Bild-Zoom und KI-Vorschlag, Export, Einstellungen/Jahresregeln.

## 4. Backend im Detail
- **Go 1.26/1.27**, `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`.
- **Config über Env** (12-Factor), z. B. mit `caarlos0/env` v11: `BELEGAPP_DATA_DIR`, `BELEGAPP_STORAGE=fs|s3`, `BELEGAPP_S3_*`, `BELEGAPP_LLM_BASE_URL`, `BELEGAPP_LLM_MODEL`, `BELEGAPP_LLM_API_KEY`, `BELEGAPP_OIDC_*`, `BELEGAPP_ADMIN_PASSWORD_HASH`.
- **SQLite-Pragmas**: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`. Eine Schreibverbindung, mehrere Leseverbindungen.
- **Upload-Härtung**: Größenlimit (z. B. 15 MB), MIME per Content-Sniffing (`gabriel-vasile/mimetype` v1.4.15), nur JPEG/PNG/WebP/PDF. Serverseitig das Bild neu codieren und ein Thumbnail erzeugen (`golang.org/x/image/draw`). Dateiname = SHA-256, damit sind Dubletten sofort erkennbar.
- **Logging**: `log/slog` (JSON). **Health**: `/healthz`, `/readyz`. Optional Prometheus-Metriken.

## 5. PDF-Erzeugung
| Option | Pro | Contra | Urteil |
|---|---|---|---|
| **Typst-CLI** (0.15.1, statische musl-Builds für amd64/arm64) | Sehr gutes Layout, Tabellen, Bilder, **PDF/A-2b**, Template als `.typ`-Datei, Daten als JSON | Zweites Binary im Image (ca. 40 MB); das Release-Binary braucht `typst` im PATH | **Empfohlen** |
| typst-go-wasm (v0.4.0, 26 ★, seit 05/2026) | Typst in-process über wazero, echtes Single-Binary | Sehr jung. ⚠️ `Files` ist `map[string]string`, das Einbetten **binärer Bilder** ist unklar | Beobachten. Später austauschbar hinter einem `PDFRenderer`-Interface |
| maroto v2 (v2.4.3, 10/2026) | Reines Go, Single-Binary, Bilder und Tabellen | Layout per Code, weniger schön, kein PDF/A | Fallback, falls Typst stört |
| go-pdf/fpdf (v0.9.0, 2023) | – | Kaum noch gepflegt | Verworfen |
| JS (pdf-lib) im Browser | – | Logik doppelt, große Bilder im Handy-Browser | Verworfen |

Umsetzung: Ein Interface `PDFRenderer` mit der Implementierung `typstcli` (`os/exec`, Timeout über Context, Arbeitsverzeichnis in `/tmp` als emptyDir). Template und Fonts (z. B. Inter, OFL) werden ins Binary eingebettet und zur Laufzeit in das Temp-Verzeichnis geschrieben. Aufruf mit `--font-path` und `--pdf-standard a-2b`. Bilder vorher auf ca. 1600 px und JPEG q80 verkleinern, damit das PDF klein bleibt. Der Wrapper `Dadido3/go-typst` (35 ★) ist nicht nötig, `os/exec` reicht.

## 6. Belegerkennung (KI)
- **Interface** `ReceiptExtractor` mit der Implementierung `openaicompat` (Chat Completions, Bild als `data:image/jpeg;base64,...`).
- Bewusst **Chat Completions statt Responses API**, weil Ollama, vLLM und LM Studio die OpenAI-Kompatibilität darüber anbieten.
- **Structured Output**: `response_format: {type: "json_schema", json_schema: {strict: true, schema: ...}}`. Ollama unterstützt das lokal mit Bildern (Docs: https://docs.ollama.com/api/openai-compatibility, https://docs.ollama.com/capabilities/structured-outputs). ⚠️ Laut Issue #12362 ignoriert Ollama *Cloud* das Schema. Fallback: `json_object` und serverseitige Validierung gegen das Schema.
- Schema (Entwurf): `datum (YYYY-MM-DD), uhrzeit, haendler {name, adresse}, gesamtbetrag_cent, waehrung, positionen[{bezeichnung, betrag_cent, mwst_satz, kategorie: lebensmittel|getraenk|alkohol|tabak|pfand|nonfood|sonstiges}], zahlungsart, konfidenz (0..1), hinweise`.
- Die App berechnet daraus einen **Vorschlag für den korrigierten Betrag** (Summe der Positionen `lebensmittel` und `getraenk`). Der Nutzer bestätigt immer, die KI füllt nur vor.
- Modelle sind frei konfigurierbar, z. B. ein kleines und günstiges Vision-Modell von OpenAI oder Gemini, lokal Qwen-VL-Varianten (z. B. `qwen3-vl:8b` in Ollama). ⚠️ Konkrete Modellnamen und Preise ändern sich schnell, darum nicht fest einbauen.
- Datenschutz: Beim Cloud-Modell landen Kassenbons bei einem Dritten (wenig personenbezogen, eventuell Kartenendziffern). Ein Schalter „KI aus“ erlaubt rein manuelle Erfassung.

## 7. Authentifizierung (Einzelnutzer)
- **Standard**: Passwort mit argon2id-Hash (`golang.org/x/crypto/argon2`), gesetzt über Env oder Ersteinrichtung. Session-Token zufällig (32 Byte) und in SQLite gespeichert (z. B. **scs v2** mit SQLite-Store oder eigene Tabelle). Cookie mit `HttpOnly; Secure; SameSite=Lax`, 30 Tage gleitend. Login-Rate-Limit.
- **CSRF**: `SameSite=Lax` plus `http.CrossOriginProtection` (Go ≥ 1.25) für alle nicht-GET-Requests.
- **Optional**: OIDC (go-oidc v3 + x/oauth2, PKCE) mit Allowlist auf `sub`/E-Mail. Alternativ ein „Trusted Header“-Modus hinter Forward-Auth (Authelia, oauth2-proxy), nur mit konfigurierter Proxy-IP.
- Kein Mehrbenutzer-Rollenmodell (YAGNI). Das Datenmodell bekommt trotzdem eine `user_id`, damit man später erweitern kann.

## 8. Betrieb (Docker/Kubernetes)
- **Image**: Multi-Stage oder GoReleaser `dockers_v2` auf Basis von `gcr.io/distroless/static-debian13:nonroot`, `USER 65532:65532`, Typst als statisches musl-Binary nach `/usr/local/bin/typst`. Labels (OCI), Healthcheck über das Binary selbst (`belegapp healthcheck`), weil das Image kein curl hat.
- **Docker Compose**: `read_only: true`, `tmpfs: /tmp`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, Volume `/data`.
- **Kubernetes** (Helm-Chart oder Kustomize im Repo): `replicas: 1`, `strategy: Recreate` (SQLite!), PVC RWO (**kein NFS** wegen SQLite-Locking), `securityContext` mit `runAsNonRoot`, `runAsUser: 65532`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile: RuntimeDefault`. Dazu `emptyDir` für `/tmp`, Secrets für API-Key, Passwort-Hash und S3, Ingress mit TLS (cert-manager).
- **S3-Modus**: Bilder im Bucket, nur SQLite auf dem PVC. Mit Litestream lässt sich auch die DB nach S3 replizieren, das Volume ist dann fast zustandslos.

## 9. CI/CD (GitHub Actions, Repo `BlackDark/vc-belegapp`)
- **`ci.yml`** (PR und Push auf main):
  - Frontend: `pnpm install --frozen-lockfile`, `biome ci`, `tsc --noEmit`, `vitest run`, `vite build`
  - Backend: `sqlc diff` bzw. `sqlc vet`, `golangci-lint run`, `go test -race ./...`, `govulncheck` (x/vuln v1.8.0)
  - Optional: Playwright-Smoke-Test gegen das gebaute Binary
  - Docker-Build ohne Push (Buildx, amd64 und arm64)
- **`release.yml`** (Tag `v*`): Frontend bauen, dann **GoReleaser v2**: Binaries für linux/amd64, arm64 (optional darwin), Archive und Checksums, `dockers_v2` multi-arch nach `ghcr.io/blackdark/vc-belegapp:{version,latest}`, **SBOM** (syft), **cosign keyless**-Signatur, `actions/attest-build-provenance`. Rechte: `contents: write`, `packages: write`, `id-token: write`, `attestations: write`.
- **Versionierung**: Conventional Commits und **release-please**, das Tags und Changelog erzeugt (optional, alternativ manuelle Tags).
- **Renovate** (`renovate.json`): `config:best-practices` (pinnt Actions und Docker-Digests), Gruppen für Go-Module, npm, Actions und Docker. Automerge für Patch und Minor von Dev-Dependencies nach grüner CI. `postUpdateOptions: ["gomodTidy"]`.
- **Repo-Hygiene**: Branch-Protection, Dependabot-Security-Alerts, CodeQL (Go und JS/TS), `SECURITY.md`, `LICENSE`.

## 10. Verworfene Alternativen
| Alternative | Warum nicht |
|---|---|
| **Bun + Hono** (bun 1.4.2, hono 4.13.13) | `bun build --compile` und `bun:sqlite` würden auch ein Single-Binary ergeben, mit einer Sprache für Frontend und Backend. Dagegen sprechen: großes Binary (~60–100 MB), Bun-Runtime-Eigenheiten, schwächeres Release-Tooling für multi-arch und distroless, PDF/Bild-Ökosystem nicht besser. Die zweitbeste Option. |
| **Rust (axum + sqlx)** | Ebenfalls exzellentes Single-Binary, aber deutlich langsamere Entwicklung und Compile-Zeiten. Für ein CRUD-Einzelnutzer-Tool überdimensioniert. |
| **PocketBase** (v0.40.5) | Liefert Auth, SQLite, S3-Dateien und Admin-UI fertig mit. Aber: eigenes Datenmodell und Admin-Oberfläche, API noch 0.x mit Breaking Changes, Berechnungs- und Exportlogik müsste trotzdem in Go-Hooks. Weniger Kontrolle über ein sauberes, getestetes Domänenmodell. Brauchbar für einen schnellen Prototyp. |
| **PostgreSQL** | Zusätzlicher Dienst, Backup-Aufwand. Bei einem Nutzer bringt es keinen Vorteil. |
| **ncruces/go-sqlite3** (WASM-SQLite) | Gut gepflegt (v0.35.6), aber modernc ist verbreiteter, und sqlc- und goose-Beispiele gibt es meist dafür. Austauschbar. |
| **mattn/go-sqlite3** | Braucht CGO und verhindert damit `static` distroless und einfaches Cross-Compile. |
| **gocloud.dev/blob** | Elegante FS/S3-Abstraktion, zieht aber das schwere AWS-SDK nach. Ein eigenes kleines Interface plus minio-go reicht. |
| **echo v5 / huma** | Gut, aber chi plus Standardbibliothek genügt. Huma nur, falls OpenAPI-Codegen gewünscht ist. |
| **SolidStart / SSR**, **Solid 2.0 RC**, **Park UI** | Siehe 3.1. |

## 11. Risiken und offene Punkte
- ⚠️ solid-ui und Tailwind v4: Die Theme-Konfiguration muss migriert werden. Beim Setup kurz prüfen, ob es inzwischen eine v4-Vorlage gibt.
- ⚠️ Kobalte ist noch 0.x. Weil die Komponenten per Copy-Paste im Repo liegen, wäre ein späterer Wechsel auf Ark UI machbar.
- ⚠️ Solid 2.0: Migration einplanen, sobald 2.0 stabil ist und Kobalte bzw. solid-ui es unterstützen.
- ⚠️ Typst als Zusatz-Binary: Für reine Binary-Releases dokumentieren, dass `typst` nötig ist, oder später typst-go-wasm bzw. maroto als Fallback hinter dem `PDFRenderer`-Interface anbieten.
- Fragen an den Nutzer: Domain und Reverse-Proxy (Traefik/Caddy/Ingress)? Soll S3 ab Tag 1 genutzt werden? Ist OIDC-Provider vorhanden? Welcher KI-Anbieter und welches Modell als Standard?
