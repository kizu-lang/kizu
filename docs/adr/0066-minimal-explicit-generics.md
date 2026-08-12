# ADR-0066: minimal explicit static arguments for function generics

## Status

Accepted.

## Context

The v0.2 stdlib and self-host compiler source already need APIs such as
`std::array::Array<T>`, `std::map::Map<K, V>`, and typed test assertions.
Keeping those as Go-only special cases makes the Go implementation thicker and
prevents self-host source from representing ordinary reusable helpers.

Kizu also needs a stable answer for how type values relate to `comptime`.
Adopting Zig-style `comptime T: type` as the primary public syntax would mix
compile-time type arguments into the runtime argument list. That makes
ownership and borrow boundaries less obvious, because `(...)` would contain
both values that can move or borrow and values that cannot exist at runtime.

Full generics are still too broad for v0.2. Kizu does not yet need inference,
bounds, generic structs beyond existing std container spellings, associated
types, higher-kinded types, or generic runtime reflection.

## Decision

Kizu adopts a minimal explicit static argument subset for function generics.

- The `<...>` list on declarations and calls is a compile-time/static argument
  list, separate from the runtime argument list `(...)`.
- v0.2 accepts only type parameters in static argument lists:
  `fn f<T>(value: T) -> T`.
- Calls must pass explicit static arguments: `f<i64>(1)`.
- Namespace-qualified calls use the same syntax:
  `std::testing::expect_equal<i64>(expected, actual)`.
- Generic function bodies are checked when a call instantiates them with a
  concrete static argument set. Top-level generic bodies are not checked as
  uninstantiated runtime code.
- Type parameters are compile-time type values inside the instantiated body.
- `type<T>` is a compile-time type literal. `comptime if T == type<i64>` checks
  only the selected branch after instantiation.
- Bare type names are not expression-level type values in v0.2. Kizu keeps
  `type<T>` as the canonical spelling so value names and type names do not
  share an implicit expression namespace, and so compound types use the same
  form as primitive types.
- `type` values are comptime-only and cannot be stored in runtime locals,
  fields, union payloads, collection elements, or return values.
- The Zig-style spelling `fn f(comptime T: type, value: T)` is not the
  canonical Kizu generic syntax. Kizu keeps type/static arguments in `<...>` so
  runtime arguments remain ordinary move/borrow checked values.

A declaration takes at most one type parameter. The std container spellings
above (`std::map::Map<K, V>`) are type constructors rather than generic bodies
and keep their existing arity. The checker rejects a second parameter at the
declaration, not at the call, because a generic body is only checked once a
call instantiates it.

This subset deliberately excludes:

- implicit type argument inference
- non-type static arguments in v0.2; future phases may add integer, bool, or
  string static arguments to the same `<...>` list when a concrete self-host
  need appears, for example a fixed-size buffer capacity
- generic methods or impl-level generic parameters
- generic structs outside the existing std container type spellings
- bounds, associated types, higher-kinded types, specialization, and reflection
- macro expansion or AST/token rewriting

## Consequences

- Public std wrappers and `std::testing::expect_equal<T>` can live in Kizu
  source while Go remains the explicit trap/storage/runtime boundary.
- Self-host compiler code can use reusable typed helpers without inventing
  per-type assertion families.
- The checker may instantiate the same generic body multiple times in v0.2;
  this is acceptable for the current interpreter-first scope.
- Future widening should extend the explicit static argument list before adding
  implicit inference or a second generic syntax.
