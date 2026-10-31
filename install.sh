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
SIGNATURE_URL="$URL_BASE/checksums.txt.sig"

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

download "$TARBALL_URL" "$TMPDIR_DL/$TARBALL" "linux/$ARCH build ($TARBALL)"
download "$CHECKSUMS_URL" "$TMPDIR_DL/checksums.txt" "checksums.txt"
# The signature is optional in the sense that an old release may not have
# one, but its absence has to be noticed rather than passed over.
if ! curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
    -o "$TMPDIR_DL/checksums.txt.sig" -w '%{http_code}' "$SIGNATURE_URL" \
    2>"$TMPDIR_DL/curl.err" >/dev/null; then
  rm -f "$TMPDIR_DL/checksums.txt.sig"
fi

# The signature is what makes the release ours: the checksum file comes from
# the same place as the binary, so without a signature a tampered release
# would serve a binary and a checksum that agree with each other.
#
# The signature covers the checksum file, and the checksum covers the
# binary, so checking the signature first and the checksum second covers the
# whole chain from the key to the binary on disk.
echo "==> Verifying signature"
verify_signature() {
  if ! command -v openssl >/dev/null 2>&1; then
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
elif [ "${OMAKAIJU_ALLOW_UNSIGNED:-}" = "1" ]; then
  echo "==> WARNING: this release is not signed; installing it unchecked."
else
  echo "error: $RELEASE carries no signature, so it cannot be verified." >&2
  echo "       Newer releases are signed. To install this one anyway, set" >&2
  echo "       OMAKAIJU_ALLOW_UNSIGNED=1." >&2
  exit 1
fi

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
