# Local Development & Hacking

Requires Go 1.27+ on a Unix-like system.

```bash
make build    # Build binary to ./omakaiju (static, like the release)
make run      # Build and launch
make test     # Run test suite
make race     # Run test suite with race detector
make lint     # Run go vet
make install  # Install to $(PREFIX)/bin, honouring DESTDIR
make clean    # Remove build artifacts
```

`make build` produces the same static binary the release publishes, and stamps
the version it was built from: the release version on a tagged checkout, and
the distance from that tag otherwise.

Alternatively, install directly via Go:

```bash
go install github.com/bjornramberg/omakaiju/cmd/omakaiju@latest
```

A binary installed that way has no version stamped into it and says `dev`.

## Automated Fuzz Testing

Crucial text-parsing and UI rendering paths (key matching, input sanitization, text truncation, fuzzy finding) are fuzzed against edge cases:

```bash
go test -run=XXX -fuzz=FuzzEmphasizeText -fuzztime=60s ./internal/ui/...
```

Fuzzing crashers are automatically written to `testdata/fuzz/` and replayed on subsequent test runs.
