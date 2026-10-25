#!/usr/bin/env bash
# Build and (re)start the sync hub container from configs/config.hub.yaml.
# The same as docker/dockerRun.sh hub -- see that script for what every flag does.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )/.."
exec docker/dockerRun.sh hub
