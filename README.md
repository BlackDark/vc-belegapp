# vc-belegapp

Selbst gehostete Einzelnutzer-PWA zum Erfassen täglicher Essenszuschuss-Belege und zum monatlichen PDF-Export. Dieses Repository enthält den Meilenstein **M0 Fundament**: lauffähiges Binary, SQLite, Health-Endpunkte, eingebettetes Frontend-Skelett, Image und CI. Fachliche Erfassung, Anmeldung, Erkennung und PDF folgen in späteren Meilensteinen.

Die Spezifikation steht in [docs/SPEC.md](docs/SPEC.md). Architekturentscheidungen: [docs/adr](docs/adr).

## Voraussetzungen

- Go 1.27.2 (`go` und `toolchain` in `go.mod`), `CGO_ENABLED=0`
- Node 24.21.0 (`.nvmrc`) und pnpm 12.10.1 (`packageManager` in `web/package.json`)
- Docker mit BuildKit für das Image

## Bauen und starten

```bash
pnpm -C web install --frozen-lockfile
pnpm -C web build
CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp

mkdir -p data
BELEGAPP_DATA_DIR="$PWD/data" BELEGAPP_LISTEN_ADDR="127.0.0.1:8080" ./bin/belegapp serve
```

`serve` ist der Default. Weitere Befehle: `migrate`, `healthcheck [--url] [--timeout]`, `version`. `hash-password`, `backup`, `restore` und `verify-audit` sind angelegt und beenden sich mit Exit 1, bis ihr Meilenstein landet.

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

`/healthz` antwortet mit `ok`. `/readyz` ist 200, wenn Datenbank, Migrationen, Blob-Store und `typst --version` in Ordnung sind, sonst 503 mit JSON. Ohne Typst im `PATH` bleibt der Prozess am Leben und `/readyz` meldet den Typst-Check als fehlgeschlagen.

Ungültige Konfiguration beendet den Prozess mit Exit-Code 2.

## Docker

```bash
docker build -t vc-belegapp:dev .
docker volume create belegapp-data
docker run --rm -d --name belegapp \
  --read-only \
  --user 65532:65532 \
  --tmpfs /tmp:rw,nosuid,size=64m \
  -v belegapp-data:/data \
  -p 127.0.0.1:8080:8080 \
  vc-belegapp:dev

curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/
docker inspect -f '{{.Config.User}}' belegapp
```

Das Image basiert auf `gcr.io/distroless/static-debian13:nonroot` (Digest gepinnt), enthält `/belegapp` und ein statisches Typst-0.15.1-Binary, lauscht auf 8080 und legt `/data` mit UID/GID 65532 an. Ein neues benanntes Volume übernimmt diese Rechte. Ein Bind-Mount muss vorher `chown -R 65532:65532` bekommen. Das Root-Dateisystem kann read-only sein; schreibbar sind das Datenvolume und optional `tmpfs` auf `/tmp` (Typst-Cache des Probes liegt unter `/data/cache`).

Compose-Beispiel: [deploy/docker-compose.yml](deploy/docker-compose.yml). Kubernetes kommt mit M4.

CI baut `linux/amd64` und `linux/arm64` ohne Push und scannt mit Trivy. Der Push nach `ghcr.io/blackdark/vc-belegapp` läuft über GoReleaser beim Tag `v*`.

## Konfiguration

Alle Variablen haben das Präfix `BELEGAPP_`. Geheimnisse dürfen statt des Werts als `BELEGAPP_<NAME>_FILE` (Pfad, Inhalt getrimmt) gesetzt werden. Beides gleichzeitig ist ein Fehler. Die vollständige Tabelle steht in [docs/SPEC.md](docs/SPEC.md) Abschnitt 14.

| Variable | Default | Bedeutung |
|---|---|---|
| `BELEGAPP_LISTEN_ADDR` | `:8080` | HTTP-Listener |
| `BELEGAPP_BASE_URL` | leer | Externe URL, Pflicht bei OIDC |
| `BELEGAPP_DATA_DIR` | `/data` | Datenverzeichnis |
| `BELEGAPP_DB_PATH` | `$DATA_DIR/belegapp.db` | SQLite-Datei |
| `BELEGAPP_TZ` | `Europe/Berlin` | Fachliche Zeitzone |
| `BELEGAPP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `BELEGAPP_LOG_FORMAT` | `json` | `json` oder `text` |
| `BELEGAPP_TRUSTED_PROXIES` | leer | CIDR-Liste |
| `BELEGAPP_SECRET_KEY` | auto, in der DB | 32 Byte, base64 |
| `BELEGAPP_COOKIE_SECURE` | `true` | |
| `BELEGAPP_SESSION_IDLE_TIMEOUT` | `720h` | |
| `BELEGAPP_SESSION_ABSOLUTE_TIMEOUT` | `2160h` | |
| `BELEGAPP_AUTH_PASSWORD_HASH` | leer | argon2id-PHC |
| `BELEGAPP_OIDC_*` | aus | Issuer, Client, Allowlist |
| `BELEGAPP_STORAGE_BACKEND` | `fs` | `fs` oder `s3` |
| `BELEGAPP_STORAGE_FS_DIR` | `$DATA_DIR/blobs` | |
| `BELEGAPP_S3_*` | | Pflicht: Bucket und Region bei `s3` |
| `BELEGAPP_LLM_*` | OpenAI-Defaults | siehe Spezifikation |
| `BELEGAPP_TYPST_BIN` | `typst` | im Image `/usr/local/bin/typst` |
| `BELEGAPP_METRICS_ADDR` | leer | z. B. `:9090`, ohne Auth |

M0 startet auch ohne Passwort und ohne OIDC und schreibt dann eine Warnung. Abschnitt 9.1 (Abbruch ohne Login-Methode) gilt ab M1, sonst ließe sich das Image nicht ohne Secrets prüfen. Unvollständiges OIDC oder S3 bleibt ein Startfehler (Exit 2). Fehlt der API-Key bei der OpenAI-Basis-URL, warnt M0 und startet trotzdem; die Erkennung ruft die API erst in einem späteren Meilenstein auf.

Metriken, wenn `BELEGAPP_METRICS_ADDR` gesetzt ist: `belegapp_http_requests_total`, `belegapp_http_request_duration_seconds` plus Go- und Prozesskollektoren. Die fachlichen Zähler aus der Spezifikation kommen mit Erkennung und Export.

## Entwicklung

```bash
make web    # biome, tsc, vitest, vite build
make test   # go test -race
make lint   # gofmt, golangci-lint, biome
make sqlc   # generate, vet, diff gegen eine migrierte Datenbank
make vuln   # govulncheck
```

Queries liegen in `internal/db/queries`, Migrationen in `internal/db/migrations`. Generierter sqlc-Code wird eingecheckt. `web/dist` ist per `//go:embed` im Binary; CI ersetzt es durch den frischen Vite-Build.

## CI und Releases

| Workflow | Auslöser | Inhalt |
|---|---|---|
| `ci.yml` | PR, Push `main` | Jobs `web`, `go`, `docker` (ohne Push, Trivy) |
| `release-please.yml` | Push `main` | Release-PR, Changelog, Tag `vX.Y.Z` |
| `release.yml` | Tag `v*`, manuell | GoReleaser v2: Archive, SBOM, Cosign, GHCR |
| `codeql.yml` | PR, wöchentlich | CodeQL für Go und TypeScript |

GoReleaser signiert Checksums und das Image keyless (Cosign) und hängt Provenance-Attestations an Checksums und Image-Digest. Renovate läuft wöchentlich (`renovate.json`).

Ein Tag, den `release-please` mit `GITHUB_TOKEN` setzt, startet andere Workflows nicht. Damit `release.yml` automatisch läuft, als Secret `RELEASE_PLEASE_TOKEN` einen Fine-grained PAT mit `contents: write` hinterlegen. Ohne das Secret kann der Release-Workflow von Hand gestartet werden.

Ruleset-Vorschlag für `main` (PR, lineare Historie, Checks `web`/`go`/`docker`, kein Force-Push) und für Tags `v*` (kein Löschen, kein Verschieben): [`.github/rulesets`](.github/rulesets). `pdf` wird Pflicht, sobald der Golden-Test existiert. Die Rulesets sind nicht angewendet; beim Tag-Schutz einen Bypass für Maintainer und release-please ergänzen, bevor das Anlegen von Tags gesperrt wird.

## Lizenz

MIT, siehe [LICENSE](LICENSE).
