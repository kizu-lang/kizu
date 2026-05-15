# ADR-0049: module graph and name resolution

## Status

Accepted.

## Context

Kizu needs multi-file compilation before the self-host compiler can become
practical. The module graph must stay explicit enough for readable diagnostics,
small rebuild scopes, and predictable build cache keys.

## Decision

`kizu.toml` is the only package root marker.

`[package].name` is the package root namespace. For `name = "app"`, source files
under configured module paths resolve as:

```text
src/main.kizu       -> app
src/lexer.kizu      -> app::lexer
src/parser/mod.kizu -> app::parser
src/parser/ast.kizu -> app::parser::ast
```

The root module is `[modules].root`. Additional source roots come from
`[modules].paths`.

The compiler rejects:

- duplicate module paths
- imports outside configured module paths
- missing imports
- cyclic imports
- ambiguous imports with the same last path segment

Name resolution order:

1. local bindings
2. current module top-level declarations
3. imported module names by last segment
4. built-in root namespace `std`
5. error

Imported modules are referenced by their last segment:

```kizu
import app::compiler::lexer;

pub fn main() -> void {
    let tokens = try lexer::lex("...");
    return void;
}
```

Local declarations may not shadow imported module names. User packages may not
use the package name `std`.

## Consequences

The compiler needs a resolver phase between parsing and type checking.

Build cache keys can include a resolved module graph hash and per-module source
hashes. Single-file edits can later be narrowed to the affected module and its
dependents.

Kizu intentionally does not support wildcard imports, relative imports, alias
imports, or re-exports in the initial module system.
