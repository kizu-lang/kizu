# ADR-0059: removed lifetime annotation proposal for borrowed views

## Status

Superseded by [ADR-0060: borrowed-return provenance without named lifetimes](0060-borrowed-return-provenance.md).

## Context

Kizu originally rejected explicit lifetime annotations to avoid Rust-style
lifetime programming. That remains a valid ergonomics concern, but v0.2 stdlib
work now needs zero-copy borrowed views for slices, strings, parser source
spans, and future matrix row/submatrix APIs.

Owned handles such as `std::arena::Arena<T>` / `std::arena::Handle<T>` solve graph and AST identity, but
they do not model view values that borrow contiguous storage. Without explicit
lifetime syntax, public signatures cannot say which input owns a returned view.

## Decision

This decision is no longer active. Kizu does not keep explicit lifetime
annotation syntax in source. Borrowed returns use `borrows <source>` instead,
and slice mutability is expressed by the outer borrow spelling.

## Deferred

The superseding design intentionally does not include:

- lifetime bounds
- anonymous lifetime markers
- lifetime parameters on `impl` or `contract`
- type aliases with lifetime parameters
- full dangling/escape enforcement
- mutable view alias analysis

Those are tracked as follow-up work instead of hidden compatibility behavior.

## Consequences

`ADR-0016` remains aligned with the current design. Kizu is still not trying to
become Rust-compatible: source-visible lifetime annotations are not part of v0.

`std::arena::Arena<T>` / `std::arena::Handle<T>` remain opaque IDs, not references. They continue to
model long-lived graph identity separately from slice/view borrowing.
