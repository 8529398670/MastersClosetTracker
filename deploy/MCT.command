#!/usr/bin/env bash
# Double-click launcher for macOS stations. It sits beside the .files folder
# build.sh publishes to, and runs the station binary in there with the shared
# .files/config.yaml: stop the copy this Mac started last, start a fresh one,
# and once the server answers, open it in Firefox if Firefox is installed. The
# macOS twin of MCT.bat. Self-contained: nothing else in .files is a script.
#
#   MCT.command                 start the server, plus Firefox once it is up
#   MCT.command login           log in as admin #1 in Firefox (server must be up)
#   MCT.command login 3         the same, as user #3
#   MCT.command <arguments>     run the binary with them instead; no browser
#                               (MCT.command manage list-users, version, ...)
#
# Closing the Terminal window (or Ctrl-C) stops the server.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )"

if [ ! -d .files ]; then
  echo "There is no .files folder next to MCT.command in $( pwd )."
  echo "MCT.command has to sit beside it."
  exit 1
fi
cd .files

# Apple silicon runs mct_arm; Intel, or an Apple silicon Mac without one
# published, runs mct (under Rosetta for the latter). sysctl rather than
# uname -m, which says x86_64 when Terminal itself runs under Rosetta.
BINARY="$( pwd )/mct"
if [ "$( sysctl -n hw.optional.arm64 2>/dev/null )" = "1" ] && [ -f "$( pwd )/mct_arm" ]; then
  BINARY="$( pwd )/mct_arm"
fi
if [ ! -f "$BINARY" ]; then
  echo "No station binary in $( pwd ) -- has build.sh published one here?"
  exit 1
fi
# A sync can drop the execute bit; put it back rather than fail.
[ -x "$BINARY" ] || chmod +x "$BINARY"

firefox_installed() {
  if [ "$( uname )" = "Darwin" ]; then
    [ -d /Applications/Firefox.app ] || [ -d "$HOME/Applications/Firefox.app" ] \
      || [ -n "$( mdfind "kMDItemCFBundleIdentifier == 'org.mozilla.firefox'" 2>/dev/null | head -n 1 )" ]
  else
    command -v firefox >/dev/null 2>&1
  fi
}

# login: mint a fresh one-time login link and open it, so whoever is at this
# Mac is signed in with one command -- what launchers/login.sh does for a
# checkout. Minting a link replaces any unused one the account had, and leaves
# the sessions it already holds alone, so this never logs anyone else out.
if [ "${1:-}" = "login" ]; then
  USER_ID="${2:-1}"
  if ! "$BINARY" health >/dev/null 2>&1; then
    echo "The server is not answering. Start it with MCT.command, then run MCT.command login again."
    exit 1
  fi
  # -url prints only the link; manage's own notes go to stderr.
  if ! URL="$( "$BINARY" manage reissue-login -user-id "$USER_ID" -url )" || [ -z "$URL" ]; then
    echo "Could not mint a login link for user #${USER_ID} -- see the error above."
    exit 1
  fi
  if firefox_installed; then
    echo "Logging in with Firefox:"
    echo "  $URL"
    if [ "$( uname )" = "Darwin" ]; then open -a Firefox "$URL"; else firefox "$URL" >/dev/null 2>&1 & fi
  else
    echo "Firefox is not installed, so opening the default browser instead."
    echo "  $URL"
    if [ "$( uname )" = "Darwin" ]; then open "$URL"; else xdg-open "$URL" >/dev/null 2>&1 || true; fi
  fi
  exit 0
fi

if [ $# -gt 0 ]; then
  exec "$BINARY" "$@"
fi

# Every station runs on the one config.yaml build.sh publishes here. Without
# it the binary would quietly make a fresh one (and a fresh database) under
# ~/.config instead, which is never what a station wants.
if [ ! -f config.yaml ]; then
  echo "No config.yaml in $( pwd ) -- build.sh publishes it beside the binaries."
  exit 1
fi

# The pids of every process running one of this folder's binaries. Matching
# the full path, not a pid file, means nothing in this synced folder records
# which machine started what, and a stale pid can never point at something
# else.
running_servers() {
  ps -axo pid=,command= | awk -v a="$( pwd )/mct" -v b="$( pwd )/mct_arm" '
    { pid = $1; sub( /^[ ]*[0-9]+[ ]/ , "" ) }
    $0 == a || $0 == b { print pid }'
}

# Stop the copy this Mac started last. It shuts down gracefully within 10
# seconds (main.go); past 15, something is stuck and waiting will not help.
OLD_PIDS="$( running_servers )"
if [ -n "$OLD_PIDS" ]; then
  for PID in $OLD_PIDS; do
    echo "Stopping the previous server (pid ${PID})..."
    kill -TERM "$PID" 2>/dev/null || true
  done
  for _ in $( seq 1 60 ); do
    [ -n "$( running_servers )" ] || break
    sleep 0.25
  done
  for PID in $( running_servers ); do
    echo "It did not exit in 15 seconds; killing pid ${PID}."
    kill -KILL "$PID" 2>/dev/null || true
  done
fi

# The port the browser should go to: server.port in config.yaml.
PORT="$( awk '/^server:/ { s = 1; next } /^[^ #]/ { s = 0 }
              s && $1 == "port:" { gsub(/["'\'']/, "", $2); print $2; exit }' \
              config.yaml 2>/dev/null || true )"
URL="http://localhost:${PORT:-5960}/"
LAUNCHER_PID=$$

# Wait in the background for the server exec'd below, then open Firefox. exec
# keeps this shell's pid, so once that pid is running the binary, the old
# server is long gone and a health check that answers is the new one. If the
# server exits instead, this gives up with it.
if firefox_installed; then
  (
    while kill -0 "$LAUNCHER_PID" 2>/dev/null; do
      if [ "$( ps -p "$LAUNCHER_PID" -o command= 2>/dev/null )" = "$BINARY" ] \
         && "$BINARY" health >/dev/null 2>&1; then
        if [ "$( uname )" = "Darwin" ]; then
          open -a Firefox "$URL"
        else
          firefox "$URL" >/dev/null 2>&1 &
        fi
        exit 0
      fi
      sleep 0.5
    done
  ) &
fi

# The banner run.sh prints: a smaller cut of misc/ascii-art-example.txt. The
# version is the binary's own ("Masters Closet Tracker 2026.10.23-1412 (...)").
# Colour only on a terminal, and never with NO_COLOR set (https://no-color.org).
VERSION="$( "$BINARY" version 2>/dev/null | awk '{ print $4 }' || true )"
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
  printf '%s\n' "  ${DIM}config${RESET}   ./config.yaml"
  printf '%s\n' "  ${DIM}state${RESET}    ~/.config/masters-closet-tracker/ unless server.app_dir says otherwise"
  printf '%s\n' "  ${DIM}open${RESET}     ${URL}"
  echo
  printf '%s\n' "  On a fresh database the first-run login link is printed below."
  printf '%s\n' "  Locked out later? ${BOLD}MCT.command login${RESET} opens a fresh admin login in Firefox."
  echo
}

exec "$BINARY"
