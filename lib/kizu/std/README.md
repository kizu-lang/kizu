# Kizu Standard Library Sources

This is the std package. `kizu.toml` names it and `src/` holds its modules, the
same shape a program written in Kizu has.

`src/internal/builtin.kizu` is the trusted primitive namespace. The primitives
themselves are still provided by the Go implementation; the module exists so
that std can name them and nothing else can. Public std modules wrap them.
Pure library behavior such as `std::sort::strings` remains ordinary Kizu source;
only its owner-safe Array slot exchange crosses the trusted primitive boundary.

Where the compiler reads this tree from is decided by `KIZU_LIB_DIR`, the
`--lib-dir` flag, or the `lib/kizu` directory beside the running binary. The
current directory is never consulted.
