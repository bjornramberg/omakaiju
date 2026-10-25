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
set -eu

REPO="bjornramberg/omakaiju"
BIN_NAME="omakaiju"

# NOTE: this must stay in the "${1:-...}" form -- under `set -u`, dash
# evaluates $1 inside "${OMAKAIJU_VERSION:-$1}" even when the env var is
# unset, which kills piped installs (`curl ... | sh`) with
# "line 20: $1: unbound variable".
VERSION="${1:-${OMAKAIJU_VERSION:-}}"
PREFIX="${OMAKAIJU_PREFIX:-$HOME/.local}"

if [ -n "$VERSION" ]; then
  # The release tags carry a leading "v" (v0.10.0), while users naturally
  # type 0.10.0 -- and /releases/download/0.10.0/... does not exist. Accept
  # both forms by normalising to the tagged form before building the URL.
  VERSION="v${VERSION#v}"
  URL_BASE="https://github.com/$REPO/releases/download/$VERSION"
  RELEASE="release $VERSION"
else
  # 'latest/download' always resolves to the newest release.
  URL_BASE="https://github.com/$REPO/releases/latest/download"
  RELEASE="the latest release"
fi

case "$(uname -s)" in
  Linux) ;;
  *) echo "error: prebuilt binaries are only provided for Linux." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64)       ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

TARBALL="${BIN_NAME}_linux_${ARCH}.tar.gz"
TARBALL_URL="$URL_BASE/$TARBALL"
CHECKSUMS_URL="$URL_BASE/checksums.txt"

command -v curl >/dev/null 2>&1 || { echo "error: curl is required." >&2; exit 1; }

TMPDIR_DL="$(mktemp -d)"
STAGED=""
# Set only once the install has actually completed; see cleanup.
INSTALL_OK=""

# Every exit path goes through here: a failed download, a failed checksum, an
# interrupt, or a normal finish. Both pieces of state are removed together,
# and there is only ever one trap -- installing a second trap over this one
# (as an earlier version of this script did) is how whole download
# directories leaked into /tmp on every install, interrupted or not.
cleanup() {
  status=$?
  trap - EXIT INT TERM
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

echo "==> Fetching $BIN_NAME for linux/$ARCH (release: ${VERSION:-latest})"

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
      echo "error: $RELEASE has no $3." >&2
      ;;
    *)
      echo "error: could not download $1." >&2
      sed 's/^/       /' "$TMPDIR_DL/curl.err" >&2 || true
      ;;
  esac
  exit 1
}

# The checksum file is fetched from the same release as the binary, so it
# proves the download arrived intact but cannot prove the release is
# authentic. Signing the releases would close that gap; for now the
# checksum is what there is to check.
download "$TARBALL_URL" "$TMPDIR_DL/$TARBALL" "linux/$ARCH build ($TARBALL)"
download "$CHECKSUMS_URL" "$TMPDIR_DL/checksums.txt" "checksums.txt"

echo "==> Verifying checksum"
(
  cd "$TMPDIR_DL"

  # -F, because the tarball name is data, not a regular expression: the dots
  # must not match arbitrary characters. Exactly one line has to name the
  # tarball -- a truncated or duplicated checksums.txt should fail rather
  # than verify whichever line happened to match.
  if matches="$(grep -F -c -- "$TARBALL" checksums.txt 2>/dev/null)"; then
    count="$matches"
  else
    count=0
  fi
  if [ "$count" -ne 1 ]; then
    echo "error: checksums.txt does not list $TARBALL exactly once (found $count)." >&2
    exit 1
  fi

  grep -F -- "$TARBALL" checksums.txt | sha256sum -c - || {
    echo "error: checksum verification failed for $TARBALL." >&2
    exit 1
  }
)

tar xzf "$TMPDIR_DL/$TARBALL" -C "$TMPDIR_DL" "$BIN_NAME"

mkdir -p "$PREFIX/bin"

# Stage the new binary beside the target and rename it into place. The
# rename is atomic within a filesystem, so the binary is never missing: a
# failed install leaves the old one working rather than leaving the user
# with nothing.
STAGED="$PREFIX/bin/.${BIN_NAME}.new.$$"

if ! install -m 0755 "$TMPDIR_DL/$BIN_NAME" "$STAGED"; then
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
  echo "error: asked for $VERSION but the downloaded binary reports \"${version_line:-nothing}\"." >&2
  echo "       Refusing to install it; $PREFIX/bin/$BIN_NAME was left untouched." >&2
  exit 1
fi

mv -f "$STAGED" "$PREFIX/bin/$BIN_NAME"

if [ -n "$INSTALLED_TAG" ]; then
  echo "==> Installed $BIN_NAME $INSTALLED_TAG (linux/$ARCH) at $PREFIX/bin/$BIN_NAME"
else
  echo "==> Installed $PREFIX/bin/$BIN_NAME"
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

INSTALL_OK=1
echo "==> Done. Run '$BIN_NAME' to start."
