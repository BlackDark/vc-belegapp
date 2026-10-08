# vc-belegapp

Self-hosted single-user PWA for capturing daily meal-allowance receipts (Belege) and exporting a monthly PDF. This repository contains **M0 foundation**, **M1 capture**, and **M2 Belegerkennung**: sign-in (password and OIDC), receipts with images, year rules, calculation, warnings, the audit log, the mobile UI, and recognition through an OpenAI-compatible chat-completions endpoint. PDF export (M3) is stubbed and returns 501.

The specification is [docs/SPEC.md](docs/SPEC.md). Architecture decisions: [docs/adr](docs/adr).

## Requirements

- Go 1.27.2 (`go` and `toolchain` in `go.mod`), `CGO_ENABLED=0`
- Node 24.21.0 (`.nvmrc`) and pnpm 12.10.1 (`packageManager` in `web/package.json`)
- Docker with BuildKit for the image

## Build and run

```bash
pnpm -C web install --frozen-lockfile
pnpm -C web build
CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp

mkdir -p data
BELEGAPP_DATA_DIR="$PWD/data" BELEGAPP_LISTEN_ADDR="127.0.0.1:8080" ./bin/belegapp serve
```

`serve` is the default. Other commands: `migrate`, `healthcheck [--url] [--timeout]`, `version`, `hash-password` (argon2id PHC on stdout), `verify-audit` (hash chain, exit 1 on a break). `backup` and `restore` stay for a later milestone.

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

`/healthz` answers `ok`. `/readyz` is 200 when the database, migrations, blob store, and `typst --version` are fine, otherwise 503 with JSON. Without Typst on `PATH` the process stays up and `/readyz` reports the Typst check as failed.

Invalid configuration exits with code 2.

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

The image is based on `gcr.io/distroless/static-debian13:nonroot` (digest pinned), contains `/belegapp` and a static Typst 0.15.1 binary, listens on 8080, and creates `/data` with UID/GID 65532. A new named volume inherits those permissions. A bind mount must be `chown -R 65532:65532` beforehand. The root filesystem can be read-only; writable paths are the data volume and an optional `tmpfs` on `/tmp` (the Typst cache for the probe lives under `/data/cache`).

Compose example: [deploy/docker-compose.yml](deploy/docker-compose.yml). Kubernetes arrives with M4.

CI builds `linux/amd64` and `linux/arm64` without pushing and scans with Trivy. The push to `ghcr.io/blackdark/vc-belegapp` runs through GoReleaser on a `v*` tag.

## Configuration

Every variable uses the prefix `BELEGAPP_`. Secrets may be set as `BELEGAPP_<NAME>_FILE` (a path; the file contents are trimmed) instead of the value. Setting both is an error. The full table is in [docs/SPEC.md](docs/SPEC.md) section 14.

| Variable | Default | Meaning |
|---|---|---|
| `BELEGAPP_LISTEN_ADDR` | `:8080` | HTTP listener |
| `BELEGAPP_BASE_URL` | empty | External URL, required for OIDC |
| `BELEGAPP_DATA_DIR` | `/data` | Data directory |
| `BELEGAPP_DB_PATH` | `$DATA_DIR/belegapp.db` | SQLite file |
| `BELEGAPP_TZ` | `Europe/Berlin` | Business time zone |
| `BELEGAPP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `BELEGAPP_LOG_FORMAT` | `json` | `json` or `text` |
| `BELEGAPP_TRUSTED_PROXIES` | empty | CIDR list |
| `BELEGAPP_SECRET_KEY` | auto, stored in the DB | 32 bytes, base64 |
| `BELEGAPP_COOKIE_SECURE` | `true` | |
| `BELEGAPP_SESSION_IDLE_TIMEOUT` | `720h` | |
| `BELEGAPP_SESSION_ABSOLUTE_TIMEOUT` | `2160h` | |
| `BELEGAPP_AUTH_PASSWORD_HASH` | empty | argon2id PHC |
| `BELEGAPP_OIDC_*` | off | Issuer, client, allowlist |
| `BELEGAPP_STORAGE_BACKEND` | `fs` | `fs` or `s3` |
| `BELEGAPP_STORAGE_FS_DIR` | `$DATA_DIR/blobs` | |
| `BELEGAPP_S3_*` | | Required for `s3`: bucket, region, and endpoint |
| `BELEGAPP_LLM_ENABLED` | `true` | In addition to `einstellungen.erkennung_aktiv` |
| `BELEGAPP_LLM_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible; a trailing slash is optional |
| `BELEGAPP_LLM_API_KEY` | empty | Bearer token; leave empty for Ollama |
| `BELEGAPP_LLM_MODEL` | `gpt-5-mini` | |
| `BELEGAPP_LLM_RESPONSE_FORMAT` | `auto` | `json_schema`, `json_object`, or `auto` |
| `BELEGAPP_LLM_REASONING_EFFORT` | empty | Sent only when set |
| `BELEGAPP_LLM_TIMEOUT` | `60s` | |
| `BELEGAPP_LLM_MAX_IMAGE_PX` | `1600` | Long edge of the JPEG sent to the model |
| `BELEGAPP_JOB_WORKERS` | `2` | Parallel recognition jobs |
| `BELEGAPP_TYPST_BIN` | `typst` | `/usr/local/bin/typst` in the image |
| `BELEGAPP_METRICS_ADDR` | empty | For example `:9090`, no auth |

`serve` exits with code 2 when neither `BELEGAPP_AUTH_PASSWORD_HASH` nor `BELEGAPP_OIDC_ISSUER` is set. `migrate` does not need authentication. Incomplete OIDC or S3 configuration is still a startup error (exit 2). A missing API key on the OpenAI base URL logs a warning and the process still starts; uploads stay at status `keine` and are entered manually.

### Ollama

Switching the endpoint is environment-only. Leave the key empty. `auto` falls back to `json_object` when the server rejects `response_format`.

```bash
BELEGAPP_LLM_BASE_URL=http://127.0.0.1:11434/v1
BELEGAPP_LLM_API_KEY=
BELEGAPP_LLM_MODEL=qwen3-vl:8b
BELEGAPP_LLM_RESPONSE_FORMAT=auto
```

On the Compose network the host name is `ollama` (`http://ollama:11434/v1`). The request sends only the normalised JPEG with EXIF removed, rescaled to `BELEGAPP_LLM_MAX_IMAGE_PX`.

Metrics, when `BELEGAPP_METRICS_ADDR` is set: `belegapp_http_requests_total`, `belegapp_http_request_duration_seconds`, `belegapp_erkennung_total{ergebnis}`, `belegapp_erkennung_dauer_seconds`, `belegapp_jobs_wartend`, plus the Go and process collectors. `belegapp_pdf_dauer_seconds` arrives with export.

## Development

```bash
make web    # biome, tsc, vitest, vite build
make test   # go test -race
make lint   # gofmt, golangci-lint, biome
make sqlc   # generate, vet, diff against a migrated database
make vuln   # govulncheck
```

Queries live in `internal/db/queries`, migrations in `internal/db/migrations`. Generated sqlc code is committed. `web/dist` is embedded with `//go:embed`; CI replaces it with the fresh Vite build.

## CI and releases

| Workflow | Trigger | Contents |
|---|---|---|
| `ci.yml` | PR, push to `main` | Jobs `web`, `go`, `docker` (no push, Trivy), `e2e` (Playwright against the fake LLM) |
| `release-please.yml` | Push to `main` | Release PR, changelog, tag `vX.Y.Z` |
| `release.yml` | Tag `v*`, manual | GoReleaser v2: archives, SBOM, Cosign, GHCR |
| `codeql.yml` | PR, weekly | CodeQL for Go and TypeScript |

GoReleaser signs checksums and the image keyless (Cosign) and attaches provenance attestations to the checksums and the image digest. Renovate runs weekly (`renovate.json`).

A tag that `release-please` creates with `GITHUB_TOKEN` does not start other workflows. To run `release.yml` automatically, store a fine-grained PAT with `contents: write` as the secret `RELEASE_PLEASE_TOKEN`. Without that secret the release workflow can be started by hand.

Suggested ruleset for `main` (PR, linear history, checks `web`/`go`/`docker`, no force-push) and for tags `v*` (no delete, no move): [`.github/rulesets`](.github/rulesets). `pdf` becomes required once the golden test exists. The rulesets are not applied; add a bypass for maintainers and release-please before locking tag creation.

## License

MIT, see [LICENSE](LICENSE).
