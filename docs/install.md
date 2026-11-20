# Install

For how the signature establishes trust, see [Security](security.md).

Install the latest prebuilt release with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh
```

The installer detects your architecture (`amd64`/`arm64`), verifies that the release is signed by the project's key, verifies the download against the signed checksums, and atomically places the binary in `~/.local/bin`. It prints the version it installed, so you can see at a glance what you got. On Omarchy it also offers to make `omakaiju` the default file manager.

To pin a version instead of taking the latest:

```bash
curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh -s -- v0.13.0
```

The version can also be given as `OMAKAIJU_VERSION=v0.13.0`, and `OMAKAIJU_PREFIX` chooses where it is installed (`~/.local` by default).

### Verifying a release

Verifying a release by hand:

```bash
TAG=v0.13.0
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/checksums.txt" -o checksums.txt
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/checksums.txt.sig" -o checksums.txt.sig
openssl pkeyutl -verify -rawin -pubin -inkey ../RELEASE_KEY.pub.pem \
  -in checksums.txt -sigfile checksums.txt.sig
```

The installer needs `openssl` 3.0 or newer for this check. The releases
published before v0.13.0 carry no signature; they can still be installed,
but only when asked for explicitly:

```bash
OMAKAIJU_ALLOW_UNSIGNED=1 curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh -s -- v0.12.0
```

### System Requirements

* **OS:** Linux (prebuilt binaries) or Unix-like systems (built from source)
* **Font:** A [Nerd Font](https://www.nerdfonts.com/) for the pane and archive icons. It is optional: set `OMAKAIJU_NO_ICONS=1` to turn the icons off on a terminal without the font.
* **openssl** 3.0 or newer, to verify the release signature

### Manual Asset Downloads

Pre-built binaries (`omakaiju_linux_amd64` / `omakaiju_linux_arm64`) and release tarballs are hosted on [GitHub Releases](https://github.com/bjornramberg/omakaiju/releases) along with `checksums.txt`. Name the version you want, so the download is the one you meant:

```bash
TAG=v0.10.0
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/omakaiju_linux_amd64" -o omakaiju
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/checksums.txt" -o checksums.txt
grep 'omakaiju_linux_amd64$' checksums.txt | sha256sum -c -
chmod +x omakaiju
```

`releases/latest/download/<file>` works too, and serves whatever GitHub currently marks as the latest release.
