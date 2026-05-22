# ADR 0044: std::map::Map<K, V> Symbol Table Map

## Status

Accepted.

## Context

The Kizu self-host compiler needs deterministic lookup tables for keywords,
symbols, and scopes. A general-purpose hash map is useful, but v0.2 must avoid
introducing hidden allocation, unbounded ownership complexity, or borrowed keys
that can outlive their source buffer.

## Decision

`std::map::Map<K, V>` is introduced as a conservative owned map prototype.

```text
std::map::Map<[]u8, V>(allocator: Allocator) -> std::map::Map<[]u8, V>
map.insert(key: []u8, value: V) -> !void
map.get(key: []u8) -> !V
map.contains(key: []u8) -> bool
map.len() -> i64
map.deinit() -> void
```

The v0.2 implementation supports only `[]u8` keys. `insert` copies the
key bytes into owned map storage, so borrowed lookup keys never become stored
references. Values are restricted to copy types in v0.2, which lets `get`
return `!V` by value without moving out of the map or exposing long-lived
borrows.

The constructor requires an explicit allocator capability. `deinit` invalidates
the map binding in the ownership checker. Using a map after `deinit` is a
use-after-free style error in safe Kizu.

Iteration, deletion, non-copy values, custom hash/equality, and map values
crossing task/thread/channel boundaries are deferred until their ownership and
borrow rules are specified.

## Consequences

- The self-host frontend can build keyword and symbol tables without linear
  scans.
- Safe Kizu does not store borrowed keys in owned maps.
- The v0.2 map is intentionally narrower than a production hash map.
- Future extensions must preserve explicit allocator and cleanup boundaries.
