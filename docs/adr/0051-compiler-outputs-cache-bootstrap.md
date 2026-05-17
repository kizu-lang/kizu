# ADR-0051: compiler outputs, build cache, and bootstrap criteria

## Status

Accepted.

## Context

Kizu should avoid hidden artifact generation and unbounded build cache growth.
The self-host compiler also needs objective switch criteria so the Go
implementation remains an oracle until the Kizu implementation proves
equivalent.

## Decision

Compiler artifacts live under `target/` by output family:

```text
target/
  check/
  interp/
  ir/
  native/
  wasm/
  c/
  cache/
```

Commands:

```text
kizu check
kizu run
kizu build --emit ir
kizu build --target wasm
kizu build --target native
```

Rules:

- `kizu check` does not create durable artifacts by default
- `kizu run` may create transient interpreter data only
- IR, WASM, native, and C outputs are produced only by explicit build commands
- debug artifacts are opt-in
- artifact names include package, target, and mode

Build cache keys include:

- compiler version
- manifest path and manifest content hash
- resolved module graph hash
- source content hashes
- public interface hash
- target
- backend
- optimization mode
- stdlib version or hash

Cache behavior:

- no-op rebuild does no compiler work beyond cheap validation
- private implementation edits should avoid downstream rechecks when possible
- public interface edits recheck dependents
- cache has a default size limit
- `kizu cache status` reports cache size and major key groups
- `kizu cache prune` removes entries predictably
- `kizu why-rebuild` explains invalidation reasons

Self-host switch criteria:

1. Kizu lexer token stream equals the Go lexer oracle.
2. Kizu parser AST summary equals the Go parser oracle.
3. Kizu diagnostics equal the Go diagnostics oracle for covered cases.
4. Kizu type checker equals the Go type checker oracle.
5. Kizu ownership checker equals the Go ownership checker oracle.
6. Kizu IR equals the Go IR oracle for covered cases.
7. Kizu backend output passes the same smoke tests as the Go backend.
8. The Kizu compiler checks and builds its own source.

Before any production path switches from Go to Kizu, these must pass:

- conformance positive and negative tests
- examples
- memory-safety negative tests
- build cache smoke tests
- self-host compiler source checks
- zero unexplained Go/Kizu oracle diffs

Any allowed oracle diff must be documented in an ADR before it is accepted.

## Consequences

The compiler must separate check, run, and build paths. Heavy artifacts and
performance measurements remain explicit so CI and local cache size stay
predictable.

The Go implementation remains the reference oracle until self-host behavior is
measured and equivalent.
