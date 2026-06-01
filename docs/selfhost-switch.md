# Selfhost Production Switch Gate

This document defines the review gate for replacing Go-owned compiler paths with
Kizu-owned components. It does not switch production behavior by itself.

The no-Go stage contract is defined in
[`docs/selfhost-bootstrap.md`](selfhost-bootstrap.md).
Selfhost test tiering and timing policy are defined in
[`docs/selfhost-test-tiers.md`](selfhost-test-tiers.md).

## Repeatable Gate

Run this before any PR that proposes using a Kizu-owned component in a production
CLI path:

```sh
just selfhost-switch-gate
```

The command builds the hosted stage2 artifact through the explicit bootstrap
boundary, runs the #458 production commands through that artifact, runs the
supported corpus and bounded CLI parity gates, builds the selfhost package from
Kizu source as a native executable to exercise checked-AST run/test lowering,
and keeps the Go project, type, and ownership packages green. The aggregate
Go/Kizu oracle is intentionally not part of `just selfhost-switch-gate`; it is
an explicit separate preflight because it runs the interpreted selfhost
production pipeline and has an independent wall-time budget.

For frontend switch PRs that need Go/Kizu oracle evidence, also run:

```sh
just selfhost-oracle
```

For oracle performance work, run the budget-enforcing gate:

```sh
just selfhost-oracle-budget
```

For cache or artifact-affecting switch PRs, also run one of:

```sh
just cache-smoke
just perf-cache
just perf-cache-isolated
```

## Switch Matrix

| Component | Kizu-owned source | Production owner | Current status | Switch criteria |
| --- | --- | --- | --- | --- |
| token / lexer | `std::kizu::lexer`, `selfhost::token`, `selfhost::{lexer, lexer_oracle}` | Go lexer | Kizu source component gate | Token kind, literal, byte span, line, and column parity remains green for examples and `selfhost/src`; the selfhost lexer returns a token array. |
| AST / parser | `std::kizu::{ast, parser}`, `selfhost::{ast, parser, parser_oracle}` | Go parser / AST | Kizu source component gate | Arena + NodeId parser parity and parser-error seeds remain green for examples and `selfhost/src`; the selfhost parser consumes token arrays, returns structured `ParseResult` values, and explicitly adapts lower-level untyped failures into typed parser errors. |
| source manager / loader | `selfhost::{source, source_oracle}` | Go project loader | Kizu source component gate | Loading `kizu.toml`, `selfhost/src`, and required std sources uses explicit fs/io capabilities; the source table preserves source ids, file paths, and text while deriving module paths from paths; missing file, invalid manifest, duplicate module, and import cycle diagnostics keep stable spans and related spans. |
| diagnostics / resolver | `selfhost::{diagnostic, resolver, resolver_oracle}` | Go project resolver | Kizu source component gate | The resolver consumes the source table, registers selfhost/std modules, scans top-level declarations into qualified symbols, and keeps missing symbol, duplicate symbol, private access, import cycle diagnostics green. |
| type checker | `selfhost::{types, types_oracle}` | Go type checker | Kizu source component gate | The type checker consumes the resolver source table, registers declared selfhost/std types, validates signature/field/variant/cast/generic-constructor type references, and keeps type-kind, arity, copyability, and stable diagnostic span gates green. |
| ownership / borrow checker | `selfhost::{ownership, ownership_oracle}` | Go ownership checker | oracle-only | Move, borrow, deinit, array, map, string, arena, handle, and borrowed-view gate remains green. |
| interpreter | none | Go interpreter | Go-owned | No switch planned before Kizu compiler frontend can emit a stable execution IR. |
| IR / backend | `selfhost::{ir, backend}` skeleton | Go IR / backend | Go-owned | Requires a separate backend fingerprint and artifact/cache issue before any production switch. |
| build cache / artifacts | none | Go cache / target paths | Go-owned | Requires explicit cache-key, prune, status, no-op rebuild, and artifact-size evidence. |
| #458 selfhost CLI path | `selfhost::{ir, backend}` plus hosted runtime ABI | `target/selfhost/stage2/selfhost` | switched for `check selfhost` and `stage selfhost` | `just selfhost-production-from-scratch` passes; Go remains only in explicit stage0 bootstrap/oracle jobs; general CLI parity remains blocked by #497. |
| #752 run/test executable lowering | `selfhost::cli::execute`, `selfhost::backend::executable`, `selfhost::backend::hosted` | hosted stage2 uses the direct bounded executable renderer; native selfhost source executable uses checked AST | switched for bounded source path | `just selfhost-native-source-gate` builds the selfhost source package as a native executable and verifies run/test artifacts carry `executable_lowering selfhost::backend::executable checked-ast`, including local string `let` plus `print(local)` multiple-statement run lowering; hosted stage2 no longer depends on the old generated source-shape matcher module. |

## Phase Replacement Checklist

Every phase switch issue must name these evidence fields before it can replace a
Go-owned production boundary:

| Phase | Current Go-owned boundary | Selfhost component | Required gate or oracle | Open blocker owner |
| --- | --- | --- | --- | --- |
| source loading | package root, manifest, file graph, import graph | `selfhost::source`, `selfhost::source::loader` | `just selfhost-oracle` plus `just selfhost-production-from-scratch` when the artifact path changes | invalid manifest/import-cycle diagnostics tracked by frontend replacement issues |
| lexer | tokenization in Go frontend | `selfhost::lexer` | lexer oracle in `just selfhost-oracle` | none before broader parser replacement |
| parser | Go AST/parser and parse diagnostics | `selfhost::parser`, `std::kizu::parser` | parser oracle plus parse CLI parity gate | structured parse diagnostics and recovery cases |
| resolver | Go project resolver and symbol table | `selfhost::resolver` | resolver oracle plus check CLI parity gate for affected sources | duplicate/private/import diagnostics |
| type checker | Go type checker | `selfhost::types` | type oracle plus check CLI parity gate | full expression/type surface and structured diagnostics |
| ownership checker | Go ownership and borrow checker | `selfhost::ownership` | ownership oracle plus negative check parity for the switched shape | array/field/mutable borrow coverage |
| IR handoff | Go IR construction | `selfhost::ir` | `just selfhost-native-source-gate` for executable handoff; backend fingerprint gate for broader IR | package IR boundary after hosted executable mode |
| backend artifact | Go backend/cache for general builds | `selfhost::backend` | `just selfhost-backend-artifact-gate` plus `just selfhost-production-from-scratch` | first real codegen slice after hosted artifact mode |
| CLI parse/check/run/test | Go `cmd/kizu` dispatch for the general CLI | `selfhost::cli` and stage2 hosted artifact | matching CLI parity gate and no `go.cmd-kizu-fallback` marker except `none` | unsupported source shapes remain issue-linked |

## Issue 927 Closeout Inventory

This inventory closes the #927 roadmap-definition stage. It names the remaining
Go-owned boundaries and records whether the replacement has a concrete switch
issue, an existing blocker issue, or an explicit deferral. It does not switch
any additional production behavior by itself.

### Source Loading And Module Graph

| Behavior | Current Go-owned boundary | Selfhost boundary | Evidence | Replacement status |
| --- | --- | --- | --- | --- |
| package root discovery | Go project loader discovers roots for general CLI paths | `selfhost::source::loader` for selfhost and selected hosted targets | `just selfhost-production-from-scratch`, `just selfhost-oracle` when frontend parity is claimed | deferred until a selected general CLI switch issue names the root-selection surface |
| manifest loading | Go `kizu.toml` parser for general packages | `selfhost::source::manifest` | source oracle and production bootstrap read `selfhost/kizu.toml` through explicit fs capability | deferred for arbitrary user manifests until diagnostic parity is selected |
| selfhost/std source discovery | Go package loader owns broad discovery | `selfhost::source::std_index` plus loader table | production and corpus gates through stage2 artifact | switched only for selfhost package and supported corpus paths |
| user file source loading | Go CLI reads arbitrary files | hosted artifact reads selected parity fixtures through `std::fs::read_file` | parse/check/run/test parity gates record `go.cmd-kizu-fallback none` | bounded slices only; broader user file loading is deferred to future command-slice issues |
| module path derivation | Go project resolver derives module paths | selfhost source table stores source ids and module paths | source/resolver oracle | deferred until module diagnostics parity is selected |
| duplicate module paths | Go project diagnostics | selfhost source/resolver diagnostics | resolver oracle | replacement blocker is the resolver diagnostics row below |
| missing imports | Go project diagnostics | selfhost source/resolver diagnostics | resolver oracle | replacement blocker is the resolver diagnostics row below |
| import cycles | Go project diagnostics | selfhost source/resolver diagnostics | resolver oracle | replacement blocker is the resolver diagnostics row below |

### CLI Command Ownership

| Command | Production owner today | Selfhost component owner | Go fallback status | Required gate | Switch blocker or deferral |
| --- | --- | --- | --- | --- | --- |
| `parse <file>` | mixed: Go general CLI, hosted artifact for bounded parity rows | `selfhost::cli`, `selfhost::parser` | no fallback in hosted parity rows | `just selfhost-parse-parity-gate` | broader parse recovery and diagnostics are deferred to command-slice issues |
| `check selfhost` | hosted stage2 artifact | `selfhost::cli::check` plus selfhost source/resolver/type/ownership gates | none | `just selfhost-production-gate` | no current blocker for the supported selfhost target |
| `check <file>` | mixed: Go general CLI, hosted artifact for bounded parity rows | `selfhost::cli::check` | no fallback in hosted parity rows | `just selfhost-check-parity-gate` | broader type/ownership surface remains deferred |
| `run <file>` | mixed: Go general CLI by default, hosted artifact for bounded run rows, public CLI selfhost path behind `KIZU_SELFHOST_RUN` (#1151) | `selfhost::cli::execute`, `selfhost::ir`, `selfhost::backend` | no fallback in hosted parity rows; no Go fallback in the gated public CLI path | `just selfhost-run-parity-gate`, `just selfhost-run-cli-switch-gate`, `just selfhost-native-source-gate` when executable lowering changes | broader execution and default-on switch deferred to #1157 (parent #1070) |
| `test <file>` | mixed: Go general CLI by default, hosted artifact for bounded test rows, public CLI selfhost path behind `KIZU_SELFHOST_TEST` (#1157) | `selfhost::cli::execute`, `selfhost::ir`, `selfhost::backend` | no fallback in hosted parity rows; no Go fallback in the gated public CLI path | `just selfhost-test-parity-gate`, `just selfhost-test-cli-switch-gate`, `just selfhost-native-source-gate` when executable lowering changes | broader discovery and default-on switch deferred to #1157 (parent #1070) |
| `stage selfhost` | hosted stage2 artifact | `selfhost::backend`, hosted runtime ABI | none | `just selfhost-production-gate` | no current blocker for the supported selfhost target |
| `fmt <file>` / `fmt --write <file>` | mixed: Go general CLI, hosted artifact for #1073 formatter parity rows | selfhost formatter writer | no fallback in hosted rows | `just selfhost-fmt-parity-gate` | broader formatter syntax surfaces outside the parity manifest are deferred |
| `build`, `ir`, `wasm`, `native` | Go general CLI | no production selfhost owner yet | no hidden fallback because no selfhost switch is claimed | Go tests and backend-specific gates | explicit deferral until package/codegen IR replaces the Go backend boundary |
| `cache`, `why-rebuild` | Go build cache | none | no hidden fallback because no selfhost switch is claimed | cache smoke/perf commands when cache behavior changes | explicit deferral until a selfhost build-cache design issue exists |

### Parser Switch Slice

The first selected parse CLI switch remains the bounded
`selfhost/tests/cli/parse-parity.tsv` manifest. It covers minimal return,
print-call, qualified `std::testing::expect`, moved-value declarations, missing
expression, and missing assignment cases. The gate is
`just selfhost-parse-parity-gate`; it runs through
`target/selfhost/stage2/selfhost`, compares checked-in goldens, and records
`go.cmd-kizu-fallback none`.

Broader parse behavior is explicitly deferred until a future command-slice issue
names the source shape, stdout/stderr contract, diagnostics, and parity gate.
That future issue must not dispatch by fixture path or source literal.

### Resolver, Type, And Ownership Replacement Blockers

| Phase | Required replacement surface | Current evidence | Remaining blocker status |
| --- | --- | --- | --- |
| resolver | missing symbols, duplicate symbols, private access, import cycles, import conflicts, local/import shadowing, user package named `std` | resolver oracle plus check parity for selected rows | broader module diagnostics are deferred until a selected switch issue names the exact diagnostic surface and gate |
| type checker | primitive types, signatures, fields, variants, optionals, error unions, casts, generic constructors, std containers, calls, field/index expressions, spans | type oracle plus bounded check parity rows | broad expression/type coverage is deferred; unsupported surfaces must stay visible as diagnostics |
| ownership checker | move after move, borrowed views, local/mutable/field borrow, array/map/string resources, arena/handle states, negative spans | ownership oracle plus moved-value check parity; borrowed-return provenance remains tracked by #538 | broad negative parity is deferred until a selected switch issue names the source shape and parity gate |

### Diagnostics Blockers

Structured diagnostics were introduced by #897. Phase switching remains blocked
where stable structured fields are required for user-visible replacement:

| Phase | Required fields | Switch impact | Status |
| --- | --- | --- | --- |
| parser | severity, code/category, primary span, recovery notes | parse CLI replacement cannot broaden without stable parse diagnostics | deferred to future parser switch slices |
| resolver | severity, code/category, primary and related spans | module/import/visibility replacement needs stable related spans | deferred to future resolver switch slices |
| type checker | severity, code/category, primary span, help text | check replacement needs stable typed diagnostics | partially available through internal structured diagnostics; broader migration deferred |
| ownership checker | severity, code/category, primary span, related source/borrow spans | negative parity needs stable borrow/move spans | deferred with #538 for multi-source provenance |
| CLI rendering | stable text plus structured source fields | hosted artifact parity compares stdout/stderr byte-for-byte | available for bounded parity manifests |
| LSP | range/code compatibility with CLI diagnostics | LSP must not regress when shared diagnostics broaden | existing LSP alignment remains tracked by #832 |

### Stdlib And Runtime Capabilities

The runtime capability inventory is in
[`docs/selfhost-runtime-abi.md`](selfhost-runtime-abi.md). Available
capabilities are explicit fs/io/process/allocator/string/array/map boundaries.
Blocked or deferred capabilities are issue-linked there: `@embed` and broader
`@` builtins are #610, fixed-buffer and user allocator APIs are #549, and
multi-source borrowed return provenance remains #538.

### Production Report Audit Surface

Production reports are the audit surface for fallback absence. A passing
production report must include:

| Field | Meaning | Guard |
| --- | --- | --- |
| `go.production none` | supported production path did not call Go compiler phases | `just selfhost-production-gate` fails if the marker changes |
| `go.cmd-kizu-fallback none` | hosted CLI parity path did not fall back to Go `cmd/kizu` | parse/check/run/test parity gates fail if the marker changes |
| `executable_lowering selfhost::backend::executable checked-ast` | native source executable artifacts use checked AST executable lowering | `just selfhost-native-source-gate` |
| stage fingerprints | stage1/stage2 artifacts and command outputs match | `just selfhost-production-from-scratch` |
| hosted/native artifact boundaries | artifact paths, runtime mode, and emitted metadata are explicit | backend artifact and production gates |

## Backend Boundary After Hosted Artifacts

The current executable path is:

```text
checked AST -> executable lowering -> data::Executable -> hosted artifact renderer
```

`data::Executable` is the temporary backend handoff for bounded `run` and `test`
artifacts. The next backend boundary must be a small package/codegen IR input
owned by `selfhost::ir`, with backend-owned data for symbols, constants, calls,
locals, and exits. Hosted artifact mode may remain as an artifact writer while
that IR is introduced, but these branches are temporary and must not grow new
source-literal, fixture-path, or static LLVM case dispatch:

- `selfhost::backend::hosted` executable renderer
- `selfhost::backend::cli_run_llvm`
- `selfhost::backend::cli_test_llvm`
- hosted metadata constants for run/test artifacts

The smallest next real codegen slice is a `main` function with a string
constant, direct `print` call, and void return lowered from the mini IR rather
than from a run/test-specific hosted template branch.

## Run CLI Switch Point For #1151

The first public `cmd/kizu` CLI command routed through the selfhost-owned
compiled artifact path (beyond the `target/selfhost/stage2/selfhost` parity
gates) is `run <file>`, behind the rollback-friendly switch point
`KIZU_SELFHOST_RUN=1`.

| Field | Value |
| --- | --- |
| switched path | `selfhost::cli::execute::run_file_cli` (lower run-codegen program → link → execute native artifact) |
| switch point | env `KIZU_SELFHOST_RUN` (default off → Go interpreter path unchanged) |
| Go fallback when enabled | none; unsupported shapes raise explicit selfhost diagnostics |
| evidence report | `target/selfhost/reports/run-cli-switch.txt` (`go.fallback none`) |
| gate | `just selfhost-run-cli-switch-gate` |
| deletion condition | flip default to selfhost and remove the gate once `run` is selfhost-owned for the general surface (#1157, parent #1070) |

Deliberately not switched by #1151, kept explicit in #1157:

- General `run` shapes the selfhost backend cannot lower yet stay explicit
  diagnostics under the gate (for example arena allocation, unsupported integer
  expressions, package/directory targets), so the default stays on the Go path.
- `test <file>` is not switched: through the public CLI the selfhost `test` path
  only emits a test-executable artifact without executing it, so routing it would
  regress the Go `testFile` behavior. It needs test-artifact execution semantics
  first.

## Test CLI Switch Point For #1157

The second public `cmd/kizu` CLI command routed through the selfhost-owned
compiled artifact path is `test <file>`, behind the rollback-friendly switch
point `KIZU_SELFHOST_TEST=1`. The selfhost `test` path now lowers the test
executable, emits LLVM, links a self-contained native artifact (both the
checked-AST/interpreter renderer `selfhost::backend::hosted` and the
stage2/native compiled bounded renderer `selfhost::backend::cli_test_llvm` emit
their own `@main` for the `kizu_test_main` entry, mirroring the run artifact
boundary), and **executes** it, so the observable output (`test: ok` on stdout
for a supported passing test, the runtime error on stderr for a failing one)
matches the Go `testFile` path for supported shapes.

| Field | Value |
| --- | --- |
| switched path | `selfhost::cli::execute::test_file_cli` (lower test executable → emit LLVM → link → execute native artifact) |
| switch point | env `KIZU_SELFHOST_TEST` (default off → Go `testFile` interpreter path unchanged) |
| Go fallback when enabled | none; unsupported shapes raise explicit selfhost diagnostics (usage, exit 64) |
| evidence report | `target/selfhost/reports/test-cli-switch.txt` (`go.fallback none`) |
| gate | `just selfhost-test-cli-switch-gate` |
| deletion condition | flip default to selfhost and remove the gate once `test` is selfhost-owned for the general discovery/runtime surface (#1157, parent #1070) |

Supported selected shape under the gate: a top-level `test` whose body is a
single `std::testing::expect(true|false)` call (for example
`selfhost/tests/cli/test_expect_ok.kizu`). It lowers, links, and executes end to
end; `expect(true)` prints `test: ok` and exits 0, `expect(false)` prints the
runtime error and exits 1.

Deliberately not switched / remaining unsupported, kept explicit in #1157:

- Unsupported test shapes under the gate (for example a `test` body wrapping the
  expectation in an `if`, as in `selfhost/tests/cli/test_if_unsupported.kizu`)
  stay explicit selfhost diagnostics (usage, exit 64) with **no Go fallback**;
  the gate-off default Go `testFile` path still runs them through the
  interpreter, which is why the switch stays gated rather than default-on.
- Broader test discovery (multiple `test` blocks, helper-driven assertions,
  non-`std::testing::expect` bodies) is not switched; those shapes are not yet
  lowerable by the selfhost test executable backend.
- The **stage2/native compiled bounded test renderer** (`cli_test_llvm`) now
  emits the same self-contained `@main` for the `kizu_test_main` entry as the
  checked-AST/interpreter renderer (`selfhost::backend::hosted`), mirroring the
  run artifact boundary. Both the `just selfhost-test-parity-gate` and the
  native-source gate link the emitted test artifact harness-free against the host
  runtime (`linkTestParityExecutableWithHost`), identical to the run path; the
  external main-providing C harness is removed. Broader test discovery and the
  default-on switch remain the follow-up work tracked below.

## Failure Policy

- Any oracle mismatch blocks a PR that relies on Go/Kizu oracle evidence.
- The general `kizu` CLI stays Go-owned until a later switch issue changes an
  explicit component selection point. The #458 selfhost command path is the
  hosted stage2 artifact, not `go run ./cmd/kizu check selfhost`.
- There is no implicit fallback from Kizu-owned logic to Go-owned logic inside a
  switched path. Rollback is a normal revert of the explicit switch commit.
- Backend, cache, and artifact changes require their own switch decision and
  measurement evidence; frontend oracle success is not enough to change them.
- Unsupported language features must stay visible in oracle output or in linked
  GitHub issues. Do not hide them behind runtime fallback.

## Release Boundary For #461

The releaseable artifact for the first runnable selfhost path is the stage2
hosted compiler produced by:

```sh
just selfhost-production-from-scratch
```

For this release boundary, only the #458 command surface is production-owned by
the artifact:

```sh
target/selfhost/stage2/selfhost check selfhost
target/selfhost/stage2/selfhost stage selfhost
```

The artifact may also run the manifest-selected #460 supported corpus. It must
not be described as a general replacement for the `kizu` CLI until #497 closes.

Rollback is a revert of the #461 production-boundary change or a release note
that points operators back to explicit bootstrap/oracle commands. Rollback must
not silently dispatch failed artifact commands to Go compiler phases.

## Local Evidence For #435

Recorded on 2026-05-20 after the resolver, type, and ownership gates were
merged:

| Command | Result | Notes |
| --- | --- | --- |
| `just selfhost-switch-gate` | passed, `real 9.42s` | Oracle output reported lexer/parser/resolver/type/ownership failures = 0. |
| `just cache-smoke` | passed, `real 1.49s` | Isolated cache created 2 entries, 2760 bytes, then pruned both entries. |

No production CLI path, backend target, cache key, or artifact location is
changed by #435.

## Local Evidence For #451

Recorded on 2026-05-20 after the selfhost type checker source-table pass was
added:

| Command | Result | Notes |
| --- | --- | --- |
| `go test ./cmd/kizu -run 'TestSelfhostResolverGate\|TestSelfhostTypeGate' -v` | passed, `ok ... 32.526s` | Resolver production symbols = 513; type production symbols = 97; type production typed nodes = 2198 after ParseResult helpers. |
| `just selfhost-oracle` | passed, `ok ... 43.498s` | Oracle output reported lexer/parser/source/resolver/type/ownership failures = 0. |
| `go test ./...` | passed, `cmd/kizu 96.172s` | Full suite remains green; the selfhost type gate is currently interpreter-heavy. |

No production CLI path, backend target, cache key, or artifact location is
changed by #451.

## Local Evidence For #461

Recorded on 2026-05-21 after the production boundary gate was added:

Historical note: during #461, `just selfhost-switch-gate` still included the
aggregate oracle. Current switch-gate policy keeps the aggregate oracle as the
separate `just selfhost-oracle` preflight described above.

| Command | Result | Notes |
| --- | --- | --- |
| `just selfhost-switch-gate` | passed, `real 143.71s` | Ran production-from-scratch, aggregate oracle, package skeleton check, and project/type/ownership Go package tests. |
| `just selfhost-production-from-scratch` | passed, `real 61.03s` | Built stage2 through explicit bootstrap, then ran production and corpus gates through the hosted artifact. |
| `just selfhost-production-gate` | passed, `real 0.31s` | Ran `check selfhost`, `stage selfhost`, and unsupported command diagnostics through `target/selfhost/stage2/selfhost`; report wrote `go.production none`. |
