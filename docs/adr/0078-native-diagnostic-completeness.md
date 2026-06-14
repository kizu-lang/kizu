# ADR-0078: native diagnostic completeness

## Status

Proposed.

## Context

ADR-0073 set the boundary for the selfhost `run` artifact, and ADR-0072 set the
diagnostic message style. This ADR is about a separate, measured gap: the native
selfhost artifact does not reproduce the full diagnostic surface of the Go
toolchain for `check`, `parse`, and `fmt`.

The native artifact is not produced by compiling selfhost source as a real
program. It is produced from a curated catalog plus hand-written LLVM dispatch:

- `selfhost/src/ir/executable_functions.kizu` is a hand-maintained catalog
  (~4500 lines) that seeds an AST traversal from named helper functions and
  emits their lowered bodies as facts/IR.
- `selfhost/src/backend/cli_*_llvm.kizu` (`cli_check_gate_llvm`,
  `cli_parse_llvm`, `cli_parse_diag_llvm`, `cli_parse_comment_llvm`,
  `cli_run_llvm`, `cli_test_llvm`, `cli_codegen_llvm`) are hand-written LLVM
  dispatch surfaces for each CLI command.

The decisive evidence that this is a curated catalog rather than a real
self-compile: `parse` and `fmt` follow the same source path, yet the native
artifact diverges in what it emits. If the artifact were a faithful compile of
selfhost source, identical source paths could not produce different native
behavior. The divergence is an artifact of which helper bodies the catalog has
been taught to emit.

A second confirmation is in the catalog itself. The undefined-variable check is
seeded from `first_hosted_check_undefined_variable_start_ast_node` and emitted by
an AST-only closure walk (`append_types_checker_function_facts` /
`append_types_checker_closure_helper_body`). It deliberately avoids the symbol
`Map`. The fact that the one type diagnostic already in the catalog was written
as a Map-free AST-only shadow is itself a signal about the cost of the diagnostics
that do need `Map`.

### Current completeness map

This consolidates the prior audits (#1332, #1334, emission completeness audit,
#1331):

| Surface | Native status | Notes |
| --- | --- | --- |
| `fmt` | ✅ complete | full formatting parity |
| `run` | ✅ limited | supported codegen subset only |
| `check` (undefined) | ⚠️ minimal | only the smallest `undefined` form is emitted |
| `check` (type mismatch ×5) | ❌ | binary / argument / assignment / return / arity mismatches not emitted |
| `check` (ownership / moved) | ❌ | borrow/move analysis is Go-deferred |
| `parse` errors (#1331) | ⚠️ pass-through | parse errors are not reported with parity |
| `fmt` errors (#1331) | ⚠️ pass-through | same validation reporting gap as parse |

The completion work is large and multi-component. Rather than land it
piecemeal, this ADR partitions the gap into tiers by mechanism cost and proposes
a per-tier direction so maintainers can decide.

## Decision

The work splits into three mechanism tiers plus an explicit Go-deferred tier.
For each tier the recommendation is stated, with the rejected options recorded.

### Tier 1 — easy (AST-only, no `Map`)

Scope: the remaining `undefined` forms and `invalid-assignment-target`. These
are decidable from the AST alone and need no symbol table or type inference.

**Recommendation: extend the hand-written `hosted_*` shadow seeds.** This reuses
the proven mechanism already in the catalog: add a seed helper, let the AST
closure walk emit its body, ~300-400 lines per diagnostic. worker-4 is proving
feasibility on `undefined`; this ADR adopts that result as the template for the
remaining Tier 1 diagnostics. No backend redesign is required.

Rejected: a Go-deferred stub for Tier 1. Rejected because the mechanism is known
to work, the marginal cost is bounded, and these are the diagnostics users hit
first.

### Tier 2 — `Map`-expressive (5 type-mismatch diagnostics)

Scope: binary / argument / assignment / return / arity type-mismatch
diagnostics. These require comparing values held in a `Map` plus type inference,
not just AST shape.

Options:

- **(a) Map-free AST-only shadow per diagnostic.** Mirror the `undefined`
  approach, encoding just enough type reasoning into AST-only closures to avoid
  `Map`. Feasible per diagnostic but each shadow is more contorted than Tier 1
  because the underlying analysis genuinely wants a symbol table.
- **(b) Route catalog bodies through a real `Map` + type-inference lowerer.** A
  large change to the catalog body lowerer so it can lower code that uses `Map`
  and type inference directly. Highest leverage but highest risk.
- **(c) Go-deferred.** Keep these five on the Go toolchain.

**Recommendation: (c) Go-deferred for now, with (b) as the eventual target.**
The fact that the existing `undefined` shadow went out of its way to avoid `Map`
is direct evidence that (a) does not scale to five `Map`-shaped diagnostics and
that (b) is the real cost. Until the catalog lowerer can handle `Map` and type
inference (option b), deferring these to Go is the honest position; (a) would
accrete five fragile shadows that (b) later has to delete.

### Tier 3 — `parse` / `fmt` error reporting (#1331)

Scope: reporting parse and fmt validation errors with native parity instead of
passing them through.

Options:

- **Route A: emit the whole validation module as a component seed** — thousands
  of lines pulled into the catalog.
- **Route B: grow hand-written heuristics in `cli_parse_diag_llvm`** — fragile,
  pure accretion, drifts from the real validator.
- **Route C: Go-deferred.**

The #1331 handoff identified three blockers, summarized: (1) the native artifact
has no faithful path to the validation module's diagnostics, so parity requires
either importing the module (Route A) or re-deriving it (Route B); (2)
hand-written heuristics in `cli_parse_diag_llvm` cannot track the real
validator and will silently diverge; (3) the parity gate only checks negatives
the catalog already emits, so a heuristic that is wrong in an unseeded case is
not caught.

**Recommendation: Route C (Go-deferred) until Route A is viable.** Route B is
rejected outright as accretion that worsens the divergence #1331 already
documents. Route A is the correct end state but is gated on the catalog being
able to seed a large component faithfully (the same lowerer capability Tier 2
option b needs). Defer until that capability exists.

### Go-deferred tier — ownership / moved

Scope: ownership and moved-value diagnostics. These require borrow/move
analysis. Go-deferred remains appropriate for the foreseeable term; this tier is
not a near-term candidate for any shadow.

### Explicit Go-deferral design

For every surface this ADR leaves on Go, the deferral should be *explicit* in
selfhost source, not an implicit gap. `selfhost/src/main.kizu` already has the
`deferred_public_command` mechanism: it dispatches a named command to
`output::deferred_public_command`, which prints
`selfhost: command '<name>' is not owned yet; deferred to Go cmd/kizu` to stderr
and returns. Tiers 2, 3, and the ownership tier should use the same pattern — a
named, observable deferral — so that the boundary between native-owned and
Go-owned diagnostics is visible in source and in the artifact's behavior, rather
than appearing as a silent missing diagnostic.

## Consequences

The parity gate's negative-case coverage is currently limited to the range the
catalog actually emits. Emission gaps have slipped through the gate because the
gate never exercised the unseeded cases — it could not, since the native
artifact had nothing to compare. Whatever tier direction maintainers accept, the
follow-up is to add negative cases to the gate for the diagnostics that move into
native ownership, so the gate stops mistaking "not emitted" for "matches".

Adopting explicit Go-deferral (above) makes the deferred surfaces testable: the
gate can assert the deferral message instead of treating the diagnostic as
absent.

Choosing Go-deferral for Tiers 2 and 3 means the native artifact stays honestly
incomplete for type-mismatch and parse/fmt error reporting until the catalog
lowerer gains `Map` + component-seed capability. That is a deliberate trade of
coverage for not accreting fragile shadows.

## Non-goals

- Expanding the `run` codegen supported subset (tracked separately).
- The #1255 boundary migration.
- Choosing the final implementation of the catalog `Map`/component-seed lowerer;
  this ADR only identifies it as the shared prerequisite for Tier 2 option (b)
  and Tier 3 Route A.
