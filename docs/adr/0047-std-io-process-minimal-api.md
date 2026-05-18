# ADR 0047: std::io / std::process minimal API

Status: 採用

## Context

The self-host compiler CLI needs stdout, stderr, argv, environment lookup, and
exit-code handling. Kizu should keep I/O explicit and avoid hidden process
globals where an `Io` capability is the correct boundary.

## Decision

Stdio helpers require explicit `Io`.

```text
std::io::write_stdout(io: Io, bytes: []const u8) -> !void
std::io::write_stderr(io: Io, bytes: []const u8) -> !void
std::io::read_stdin(io: Io) -> ![]const u8
```

Process helpers expose CLI state without filesystem or stdio side effects.

```text
std::process::arg_count() -> i64
std::process::arg(index: i64) -> ![]const u8
std::process::env(name: []const u8) -> ![]const u8
std::process::exit_code(code: i64) -> i64
```

`kizu run <file> -- args...` and `kizu test <file> -- args...` pass explicit
program arguments into the interpreter.

`std::process::exit_code` returns the code value in v0.2. It does not terminate
the interpreter yet. Final process exit validation belongs to the runner that
turns this value into an actual host exit status.

## Consequences

- CLI examples can distinguish stdout and stderr while preserving explicit I/O.
- Self-host compiler argument parsing can be prototyped in Kizu source.
- Actual process termination remains a later runner concern.
