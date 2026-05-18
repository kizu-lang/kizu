# Kizu Standard Library Sources

This directory is the migration target for Kizu-written `std` modules.

The current implementation still uses Go-backed trusted primitives. Public std
APIs should move here as Kizu wrappers while host and runtime boundaries remain
in `internal/stdprim`.

This is a migration layout, not an active package-loader input yet. Keep files
parseable, but do not assume `kizu check std` works until package loading is
reintroduced with explicit acceptance tests.
