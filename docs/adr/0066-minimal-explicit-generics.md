# ADR-0066: minimal explicit function generics

## Status

Accepted.

## Context

The v0.2 stdlib and self-host compiler source already need APIs such as
`std::array::Array<T>`, `std::map::Map<K, V>`, and typed test assertions.
Keeping those as Go-only special cases makes the Go implementation thicker and
prevents self-host source from representing ordinary reusable helpers.

Full generics are still too broad for v0.2. Kizu does not yet need inference,
bounds, generic structs beyond existing std container spellings, associated
types, higher-kinded types, or generic runtime reflection.

## Decision

Kizu adopts a minimal explicit function generic subset.

- Function declarations may have type parameters: `fn f<T>(value: T) -> T`.
- Calls must pass explicit type arguments: `f<i64>(1)`.
- Namespace-qualified generic calls use the same syntax:
  `std::testing::expect_equal<i64>(expected, actual)`.
- Generic function bodies are checked when a call instantiates them with a
  concrete type argument set. Top-level generic bodies are not checked as
  uninstantiated runtime code.
- Type parameters are compile-time type values inside the instantiated body.
- `type<T>` is a compile-time type literal. `comptime if T == type<i64>` checks
  only the selected branch after instantiation.

This subset deliberately excludes:

- implicit type argument inference
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
- Future widening should build on this explicit syntax rather than adding
  implicit inference first.
