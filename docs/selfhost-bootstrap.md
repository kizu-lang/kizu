# No-Go Selfhost Bootstrap Contract

This document defines the stage contract for turning Kizu source into a compiler
binary whose production path does not depend on the Go implementation.

It is a contract for the remaining roadmap, not a claim that the no-Go compiler
already exists.

## Stage Names

| Stage | Artifact | Allowed implementation | Required output |
| --- | --- | --- | --- |
| stage0 | bootstrap compiler | Current Go compiler during transition, or a previously released Kizu compiler later | Builds the stage1 compiler from `selfhost/` and `std/` sources. |
| stage1 | Kizu-built compiler | Kizu compiler source built by stage0 | Builds stage2 from the same source tree and runs the supported selfhost checks. |
| stage2 | Kizu-built compiler | Stage1 compiler output | Matches stage1 fingerprints for the supported corpus. |

Stage0 is a bootstrap and oracle boundary. It is not the final production path.

## Final No-Go Definition

The final production `kizu` binary must not call these Go-owned implementations:

- Go lexer, parser, AST, resolver, type checker, ownership checker, or borrow
  checker
- Go interpreter as the execution engine for compiler logic
- Go `stdprim` runtime behavior for required std APIs
- Go IR lowering, LLVM/WASM/native backend code, or build-cache implementation
- Go `cmd/kizu` dispatch as the compiler CLI

Go may remain available only as an explicit stage0 bootstrap or oracle command.
It must be visibly separate from the production binary.

## Bootstrap Commands

The final bootstrap runner introduced by #459 must provide this shape:

```sh
just selfhost-bootstrap
```

It expands to the following logical steps:

```sh
just selfhost-bootstrap-stage1
just selfhost-bootstrap-stage2
just selfhost-bootstrap-compare
```

The concrete stage commands must use these artifact locations unless a later
issue updates this contract:

```text
target/selfhost/stage0/
target/selfhost/stage1/
target/selfhost/stage2/
target/selfhost/reports/
```

During the current transition, run this preflight before starting stage work:

```sh
just selfhost-bootstrap-preflight
```

The preflight runs the existing selfhost switch gate and an isolated cache smoke
test. It does not build stage1 or stage2.

## Artifact Contract

Each stage build must emit:

- compiler binary path
- input source manifest hash
- std source hash
- selfhost source hash
- target/backend/runtime mode
- cache key inputs
- diagnostic/oracle summary
- elapsed time
- artifact size

The stage comparison compares deterministic fingerprints. Timestamps, absolute
temporary paths, and host-specific linker metadata must be excluded from the
fingerprint or recorded as explained non-deterministic metadata.

## Cache Inputs

Bootstrap cache keys include:

- compiler version or stage fingerprint
- `kizu.toml` manifest hash
- module graph hash
- source hashes
- public interface hash
- std source hash
- target/backend/runtime mode
- optimization mode
- runtime ABI version

No stage command may populate an unbounded cache. Bootstrap commands must support
isolated cache directories for CI and local reproduction.

## Host Capability Boundary

Host access is allowed only through explicit runtime/ABI capabilities:

- allocator creation and allocation
- filesystem reads and writes
- stdout, stderr, and stdin
- process argv, env, and exit code
- target linking or object emission

These are runtime boundaries, not Go fallbacks. Public Kizu APIs must keep the
capability visible in signatures or constructors where the language model
requires it.

## CI Contract

Until #459 exists, CI should run:

```sh
just selfhost-bootstrap-preflight
go test ./...
```

After #459, CI should add:

```sh
just selfhost-bootstrap
```

Heavy cache/perf measurements remain explicit jobs unless a switch issue changes
the CI policy with recorded timing and cache-size evidence.

## Failure And Rollback Policy

- Any stage comparison mismatch blocks the production switch.
- Any oracle mismatch blocks the production switch.
- Any hidden fallback to Go blocks the production switch.
- Rollback is a revert of the explicit component switch or stage-runner change.
- A newly discovered blocker gets a new GitHub issue linked from #445 before the
  dependent roadmap issue is closed.

## Roadmap Issue Contract

Issues #447 through #461 depend on this bootstrap contract. If a later issue
needs to change a stage artifact, command, runtime boundary, or cache key rule,
it must update this document in the same PR.
