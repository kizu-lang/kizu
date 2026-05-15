# Go/Kizu Compiler 1:1 Audit

This document is the completion checklist for replacing Go compiler phases with
Kizu compiler phases. It records what "1:1" means in concrete artifacts and
which parts are still missing.

## Completion Definition

Kizu compiler 1:1 with the Go compiler means:

- every Go compiler phase has a Kizu implementation for the same input shape
- every phase emits a checked-in oracle snapshot with the same normalized schema
- positive examples pass in both implementations
- negative examples fail in both implementations with matching diagnostic
  message substrings and spans
- module packages, std sources, and single-file sources are all covered
- IR and backend smoke outputs are compared, not only counted
- build cache keys include every input used by the compared phases
- any accepted difference is documented in an ADR before the switch

Passing `go test ./...` is necessary but not sufficient. The tests must prove
each row below.

## Current Evidence

| Phase | Current evidence | Coverage strength | Missing for 1:1 |
| --- | --- | --- | --- |
| lexer | `tests/selfhost` compares token kind, literal, byte span, line, column | strong for covered files | broaden to all conformance parseable files |
| parser | self-host normalized AST counts vs Go parser counts | weak | full AST node model and dump equality |
| resolver | Go module resolver tests and module conformance runner | Go-only | Kizu module graph resolver and graph snapshot equality |
| diagnostics | Go multi-file diagnostics for visibility; selected self-host semantic counts | weak | Kizu diagnostic object with primary/related spans and equality tests |
| type | Go type checker runs conformance; self-host has semantic summary | weak | Kizu type checker for selected language subset and pass/fail equality |
| ownership | Go ownership checker runs conformance; selected negative fixtures pinned | weak | Kizu move/borrow checker with memory-safety fixture equality |
| IR | self-host IR summary counts vs Go-derived counts | weak | compare normalized IR dump/function/block/instruction shape |
| backend | Go LLVM/WASM smoke; self-host artifact count | weak | compare backend smoke status and selected output fingerprints |
| cache | Go module-aware cache tests include std source hash | medium | self-host cache input contract or explicit Go-owned cache decision |

## Required Issues

The remaining work should stay issue-driven. Each issue must include examples,
oracle tests, and acceptance evidence before closing.

1. Full parser AST oracle: #112
2. Kizu module resolver oracle: #113
3. Kizu diagnostic object oracle: #114
4. Kizu type checker subset oracle: #115
5. Kizu ownership/memory-safety oracle: #116
6. Normalized IR dump oracle: #117
7. Backend smoke fingerprint oracle: #118
8. Cache ownership and self-host switch decision: #119
9. 1:1 completion gate that fails if any phase lacks coverage: #111

## Non-Goals For The Audit

This audit does not lower the bar by treating normalized counts as full
compatibility. Counts are only temporary bootstrap evidence.

This audit does not allow hidden fallback from Kizu to Go in a phase declared as
Kizu-owned. Fallbacks must be explicit, documented, and tested as Go-owned.
