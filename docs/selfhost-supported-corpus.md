# Selfhost Supported Corpus

The production-switch corpus for #460 is manifest-driven:

```text
selfhost/tests/supported-corpus.tsv
```

Only active rows in that manifest are production-switch gates. The current
supported subset is intentionally small:

- `check selfhost`
- `check examples/hello.kizu`
- `check examples/negative/moved_value.kizu`

The gate runs these entries through the hosted selfhost compiler artifact built
by `just selfhost-bootstrap`, then compares user-visible stdout, stderr, and
exit code. It does not compare internal oracle counts.

Run the corpus against an existing passing bootstrap artifact with:

```sh
just selfhost-corpus-gate
```

That command requires `target/selfhost/stage2/selfhost` and
`target/selfhost/reports/bootstrap.txt` from a passing `just selfhost-bootstrap`
run. It does not rebuild bootstrap artifacts itself, so corpus validation stays
sub-second during local iteration. From a clean workspace, run:

```sh
just selfhost-corpus-gate-from-scratch
```

Cases outside the manifest are excluded by selector. Broad command parity,
general example/conformance coverage, and unsupported ABI shapes remain blocked
by #497 and #495. Adding a corpus entry requires updating the manifest and the
hosted artifact behavior in the same change.

The #525 `parse <file>` parity cases are intentionally tracked in a separate
CLI manifest:

```text
selfhost/tests/cli/parse-parity.tsv
```

Run them against an existing passing bootstrap artifact with:

```sh
just selfhost-parse-parity-gate
```

That gate records command args, fixture paths, checked-in stdout/stderr golden
paths, expected exit codes, and hosted-artifact output fingerprints in
`target/selfhost/reports/parse-parity.txt`.
For #579, `parse_ok_minimal_alias.kizu` uses the same positive source text as
`parse_ok_minimal.kizu` to prove the hosted positive parse path is source-driven
instead of tied to one fixture path. For #594, `parse_print_hello_alias.kizu`
does the same for the first print-call statement parse shape. For #598,
`test_expect_ok_alias.kizu` and `test_expect_failure_alias.kizu` do the same for
the first `std::testing::expect` qualified-call parse shapes. For #600,
`check_moved_value_alias.kizu` reuses the `examples/negative/moved_value.kizu`
source bytes for the first declaration, record-literal, field-access, and direct
call parse shape. For #586, `parse_invalid_missing_expr_alias.kizu` does the
same for the first negative missing-expression diagnostic path.

The #530 `check <file>` parity cases are also tracked in a separate CLI
manifest so repeated local validation stays focused:

```text
selfhost/tests/cli/check-parity.tsv
```

Run them against an existing passing bootstrap artifact with:

```sh
just selfhost-check-parity-gate
```

That gate records command args, fixture paths, checked-in stdout/stderr golden
paths, expected exit codes, and hosted-artifact output fingerprints in
`target/selfhost/reports/check-parity.txt`.
For #592, `check_hello_alias.kizu` and `check_moved_value_alias.kizu` use the
same source bytes as the original check fixtures to prove hosted check dispatch
is source-driven for the first success and move-check failure shapes.

Issue #531 defines the first hosted `run <file>` and `kizu test <file>`
strategy, but those cases are not part of the production-switch corpus yet.
Their implementation children must use separate `run-parity.tsv` and
`test-parity.tsv` manifests, emit/link/execute backend artifacts, and record
`go.cmd-kizu-fallback none` before any row is promoted into this supported
corpus.
For #588, `run_hello_alias.kizu` and `run_invalid_missing_expr_alias.kizu` use
the same source bytes as the original run fixtures to prove hosted run dispatch
is source-driven for the first success and frontend-failure shapes.
For #590, `test_expect_ok_alias.kizu` and `test_expect_failure_alias.kizu` do the
same for the first single-file test artifact shapes.
