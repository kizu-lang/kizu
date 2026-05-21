# ADR-0070: package export surface

## Status

Accepted.

## Context

ADR-0050 keeps Kizu visibility default-private and uses one `pub` modifier for
top-level declarations and struct fields. That is enough inside one module, but
the standard library needs implementation modules that are shared across std
source files without becoming user-facing API.

Adding `internal`, `pub(crate)`, `pub(super)`, or re-exports would increase the
language surface and the resolver work before the self-host compiler needs it.

## Decision

`kizu.toml` accepts an optional `[modules].exports` string array.

`exports` lists package-qualified module paths that are visible outside the
package:

```toml
[modules]
root = "src/main.kizu"
paths = ["src"]
exports = ["app", "app::lexer"]
```

Rules:

- `pub` still exposes declarations and fields to modules that may import the
  defining module.
- a module not listed in `exports` is package-internal.
- package-internal modules may contain `pub` declarations shared by modules in
  the same package.
- external code cannot import or refer to a package-internal module.
- if `exports` is omitted, the root module is exported.
- `exports` does not add alias imports, wildcard imports, re-exports, or
  item-level visibility modifiers.

The standard library uses this boundary to keep modules such as
`std::path_bits` available to std source while rejecting user references to
them.

## Consequences

Package public API now has two layers: exported modules and `pub` declarations
inside those modules.

The compiler can compute a public interface hash from exported module public
declarations without treating package-internal helper modules as stable API.

This keeps the resolver thin for v0.3 while leaving room for future
`pub import` or item-level re-export if package manager compatibility later
requires it.
