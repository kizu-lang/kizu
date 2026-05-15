# ADR-0050: visibility and diagnostics

## Status

Accepted.

## Context

Kizu needs clear module boundaries without hiding safety-critical implementation
details behind implicit behavior. Visibility also affects incremental checking:
public interfaces are dependency boundaries, while private implementation should
not force unnecessary downstream work once incremental compilation exists.

## Decision

Kizu uses default-private visibility.

Rules:

- top-level declarations are private by default
- `pub` exposes a top-level declaration to other modules
- struct fields are private by default
- `pub` exposes a struct field to other modules
- public enum tags are visible when the enum type is public
- public union variants are visible when the union type is public
- public function signatures may not expose private types
- external modules may not construct private fields
- external modules may not access private fields

The initial module system does not include `pub(crate)`, `pub(super)`,
`protected`, wildcard exports, or re-exports.

Diagnostics use source spans with:

- file
- byte start
- byte end
- line
- column

Multi-file diagnostics include one primary span and optional related spans.

Example:

```text
error: private function `lex_source` is not visible
  --> src/main.kizu:4:22

imported module `lexer` is here:
  --> src/main.kizu:1:8

help: mark it as `pub fn lex_source` or expose a public wrapper
```

Import-cycle diagnostics render the cycle:

```text
error: cyclic import detected
  app::a -> app::b -> app::a
```

## Consequences

The resolver and type checker must track declaration visibility and module
ownership for every named item.

The compiler must preserve enough span information from lexing through checking
to render multi-file diagnostics without falling back to vague internal errors.

Default-private visibility protects unsafe, allocator, raw pointer, and internal
representation details from leaking into stable public APIs.
