# Kizu Conformance Tests

`v0_1.json` carries the Kizu v0.1 language core and `v0_2.json` the v0.2 stdlib
prototypes. Both are read by the same driver.

The current Go implementation reads this manifest from `cmd/kizu/conformance_test.go`.
Any future compiler implementation should run the same manifest and produce the
same pass/fail results before replacing the Go implementation.

## Case Modes

- `run`: `kizu check <path>` must pass, then `kizu run <path>` -- which builds a
  native executable and runs it -- must match `stdout`.
- `check`: `kizu check <path>` must pass. Use this for static-boundary examples.
- `error`: `command <path>` must fail and output `stderr_contains`.

## Pending Entries

A case may carry `pending` with a reason:

```json
{ "name": "enum", "mode": "run", "pending": "native drops the enum name from print" }
```

`run` builds a native executable and runs it (ADR-0083), so a case whose
backend support is missing cannot pass yet. A pending case is checked for
*still failing*: once it starts passing, the test fails and asks for the entry
to be removed. The list therefore cannot outlive the gaps it names.

## Coverage Rule

Every `.kizu` file under `examples/` and `examples/negative/` must appear exactly once
across the manifests. The Go conformance test enforces this so new examples cannot silently
fall out of the reusable compiler test corpus.

## Module Fixtures

`modules/basic` is the first reusable multi-file package fixture. The current Go
tests resolve its `kizu.toml` module graph and parse every source file. Full
multi-file import resolution, visibility checking, and diagnostics are tracked
by GitHub Issues.
