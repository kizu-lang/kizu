# ADR 0034: Zig-Style Dereference and Field Assignment

Status: Superseded by [ADR-0068](0068-safe-borrow-field-access.md) for safe
borrow field access and [ADR-0069](0069-raw-pointer-dereference-syntax.md) for
raw pointer dereference.

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

The original v0.1 rules were:

- `let` binding assignment is rejected.
- Field assignment through a `let` binding is rejected.
- Field assignment through a `var` binding is allowed.
- Assignment through `&T` is rejected.
- Assignment through `&mut T` is allowed with explicit `.*`.
- v0.1 did not add automatic safe-borrow field access.

## Consequences

Mutation originally remained visually explicit at borrow boundaries.
ADR-0068 changes that tradeoff for safe borrow field access while keeping raw
pointer behavior explicit. ADR-0069 keeps raw pointer dereference explicit with
postfix `.*` inside `unsafe`.
