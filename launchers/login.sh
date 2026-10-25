#!/usr/bin/env bash
# Log in as the admin: mint a fresh one-time login link and open it in
# Firefox, so whoever is at this machine is signed in with one command. The
# macOS twin of login.bat.
#
#   launchers/login.sh        admin #1, the one the first run created
#   launchers/login.sh 3      user #3 instead (./run.sh manage list-users for ids)
#
# The server has to be running (./run.sh), because the link is redeemed by it.
# Minting a link replaces any unused one the account had, and leaves the
# sessions it already holds alone, so this never logs anyone else out.
#
# It uses the dist/mct ./run.sh last built and does not rebuild.
set -euo pipefail
cd "$( dirname "${BASH_SOURCE[0]}" )/.."

BINARY="$( pwd )/dist/mct"
USER_ID="${1:-1}"

if [ ! -x "$BINARY" ]; then
  echo "No dist/mct yet. Start the server with ./run.sh first."
  exit 1
fi

if ! "$BINARY" health >/dev/null 2>&1; then
  echo "The server is not answering. Start it with ./run.sh, then run launchers/login.sh again."
  exit 1
fi

# -url prints only the link; manage's own notes go to stderr.
if ! URL="$( "$BINARY" manage reissue-login -user-id "$USER_ID" -url )" || [ -z "$URL" ]; then
  echo "Could not mint a login link for user #${USER_ID} -- see the error above."
  exit 1
fi

# On macOS, open -a finds Firefox wherever it is installed; Spotlight answers
# whether it is installed at all without opening anything. xdg-open covers
# Linux.
if [ "$( uname )" = "Darwin" ]; then
  if [ -d /Applications/Firefox.app ] || [ -d "$HOME/Applications/Firefox.app" ] \
     || [ -n "$( mdfind "kMDItemCFBundleIdentifier == 'org.mozilla.firefox'" 2>/dev/null | head -n 1 )" ]; then
    echo "Logging in with Firefox:"
    echo "  $URL"
    open -a Firefox "$URL"
  else
    echo "Firefox is not installed, so opening the default browser instead."
    echo "  $URL"
    open "$URL"
  fi
elif command -v firefox >/dev/null 2>&1; then
  echo "Logging in with Firefox:"
  echo "  $URL"
  firefox "$URL" >/dev/null 2>&1 &
elif command -v xdg-open >/dev/null 2>&1; then
  echo "Firefox is not installed, so opening the default browser instead."
  echo "  $URL"
  xdg-open "$URL" >/dev/null 2>&1
else
  echo "No browser to open it with. Paste this into one:"
  echo "  $URL"
fi
