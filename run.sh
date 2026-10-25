#!/usr/bin/env bash
# Build and run the server from this checkout, outside Docker.
#
#   ./run.sh                    rebuild if anything changed, stop the copy this
#                               checkout started last, start a fresh one here
#   ./run.sh manage <command>   run an admin command against it (see below)
#   ./run.sh <anything else>    same: passed straight to the binary
#
# The server runs in the foreground: its log streams here and Ctrl-C stops it.
# Running ./run.sh again from another terminal stops this one and takes over,
# so "edit, ./run.sh, reload the page" is the whole loop.
#
# With arguments it is a one-off instead -- the same build, then the binary
# with those arguments -- and the running server is left alone. That is what
# makes the admin CLI usable while it is up: `manage` talks to the live server
# over its control socket rather than opening the database itself.
#
#   ./run.sh manage list-users
#   ./run.sh manage reissue-login -user-id 1     fresh login link (locked out)
#   ./run.sh manage create-login -name "Grace" -role user
#   ./run.sh manage create-invite -user-id 1 -label "Book club"
#   ./run.sh manage create-key -user-id 1 -name "CI"
#   ./run.sh manage paths                        where the state lives
#   ./run.sh config check
#   ./run.sh version
#
# This is for development and internal boxes. It is a plain process with your
# permissions -- none of dockerRun.sh's read-only filesystem, dropped
# capabilities, or pid and memory limits apply -- so use Docker for anything
# long-lived or internet-facing.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"


# The dev build goes in dist/ beside build.sh's releases, unversioned so
# login.sh and the next run know where to find it.
BINARY="$( pwd )/dist/mct"
RUN_DIR="$( pwd )/.run"
PID_FILE="${RUN_DIR}/server.pid"

# Rebuild every time and let Go decide whether that means anything: its build
# cache makes an unchanged tree a near-instant no-op, and catches every input
# that matters -- .go files, go.mod, and the embedded static/ and
# language.yaml -- with no timestamp bookkeeping here to get wrong.
#
# This happens before anything is stopped, so a build error leaves the server
# that was already running untouched.
#
# Version metadata is stamped the way build.sh does it, so `./run.sh version`
# says what is running. No -s -w: this build is for debugging, not shipping.
# The version is the UTC build time (2026.10.23-1412): it sorts, it says
# when, and the commit hash is stamped separately alongside it.
VERSION="$( date -u +%Y.%m.%d-%H%M )"
COMMIT="$( git rev-parse --short HEAD 2>/dev/null || echo "unknown" )"
mkdir -p dist
go build -trimpath \
  -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" \
  -o "$BINARY" .

if [ $# -gt 0 ]; then
  exec "$BINARY" "$@"
fi

# First run on this checkout: config.yaml is gitignored, so a fresh clone has
# none. Without one here the binary would quietly create
# ~/.config/masters-closet-tracker/config.yaml instead, which is the right thing for
# a portable binary and the wrong one for a checkout. Write it here and stop,
# like dockerRun.sh, so it gets looked at before the first start.
if [ ! -f configs/config.yaml ]; then
  "$BINARY" config init -file ./configs/config.yaml
  cat <<NOTE

For plain http://localhost set server.secure_cookies: false in configs/config.yaml,
or the browser drops the session cookie and login silently does nothing.
Then run ./run.sh again.
NOTE
  exit 0
fi

# Stop the copy this checkout started last. The pid file says which process
# that was; the command-line check makes sure it still is, because a pid left
# behind by a server that has since exited can belong to anything by now.
if [ -f "$PID_FILE" ]; then
  OLD_PID="$( cat "$PID_FILE" )"
  if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null \
     && [ "$( ps -p "$OLD_PID" -o command= 2>/dev/null )" = "$BINARY" ]; then
    echo "Stopping the previous server (pid ${OLD_PID})..."
    kill -TERM "$OLD_PID" 2>/dev/null || true
    # It shuts down gracefully within 10 seconds (main.go). Past that,
    # something is stuck, and waiting longer will not unstick it.
    for _ in $( seq 1 60 ); do
      kill -0 "$OLD_PID" 2>/dev/null || break
      sleep 0.25
    done
    if kill -0 "$OLD_PID" 2>/dev/null; then
      echo "It did not exit in 15 seconds; killing it."
      kill -KILL "$OLD_PID" 2>/dev/null || true
    fi
  fi
  rm -f "$PID_FILE"
fi

mkdir -p "$RUN_DIR"

# The banner: a smaller cut of misc/ascii-art-example.txt. Colour only on a
# terminal, and never with NO_COLOR set (https://no-color.org).
ART="" BOLD="" DIM="" RESET=""
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  ART=$'\e[36m' BOLD=$'\e[1m' DIM=$'\e[2m' RESET=$'\e[0m'
fi
{
  echo
  printf '%s\n' "  ${ART}        ----------------${RESET}"
  printf '%s\n' "  ${ART}   --------------------------${RESET}"
  printf '%s\n' "  ${ART}       +----------------+${RESET}"
  printf '%s\n' "  ${ART} ------|     |    |     |------${RESET}"
  printf '%s\n' "  ${ART}   ----+     |    |     +----${RESET}"
  printf '%s\n' "  ${ART} ------------|    |------------${RESET}"
  printf '%s\n' "  ${ART}     +-------+    +-------+       ${RESET}   ${BOLD}MASTER'S CLOSET${RESET}"
  printf '%s\n' "  ${ART}-----|                    |-----  ${RESET}   ${DIM}tracker${RESET}  ${VERSION}"
  printf '%s\n' "  ${ART}     +-------+    +-------+       ${RESET}   ${DIM}from${RESET}     $( pwd )"
  printf '%s\n' "  ${ART} ------------|    |------------${RESET}"
  printf '%s\n' "  ${ART}   ----+-----|    |-----+----${RESET}"
  printf '%s\n' "  ${ART}       |     |    |     |${RESET}"
  printf '%s\n' "  ${ART} ------+-----+    +-----+------${RESET}"
  printf '%s\n' "  ${ART}             +----+${RESET}"
  printf '%s\n' "  ${ART}      --------------------${RESET}"
  printf '%s\n' "  ${ART}  ----------------------------${RESET}"
  echo
  printf '%s\n' "  ${DIM}config${RESET}   ./configs/config.yaml"
  printf '%s\n' "  ${DIM}state${RESET}    ~/.config/masters-closet-tracker/ unless server.app_dir says otherwise"
  printf '%s\n' "  ${DIM}static${RESET}   ./static is served from disk, so frontend edits need only a reload"
  echo
  printf '%s\n' "  On a fresh database the first-run login link is printed below."
  printf '%s\n' "  Locked out later? ${BOLD}launchers/login.sh${RESET} opens a fresh admin login in Firefox."
  echo
}

# exec keeps this shell's pid, so the one written here is the server's.
echo $$ > "$PID_FILE"
exec "$BINARY"
