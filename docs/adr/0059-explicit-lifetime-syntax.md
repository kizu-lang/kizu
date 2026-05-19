# ADR-0059: explicit lifetime syntax for borrowed views

## Status

採用。

## Context

Kizu originally rejected explicit lifetime annotations to avoid Rust-style
lifetime programming. That remains a valid ergonomics concern, but v0.2 stdlib
work now needs zero-copy borrowed views for slices, strings, parser source
spans, and future matrix row/submatrix APIs.

Owned handles such as `arena<T>` / `handle<T>` solve graph and AST identity, but
they do not model view values that borrow contiguous storage. Without explicit
lifetime syntax, public signatures cannot say which input owns a returned view.

## Decision

Kizu adopts named lifetime parameters for borrowed views:

```kizu
struct Row<'a, T> {
    data: []'a const T
}

fn row<'a, T>(matrix: &'a Matrix<T>, index: i64) -> !Row<'a, T>
```

Rules for the first implementation:

- Lifetime parameters share the `<...>` list with type parameters, and
  lifetimes come first: `<'a, T>`.
- Borrow types use `&'a T` and `&'a mut T`.
- Slice view types use `[]'a const T` and `[]'a mut T`.
- `[]const T` remains an elided local spelling where no boundary lifetime is
  needed.
- Borrow-returning functions must spell the returned lifetime explicitly.
- Struct and union fields may store borrowed views only when the declaration
  has an explicit lifetime parameter.
- `'static` is reserved for string literals and compile-time immutable data.
- Lifetimes are compile-time checker information and are erased from runtime
  representation.

## Deferred

The first implementation intentionally does not include:

- lifetime bounds such as `where 'b: 'a`
- anonymous lifetime `'_`
- lifetime parameters on `impl`, `satisfy`, or `contract`
- type aliases with lifetime parameters
- full dangling/escape enforcement
- mutable view alias analysis

Those are tracked as follow-up work instead of hidden compatibility behavior.

## Consequences

`ADR-0016` is superseded. Kizu is still not trying to become Rust-compatible:
the language adopts explicit lifetimes only where borrowed views cross function
or type boundaries.

`arena<T>` / `handle<T>` remain opaque IDs, not references. They continue to
model long-lived graph identity separately from slice/view borrowing.
