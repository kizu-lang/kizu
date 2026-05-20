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
oracle paths. Production resolver/type/ownership/IR/backend checks run through a
single Kizu-owned pipeline oracle so that the selfhost package is loaded,
checked, and interpreted once for those stages.

Measured locally during #456/#503:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | about 295s |

Measured locally on 2026-05-21 after #506:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 77.8s |

Measured locally on 2026-05-21 during #458 CLI work:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 83.5s |

## Direct Heavyweight Gates

Direct heavyweight gates are for debugging one selfhost stage without running
the whole aggregate oracle:

```sh
just selfhost-integration-gates
just selfhost-cli-gate
```

This tier runs with `KIZU_RUN_SELFHOST_GATES=1`. It should not be chained after
`just selfhost-oracle` in routine preflight. Use it when a specific resolver,
type, ownership, IR, backend, one-pass pipeline, or CLI contract gate needs
focused output.

`just selfhost-integration-gates` intentionally excludes the CLI contract gate.
The CLI gate stages backend artifacts and overlaps with the backend/pipeline
checks, so it has its own explicit recipe:

```sh
just selfhost-cli-gate
```

Measured locally during #456/#503:

| Command | Elapsed |
| --- | ---: |
| `KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -timeout=10m -v` | 55.8s |

Measured locally on 2026-05-21 after #506:

| Command | Elapsed |
| --- | ---: |
| `KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu -run TestSelfhostPipelineGate -count=1 -timeout=10m -v` | 56.4s |

Measured locally on 2026-05-21 during #458 CLI work:

| Command | Elapsed |
| --- | ---: |
| initial `TestSelfhostCLIGate` with separate `check selfhost` and `stage selfhost` runs | 114.7s |
| one-pass `just selfhost-cli-gate` contract gate | 59.6s |
| `KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -timeout=10m -v` with hosted artifact CLI smoke | 60.3s |

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

The heavyweight gates are slow because each direct gate performs this full
sequence:

```text
loadPackageProgram("selfhost")
checkProgram(program)
interp.New(...).RunEntry(program, ...)
```

Profiling `TestSelfhostBackendArtifactGate` during #506 showed that one direct
backend gate spent its time inside interpreted selfhost execution, not Go test
startup. The representative command completed in 55.8s. CPU samples were
dominated by interpreter evaluation plus allocation/copying and GC work:
`internal/interp.(*Interpreter).evalExpr`, `evalWhileStmt`, `evalLetStmt`,
`evalFieldCallExpr`, `evalQualifiedUserCall`, `runtime.duffcopy`,
`runtime.duffzero`, `runtime.mallocgc`, and `runtime.gcDrain`.

During #456/#503 this per-gate cost was multiplied across resolver, type,
ownership, IR, and backend production gates, so the aggregate oracle took about
295s. After #506 the aggregate oracle still pays for one full interpreted
selfhost production pipeline, but no longer repeats it per stage; the measured
aggregate cost is about 78-84s.

During #458, profiling `TestSelfhostCLIGate` showed the same pattern. The slow
path was not stdout/stderr, filesystem writes, or clang. The initial test ran
`check selfhost` and `stage selfhost` as two separate interpreted selfhost
passes, then repeated detailed backend artifact validation. CPU samples were
again dominated by interpreter evaluation and allocation/GC work. The gate now
uses one Kizu-owned CLI contract entry and leaves detailed backend artifact and
host-link validation to `TestSelfhostBackendArtifactGate`.

## Current Decision

Do not cache or share checked selfhost program state between tests yet. The
interpreter path has observable I/O and artifact side effects, and the backend
gates write `target/selfhost`. A shared setup would need a stronger contract for
immutable program state, isolated target directories, and entry point side
effects.

Instead, the aggregate oracle uses one explicit selfhost production pipeline
entry point. That keeps the Go compiler layer thin, leaves direct focused gates
available for debugging, and avoids silently sharing mutable interpreter state.

The accepted policy for now is:

- daily gate: fast default `go test ./...`;
- aggregate oracle: explicit bootstrap/preflight command;
- direct heavyweight gates: explicit debugging commands;
- aggregate production checks: one pass through `selfhost::pipeline_oracle`;
- CLI contract checks: one pass through `selfhost::cli_contract_gate`;
- no routine recipe runs both aggregate oracle and direct heavyweight gates.

Future optimization can revisit interpreter value representation, checked-package
caching, isolated artifact directories, or smaller interpreted corpora once
those contracts are explicit.
