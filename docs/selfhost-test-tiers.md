# Selfhost Test Tiers

This document defines the test tiers for Kizu-owned selfhost compiler work.
The goal is to keep daily development fast while preserving explicit,
bootstrap-critical selfhost checks.

## Gate Taxonomy

Use the smallest gate that proves the changed boundary. Do not chain oracle or
direct heavyweight gates into routine hosted artifact validation.

| Change type | Primary gate | Tier | Notes |
| --- | --- | --- | --- |
| Ordinary Go/compiler edit | `go test ./...` and `pre-commit run --all-files` | daily | Default validation path. |
| Existing stage2 selfhost artifact validation | `just selfhost-fast-gate` | daily selfhost loop | Reuses `target/selfhost/stage2/selfhost`; does not rebuild bootstrap artifacts. |
| Selfhost source checkpoint | `just selfhost-production-from-scratch` | checkpoint | Rebuilds through `just selfhost-bootstrap`, then runs the fast hosted artifact gates. |
| Backend/runtime artifact contract | `just selfhost-backend-artifact-gate` | focused stage0-native gate | Builds a native stage0 selfhost executable, runs `stage selfhost`, then reuses the textual metadata, runtime, host, and hosted CLI smoke checks. |
| Run/test executable lowering, backend executable metadata, or native selfhost source behavior | `just selfhost-native-source-gate` | focused source-path gate | Builds the selfhost package from Kizu source as a native executable and exercises checked-AST executable artifacts. |
| Production ownership switch review | `just selfhost-switch-gate` | production switch | Runs production-from-scratch, native-source-gate, package skeleton checks, and project/type/ownership package tests. It intentionally excludes `just selfhost-oracle`. |
| Frontend parity or Go/Kizu oracle evidence | `just selfhost-oracle` | explicit oracle | Functional parity gate; logs but does not enforce the wall-time budget. |
| Oracle performance or budget changes | `just selfhost-oracle-budget` | explicit performance gate | Same oracle with budget enforcement enabled. |
| Debugging one interpreted selfhost stage or the CLI contract | `just selfhost-integration-gates` or `just selfhost-cli-gate` | focused debugging | Direct heavyweight interpreter gates; not routine preflight. |
| Run tape / renderer internals that only exist as interpreter entry points | raw `go test` with the explicit `KIZU_RUN_SELFHOST_RUN_*` env | last-resort debugging | No `just` recipe on purpose. Prefer hosted stage2 parity, native-source, or production-from-scratch gates first. |

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
just selfhost-oracle-budget
```

It runs with `KIZU_RUN_SELFHOST_ORACLE=1` and compares Go-owned behavior against
Kizu-owned production lexer, parser, source, resolver, type, ownership, IR, and
backend oracle paths. Production resolver/type/ownership/IR/backend checks run
through a single Kizu-owned pipeline oracle so that the selfhost package is
loaded, checked, and interpreted once for those stages.

The aggregate oracle has a 60 second local budget reported by
`TestSelfhostOracleRunner`. `just selfhost-oracle-budget` enforces that budget
with `KIZU_ENFORCE_SELFHOST_ORACLE_BUDGET=1`; the ordinary
`just selfhost-oracle` recipe fails on parity mismatches but only logs a budget
overrun. This keeps functional oracle signal separate from interpreter
performance work. Both recipes set `GOGC=1000` for this allocation-heavy
interpreted path. The broader std lexer/parser examples and selfhost-source
parity harnesses remain in ordinary `go test ./cmd/kizu` tests; the aggregate
oracle must not duplicate them.

The pipeline oracle writes its fixed `target/selfhost` artifact paths while it
runs, but the Go harness isolates that target by moving any existing
`target/selfhost` aside and restoring it afterward. A slow or failing aggregate
oracle must not destroy a previously bootstrapped
`target/selfhost/stage2/selfhost` artifact used by fast hosted gates.

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
| `just selfhost-oracle` | 84.0s |

Measured locally on 2026-05-22 during #568:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 54.8s |

Measured locally on 2026-05-22 during #570:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 57.0s |

Measured locally on 2026-05-22 during #574:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.6s |

Measured locally on 2026-05-22 during #575:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 57.1s |

Measured locally on 2026-05-22 during #576:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 57.1s |

Measured locally on 2026-05-22 during #577:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.9s |

Measured locally on 2026-05-22 during #578:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 55.2s |

Measured locally on 2026-05-22 during #579:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 55.0s |

Measured locally on 2026-05-22 during #586:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.5s |

Measured locally on 2026-05-22 during #588:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.3s |

Measured locally on 2026-05-22 during #590:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 55.3s |

Measured locally on 2026-05-22 during #592:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 55.3s |

Measured locally on 2026-05-22 during #594:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.2s |

Measured locally on 2026-05-22 during #596:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.5s |

Measured locally on 2026-05-22 during #598:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.7s |

Measured locally on 2026-05-22 during #600:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 57.6s |

Measured locally on 2026-05-22 during #602:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 56.3s |

Measured locally on 2026-05-22 during #604:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-oracle` | 57.0s |

## Direct Heavyweight Gates

Direct heavyweight gates are for debugging one selfhost stage without running
the whole aggregate oracle:

```sh
just selfhost-integration-gates
just selfhost-backend-artifact-gate
just selfhost-cli-gate
```

This tier runs with `KIZU_RUN_SELFHOST_GATES=1`. It should not be chained after
`just selfhost-oracle` in routine preflight. Use it when a specific resolver,
type, ownership, IR, one-pass pipeline, backend artifact, CLI contract, or
format-driver gate needs focused output.

The format driver facts/lowering gates also execute selfhost internals through
`interp.New(...).RunEntry(...)`. They are env-gated by `KIZU_RUN_SELFHOST_GATES=1`
and should be run as raw focused commands only when pinning #1165 / #1162
blockers, not as part of default `go test ./cmd/kizu`.

The run tape and run renderer internal gates are heavier than ordinary focused
debugging. They call `interp.New(...).RunEntry(...)` on selfhost backend entry
points such as `selfhost::backend::run_tape_gate::run_tape_lowering_gate`,
build source-driven facts, and run compiled-MIR lowering through the Go
interpreter. They are not routine validation and intentionally have no `just`
recipes. Use them only when a measured blocker cannot be pinned by the hosted
stage2 parity gates, `just selfhost-native-source-gate`, or
`just selfhost-production-from-scratch`.

When one of these interpreter-only internals is necessary, run the raw command
with a clear budget and keep full logs, for example:

```sh
mkdir -p target/selfhost/reports
KIZU_RUN_SELFHOST_RUN_TAPE=1 \
  go test -timeout=30m ./cmd/kizu \
  -run 'TestSelfhostRunTapeLoweringGate$' -count=1 -v \
  > target/selfhost/reports/run-tape-debug.log 2>&1
```

Do not pipe these gates through `tail` or start multiple background watcher
shells. Hidden output makes timeout and failure diagnosis slower than the gate
itself.

`just selfhost-integration-gates` intentionally excludes the CLI contract and
backend artifact gates. The CLI gate still uses an interpreted CLI contract
entry, while the backend artifact gate now builds a native stage0 selfhost
executable and validates artifacts generated by `stage selfhost`; both have
their own explicit recipes:

```sh
just selfhost-backend-artifact-gate
just selfhost-cli-gate
```

`TestSelfhostBackendArtifactGate` records the old BackendArtifactGate contract
inventory in `target/selfhost/reports/backend-artifact-stage0-native.txt`:

```text
contract.report artifact-paths-and-byte-counts
contract.textual-llvm required-runtime-cli-executable-fragments
contract.textual-llvm forbids-fixed-cli-fixture-paths
contract.textual-llvm forbids-source-shape-dispatch
contract.metadata selfhost-checked-package-no-go-fallback
contract.runtime-storage textual-metadata-link-smoke
contract.host-capability textual-metadata-link-smoke
contract.hosted-cli link-and-smoke
```

The gate applies that contract to native-source stage0 artifacts; bootstrap and
production gates then carry the same artifacts through stage1/stage2.

Measured locally during #456/#503:

| Command | Elapsed |
| --- | ---: |
| `KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -timeout=10m -v` | 55.8s |

Measured locally on 2026-05-31 after moving BackendArtifactGate to the explicit
stage0 native bootstrap path:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-backend-artifact-gate` | 10.14s real / 9.97s go test |

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
just selfhost-bootstrap
```

The preflight runs `just selfhost-switch-gate` and then runs cache smoke
coverage. After #461 and #752, the switch gate includes
production-from-scratch, the native selfhost source gate, package skeleton
checks, and the small Go package checks required by the switch review. The
aggregate selfhost oracle is kept as an explicit parity/performance preflight
instead of being chained into the switch gate, because it executes the
interpreted production pipeline and has a separate wall-time budget.

`just selfhost-bootstrap` is the #459 stage0-stage1-stage2 comparison runner. It
uses the explicit Go native stage0 bootstrap compiler to build a selfhost
executable, emits the supported artifact set through that executable's `stage
selfhost`, then links and runs stage1 and stage2 in hosted no-Go mode. It
compares stdout/stderr/exit codes and deterministic SHA-256 artifact
fingerprints, and writes `target/selfhost/reports/bootstrap.txt`.

Measured locally on 2026-05-21 during #459 bootstrap runner work:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-bootstrap` | 61.4s |

## Production Boundary Gate

The #461 production boundary gate runs only the hosted stage2 artifact for the
#458 command surface:

```sh
just selfhost-production-gate
just selfhost-fast-gate
```

It requires the same `target/selfhost/stage2/selfhost` executable and passing
bootstrap report as the corpus gate. It does not rebuild artifacts. From a clean
workspace, run:

```sh
just selfhost-production-from-scratch
```

That command runs `just selfhost-bootstrap` followed by `just selfhost-fast-gate`.
`just selfhost-fast-gate` reuses the existing stage2 artifact and runs
`selfhost-production-gate`, `selfhost-corpus-gate`, `selfhost-parse-parity-gate`,
`selfhost-check-parity-gate`, `selfhost-fmt-parity-gate`,
`selfhost-run-parity-gate`, and `selfhost-test-parity-gate` without rebuilding.
Go is present only as the
explicit stage0 native bootstrap compiler in the first step and as the test
runner for the gate; the production commands are direct executions of the
hosted artifact.

`just selfhost-native-source-gate` is a focused #752 source-path gate. It builds
the selfhost package from Kizu source as a native executable, runs `check
selfhost` and `stage selfhost`, then emits and executes representative `run` and
`test` artifacts through `selfhost::backend::executable`. The gate validates the
artifact metadata marker
`executable_lowering selfhost::backend::executable checked-ast` and the root
host runtime path `target/selfhost/selfhost.host.ll`. This proves the Kizu-owned
checked-AST lowering path works, including a run fixture with a local string
`let`, `print(local)`, and multiple statements. The hosted stage2 executable
path is validated through the direct bounded renderer before artifact emission.

Measured locally on 2026-05-21 during #461:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-production-gate` | 0.31s |
| `just selfhost-production-from-scratch` | 61.0s |

Measured locally on 2026-05-22 during #569:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-run-parity-gate` after bootstrap | 0.52s |
| `just selfhost-production-from-scratch` including run parity | about 58s |

Measured locally on 2026-05-22 during #570:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-test-parity-gate` after bootstrap | 0.69s |
| `just selfhost-production-from-scratch` including run/test parity | 61.6s |

## Supported Corpus Gate

The #460 supported corpus gate uses the hosted stage2 artifact produced by the
bootstrap runner:

```sh
just selfhost-bootstrap
just selfhost-corpus-gate
```

`just selfhost-corpus-gate` intentionally does not call
`runSelfhostBootstrap` internally. It requires
`target/selfhost/stage2/selfhost` plus a passing
`target/selfhost/reports/bootstrap.txt` report, then runs only the active
manifest rows from `selfhost/tests/supported-corpus.tsv`.

Measured locally on 2026-05-21 during #460:

| Command | Elapsed |
| --- | ---: |
| initial `TestSelfhostSupportedCorpusGate` with embedded bootstrap | 60.6s |
| corpus execution inside that report | 9ms |
| `just selfhost-corpus-gate` after bootstrap separation | 0.36s |
| `just selfhost-corpus-gate-from-scratch` | 61.2s |

The 60s path was not caused by corpus size. It came from rebuilding and running
the stage0-stage1-stage2 bootstrap inside the corpus test. Profiling that path
showed the same interpreter-heavy bootstrap cost as the direct backend artifact
gate: CPU samples were dominated by interpreter evaluation, allocation/copying,
and GC, with allocation profile top entries in `internal/interp.(*Env).Define`,
`evalStructLiteralExpr`, `NewEnv`, and `qualifiedName`.

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
`runtime.duffzero`, `runtime.mallocgc`, and `runtime.gcDrain`. As of
2026-05-31, `TestSelfhostBackendArtifactGate` no longer calls
`selfhost::backend_artifact_gate` through the interpreter; it builds a native
stage0 selfhost executable, runs `stage selfhost`, and then applies the same
textual artifact, metadata, runtime, host, and hosted CLI smoke checks to the
generated files. A local `just selfhost-backend-artifact-gate` run completed in
10.14s real time (9.97s inside `go test`).

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

During #568, profiling the aggregate oracle showed interpreter allocation
pressure in lexical environments, struct values, std wrapper calls, and
qualified namespace rendering. The current mitigation returns unborrowed child
environments to pools and caches immutable qualified AST names per interpreter
run. The aggregate oracle remains one interpreted production pass and must stay
under its 60s budget before adding more oracle coverage.

## Current Decision

Do not cache or share checked selfhost program state between tests yet. The
interpreter path has observable I/O and artifact side effects, and the backend
gates write `target/selfhost`. A shared setup would need a stronger contract for
immutable program state, isolated target directories, and entry point side
effects.

Instead, the aggregate oracle uses one explicit selfhost production pipeline
entry point. The backend artifact contract uses the explicit native stage0
bootstrap path instead of the interpreter. That keeps the Go compiler layer
thin, leaves direct focused gates available for debugging, and avoids silently
sharing mutable interpreter state.

The accepted policy for now is:

- daily gate: fast default `go test ./...`;
- aggregate oracle: explicit bootstrap/preflight command with a 60s local budget;
- aggregate oracle budget: explicit performance gate, not a routine switch gate;
- direct heavyweight interpreter gates: explicit debugging commands;
- run tape/render interpreter internals: no routine `just` recipe; raw command
  only for a measured blocker with full logs and an explicit time budget;
- backend artifact contract: explicit stage0-native gate, not the daily loop;
- hosted artifact fast gate: routine selfhost CLI parity loop after bootstrap;
- production switch gate: production-from-scratch plus native source-path and
  package checks, without aggregate oracle;
- aggregate production checks: one pass through `selfhost::pipeline_oracle`;
- CLI contract checks: one pass through `selfhost::cli_contract_gate`;
- no routine recipe runs both aggregate oracle and direct heavyweight gates.

Future optimization can revisit interpreter value representation, checked-package
caching, isolated artifact directories, or smaller interpreted corpora once
those contracts are explicit.
