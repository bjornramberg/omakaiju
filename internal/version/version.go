// Package version carries the release the binary was built from. It is
// stamped at build time — GoReleaser injects the release tag, the Makefile
// injects `git describe` — so an unstamped source build falls back to a
// neutral value instead of guessing.
package version

import "strings"

// Version is the release this binary was built from, e.g. "0.9.1". The
// default marks a build that never went through the stamped build paths.
var Version = "dev"

// Display formats the release for a human.
//
// GoReleaser stamps the bare patch number ("0.9.1"), while the Makefile
// stamps raw `git describe` output ("v0.9.1-dirty"), so the leading v is
// normalised rather than doubled. A build that was never stamped says so
// plainly rather than pretending to be a release.
func Display() string {
	name := strings.TrimSpace(Version)
	if name == "" || name == "dev" {
		return "dev"
	}
	return "v" + strings.TrimPrefix(name, "v")
}
