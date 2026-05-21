# ADR 0058: Selfhost AST NodeId Arena

## Status

Accepted

## Context

The selfhost compiler frontend needs an AST representation that does not copy the
Go implementation's interface, pointer, and slice shape. Declarations,
statements, and expressions need one stable representation with spans and
variable-length child lists.

Kizu v0.2 currently rejects `std::array::Array<std::arena::Handle<T>>` and structs that
contain handles because general handle storage needs arena lifetime rules that
are not complete. A compiler AST is a narrower case: node handles are owned by
one AST arena and are only resolved through AST methods.

## Decision

`std::kizu::ast::Ast` owns:

- `std::arena::Arena<AstNode>(allocator)` for node storage
- `std::array::Array<NodeId>` for variable-length child ranges
- `SourceFile` metadata

`Ast.deinit()` explicitly releases both node arena storage and child array storage.

`NodeId` is an AST-scoped opaque wrapper over `std::arena::Handle<AstNode>`. It is copyable
as an id value, but it does not expose pointer operations and is resolved only
through `Ast.get`.

`Ast` is an owner, not a copy scalar. The checker tracks known local `Ast`
provenance for `NodeId` values produced by AST construction methods and rejects
known cross-AST `Ast.get(id)` calls. Function parameters can still carry unknown
provenance until the language has a richer owner-parameter relation.

`AstNode` stores a `Span` and an `AstData` union. Recursive relationships use
`NodeId`, and variable-length relationships use `ChildRange` into the AST-owned
child list. The initial shape includes ranges for function params, struct
fields, block statements, call args, and match arms.

## Consequences

- The Go checker keeps the general `Array<std::arena::Handle<T>>` rejection.
- A narrow exception allows `std::kizu::ast::NodeId` in `std::array::Array`.
- Copying `NodeId` copies an opaque id, not an AST node or raw pointer.
- Parser APIs return `ParseResult { ast, root }` so a root `NodeId` is paired
  with the AST that owns it.
- The initial parser path covers function declarations, empty parameter lists,
  blocks with any number of return statements, return, call, and binary-add
  expression nodes without parser builtins.
- Future parser work can add parsed params, struct declaration syntax, richer
  expressions, and parsed match-arm ranges without adding parser builtins.

ADR-0062 refines the allowed `AstNode` payload set, allocator semantics,
runtime handle diagnostic responsibility, and selfhost cleanup discipline for
this AST arena.

## Non-goals

- This does not make arbitrary handle wrappers safe for array storage.
- This does not expose raw pointers or mutable arena storage to safe Kizu.
- This does not complete the full parser grammar for Issue #393.
