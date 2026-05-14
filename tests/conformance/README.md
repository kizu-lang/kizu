# Kizu Conformance Tests

`v0_1.json` is the reusable conformance manifest for Kizu v0.1.

The current Go implementation reads this manifest from `cmd/kizu/conformance_test.go`.
A future self-host compiler should run the same manifest and produce the same pass/fail
results before replacing the Go implementation.

## Case Modes

- `run`: `kizu check <path>` must pass, then `kizu run <path>` must match `stdout`.
- `check`: `kizu check <path>` must pass. Use this for static-boundary examples.
- `error`: `command <path>` must fail and output `stderr_contains`.

## Coverage Rule

Every `.kizu` file under `examples/` and `examples/negative/` must appear exactly once
in `v0_1.json`. The Go conformance test enforces this so new examples cannot silently
fall out of the self-host compiler test corpus.
