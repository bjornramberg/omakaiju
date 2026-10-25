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

.PHONY: all build run install clean lint test race

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
