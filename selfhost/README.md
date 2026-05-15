# Kizu Self-Host Frontend Skeleton

This directory contains Kizu source for the future self-host compiler frontend.

The current goal is not to replace the Go implementation. The skeleton is a
small compatibility probe that verifies whether the v0.2 standard library is
enough to write compiler-shaped Kizu code.

## Current Entry

```sh
kizu run selfhost/frontend.kizu
```

The skeleton currently includes:

- token representation
- lexer-shaped source scanning with `std::mem`
- token storage with `std::array::Array<T>`
- diagnostic text construction with `std::string::String`
- parser summary shape

## Conformance Reuse

The future self-host compiler must reuse `tests/conformance/v0_*.json` and
produce the same pass/fail behavior as the Go implementation before it can
replace any production path.

For now, Go tests parse, check, and run the skeleton source. Missing APIs found
while growing this directory must be reflected back into the relevant v0.2
stdlib issue instead of becoming isolated TODOs here.

Current stdlib feedback is tracked in [`STDLIB.md`](STDLIB.md).

## Source Policy

Kizu compiler source in this directory follows the stricter compiler-code style:

- module comment at the top of each file
- comment immediately before each function
- line width at most 100 columns
- function body at most 70 lines
- function body at most 45 semicolon-terminated statements
