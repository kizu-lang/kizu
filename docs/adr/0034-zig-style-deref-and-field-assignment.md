# ADR 0034: Zig-Style Dereference and Field Assignment

Status: Accepted

## Context

Kizu v0.1 needs enough mutation semantics to make `var`, `&mut T`, and struct fields useful.
The language is intentionally Zig-leaning for low-level syntax, while keeping memory-safety
checks explicit.

## Decision

Kizu uses postfix `.*` for explicit dereference.

```kizu
fn rename(user: &mut User) -> void {
    user.*.name = "bob";
}
```

Kizu supports field assignment on mutable values.

```kizu
fn main() -> void {
    var user = User { name: "alice", age: 30 };
    user.age = 31;
}
```

The v0.1 rules are:

- `let` binding assignment is rejected.
- Field assignment through a `let` binding is rejected.
- Field assignment through a `var` binding is allowed.
- Assignment through `&T` is rejected.
- Assignment through `&mut T` is allowed with explicit `.*`.
- v0.1 does not add Zig-style automatic dereference for field access.

## Consequences

Mutation remains visually explicit at borrow boundaries.
`user.*.field` is noisier than automatic dereference, but it avoids hidden pointer behavior in
the first memory-safety release.
