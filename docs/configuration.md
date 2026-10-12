# Configuration

Every variable uses the prefix `BELEGAPP_`. A secret may be set as `BELEGAPP_<NAME>_FILE` (path; contents trimmed) instead of the value. Setting both is an error. [internal/config/readme_test.go](../internal/config/readme_test.go) fails `go test` when a variable in code is missing from the table below, or when a default in code is not the backticked default cell.

<!-- config-env-start -->

| Variable | Default | Meaning |
| --- | --- | --- |
| `BELEGAPP_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BELEGAPP_BASE_URL` | empty | Public origin. Required for OIDC. Also the CSRF trusted origin |
| `BELEGAPP_DATA_DIR` | `/data` | Data directory |
| `BELEGAPP_DB_PATH` | `${DATA_DIR}/belegapp.db` | SQLite file |
| `BELEGAPP_TZ` | `Europe/Berlin` | Business time zone. Zone data is embedded |
| `BELEGAPP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `BELEGAPP_LOG_FORMAT` | `json` | `json` or `text` |
| `BELEGAPP_TRUSTED_PROXIES` | empty | Comma-separated CIDRs. `X-Forwarded-For` is honoured only from these |
| `BELEGAPP_SECRET_KEY`, `BELEGAPP_SECRET_KEY_FILE` | empty | 32 bytes, base64. Empty generates a key and stores it in the database. HMAC for the OIDC cookie |
| `BELEGAPP_COOKIE_SECURE` | `true` | `false` only for local HTTP. The cookie then drops the `__Host-` prefix |
| `BELEGAPP_SESSION_IDLE_TIMEOUT` | `720h` | Idle session lifetime |
| `BELEGAPP_SESSION_ABSOLUTE_TIMEOUT` | `2160h` | Absolute session lifetime. Must be at least the idle timeout |
| `BELEGAPP_AUTH_PASSWORD_HASH`, `BELEGAPP_AUTH_PASSWORD_HASH_FILE` | empty | argon2id PHC from `belegapp hash-password`. Empty disables password login |
| `BELEGAPP_OIDC_ISSUER_URL` | empty | Issuer. Empty disables OIDC |
| `BELEGAPP_OIDC_CLIENT_ID` | empty | Client id. Required when an issuer is set |
| `BELEGAPP_OIDC_CLIENT_SECRET`, `BELEGAPP_OIDC_CLIENT_SECRET_FILE` | empty | Empty means a public client (PKCE only) |
| `BELEGAPP_OIDC_SCOPES` | `openid profile email` | Space-separated scopes |
| `BELEGAPP_OIDC_ALLOWED_SUBJECTS` | empty | Comma-separated `sub` values |
| `BELEGAPP_OIDC_ALLOWED_EMAILS` | empty | Comma-separated emails, or `*` for every verified email at the issuer. A match also requires `email_verified`. At least one of the two allowlists must be set |
| `BELEGAPP_OIDC_BUTTON_LABEL` | `Mit SSO anmelden` | Label of the OIDC button |
| `BELEGAPP_OIDC_RP_LOGOUT` | `false` | After local logout, redirect to the provider end-session endpoint when it exists |
| `BELEGAPP_STORAGE_BACKEND` | `fs` | `fs` or `s3` |
| `BELEGAPP_STORAGE_FS_DIR` | `${DATA_DIR}/blobs` | Filesystem blob directory |
| `BELEGAPP_S3_ENDPOINT` | empty | Host, for example `minio:9000`. Required for `s3` |
| `BELEGAPP_S3_REGION` | `us-east-1` | Region. Required for `s3` |
| `BELEGAPP_S3_BUCKET` | empty | Bucket. Required for `s3` |
| `BELEGAPP_S3_PREFIX` | `belegapp/` | Key prefix |
| `BELEGAPP_S3_ACCESS_KEY_ID`, `BELEGAPP_S3_ACCESS_KEY_ID_FILE` | empty | Access key |
| `BELEGAPP_S3_SECRET_ACCESS_KEY`, `BELEGAPP_S3_SECRET_ACCESS_KEY_FILE` | empty | Secret key |
| `BELEGAPP_S3_USE_TLS` | `true` | TLS to the endpoint |
| `BELEGAPP_S3_FORCE_PATH_STYLE` | `true` | Path-style addressing |
| `BELEGAPP_S3_SSE` | `false` | SSE-S3 |
| `BELEGAPP_LLM_ENABLED` | `true` | Global switch, in addition to the in-app Belegerkennung toggle |
| `BELEGAPP_LLM_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible base URL. A trailing slash is optional |
| `BELEGAPP_LLM_API_KEY`, `BELEGAPP_LLM_API_KEY_FILE` | empty | Bearer token. Empty is valid for Ollama. Empty on the OpenAI host leaves recognition off |
| `BELEGAPP_LLM_MODEL` | `gpt-5-mini` | Model id |
| `BELEGAPP_LLM_RESPONSE_FORMAT` | `auto` | `json_schema`, `json_object`, or `auto` (schema first, then one fallback) |
| `BELEGAPP_LLM_REASONING_EFFORT` | empty | Sent only when set, for example `low` |
| `BELEGAPP_LLM_TIMEOUT` | `60s` | Recognition request timeout |
| `BELEGAPP_LLM_MAX_IMAGE_PX` | `1600` | Long edge of the JPEG sent to the model |
| `BELEGAPP_JOB_WORKERS` | `2` | Parallel recognition jobs |
| `BELEGAPP_UPLOAD_MAX_BYTES` | `15728640` | Image upload limit, 15 MiB |
| `BELEGAPP_IMPORT_MAX_BYTES` | `4294967296` | Datenimport limit, 4 GiB |
| `BELEGAPP_UNASSIGNED_IMAGE_TTL` | `24h` | Delete unassigned Belegbilder, then blobs nothing still references |
| `BELEGAPP_EXPORT_RETENTION_YEARS` | `10` | Retention clock, 1–100. `aufbewahrung_bis` is the end of the export's calendar year plus N years, in `BELEGAPP_TZ`. Nothing is deleted |
| `BELEGAPP_TYPST_BIN` | `typst` | Typst binary. The image uses `/usr/local/bin/typst` |
| `BELEGAPP_PDF_TIMEOUT` | `120s` | Monatsexport compile timeout |
| `BELEGAPP_METRICS_ADDR` | empty | Prometheus listen address, for example `:9090`. No authentication |
| `BELEGAPP_MAIL_ENABLED` | `false` | Global switch for the IMAP intake. Off means no ticker and no mail job, whatever else is set |
| `BELEGAPP_MAIL_HOST` | empty | IMAP host, for example `imap.example.de`. Required when the intake is on |
| `BELEGAPP_MAIL_PORT` | `993` | IMAP port. `993` is implicit TLS, `143` with `BELEGAPP_MAIL_STARTTLS` |
| `BELEGAPP_MAIL_USERNAME` | empty | Login name. Required when the intake is on |
| `BELEGAPP_MAIL_PASSWORD`, `BELEGAPP_MAIL_PASSWORD_FILE` | empty | App password of a dedicated mail account. Never logged |
| `BELEGAPP_MAIL_TLS` | `true` | Implicit TLS. Exactly one of `BELEGAPP_MAIL_TLS` and `BELEGAPP_MAIL_STARTTLS` has to be true; plaintext IMAP is refused |
| `BELEGAPP_MAIL_STARTTLS` | `false` | Upgrade a plain connection with STARTTLS. Then use port `143` |
| `BELEGAPP_MAIL_INBOX` | `INBOX` | Polled folder, ideally one only receipts are forwarded to |
| `BELEGAPP_MAIL_DONE_FOLDER` | empty | Folder an ingested message is moved to. Empty only sets `\Seen` plus the `$Belegapp` marker. Needs a server with `MOVE` |
| `BELEGAPP_MAIL_ALLOWED_SENDERS` | empty | Comma-separated sender addresses, exact match. Empty admits every sender |
| `BELEGAPP_MAIL_INTERVAL` | `5m` | Poll period |
| `BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL` | `50` | Messages one poll reads. A larger backlog is drained over the next polls |
| `BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE` | `10` | Pictures stored per message. The rest is skipped and logged |
| `BELEGAPP_NOTIFY_WEBHOOK_URL` | empty | Absolute http(s) address that is told when an image waits for review. Empty is the switch: no job is created and nothing is sent |
| `BELEGAPP_NOTIFY_WEBHOOK_TOKEN`, `BELEGAPP_NOTIFY_WEBHOOK_TOKEN_FILE` | empty | Sent as `Authorization: Bearer …` when set. Never logged, never part of the payload |
| `BELEGAPP_NOTIFY_TIMEOUT` | `10s` | Deadline for one webhook delivery, connect to answer |

<!-- config-env-end -->

`serve` requires `BELEGAPP_AUTH_PASSWORD_HASH` or `BELEGAPP_OIDC_ISSUER_URL`. OIDC also needs a client id and at least one allowlist (`sub` or verified email). Incomplete OIDC or S3 configuration is a startup error. `migrate` does not need authentication. Discovery retries in the background; until it succeeds, `/api/v1/auth/config` reports OIDC as off and the process still serves password login. Redirect URI, allowlists, and the reverse proxy are in [deployment.md](deployment.md).

## Ollama

Leave the key empty. `auto` falls back to `json_object` when the server rejects `response_format`.

```bash
BELEGAPP_LLM_BASE_URL=http://127.0.0.1:11434/v1
BELEGAPP_LLM_API_KEY=
BELEGAPP_LLM_MODEL=qwen3-vl:8b
BELEGAPP_LLM_RESPONSE_FORMAT=auto
```

On the Compose network the host name is `ollama` (`http://ollama:11434/v1`). The request sends only the normalised JPEG, EXIF removed, rescaled to `BELEGAPP_LLM_MAX_IMAGE_PX`.

## Receipt intake by e-mail

Forward receipts to a dedicated mailbox or folder and the poll puts them into *Offene Bilder*. It is off unless `BELEGAPP_MAIL_ENABLED` is `true`; without it no ticker runs and no mail job is created, so the other variables can stay in place.

```bash
BELEGAPP_MAIL_ENABLED=true
BELEGAPP_MAIL_HOST=imap.example.de
BELEGAPP_MAIL_USERNAME=belege@example.de
BELEGAPP_MAIL_PASSWORD_FILE=/run/secrets/imap_password
BELEGAPP_MAIL_INBOX=Belege
BELEGAPP_MAIL_DONE_FOLDER=Belege/erledigt
BELEGAPP_MAIL_ALLOWED_SENDERS=kasse@example.de,lieferando@example.de
BELEGAPP_MAIL_INTERVAL=5m
```

Use a dedicated account with an app password. **Mail is never deleted.** An ingested message is flagged `\Seen` plus the keyword `$Belegapp`, and moved to `BELEGAPP_MAIL_DONE_FOLDER` when that is set. A server without the `MOVE` extension is reported in the log instead: the go-imap client would otherwise fall back to `COPY` + `\Deleted` + `EXPUNGE`, which this application never does.

Only JPEG, PNG, and WebP attachments are stored (SPEC §1.2). PDF and every other type are skipped and logged; there is no rasteriser. An attachment over `BELEGAPP_UPLOAD_MAX_BYTES` is skipped, and at most `BELEGAPP_MAIL_MAX_ATTACHMENTS_PER_MESSAGE` attachments are kept per message. The per-attachment cap is the same number as for a manual upload.

A single attachment that cannot be stored — a corrupt file, a picture over the pixel limit — is logged and skipped, and the rest of the message and the rest of the poll carry on. Such an attachment is decided by its own bytes and would be refused on every retry, so the message is flagged anyway; re-reading it would only add another copy of the images it did carry. A failure of the blob store or the database ends the poll instead, and the messages already stored are flagged before the error is reported, so the job retry does not duplicate them.

One poll reads at most `BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL` messages; the rest waits for the next one.

`BELEGAPP_MAIL_ALLOWED_SENDERS` compares the full address, case-insensitively. Empty admits every sender. A non-matching sender is rejected silently — one log line, nothing shown to the user — and its message is left untouched in the folder: the poll reads message bodies with `BODY.PEEK[]`, which sets no flag, so even `\Seen` stays where the operator left it.

The two flags serve one purpose and are written together: `\Seen` is what the operator sees in their own client, and `$Belegapp` is what the poll searches on. The keyword is authoritative because a phone mail client marks a receipt read when it previews the picture, and that must not hide it from the intake. A server that keeps no keywords answers `BAD` to the probe, and the poll then searches on `\Seen` alone. A server that searches on the keyword but refuses to store it is downgraded to `\Seen` for the rest of the process, and the poll reports the failed store instead of claiming a success that did not happen.

Transport: `BELEGAPP_MAIL_TLS` (implicit TLS, the default) or `BELEGAPP_MAIL_STARTTLS` on port `143`. Exactly one of the two must be set — there is no plaintext mode, because the password would cross the network in the clear.

## Review notifications

An outbound webhook tells you that an image is waiting in *Offene Bilder*. It fires on **every** ingest — a manual phone upload, the IMAP poll, and the watched folder alike — because all three end up in the same list. The recipient filters on the payload's `quelle` field if a watched folder should be quieter than a photo.

```bash
BELEGAPP_NOTIFY_WEBHOOK_URL=https://ntfy.sh/belegapp-pruefen
BELEGAPP_NOTIFY_WEBHOOK_TOKEN_FILE=/run/secrets/ntfy_token
BELEGAPP_NOTIFY_TIMEOUT=10s
```

There is no UI setting. The address comes from the environment, so no web request can choose where this application sends data. Loopback and private addresses are allowed: an ntfy relay or a small script on the home network is a supported setup.

Without `BELEGAPP_NOTIFY_WEBHOOK_URL` nothing happens at all — no queue row is written and no payload is built. `BELEGAPP_NOTIFY_WEBHOOK_TOKEN` alone changes nothing.

The startup log prints a **redacted** address, never the raw one: scheme, host and port stay, every path segment becomes `***`, and a query string or fragment becomes a single `***`. That is deliberate — for this receiver class the credential often *is* the address (`https://ntfy.sh/<topic>`, `…/bot<TOKEN>/sendMessage`, `…?token=…`), and a startup line ends up on stdout, in the container log and in journald, where it outlives the process. `https://ntfy.sh/belegapp-pruefen` therefore logs as `https://ntfy.sh/***`, and whether a bearer token is configured is reported as a boolean, never as a value. The raw URL reaches no log call and no stored job error.

The token, when set, is sent as `Authorization: Bearer …`. A token in the URL (`https://user:pass@host/…`) is refused at startup for that reason.

### Payload

```json
{
  "version": 1,
  "event": "bild.pruefen",
  "bild_id": "01hq3k7z8m9n0p1q2r3s4t5u6v",
  "erstellt_am": "2026-10-08T13:00:00Z",
  "quelle": "manuell",
  "abschluss_url": "/belege/neu?bild=01hq3k7z8m9n0p1q2r3s4t5u6v"
}
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version. A receiver that does not know it ignores the document |
| `event` | Always `bild.pruefen` today. One event name per shape, so a filter can name it |
| `bild_id` | The unassigned Belegbild. It is the id `GET /belegbilder/{id}` already answers for |
| `erstellt_am` | When the image was stored, RFC 3339 in UTC |
| `quelle` | `manuell`, `mail`, `watch`, or `unknown` — how the picture arrived. An intake that names no source, or one this version does not know, arrives as `unknown`, never as `manuell` |
| `abschluss_url` | Deep link into the review form, a path inside this application. Prefix it with your own base URL |

The document carries **no** image bytes, no blob key, no SHA-256, no mailbox host or folder, and no credential. A receiver learns that a picture arrived and gets an id it can look up itself. `abschluss_url` is a path, not an absolute link, so the payload says nothing about where this instance lives.

### Retries

The delivery is a `notify` queue job. A `5xx`, a `429`, a `408`, a timeout, or a refused connection is retried by the job queue with its backoff; a `4xx` is a decision by the receiver and is never repeated. The HTTP client itself never retries, so a recipient sees one message per job attempt. The response body is ignored — any `2xx` counts as delivered.

## Metrics

When `BELEGAPP_METRICS_ADDR` is set, the process exposes `belegapp_http_requests_total`, `belegapp_http_request_duration_seconds`, `belegapp_erkennung_total`, `belegapp_erkennung_dauer_seconds`, `belegapp_pdf_dauer_seconds`, `belegapp_jobs_wartend`, plus the Go and process collectors. The listener has no authentication. Keep it off the public proxy.
