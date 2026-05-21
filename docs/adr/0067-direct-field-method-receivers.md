# ADR-0067: direct field method receivers

## Status

Accepted.

## Context

Kizu uses `.` for runtime field access and method calls. The v0.2 stdlib and
self-host compiler source need owned structs to delegate to storage fields:

```kizu
self.related.len()
self.related.append(item)
self.related.deinit()
```

Forcing every owned field into a temporary local would either hide cleanup
boundaries or make Kizu source stdlib awkward to write. At the same time,
blindly accepting `owner.field.deinit()` would leave a partially destroyed owner
value alive in safe code.

## Decision

Kizu v0.2 supports one-level direct field method receivers:

```kizu
owner.field.method(args)
```

The receiver path is still explicit and uses ordinary field access syntax. v0.2
does not support nested method receiver paths such as `owner.a.b.method()`.

Direct field receiver calls follow the field owner's ownership state:

- read-only methods may run when the owner and field are readable
- mutating methods may run only when the owner and field are not borrowed
- cleanup methods such as `field.deinit()` are allowed only inside the owning
  type's concrete `deinit(self: Owner) -> void` method
- after field cleanup, the field cannot be read, mutated, or cleaned again

General field cleanup remains rejected:

```kizu
registry.users.deinit(); // rejected outside Registry.deinit
```

The accepted cleanup shape is explicit owner cleanup:

```kizu
impl Registry {
    fn deinit(self: Registry) -> void {
        self.users.deinit();
    }
}
```

## Consequences

- Kizu source stdlib can implement wrapper cleanup and storage forwarding
  without adding Go-only special cases.
- Safe code cannot observe a partially destroyed owner value after arbitrary
  field cleanup.
- The ownership checker must track direct field cleanup state during a function
  body.
- Nested receiver paths stay deferred until the borrow and partial-cleanup
  model needs them.
