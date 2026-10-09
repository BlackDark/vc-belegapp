#!/bin/sh
# Cross-compile belegapp for every GoReleaser target. CGO_ENABLED=0, so the
# runner builds foreign arches natively (no QEMU).
set -eu
cd "$(dirname "$0")/.."

version=${VERSION:-dev}
commit=${COMMIT:-none}
date=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
out=${OUT:-dist}
ldflags="-s -w -X main.version=${version} -X main.commit=${commit} -X main.date=${date}"

go mod download

# One target per runner. Parallel compiles on a single runner split the
# cores and finish no sooner, which held up e2e.
if [ "$#" -eq 0 ]; then
  set -- linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
fi

for target in "$@"; do
  os=${target%%/*}
  arch=${target#*/}
  dest="$out/$os/$arch"
  mkdir -p "$dest"
  echo "build $target"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "$ldflags" -o "$dest/belegapp" ./cmd/belegapp
done
