# Kizu Conformance Tests

`v0_1.json` is the reusable conformance manifest that started as the Kizu v0.1
language-core suite and now also carries v0.2 stdlib prototype coverage.

The current Go implementation reads this manifest from `cmd/kizu/conformance_test.go`.
Any future compiler implementation should run the same manifest and produce the
same pass/fail results before replacing the Go implementation.

## Case Modes

- `run`: `kizu check <path>` must pass, then `kizu run <path>` must match `stdout`.
- `check`: `kizu check <path>` must pass. Use this for static-boundary examples.
- `error`: `command <path>` must fail and output `stderr_contains`.

## Coverage Rule

Every `.kizu` file under `examples/` and `examples/negative/` must appear exactly once
in `v0_1.json`. The Go conformance test enforces this so new examples cannot silently
fall out of the reusable compiler test corpus.

## Module Fixtures

`modules/basic` is the first reusable multi-file package fixture. The current Go
tests resolve its `kizu.toml` module graph and parse every source file. Full
multi-file import resolution, visibility checking, and diagnostics are tracked
by GitHub Issues.
