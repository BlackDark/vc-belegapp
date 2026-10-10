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

## Metrics

When `BELEGAPP_METRICS_ADDR` is set, the process exposes `belegapp_http_requests_total`, `belegapp_http_request_duration_seconds`, `belegapp_erkennung_total`, `belegapp_erkennung_dauer_seconds`, `belegapp_pdf_dauer_seconds`, `belegapp_jobs_wartend`, plus the Go and process collectors. The listener has no authentication. Keep it off the public proxy.
