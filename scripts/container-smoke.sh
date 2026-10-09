#!/bin/sh
# Start a prebuilt image the way production does and exercise password login
# plus one Beleg round-trip. Non-2xx responses fail the script.
set -eu

image=${1:?image ref}
port=${SMOKE_PORT:-18080}
name="belegapp-smoke-$$"
vol="belegapp-smoke-$$"
base="http://127.0.0.1:${port}"
# Low-cost PHC for the password "belegapp-e2e" (m=8192). Production hashes
# stay on the hash-password parameters.
password_hash='$argon2id$v=19$m=8192,t=1,p=1$ZTJlc2FsdGUyZXNhbHQ$fpwELKXJzuNANkbVeL78/95t50JZd5M7U094xSidoBM'
workdir=$(mktemp -d)
cookie="$workdir/cookies"
body="$workdir/body"

cleanup() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ]; then
    echo "smoke failed; container logs:" >&2
    docker logs "$name" >&2 || true
  fi
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm "$vol" >/dev/null 2>&1 || true
  rm -rf "$workdir"
  exit "$status"
}
trap cleanup EXIT

# 1x1 PNG. The server normalises it to JPEG.
python3 - "$workdir/beleg.png" <<'PY'
import base64, pathlib, sys
png = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)
pathlib.Path(sys.argv[1]).write_bytes(png)
PY

docker run -d --name "$name" \
  --read-only \
  --user 65532:65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --tmpfs /tmp:rw,nosuid,size=128m \
  -v "$vol:/data" \
  -p "127.0.0.1:${port}:8080" \
  -e BELEGAPP_LISTEN_ADDR=:8080 \
  -e BELEGAPP_COOKIE_SECURE=false \
  -e BELEGAPP_BASE_URL="$base" \
  -e BELEGAPP_TZ=Europe/Berlin \
  -e BELEGAPP_AUTH_PASSWORD_HASH="$password_hash" \
  -e BELEGAPP_LLM_ENABLED=false \
  -e BELEGAPP_LOG_FORMAT=text \
  -e BELEGAPP_LOG_LEVEL=info \
  "$image"

# req METHOD URL [curl args...] — writes the body and fails unless the status is 2xx.
req() {
  method=$1
  url=$2
  shift 2
  code=$(curl -sS -o "$body" -w '%{http_code}' -b "$cookie" -c "$cookie" -X "$method" "$url" "$@")
  case "$code" in
    2*) ;;
    *)
      echo "$method $url -> HTTP $code" >&2
      cat "$body" >&2 || true
      echo >&2
      exit 1
      ;;
  esac
}

ready=0
i=0
while [ "$i" -lt 40 ]; do
  i=$((i + 1))
  if curl -fsS "$base/healthz" >/dev/null 2>&1 && curl -fsS "$base/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "healthz/readyz did not become ready" >&2
  exit 1
fi

req POST "$base/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"passwort":"belegapp-e2e"}'

req GET "$base/api/v1/jahresregeln/2026/vorschlag"
cp "$body" "$workdir/regel.json"

req PUT "$base/api/v1/jahresregeln/2026" \
  -H 'Content-Type: application/json' \
  --data-binary @"$workdir/regel.json"

req POST "$base/api/v1/belegbilder" \
  -F "datei=@$workdir/beleg.png;type=image/png" \
  -F "erkennung=false"
bild_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$body")

python3 - "$workdir/beleg.json" "$bild_id" <<'PY'
import json, sys
json.dump({
    "datum": "2026-10-05",
    "mahlzeit": "mittag",
    "bezugsort": "supermarkt",
    "arbeitsort": "betrieb",
    "haendler_name": "REWE",
    "haendler_ort": "Koeln",
    "belegbetrag_cent": 840,
    "notiz": "",
    "bild_ids": [sys.argv[2]],
}, open(sys.argv[1], "w"))
PY

req POST "$base/api/v1/belege" \
  -H 'Content-Type: application/json' \
  --data-binary @"$workdir/beleg.json"
beleg_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$body")

req GET "$base/api/v1/belege/$beleg_id"
python3 - "$body" <<'PY'
import json, sys
row = json.load(open(sys.argv[1]))
if row.get("haendler_name") != "REWE" or row.get("belegbetrag_cent") != 840:
    raise SystemExit(f"unexpected beleg: {row.get('haendler_name')} {row.get('belegbetrag_cent')}")
print(f"smoke ok {row['id']} {row['datum']}")
PY
