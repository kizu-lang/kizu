# ADR-0048: module import and manifest policy

## Status

Accepted.

## Context

The self-host compiler cannot stay single-file forever. Kizu needs a module
graph before parser, resolver, type checker, ownership checker, and build cache
work can become production-shaped.

The manifest format also affects bootstrap complexity. A custom format or KDL
would add another parser before the language is ready to self-host.

## Decision

Kizu uses `kizu.toml` as the package manifest format.

The manifest is declarative only. It must not contain executable logic, build
scripts, conditional imports, or compiler plugins.

Initial manifest shape:

```toml
[package]
name = "app"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
```

The package name is the root user namespace. If `name = "app"`, files under the
configured module paths are imported with paths such as `app::lexer` and
`app::parser::ast`.

Kizu source imports use explicit top-level declarations:

```kizu
import app::lexer;
import app::parser::ast;

pub fn main() -> void {
    let tokens = try lexer::lex("fn main() -> void { return void; }");
    return void;
}
```

`std::...` is the built-in standard-library namespace and remains available
without a user import.

Visibility rules:

- declarations are private by default
- `pub` marks externally visible top-level declarations
- struct fields are private by default
- `pub` fields are externally visible
- enum tags and union variants are externally visible when their type is public
- `pub(crate)`, `pub(super)`, `protected`, wildcard exports, and re-exports are
  not part of the initial module system

Import rules:

- imports are top-level only
- wildcard imports are not allowed
- relative imports are not allowed
- alias imports are postponed
- re-exports are postponed
- cyclic imports are compile errors
- imported modules are referenced by their last path segment

Example:

```kizu
import app::compiler::lexer;

pub fn main() -> void {
    let tokens = try lexer::lex("...");
    return void;
}
```

## Consequences

The compiler needs an explicit resolver phase between parsing and type checking.

The build cache key must include at least:

- compiler version
- manifest path and manifest content hash
- root module
- resolved module graph
- source content hashes
- target triple or backend target

Public interfaces become part of the stable dependency boundary. Private
implementation changes should be eligible for narrower downstream rechecks once
incremental compilation exists.

KDL can be reconsidered later, but `kizu.toml` is the zero-debt default until a
concrete benefit outweighs the extra parser and tooling cost.
