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
