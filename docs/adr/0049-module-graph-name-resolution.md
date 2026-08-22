# ADR-0049: module graph and name resolution

## Status

Accepted.

## Context

The original one-file-per-module mapping made a source split change namespace,
visibility, imports, and cache identity at once. It also forced test files into
child modules, even when they were testing private implementation details of
the package beside them. Kizu needs multi-file compilation without adding
marker files or per-file module declarations.

## Rationale

One directory is one module. A directory is the stable unit people already use
to group one responsibility, so dividing an implementation across files does
not change its public name. Top-level declarations and ordinary private fields
are shared across those files, while imports stay file-local so each file still
states its external dependencies where a reviewer reads them.

Files ending in `_test.kizu` join that same module only for package tests. This
gives white-box tests access to private declarations without putting test code
in production commands. Black-box or dependency-reversing tests use a separate
test-only directory module and import the public API; test mode does not weaken
cycle detection.

The configured source-root directory already identifies the package root
module, so a separate `[modules].root` duplicates the filesystem and is
removed. Filenames such as `main.kizu` and `mod.kizu` remain ordinary names
rather than alternate module declarations.

`unsafe struct` construction and field writes are the deliberate exception to
module-wide privacy. ADR-0089 made their declaration file the audit boundary;
letting another file mutate the invariant merely because both files share a
module would silently widen that memory-safety boundary.

The exact mapping, visibility, import, test-file, and diagnostic rules live in
`SPEC.md`.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Keep one file as one module | Splitting an implementation changes names and visibility, and recreates Go packages as artificial Kizu submodules. |
| Support both `foo.kizu` and `foo/mod.kizu` | Two filesystem spellings for one module create collisions and make ownership ambiguous. |
| Require `mod.kizu` | Repeated marker files add configuration without identifying anything the directory does not already identify. |
| Declare a module at the top of every file | Repetition permits the declared name and directory to disagree and adds another fact every file must maintain. |
| Merge imports across a module | A file could rely on an import it never declares, hiding its dependencies from local review. |
| Treat `_test.kizu` as a child module | White-box tests would need production declarations to become public or would duplicate helpers solely to cross the artificial boundary. |
| Let tests ignore import cycles | Test builds would have a different and less predictable module graph than production. |
| Make every private `unsafe struct` mutation module-wide | The memory-safety audit set would grow from one file to an arbitrarily large directory. |
