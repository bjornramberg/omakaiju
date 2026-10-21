BINARY := omakaiju
PREFIX := /usr/local
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X github.com/bjornramberg/omakaiju/internal/version.Version=$(VERSION)

.PHONY: all build run install clean lint test race

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/omakaiju

run: build
	./$(BINARY)

install: build
	install -Dm755 $(BINARY) $(PREFIX)/bin/$(BINARY)

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
