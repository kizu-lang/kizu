# ADR-0079: struct literal field initializers name each field once

## Status

Accepted.

## Context

`SPEC.md` section 6.4 declares `struct` and shows struct literals such as
`User { name: "alice", age: 30 }`, but it never states what a literal owes the
declaration it builds. It does not say whether a literal may leave a declared
field out, whether it may name a field the struct does not declare, and it does
not say what a literal that writes the same field twice means. The spec text is
silent on all three.

The implementations had already settled the first two the same way and
disagreed about the third.

- The Go reference type checker rejects a missing field
  (`type error: missing field ...`) and an undeclared field
  (`type error: unknown field ...`), but it collected the written fields into a
  map keyed by name, so `User { name: "a", age: 30, age: 31 }` type checked and
  the last initializer silently won.
- The selfhost backend maps written fields onto declared slots by name and
  refuses a literal that leaves a slot unfilled, names a slot that does not
  exist, or fills one slot twice, naming the function, the struct, the field and
  the literal node.

So the reference path compiled a program the selfhost path refused. One program
had two answers, which is the state neither implementation may be left in.

A duplicate is also not a shorthand for anything. Nothing in the language reads
a struct literal as a sequence of assignments: it is one value built from one
declaration, and `let`-bound fields are not reassignable afterwards. Two
initializers for one slot express an intent the language has no way to state,
and picking one of them silently is exactly the class of quiet wrong answer this
compiler refuses elsewhere.

## Decision

A struct literal names each field of the struct it builds exactly once.

- The literal's type must name a declared struct.
- Every declared field must be given exactly one initializer.
- A name the struct does not declare is an error.
- A name written more than once is an error. Last-wins is not the rule; the
  duplicate is a mistake, not a shorthand.
- Written order is free. Fields are matched to the declaration by name, and the
  declaration order is the only order the built value has.

The duplicate is reported where it is written, before the declared fields are
measured against the literal, so a literal that repeats a name reports that
repetition rather than a missing field it also causes.

The reference diagnostic follows ADR-0072:

```text
type error: duplicate field `User.age`
```

## Consequences

- `internal/types/checker.go` rejects the repeated name in
  `checkStructLiteralExpr` instead of overwriting the earlier initializer.
- The selfhost backend refusal in
  `selfhost/src/backend/compiled_mir_lower_struct.kizu` is correct as written
  and stays. It is now unreachable through a well-formed frontend, which is what
  a backend refusal of a shape the language forbids should be.
- `examples/negative/duplicate_struct_field.kizu` carries the case in the
  reusable conformance manifest (`negative_duplicate_struct_field`), so neither
  implementation can drift back to accepting it without a failing test.
- The selfhost type checker
  (`selfhost/src/types/struct_literal.kizu`) reports the same diagnostic, so
  `kizu check` gives one answer whichever checker runs it.
- Should a later ADR want field update syntax (`Type { ..base, field: value }`),
  it introduces a new form with its own rule. It does not reinterpret a repeated
  field name.
