#!/bin/sh
# omakaiju installer.
#
# Downloads the latest prebuilt Linux binary from GitHub Releases and
# installs it into ~/.local/bin (or $OMAKAIJU_PREFIX/bin).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh
#   ... or pinned to a version:
#   curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh -s -- v0.9.0
#
# Options:
#   OMAKAIJU_PREFIX=/usr/local  install prefix   (default: $HOME/.local)
#   OMAKAIJU_VERSION=v0.9.0     release version (default: latest)
#
#   OMAKAIJU_ALLOW_UNSIGNED=1   install a release that carries no signature
#                               (for the releases published before signing)
#   OMAKAIJU_NO_ICONS=1         the terminal has no Nerd Font; the installer
#                               stops suggesting one (the same setting turns
#                               the binary's own icons off)
#   NO_COLOR=1                  no colour in the installer's own output
set -eu

REPO="bjornramberg/omakaiju"
BIN_NAME="omakaiju"

# The public key the releases are signed with.
#
# The checksum file is fetched from the same release as the binary, so on its
# own it proves only that the download arrived intact: a release that was
# tampered with would serve a binary and a checksum that agree with each
# other. The signature is what says the release is ours. The key is pinned
# here, in the script the user fetched, so the trust does not depend on the
# server that served the files.
#
# The matching private key lives in the repository's secrets and never leaves
# the release workflow. The public key is also published as
# RELEASE_KEY.pub.pem, so it can be checked against another source.
RELEASE_PUBLIC_KEY='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA1CBQJaZySQh+W9wPTozB43IR0jItzQVh/qGl3lp2ByQ=
-----END PUBLIC KEY-----'

# NOTE: this must stay in the "${1:-...}" form -- under `set -u`, dash
# evaluates $1 inside "${OMAKAIJU_VERSION:-$1}" even when the env var is
# unset, which kills piped installs (`curl ... | sh`) with
# "line 20: $1: unbound variable".
VERSION="${1:-${OMAKAIJU_VERSION:-}}"
PREFIX="${OMAKAIJU_PREFIX:-$HOME/.local}"

# Where the releases are fetched from. It can be pointed somewhere else for
# a mirror or a test; the signature still has to check out against the key
# pinned in this script, so a different host cannot serve a different
# binary.
BASE_URL="${OMAKAIJU_BASE_URL:-https://github.com/$REPO/releases}"

if [ -n "$VERSION" ]; then
  # The release tags carry a leading "v" (v0.10.0), while users naturally
  # type 0.10.0 -- and /releases/download/0.10.0/... does not exist. Accept
  # both forms by normalising to the tagged form before building the URL.
  VERSION="v${VERSION#v}"
  URL_BASE="$BASE_URL/download/$VERSION"
  RELEASE="release $VERSION"
else
  # 'latest/download' always resolves to the newest release.
  URL_BASE="$BASE_URL/latest/download"
  RELEASE="the latest release"
fi

# --- presentation ----------------------------------------------------------
#
# Everything the installer says goes to stdout, so `curl ... | sh`, a run
# redirected to a log, and one attached to a terminal all see the same
# account of what happened. Only the presentation changes: a terminal gets
# the tick-boxes and the spinner, a pipe or a file gets plain lines. That
# decision is made once, here, because the step helpers below consult it on
# every line.
TTY=0
if [ -t 1 ]; then
  # A terminal advertised as dumb cannot be trusted with carriage returns
  # or colours; it gets the plain stream, which is readable everywhere.
  case "${TERM:-}" in
    ''|dumb) ;;
    *) TTY=1 ;;
  esac
fi

# Colour is opt-out through NO_COLOR and is skipped when the terminal says
# it has none. tput is consulted when it exists; when it does not, TERM=dumb
# above is the best signal available.
supports_colour() {
  [ -z "${NO_COLOR:-}" ] || return 1
  case "${TERM:-}" in
    ''|dumb) return 1 ;;
  esac
  if command -v tput >/dev/null 2>&1; then
    colours="$(tput colors 2>/dev/null)" || return 1
    case "$colours" in
      ''|*[!0-9]*) return 1 ;;
    esac
    [ "$colours" -ge 8 ] || return 1
  fi
  return 0
}

GREEN=""
RED=""
YELLOW=""
RESET=""
if [ "$TTY" = 1 ] && supports_colour; then
  GREEN="$(printf '\033[32m')"
  RED="$(printf '\033[31m')"
  YELLOW="$(printf '\033[33m')"
  RESET="$(printf '\033[0m')"
fi

TICK='✓'
CROSS='✗'
PENDING='⠹'
SPINNER_PID=""
STEP_OPEN=""

# The spinner redraws one line in place. It is started for the one step that
# can take seconds (the download) and killed by stop_spinner before the
# result of that step is printed, so it can never scribble over the outcome.
spinner() {
  label=$1
  set -- '⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏'
  while :; do
    for frame in "$@"; do
      printf '\r\033[2K  %s %s …' "$frame" "$label"
      sleep 0.1
    done
  done
}

# stop_spinner is safe to call at any time: with no spinner running it does
# nothing. Wait for the process to go away after killing it, so a frame
# cannot be written between the kill and the line that replaces it.
stop_spinner() {
  if [ -n "$SPINNER_PID" ]; then
    pid="$SPINNER_PID"
    SPINNER_PID=""
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    printf '\r\033[2K'
  fi
}

step_start() {
  STEP_OPEN=1
  if [ "$TTY" = 1 ]; then
    printf '  %s %s …' "$PENDING" "$1"
  else
    printf '==> %s\n' "$1"
  fi
}

# A step with a spinner first draws its pending line, then lets the spinner
# take the line over. Drawing it first means the step is named even when the
# work finishes before the spinner's first frame.
step_start_spin() {
  step_start "$1"
  if [ "$TTY" = 1 ]; then
    spinner "$1" &
    SPINNER_PID=$!
  fi
}

step_done() {
  stop_spinner
  if [ "$TTY" = 1 ]; then
    printf '\r\033[2K  %s%s%s %s\n' "$GREEN" "$TICK" "$RESET" "$1"
  else
    printf '    ok: %s\n' "$1"
  fi
  STEP_OPEN=""
}

step_warn() {
  stop_spinner
  if [ "$TTY" = 1 ]; then
    printf '\r\033[2K  %s!%s %s\n' "$YELLOW" "$RESET" "$1"
  else
    printf '    warn: %s\n' "$1"
  fi
  STEP_OPEN=""
}

step_fail() {
  stop_spinner
  if [ "$TTY" = 1 ]; then
    printf '\r\033[2K  %s%s%s %s\n' "$RED" "$CROSS" "$RESET" "$1"
  else
    printf '    failed: %s\n' "$1"
  fi
  STEP_OPEN=""
}

warn() {
  if [ "$TTY" = 1 ]; then
    printf '  %sWARNING:%s %s\n' "$YELLOW" "$RESET" "$1"
  else
    printf 'WARNING: %s\n' "$1"
  fi
}

# nerd_font_check -- warn when the terminal probably cannot draw the icons.
#
# This is a guess and is worded as one: the script cannot see which font the
# terminal has loaded. It looks for terminals that are known to work with a
# Nerd Font and stays quiet when it has nothing to go on, rather than warning
# at every terminal it does not recognise. It only ever warns; the install
# goes ahead either way.
nerd_font_check() {
  # The same values the binary's own reader treats as "icons on": an unset
  # or empty variable, or a value that parses as false. Anything else means
  # the user already turned the icons off, so there is nothing to warn about.
  case "${OMAKAIJU_NO_ICONS:-}" in
    ''|0|f|F|FALSE|false|False) ;;
    *) return 0 ;;
  esac

  # The kernel console cannot draw them at all, and neither can a dumb
  # terminal. Both are identified with certainty rather than guessed at.
  case "${TERM:-}" in
    linux*|dumb) ;;
    *)
      # Terminals that are known to work with a Nerd Font, by name or by a
      # variable only they set.
      case "${TERM_PROGRAM:-}${LC_TERMINAL:-}" in
        *iTerm*|*WezTerm*|*wezterm*|*kitty*|*Kitty*|*ghostty*|*Ghostty*|*Warp*)
          return 0
          ;;
      esac
      if [ -n "${KITTY_WINDOW_ID:-}" ] || [ -n "${WEZTERM_EXECUTABLE:-}" ] ||
         [ -n "${WEZTERM_PANE:-}" ] || [ -n "${GHOSTTY_RESOURCES_DIR:-}" ] ||
         [ -n "${WT_SESSION:-}" ]; then
        return 0
      fi

      # A plain terminal name. The colour-depth suffix is dropped so
      # xterm-256color and xterm are recognised as the same thing. screen
      # and tmux are deliberately absent: they hide which terminal is really
      # underneath, and guessing there would be worse than saying nothing.
      base="${TERM:-}"
      base="${base%%-*}"
      case "$base" in
        xterm|vt100|vt220|ansi|rxvt) ;;
        *) return 0 ;;
      esac
      ;;
  esac

  if [ "$TTY" = 1 ]; then
    printf '  %sWARNING:%s this terminal may not have a Nerd Font; some icons may show as boxes.\n' \
      "$YELLOW" "$RESET"
    printf '          Set OMAKAIJU_NO_ICONS=1 to turn the icons off.\n'
  else
    printf 'WARNING: this terminal may not have a Nerd Font; some icons may show as boxes.\n'
    printf '         Set OMAKAIJU_NO_ICONS=1 to turn the icons off.\n'
  fi
}

# file_size PATH -- a compact size for the download's outcome line.
file_size() {
  bytes="$(wc -c <"$1" | tr -d '[:space:]')"
  if [ "$bytes" -ge 1048576 ]; then
    printf '%s.%s MB' "$((bytes / 1048576))" "$(((bytes % 1048576) * 10 / 1048576))"
  elif [ "$bytes" -ge 1024 ]; then
    printf '%s KB' "$((bytes / 1024))"
  else
    printf '%s B' "$bytes"
  fi
}

# --- steps -----------------------------------------------------------------

step_start "checking the terminal"

case "$(uname -s)" in
  Linux) ;;
  *)
    step_fail "prebuilt binaries are only provided for Linux"
    echo "error: prebuilt binaries are only provided for Linux." >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64)       ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    step_fail "unsupported architecture: $(uname -m)"
    echo "error: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

command -v curl >/dev/null 2>&1 || {
  step_fail "curl is required"
  echo "error: curl is required." >&2
  exit 1
}

step_done "terminal looks fine"
nerd_font_check

TARBALL="${BIN_NAME}_linux_${ARCH}.tar.gz"
TARBALL_URL="$URL_BASE/$TARBALL"
CHECKSUMS_URL="$URL_BASE/checksums.txt"
SIGNATURE_URL="$URL_BASE/checksums.txt.sig"

TMPDIR_DL="$(mktemp -d)"
STAGED=""
# Set only once the install has actually completed; see cleanup.
INSTALL_OK=""

# Every exit path goes through here: a failed download, a failed checksum, an
# interrupt, or a normal finish. Both pieces of state are removed together,
# and there is only ever one trap -- installing a second trap over this one
# (as an earlier version of this script did) is how whole download
# directories leaked into /tmp on every install, interrupted or not.
#
# The trap is removed first, so a second Ctrl-C while the cleanup is still
# running is not swallowed by it and stops the installer at once. The
# spinner, if one is still alive, is killed here too: it is a child of this
# shell and would otherwise outlive the interrupt.
cleanup() {
  status=$?
  trap - EXIT INT TERM
  stop_spinner
  if [ "$TTY" = 1 ] && [ -n "$STEP_OPEN" ]; then
    printf '\n'
  fi
  if [ -n "$STAGED" ]; then
    rm -f "$STAGED" || true
  fi
  rm -rf "$TMPDIR_DL" || true
  # A handler that merely returns lets dash resume the script after a
  # signal, so stop here explicitly. dash also runs the handler with $? = 0
  # when a signal interrupts the script, so an unfinished run is forced to a
  # non-zero status rather than reporting the interrupt as success.
  if [ -z "$INSTALL_OK" ] && [ "$status" -eq 0 ]; then
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

if [ -n "$VERSION" ]; then
  FETCH_WHAT="$VERSION for linux/$ARCH"
else
  FETCH_WHAT="the latest release for linux/$ARCH"
fi

step_start_spin "fetching $BIN_NAME $FETCH_WHAT"

# download URL DEST WHAT -- fetch URL into DEST, reporting the failures worth
# naming in the installer's own words instead of curl's.
#
# --retry-all-errors makes the retries cover transient failures that plain
# --retry skips (a connection reset, a truncated response), so a flaky
# network does not need a second run of the installer.
download() {
  if http_code="$(curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
      -o "$2" -w '%{http_code}' "$1" 2>"$TMPDIR_DL/curl.err")"; then
    return 0
  fi
  case "$http_code" in
    404)
      # The release exists but does not carry this asset: naming that beats
      # echoing curl's "requested URL returned error: 404".
      step_fail "the release does not carry $3"
      echo "error: $RELEASE has no $3." >&2
      ;;
    *)
      step_fail "could not fetch $3"
      echo "error: could not download $1." >&2
      sed 's/^/       /' "$TMPDIR_DL/curl.err" >&2 || true
      ;;
  esac
  exit 1
}

download "$TARBALL_URL" "$TMPDIR_DL/$TARBALL" "linux/$ARCH build ($TARBALL)"
download "$CHECKSUMS_URL" "$TMPDIR_DL/checksums.txt" "checksums.txt"
# The signature is optional in the sense that an old release may not have
# one, but its absence has to be noticed rather than passed over.
if ! curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
    -o "$TMPDIR_DL/checksums.txt.sig" -w '%{http_code}' "$SIGNATURE_URL" \
    2>"$TMPDIR_DL/curl.err" >/dev/null; then
  rm -f "$TMPDIR_DL/checksums.txt.sig"
fi

step_done "fetched $BIN_NAME $FETCH_WHAT ($(file_size "$TMPDIR_DL/$TARBALL"))"

# The signature is what makes the release ours: the checksum file comes from
# the same place as the binary, so without a signature a tampered release
# would serve a binary and a checksum that agree with each other.
#
# The signature covers the checksum file, and the checksum covers the
# binary, so checking the signature first and the checksum second covers the
# whole chain from the key to the binary on disk.
step_start "verifying the release signature"
verify_signature() {
  if ! command -v openssl >/dev/null 2>&1; then
    step_fail "openssl is required to verify the release"
    echo "error: openssl is required to verify the release signature." >&2
    echo "       Install it (openssl 3.0 or newer), or set" >&2
    echo "       OMAKAIJU_ALLOW_UNSIGNED=1 to install without checking." >&2
    exit 1
  fi

  printf '%s\n' "$RELEASE_PUBLIC_KEY" >"$TMPDIR_DL/release-key.pub.pem"

  # -rawin is what makes this an Ed25519 signature over the file's bytes;
  # it needs OpenSSL 3.0 or newer.
  if ! openssl pkeyutl -verify -rawin -pubin \
      -inkey "$TMPDIR_DL/release-key.pub.pem" \
      -in "$TMPDIR_DL/checksums.txt" \
      -sigfile "$TMPDIR_DL/checksums.txt.sig" >/dev/null 2>"$TMPDIR_DL/openssl.err"; then
    step_fail "the release signature does not check out"
    echo "error: the release signature does not check out." >&2
    sed 's/^/       /' "$TMPDIR_DL/openssl.err" >&2 || true
    echo "       The download was not installed." >&2
    exit 1
  fi
}

# The releases published before signing carry no signature. They can still be
# installed, but only when the user says so: a release that has no signature
# is not authenticated, and pretending otherwise would make the check theatre.
if [ -f "$TMPDIR_DL/checksums.txt.sig" ]; then
  verify_signature
  step_done "the release is signed by the project's key"
elif [ "${OMAKAIJU_ALLOW_UNSIGNED:-}" = "1" ]; then
  step_warn "the release is unsigned; OMAKAIJU_ALLOW_UNSIGNED=1, so installing unchecked"
  warn "this release is not signed; installing it unchecked."
else
  step_fail "the release carries no signature"
  echo "error: $RELEASE carries no signature, so it cannot be verified." >&2
  echo "       Newer releases are signed. To install this one anyway, set" >&2
  echo "       OMAKAIJU_ALLOW_UNSIGNED=1." >&2
  exit 1
fi

step_start "verifying the checksum"

# -F, because the tarball name is data, not a regular expression: the dots
# must not match arbitrary characters. Exactly one line has to name the
# tarball -- a truncated or duplicated checksums.txt should fail rather than
# verify whichever line happened to match.
if matches="$(grep -F -c -- "$TARBALL" "$TMPDIR_DL/checksums.txt" 2>/dev/null)"; then
  count="$matches"
else
  count=0
fi
if [ "$count" -ne 1 ]; then
  step_fail "checksums.txt does not list $TARBALL exactly once"
  echo "error: checksums.txt does not list $TARBALL exactly once (found $count)." >&2
  exit 1
fi

# The subshell runs sha256sum where the checksum file's relative path makes
# sense; its "OK"/"FAILED" chatter is kept out of the step line.
if ! ( cd "$TMPDIR_DL" && grep -F -- "$TARBALL" checksums.txt | sha256sum -c - ) \
    >"$TMPDIR_DL/sha256sum.out" 2>&1; then
  step_fail "the download does not match the signed checksums"
  echo "error: checksum verification failed for $TARBALL." >&2
  sed 's/^/       /' "$TMPDIR_DL/sha256sum.out" >&2 || true
  exit 1
fi
step_done "the download matches the signed checksums"

step_start "installing $BIN_NAME"

tar xzf "$TMPDIR_DL/$TARBALL" -C "$TMPDIR_DL" "$BIN_NAME"

mkdir -p "$PREFIX/bin"

# Stage the new binary beside the target and rename it into place. The
# rename is atomic within a filesystem, so the binary is never missing: a
# failed install leaves the old one working rather than leaving the user
# with nothing.
STAGED="$PREFIX/bin/.${BIN_NAME}.new.$$"

if ! install -m 0755 "$TMPDIR_DL/$BIN_NAME" "$STAGED"; then
  step_fail "could not write to $PREFIX/bin"
  echo "error: could not write to $PREFIX/bin. Check the permissions." >&2
  exit 1
fi

# Ask the staged binary what it is before the rename. This is the version
# that will be installed, and checking it here means a release that does not
# contain the build its tag promises never replaces a working install.
INSTALLED_TAG=""
if raw_version="$("$STAGED" --version 2>/dev/null)"; then
  version_line="$(printf '%s\n' "$raw_version" | head -n 1)"
  # The binary prints "omakaiju v0.10.0"; keep the version token as it is.
  INSTALLED_TAG="${version_line##* }"
fi
INSTALLED_VERSION="${INSTALLED_TAG#v}"

# A pinned install must actually install the version asked for. "latest"
# pointing at an older release was silent exactly because nobody compared the
# two; comparing the normalised versions makes that loud.
if [ -n "$VERSION" ] && [ -n "$INSTALLED_VERSION" ] &&
   [ "$INSTALLED_VERSION" != "${VERSION#v}" ]; then
  step_fail "the downloaded binary is not $VERSION"
  echo "error: asked for $VERSION but the downloaded binary reports \"${version_line:-nothing}\"." >&2
  echo "       Refusing to install it; $PREFIX/bin/$BIN_NAME was left untouched." >&2
  exit 1
fi

mv -f "$STAGED" "$PREFIX/bin/$BIN_NAME"
# The staged name is gone now; clearing it keeps cleanup from touching the
# binary that was just put in place.
STAGED=""
INSTALL_OK=1

if [ -n "$INSTALLED_TAG" ]; then
  step_done "installed $BIN_NAME $INSTALLED_TAG (linux/$ARCH) at $PREFIX/bin/$BIN_NAME"
else
  step_done "installed $PREFIX/bin/$BIN_NAME"
fi

# The binary is in place but might not run: a wrong architecture, a missing
# loader, a truncated download. Saying so now beats the user finding out
# when they try to launch it. (The version probe above is the same test, so
# an empty INSTALLED_TAG is exactly the "did not run" case.)
if [ -z "$INSTALLED_TAG" ]; then
  echo "    NB: $PREFIX/bin/$BIN_NAME is installed but did not run." >&2
  echo "    Check that the release matches this machine's architecture." >&2
fi

case ":$PATH:" in
  *":$PREFIX/bin:"*) ;;
  *) echo "    NB: $PREFIX/bin is not on your PATH — add it to your shell profile." ;;
esac
