# Deployment

The process runs as UID/GID 65532, with a read-only root filesystem, no capabilities, and no new privileges. Writable paths are the data volume and a tmpfs on `/tmp`. A named volume inherits ownership from the image. A bind mount needs `chown -R 65532:65532` first. `serve` exits 2 until a password hash or OIDC is configured. Variables: [configuration.md](configuration.md).

Image tags have no `v` prefix. Git tag `v1.1.1` is the image `ghcr.io/blackdark/vc-belegapp:1.1.1`, plus `1.1`, `1`, and `latest`.

## Docker

`/healthz` returns `ok`. `/readyz` is 200 when SQLite, migrations, the blob store, and `typst --version` are fine, otherwise 503. The image is `gcr.io/distroless/static-debian13:nonroot` plus a static Typst 0.15.1 binary, so the healthcheck is `belegapp healthcheck` (no curl).

```bash
printf '%s' 'a-long-password' | docker run --rm -i --entrypoint /belegapp \
  ghcr.io/blackdark/vc-belegapp:1.1.1 hash-password

docker volume create belegapp-data
docker run -d --name belegapp \
  --read-only \
  --user 65532:65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --tmpfs /tmp:rw,nosuid,size=64m \
  -v belegapp-data:/data \
  -p 127.0.0.1:8080:8080 \
  -e BELEGAPP_AUTH_PASSWORD_HASH='<argon2id PHC>' \
  ghcr.io/blackdark/vc-belegapp:1.1.1
```

`docker build -t vc-belegapp:dev .` compiles the web bundle and the Go binary inside the image. CI and releases do not use that Dockerfile. They copy a host-built `linux/$TARGETARCH/belegapp` with [Dockerfile.goreleaser](../Dockerfile.goreleaser) (`make docker-prebuilt` does the same locally).

## Docker Compose

[deploy/docker-compose.yml](../deploy/docker-compose.yml) and [deploy/.env.example](../deploy/.env.example). Copy the example to `deploy/.env` and point the secret files at an OIDC client secret, an argon2id hash, and an LLM API key. Paths in the example are relative to the compose file. `${VAR:?}` makes Compose refuse to start while a required value is empty. The example is read-only, UID 65532, capabilities dropped, 1 CPU / 512 MiB. It publishes `127.0.0.1:8080` and sets `BELEGAPP_TRUSTED_PROXIES` so a proxy on the Docker bridge can pass the client address.

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

## Kubernetes

[deploy/k8s](../deploy/k8s) is Kustomize: one replica, `Recreate` (SQLite), UID 65532, read-only root, a PVC at `/data`, an `emptyDir` at `/tmp`, a Service, an Ingress, and a backup CronJob. The Ingress example is nginx with cert-manager and `proxy-body-size: 4g`, which matches `BELEGAPP_IMPORT_MAX_BYTES`. The ConfigMap sets `BELEGAPP_BASE_URL` and `BELEGAPP_TRUSTED_PROXIES` (`10.0.0.0/8`). Replace the placeholder hash in [deploy/k8s/secret.yaml](../deploy/k8s/secret.yaml) before a real deploy. `networkpolicy.yaml` is not part of the default kustomization, because the IdP, LLM, and S3 hosts differ per install.

```bash
kubectl kustomize deploy/k8s
kubectl apply -k deploy/k8s
```

A ReadWriteOnce volume often cannot be mounted by the CronJob while the Deployment holds it. Use a storage class that allows a second mount on the same node, or run the backup on the host against the data directory. See [backup.md](backup.md).

## Binary

Go 1.27.2, Node 24.21.0 (`.nvmrc`), pnpm 12.10.1 (`packageManager` in `web/package.json`). Typst 0.15.1 must be on `PATH` for PDFs and `/readyz` ([scripts/install-typst.sh](../scripts/install-typst.sh)).

```bash
pnpm -C web install --frozen-lockfile
pnpm -C web build
CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp
mkdir -p data
printf '%s' 'a-long-password' | ./bin/belegapp hash-password
BELEGAPP_DATA_DIR="$PWD/data" \
BELEGAPP_LISTEN_ADDR="127.0.0.1:8080" \
BELEGAPP_COOKIE_SECURE=false \
BELEGAPP_AUTH_PASSWORD_HASH='<argon2id PHC>' \
  ./bin/belegapp serve
```

`serve` is the default. Also: `migrate`, `healthcheck [--url] [--timeout]`, `version`, `hash-password` (reads stdin, no echo), `verify-audit`, `backup --out <zip>`, `restore <zip> --yes`.

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

An empty client secret is a public client (PKCE only). At least one allowlist is required: comma-separated `sub` values (`BELEGAPP_OIDC_ALLOWED_SUBJECTS`), or comma-separated emails (`BELEGAPP_OIDC_ALLOWED_EMAILS`). Either one is optional on its own. An email match also requires `email_verified`. Set `BELEGAPP_OIDC_ALLOWED_EMAILS=*` to admit every account with a verified email at the issuer, e.g. when the issuer is a single-tenant directory. The list is never implicitly empty: without one of the two, startup fails. Scopes default to `openid profile email`. The button label defaults to `Mit SSO anmelden`.

Password login can stay on as a fallback. `serve` accepts either method. Incomplete OIDC (issuer set, but no client id or no allowlist) is a startup error.

Discovery retries in the background. Until it succeeds, `/api/v1/auth/config` reports OIDC as off and password login still works. `state`, `nonce`, and the PKCE verifier sit in a short-lived HMAC cookie. An empty `BELEGAPP_SECRET_KEY` generates a 32-byte key and stores it in the database.

`BELEGAPP_OIDC_RP_LOGOUT=true` sends the browser to the provider's end-session endpoint after local logout, when the discovery document has one. The post-logout redirect is `{BELEGAPP_BASE_URL}/login`.

The Compose example and [deploy/.env.example](../deploy/.env.example) set issuer, client id, secret file, and allowed emails together.
