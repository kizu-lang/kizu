# ADR-0077: error union implicit widening

## Status

Superseded by ADR-0086.

`cast<ErrorType!T>` and the `[]u8` failure payload this ADR is written against
were both removed when errors became names. An inferred `!T` now absorbs any
error set directly, so the widening this ADR proposed has no cast to attach to.

## Context

ADR-0030 chose Zig-style error unions for Kizu. The current specification and
Go checker support two source-level forms:

- `!T`, an anonymous error union whose failure payload is a copied `[]u8`
  message produced by `error(message)`
- `ErrorType!T`, a typed error union whose failure payload is a declared union
  value

The current conversion rule is intentionally explicit. `try` can propagate a
typed error union only when the source and destination use the same
`ErrorType`. `cast<ErrorType!T>(expr)` can adapt `expr: !T` to a typed error
union only when `ErrorType` has a `Message([]u8)` variant. `error(message)`
itself is rejected inside a typed-error-returning function.

The Go checker implements that shape in a few concentrated places:

- `checkTryExpr` rejects propagation when `sourceError != targetError`.
- `checkErrorUnionCast` accepts only explicit `!T` to `ErrorType!T` casts with
  the same success payload type and a `Message([]u8)` destination variant.
- `checkErrorCall` only constructs anonymous `!T`, not typed errors.
- regular return and call argument checking still rely on exact type equality
  after existing contextual integer literal coercion.

The selfhost source now shows this boilerplate in real compiler paths. A scan
of `selfhost/src/**/*.kizu` found six `cast<...!T>(...)` targets whose target
type is an error union:

```text
selfhost/src/parser.kizu:68: ParseError!std::array::Array<std::kizu::lexer::Token>
selfhost/src/parser.kizu:72: ParseError!validation::ParseValidation
selfhost/src/parser.kizu:79: ParseError!std::kizu::ast::ParseResult
selfhost/src/parser.kizu:141: ParseError!ast::AstSummary
selfhost/src/parser.kizu:184: ParseError!std::kizu::lexer::Token
selfhost/src/parser_oracle.kizu:18: parser::ParseError!void
```

The dominant pattern is a function returning `ParseError!T` that calls a helper
returning anonymous `!T` and has to spell `try cast<ParseError!T>(...)` before
`try` can propagate failure. The casts are not low-level representation changes;
they express a checked widening of the possible failure payload.

Zig is a useful comparison point but not a direct template. The
[Zig language reference](https://ziglang.org/documentation/master/#Error-Set-Type)
allows an error set value from a subset to coerce to a superset and rejects the
reverse direction; `anyerror` is the global error set and explicit casts back to
a smaller set are checked. Kizu does not have Zig error sets or inferred error
sets in v0. Its typed errors are ordinary tagged unions, so any implicit
conversion must be defined over Kizu union variants and payload types.

## Decision

Kizu should adopt implicit error union widening as a contextual type conversion.
The conversion is allowed only when the destination error union type is already
known from the enclosing expression. It must not create a new inference path or
guess among multiple possible destination error unions.

An error union `SourceError!T` widens to `TargetError!T` when all of these hold:

- the success payload type `T` is identical after existing type normalization
- both `SourceError` and `TargetError` are declared union types
- every variant in `SourceError` exists in `TargetError`
- each shared variant has the same payload type

An anonymous error union `!T` widens to `TargetError!T` when all of these hold:

- the success payload type `T` is identical after existing type normalization
- `TargetError` is a declared union type
- `TargetError` has a `Message([]u8)` variant

For anonymous-to-typed widening, a failure payload is wrapped exactly as the
current explicit typed error cast wraps it: `error(message)` becomes
`TargetError::Message(message)` on the propagated failure path, and the success
payload is unchanged.

The following contexts may use the widening:

- `try expr` propagation into the current function's declared error union return
- `return expr` when the function's declared return type is an error union
- call arguments when the parameter type is an error union
- assignment or initialization only when the destination type is explicit and
  already known

The following cases remain invalid without an explicit cast or a future ADR:

- narrowing from a superset to a subset
- any conversion where the success payload types differ
- variant renaming or variant payload conversion
- typed error union to anonymous `!T`
- anonymous `!T` to a typed error union without `Message([]u8)`
- using `error(message)` directly as typed error construction
- `error.Invalid`-style error literals or other error literal sugar

Existing explicit casts stay legal. The accepted explicit cast remains useful
when a reader wants to mark the adaptation point, when the destination type is
not otherwise known, or when code is compiled by an older bootstrap stage.

## Non-goals

This ADR does not add `error.Invalid`-style syntax, error-set declarations,
inferred error sets, or a typed-error literal form. Those are separate language
design questions.

This ADR also does not make low-level casts more permissive. Pointer casts,
integer casts, ABI layout conversions, and ownership-affecting moves remain
under their existing rules.

## Implementation Order

The feature should be implemented in an order that does not block bootstrap:

1. Add a Go checker predicate for contextual error union widening and use it in
   `try`, return, call argument, and explicit destination assignment checks.
2. Preserve an explicit lowering artifact for the conversion, either by
   inserting the same IR shape as the existing explicit cast or by teaching the
   relevant lowering path to emit the current error-union cast operation.
3. Update backend tests to prove that the existing runtime representation and
   failure-payload wrapping are reused.
4. Implement the same predicate in the selfhost checker before selfhost source
   starts depending on the new syntax.
5. Only after both checker paths accept the rule, remove redundant selfhost
   `try cast<...>(...)` sites in a separate cleanup.
6. If maintainers accept the proposal, update `SPEC.md` with the final rule.

The named-union subset rule can land after the anonymous `!T` to `Named!T`
case if implementation risk needs to be staged. The parser boilerplate is
already addressed by the anonymous-to-typed case, while the subset rule is the
more general design match for Zig's subset-to-superset coercion.

## Consequences

Error propagation becomes less noisy in selfhost compiler code. For example,
functions returning `ParseError!T` can call anonymous `!T` helpers through
plain `try helper(...)` once `ParseError` declares `Message([]u8)`.

The change is backward compatible at the source level because existing explicit
casts remain accepted.

The change should not require a new ABI. It reuses the existing error union
runtime layout and the existing explicit typed error cast behavior. The
implementation still needs checker and lowering work so the implicit conversion
is represented explicitly before backend emission.

The structural subset rule weakens purely nominal separation between error
union declarations. That is intentional only for widening and only when every
source failure can be represented by the destination without payload conversion.
Narrowing remains explicit so lossy or assertion-bearing conversions are visible
in source.

Bootstrap timing is review-critical. Removing explicit casts from selfhost code
before both the Go checker and selfhost checker understand the rule would create
a staged compiler dependency. The safe rollout is checker support first,
selfhost support second, source cleanup last.
