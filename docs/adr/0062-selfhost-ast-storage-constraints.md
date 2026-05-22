# ADR-0062: Selfhost AST storage constraints

## Status

Accepted.

## Context

ADR-0058 introduced `std::kizu::ast::Ast` as the narrow selfhost AST owner:
nodes live in `std::arena::Arena<AstNode>(allocator)`, child lists live in
`std::array::Array<NodeId>`, and `NodeId` is an opaque wrapper over
`std::arena::Handle<AstNode>`.

Before expanding hosted `check <file>` beyond the current narrow corpus, the
allocator, arena payload, handle provenance, cleanup, and borrowed-view rules
need to be explicit. Without that boundary, selfhost frontend work could
accidentally require recursive arena cleanup, hidden allocator behavior, or a
runtime fallback that the hosted artifact cannot support.

## Decision

The selfhost AST arena is a special-purpose storage shape for
`std::kizu::ast::AstNode`; it is not a general-purpose arena payload expansion.
This ADR constrains hosted selfhost AST storage only. It does not define the
final `std::arena::Arena<T>` payload policy for all Kizu programs, and it must not be read
as a decision to keep Kizu's general storage model artificially small.

Kizu should preserve explicit systems-language ergonomics: future arena payload
expansion is allowed when the ownership, borrow, allocator, cleanup, and failure
rules are specified up front. Such expansion must use visible ownership and
cleanup rules rather than hidden destructors, hidden allocators, or runtime
fallback paths.

For the selfhost AST arena, `AstNode` payloads may contain only:

- scalar copy values such as `bool`, `i64`, `u8`, enum tags, and small id
  wrappers
- span, token, symbol, child-range, and `NodeId` values
- source spans or symbol ids that can be resolved against `SourceFile` inside
  the owning `Ast`
- other AST payload records that recursively obey this same rule

`AstNode` payloads must not contain owned containers or runtime capabilities:

- `std::array::Array<T>`
- `std::string::String`
- `std::map::Map<K, V>`
- `std::mem::Box<T>`
- `std::arena::Arena<T>` or arbitrary `std::arena::Handle<T>` outside `NodeId`
- `Allocator`, `Io`, task/thread/channel/mutex/atomic capabilities
- raw pointers

Variable-length relationships must use `ChildRange` into the AST-owned child
array. Owned text construction for diagnostics, paths, or rendering stays
outside the arena in explicit `String` or `Array` owners.

`NodeId` remains copyable as an opaque id. The current runtime representation
of a handle as `(arena runtime pointer, zero-based slot index)` is sufficient
for this selfhost AST slice. No generation counter is required until a concrete
safe Kizu use case needs handle reuse detection after deallocation.

Static checking is responsible for safe-side ownership and lifetime rules:

- known cross-`Ast` `Ast.get(id)` calls are rejected
- known use after `Ast.deinit()` is rejected
- known `NodeId` values cannot outlive their owning `Ast`
- `NodeId` cannot be cast to, stored as, or used as a raw pointer
- borrowed views from `Array.at`, `String.as_bytes`, and `arena.get` cannot be
  stored in structs, arrays, maps, arena payloads, or returned beyond their
  declared source
- `Ast.deinit()` cannot run while a node or child-list view is borrowed

Runtime checking is still required at the hosted artifact boundary:

- `@kizu_rt_arena_get` diagnoses mismatched arena pointers and out-of-range slot
  indexes with `invalid arena handle`
- runtime diagnostics are a backstop for unknown provenance and corrupted
  handles, not a replacement for static safe Kizu checks
- use-after-deinit is not made safe by runtime handle representation; safe code
  must be statically rejected before the arena is released

`Allocator` is a visible copyable capability value. Passing an allocator to
`std::arena::Arena<T>`, `Array<T>`, `String`, `Map<K, V>`, or `Box<T>` reads the capability;
it does not transfer allocator ownership. The created owner stores the runtime
allocator pointer needed by its own `deinit`. Allocation failure is recoverable
and must be represented by `!T` or `!void` on operations that can allocate.
There is no hidden default allocator and no implicit global allocator.

Allocator values may be copied within a local selfhost compiler execution.
Crossing task/thread/channel boundaries with allocator-backed owners or
allocator-dependent storage remains rejected unless a later concurrency issue
defines an explicit capability-transfer rule.

Selfhost code that creates an owned container should register cleanup in the
same lexical block as soon as the owner is established:

```kizu
var nodes = std::array::Array<NodeId>(allocator);
defer nodes.deinit();
```

This is a coding rule for selfhost-owned containers, not an implicit destructor.
`defer` is preferred because it covers explicit `return` and `try` error-return
paths while preserving visible cleanup. Existing manual cleanup may remain only
where the owner is returned or where the current compiler subset has not yet
made `defer` available for that path; new selfhost code should not add manual
multi-exit cleanup patterns without a reason.

## Consequences

- Hosted selfhost frontend expansion can add more AST node variants without
  requiring recursive arena cleanup.
- `Ast.deinit()` stays a shallow owner cleanup: it releases node arena storage
  and child-list storage, but not arbitrary owned payloads inside nodes.
- The runtime ABI can keep `%kizu.handle = type { ptr, i64 }` for this slice.
- Any future need for arena payloads that own `String`, `Array`, `Map`, `Box`,
  nested arenas, or concurrency capabilities must become a bounded #495/#496
  child issue before implementation. That future work may broaden general
  `std::arena::Arena<T>` or a specific arena owner once it defines explicit cleanup and
  checker behavior.
- `run <file>` and `kizu test <file>` remain separate execution-strategy work;
  this ADR only fixes the storage contract needed to expand hosted
  `check <file>`.
