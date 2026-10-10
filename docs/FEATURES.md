# Features

Inventory of what is implemented on `main`. Each entry names the PR(s) and the first release that contained it. Releases v1.0.0 and v1.0.1 exist on GitHub without a container image; the first image is [v1.0.2](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.2). For behaviour details follow the links into the [specification](SPEC.md).

## M0 Fundament

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#1](https://github.com/BlackDark/vc-belegapp/pull/1).

- Single static Go binary (chi, modernc SQLite, sqlc, goose migrations) with the SolidJS/Vite frontend embedded ([ADR 0001](adr/0001-go-single-binary-sqlite.md)).
- Environment configuration, structured logging, `/healthz` and `/readyz` (SQLite, migrations, blob store, `typst --version`).
- CLI subcommands `serve`, `migrate`, `healthcheck`, `version`; `hash-password` arrived with auth in [#2](https://github.com/BlackDark/vc-belegapp/pull/2).
- Distroless nonroot image (UID 65532) with Typst, initial CI, Renovate, and release-please scaffolding.

## M1 Erfassen

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#2](https://github.com/BlackDark/vc-belegapp/pull/2), hardened in [#8](https://github.com/BlackDark/vc-belegapp/pull/8).

- **Auth:** password (argon2id PHC from env/secret) and OIDC (authorization code + PKCE, allowlist by `sub` or verified e-mail); server-side sessions in SQLite, `__Host-` cookie, CSRF via `http.CrossOriginProtection` ([ADR 0003](adr/0003-auth-passwort-und-oidc.md)).
- **Storage:** `BlobStore` on the filesystem or S3 (minio-go), contract-tested against both ([ADR 0004](adr/0004-storage-fs-und-s3.md)). Content-addressed keys and image deduplication came in [#10](https://github.com/BlackDark/vc-belegapp/pull/10); unreferenced-image reclaim in [#9](https://github.com/BlackDark/vc-belegapp/pull/9).
- **Belege:** one Beleg per Belegtag, up to three images, server-side JPEG normalisation with EXIF stripped, Belegbetrag plus optional korrigierter Betrag with reason, soft delete, versions.
- **Jahresregeln:** per-year employer rules (Tageszuschuss, Mahlzeitarten, Pauschalierung, Gehaltsumwandlung, Bundesland, Eigenanteil-Variante, Monatslimit, eigene Feiertage) with suggestions.
- **Sachbezug:** official Sachbezugswerte shipped in the binary (`internal/rules`) and applied per Mahlzeit.
- **Pauschalsteuer, Soli, KiSt:** 25 % Pauschalsteuer, Solidaritätszuschlag, and Kirchensteuer (simplified-procedure rates by Bundesland) in integer cents, checked against the SPEC §6.5 test vectors. See [calculation](calculation.md).
- Warnings (weekend, holiday, month cap, duplicates, subsidy above the ceiling), holiday calendar per Bundesland, month view.
- Append-only Änderungsprotokoll with a SHA-256 hash chain ([ADR 0005](adr/0005-monatssperre-aenderungsprotokoll.md)); `belegapp verify-audit`.

## M2 Belegerkennung

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#3](https://github.com/BlackDark/vc-belegapp/pull/3).

- Background recognition via a SQLite job queue with retries against any OpenAI-compatible `/chat/completions` endpoint (OpenAI, Gemini OpenAI endpoint, Ollama, vLLM, LM Studio), JSON schema with `json_object` fallback ([ADR 0006](adr/0006-belegerkennung-openai-kompatibel.md)).
- Pre-fills date, merchant, place, amount, and Bezugsort; suggests corrections; never blocks manual capture. "Verbindung testen" in Einstellungen.

## M3 Monatsexport

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#5](https://github.com/BlackDark/vc-belegapp/pull/5), refined in [#9](https://github.com/BlackDark/vc-belegapp/pull/9) and [#10](https://github.com/BlackDark/vc-belegapp/pull/10).

- **Typst PDF/A-2b** monthly PDF (cover, month table, totals, receipt pages, Arbeitnehmererklärung), validated with veraPDF in CI ([ADR 0002](adr/0002-typst-pdf.md)). Draft preview with ENTWURF watermark; optional CSV and ZIP.
- **Monatssperre:** the final export locks the month; later edits need an Änderungsgrund, set the status to `geaendert`, and require export version n+1 with a change list.
- **Audit-Log:** every domain change, including image changes ([#10](https://github.com/BlackDark/vc-belegapp/pull/10)), lands in the hash-chained Änderungsprotokoll.
- **Prüfpunkte:** pass/warning/fail checks in the export dialog and PDF. Cheap checks on the month screen, full image hashing and chain walk at export time ([#9](https://github.com/BlackDark/vc-belegapp/pull/9), [#10](https://github.com/BlackDark/vc-belegapp/pull/10)).
- Retention marker `BELEGAPP_EXPORT_RETENTION_YEARS` (default 10) and CSV/ZIP defaults in Einstellungen ([#10](https://github.com/BlackDark/vc-belegapp/pull/10)).

## M4 Datenexport/-import

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#6](https://github.com/BlackDark/vc-belegapp/pull/6).

- Versioned zip (manifest, SQLite snapshot, referenced blobs, `csv/belege.csv`); import replaces the whole instance after manifest, checksum, schema, and audit-chain checks, taking a safety backup first. Format: [DATENEXPORT.md](DATENEXPORT.md).
- UI in Einstellungen plus the CLI `belegapp backup` and `belegapp restore`; storage backend switch via import. See [backup](backup.md).

## CI/CD and release

- **CI** ([#1](https://github.com/BlackDark/vc-belegapp/pull/1), [#7](https://github.com/BlackDark/vc-belegapp/pull/7), v1.0.0): web and Go builds, tests, sqlc/goose checks, PDF/A validation, CodeQL. The web bundle is built once and all GoReleaser targets are cross-compiled on the runner (`CGO_ENABLED=0`, no QEMU); images only copy binaries.
- **Container smoke test** and **Playwright e2e** of the main flows ([#7](https://github.com/BlackDark/vc-belegapp/pull/7), v1.0.0); viewport-sized screenshots and a sidebar position test ([#17](https://github.com/BlackDark/vc-belegapp/pull/17), v1.1.0).
- **release-please** ([#6](https://github.com/BlackDark/vc-belegapp/pull/6), v1.0.0) with workflow dispatch fixed in [#12](https://github.com/BlackDark/vc-belegapp/pull/12) (v1.0.1); CI runs on branch push so release PRs no longer show zero-job failures ([#19](https://github.com/BlackDark/vc-belegapp/pull/19), v1.1.1).
- **GoReleaser** binaries for linux/darwin amd64/arm64, **GHCR multi-arch image**, **cosign** keyless signatures, **SBOM** (syft), build-provenance attestations ([#6](https://github.com/BlackDark/vc-belegapp/pull/6); first working image in v1.0.2 after [#15](https://github.com/BlackDark/vc-belegapp/pull/15)).
- **Release dry run:** GoReleaser snapshot in CI on every change, sharing the steps with the tag build ([#15](https://github.com/BlackDark/vc-belegapp/pull/15), v1.0.2).
- Docs link check (`internal/doccheck`) and README env-table drift test ([#11](https://github.com/BlackDark/vc-belegapp/pull/11), [#21](https://github.com/BlackDark/vc-belegapp/pull/21)).

## Deployment

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0); first image [v1.0.2](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.2). PRs [#1](https://github.com/BlackDark/vc-belegapp/pull/1), [#6](https://github.com/BlackDark/vc-belegapp/pull/6), [#7](https://github.com/BlackDark/vc-belegapp/pull/7).

- Docker (read-only root FS, no capabilities, no-new-privileges, UID/GID 65532), Docker Compose example with `_FILE` secrets, Kustomize manifests including a backup CronJob and NetworkPolicy, standalone binary (needs `typst` in `PATH`). Optional Prometheus metrics. See [deployment](deployment.md).

## UI

- SolidJS with TanStack Query, PWA via vite-plugin-pwa (manifest, icons, update prompt) ([#2](https://github.com/BlackDark/vc-belegapp/pull/2), [#6](https://github.com/BlackDark/vc-belegapp/pull/6), v1.0.0).
- Redesign on **shadcn/solid-ui** (Kobalte, Tailwind 4): collapsible sidebar on desktop, bottom navigation on phones, dashboard on Heute, month table with status badges ([#17](https://github.com/BlackDark/vc-belegapp/pull/17), v1.1.0).
- **Dark theme by default**, applied before first paint, with a **Hell / Dunkel / System** toggle ([#17](https://github.com/BlackDark/vc-belegapp/pull/17), v1.1.0).
- Whole-receipt Heute preview ([#13](https://github.com/BlackDark/vc-belegapp/pull/13), v1.0.1); clearer error messages, screen-reader labels on Prüfpunkte, shared amount formatting, theme unit tests ([#22](https://github.com/BlackDark/vc-belegapp/pull/22), unreleased).

## Documentation

- README with screenshots ([#11](https://github.com/BlackDark/vc-belegapp/pull/11), v1.0.0), split into focused pages under `docs/` ([#21](https://github.com/BlackDark/vc-belegapp/pull/21), unreleased). Index: [docs/README.md](README.md).
