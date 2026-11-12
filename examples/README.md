# Kizu Examples

Programs worth reading, and the output they produce.

An example is a program a person can read start to finish and learn one thing
from. A language assertion that is not such a program belongs in
`tests/behavior/` instead, where the whole suite links and runs once.

Every example ends with the case it declares -- the command to run it with, the
feature tags it covers, and what that has to produce. The test harness
(`compiler/tests/harness`) walks this directory and reads those blocks, so no
list here has to be kept in step with the directory. The grammar of a case is
documented at the top of
`compiler/tests/harness/src/conformance/conformance.kizu`.

```
examples/*.kizu             one program each: a feature and its output
examples/negative/*.kizu    one refusal each: the rule, and the diagnostic it produces
examples/modules/           examples that need a package root rather than one file
examples/fixtures/          fixed inputs the examples above read
```

`negative/` is where the safety rules are readable as programs: each file does
the thing the language refuses and declares the diagnostic substring that
refusal has to contain. Those are the ones to read when the question is *what
does Kizu not let me do*.

`modules/` needs a package root, so those run as `kizu check <package-root>`
and declare their case in `src/main.kizu`.

Run them the way the project gate does:

```sh
just verify
```

To run one with the compiler `just stage1` builds:

```sh
just run examples/hello.kizu
just kizu-check examples/negative/moved_value.kizu
just kizu-check examples/modules/compiler_phases
```

Which backends accept which examples, grouped by feature tag, is summarized in
the top-level [README](../README.md).
The memory-safety invariants and the examples that stand for each are in
[docs/memory-safety.md](../docs/memory-safety.md).
