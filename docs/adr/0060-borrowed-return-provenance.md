# ADR-0060: borrowed-return provenance without named lifetimes

## Status

Accepted.

## Context

Named lifetime parameters for borrowed views were proposed and dropped: they move
Kizu toward Rust-style lifetime programming. The project goal is the opposite:
safe borrowed views must be explicit, while long-lived identity should be
modeled with owned storage, arena/handle, IDs, or small unsafe wrappers.

The selfhost compiler still needs zero-copy views for source slices, path
helpers, `String.as_bytes`, `Array.at`, and `Box.borrow`. Those APIs need to say
which input owns a returned view without exposing users to generic lifetime
variables.

## Decision

Kizu uses return provenance syntax for borrowed returns:

```kizu
fn first(bytes: []u8) -> []u8 borrows bytes
fn shared(value: &i64) -> &i64 borrows value
fn as_bytes(self: std::string::String) -> []u8 borrows self
fn at<T>(self: std::array::Array<T>, index: i64) -> !&T borrows self
```

Rules:

- `borrows <source>` names one function parameter or `self` parameter.
- `borrows <source>` names exactly one source. Multiple-source alternatives are
  not part of v0.
- The returned view may not outlive that source.
- Borrow returns such as `-> &T` require `borrows <source>`.
- Slice view returns may use `borrows <source>` when they return input-backed
  storage.
- Named lifetime annotations are not source syntax.
- Borrow fields in structs and union payloads are not part of v0.2.
- View-struct syntax is not part of v0. Values that need to be stored should
  keep spans, ids, handles, or owned data instead of embedded borrows.

## Consequences

Kizu keeps the visible safety boundary needed for zero-copy system code without
adding lifetime variables, lifetime bounds, or anonymous lifetime syntax.

APIs that need longer-lived relationships should use owned containers,
`std::arena::Arena<T>` / `std::arena::Handle<T>`, stable IDs, spans, copied
keys, or explicit unsafe wrappers with safe-side invariants. If a future API
needs a return tied to multiple sources or a first-class view aggregate, it must
be added as a bounded follow-up with examples and checker coverage instead of
reintroducing general lifetime programming.

