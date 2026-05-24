# Selfhost CLI Parity Scope

Issue #497 tracks hosted selfhost CLI parity after the first bootstrap path.
This document records the release-scope decision after #530.

The hosted selfhost artifact is not a general-purpose replacement for Go
`cmd/kizu` yet. It must not add hidden Go parser, Go interpreter, Go test
runner, or Go build-cache fallback paths. Every additional command needs a
bounded child issue with command arguments, input/output paths, stdout/stderr,
exit codes, unsupported behavior, and hosted-artifact validation.

## Current Hosted Surface

The current hosted stage2 artifact supports these command slices:

| Slice | Scope | Validation |
| --- | --- | --- |
| `check selfhost` | Minimum compiler package check from #458 | `just selfhost-production-gate` |
| `stage selfhost` | Stage2 artifact materialization from #459 | `just selfhost-production-gate` |
| `check <print-hello source file>` | #530/#592 positive source-shape check slice | `just selfhost-check-parity-gate` |
| `check <minimal-main-return source file>` | #602 positive return-statement source-shape check slice | `just selfhost-check-parity-gate` |
| `check <std::testing::expect source file>` | #604 positive qualified-call source-shape check slices for expect true/false | `just selfhost-check-parity-gate` |
| `check <moved-value source file>` | #530/#592 negative source-shape check slice | `just selfhost-check-parity-gate` |
| `parse <minimal-main-return source file>` | #579 positive source-shape slice; manifest covers the original and alias fixtures | `just selfhost-parse-parity-gate` |
| `parse <print-call source file>` | #594 positive call-statement source-shape slice; manifest covers the original and alias fixtures | `just selfhost-parse-parity-gate` |
| `parse <std::testing::expect source file>` | #598 positive qualified-call source-shape slices for expect true/false | `just selfhost-parse-parity-gate` |
| `parse <moved-value declarations source file>` | #600 positive declaration, record-literal, field-access, and call source-shape slice | `just selfhost-parse-parity-gate` |
| `parse <missing-expression source file>` | #586 negative source-shape slice; manifest covers the original and alias fixtures | `just selfhost-parse-parity-gate` |
| `parse <missing-assign source file>` | #646 negative binding source-shape slice; manifest covers the original and alias fixtures | `just selfhost-parse-parity-gate` |
| `fmt <source file>` | #648/#650 stdout formatter slice routed through the hosted selfhost formatter path | `KIZU_RUN_SELFHOST_GATES=1 go test -timeout=20m ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -v` |
| `fmt --write <source file>` | #629 bounded formatter mutation slice using the same hosted formatter bytes and `fs_write_file` | `KIZU_RUN_SELFHOST_GATES=1 go test -timeout=20m ./cmd/kizu -run TestSelfhostBackendArtifactGate -count=1 -v` |
| `run <top-level main print-string source file>` | #588/#752 positive source-shape slice lowered to the bounded executable model before canonical artifact emission | `just selfhost-run-parity-gate` |
| `run <top-level main return-only source file>` | #752 positive no-output executable slice lowered to the same bounded executable model before canonical artifact emission | `just selfhost-run-parity-gate` |
| `run <missing-expression source file>` | #588 negative source-shape slice, no artifact execution | `just selfhost-run-parity-gate` |
| `run <control-flow print source file>` | #752 unsupported branch-control source-shape slice, no artifact execution | `just selfhost-run-parity-gate` |
| `test <top-level expect-ok source file>` | #590/#752 positive source-shape slice lowered to the bounded executable model before canonical artifact emission | `just selfhost-test-parity-gate` |
| `test <top-level expect-failure source file>` | #590/#752 assertion-failure source-shape slice lowered to the bounded executable model before canonical artifact emission | `just selfhost-test-parity-gate` |
| `test <control-flow expect source file>` | #752 unsupported branch-control source-shape slice, no artifact execution | `just selfhost-test-parity-gate` |

`selfhost/tests/cli/parse-parity.tsv` is the
#525/#579/#586/#594/#598/#600/#646 parse parity manifest. It records command
args, fixture paths, expected exit codes, and checked-in stdout/stderr golden
paths. The positive minimal-main-return, positive print-call, positive
`std::testing::expect(true|false)`, positive moved-value
declaration/record/field/call, negative missing-expression, and negative
missing-assign rows each include the original fixture plus an alias fixture
with the same source bytes, proving the hosted paths are no longer bound to one
fixed path. The gate runs through `target/selfhost/stage2/selfhost` and records
`go.cmd-kizu-fallback none`.

`selfhost/tests/cli/check-parity.tsv` is the #530/#602/#604/#646 check
parity manifest. It records command args, fixture paths, expected exit codes,
and checked-in stdout/stderr golden paths for the bounded `check <file>` slice.
For #592, the positive hello and negative moved-value rows each include the
original fixture plus an alias fixture with the same source bytes. For #602,
the positive minimal-main-return row reuses the parse fixture and alias to add
the first return-statement check source shape. For #604, the
`std::testing::expect(true|false)` rows reuse the test fixtures and aliases to
cover the first error-union return plus qualified-call check source shapes. For
#646, the missing binding assignment rows reuse the parse fixtures and aliases
to cover another parse-diagnostic check source shape.
These rows prove the hosted dispatch paths are no longer bound to one fixed
path. The fast
`just selfhost-check-parity-gate` recipe reuses an existing passing
`target/selfhost/stage2/selfhost` artifact and records `go.cmd-kizu-fallback
none`; it does not bootstrap from scratch by default.

`selfhost/tests/cli/run-parity.tsv` is the #569/#588/#752 run parity manifest. It
records command args, fixture paths, expected exit codes, checked-in
stdout/stderr golden paths, hosted artifact mode, and the canonical artifact
stem for the bounded `run <file>` slice. The positive print-hello and negative
missing-expression rows each include the original fixture plus an alias fixture
with the same source bytes, proving the hosted dispatch paths are no longer
bound to one fixed path. The #752 return-only row proves the executable model
can emit a successful no-output artifact instead of assuming every supported
`run` shape is a stdout write. The #752 branch-control row proves the hosted run
path does not search for `print` inside non-top-level control flow and then emit
a fake artifact. The hosted compiler emits fixture artifacts under
`target/selfhost/run/`; the gate links and executes those artifacts with the
explicit selfhost host runtime and records `go.cmd-kizu-fallback none`.
The separate `just selfhost-native-source-gate` builds the selfhost source
package as a native executable and verifies representative run artifacts are
produced through `selfhost::backend::executable` checked-AST lowering. That gate
is source-path evidence for #752; the hosted stage2 parity gate remains the
release surface until the generated matcher is removed.

`selfhost/tests/cli/test-parity.tsv` is the #570/#590/#752 single-file test parity
manifest. It records command args, fixture paths, expected exit codes,
checked-in stdout/stderr golden paths, hosted artifact mode, and the canonical
artifact stem for the bounded `test <file>` slice. The expect-ok and
expect-failure rows each include the original fixture plus an alias fixture with
the same source bytes, proving the hosted dispatch paths are no longer bound to
one fixed path. The #752 branch-control row proves the hosted test path does not
search for `std::testing::expect` inside non-top-level control flow and then
emit a fake artifact. The hosted compiler emits fixture artifacts under
`target/selfhost/test/`; the gate links and executes those artifacts with the
explicit selfhost host runtime, records `go.cmd-kizu-fallback none`, and does not
claim general test discovery.
The native source gate also covers expect-ok and expect-failure artifacts through
the Kizu-owned checked-AST lowering path and requires the metadata marker
`executable_lowering selfhost::backend::executable checked-ast`.

Unsupported commands, wrong arity, and arguments beginning with `-` remain
deterministic usage/unsupported paths with exit code `64`.
The hosted `fmt` slice accepts only the two-argument `fmt <file>` form and uses
its own formatter dispatch instead of the `parse` command's `fn main` guard.
In-place rewrite flags remain owned by the Go CLI until a separate issue
defines hosted mutation behavior.

## Hosted Run And Test Strategy

Issue #531 fixes the execution model for the first no-Go hosted `run <file>`
and `kizu test <file>` children. Hosted execution uses backend artifact
emit/link/execute. It must not add a selfhost interpreter, Go `cmd/kizu`
fallback, Go interpreter fallback, or hidden runtime dispatch.

The hosted compiler artifact remains responsible for parsing, checking, and
lowering the selected fixture. The runnable program or test is a separate
backend artifact linked with the explicit selfhost runtime. The parity gate may
own executing that emitted artifact for the first child issue; the hosted
compiler must still record `go.cmd-kizu-fallback none`. A future single-command
launcher may wrap the same sequence, but broad user-facing run/test parity is
not claimed until the bounded manifests below pass through hosted-artifact
gates.

The first `run <file>` child starts with this manifest shape:

```text
selfhost/tests/cli/run-parity.tsv
# Columns: name command fixture exit stdout_golden stderr_golden artifact_mode artifact_stem
run_hello run selfhost/tests/cli/run_hello.kizu 0 selfhost/tests/cli/golden/run_hello.stdout selfhost/tests/cli/golden/run_hello.stderr hosted-artifact run_hello
run_hello_alias run selfhost/tests/cli/run_hello_alias.kizu 0 selfhost/tests/cli/golden/run_hello.stdout selfhost/tests/cli/golden/run_hello.stderr hosted-artifact run_hello_alias
run_helper_before_main run selfhost/tests/cli/run_helper_before_main.kizu 0 selfhost/tests/cli/golden/run_hello.stdout selfhost/tests/cli/golden/run_hello.stderr hosted-artifact run_helper_before_main
run_print_custom run selfhost/tests/cli/run_print_custom.kizu 0 selfhost/tests/cli/golden/run_print_custom.stdout selfhost/tests/cli/golden/run_hello.stderr hosted-artifact run_print_custom
run_return run selfhost/tests/cli/run_return.kizu 0 selfhost/tests/cli/golden/run_hello.stderr selfhost/tests/cli/golden/run_hello.stderr hosted-artifact run_return
run_if_unsupported run selfhost/tests/cli/run_if_unsupported.kizu 64 selfhost/tests/cli/golden/run_hello.stderr selfhost/tests/cli/golden/usage.stderr hosted-artifact -
run_invalid_missing_expr run selfhost/tests/cli/run_invalid_missing_expr.kizu 1 selfhost/tests/cli/golden/run_invalid_missing_expr.stdout selfhost/tests/cli/golden/run_invalid_missing_expr.stderr hosted-artifact -
run_invalid_missing_expr_alias run selfhost/tests/cli/run_invalid_missing_expr_alias.kizu 1 selfhost/tests/cli/golden/run_invalid_missing_expr.stdout selfhost/tests/cli/golden/run_invalid_missing_expr.stderr hosted-artifact -
```

`run_hello.kizu` is the first positive fixture:

```kizu
fn main() {
    print("hello, kizu");
}
```

Its golden output is stdout `hello, kizu\n`, empty stderr, and exit code `0`.
`run_helper_before_main.kizu` proves the hosted compiler scans leading function
declarations before lowering `main`, matching the bounded AST lowering path for
that declaration-order case.
`run_print_custom.kizu` uses a different string literal so the hosted backend
must derive the emitted stdout payload from source rather than from a fixed
`hello, kizu` artifact template.
The negative fixture reuses the smallest parse failure shape,
`fn main() { let value = ; }`; its golden output is empty stdout, the hosted
parse diagnostic on stderr, and exit code `1`. The `run` child must not execute
an artifact when frontend checking fails.

The first `kizu test <file>` child layers on the same backend artifact path. It
does not add test discovery. It emits a test artifact whose entry runs `main`;
after `main` returns without unhandled error or test trap, the test runner
writes `test: ok\n`.

```text
selfhost/tests/cli/test-parity.tsv
# Columns: name command fixture exit stdout_golden stderr_golden artifact_mode artifact_stem
test_expect_ok test selfhost/tests/cli/test_expect_ok.kizu 0 selfhost/tests/cli/golden/test_expect_ok.stdout selfhost/tests/cli/golden/test_expect_ok.stderr hosted-artifact test_expect_ok
test_expect_ok_alias test selfhost/tests/cli/test_expect_ok_alias.kizu 0 selfhost/tests/cli/golden/test_expect_ok.stdout selfhost/tests/cli/golden/test_expect_ok.stderr hosted-artifact test_expect_ok_alias
test_helper_before_main test selfhost/tests/cli/test_helper_before_main.kizu 0 selfhost/tests/cli/golden/test_expect_ok.stdout selfhost/tests/cli/golden/test_expect_ok.stderr hosted-artifact test_helper_before_main
test_expect_failure test selfhost/tests/cli/test_expect_failure.kizu 1 selfhost/tests/cli/golden/test_expect_failure.stdout selfhost/tests/cli/golden/test_expect_failure.stderr hosted-artifact test_expect_failure
test_expect_failure_alias test selfhost/tests/cli/test_expect_failure_alias.kizu 1 selfhost/tests/cli/golden/test_expect_failure.stdout selfhost/tests/cli/golden/test_expect_failure.stderr hosted-artifact test_expect_failure_alias
test_if_unsupported test selfhost/tests/cli/test_if_unsupported.kizu 64 selfhost/tests/cli/golden/test_expect_ok.stderr selfhost/tests/cli/golden/usage.stderr hosted-artifact -
```

`test_expect_ok.kizu` is the first positive fixture:

```kizu
fn main() -> !void {
    std::testing::expect(true);
    return;
}
```

Its golden output is stdout `test: ok\n`, empty stderr, and exit code `0`.
`test_helper_before_main.kizu` covers the same leading-declaration scan for the
bounded test executable path.
`test_expect_failure.kizu` uses `std::testing::expect(false)` and must produce
empty stdout, a deterministic assertion diagnostic containing
`expected condition to be true`, and exit code `1`.

The first `run` and `test` children use the existing `selfhost-abi-v0` runtime
surface: explicit `std::io::blocking`, `std::io::write_stdout`,
`std::io::write_stderr`, `std::process::exit_code`, process exit, and trap
boundaries. They do not require new storage layout from #496. They also do not
require in-artifact child process spawn/wait; the first gate can execute the
emitted artifact outside the hosted compiler process. If a later single-process
hosted CLI needs spawn/wait or broader process management, that ABI extension
must be tracked by #495 before claiming public parity.

Each implementation child must write its artifacts under a bounded
`target/selfhost/run/` or `target/selfhost/test/` subdirectory, record artifact
paths and sizes in `target/selfhost/reports/`, and avoid persistent cache growth
outside the explicit build/cache design.

## Deferred Slices

These #497 slices are explicitly deferred from the current hosted artifact
release scope:

| Slice | Decision | Reason | Next child issue shape |
| --- | --- | --- | --- |
| broader `check <file>` frontend | Partially deferred | #592, #602, and #604 move the first positive, return-statement, qualified-call, and negative check shapes off single hardcoded paths, but broader parsing, type checking, move checking, and borrow checking are not claimed. | Add one check source shape at a time, keeping checked-in goldens and no Go fallback. |
| broader `parse <file>` diagnostics | Partially deferred | #579, #586, #594, #598, and #600 move the first positive, call-statement, qualified-call, declaration/record/field/call, and negative parse shapes off single hardcoded paths, but broader parsing and diagnostic recovery are not claimed. | Add one parse diagnostic/source shape at a time, keeping checked-in goldens and no Go fallback. |
| broader `fmt <file>` and `fmt --write` | Partially deferred | #648/#650 and #629 cover the first stdout and in-place write formatter slice over the existing hosted formatter path; broader source-preserving comments, import ordering, and formatter coverage are not claimed. | Extend formatter slices under #629 with explicit stdout/stderr, mutation, and comment-trivia contracts. |
| broader `run <file>` | Partially deferred | #588 moves the first positive and negative run shapes off single hardcoded paths, but broader frontend, lowering, and artifact naming are not claimed. | Extend `run-parity.tsv` one source shape at a time, using hosted-artifact validation and no Go fallback. |
| broader `kizu test <file>` | Partially deferred | #590 moves the first expect-ok and expect-failure shapes off single hardcoded paths, but broader frontend, lowering, artifact naming, and discovery are not claimed. | Extend `test-parity.tsv` one source shape at a time, using hosted-artifact validation and no discovery. |
| cache/status, cache/prune, why-rebuild, cache artifact commands | Deferred | Hosted artifact cache ownership, persistence, pruning, and no-op rebuild semantics are not designed. | Split into cache command issues with explicit cache directory, artifact paths, mutation rules, stdout/stderr, and cache-size acceptance checks. |
| non-critical diagnostic/display parity commands | Deferred | Diagnostic formatting must be command-specific to avoid broad parity without contracts. | Create one command/display contract per slice, including exact stdout/stderr and unsupported behavior. |

No general-purpose release artifact replacement is claimed while these slices
are deferred. Future work must either implement one bounded child issue at a
time or update this decision with a linked issue and documented release scope.
