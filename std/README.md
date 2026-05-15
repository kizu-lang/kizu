# Kizu std Source Skeleton

`std/` is the future home of Kizu-written standard-library wrappers.

In v0.3 these sources are a skeleton only. Runtime behavior still comes from
explicit Go-backed primitives in `internal/types`, `internal/ownership`, and
`internal/interp`. The public declarations here document the wrapper boundary
and participate in build-cache std source hashing.

Rules:

- user packages cannot use the `std` package name
- host I/O, process access, allocation, threads, mutexes, atomics, and raw
  storage remain trusted primitives
- Kizu source should migrate pure helpers first
- wrappers must not introduce hidden allocators, hidden runtimes, implicit I/O,
  or silent fallback behavior
