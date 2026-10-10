# Roadmap

What is planned, what was rejected, and ideas under consideration. Implemented work is in [FEATURES.md](FEATURES.md).

## Approved backlog (not started)

| Item | Benefit | Effort | Risk |
| --- | --- | --- | --- |
| Serve the official values (Sachbezugswerte, Kirchensteuer rates) only from the API and drop `web/src/lib/amtlich.ts` | One source of truth with `internal/rules`; no frontend/backend drift when 2027 values land | Medium | Medium |

## Decided against

- **Sharing the Typst Docker stage.** GoReleaser builds `Dockerfile.goreleaser` from a temp context that holds only the binary, so that stage cannot `COPY scripts/install-typst.sh`. Both files keep the pin; `go test ./internal/doccheck` fails when the version in either Dockerfile, `scripts/install-typst.sh`, or `.mise.toml` differs.
- **Custom thin sidebar.** The native shadcn (solid-ui) sidebar stays; no custom navigation just to shrink the main chunk.
- **Splitting `internal/service`.** Large effort and high risk for little user benefit.
- **Skipping unchanged CI jobs.** Path filters could hide real breaks across the embedded web/Go build.
- **Deep-importing Lucide icons and subsetting Inter further.** Little measurable gain.

## Possible features

### Receipt intake by e-mail

Goal: forward or redirect a receipt (photo or PDF) somewhere and have it appear as a Beleg to review. SPEC §1.2 currently excludes PDF and e-mail receipts, so any option below also needs PDF input (render the first pages to images before recognition).

| Option | How | Pros | Cons |
| --- | --- | --- | --- |
| 1. Scheduled IMAP poll | A job polls a dedicated mailbox, folder, or label (e.g. `Belege`) on an interval. Extract image/PDF attachments, dedupe by SHA-256, run Belegerkennung, create Belege marked for review, then move or flag the message. Config via env, e.g. `BELEGAPP_MAIL_IMAP_URL`, `_USER`, `_PASSWORD_FILE` (app password), `_FOLDER`, `_INTERVAL`. | Works with any provider and with forwarding rules; no public endpoint; fits the existing job queue | Credentials stored in the app; polling delay; OAuth-only providers need app passwords |
| 2. Inbound-mail webhook | A provider (Mailgun, Postmark, SendGrid Inbound Parse, Cloudflare Email Workers) posts parsed mail to a signed endpoint. | Instant; no mailbox credentials | Needs a public endpoint, a provider account, and signature verification per provider |
| 3. PWA Web Share Target | Register `share_target` in the manifest so "Share → Belegapp" on the phone uploads an image or PDF. | No server-side mail at all; native feel; already in SPEC M5 | Phone only; Android well supported, iOS limited |
| 4. Watched folder / WebDAV | Import files that appear in a folder (or a Nextcloud/WebDAV path), e.g. from a scanner or sync client. | Simple; works with scanners | Another credential or volume; less useful on the go |
| 5. Review notification | Notify (e-mail, ntfy, webhook) when an imported Beleg needs review. | Closes the loop for options 1–4 | Extra outbound configuration |

Security notes for all intake paths:

- Sender allowlist (exact addresses, ideally with DKIM/SPF pass) for mail; reject everything else silently.
- Size and type limits (JPEG, PNG, HEIC, PDF only; cap per attachment and per message).
- PDFs are untrusted: rasterise in a sandboxed step with page and time limits; never execute embedded content.
- Idempotency: dedupe by message ID and attachment SHA-256 so retries and re-forwards do not create duplicates; respect the one-Beleg-per-day rule (conflicts become review items).
- Imported Belege are never final until confirmed in the UI, and never touch a locked month without an Änderungsgrund.

Suggested first slice: IMAP poll (option 1) plus the Web Share Target (option 3), both landing in a "needs review" state.

### Deferred from the specification

From [SPEC.md](SPEC.md) §1.2, §18 (M5), and §19.2, and [NOTES.md](NOTES.md):

- Multi-user, roles, tenants (the data model is kept extensible).
- Automatic delivery to the employer (mail, HR API).
- Offline capture with a sync queue.
- Litestream documentation and a scheduled automatic Datenexport in the app (today: the Kustomize backup CronJob).
- Teilbelege (several slips for one meal) and rule periods within a year.
- Reclaiming Monatsexport blobs after the retention window (SPEC §19.4).
- Maintenance: update 2027 Sachbezugswerte before January 2027, verify Kirchensteuer rates per Bundesland, compare Pauschalsteuer rounding with payroll, build an eval set for recognition, Solid 2.0 migration, watch typst-go-wasm for a true single binary.
