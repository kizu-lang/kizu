# ADR-0054: package component test command

## Status

採用

## Context

Kizu self-host compiler modules are moving from the legacy
`selfhost/frontend.kizu` oracle harness into a normal multi-file package under
`selfhost/src`. The project needs a command that can be used by local workflows
and future pre-commit gates before package execution replaces any production
compiler path.

Kizu also avoids hidden test discovery, hidden global runtime behavior, and
implicit I/O. Test entry points must therefore be explicit enough for build
cache inputs and component ownership to remain clear.

## Decision

`kizu test <file>` continues to execute one single-file test program through the
interpreter.

`kizu test <package-dir>` and `kizu test <package-dir>/kizu.toml` are accepted
as package component test targets. In v0.3 this command:

1. loads the package manifest and module graph;
2. parses every package module;
3. runs the same type, ownership, borrow, visibility, and module-boundary checks
   as `kizu check`;
4. discovers explicit component test modules whose file name ends in
   `_test.kizu`;
5. counts public or private functions whose names end in `_test`;
6. flattens package modules into an explicit runtime program for this test
   command only;
7. registers module-qualified function, enum, and union names using the same
   final-segment import names that source code uses;
8. executes each `_test` function through the interpreter;
9. reports `test: ok (N component tests)` when every test function returns
   without an unhandled `!T` error.

The v0.3 package component runtime is a test runner, not a production module
runtime. It is intentionally explicit and command-scoped. Production compiler
switching still requires the component readiness gate and Go/Kizu oracle
comparison.

## Rules

- Hidden recursive test discovery outside configured `[modules].paths` is not
  allowed.
- Test modules are ordinary package modules and must use explicit `import`.
- `std::testing` assertions remain the public assertion API used by component
  test source.
- Imported user module calls execute through explicit module-qualified runtime
  names such as `lexer.lex` for source spelling `lexer::lex`.
- Qualified enum values such as `token::TokenKind::Eof` are resolved through the
  same explicit module-qualified runtime names.
- A package with no `_test.kizu` module or no `_test` function fails as an
  invalid package test target.
- The command must not introduce hidden global I/O, a hidden allocator, or a
  detached task runtime.

## Consequences

- `selfhost/src` can contain executable component tests for explicit module
  boundaries.
- The command gives `just`, pre-commit, and PR verification a stable hook for
  self-host component tests.
- The package runtime remains narrower than a production runtime, so production
  switching still depends on component-specific oracle evidence.
