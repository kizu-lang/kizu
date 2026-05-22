# ADR-0068: safe borrow field access

## Status

Accepted.

## Context

Kizu originally required explicit postfix dereference for field mutation through
safe borrows:

```kizu
fn rename(user: &var User) -> void {
    user.*.name = "bob";
}
```

That made borrow boundaries visible, but it also made ordinary safe code noisy.
Kizu safe borrows can have a pointer-like implementation, but they are checked
borrow capabilities rather than raw pointers: the checker already knows whether
a binding is `&T` or `&var T`, enforces aliasing rules, and rejects moves through
borrowed storage.

## Decision

Kizu permits direct field access through safe borrow bindings:

```kizu
fn show(user: &User) -> void {
    print(user.name);
}

fn rename(user: &var User) -> void {
    user.name = "bob";
}
```

The rules are:

- `&T` and `&var T` may read fields with `borrow.field`
- only `&var T` may assign fields with `borrow.field = value`
- assigning through `&T` is rejected
- raw pointers remain outside this shortcut and require explicit unsafe
  operations or explicit `.*` dereference
- postfix `.*` remains available for explicit safe-borrow dereference and for
  copy reads such as `value.*`

## Consequences

- Safe Kizu code reads closer to ordinary struct access.
- The safe borrow and raw pointer boundary is clearer: automatic field access is
  a checked borrow feature, not a pointer feature.
- Existing explicit `borrow.*.field` code remains valid, but examples should use
  direct safe-borrow field access unless demonstrating dereference itself.

ADR-0034 is superseded for safe borrow field access. ADR-0069 defines raw
pointer dereference with the same postfix `.*` spelling inside `unsafe`.
