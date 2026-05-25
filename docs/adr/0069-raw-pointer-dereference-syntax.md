# ADR-0069: raw pointer dereference syntax

## Status

Accepted.

## Context

Kizu already has raw pointer types for C ABI and low-level code:

```text
ptr<T>
ptr<const T>
?ptr<T>
?ptr<const T>
```

Before this ADR, raw pointer access was expressed only through `ptr_read` and
`ptr_write`. That kept the `@unsafe` boundary explicit, but it made struct pointer
updates harder to read than the operation being performed:

```kizu
@unsafe(ptr_write) {
    ptr_write(node, updated_node);
}
```

ADR-0068 also made checked safe-borrow field access direct:

```kizu
user.name = "bob";
```

Raw pointer access needs a different surface. It should stay visually explicit
because the compiler does not prove raw pointer lifetime, provenance, aliasing,
or nullability obligations.

## Decision

Kizu supports explicit raw pointer dereference with postfix `.*`, only inside
`@unsafe(ptr_deref)`:

```kizu
@unsafe(ptr_deref) {
    let tag = node.*.tag;
    node.*.tag = 1;
    node.* = replacement;
}
```

The rules are:

- `ptr<T>` may be read with `p.*` inside `@unsafe(ptr_deref)`
- `ptr<T>` may be written with `p.* = value` inside `@unsafe(ptr_deref)`
- `ptr<T>` to a struct may use `p.*.field` for field read and assignment inside
  `@unsafe(ptr_deref)`
- `ptr<const T>` may be read with `p.*` inside `@unsafe(ptr_deref)`
- assignment through `ptr<const T>` is rejected
- nullable raw pointers, `?ptr<T>` and `?ptr<const T>`, cannot be directly
  dereferenced
- raw pointer field access without explicit dereference, such as `p.field`,
  remains rejected
- checked safe-borrow field access remains `borrow.field`

`ptr_read` and `ptr_write` remain available as explicit `@unsafe` builtins requiring
`@unsafe(ptr_read)` and `@unsafe(ptr_write)`. The syntax does not introduce
implicit pointer dereference.

## Consequences

- Safe borrow code and raw pointer code have distinct surfaces:

```text
&T / &var T  -> borrow.field
ptr<T>       -> @unsafe(ptr_deref) { pointer.*.field }
```

- Low-level code can mutate a field through a struct pointer without rebuilding
  an entire struct value.
- Raw pointer dereference remains visible at the use site and remains fenced by
  `@unsafe(ptr_deref)`.
