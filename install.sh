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
  URL_BASE="https://github.com/$REPO/releases/download/$VERSION"
else
  # 'latest/download' always resolves to the newest release.
  URL_BASE="https://github.com/$REPO/releases/latest/download"
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

TMPDIR_DL="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_DL"' EXIT

command -v curl >/dev/null 2>&1 || { echo "error: curl is required." >&2; exit 1; }

echo "==> Fetching $BIN_NAME for linux/$ARCH (release: ${VERSION:-latest})"

# The checksum file is fetched from the same release as the binary, so it
# proves the download arrived intact but cannot prove the release is
# authentic. Signing the releases would close that gap; for now the
# checksum is what there is to check.
curl -fsSL --retry 3 -o "$TMPDIR_DL/$TARBALL" "$TARBALL_URL"
curl -fsSL --retry 3 -o "$TMPDIR_DL/checksums.txt" "$CHECKSUMS_URL"

echo "==> Verifying checksum"
(
  cd "$TMPDIR_DL"
  grep "$TARBALL" checksums.txt | sha256sum -c -
)

tar xzf "$TMPDIR_DL/$TARBALL" -C "$TMPDIR_DL" "$BIN_NAME"

mkdir -p "$PREFIX/bin"

# Stage the new binary beside the target and rename it into place. The
# rename is atomic within a filesystem, so the binary is never missing: a
# failed install leaves the old one working rather than leaving the user
# with nothing.
STAGED="$PREFIX/bin/.${BIN_NAME}.new.$$"
trap 'rm -f "$STAGED"' EXIT INT TERM

if ! install -m 0755 "$TMPDIR_DL/$BIN_NAME" "$STAGED"; then
  echo "error: could not write to $PREFIX/bin. Check the permissions." >&2
  exit 1
fi
mv -f "$STAGED" "$PREFIX/bin/$BIN_NAME"
trap - EXIT INT TERM

echo "==> Installed $PREFIX/bin/$BIN_NAME"

# The binary is in place but might not run: a wrong architecture, a missing
# loader, a truncated download. Saying so now beats the user finding out
# when they try to launch it.
if ! "$PREFIX/bin/$BIN_NAME" --version >/dev/null 2>&1; then
  echo "    NB: $PREFIX/bin/$BIN_NAME is installed but did not run." >&2
  echo "    Check that the release matches this machine's architecture." >&2
fi

case ":$PATH:" in
  *":$PREFIX/bin:"*) ;;
  *) echo "    NB: $PREFIX/bin is not on your PATH — add it to your shell profile." ;;
esac

echo "==> Done. Run '$BIN_NAME' to start."
