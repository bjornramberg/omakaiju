#!/bin/sh
# Tests the installer's signature check against a release served locally.
#
# The property under test: the installer installs a binary only when the
# release's signature checks out against the public key pinned in the
# script. A tampered binary, a tampered checksum file, a signature from
# another key and a release with no signature at all must all be refused.
#
# Run it from the repository root:
#   sh scripts/installer_test.sh
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
WORK="$(mktemp -d)"
PORT="${INSTALLER_TEST_PORT:-8731}"
SERVER=""

cleanup() {
  if [ -n "$SERVER" ]; then
    kill "$SERVER" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

pass() {
  echo "  ok: $1"
}

# The release the fake server offers.
TAG="v9.9.9"
REL="$WORK/download/$TAG"
mkdir -p "$REL"

# A key of our own for the test, so the real signing key is never involved.
openssl genpkey -algorithm ED25519 -out "$WORK/test-key.pem" 2>/dev/null
openssl pkey -in "$WORK/test-key.pem" -pubout -out "$WORK/test-key.pub.pem" 2>/dev/null
# A second key, for the case where the signature is made by someone else.
openssl genpkey -algorithm ED25519 -out "$WORK/other-key.pem" 2>/dev/null

# The binary the release carries.
cat >"$WORK/omakaiju" <<'EOF'
#!/bin/sh
echo "omakaiju v9.9.9"
EOF
chmod 0755 "$WORK/omakaiju"
tar czf "$REL/omakaiju_linux_amd64.tar.gz" -C "$WORK" omakaiju
( cd "$REL" && sha256sum omakaiju_linux_amd64.tar.gz >checksums.txt )

sign() {
  openssl pkeyutl -sign -rawin -inkey "$1" -in "$REL/checksums.txt" -out "$REL/checksums.txt.sig"
}

# The installer, with its pinned key swapped for the test key. The shipped
# script has no way to be told a different key: the key is the trust, and it
# is the one in the script.
awk -v key="$(cat "$WORK/test-key.pub.pem")" '
  /^RELEASE_PUBLIC_KEY=/ {
    print "RELEASE_PUBLIC_KEY=\x27" key "\x27"
    skipping = 1
    next
  }
  skipping && /END PUBLIC KEY/ { skipping = 0; next }
  skipping { next }
  { print }
' "$ROOT/install.sh" >"$WORK/install.sh"
if ! grep -q "MCowBQYDK2VwAyEA" "$WORK/install.sh"; then
  fail "the test key was not written into the installer"
fi

# A local server for the fake release.
( cd "$WORK" && exec python3 -m http.server "$PORT" --bind 127.0.0.1 ) >/dev/null 2>&1 &
SERVER=$!
BASE="http://127.0.0.1:$PORT"

# Wait for the server.
i=0
while [ "$i" -lt 50 ]; do
  if curl -fsS -o /dev/null "$BASE/download/$TAG/checksums.txt" 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.1
done
[ "$i" -lt 50 ] || fail "the local server did not come up"

install_run() {
  # install_run EXPECTED_EXIT DESCRIPTION [ENV...]
  want="$1"
  what="$2"
  shift 2
  prefix="$WORK/prefix"
  rm -rf "$prefix"
  out="$WORK/out.txt"
  set +e
  env "$@" OMAKAIJU_PREFIX="$prefix" OMAKAIJU_BASE_URL="$BASE" \
    sh "$WORK/install.sh" "$TAG" >"$out" 2>&1
  got=$?
  set -e
  if [ "$got" != "$want" ]; then
    echo "--- installer output ---" >&2
    cat "$out" >&2
    fail "$what: exit $got, want $want"
  fi
  echo "$out"
}

# 1. A signed release installs.
sign "$WORK/test-key.pem"
out="$(install_run 0 "a signed release installs")"
[ -x "$WORK/prefix/bin/omakaiju" ] || fail "the binary was not installed"
grep -q "omakaiju v9.9.9" "$WORK/prefix/bin/omakaiju" || fail "the wrong binary was installed"
pass "a signed release installs"

# 2. A binary that changed after the signature was made is refused.
cp "$REL/omakaiju_linux_amd64.tar.gz" "$WORK/good.tar.gz"
printf 'tampered' >>"$WORK/omakaiju"
tar czf "$REL/omakaiju_linux_amd64.tar.gz" -C "$WORK" omakaiju
out="$(install_run 1 "a tampered binary is refused")"
grep -q "checksum verification failed" "$out" || fail "the refusal did not name the checksum"
pass "a tampered binary is refused"
cp "$WORK/good.tar.gz" "$REL/omakaiju_linux_amd64.tar.gz"

# 3. A checksum file that changed after the signature was made is refused.
printf 'extra\n' >>"$REL/checksums.txt"
out="$(install_run 1 "a tampered checksum file is refused")"
grep -q "signature does not check out" "$out" || fail "the refusal did not name the signature"
pass "a tampered checksum file is refused"

# 4. A signature made with a different key is refused.
( cd "$REL" && sha256sum omakaiju_linux_amd64.tar.gz >checksums.txt )
sign "$WORK/other-key.pem"
out="$(install_run 1 "a signature from another key is refused")"
grep -q "signature does not check out" "$out" || fail "the refusal did not name the signature"
pass "a signature from another key is refused"

# 5. A release with no signature at all is refused, unless the user says so.
rm -f "$REL/checksums.txt.sig"
out="$(install_run 1 "an unsigned release is refused")"
grep -q "carries no signature" "$out" || fail "the refusal did not name the missing signature"
pass "an unsigned release is refused"

out="$(install_run 0 "an unsigned release installs when allowed" OMAKAIJU_ALLOW_UNSIGNED=1)"
grep -q "WARNING" "$out" || fail "the unsigned install did not warn"
pass "an unsigned release installs when the user allows it"

echo "installer tests passed"
