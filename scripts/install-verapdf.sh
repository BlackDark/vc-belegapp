#!/bin/sh
# Install pinned veraPDF greenfield 1.30.2 (CLI pack only). Requires Java 21.
set -eu
dest="${1:-$HOME/verapdf}"
version=1.30.2
sha=6cc6341cb1af644044054b81f00a6590a7918abb18f762243de115258bcad838
url="https://software.verapdf.org/releases/1.30/verapdf-greenfield-${version}-installer.zip"
root=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/verapdf.zip" "$url"
echo "${sha}  $tmp/verapdf.zip" | sha256sum -c -
unzip -q "$tmp/verapdf.zip" -d "$tmp"
jar=$(find "$tmp" -name "verapdf-izpack-installer-${version}.jar" | head -n 1)
if [ -z "$jar" ]; then
  echo "veraPDF installer jar not found" >&2
  exit 1
fi
sed "s|__INSTALL_PATH__|${dest}|g" "$root/verapdf-auto-install.xml" > "$tmp/auto-install.xml"
rm -rf "$dest"
java -jar "$jar" "$tmp/auto-install.xml"
"$dest/verapdf" --version
