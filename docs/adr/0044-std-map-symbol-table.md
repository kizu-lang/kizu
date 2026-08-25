# ADR 0044: std::map::Map<K, V> Symbol Table Map

## Status

Accepted.

## Context

Lookup tables — keywords, symbols, scopes — need a hash map. A general-purpose
one brings hidden allocation, keys that outlive the buffer they were read from,
and a hashing contract users have to implement for their own types. The current
API is in `docs/std/map.md`; this records why it has the shape it does.

## Decision

The map owns its storage and its keys. The constructor takes an explicit
allocator capability, `insert` copies the key bytes into map storage, and
`deinit` invalidates the binding in the ownership checker. A borrowed lookup key
therefore never becomes a stored reference, and using a map after `deinit` is a
use-after-free the checker refuses.

The key is hashed and compared as the bytes it occupies. That one representation
is what decides which types may be keys: a type qualifies when its bytes are its
value — no padding to read, no pointer to follow, and no two byte patterns that
have to compare equal. `[]u8` and the integer types answer that, and they are
what `K` accepts.

### Rejected

| Option | Why not |
| --- | --- |
| `[]u8` keys only | The runtime already takes a key as (pointer, length), so an integer key is its own bytes and needs neither a second lookup path nor an encoding step at the call site. Refusing it made callers spell integer keys as strings to reach a table the runtime could already index. |
| Any copy type as a key | A struct's padding bytes are not part of its value, so two equal structs can hash apart. Deciding a layout for that is a separate decision from having integer keys. |
| `f32` / `f64` keys | `0.0` and `-0.0` are equal with different bytes, and a NaN is unequal to its own bytes. Byte equality is not float equality, and a key type that lies about equality is worse than one that is absent. |
| A `Hash` / `Eq` contract users implement | Nothing needs it yet: every accepted key hashes as its bytes. Adding the contract first would fix a shape before there is a type that requires it. |
| Owned keys (`String`, `Array<T>`) | The map already copies key bytes, so an owned key would give the map a second cleanup obligation and a second answer to what `key_at` hands back. Deferred. |
| Borrowed keys stored as-is | A stored key would outlive the buffer it was read from, which is the failure the copy exists to prevent. |
| Hidden default allocator | `map::new<K, V>()` would allocate without the call site saying so, against the explicit-capability rule. |

Deletion and map values crossing task/thread/channel boundaries stay deferred
until their ownership and borrow rules are specified. Owner values are settled
by ADR-0123, iteration order by ADR-0088.

## Consequences

- A frontend builds keyword and symbol tables without linear scans, and indexes
  by integer id without turning the id into text first.
- Safe Kizu does not store borrowed keys in owned maps.
- Widening `K` later is a decision about byte representation, not about the map:
  a type becomes a key when its bytes are settled.
