#!/bin/sh
# Tests the installer against a release served locally.
#
# The property under test: the installer installs a binary only when the
# release's signature checks out against the public key pinned in the
# script. A tampered binary, a tampered checksum file, a signature from
# another key and a release with no signature at all must all be refused.
#
# The presentation is tested too, because it is the part a user sees and the
# part that must stay out of the way: a pipe gets plain lines with no escape
# sequences, a terminal gets tick-boxes that leave no spinner frame behind,
# NO_COLOR and the Nerd Font guess behave, and a failed run leaves nothing in
# the temporary directory.
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

# A valid signed release for the presentation tests below.
( cd "$REL" && sha256sum omakaiju_linux_amd64.tar.gz >checksums.txt )
sign "$WORK/test-key.pem"

# 6. When stdout is not a terminal the output is plain: no escape sequences,
#    no spinner frames, and still an account of every step and the version.
out="$(install_run 0 "a signed release installs for the plain output test")"
python3 - "$WORK/out.txt" <<'PY' || fail "the non-terminal output is not plain"
import sys

raw = open(sys.argv[1], "rb").read()
assert b"\x1b" not in raw, "escape sequence in non-terminal output"
assert b"\r" not in raw, "carriage return in non-terminal output"
text = raw.decode("utf-8")
assert not any(0x2800 <= ord(c) <= 0x28FF for c in text), \
    "spinner frame in non-terminal output"
for want in (
    "terminal looks fine",
    "fetched omakaiju v9.9.9 for linux/amd64",
    "signed by the project's key",
    "matches the signed checksums",
    "installed omakaiju v9.9.9",
):
    assert want in text, "missing outcome: " + want
PY
pass "non-terminal output is plain and reports every step"

# A pty runner, because the terminal presentation only happens when stdout
# really is a terminal. It captures the bytes the installer wrote, including
# the carriage returns the spinner uses, so the tests can see the final
# screen rather than a tidied version of it.
cat >"$WORK/pty_run.py" <<'PY'
import errno
import os
import pty
import select
import subprocess
import sys
import time

script, outfile, want = sys.argv[1], sys.argv[2], int(sys.argv[3])
args = sys.argv[4:]

master, slave = pty.openpty()
proc = subprocess.Popen(
    ["sh", script] + args,
    stdin=subprocess.DEVNULL,
    stdout=slave,
    stderr=slave,
)
os.close(slave)

chunks = []
deadline = time.time() + 120
while True:
    ready, _, _ = select.select([master], [], [], 1.0)
    if not ready:
        if proc.poll() is not None:
            try:
                data = os.read(master, 4096)
            except OSError:
                break
            if not data:
                break
            chunks.append(data)
            continue
        if time.time() > deadline:
            proc.kill()
            sys.exit("pty run timed out")
        continue
    try:
        data = os.read(master, 4096)
    except OSError as exc:
        if exc.errno == errno.EIO:
            break
        raise
    if not data:
        break
    chunks.append(data)

code = proc.wait()
with open(outfile, "wb") as f:
    f.write(b"".join(chunks))
if code != want:
    sys.stderr.write("pty run exited %d, want %d\n" % (code, want))
    sys.exit(1)
PY

pty_run() {
  # pty_run OUTFILE EXPECTED_EXIT [ENV...]
  outfile="$1"
  want="$2"
  shift 2
  prefix="$WORK/prefix"
  rm -rf "$prefix"
  env "$@" OMAKAIJU_PREFIX="$prefix" OMAKAIJU_BASE_URL="$BASE" \
    python3 "$WORK/pty_run.py" "$WORK/install.sh" "$outfile" "$want" "$TAG"
}

# 7. A terminal run draws tick-boxes and ends clean: the spinner's line is
#    always replaced, so no half-drawn line or braille frame is left on the
#    screen, and the output ends on a new line.
pty_run "$WORK/tty.txt" 0 TERM=xterm-256color TERM_PROGRAM=WezTerm
python3 - "$WORK/tty.txt" <<'PY' || fail "the terminal output is not a clean tick list"
import sys

text = open(sys.argv[1], "rb").read().decode("utf-8", "replace")
assert "✓" in text, "no tick marks in terminal output"
# A pty turns each newline into CRLF, so drop the empty tail after the CR
# before picking the segment the spinner left behind.
rendered = "\n".join(
    ([part for part in line.split("\r") if part] or [""])[-1]
    for line in text.split("\n")
)
assert not any(0x2800 <= ord(c) <= 0x28FF for c in rendered), \
    "a spinner frame was left on the screen"
assert "\r" not in rendered
assert text.endswith("\n"), "terminal output does not end on a new line"
assert not text.endswith("\r"), "terminal output ends mid-redraw"
assert "\x1b[32m" in text, "terminal output is not coloured"
installed = [line for line in rendered.split("\n") if "installed omakaiju" in line]
assert installed, "the install outcome is missing"
assert "✓" in installed[-1], "the install outcome is not ticked"
PY
pass "terminal output is a clean tick list"

# 8. NO_COLOR turns the colour off without turning the tick-boxes off.
pty_run "$WORK/tty-nocolor.txt" 0 TERM=xterm-256color TERM_PROGRAM=WezTerm NO_COLOR=1
python3 - "$WORK/tty-nocolor.txt" <<'PY' || fail "NO_COLOR did not turn colour off"
import sys

text = open(sys.argv[1], "rb").read().decode("utf-8", "replace")
for colour in ("\x1b[31m", "\x1b[32m", "\x1b[33m"):
    assert colour not in text, "colour escape with NO_COLOR set"
assert "✓" in text, "NO_COLOR removed the tick-boxes too"
PY
pass "NO_COLOR turns colour off and leaves the tick-boxes"

# 9. The Nerd Font warning is a guess: it appears for a terminal known not to
#    draw the icons, stays quiet for one known to work, and is never a reason
#    to refuse the install.
install_run 0 "an unlikely terminal is warned about the font" \
  -u TERM_PROGRAM -u LC_TERMINAL -u KITTY_WINDOW_ID -u WEZTERM_EXECUTABLE \
  -u WEZTERM_PANE -u GHOSTTY_RESOURCES_DIR -u WT_SESSION \
  TERM=linux >/dev/null
grep -q "may not have a Nerd Font" "$WORK/out.txt" ||
  fail "the kernel console was not warned about the font"
grep -q "OMAKAIJU_NO_ICONS=1" "$WORK/out.txt" ||
  fail "the font warning did not point at OMAKAIJU_NO_ICONS"
pass "an unlikely terminal is warned about the font"

install_run 0 "a known terminal is not warned about the font" \
  TERM=xterm-256color TERM_PROGRAM=WezTerm >/dev/null
if grep -q "Nerd Font" "$WORK/out.txt"; then
  fail "a known terminal was warned about the font"
fi
pass "a known terminal is not warned about the font"

install_run 0 "the font warning is suppressed when icons are off" \
  -u TERM_PROGRAM -u LC_TERMINAL -u KITTY_WINDOW_ID -u WEZTERM_EXECUTABLE \
  -u WEZTERM_PANE -u GHOSTTY_RESOURCES_DIR -u WT_SESSION \
  TERM=linux OMAKAIJU_NO_ICONS=1 >/dev/null
if grep -q "Nerd Font" "$WORK/out.txt"; then
  fail "the font warning ignored OMAKAIJU_NO_ICONS"
fi
pass "the font warning is suppressed when icons are off"

# A multiplexer hides which terminal is really underneath: the check has
# nothing to identify, so it stays quiet rather than guessing.
install_run 0 "an unidentified terminal is not guessed about" \
  -u TERM_PROGRAM -u LC_TERMINAL -u KITTY_WINDOW_ID \
  -u WEZTERM_EXECUTABLE -u WEZTERM_PANE -u GHOSTTY_RESOURCES_DIR \
  -u WT_SESSION TERM=screen-256color >/dev/null
if grep -q "Nerd Font" "$WORK/out.txt"; then
  fail "an unidentified terminal was guessed about"
fi
pass "an unidentified terminal is not guessed about"

# 10. A failed signature still cleans up: the temporary download directory
#     and the staged binary are gone, and nothing was installed.
mkdir -p "$WORK/tmp"
printf 'extra\n' >>"$REL/checksums.txt"
out="$(install_run 1 "a failed signature leaves nothing behind" TMPDIR="$WORK/tmp")"
grep -q "signature does not check out" "$WORK/out.txt" ||
  fail "the failed signature was not named"
if [ -n "$(ls -A "$WORK/tmp")" ]; then
  fail "the temporary download directory was left behind"
fi
if find "$WORK/prefix" -name '.omakaiju.new.*' 2>/dev/null | grep -q .; then
  fail "the staged binary was left behind"
fi
if [ -e "$WORK/prefix/bin/omakaiju" ]; then
  fail "a refused release left a binary behind"
fi
pass "a failed signature leaves no temporary files behind"

echo "installer tests passed"
