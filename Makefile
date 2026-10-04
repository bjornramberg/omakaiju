BINARY := omakaiju
PREFIX := /usr/local

.PHONY: all build run install clean lint test

all: build

build:
	go build -o $(BINARY) ./cmd/omakaiju

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
