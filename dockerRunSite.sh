#!/usr/bin/env bash
# Build and (re)start the public site container from config.site.yaml.
# The same as ./dockerRun.sh site -- see that script for what every flag does.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"
exec ./dockerRun.sh site
