#!/bin/sh
# Compare sqlc's schema with a goose-migrated SQLite database.
# The config file has to live in the module root: sqlc resolves schema paths
# from the config file's directory. goose_db_version is bookkeeping; any other
# drift fails the check.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

work=$(mktemp -d "$root/.sqlc-check.XXXXXX")
cfg=$(mktemp "$root/.sqlc-check.XXXXXX.yaml")
cleanup() {
  rm -rf "$work" "$cfg"
}
trap cleanup EXIT

db="$work/belegapp.db"
if ! CGO_ENABLED=0 BELEGAPP_DATA_DIR="$work" BELEGAPP_DB_PATH="$db" \
  go run ./cmd/belegapp migrate >"$work/migrate.log" 2>&1; then
  cat "$work/migrate.log" >&2
  exit 1
fi

# uri is relative to the module root, where sqlc is invoked.
rel=${db#"$root/"}
cat >"$cfg" <<EOF
version: "2"
sql:
  - engine: sqlite
    schema: internal/db/migrations
    queries: internal/db/queries
    database:
      uri: "file:${rel}"
    gen:
      go:
        package: db
        out: internal/db
        sql_package: database/sql
EOF

set +e
go tool sqlc diff -f "$cfg" >"$work/diff.sql" 2>"$work/diff.err"
status=$?
set -e

filtered=$(grep -v -E 'goose_db_version|^[[:space:]]*$|^--' "$work/diff.sql" || true)
if [ "$status" -ne 0 ] || [ -n "$filtered" ]; then
  echo "sqlc diff failed (exit $status)" >&2
  cat "$work/diff.err" >&2
  cat "$work/diff.sql" >&2
  exit 1
fi
