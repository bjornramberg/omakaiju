# Project practices

These are the rules that outlive a single change. Read them before editing.

## Destructive actions

Read [docs/destructive-actions.md](docs/destructive-actions.md). In short: name
the action, say what is affected, make `Enter` the safe answer, require two
steps for permanent deletion, and ask before replacing a destination (or report
the replacement). Questions must stay readable at `ui.MinWidth`.

## Before finishing

Run the checks the repository already defines:

```bash
gofmt -w <changed files>
go test ./...
go vet ./...
```

`make test`, `make race`, and `make lint` run the same checks through the
project's targets. Do not add a new test framework or a new runtime dependency
for a change that the standard library and the existing test helpers can cover.
