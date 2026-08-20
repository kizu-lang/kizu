# ADR-0074: control expressions and statement semicolons

## Status

Accepted.

Supersedes the earlier decisions on `if` expression, which this one restates in
full.

## Context

ADR-0036 made simple statement semicolons required. `if` expression was then
removed, to avoid making semicolon presence affect expression values.

Selfhost compiler work still needs ordinary value selection in expression
position. Requiring callers to introduce a mutable temporary for every
conditional value makes compiler code noisier without improving ownership
clarity. The problematic rule is not `if` or `match` as expressions; it is
treating semicolon-terminated statements inside branch blocks as branch values.

## Decision

Kizu supports `if` and `match` in expression position.

Expression-position `if` requires `else`. The final branch item is an expression
value and does not use `;`.

```kizu
let label = if adult {
    "adult"
} else {
    "minor"
};
```

Expression-position `match` requires every arm value to have the same type.
Arm values are expressions and do not use `;`. Match arms are still separated by
`,` according to ADR-0064.

```kizu
let label = match color {
    Red => "red",
    Green => "green",
    Blue => "blue",
};
```

The enclosing simple statement still requires `;`.

```kizu
let value = if cond { 1 } else { 2 };
value = match tag { A => 1, B => 2, };
print(value);
```

Statement-position `if` / `match` remain block statements and do not take an
extra trailing `;`.

Function return remains explicit. Function bodies do not return their final
expression implicitly.

## Consequences

- ADR-0036 remains the statement termination rule.
- Semicolon presence never changes a branch expression value.
- Parser, type checker, ownership checker, interpreter, IR, and selfhost
  compiler must support branch value type checking for expression-position
  `if` / `match`.
- Branch-local moves are merged into the outer ownership state for expression
  `if` / `match`.
- Stale examples that relied on optional statement semicolons must be updated.
