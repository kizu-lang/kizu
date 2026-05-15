# Self-Host Stdlib Feedback

This file records which v0.2 stdlib issues are exercised by the current
self-host frontend skeleton.

## Exercised Now

- #24 `std::mem`: used for byte length, checked byte access, byte comparison,
  and checked slicing in `lex_source`.
- #27 `std::array::Array<T>`: used as the owned token stream.
- #29 `std::string::String`: used for owned diagnostic text construction and
  local `as_bytes` views.
- #28 `std::map::Map<K, V>`: used as a `[]const u8` keyed keyword table.
- #26 `std::fs` / `std::path`: used to load a source fixture, validate path
  decomposition, and inspect file metadata.
- #25 `std::io` / `std::process`: used for explicit stdout writes and prototype
  CLI path arguments.
- #30 `std::testing`: used for Kizu-native component assertions inside the
  frontend smoke path.

## v0.2 Handoff

The minimal stdlib needed before v0.3 self-host compiler work is now exercised
by `selfhost/frontend.kizu`. Further work belongs in #31 unless it discovers a
stdlib gap that blocks compiler-shaped Kizu code.

Missing APIs discovered here must be added to the corresponding issue instead
of becoming local-only TODOs.
