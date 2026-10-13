# Roadmap

What is planned, what was rejected, and ideas under consideration. Implemented work is in [FEATURES.md](FEATURES.md).

## Approved backlog (not started)

Nothing in the approved backlog. The last item — serving the official values (Sachbezugswerte, Kirchensteuer rates) only from the API and dropping `web/src/lib/amtlich.ts` — landed with `GET /werte`; see [FEATURES.md](FEATURES.md).

## Decided against

- **Sharing the Typst Docker stage.** GoReleaser builds `Dockerfile.goreleaser` from a temp context that holds only the binary, so that stage cannot `COPY scripts/install-typst.sh`. Both files keep the pin; `go test ./internal/doccheck` fails when the version in either Dockerfile, `scripts/install-typst.sh`, or `.mise.toml` differs.
- **Custom thin sidebar.** The native shadcn (solid-ui) sidebar stays; no custom navigation just to shrink the main chunk.
- **Splitting `internal/service`.** Large effort and high risk for little user benefit.
- **Skipping unchanged CI jobs.** Path filters could hide real breaks across the embedded web/Go build.
- **Deep-importing Lucide icons and subsetting Inter further.** Little measurable gain.

## Possible features

### Receipt intake by e-mail

Goal: forward or redirect a receipt (photo or PDF) somewhere and have it appear as a Beleg to review. SPEC §1.2 currently excludes PDF receipts, so any option below that takes a PDF also needs PDF input (render the first pages to images before recognition).

| Option | How | Pros | Cons |
| --- | --- | --- | --- |
| ~~1. Scheduled IMAP poll~~ | **Done.** A `mail` job polls a dedicated folder on `BELEGAPP_MAIL_INTERVAL`, stores the image attachments as unassigned Belegbilder in *Offene Bilder*, then flags the message `\Seen` + `$Belegapp` and moves it to `BELEGAPP_MAIL_DONE_FOLDER`. Config via `BELEGAPP_MAIL_*`; see [SPEC](SPEC.md) §8.2.1 and [configuration](configuration.md). | Works with any provider and with forwarding rules; no public endpoint; fits the existing job queue | Credentials stored in the app; polling delay; OAuth-only providers need app passwords; images only, no PDF |
| 2. Inbound-mail webhook | A provider (Mailgun, Postmark, SendGrid Inbound Parse, Cloudflare Email Workers) posts parsed mail to a signed endpoint. | Instant; no mailbox credentials | Needs a public endpoint, a provider account, and signature verification per provider |
| 3. PWA Web Share Target | Register `share_target` in the manifest so "Share → Belegapp" on the phone uploads an image or PDF. | No server-side mail at all; native feel; already in SPEC M5 | Phone only; Android well supported, iOS limited |
| ~~4a. Watched folder~~ | **Done.** A `watch` job polls `BELEGAPP_WATCH_FOLDER` on `BELEGAPP_WATCH_INTERVAL` and stores the JPEG/PNG/WebP files as unassigned Belegbilder in *Offene Bilder*. A file is read only once it is unchanged across two polls; sources are **moved** to `.processed/` (or `.abgelehnt/` for one the content check refuses), never deleted, and a failed move cannot duplicate because the dedupe is an indexed `upload_sha256` lookup rather than an in-memory set. Symlinks are not followed. See [SPEC](SPEC.md) §8.2.3 and [configuration](configuration.md). | Works with scanners, download folders, and phone sync; no credential at all | Needs a mounted volume; useful on a server, less so on the go |
| 4b. WebDAV | The same intake against a Nextcloud/WebDAV path, so a phone can drop a receipt without a sync client on the host. | No volume to mount; reaches the phone directly | Another credential to store; a network round trip per file; WebDAV MOVE support varies by server |
| ~~5. Review notification~~ | **Done.** An outbound webhook (`BELEGAPP_NOTIFY_WEBHOOK_URL`, ntfy-compatible) fires on every ingest and posts a versioned `bild.pruefen` document naming the image, the intake source, and a deep link — no image data, no credential. See [SPEC](SPEC.md) §8.2.2 and [configuration](configuration.md). | Closes the loop for every intake path | Extra outbound configuration; still one credential on the operator's side |

Security notes for all intake paths:

- Sender allowlist (exact addresses, ideally with DKIM/SPF pass) for mail; reject everything else silently. **Done for the IMAP poll** via `BELEGAPP_MAIL_ALLOWED_SENDERS`.
- Size and type limits (JPEG, PNG, WebP only; cap per attachment and per message). **Done**: `BELEGAPP_UPLOAD_MAX_BYTES` per attachment, `BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE` per message, PDF skipped and logged.
- PDFs are untrusted: rasterise in a sandboxed step with page and time limits; never execute embedded content. **Still open** — the poll has no rasteriser.
- Idempotency: dedupe by message ID and attachment SHA-256 so retries and re-forwards do not create duplicates; respect the one-Beleg-per-day rule (conflicts become review items). **Partly done**: the poll marks each message `\Seen` plus the `$Belegapp` keyword and never reprocesses it; SHA-256 deduplication then comes free from the content-addressed blob key. The watched folder asks the indexed `upload_sha256` before every store, so a failed move cannot produce a second copy. Conflicts on a day still land as separate review items.
- Imported Belege are never final until confirmed in the UI, and never touch a locked month without an Änderungsgrund. **Done**: both intakes only ever create unassigned Belegbilder, which the *Offene Bilder* screen hands to the normal review flow.

Suggested next slice: the Web Share Target (option 3) and WebDAV (option 4b), both landing in the same "needs review" state.

The visibility prerequisite is done as well: `GET /belegbilder/offen` and the *Offene Bilder* screen list every uploaded image no Beleg owns, with a jump into the review form, and the IMAP poll drops its attachments there. What is still missing is a flag on the Beleg itself that says "never reviewed".

### Deferred from the specification

From [SPEC.md](SPEC.md) §1.2, §18 (M5), and §19.2, and [NOTES.md](NOTES.md):

- Multi-user, roles, tenants (the data model is kept extensible).
- Automatic delivery to the employer (mail, HR API).
- Offline capture with a sync queue.
- Litestream documentation and a scheduled automatic Datenexport in the app (today: the Kustomize backup CronJob).
- Teilbelege (several slips for one meal) and rule periods within a year.
- Reclaiming Monatsexport blobs after the retention window (SPEC §19.4).
- Maintenance: update 2027 Sachbezugswerte before January 2027, verify Kirchensteuer rates per Bundesland, compare Pauschalsteuer rounding with payroll, build an eval set for recognition, Solid 2.0 migration, watch typst-go-wasm for a true single binary.
