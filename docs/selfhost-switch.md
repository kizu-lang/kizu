# Selfhost Production Switch Gate

This document defines the review gate for replacing Go-owned compiler paths with
Kizu-owned components. It does not switch production behavior by itself.

The no-Go stage contract is defined in
[`docs/selfhost-bootstrap.md`](selfhost-bootstrap.md).

## Repeatable Gate

Run this before any PR that proposes using a Kizu-owned component in a production
CLI path:

```sh
just selfhost-switch-gate
```

The command checks the source-owned `selfhost` package, runs the Go/Kizu oracle
suite, runs the component gate entries directly, and keeps the Go project, type,
and ownership packages green.

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
| diagnostics / resolver | `selfhost::{diagnostic, resolver, resolver_oracle}` | Go project resolver | oracle-only | Symbol/visibility map gate and missing symbol, duplicate symbol, private access, import cycle diagnostics remain green. |
| type checker | `selfhost::{types, types_oracle}` | Go type checker | oracle-only | Type-kind, arity, copyability, and stable diagnostic span gate remains green. |
| ownership / borrow checker | `selfhost::{ownership, ownership_oracle}` | Go ownership checker | oracle-only | Move, borrow, deinit, array, map, string, arena, handle, and borrowed-view gate remains green. |
| interpreter | none | Go interpreter | Go-owned | No switch planned before Kizu compiler frontend can emit a stable execution IR. |
| IR / backend | `selfhost::{ir, backend}` skeleton | Go IR / backend | Go-owned | Requires a separate backend fingerprint and artifact/cache issue before any production switch. |
| build cache / artifacts | none | Go cache / target paths | Go-owned | Requires explicit cache-key, prune, status, no-op rebuild, and artifact-size evidence. |

## Failure Policy

- Any oracle mismatch blocks the switch PR.
- The production path stays Go-owned until the switch PR changes an explicit
  component selection point.
- There is no implicit fallback from Kizu-owned logic to Go-owned logic inside a
  switched path. Rollback is a normal revert of the explicit switch commit.
- Backend, cache, and artifact changes require their own switch decision and
  measurement evidence; frontend oracle success is not enough to change them.
- Unsupported language features must stay visible in oracle output or in linked
  GitHub issues. Do not hide them behind runtime fallback.

## Local Evidence For #435

Recorded on 2026-05-20 after the resolver, type, and ownership gates were
merged:

| Command | Result | Notes |
| --- | --- | --- |
| `just selfhost-switch-gate` | passed, `real 9.42s` | Oracle output reported lexer/parser/resolver/type/ownership failures = 0. |
| `just cache-smoke` | passed, `real 1.49s` | Isolated cache created 2 entries, 2760 bytes, then pruned both entries. |

No production CLI path, backend target, cache key, or artifact location is
changed by #435.
