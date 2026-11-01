BINARY := omakaiju
PREFIX := /usr/local
DESTDIR ?=

# The version stamped into the binary. A commit that is exactly a tag gets the
# bare version GoReleaser stamps ("0.10.0"), so a local build and a released
# binary report the same thing. Any other commit keeps git describe's
# "<tag>-<commits>-g<sha>" ("0.10.0-4-g2b83849"). A tree with tracked edits
# is marked "-dirty" in either case, so a build that is not the tagged source
# never claims to be the release. A repository with no tags — or a source tree
# that is not a git checkout — says "dev". The value never contains spaces, so
# it is safe to pass through -X.
VERSION := $(shell \
	if tag=$$(git describe --tags --exact-match 2>/dev/null) && [ -n "$$tag" ]; then \
		printf '%s\n' "$$tag" | head -n1 | sed 's/^v//' | tr -d '[:space:]'; \
		git diff --quiet HEAD -- 2>/dev/null || printf '%s' '-dirty'; \
	elif desc=$$(git describe --tags 2>/dev/null) && [ -n "$$desc" ]; then \
		printf '%s' "$$desc" | sed 's/^v//' | tr -d '[:space:]'; \
		git diff --quiet HEAD -- 2>/dev/null || printf '%s' '-dirty'; \
	else \
		printf '%s' dev; \
	fi)
LDFLAGS := -s -w -X github.com/bjornramberg/omakaiju/internal/version.Version=$(VERSION)

.PHONY: all build run install clean lint test race check fmt tidy fuzz arm64 vuln static shell installer

all: build

# CGO_ENABLED=0 and -trimpath make the local build byte-for-byte the kind of
# static binary the release publishes, without the build machine's paths
# baked in.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/omakaiju

run: build
	./$(BINARY)

# DESTDIR lets a package build stage the install into a temporary root. The
# directory is created with mkdir -p and the file copied with install -m, so
# this works with BSD and macOS install as well as GNU.
install: build
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	install -m755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

clean:
	rm -f $(BINARY)

lint:
	go vet ./...

test:
	go test ./...

# The race detector finds what no other tool can: a program that corrupts
# its own state while looking perfectly correct in every other check.
race:
	go test -race ./...

# The full gate: everything CI runs, in the same order. The release workflow
# runs this too, so a release cannot ship something CI would have refused.
check: fmt tidy lint test race fuzz arm64 vuln static shell installer

# The tree has to be gofmt'd. A file that is not is a failure, not a note.
fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

# The module files are part of the build: if they need tidying, the tree is
# out of date and the build would not be what go.mod says it is.
tidy:
	go mod tidy -diff

# A short run of each fuzzer. They cover the paths that take text from the
# user or from the filesystem.
fuzz:
	go test -run=XXX -fuzz=FuzzEmphasizeText -fuzztime=30s ./internal/ui/...
	go test -run=XXX -fuzz=FuzzTruncate -fuzztime=30s ./internal/ui/...
	go test -run=XXX -fuzz=FuzzTrimPathLeft -fuzztime=30s ./internal/ui/...
	go test -run=XXX -fuzz=FuzzSanitizeName -fuzztime=30s ./internal/ui/...
	go test -run=XXX -fuzz=FuzzMatch -fuzztime=30s ./internal/fuzzy/...
	go test -run=XXX -fuzz=FuzzFilter -fuzztime=30s ./internal/fuzzy/...

# The release publishes linux/arm64 as well as amd64, and the runner is
# amd64: cross-compile so an arm64-only breakage is caught here.
arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath ./...

# Known vulnerabilities in the standard library and in the modules we depend
# on. The tool version is pinned: a scan that changes with the day is a scan
# nobody can reproduce.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# The linter that finds the mistakes the compiler does not. Pinned for the
# same reason as the scanner above.
static:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...

# The installer and its test are shell; they are checked like any other code.
shell:
	@command -v shellcheck >/dev/null 2>&1 || { \
		echo "shellcheck is not installed; the CI runner has it"; exit 1; \
	}
	shellcheck install.sh scripts/installer_test.sh

# The installer is the front door: it puts a binary on the PATH, and it only
# does so when the release's signature checks out.
installer:
	sh scripts/installer_test.sh
