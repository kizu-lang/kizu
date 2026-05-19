# ADR 0058: Selfhost AST NodeId Arena

## Status

Accepted

## Context

The selfhost compiler frontend needs an AST representation that does not copy the
Go implementation's interface, pointer, and slice shape. Declarations,
statements, and expressions need one stable representation with spans and
variable-length child lists.

Kizu v0.2 currently rejects `std::array::Array<handle<T>>` and structs that
contain handles because general handle storage needs arena lifetime rules that
are not complete. A compiler AST is a narrower case: node handles are owned by
one AST arena and are only resolved through AST methods.

## Decision

`std::kizu::ast::Ast` owns:

- `arena<AstNode>` for node storage
- `std::array::Array<NodeId>` for variable-length child ranges
- `SourceFile` metadata

`NodeId` is an AST-scoped opaque wrapper over `handle<AstNode>`. It is copyable
as an id value, but it does not expose pointer operations and is resolved only
through `Ast.get`.

`AstNode` stores a `Span` and an `AstData` union. Recursive relationships use
`NodeId`, and variable-length relationships use `ChildRange` into the AST-owned
child list.

## Consequences

- The Go checker keeps the general `Array<handle<T>>` rejection.
- A narrow exception allows `std::kizu::ast::NodeId` in `std::array::Array`.
- Copying `NodeId` copies an opaque id, not an AST node or raw pointer.
- Parser APIs return `ParseResult { ast, root }` so a root `NodeId` is paired
  with the AST that owns it.
- Future parser work can add block, return, call, binary expression, params,
  fields, statements, args, and match-arm ranges without adding parser builtins.

## Non-goals

- This does not make arbitrary handle wrappers safe for array storage.
- This does not expose raw pointers or mutable arena storage to safe Kizu.
- This does not complete the full parser grammar for Issue #393.
