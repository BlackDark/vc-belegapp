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

- **Auth:** password (argon2id PHC from env/secret) and OIDC (authorization code + PKCE, allowlist by `sub` or verified e-mail, with whole-label wildcards such as `*@example.de` and `*`); server-side sessions in SQLite, `__Host-` cookie, CSRF via `http.CrossOriginProtection` ([ADR 0003](adr/0003-auth-password-and-oidc.md)).
- **Storage:** `BlobStore` on the filesystem or S3 (minio-go), contract-tested against both ([ADR 0004](adr/0004-storage-fs-and-s3.md)). Content-addressed keys and image deduplication came in [#10](https://github.com/BlackDark/vc-belegapp/pull/10); unreferenced-image reclaim in [#9](https://github.com/BlackDark/vc-belegapp/pull/9).
- **Belege:** one Beleg per Belegtag, up to three images, server-side JPEG normalisation with EXIF stripped, Belegbetrag plus optional korrigierter Betrag with reason, soft delete, versions.
- **Offene Bilder:** `GET /belegbilder/offen` (status filter, limit/offset) and the *Offene Bilder* screen list every uploaded image no Beleg owns — thumbnail, upload time, recognition-status badge — each linking into the review form. The list carries no blob key and no recognition payload.
- **IMAP-Eingang:** a `mail` job polls a dedicated folder every `BELEGAPP_MAIL_INTERVAL` and stores the image attachments (JPEG/PNG/WebP) as unassigned Belegbilder, which is where the *Offene Bilder* screen picks them up. `BELEGAPP_MAIL_ALLOWED_SENDERS` rejects anything else silently; `BELEGAPP_UPLOAD_MAX_BYTES` caps each attachment and `BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE` caps each message. A processed message is flagged `\Seen` plus the `$Belegapp` keyword and moved to `BELEGAPP_MAIL_DONE_FOLDER`; **mail is never deleted**, and a server without `MOVE` is reported instead of falling back to `COPY + \Deleted + EXPUNGE`. Off by default (`BELEGAPP_MAIL_ENABLED`), implicit TLS or STARTTLS only. See [SPEC](SPEC.md) §8.2.1.
- **Überwachter Ordner:** a `watch` job polls a local folder every `BELEGAPP_WATCH_INTERVAL` and stores the JPEG/PNG/WebP files in it as unassigned Belegbilder, which is where the *Offene Bilder* screen picks them up — the intake for a scanner, a download directory, or a phone sync client. A file is read only after it is unchanged across two consecutive polls (size and mtime), so a sync client writing in place is never read half-copied; dotfiles, partial suffixes (`.tmp`, `.part`, `.crdownload`, `.partial`), subdirectories, and symlinks are skipped without being opened, and a symlink is never resolved. **Sources are moved, never deleted**: an ingested file goes to `.processed/`, one this application will never store (a `.jpg` that is not an image, one over `BELEGAPP_UPLOAD_MAX_BYTES`) to `.abgelehnt/`, both with the reason in the log. The move is deliberately after the store, and a failed move cannot duplicate: the next poll hashes the bytes and looks up the indexed `belegbilder.upload_sha256`, so the dedupe lives in the database and survives a restart. Off unless `BELEGAPP_WATCH_FOLDER` is set. See [SPEC](SPEC.md) §8.2.3 and [configuration](configuration.md).
- **Prüfbenachrichtigung:** an outbound HTTP webhook (ntfy-compatible) fires on every ingest — manual upload, IMAP poll, watched folder alike — and announces that an image is waiting in *Offene Bilder*. Each image gets one `notify` job that posts a versioned JSON document (`bild.pruefen` with `bild_id`, `erstellt_am`, `quelle`, and a deep link); the payload carries no image bytes, no blob key, no hash, and no credential. The HTTP client never retries — the job queue owns the backoff, so a 5xx or timeout repeats while a 4xx does not. Off unless `BELEGAPP_NOTIFY_WEBHOOK_URL` is set; `BELEGAPP_NOTIFY_WEBHOOK_TOKEN` is sent as a bearer token and never logged. See [SPEC](SPEC.md) §8.2.2 and [configuration](configuration.md).
- **Jahresregeln:** per-year employer rules (Tageszuschuss, Mahlzeitarten, Pauschalierung, Gehaltsumwandlung, Bundesland, Eigenanteil-Variante, Monatslimit, eigene Feiertage) with suggestions.
- **Sachbezug:** official Sachbezugswerte shipped in the binary (`internal/rules`) and applied per Mahlzeit.
- **Pauschalsteuer, Soli, KiSt:** 25 % Pauschalsteuer, Solidaritätszuschlag, and Kirchensteuer (simplified-procedure rates by Bundesland) in integer cents, checked against the SPEC §6.5 test vectors. See [calculation](calculation.md).
- Warnings (weekend, holiday, month cap, duplicates, subsidy above the ceiling), holiday calendar per Bundesland, month view.
- Append-only Änderungsprotokoll with a SHA-256 hash chain ([ADR 0005](adr/0005-month-lock-audit-log.md)); `belegapp verify-audit`.
- **Queue history pruning:** finished job rows (`fertig`, `fehler`) older than `BELEGAPP_JOB_RETENTION` (default `168h`) are deleted hourly. The status filter is the rule, not the age: a job in `wartend` or `laeuft` still owes work and is never deleted, whatever its age. Four of the five job types are created by a timer rather than by a person, so without this the `jobs` table would grow by roughly a million rows a year at the default watched-folder interval. The prune is not part of the queue — own goroutine, own ticker, 1000-row batches — so it cannot delay a claim, and it runs `PRAGMA incremental_vacuum` on databases created with `auto_vacuum = INCREMENTAL` so the file actually shrinks. The same pass ends with `ANALYZE`, which is what makes SQLite choose the indexes behind the blob-key lookup of every image delete and the open-job lookup of every mail and folder tick; it carries the same 15-second ceiling as a delete batch and a failure to run it is a log line, not a failed pass. An existing database is converted once, on the first start after the upgrade: one `VACUUM`, announced as a log line, and `BELEGAPP_DB_AUTO_VACUUM_MIGRATE=false` skips it. See [SPEC](SPEC.md) §5.2 and [configuration](configuration.md).
- **Indexes that match the questions the queue actually asks:** `belegbilder(blob_key)`, `belegbilder(thumb_blob_key)` for "does anything still name this blob", which is asked four times per image delete, and the partial `jobs(typ, payload) WHERE status IN ('wartend','laeuft')` for "is a poll of mine already queued", which is asked on every mail tick and every watched-folder tick. Each is asserted with an `EXPLAIN QUERY PLAN` test that also drops the index again and checks the plan really was measuring something. `belegbilder_beleg` was left in place: it is a prefix of the unique `belegbilder_beleg_seite`, but dropping it leaves every plan it serves exactly as it was. See [SPEC](SPEC.md) §5.2.

## M2 Belegerkennung

First release: [v1.0.0](https://github.com/BlackDark/vc-belegapp/releases/tag/v1.0.0). PR [#3](https://github.com/BlackDark/vc-belegapp/pull/3).

- Background recognition via a SQLite job queue with retries against any OpenAI-compatible `/chat/completions` endpoint (OpenAI, Gemini OpenAI endpoint, Ollama, vLLM, LM Studio), JSON schema with `json_object` fallback ([ADR 0006](adr/0006-receipt-recognition-openai-compatible.md)).
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

- Versioned zip (manifest, SQLite snapshot, referenced blobs, `csv/belege.csv`); import replaces the whole instance after manifest, checksum, schema, and audit-chain checks, taking a safety backup first. Format: [data-export.md](data-export.md).
- UI in Einstellungen plus the CLI `belegapp backup` and `belegapp restore`; storage backend switch via import. See [backup](backup.md).

## CI/CD and release

- **CI** ([#1](https://github.com/BlackDark/vc-belegapp/pull/1), [#7](https://github.com/BlackDark/vc-belegapp/pull/7), v1.0.0): web and Go builds, tests, sqlc/goose checks, PDF/A validation, CodeQL. The web bundle is built once and all GoReleaser targets are cross-compiled on the runner (`CGO_ENABLED=0`, no QEMU); images only copy binaries.
- **Container smoke test** and **Playwright e2e** of the main flows ([#7](https://github.com/BlackDark/vc-belegapp/pull/7), v1.0.0); viewport-sized screenshots and a sidebar position test ([#17](https://github.com/BlackDark/vc-belegapp/pull/17), v1.1.0).
- **release-please** ([#6](https://github.com/BlackDark/vc-belegapp/pull/6), v1.0.0) with workflow dispatch fixed in [#12](https://github.com/BlackDark/vc-belegapp/pull/12) (v1.0.1); CI runs on branch push so release PRs no longer show zero-job failures ([#19](https://github.com/BlackDark/vc-belegapp/pull/19), v1.1.1).
- **GHCR multi-arch image**, **cosign** keyless signature by digest, **SBOM and build-provenance attestations** on the image ([#6](https://github.com/BlackDark/vc-belegapp/pull/6); first working image in v1.0.2 after [#15](https://github.com/BlackDark/vc-belegapp/pull/15)). The image is the only release artifact: no binary tarballs, no checksums, no Syft documents.
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
- **Official values from the API only:** `GET /werte` serves the Sachbezugswerte, the simplified Kirchensteuer rates per Bundesland, and the German meal and purchase-place labels straight from `internal/rules`. The app shell loads them once, so the frontend keeps no copy of its own — the meal codes in the Jahresregel editor come from the same table (unreleased).

## Documentation

- README with screenshots ([#11](https://github.com/BlackDark/vc-belegapp/pull/11), v1.0.0), split into focused pages under `docs/` ([#21](https://github.com/BlackDark/vc-belegapp/pull/21), unreleased). Index: [docs/README.md](README.md).
