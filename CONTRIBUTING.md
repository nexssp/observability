# Contributing to Nexss Observability

Nexss Observability is deliberately lightweight and modular. Contributions should improve correctness, clarity, portability, or measured performance without expanding the public API unnecessarily.

Before opening a pull request, run:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go test -run='^$' -bench=. -benchmem ./...
```

A change to a public interface requires a compatibility explanation and tests. A performance claim requires a benchmark on a documented Go version and hardware.

## Local pre-commit

Install pre-commit:

    pre-commit install or prek install

Hooks run before commit and push.
