#!/usr/bin/env bash
# Build portable, self-contained binaries -- one file per platform, each with
# the entire frontend compiled in.
#
# What you get is a single executable with no runtime dependencies at all: no
# Docker, no Go toolchain, no ./static directory, no language.yaml, no libc.
# Copy it to a machine, run it, and it serves the app. That is possible
# because of three choices working together:
#
#   CGO_ENABLED=0   static linking, so there is no libc version to match
#   go:embed        the frontend lives inside the binary (see main.go)
#   bolt            the database is one file the binary creates on first run
#
# Docker is still the better answer for a long-lived internet-facing
# deployment -- it is where the read-only filesystem, dropped capabilities,
# and restart policy come from. These binaries are for everything else:
# handing a colleague a working copy, an internal box with no container
# runtime, a Raspberry Pi, a USB stick.
#
# The binary name, the output directory, and the target list are the build:
# section of configs/config.yaml. Targets given on the command line replace the list
# for one run:
#
#   ./build.sh                  build every target in build.targets
#   ./build.sh linux/amd64      build one
#   ./build.sh darwin/arm64 linux/arm64
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"

# config.yaml is read through the app itself rather than parsed here, so this
# script gets the same defaults and validation as the server -- and Go is
# already required to build. With no config.yaml yet (a fresh clone), the
# defaults from server/config/defaults.yaml apply.
#
# Assigned before it is eval'd so that a config error stops the script:
# `eval "$( failing )"` would quietly eval nothing and carry on.
# Three binaries come out of this repo: the station (mct, package "."), the
# sync hub (mct-hub, ./cmd/hub) and the public site (mct-site, ./cmd/site).
# Each reads its own config's build: section -- configs/config.yaml,
# configs/config.hub.yaml, configs/config.site.yaml -- for its name and default targets.
#
#   ./build.sh                         all three, each for its configured targets
#   ./build.sh linux/amd64             all three, for just that platform
#   ./build.sh station|hub|site [os/arch ...]
WHICH=( station hub site )
if [ "${1:-}" = "station" ] || [ "${1:-}" = "hub" ] || [ "${1:-}" = "site" ]; then
  WHICH=( "$1" )
  shift
fi
EXPLICIT_TARGETS=( "$@" )

# Version metadata, stamped into the binary and readable later with
# `./mct version`. Being able to ask a binary what it is beats
# guessing from a filename once a few copies are in circulation.
# The version is the UTC build time (2026.10.23-1412): it sorts, it says
# when, and the commit hash is stamped separately alongside it.
VERSION="$( date -u +%Y.%m.%d-%H%M )"
COMMIT="$( git rev-parse --short HEAD 2>/dev/null || echo "unknown" )"
# The commit's own timestamp rather than "now", so building the same commit
# twice produces the same bytes. Outside a git checkout there is no commit to
# anchor to, and the current time is the honest answer. (`date -r` is BSD and
# macOS; `date -d @` is GNU.)
BUILD_EPOCH="$( git log -1 --format=%ct 2>/dev/null || date +%s )"
BUILD_DATE="$( date -u -r "$BUILD_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
  || date -u -d "@$BUILD_EPOCH" +%Y-%m-%dT%H:%M:%SZ )"

# -s -w drop the symbol and DWARF tables (smaller, less detail for anyone
# poking at a deployed binary). -trimpath keeps build-machine paths out of it,
# which also makes the output reproducible across checkout locations.
LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X main.version=${VERSION}"
LDFLAGS="$LDFLAGS -X main.commit=${COMMIT}"
LDFLAGS="$LDFLAGS -X main.buildDate=${BUILD_DATE}"

FAILED=()
for ROLE in "${WHICH[@]}"; do
  if [ "$ROLE" = "hub" ]; then
    PACKAGE="./cmd/hub"
    BUILD_CONFIG="$( go run ./cmd/hub config shell -file ./configs/config.hub.yaml build )"
  elif [ "$ROLE" = "site" ]; then
    PACKAGE="./cmd/site"
    BUILD_CONFIG="$( go run ./cmd/site config shell -file ./configs/config.site.yaml build )"
  else
    PACKAGE="."
    BUILD_CONFIG="$( go run . config shell -file ./configs/config.yaml build )"
  fi
  eval "$BUILD_CONFIG"
  BINARY_NAME="$build_binary_name"
  OUTPUT_DIR="$build_output_dir"
  if [ "$ROLE" = "station" ]; then
    STATION_NAME="$BINARY_NAME"
    STATION_OUTPUT_DIR="$OUTPUT_DIR"
    STATION_PUBLISH_DIR="$build_publish_dir"
  fi
  if [ ${#EXPLICIT_TARGETS[@]} -gt 0 ]; then
    TARGETS=( "${EXPLICIT_TARGETS[@]}" )
  else
    read -r -a TARGETS <<< "$build_targets"
  fi
  mkdir -p "$OUTPUT_DIR"
  echo "Building ${BINARY_NAME} ${VERSION} (${COMMIT})"

  for TARGET in "${TARGETS[@]}"; do
    GOOS="${TARGET%%/*}"
    GOARCH="${TARGET##*/}"

    OUTPUT="${OUTPUT_DIR}/${BINARY_NAME}_${VERSION}_${GOOS}_${GOARCH}"
    [ "$GOOS" = "windows" ] && OUTPUT="${OUTPUT}.exe"

    printf "  %-22s " "${GOOS}/${GOARCH}"
    if CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
         go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT" "$PACKAGE" 2>/tmp/build_err_$$; then
      SIZE="$( du -h "$OUTPUT" | cut -f1 | tr -d ' ' )"
      echo "ok  ${SIZE}  ${OUTPUT}"
      # Windows prints through SumatraPDF.exe, which cannot be compiled in;
      # the station looks for it beside itself first.
      if [ "$ROLE" = "station" ] && [ "$GOOS" = "windows" ]; then
        cp assets/windows/SumatraPDF.exe "${OUTPUT_DIR}/SumatraPDF.exe"
      fi
    else
      echo "FAILED"
      sed 's/^/      /' /tmp/build_err_$$ >&2
      FAILED+=( "$ROLE $TARGET" )
    fi
    rm -f /tmp/build_err_$$
  done
  echo ""
done

# Checksums, so whoever receives a binary can confirm it is the one you built.
# Handing someone an executable is a trust decision; give them a way to check.
if command -v shasum >/dev/null 2>&1; then
  ( cd "$OUTPUT_DIR" && shasum -a 256 mct_* mct-hub_* mct-site_* > SHA256SUMS 2>/dev/null ) || true
elif command -v sha256sum >/dev/null 2>&1; then
  ( cd "$OUTPUT_DIR" && sha256sum mct_* mct-hub_* mct-site_* > SHA256SUMS 2>/dev/null ) || true
fi

echo ""
if [ ${#FAILED[@]} -gt 0 ]; then
  echo "Failed: ${FAILED[*]}" >&2
  exit 1
fi

# Publish: copy the station binaries into build.publish_dir (the shared
# Dropbox folder), under the fixed names the stations have always run. Only
# after every build succeeded, so a broken build never reaches a station.
# Each file is copied to a temporary name and then renamed, so the sync never
# picks up a half-written binary.
if [ -n "${STATION_PUBLISH_DIR:-}" ]; then
  if [ ! -d "$STATION_PUBLISH_DIR" ]; then
    echo "build.publish_dir ${STATION_PUBLISH_DIR} does not exist (is Dropbox running?); nothing published" >&2
    exit 1
  fi
  publish() {
    local from="$1" name="$2"
    if [ ! -f "$from" ]; then
      echo "  (not built this run, skipped: ${name})"
      return
    fi
    cp "$from" "${STATION_PUBLISH_DIR}/.${name}.partial"
    mv -f "${STATION_PUBLISH_DIR}/.${name}.partial" "${STATION_PUBLISH_DIR}/${name}"
    echo "  ${name}"
  }
  BUILT="${STATION_OUTPUT_DIR}/${STATION_NAME}_${VERSION}"
  echo "Publishing to ${STATION_PUBLISH_DIR}"
  publish "${BUILT}_windows_amd64.exe" "${STATION_NAME}.exe"
  if [ -f "${BUILT}_windows_amd64.exe" ]; then
    publish assets/windows/SumatraPDF.exe SumatraPDF.exe
  fi
  publish "${BUILT}_darwin_amd64" "${STATION_NAME}"
  publish "${BUILT}_darwin_arm64" "${STATION_NAME}_arm"
  # Every station runs on this same config.yaml.
  publish configs/config.yaml config.yaml
  echo ""
fi

cat <<NOTE
Done. ${OUTPUT_DIR}/ holds one self-contained binary per platform, plus
SHA256SUMS.

To run one:

  ./${OUTPUT_DIR}/${BINARY_NAME}_${VERSION}_\$(go env GOOS)_\$(go env GOARCH)

That is the whole command. No files need to sit beside it -- the frontend is
compiled in. Its one config file is ./config.yaml if there is one in the
directory it is started from, else ~/.config/masters-closet-tracker/config.yaml,
which it creates on first run along with the rest of its state:

  ~/.config/masters-closet-tracker/
    config.yaml     every setting, plus the secret key (mode 0600)
    app.db          the database
    storage/        application files

server.app_dir in that config.yaml moves the state somewhere else. Ask a
built binary where it is looking with:

  ./${BINARY_NAME} manage paths

Two things to pass on with any binary you hand someone:

  * Back up config.yaml together with app.db. Its secret_key encrypts the
    database, so app.db without it is unreadable -- and either one alone is
    useless.
  * server.secure_cookies must be true anywhere real, behind an HTTPS proxy.
    It defaults to true; set it false only for local http://localhost, or the
    browser will drop the session cookie and login will look like it silently
    does nothing.

The binary is also the admin CLI:

  ./${BINARY_NAME} manage list-users
  ./${BINARY_NAME} manage reissue-login -user-id 1
  ./${BINARY_NAME} version

If a static/ directory sits beside config.yaml, or a language.yaml in the app
directory, those are used instead of the embedded copies -- which is what makes
editing the frontend during development work, and lets an operator reword the
UI without a rebuild.
NOTE
