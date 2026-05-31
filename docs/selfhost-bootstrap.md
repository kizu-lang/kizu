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

The minimum selfhost compiler CLI introduced by #458 has two supported commands:

```sh
selfhost check selfhost
selfhost stage selfhost
```

`check selfhost` loads `selfhost/` and reachable `std/` wrappers, then runs the
Kizu-owned source, resolver, type, and ownership phases. On success it writes:

```text
check: ok
```

and returns exit code `0`. Unsupported commands or targets write a diagnostic to
stderr and return exit code `64`.

`stage selfhost` emits the deterministic stage artifacts under `target/selfhost`:

```text
target/selfhost/selfhost.ir
target/selfhost/selfhost.ir.manifest
target/selfhost/selfhost.ll
target/selfhost/selfhost.ll.meta
target/selfhost/selfhost.storage.ll
target/selfhost/selfhost.storage.ll.meta
target/selfhost/selfhost.host.ll
target/selfhost/selfhost.host.ll.meta
```

On success it writes `stage: ok`, each emitted backend/runtime artifact path,
and returns exit code `0`. These are the stage input/output paths that #459 must
use unless a later issue updates this contract.

Validate this CLI contract with:

```sh
just selfhost-cli-gate
just selfhost-backend-artifact-gate
```

`just selfhost-cli-gate` validates the Kizu source CLI through the stage0
interpreter. `just selfhost-backend-artifact-gate` builds a native selfhost
executable with the Go compiler only as the explicit stage0 bootstrap, runs
that executable's `stage selfhost`, then links the generated `selfhost.ll`
artifact with the hosted runtime and runs the artifact CLI entry for `check
selfhost`, `stage selfhost`, and unsupported command diagnostics. The latter is
the no-Go smoke for the #458 artifact CLI path; it no longer renders the backend
artifact through the Go interpreter.

`just selfhost-parse-parity-gate` validates the #525 `parse <file>` slice
through `target/selfhost/stage2/selfhost` after `just selfhost-bootstrap`. It
uses `selfhost/tests/cli/parse-parity.tsv` and checked-in stdout/stderr goldens;
Go `cmd/kizu` is not part of that validation path.

The bootstrap runner introduced by #459 provides this shape:

```sh
just selfhost-bootstrap
```

The runner performs these logical steps internally:

1. stage0 uses the explicit Go native bootstrap compiler to build a selfhost
   executable from source and emit the supported LLVM artifact set with
   `stage selfhost`.
2. stage1 links the emitted Kizu-built artifact and runs `check selfhost` plus
   `stage selfhost`.
3. stage1 materializes the supported stage2 artifact set through the hosted
   `stage selfhost` command.
4. stage2 links from the stage2 artifact set and runs the same supported CLI
   commands.
5. the runner compares user-visible stdout/stderr/exit codes and deterministic
   SHA-256 fingerprints for the supported artifact set.

The concrete stage commands must use these artifact locations unless a later
issue updates this contract:

```text
target/selfhost/stage0/
target/selfhost/stage1/
target/selfhost/stage2/
target/selfhost/reports/
```

The durable report is:

```text
target/selfhost/reports/bootstrap.txt
```

It records the stage0 compiler mode, the stage0 backend artifact report,
stage1/stage2 executables, command output fingerprints, artifact fingerprints,
cache directory and size, elapsed time, and comparison status. Stage1 and
stage2 are required to run in `hosted-artifact no-go` mode; stage0 is the only
explicit Go bootstrap boundary and is not a production fallback.

During the current transition, run this preflight before starting stage work:

```sh
just selfhost-bootstrap-preflight
```

The preflight runs the existing selfhost switch gate and an isolated cache smoke
test. After #461, the switch gate includes the explicit bootstrap runner, so the
preflight builds stage1/stage2 and then runs production and corpus checks through
the hosted artifact.

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

The minimum runtime ABI for the first selfhost artifact is
[`docs/selfhost-runtime-abi.md`](selfhost-runtime-abi.md). Backend, runtime, and
host-primitive roadmap issues must update that document when they add a new
reachable ABI shape.

## CI Contract

After #461, CI for the first runnable selfhost path should run:

```sh
just selfhost-production-from-scratch
go test ./...
```

`just selfhost-production-from-scratch` builds the stage2 hosted artifact
through the explicit stage0 native bootstrap boundary, then runs the #458
production command path, the supported corpus, parse parity, check parity, run
parity, and test parity through that artifact.

Local jobs that already have a passing stage2 artifact can run the fast gates:

```sh
just selfhost-fast-gate
just selfhost-production-gate
just selfhost-corpus-gate
just selfhost-parse-parity-gate
just selfhost-check-parity-gate
just selfhost-fmt-parity-gate
just selfhost-run-parity-gate
just selfhost-test-parity-gate
```

`just selfhost-fast-gate` runs the hosted artifact gates above without
rebuilding bootstrap artifacts.

`just selfhost-production-gate` is the #461 production-boundary gate. It runs
`target/selfhost/stage2/selfhost check selfhost`, `stage selfhost`, and the
unsupported-command diagnostic path directly through the hosted stage2 artifact.
The test harness does not call Go lexer, parser, resolver, type checker,
ownership checker, interpreter, stdprim, IR, backend, cache, or `cmd/kizu`
dispatch for that production path. The durable report is:

```text
target/selfhost/reports/production-boundary.txt
```

`just selfhost-corpus-gate` is the #460 manifest-driven production-switch
corpus gate. It runs only entries from
[`selfhost/tests/supported-corpus.tsv`](../selfhost/tests/supported-corpus.tsv)
through the hosted stage2 artifact and compares user-visible stdout, stderr, and
exit codes. The corpus gate reuses the stage2 artifact and passing bootstrap
report from `just selfhost-bootstrap`; it does not rebuild bootstrap artifacts
inside the corpus test. Clean jobs can use
`just selfhost-corpus-gate-from-scratch` as the explicit combined command.

`just selfhost-check-parity-gate` is the #530 fast parity gate for the bounded
`check <file>` slice. It runs only entries from
[`selfhost/tests/cli/check-parity.tsv`](../selfhost/tests/cli/check-parity.tsv)
through the hosted stage2 artifact and compares checked-in stdout/stderr
goldens plus exit codes. It reuses the stage2 artifact and passing bootstrap
report; it does not rebuild bootstrap artifacts inside the parity test.

`just selfhost-fmt-parity-gate` is the #1073 fast parity gate for the bounded
`fmt <file>` and `fmt --write <file>` slices. It runs only entries from
[`selfhost/tests/cli/fmt-parity.tsv`](../selfhost/tests/cli/fmt-parity.tsv)
through the hosted stage2 artifact, compares checked-in stdout/stderr goldens,
mirrors successful stdout rows through `fmt --write`, and checks that write
failures preserve the copied input file.

Heavy cache/perf measurements remain explicit jobs unless a switch issue changes
the CI policy with recorded timing and cache-size evidence.

## Local Gate And Recovery Checklist

Use the narrowest gate that proves the changed boundary:

| Change kind | First gate | Checkpoint gate |
| --- | --- | --- |
| selfhost source without backend/runtime changes | `just selfhost-fast-gate` when `target/selfhost/stage2/selfhost` exists | `just selfhost-production-from-scratch` before merge |
| executable run/test lowering | targeted Go test or `just selfhost-run-parity-gate` / `just selfhost-test-parity-gate` | `just selfhost-native-source-gate` and `just selfhost-fast-gate` |
| backend/native/runtime artifact behavior | `just selfhost-backend-artifact-gate` or `just selfhost-native-source-gate` | `just selfhost-production-from-scratch` |
| frontend parity or oracle evidence | affected parity gate | `just selfhost-oracle` when the PR claims Go/Kizu oracle parity |
| gate timing budget work | targeted timing command | `just selfhost-oracle-budget` |

When a selfhost gate fails, classify it before rebuilding everything:

| Symptom | Likely boundary | First recovery step |
| --- | --- | --- |
| parse/check diagnostic changed for one fixture | parser, resolver, type, or ownership | run the matching parity gate and inspect the checked-in golden |
| `target/selfhost/stage2/selfhost` is missing | stale or absent bootstrap artifact | run `just selfhost-production-from-scratch` |
| stage1/stage2 fingerprint mismatch | IR, backend, runtime, or nondeterministic artifact metadata | inspect `target/selfhost/reports/bootstrap.txt` and rerun `just selfhost-bootstrap` from a clean `target/selfhost` |
| hosted artifact links but CLI exits 64 | executable lowering or supported block shape | run the affected run/test parity case and inspect emitted `.ll.meta` |
| artifact contains a forbidden fallback marker | production boundary regression | remove the fallback path; do not add dispatch to Go |
| runtime storage or host capability smoke fails | runtime ABI or host wrapper | run `just selfhost-backend-artifact-gate` before broader gates |

If artifact staleness is suspected, remove only `target/selfhost` and rebuild
with `just selfhost-production-from-scratch`. Do not add automatic fallback from
the hosted artifact to Go compiler phases to recover a local gate.

## Failure And Rollback Policy

- Any stage comparison mismatch blocks the production switch.
- Any oracle mismatch blocks the production switch.
- Any hidden fallback to Go blocks the production switch.
- Rollback is a revert of the explicit production-boundary or stage-runner
  change; do not add automatic fallback from the hosted artifact to Go compiler
  phases.
- A newly discovered blocker gets a new GitHub issue linked from #445 before the
  dependent roadmap issue is closed.

## Roadmap Issue Contract

Issues #447 through #461 depend on this bootstrap contract. If a later issue
needs to change a stage artifact, command, runtime boundary, or cache key rule,
it must update this document in the same PR.
