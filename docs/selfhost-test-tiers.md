# Selfhost Test Tiers

This document defines the test tiers for Kizu-owned selfhost compiler work.
The goal is to keep daily development fast while preserving explicit,
bootstrap-critical selfhost checks.

## Daily Gate

Daily validation is the default path:

```sh
go test ./...
pre-commit run --all-files
```

This tier must stay comfortably below two minutes on a warm developer machine.
It runs ordinary unit tests, command smokes, conformance checks, and the
lightweight lexer/parser parity harnesses. It does not run heavyweight selfhost
integration gates by default.

Measured locally on 2026-05-21 after #503:

| Command | Elapsed |
| --- | ---: |
| `go test ./cmd/kizu -count=1` | 31.7s |
| `pre-commit run --all-files` | 33.5s |

## Aggregate Selfhost Oracle

The aggregate oracle is the bootstrap preflight tier:

```sh
just selfhost-oracle
```

It runs with `KIZU_RUN_SELFHOST_ORACLE=1` and compares Go-owned behavior against
Kizu-owned lexer, parser, source, resolver, type, ownership, IR, and backend
oracle paths. This is intentionally explicit because it interprets the selfhost
package repeatedly and is not suitable for every edit-save-test loop.

Measured locally during #456/#503:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | about 295s |

## Direct Heavyweight Gates

Direct heavyweight gates are for debugging one selfhost stage without running
the whole aggregate oracle:

```sh
just selfhost-integration-gates
```

This tier runs with `KIZU_RUN_SELFHOST_GATES=1`. It should not be chained after
`just selfhost-oracle` in routine preflight, because the aggregate oracle already
executes the same stage gate functions. Use it when a specific resolver, type,
ownership, IR, or backend gate needs focused output.

Measured locally during #456/#503:

| Command | Elapsed |
| --- | ---: |
| `KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -timeout=10m -v` | 55.8s |

## Bootstrap Preflight

Bootstrap-oriented work uses:

```sh
just selfhost-bootstrap-preflight
```

This runs the aggregate selfhost oracle once through `just selfhost-switch-gate`
and then runs cache smoke coverage. It intentionally does not run
`just selfhost-integration-gates` after the aggregate oracle, to avoid duplicate
selfhost package loading, checking, and interpreted `RunEntry` execution.

## Cost Model

The heavyweight gates are slow because each one currently performs this full
sequence:

```text
loadPackageProgram("selfhost")
checkProgram(program)
interp.New(...).RunEntry(program, ...)
```

During #456 this cost was about 55s per heavy gate, while the aggregate oracle
was about 295s. Repeating that path in the daily gate pushed `cmd/kizu` close to
Go's default ten-minute package timeout.

## Current Decision

Do not cache or share checked selfhost program state between these tests yet.
The interpreter path has observable I/O and artifact side effects, and the
backend gates write `target/selfhost`. A shared setup would need a stronger
contract for immutable program state, isolated target directories, and entry
point side effects.

The accepted policy for now is:

- daily gate: fast default `go test ./...`;
- aggregate oracle: explicit bootstrap/preflight command;
- direct heavyweight gates: explicit debugging command;
- no routine recipe runs both aggregate oracle and direct heavyweight gates.

Future optimization can revisit checked-package caching, isolated artifact
directories, or smaller interpreted corpora once those contracts are explicit.
