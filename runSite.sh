#!/usr/bin/env bash
# Run the public site from this checkout, the way run.sh runs the station:
#
#   ./runSite.sh                 (re)build and run it, in the foreground
#   ./runSite.sh config check    any arguments: run that command instead
#
# It reads ./config.site.yaml (written on first use) and serves the pages
# straight from ./static, so an edit shows on the next refresh.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"

BINARY="$( pwd )/mct-site"
VERSION="$( date -u +%Y.%m.%d-%H%M )"
COMMIT="$( git rev-parse --short HEAD 2>/dev/null || echo "unknown" )"
go build -trimpath -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o "$BINARY" ./cmd/site

if [ $# -gt 0 ]; then
  exec "$BINARY" "$@"
fi
if [ ! -f config.site.yaml ]; then
  "$BINARY" config init -file ./config.site.yaml
  echo "Review config.site.yaml, then run ./runSite.sh again."
  exit 0
fi
echo "Starting mct-site ${VERSION} from $( pwd ) (config ./config.site.yaml)"
exec "$BINARY"
