# ADR-0064: comma list delimiters

## Status

Accepted.

## Context

Kizu previously mixed separators for list-like syntax:

- struct fields used `;`
- enum tags could rely on newlines
- union variants accepted `;`
- match arms accepted `;`

That made declaration lists look like statement sequences and forced the parser to keep
compatibility paths that do not help the self-host compiler. Kizu should keep `;` for
statement termination and use one delimiter family for lists.

## Decision

Use `,` for brace-delimited list entries:

- struct fields
- enum tags
- union variants
- struct literal fields
- match arms

Trailing commas are allowed. A semicolon may still appear inside a match arm body when
the arm body is a simple statement, for example `Tag => return value;,`. It is not the
match arm separator.

## Consequences

- Parser errors report `expected ','` when old list separators are used.
- Examples, conformance cases, and self-host source use comma-delimited list syntax.
- `;` remains statement syntax and is not reused as declaration-list punctuation.
