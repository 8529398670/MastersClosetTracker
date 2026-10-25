#!/usr/bin/env bash
# Build and (re)start the sync hub container from config.hub.yaml.
# The same as ./dockerRun.sh hub -- see that script for what every flag does.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"
exec ./dockerRun.sh hub
