# ADR-0075: owner aggregate cleanup and errdefer

## Status

Accepted.

Extends ADR-0061, ADR-0067, and ADR-0074.

## Context

Kizu uses explicit cleanup instead of Drop, RAII, or implicit destructors.
Selfhost compiler work now constructs records that contain owned containers,
for example MIR records with `std::array::Array<T>` fields and checker paths
with local `std::map::Map<K, V>` owners.

Two patterns need a source-visible contract:

- an owned container is embedded inside a struct or union and the aggregate is
  returned, stored, or passed to another function
- a function builds an owned result, performs fallible work with `try`, and
  returns the owner on success

Plain `defer` handles local cleanup, but it cannot represent "cleanup on error,
transfer on success" without either hidden destructors or awkward manual cleanup
at every error return path.

## Decision

Any struct or union that contains an owner field or owner payload is an owner
aggregate. Owner aggregates are non-copy values. Passing one by value consumes
it. Read-only APIs use `&T`, mutating APIs use `&var T`, and consuming APIs take
owner aggregates by value.

Named owner aggregates that are constructed as standalone owners, returned from
functions, or accepted by value must expose a source-visible cleanup contract:

```kizu
impl MirFunction {
    fn deinit(self: MirFunction) -> void {
        self.call_args.deinit();
        self.struct_fields.deinit();
        self.expr_insts.deinit();
        self.multi_stmts.deinit();
    }
}
```

Direct owner field cleanup remains limited to the owning type's
`deinit(self: Owner) -> void` body. General code cannot run
`value.field.deinit()` and continue with a partially destroyed owner.

Container cleanup may perform structural element cleanup as part of an explicit
container cleanup call such as `array.deinit()`. This does not synthesize a
callable destructor for the element type, and it does not add scope-exit
cleanup.

Kizu adds Zig-style `errdefer <expr-stmt>;` for fallible owner construction.
`errdefer` registers the same cleanup call shape as `defer`, but it runs only
when the current lexical block exits through an error return path:

```kizu
fn make_values(allocator: std::mem::Allocator) -> !std::array::Array<i64> {
    let values = std::array::Array<i64>(allocator);
    errdefer values.deinit();

    try values.append(1);
    return values;
}
```

`defer` and `errdefer` share a block cleanup stack and execute in reverse
registration order. Normal exits skip `errdefer` entries. Error exits execute
both `defer` and `errdefer` entries. The ownership checker validates the cleanup
receiver on each path where the entry can run.

## Selfhost Application

For #992, selfhost code should move toward these shapes:

- local `Map`, `Array`, `String`, `Arena`, and `Box` owners use immediate
  `defer owner.deinit()` unless the owner is returned
- fallible builders that return owners use `errdefer owner.deinit()`
- renderer and inspection APIs borrow owned compiler records instead of taking
  them by value
- `MirFunction`, `MirStmt`, and `MirExprInst` expose explicit `deinit` methods
  while they contain owned arrays
- #991 can still refactor MIR into tagged-union payloads later; this ADR defines
  the cleanup contract both before and after that representation change

## Consequences

- Cleanup boundaries stay visible in source.
- Owner transfer across functions is explicit: borrow for read-only use, move for
  consuming use, return for ownership transfer.
- The checker needs path-sensitive validation for `errdefer` receivers.
- Existing manual cleanup after `try` should be replaced with `defer` or
  `errdefer` instead of adding fallback error paths.
- No hidden destructor, Go fallback, or static artifact branch is introduced.
