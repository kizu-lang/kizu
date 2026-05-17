# Kizu Compiler Sources

This directory is the future Kizu implementation target for the compiler.

Files are intentionally named to mirror the current Go packages under
`internal/`. Keep this layout aligned so Go compiler code can be migrated to
Kizu one module at a time.

This is a migration layout, not an active compiler implementation yet. Keep
files parseable, but do not add behavior here without matching Go package
parity tests and an issue-scoped migration plan.
