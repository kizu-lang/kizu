# Self-Host Stdlib Feedback

This file records which v0.2 stdlib issues are exercised by the current
self-host frontend skeleton.

## Exercised Now

- #24 `std::mem`: used for byte length, checked byte access, byte comparison,
  and checked slicing in `lex_source`.
- #27 `std::array::Array<T>`: used as the owned token stream.
- #29 `std::string::String`: used for owned diagnostic text construction and
  local `as_bytes` views.

## Still Needed

- #28 `std::map::Map<K, V>`: needed when parser/checker skeleton grows symbol
  tables, scopes, or keyword tables beyond linear byte checks.
- #26 `std::fs` / `std::path`: needed when the self-host runner loads source
  files instead of using in-memory smoke input.
- #25 `std::io` / `std::process`: needed for CLI args, stdout/stderr, and exit
  code behavior.
- #30 `std::testing`: needed for Kizu-native lexer/parser component tests.

Missing APIs discovered here must be added to the corresponding issue instead
of becoming local-only TODOs.
