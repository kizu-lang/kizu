# ADR-0054: package component test command

## Status

採用

## Context

Kizu self-host compiler modules are moving from the legacy
`selfhost/frontend.kizu` oracle harness into a normal multi-file package under
`selfhost/src`. The project needs a command that can be used by local workflows
and future pre-commit gates before the package interpreter is ready to execute
cross-module Kizu code directly.

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
6. reports `test: ok (N component tests)` when at least one component test is
   present and all package checks pass.

The v0.3 package component command is a static component gate, not a production
package runtime. Cross-module test function execution remains blocked until the
package interpreter or self-host runner has a documented module runtime model.

## Rules

- Hidden recursive test discovery outside configured `[modules].paths` is not
  allowed.
- Test modules are ordinary package modules and must use explicit `import`.
- `std::testing` assertions remain the public assertion API used by component
  test source.
- A package with no `_test.kizu` module or no `_test` function fails as an
  invalid package test target.
- The command must not introduce hidden global I/O, a hidden allocator, or a
  detached task runtime.

## Consequences

- `selfhost/src` can contain checkable component tests before the package
  interpreter can execute cross-module calls.
- The command gives `just`, pre-commit, and PR verification a stable hook for
  self-host component tests.
- The runtime execution gap is explicit and remains tracked by the self-host
  readiness gate instead of being hidden behind Go fallback behavior.
