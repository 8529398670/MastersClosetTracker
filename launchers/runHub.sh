#!/usr/bin/env bash
# Run the sync hub from this checkout, the way ./run.sh runs the station:
#
#   launchers/runHub.sh                          (re)build and run it, in the foreground
#   launchers/runHub.sh keys create -name "X"    any arguments: run that command instead
#
# It reads ./configs/config.hub.yaml (written on first use) and keeps its database in
# ~/.config/mct-hub/ unless server.app_dir says otherwise.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )/.."

BINARY="$( pwd )/dist/mct-hub"
VERSION="$( date -u +%Y.%m.%d-%H%M )"
COMMIT="$( git rev-parse --short HEAD 2>/dev/null || echo "unknown" )"
mkdir -p dist
go build -trimpath -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o "$BINARY" ./cmd/hub

if [ $# -gt 0 ]; then
  exec "$BINARY" "$@"
fi
if [ ! -f configs/config.hub.yaml ]; then
  "$BINARY" config init -file ./configs/config.hub.yaml
  echo "Review configs/config.hub.yaml, then run launchers/runHub.sh again."
  exit 0
fi
echo "Starting mct-hub ${VERSION} from $( pwd ) (config ./configs/config.hub.yaml)"
exec "$BINARY"
