// Package version carries the release the binary was built from. It is
// stamped at build time — GoReleaser injects the release tag, the Makefile
// injects `git describe` — so an unstamped source build falls back to a
// neutral value instead of guessing.
package version

// Version is the release this binary was built from, e.g. "0.9.1". The
// default marks a build that never went through the stamped build paths.
var Version = "dev"
