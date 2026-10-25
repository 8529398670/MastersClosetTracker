#!/usr/bin/env bash
# Run the sync hub from this checkout, the way run.sh runs the station:
#
#   ./runHub.sh                          (re)build and run it, in the foreground
#   ./runHub.sh keys create -name "X"    any arguments: run that command instead
#
# It reads ./config.hub.yaml (written on first use) and keeps its database in
# ~/.config/mct-hub/ unless server.app_dir says otherwise.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"

BINARY="$( pwd )/mct-hub"
VERSION="$( date -u +%Y.%m.%d-%H%M )"
COMMIT="$( git rev-parse --short HEAD 2>/dev/null || echo "unknown" )"
go build -trimpath -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o "$BINARY" ./cmd/hub

if [ $# -gt 0 ]; then
  exec "$BINARY" "$@"
fi
if [ ! -f config.hub.yaml ]; then
  "$BINARY" config init -file ./config.hub.yaml
  echo "Review config.hub.yaml, then run ./runHub.sh again."
  exit 0
fi
echo "Starting mct-hub ${VERSION} from $( pwd ) (config ./config.hub.yaml)"
exec "$BINARY"
