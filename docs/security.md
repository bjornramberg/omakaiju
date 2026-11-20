# Security

## The releases are signed

Every release since v0.13.0 carries a signature over its checksum file, and
the installer checks that signature before it installs anything. The checksum
file comes from the same place as the binary, so on its own it proves only
that the download arrived intact; the signature is what says the release is
ours. A release that was tampered with — even one that serves a checksum
file matching its tampered binary — is refused.

The public key is pinned in the installer script itself, so the trust does
not depend on the server that served the files. It is also published as
[`RELEASE_KEY.pub.pem`](../RELEASE_KEY.pub.pem), so it can be compared against
another copy of it. The private key lives in the repository's secrets and
never leaves the release workflow.

The step-by-step install and manual verification commands are in [Install](install.md).
