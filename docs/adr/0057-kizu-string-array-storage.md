# ADR 0057: Kizu String over Array storage

Status: Accepted

## Context

ADR 0056 kept `std::string::String` storage behind `std::builtin::string_*`
because safe Kizu did not yet have a public owned byte storage type. That kept
the public wrapper in Kizu, but append, reserve, truncate, view, and cleanup
behavior still lived in Go as string-specific logic.

The next migration goal is stricter: `std::string::String` should be ordinary
stdlib source wherever possible, with Go retaining only lower-level runtime
storage primitives.

## Decision

Represent `std::string::String` in Kizu source as a std-owned struct backed by
`std::array::Array<u8>`.

`std/src/string/string.kizu` owns the public constructor and all public `String`
methods. Go no longer provides `std::builtin::string_*` or an `OwnedString`
runtime value.

The remaining trusted boundary is `std::array::Array<T>` storage. For
`String`, the Array boundary provides capacity management, truncation, cleanup,
and a read-only `Array<u8>.as_bytes()` view. That is a storage primitive, not a
string implementation.

The hosted selfhost runtime template is an exception at the ABI artifact layer:
it may define String byte-buffer runtime symbols for `selfhost-abi-v0` so the
bootstrap artifact can run without Go stdprim storage. Those symbols are not
public `std::builtin::string_*` calls and do not change the Kizu-facing stdlib
decision in this ADR.

The Array helpers needed only by `String` are std-only in v0.2; they are not
public `std::array` API.

The `String` storage field remains private to std source. User code cannot
access or mutate the backing `Array<u8>` directly.

## Safety Rules

- `String` construction still requires an explicit `Allocator`.
- `String` remains non-copy / move-only.
- `append_bytes` is implemented in Kizu by copying source bytes one byte at a
  time into the backing Array.
- `append_byte`, `reserve`, `truncate`, `clear`, `len`, `capacity`,
  `as_bytes`, and `deinit` are public Kizu methods.
- `truncate` and `reserve` keep String-specific diagnostics in Kizu source.
- `as_bytes` remains a local read-only view at the checker boundary.
- Safe Kizu still receives no raw pointer or mutable backing slice.

## Consequences

- The Go interpreter/compiler no longer has string-specific storage behavior.
- `std::array::Array` remains a runtime storage primitive and is still tracked
  by #360 for broader wrapper splitting.
- ADR 0056's rejection of `String` over Array is superseded for `String` only;
  the broader warning about public `OwnedBytes` and generic storage provenance
  still stands.
