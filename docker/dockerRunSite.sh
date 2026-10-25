#!/usr/bin/env bash
# Build and (re)start the public site container from configs/config.site.yaml.
# The same as docker/dockerRun.sh site -- see that script for what every flag does.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )/.."
exec docker/dockerRun.sh site
