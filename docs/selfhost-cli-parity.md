# Selfhost CLI Parity Scope

Issue #497 tracks hosted selfhost CLI parity after the first bootstrap path.
This document records the release-scope decision after #525.

The hosted selfhost artifact is not a general-purpose replacement for Go
`cmd/kizu` yet. It must not add hidden Go parser, Go interpreter, Go test
runner, or Go build-cache fallback paths. Every additional command needs a
bounded child issue with command arguments, input/output paths, stdout/stderr,
exit codes, unsupported behavior, and hosted-artifact validation.

## Current Hosted Surface

The current hosted stage2 artifact supports these command slices:

| Slice | Scope | Validation |
| --- | --- | --- |
| `check selfhost` | Minimum compiler package check from #458 | `just selfhost-production-gate` |
| `stage selfhost` | Stage2 artifact materialization from #459 | `just selfhost-production-gate` |
| `check examples/hello.kizu` | Positive supported corpus row from #460 | `just selfhost-corpus-gate` |
| `check examples/negative/moved_value.kizu` | Negative supported corpus row from #460 | `just selfhost-corpus-gate` |
| `parse selfhost/tests/cli/parse_ok_minimal.kizu` | #525 positive parse fixture only | `just selfhost-parse-parity-gate` |
| `parse selfhost/tests/cli/parse_invalid_missing_expr.kizu` | #525 negative parse fixture only | `just selfhost-parse-parity-gate` |

`selfhost/tests/cli/parse-parity.tsv` is the #525 parse parity manifest. It
records command args, fixture paths, expected exit codes, and checked-in
stdout/stderr golden paths. The gate runs through
`target/selfhost/stage2/selfhost` and records `go.cmd-kizu-fallback none`.

Unsupported commands, wrong arity, and arguments beginning with `-` remain
deterministic usage/unsupported paths with exit code `64`.

## Deferred Slices

These #497 slices are explicitly deferred from the current hosted artifact
release scope:

| Slice | Decision | Reason | Next child issue shape |
| --- | --- | --- | --- |
| `run <file>` | Deferred | Runtime execution responsibility is not defined for the no-Go hosted path. Adding it now would require either an interpreter fallback or a new runtime execution surface. | Define one fixture command contract, runtime ownership boundary, stdout/stderr, exit codes, and hosted-artifact validation. |
| `kizu test <file>` | Deferred | Test discovery, assertion reporting, and program execution still depend on semantics outside the hosted compiler artifact. | Define one test fixture contract and the no-Go test runner responsibility before implementation. |
| cache/status, cache/prune, why-rebuild, cache artifact commands | Deferred | Hosted artifact cache ownership, persistence, pruning, and no-op rebuild semantics are not designed. | Split into cache command issues with explicit cache directory, artifact paths, mutation rules, stdout/stderr, and cache-size acceptance checks. |
| non-critical diagnostic/display parity commands | Deferred | Diagnostic formatting must be command-specific to avoid broad parity without contracts. | Create one command/display contract per slice, including exact stdout/stderr and unsupported behavior. |

No general-purpose release artifact replacement is claimed while these slices
are deferred. Future work must either implement one bounded child issue at a
time or update this decision with a linked issue and documented release scope.
