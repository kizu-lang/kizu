# Selfhost Test Tiers

This document defines the test tiers for Kizu-owned selfhost compiler work.
The goal is to keep daily development fast while preserving explicit,
build-critical selfhost checks.

## Policy update (2026-08-12, ADR-0081)

Selfhost sources are written in full Kizu, and the Go backend (stage0) is the
only thing that builds a selfhost executable: `just selfhost-native`. The
self-compilation comparison is gone along with the backend that performed it.

There is no nightly. Every gate runs on every push and pull request, because a
gate whose red surfaces the next morning is weaker than one that surfaces in the
PR. That constrains what the gates may cost: they are kept fast rather than
deferred. Runner measurements at the time of the change: stage0 build 36s (cached
when neither the compiler nor the Kizu it compiles changed), the seven parity
gates 41s together.

`--opt` stays on the stage0 build deliberately. Dropping it takes the build from
79s to 3s locally, but the resulting binary is ~2.5x slower on compute-bound
gates (production 4.3s -> 10.7s), which nets out to roughly 12s saved on CI and a
slower binary for everyone running gates locally.

## Gate Taxonomy

Use the smallest gate that proves the changed boundary. Do not chain oracle or
direct heavyweight gates into routine hosted artifact validation.

| Change type | Primary gate | Tier | Notes |
| --- | --- | --- | --- |
| Ordinary Go/compiler edit | `go test ./...` and `pre-commit run --all-files` | daily | Default validation path. |
| Existing selfhost artifact validation | `just selfhost-fast-gate` | daily selfhost loop | Reuses `target/selfhost/stage0-native/selfhost`; does not rebuild the stage0 binary. |
| Selfhost source checkpoint | `just selfhost-production-from-scratch` | checkpoint | Rebuilds through `just selfhost-native`, then runs the fast artifact gates. |
| Control-flow lowering behavior | `just selfhost-control-flow-execution` | daily, and whenever branch/merge/field lowering changes | Renders selfhost MIR to LLVM through the interpreter, links each gate with a driver, and runs it. Behaviour is checked by exit code, so a wrong branch edge, arm merge, or field hop fails outright. Needs no staged artifact. |
| Run/test executable lowering, backend executable metadata, or native selfhost source behavior | `just selfhost-native-source-gate` | focused source-path gate | Builds the selfhost package from Kizu source as a native executable and exercises checked-AST executable artifacts. |
| Production ownership switch review | `just selfhost-switch-gate` | production switch | Runs production-from-scratch, native-source-gate, package skeleton checks, and project/type/ownership package tests. It intentionally excludes `just selfhost-oracle`. |
| Frontend parity or Go/Kizu oracle evidence | `just selfhost-oracle` | explicit oracle | Functional parity gate; logs but does not enforce the wall-time budget. |
| Oracle performance or budget changes | `just selfhost-oracle-budget` | explicit performance gate | Same oracle with budget enforcement enabled. |
| Debugging one interpreted selfhost stage or the CLI contract | `just selfhost-integration-gates` or `just selfhost-cli-gate` | focused debugging | Direct heavyweight interpreter gates; not routine preflight. |
| Package dependency numeric identity blockers | raw `go test` with `KIZU_RUN_SELFHOST_PACKAGE_IDENTITY=1` | last-resort debugging | Two `interp.New(...).RunEntry(...)` gates that pin the next interpreted-consumer blocker. Measured together at 2043s. No `just` recipe on purpose; write full output to a log file. |

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

The budget is what makes the pre-commit hook usable: `.pre-commit-config.yaml` runs
`go test ./...` unconditionally under a 10-minute timeout. A heavyweight gate that
reaches this tier does not slow the hook down, it makes the hook impossible to
pass, and `--no-verify` becomes routine. That is what happened to the three
`interp.New(...).RunEntry(...)` numeric-package-collector gates: they grew to
2287s of the 2383s `cmd/kizu` took, so the hook timed out regardless of whether
the code was correct. They are opt-in behind
`KIZU_RUN_SELFHOST_PACKAGE_IDENTITY=1` as of this measurement.

Measured locally on 2026-08-11, after moving the std lexer/parser parity
harnesses from the Go interpreter to native harness binaries and sharing one
`go build` CLI binary across command smokes:

| Command | Elapsed |
| --- | ---: |
| `go test ./... -count=1` | 81.7s |
| `go test ./cmd/kizu -count=1` | 81.4s |

The same commands immediately before those two changes measured 160.2s for
`go test ./cmd/kizu -count=1`: the three selfhost-corpus parity tests took
61.7s interpreting the std lexer/parser, and 78 command smokes paid a
`go run .` link each. The parity harnesses now compile with the Go backend and
run as native binaries (about 1.5s per corpus pass), and `TestMain` builds the
smoke CLI once. No single test exceeds 6s in the current profile; the remaining
time is a long tail of per-test `loadPackageProgram` + `checkProgram` reloads
and real CLI/clang work.

Measured locally on 2026-07-30:

| Command | Elapsed |
| --- | ---: |
| `go test ./... -count=1` | 87.5s |
| `go test ./cmd/kizu -count=1` | 86.5s |

The same commands before the three gates became opt-in, for comparison:

| Command | Elapsed |
| --- | ---: |
| `go test ./... -count=1` | ~2400s |
| `go test ./cmd/kizu -count=1` | 2382.8s |

Measured locally on 2026-05-21 after #503, when the tier was still inside budget:

| Command | Elapsed |
| --- | ---: |
| `go test ./cmd/kizu -count=1` | 31.7s |
| `pre-commit run --all-files` | 33.5s |

The gap between 31.7s and 86.5s is unexplained drift, not a single gate. Anything
that pushes this tier past two minutes should be measured and either made opt-in
or fixed, rather than absorbed.

## Aggregate Selfhost Oracle

The aggregate oracle is the checkpoint tier:

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
oracle must not destroy a previously built
`target/selfhost/stage0-native/selfhost` artifact used by fast hosted gates.

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

Direct heavyweight gates debug one selfhost stage without running the whole
aggregate oracle:

```sh
just selfhost-integration-gates
just selfhost-cli-gate
just selfhost-control-flow-execution
```

They run with `KIZU_RUN_SELFHOST_GATES=1` and drive selfhost entry points through
`interp.New(...).RunEntry(...)`. Use them when a specific resolver, type,
ownership, IR, or CLI contract gate needs focused output; do not chain them after
`just selfhost-oracle` in routine preflight.

Run them raw, with a budget, and keep full logs:

```sh
mkdir -p target/selfhost/reports
KIZU_RUN_SELFHOST_GATES=1 go test -timeout=30m ./cmd/kizu \
  -run 'TestSelfhostCLIContractGate$' -count=1 -v \
  > target/selfhost/reports/cli-gate-debug.log 2>&1
```

Do not pipe these through `tail` or start background watcher shells. Hidden
output makes timeout and failure diagnosis slower than the gate itself.

## Stage0 Native Build

`just selfhost-native` is the required gate (ADR-0081). It compiles the selfhost
package to a native executable with the Go backend and smokes `check`, `run`, and
`fmt` through the result. Nothing else produces a selfhost binary, so a red here
means selfhost cannot be built at all.

```sh
just selfhost-native
```

Every parity gate below runs against `target/selfhost/stage0-native/selfhost` and
requires this to have been run first. The `*-from-scratch` recipes chain it.

Measured on the CI runner (macos-14) on 2026-08-12:

| Step | Elapsed |
| --- | ---: |
| stage0 native build | 36s |
| seven parity gates together | 41s |


## Production Boundary Gate

The #461 production boundary gate runs only the stage0-native artifact for the
#458 command surface:

```sh
just selfhost-production-gate
just selfhost-fast-gate
```

It requires the same `target/selfhost/stage0-native/selfhost` executable and passing
stage0-native binary as the corpus gate. It does not rebuild it. From a clean
workspace, run:

```sh
just selfhost-production-from-scratch
```

That command runs `just selfhost-native` followed by `just selfhost-fast-gate`.
`just selfhost-fast-gate` reuses the existing stage0-native binary and runs
`selfhost-production-gate`, `selfhost-corpus-gate`, `selfhost-parse-parity-gate`,
`selfhost-check-parity-gate`, `selfhost-fmt-parity-gate`,
`selfhost-run-parity-gate`, and `selfhost-test-parity-gate` without rebuilding.
Go is present only as the
explicit stage0 native compiler in the first step and as the test
runner for the gate; the production commands are direct executions of the
hosted artifact.

`just selfhost-native-source-gate` is a focused #752 source-path gate. It builds
the selfhost package from Kizu source as a native executable, runs `check
selfhost`, then emits and executes representative `run` and
`test` artifacts through `selfhost::backend::executable`. The gate validates the
artifact metadata marker
`executable_lowering selfhost::backend::executable checked-ast` and the root
host runtime path `target/selfhost/selfhost.host.ll`. This proves the Kizu-owned
checked-AST lowering path works, including a run fixture with a local string
`let`, `print(local)`, and multiple statements. The stage0-native executable
path is validated through the direct bounded renderer before artifact emission.

Measured locally on 2026-05-21 during #461:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-production-gate` | 0.31s |
| `just selfhost-production-from-scratch` | 61.0s |

Measured locally on 2026-05-22 during #569:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-run-parity-gate` after `just selfhost-native` | 0.52s |
| `just selfhost-production-from-scratch` including run parity | about 58s |

Measured locally on 2026-05-22 during #570:

| Command | Elapsed |
| --- | ---: |
| `just selfhost-test-parity-gate` after `just selfhost-native` | 0.69s |
| `just selfhost-production-from-scratch` including run/test parity | 61.6s |

## Supported Corpus Gate

The #460 supported corpus gate uses the stage0-native artifact:

```sh
just selfhost-native
just selfhost-corpus-gate
```

`just selfhost-corpus-gate` intentionally does not call
`runSelfhostBootstrap` internally. It requires
`target/selfhost/stage0-native/selfhost` plus a passing
stage0-native binary, then runs only the active
manifest rows from `selfhost/tests/supported-corpus.tsv`.

Measured locally on 2026-05-21 during #460:

| Command | Elapsed |
| --- | ---: |
| initial `TestSelfhostSupportedCorpusGate` with an embedded build | 60.6s |
| corpus execution inside that report | 9ms |
| `just selfhost-corpus-gate` after separating the build | 0.36s |
| `just selfhost-corpus-gate-from-scratch` | 61.2s |

The 60s path was not caused by corpus size. It came from rebuilding and running
the whole build inside the corpus test. Profiling that path
showed the same interpreter-heavy cost as the direct backend artifact
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
stage0 native path instead of the interpreter. That keeps the Go compiler layer
thin, leaves direct focused gates available for debugging, and avoids silently
sharing mutable interpreter state.

The accepted policy for now is:

- daily gate: fast default `go test ./...`;
- aggregate oracle: explicit preflight command with a 60s local budget;
- aggregate oracle budget: explicit performance gate, not a routine switch gate;
- direct heavyweight interpreter gates: explicit debugging commands;
- run tape/render interpreter internals: no routine `just` recipe; raw command
  only for a measured blocker with full logs and an explicit time budget;
- backend artifact contract: explicit stage0-native gate, not the daily loop;
- artifact fast gate: routine selfhost CLI parity loop after `just selfhost-native`;
- production switch gate: production-from-scratch plus native source-path and
  package checks, without aggregate oracle;
- aggregate production checks: one pass through `selfhost::pipeline_oracle`;
- CLI contract checks: one pass through `selfhost::cli_contract_gate`;
- no routine recipe runs both aggregate oracle and direct heavyweight gates.

Future optimization can revisit interpreter value representation, checked-package
caching, isolated artifact directories, or smaller interpreted corpora once
those contracts are explicit.
