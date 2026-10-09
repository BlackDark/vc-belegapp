#!/bin/sh
# Normalize the web-dist artifact. upload-artifact sometimes nests dist/.
set -eu
cd "$(dirname "$0")/.."
if [ -f web/dist/index.html ]; then
  exit 0
fi
if [ -f web/dist/dist/index.html ]; then
  mv web/dist/dist/* web/dist/
  rmdir web/dist/dist
  exit 0
fi
if [ -f web/dist/web/dist/index.html ]; then
  mkdir -p web/dist-fixed
  mv web/dist/web/dist/* web/dist-fixed/
  rm -rf web/dist
  mv web/dist-fixed web/dist
  exit 0
fi
echo "unexpected web-dist layout" >&2
find web -name index.html | head
exit 1
