#!/bin/sh
# Install pinned Typst. The first argument is the destination directory.
# SHA256 values must change in the same commit as version.
set -eu
dest="${1:-/usr/local/bin}"
# renovate: datasource=github-releases depName=typst/typst
version=0.15.1
case "$(uname -m)" in
  x86_64|amd64)
    arch=x86_64
    sha=a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c
    ;;
  aarch64|arm64)
    arch=aarch64
    sha=5aa8d74a3d906e60ea12a66ac2f37f8eef1b14cbad7182a745e393a10c23dcee
    ;;
  *)
    echo "unsupported architecture $(uname -m)" >&2
    exit 1
    ;;
esac
url="https://github.com/typst/typst/releases/download/v${version}/typst-${arch}-unknown-linux-musl.tar.xz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/typst.tar.xz" "$url"
echo "${sha}  $tmp/typst.tar.xz" | sha256sum -c -
tar -xJf "$tmp/typst.tar.xz" -C "$tmp"
mkdir -p "$dest"
install -m 0755 "$tmp/typst-${arch}-unknown-linux-musl/typst" "$dest/typst"
"$dest/typst" --version
