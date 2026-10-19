# Deployment

The process runs as UID/GID 65532, with a read-only root filesystem, no capabilities, and no new privileges. Writable paths are the data volume and the temporary directory — a tmpfs on `/tmp` in the examples, which is RAM; [Writable space](#writable-space) says how big it has to be. A named volume inherits ownership from the image. A bind mount needs `chown -R 65532:65532` first. `serve` exits 2 until a password, OIDC, or trusted-header authentication is configured; the password is a row in the database and is written with `set-password`, see [configuration.md](configuration.md#the-instance-password). Variables: [configuration.md](configuration.md).

Image tags have no `v` prefix. Git tag `v1.1.1` is the image `ghcr.io/blackdark/vc-belegapp:1.1.1`, plus `1.1`, `1`, and `latest`.

## Docker

`/healthz` returns `ok`. `/readyz` is 200 when SQLite, migrations, the blob store, and `typst --version` are fine, otherwise 503. The image is `gcr.io/distroless/static-debian13:nonroot` plus a static Typst 0.15.1 binary, so the healthcheck is `belegapp healthcheck` (no curl).

```bash
docker volume create belegapp-data

printf '%s\n' 'a-long-password' | docker run --rm -i \
  --user 65532:65532 \
  -v belegapp-data:/data \
  ghcr.io/blackdark/vc-belegapp:1.1.1 set-password

docker run -d --name belegapp \
  --read-only \
  --user 65532:65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --tmpfs /tmp:rw,nosuid,size=64m \
  -v belegapp-data:/data \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/blackdark/vc-belegapp:1.1.1
```

The first command is the setup step, and it is a command rather than an environment variable on purpose: the password is an argon2id hash in the database on that volume, so it survives a container replacement, needs no secret file and no `docker run` flag, and is replaced with `docker exec -it belegapp set-password` whenever the operator wants to. It applies the schema itself and needs no running server.

`docker build -t vc-belegapp:dev .` compiles the web bundle and the Go binary inside the image. CI and releases do not use that Dockerfile. They copy a host-built `linux/$TARGETARCH/belegapp` with [Dockerfile.goreleaser](../Dockerfile.goreleaser) (`make docker-prebuilt` does the same locally).

## Docker Compose

[deploy/docker-compose.yml](../deploy/docker-compose.yml) and [deploy/.env.example](../deploy/.env.example). Copy the example to `deploy/.env` and point the secret files at an LLM API key — and, if OIDC is used, at an OIDC client secret. Paths in the example are relative to the compose file. `${VAR:?}` makes Compose refuse to start while a required value is empty. The example is read-only, UID 65532, capabilities dropped, 1 CPU / 512 MiB. It publishes `127.0.0.1:8080` and sets `BELEGAPP_TRUSTED_PROXIES` so a proxy on the Docker bridge can pass the client address. The password is set once against the same volume:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml run --rm belegapp set-password
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

The OIDC lines in the example are commented out: uncomment them to sign in through an identity provider as well. Set `OIDC_SECRET_FILE` in `deploy/.env` for a confidential client and leave it unset for a public one (PKCE only). The forward-auth lines below them are commented out for the same reason; see [Trusted headers](#trusted-headers-forward-auth).

## Kubernetes

[deploy/k8s](../deploy/k8s) is Kustomize: one replica, `Recreate` (SQLite), UID 65532, read-only root, a PVC at `/data`, an `emptyDir` at `/tmp`, a Service, an Ingress, and a backup CronJob. The Ingress example is nginx with cert-manager and `proxy-body-size: 4g`, which matches `BELEGAPP_IMPORT_MAX_BYTES`. The ConfigMap sets `BELEGAPP_BASE_URL` and `BELEGAPP_TRUSTED_PROXIES` (`10.0.0.0/8`). [deploy/k8s/secret.yaml](../deploy/k8s/secret.yaml) holds no password: set it once against the same volume with `kubectl exec -it deploy/belegapp -- /belegapp set-password`. `networkpolicy.yaml` is not part of the default kustomization, because the IdP, LLM, and S3 hosts differ per install.

```bash
kubectl kustomize deploy/k8s
kubectl apply -k deploy/k8s
```

A ReadWriteOnce volume often cannot be mounted by the CronJob while the Deployment holds it. Use a storage class that allows a second mount on the same node, or run the backup on the host against the data directory. See [backup.md](backup.md).

## Watched folder

`BELEGAPP_WATCH_FOLDER` points the intake at a local folder. It has to be **readable and writable** by the process: the poll *moves* each ingested file into `<folder>/.processed/`, which a read-only mount refuses. Nothing is ever deleted (see [configuration](configuration.md)), so the folder is also the operator's archive.

**Docker Compose.** Bind-mount the folder and create it first with the right owner, exactly like `./data`:

```yaml
volumes:
  - "./data:/data"
  - "./watch:/watch" # mkdir -p watch && chown 65532:65532 watch
environment:
  BELEGAPP_WATCH_FOLDER: /watch
```

The process runs read-only with a read-only root filesystem, but that does not affect a bind mount: `/watch` is writable as long as the host directory is owned by UID 65532.

**Kubernetes.** Mount a volume at the folder and point the variable at it. A `subPath` keeps the app's two subfolders (`.processed`, `.abgelehnt`) on the same volume as the data:

```yaml
volumeMounts:
  - name: watch
    mountPath: /watch
  - name: data
    mountPath: /data
volumes:
  - name: watch
    persistentVolumeClaim:
      claimName: belegapp-watch
  - name: data
    persistentVolumeClaim:
      claimName: belegapp-data
```

Without a `subPath` the mount point is the volume root, which is fine for a dedicated volume and wrong for a shared one — it would hide whatever else is on it. With `subPath: watch` under the data PVC, `/watch` is a directory inside `/data`; pick one or the other, not both. A `ReadWriteOnce` watch PVC is the simplest choice: one pod, one node.

A PVC that is not mounted yet makes the folder unreadable, and the poll then fails the job rather than silently doing nothing — that is the intended signal, not a crash.

## Database size

Everything lives in one SQLite file under `/data`. Size it for the receipts and the images, not for the queue: `BELEGAPP_JOB_RETENTION` (default `168h`) prunes finished job rows hourly, so a watched folder polling every 30 s adds about 20 000 rows a week rather than a million a year. See [configuration](configuration.md#the-jobs-table-and-the-size-of-the-database) for what is deleted and what is not.

Two caveats an operator should know before sizing a volume:

- **An existing database is rewritten once, on the first start after the upgrade.** `auto_vacuum = INCREMENTAL` is set when a database is *created*, and SQLite ignores the pragma on a file that already has tables. Such a database keeps deleting its rows and keeps its file size, and `PRAGMA incremental_vacuum` on it is a no-op. The process closes that gap itself: at startup it reads the mode, and only when it is not `INCREMENTAL` does it set the pragma and rewrite the file. That is one `VACUUM` — the database is unavailable while it runs and it needs transient free disk roughly the size of the file — and it happens once; every later start finds `INCREMENTAL` and stays quiet. Size that first start as a maintenance window. A failure is a warning, not a stop, so a read-only volume or a full disk does not take the instance down; the next start retries. `BELEGAPP_DB_AUTO_VACUUM_MIGRATE=false` turns it off entirely, and `belegapp verify-audit` never converts: that command reads, and starting a full rewrite is not its job.

  With the conversion switched off, or to compact at a chosen time instead, do it by hand with the process stopped:

  ```bash
  sqlite3 /data/belegapp.db 'PRAGMA auto_vacuum=INCREMENTAL; VACUUM;'
  ```
- **Deleted rows never grow the file, but the file also does not shrink on a healthy database without the reclaim.** On a database with `auto_vacuum` the hourly prune runs `PRAGMA incremental_vacuum` (4 MiB per pass), which does shrink it. Skipping that (for example by giving the data volume far more space than needed) costs nothing but disk.

## Writable space

The data directory (`/data`) and the temporary directory (`/tmp`, or whatever `TMPDIR` names) are the only writable paths. The root filesystem is read-only, so `/tmp` is a tmpfs in the examples — and a tmpfs is RAM.

Everything bigger than one request passes through the temporary directory:

| File | Written by | Size |
| --- | --- | --- |
| `belegapp-import-*.zip` | a Datenimport upload, staged before it is validated | up to `BELEGAPP_IMPORT_MAX_BYTES`, 4 GiB by default |
| `belegapp-fetch-*.zip` | that archive read back from the store when the import is confirmed | the same |
| `belegapp-safety-*.zip` | the safety copy taken before an import replaces the dataset | an export |
| `belegapp-member-*` | the database and CSV members extracted from an import | the database |
| `belegapp-snap-*/` | the SQLite snapshot inside an export — the CLI `backup` included — and inside the safety copy | the database |
| `belegapp-export-*.zip` | the archive of a **Datenexport**, the in-app one. `belegapp backup --out` streams straight to its destination and never writes the zip here | an export |
| `belegapp-pdf-*` | the Typst run behind a Monatsexport | a few pages |

An export is the database plus every referenced image, so size the temporary directory for the dataset. Confirming a Datenimport is the peak: the uploaded archive, the safety copy and the extracted members are there at once — the dataset twice over, plus the upload.

`BELEGAPP_IMPORT_MAX_BYTES` bounds the upload, not the free space. An import that does not fit fails as `500 E_INTERN`, with the reason only in the log, because a short write is not a request that exceeded its limit. The Ingress example advertises `proxy-body-size: 4g` to match that limit, so a 4 GiB import is meant to be possible: budget the temporary directory for it, or set `BELEGAPP_IMPORT_MAX_BYTES` down to what the instance can actually stage.

| Example | `/tmp` | Note |
| --- | --- | --- |
| the `docker run` line above | `--tmpfs … size=64m` | RAM. Enough for a smoke test and a small instance |
| [deploy/docker-compose.yml](../deploy/docker-compose.yml) | tmpfs, 512 MiB | RAM, and it counts against `mem_limit: 512m` |
| [deploy/k8s](../deploy/k8s) | `emptyDir`, `sizeLimit: 1Gi` | Node disk, in the Deployment and in the backup CronJob alike |

A tmpfs page is memory, so an archive on a tmpfs competes with the process itself: the container is killed rather than told its disk ran out. Give the temporary directory disk before a large import is attempted — a `--mount type=volume` or a bind mount in Docker, a larger `emptyDir` (or a volume) in Kubernetes — and size the memory limit for the process, not for the archive. The CLI backup is the mild case: its zip goes wherever `--out` points, and only the snapshot lands here.

**`TMPDIR` moves all of it onto the data volume.** Go writes its temporary files where `TMPDIR` says:

```yaml
environment:
  TMPDIR: /data/tmp # mkdir -p data/tmp && chown 65532:65532 data/tmp
```

Then one volume has to hold the data and the archives, `/tmp` can stay small, and the ceiling is the volume rather than the memory limit. The directory has to exist and belong to UID 65532: startup creates `/data`, the blob directory and the directory of `BELEGAPP_DB_PATH`, and nothing else. Every file in the table is removed when its operation ends, so what a crash leaves behind is a `belegapp-*` file that is safe to delete by hand.

## Blob storage

`BELEGAPP_STORAGE_BACKEND` is `fs` (default) or `s3`. The store holds more than the receipt images: the same backend also carries every Datenexport archive (`datenexporte/…`) and every staged Datenimport (`importe/…`), because those are objects too.

- **`fs`** writes under `BELEGAPP_STORAGE_FS_DIR` (`${DATA_DIR}/blobs`): `bilder/<first two hex>/<sha256>.jpg` for a normalised receipt, `thumbs/…` for its thumbnail, `datenexporte/…` for the archives. All three sit inside the data volume, so backing up the volume is enough — but the archives, not the images, are what makes it grow. Nothing deletes an export: `BELEGAPP_EXPORT_RETENTION_YEARS` writes a retention *date* (10 years by default) and no sweeper acts on it. An instance that exports every month accumulates one archive per export until an operator removes old ones. The retention decision is in [SPEC.md](SPEC.md) §19.4.
- **`s3`** requires `BELEGAPP_S3_ENDPOINT`, `BELEGAPP_S3_REGION` and `BELEGAPP_S3_BUCKET`; a missing one is a startup error, not a degraded mode. `BELEGAPP_S3_PREFIX` (default `belegapp/`) namespaces the keys inside the bucket, so one bucket can hold several instances. Path-style addressing is the default and is what MinIO wants; turn it off for a bucket addressed by virtual host. `BELEGAPP_S3_SSE=true` asks for SSE-S3 — encryption with a KMS key is a bucket setting, not this one. Credentials are the two variables (each also as `_FILE`) and nothing else: the client is built with static credentials, so a workload identity or an instance role is never consulted.
- **The database stays on the data volume either way**, which is why a bucket copied on its own is not a backup. A Datenexport is the consistent unit: it snapshots the database and reads the referenced images through the store, so database and images always belong to the same point in time. See [backup.md](backup.md).

## Egress

The instance dials out to five hosts, and to nothing else besides DNS and the answers to inbound requests. [deploy/k8s/networkpolicy.yaml](../deploy/k8s/networkpolicy.yaml) is an example that allows DNS only, because these hosts differ per installation.

| Destination | Opened by | Why |
| --- | --- | --- |
| `BELEGAPP_OIDC_ISSUER_URL` | `serve` | Discovery at startup, then JWKS refreshes, over HTTPS |
| `BELEGAPP_LLM_BASE_URL` | the `erkennung` job | The receipt itself, see below |
| `BELEGAPP_S3_ENDPOINT` | the blob store | Only with `BELEGAPP_STORAGE_BACKEND=s3` |
| `BELEGAPP_MAIL_HOST:BELEGAPP_MAIL_PORT` | the `mail` job | Only with `BELEGAPP_MAIL_ENABLED=true` |
| `BELEGAPP_NOTIFY_WEBHOOK_URL` | the `notify` job | Only when the webhook is set |

The second line is the one to think about. **Receipts go to whatever that base URL is.** With the default it is `api.openai.com`, so every recognised receipt — the normalised JPEG, EXIF removed and rescaled to `BELEGAPP_LLM_MAX_IMAGE_PX` — leaves the host. Pointing `BELEGAPP_LLM_BASE_URL` at an Ollama instance on the same network keeps the images inside it; that is a supported setup, not a fallback, and [configuration.md](configuration.md#ollama) has the variables. The request body is one user message: the extraction prompt and a base64 `data:` URL of that JPEG. It carries no Beleg, no database row, no user name and no credential.

## Metrics

Setting `BELEGAPP_METRICS_ADDR` (for example `:9090`) starts a second listener for Prometheus. The metric names are in [configuration.md](configuration.md#metrics).

That listener has **no authentication**, and it is a separate port from the application — which is the point: publish it to the scrape network only, never through the reverse proxy and never with an Ingress. Nothing in [deploy/k8s](../deploy/k8s) exposes it. The Service publishes `8080`, the Deployment declares no metrics port, and the Ingress has no rule for one, so enabling scraping means either adding the port to the Service or scraping the pod address directly.

## Upgrading

`serve` applies the database migrations before it listens (`migrate` is that same step on its own, for a deployment that wants it outside the start window). An upgrade is a new image and a restart:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml pull
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

In Kubernetes the Deployment is `Recreate` with one replica, so the pod is replaced, not rolled; `kubectl rollout status deploy/belegapp` reports when it is back.

Two things to know before jumping a tag:

- **Take a Datenexport first** ([backup.md](backup.md)). The shipped migrations only ever move forward — there is no down migration — so the zip is what a roll-back restores, and it is the only roll-back there is.
- **A release that changes the physical layout of the file does a one-off rewrite on the first start**, which is slower and needs transient free disk on the data volume. The `auto_vacuum` conversion above is the current example, and a release that adds another such step says so in its notes.

Image tags are `X.Y.Z`, `X.Y`, `X`, and `latest`; the Compose and Kubernetes examples use `:1`. That follows every 1.x release, which is what an instance wants when security fixes should not wait for an edit; pin `:1.1.1` when each upgrade should be a decision. The database is only ever moved by a **start**, so a container that was pulled but never started leaves the instance exactly as it was.

## Binary

Go 1.27.2, Node 24.21.0 (`.nvmrc`), pnpm 12.10.1 (`packageManager` in `web/package.json`). Typst 0.15.1 must be on `PATH` for PDFs and `/readyz` ([scripts/install-typst.sh](../scripts/install-typst.sh)).

```bash
pnpm -C web install --frozen-lockfile
pnpm -C web build
CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp
mkdir -p data
BELEGAPP_DATA_DIR="$PWD/data" ./bin/belegapp set-password
BELEGAPP_DATA_DIR="$PWD/data" \
BELEGAPP_LISTEN_ADDR="127.0.0.1:8080" \
BELEGAPP_COOKIE_SECURE=false \
  ./bin/belegapp serve
```

`serve` is the default. Also: `migrate`, `healthcheck [--url] [--timeout]`, `version`, `set-password` (reads stdin, no echo), `verify-audit`, `backup --out <zip>`, `restore <zip> --yes`.

`BELEGAPP_COOKIE_SECURE=false` is only for local HTTP. The session cookie then drops the `__Host-` prefix. Leave the default `true` behind TLS.

## Reverse proxy

The process speaks HTTP on `BELEGAPP_LISTEN_ADDR` (default `:8080`). Terminate TLS on the proxy. The app does not set HSTS. Set `BELEGAPP_BASE_URL` to the public origin (`https://belege.example.de`). That origin is the CSRF trusted origin and the base of the OIDC redirect URI.

`BELEGAPP_COOKIE_SECURE` defaults to `true`. Browsers then send the session cookie only over HTTPS, and the cookie name is `__Host-belegapp_session` (no `Domain`, `Path=/`).

Rate limits use the client address. `X-Forwarded-For` is read only when the immediate peer is inside `BELEGAPP_TRUSTED_PROXIES`, and only from the right, so a client-supplied value cannot hide the hop the proxy appended. The app does not use `X-Forwarded-Proto`. Compose defaults the list to `172.16.0.0/12`. The Kubernetes ConfigMap uses `10.0.0.0/8`. Set the CIDR of the proxy as the process sees it.

Datenimport accepts up to 4 GiB (`BELEGAPP_IMPORT_MAX_BYTES`). The proxy body limit has to allow that. The Ingress example sets `nginx.ingress.kubernetes.io/proxy-body-size: 4g`.

Nginx on the same host as a binary that listens on `127.0.0.1:8080`:

```nginx
server {
    listen 443 ssl;
    server_name belege.example.de;
    client_max_body_size 4g;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

That peer is `127.0.0.1`, so `BELEGAPP_TRUSTED_PROXIES=127.0.0.1/32`. A published container port is different: Docker rewrites the source to the bridge gateway, which is why Compose uses `172.16.0.0/12`. Point `BELEGAPP_BASE_URL` at `https://belege.example.de`.

## Trusted headers (forward auth)

When the proxy already signs the browser in — a forward-auth gate in Traefik or Envoy Gateway, oauth2-proxy, Authelia behind Caddy — belegapp does not need a second login of its own. Set:

```bash
BELEGAPP_AUTH_HEADER_ENABLED=true
BELEGAPP_AUTH_HEADER_NAME=Remote-Email
BELEGAPP_AUTH_HEADER_ALLOWED=eduard@example.de
BELEGAPP_AUTH_HEADER_LOGOUT_URL=https://auth.example.de/logout
```

and leave `BELEGAPP_TRUSTED_PROXIES` on the network the proxy reaches this process from (the values in the Compose and Kubernetes examples are already the right shape). The first request from such a browser has no belegapp cookie; the header decides, a normal session is created for the actor `header:<value>`, and from then on the cookie is the authority. No password has to exist: `serve` starts as long as one of the three methods is configured. Nothing about the request path changes, so the proxy keeps doing everything it did — this only removes the second login.

The header is the whole identity, so three things have to hold:

- **The proxy must overwrite the header, never append it.** A client can send `Remote-Email` itself, and a header that arrives twice is treated as no header — there is no way to tell the proxy's value from the client's. Verify with a `curl -H 'Remote-Email: whoever@example.com'` against the public host: the app must answer `401`, not sign that identity in.
- **`BELEGAPP_TRUSTED_PROXIES` is required and must be exact.** A header from outside those networks is ignored, whatever it says. An empty list is a startup error, and the process must never be reachable directly on a network inside the range — that would let anybody assert an identity by guessing one header name.
- **The allowlist is required.** `BELEGAPP_AUTH_HEADER_ALLOWED` takes the same whole-label wildcards as `BELEGAPP_OIDC_ALLOWED_EMAILS` (`*`, `*@example.de`, `*@*.example.de`, an exact address) and, because the header is the operator's choice, a bare value such as `eduard` compared literally. `Remote-Email` is only the default name: a proxy that asserts a login name (`Remote-User`, `X-Forwarded-User`, `X-Auth-Request-User`) works by naming that header instead. `*` admits everybody the proxy authenticates — a legitimate choice when the proxy enforces group membership itself, a bad one when the IdP is open to the world.

There is no logout button for such a session, because dropping the belegapp session only earns a new one from the proxy on the next request. With `BELEGAPP_AUTH_HEADER_LOGOUT_URL` set, `Einstellungen` links there instead. Removing somebody from the allowlist ends their next sign-in, not the session they are in: to end one immediately, `Einstellungen → Sitzungen`.

## OIDC

Authorization code with PKCE (S256). Register this redirect URI on the provider, with no trailing slash on the origin:

```text
{BELEGAPP_BASE_URL}/api/v1/auth/oidc/callback
```

Minimum:

```bash
BELEGAPP_BASE_URL=https://belege.example.de
BELEGAPP_OIDC_ISSUER_URL=https://auth.example.de
BELEGAPP_OIDC_CLIENT_ID=belegapp
BELEGAPP_OIDC_CLIENT_SECRET_FILE=/run/secrets/oidc_secret
BELEGAPP_OIDC_ALLOWED_EMAILS=eduard@example.de
```

An empty client secret is a public client (PKCE only). At least one allowlist is required: comma-separated `sub` values (`BELEGAPP_OIDC_ALLOWED_SUBJECTS`), or comma-separated emails (`BELEGAPP_OIDC_ALLOWED_EMAILS`). Either one is optional on its own. An email match also requires `email_verified`. Entries may be whole-label wildcard patterns: `*@example.de` admits one whole domain, `*@*.example.de` that domain and its subdomains, and `*` every account with a verified email at the issuer — use that last one when the issuer is a single-tenant directory. Anything else, such as `te*@example.de`, is a startup error. The list is never implicitly empty: without one of the two, startup fails. Scopes default to `openid profile email`. The button label defaults to `Mit SSO anmelden`. The syntax is in [configuration.md](configuration.md#the-oidc-email-allowlist).

Password login can stay on as a fallback, and so can OIDC: `serve` accepts any of the three methods. They are independent of each other — an instance with only a password has no OIDC variables set at all. Incomplete OIDC (issuer set, but no client id or no allowlist) is a startup error.

Discovery retries in the background. Until it succeeds, `/api/v1/auth/config` reports OIDC as off and password login still works. `state`, `nonce`, and the PKCE verifier sit in a short-lived HMAC cookie. An empty `BELEGAPP_SECRET_KEY` generates a 32-byte key and stores it in the database.

`BELEGAPP_OIDC_RP_LOGOUT=true` sends the browser to the provider's end-session endpoint after local logout, when the discovery document has one. The post-logout redirect is `{BELEGAPP_BASE_URL}/login`.

The Compose example keeps the OIDC lines commented out and [deploy/.env.example](../deploy/.env.example) sets issuer, client id, secret file, and allowed emails for the case where they are uncommented.
