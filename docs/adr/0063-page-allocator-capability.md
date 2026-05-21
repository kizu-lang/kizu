# ADR-0063: page_allocator capability stability

## Status

Accepted.

## Context

Kizu v0.2 already requires explicit allocator capabilities for owned storage:
`std::array::Array<T>`, `std::string::String`, `std::map::Map<K, V>`,
`std::mem::Box<T>`, and `arena<T>` all take an `Allocator` argument.

The public API shape repeatedly uses `std::mem::page_allocator() -> Allocator`,
but `Allocator` itself was not specified as a stable capability type. That left
open whether it was a contract, a struct, a primitive value, move-only state, or
a user-implementable interface.

Selfhost work needs a stable answer before more compiler storage moves into
Kizu source. At the same time, stabilizing user-defined allocators now would
force decisions about raw pointers, mutable byte slices, alignment, and
allocator state that are broader than the current v0.2 storage boundary.

## Decision

`std::mem::page_allocator()` is the stable v0.2 allocator factory:

```text
std::mem::page_allocator() -> Allocator
```

`Allocator` is a visible opaque capability type. In v0.2 it is not a user-facing
contract, not a struct with fields, and not a general interface that user code
can implement. Safe Kizu code can name the type, bind values of that type, and
pass those values to APIs that explicitly require an allocator capability.

`Allocator` is copyable. Passing an allocator to `Array<T>`, `String`,
`Map<K, V>`, `Box<T>`, or `arena<T>` reads the capability and does not move the
allocator binding. The created owner stores the runtime allocator handle it
needs for future allocation and `deinit`; the allocator value itself has no
user-visible cleanup method.

Allocating APIs must continue to expose allocation failure as `!T` or `!void`.
There is no hidden default allocator, no implicit global allocator, and no
fallback from a missing allocator argument to `page_allocator()`.

The trusted primitive behind `page_allocator()` remains a small host/runtime
boundary. Public Kizu code uses `std::mem::page_allocator()`. The primitive does
not expose raw pointers, allocation methods, mutable backing slices, allocator
metadata, or deallocation functions to safe Kizu.

User-defined allocators, fixed-buffer allocators, and testing allocators are
deferred to #549. They must not be added by widening `Allocator` implicitly; a
future issue must define the interface, ownership rules, alignment behavior,
failure diagnostics, and unsafe boundary before implementation.

## Consequences

- Existing selfhost code can rely on a stable public allocator factory.
- Owned storage constructors share one allocator capability model.
- Reusing one allocator for several owners is valid and does not consume the
  allocator binding.
- v0.2 avoids introducing a broad raw allocation API before pointer and mutable
  slice rules are mature.
- Custom allocator work remains possible, but it must be designed explicitly
  rather than inferred from the page allocator prototype.
