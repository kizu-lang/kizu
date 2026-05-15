# Kizu Conformance Tests

`v0_1.json` is the reusable conformance manifest that started as the Kizu v0.1
language-core suite and now also carries v0.2 stdlib prototype coverage.

The current Go implementation reads this manifest from `cmd/kizu/conformance_test.go`.
A future self-host compiler should run the same manifest and produce the same
pass/fail results before replacing the Go implementation.

## Case Modes

- `run`: `kizu check <path>` must pass, then `kizu run <path>` must match `stdout`.
- `check`: `kizu check <path>` must pass. Use this for static-boundary examples.
- `error`: `command <path>` must fail and output `stderr_contains`.

## Coverage Rule

Every `.kizu` file under `examples/` and `examples/negative/` must appear exactly once
in `v0_1.json`. The Go conformance test enforces this so new examples cannot silently
fall out of the self-host compiler test corpus.

## Module Fixtures

`modules/v0_3.json` is the reusable module conformance manifest. The Go runner
executes every case with `kizu check <path>`. The self-host runner reads the same
manifest and executes pass-case root sources through the current frontend
skeleton.

`modules/basic` is the first positive multi-file package fixture. Negative
fixtures, such as `modules/missing_import`, `modules/private_module_access`,
`modules/private_type_leak`, and `modules/private_field_construction`, keep
expected diagnostic substrings next to the fixture manifest.

Module visibility diagnostics must include a primary location and a related
location so the same expected diagnostic corpus can be reused by the future
self-host compiler.
