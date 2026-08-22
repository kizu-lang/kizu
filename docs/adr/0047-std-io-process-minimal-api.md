# ADR 0047: std::io / std::process minimal API

Status: 採用

## Context

Command-line programs need stdout, stderr, argv, environment lookup, and
exit-code handling. Kizu should keep I/O explicit and avoid hidden process
globals where an `Io` capability is the correct boundary. Most command output
is line-oriented, but callers should not need a formatting abstraction merely
to append a newline.

## Decision

Stdio helpers require explicit `Io`.

```text
std::io::write_stdout(io: Io, bytes: &[]u8) -> !void
std::io::write_stdout_line(io: Io, bytes: &[]u8) -> !void
std::io::write_stderr(io: Io, bytes: &[]u8) -> !void
std::io::write_stderr_line(io: Io, bytes: &[]u8) -> !void
```

The line helpers compose byte writes with a newline. They do not introduce
format strings, implicit allocation, or a generic `Writer`. Values are formatted
explicitly into caller-owned bytes through `std::fmt`; `std::io` remains the
separate capability-bearing output boundary. The concrete API belongs to
`docs/std/io.md`.

Process helpers expose CLI state without filesystem or stdio side effects.

```text
std::process::arg_count() -> i64
std::process::arg(index: i64) -> ![]u8
std::process::env(name: []u8) -> ![]u8
std::process::exit_code(code: i64) -> i64
```

`kizu run <file> -- args...` and `kizu test <file> -- args...` pass explicit
program arguments into the interpreter.

`std::process::exit_code` returns the code value in v0.2. It does not terminate
the interpreter yet. Final process exit validation belongs to the runner that
turns this value into an actual host exit status.

## Consequences

- CLI examples can distinguish stdout and stderr while preserving explicit I/O.
- Common line output does not fall back to the diagnostic-only `print` builtin.
- Actual process termination remains a later runner concern.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Route ordinary output through builtin `print` | It is a diagnostic primitive, not an explicit fallible I/O API. |
| Make formatting generic over a `Writer` | The current sink is caller-owned `String`, and `std::io` already consumes bytes. Combining representation with capability-bearing I/O would add a second abstraction without removing a copy or runtime boundary. |
| Add format strings to line output | `std::fmt` already supports explicit caller-owned diagnostic construction; line output only needs bytes plus a newline. |
