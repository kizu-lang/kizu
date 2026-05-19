# Kizu Selfhost Compiler Package

This package is the source-owned home for Kizu compiler components that will
eventually replace Go-owned frontend phases.

The initial skeleton defines module boundaries only:

- `selfhost::token`
- `selfhost::lexer`
- `selfhost::ast`
- `selfhost::parser`
- `selfhost::diagnostic`
- `selfhost::resolver`
- `selfhost::types`
- `selfhost::ownership`
- `selfhost::ir`
- `selfhost::backend`

Generated `target/selfhost*` artifacts are not source of truth. New selfhost
work should land in this package or in `std::kizu` when it is reusable stdlib
compiler infrastructure.

The token and lexer boundary is currently `std::kizu::lexer`. Selfhost compiler
modules should reuse that implementation instead of duplicating token shapes in
`selfhost::lexer`; the oracle suite compares the direct token stream and the
Array-backed `tokenize` path against the Go lexer.

Check the package with:

```sh
kizu check selfhost
```

Run the current Go/Kizu component oracle suite with:

```sh
just selfhost-oracle
```
