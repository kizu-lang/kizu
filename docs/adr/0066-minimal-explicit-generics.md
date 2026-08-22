# ADR-0066: minimal explicit static arguments for function generics

## Status

Accepted.

## Context

The v0.2 stdlib and self-host compiler source already need APIs such as
`std::array::Array<T>`, `std::map::Map<K, V>`, and typed test assertions.
Keeping those as Go-only special cases makes the Go implementation thicker and
prevents self-host source from representing ordinary reusable helpers.

Kizu also needs a stable answer for how type values relate to `comptime`.
Adopting Zig-style `comptime T: type` as the primary public syntax would mix
compile-time type arguments into the runtime argument list. That makes
ownership and borrow boundaries less obvious, because `(...)` would contain
both values that can move or borrow and values that cannot exist at runtime.

Full generics are still too broad for v0.2. Kizu does not need general inference,
bounds, generic structs beyond existing std container spellings, associated
types, higher-kinded types, or generic runtime reflection.

Self-host compiler diagnostics do need a function to accept an ordered,
heterogeneous tail. A format-only builtin would make an ordinary library
operation privileged, while a runtime tagged argument array would add boxing,
dispatch, and an allocation-shaped API to code whose types are already known.

## Decision

Kizu adopts a minimal explicit static argument subset for function generics.

- The `<...>` list on declarations and calls is a compile-time/static argument
  list, separate from the runtime argument list `(...)`.
- A static argument list holds type parameters and compile-time values. A bare
  name declares a type, `name: Type` declares a value: `fn f<T>(value: T) -> T`,
  `fn sized<n: i64>()`, `fn each<worker: Function>(start: i64, end: i64)`.
- A compile-time value cannot be a runtime parameter. `comptime n: i64` in
  `(...)` is rejected: the reason this ADR gives for keeping types out of the
  runtime list applies to any value that cannot exist at runtime.
- Calls must pass explicit fixed static arguments: `f<i64>(1)`.
- Namespace-qualified calls use the same syntax:
  `std::testing::expect_equal<i64>(expected, actual)`.
- Generic function bodies are checked when a call instantiates them with a
  concrete static argument set. Top-level generic bodies are not checked as
  uninstantiated runtime code.
- Type parameters are compile-time type values inside the instantiated body.
- `type<T>` is a compile-time type literal. `comptime if T == type<i64>` checks
  only the selected branch after instantiation.
- Bare type names are not expression-level type values in v0.2. Kizu keeps
  `type<T>` as the canonical spelling so value names and type names do not
  share an implicit expression namespace, and so compound types use the same
  form as primitive types.
- `type` values are comptime-only and cannot be stored in runtime locals,
  fields, union payloads, collection elements, or return values.
- The Zig-style spelling `fn f(comptime T: type, value: T)` is not the
  canonical Kizu generic syntax. Kizu keeps type/static arguments in `<...>` so
  runtime arguments remain ordinary move/borrow checked values.

Kizu also adopts one deliberately narrow runtime-argument capture. A
body-bearing free function may replace its last parameter declaration with
`name: ...`.

```kizu
fn append(out: &var String, parts: ...) -> !void {
    comptime for parts |T, part| {
        comptime if T == type<[]u8> {
            try out.append_bytes(part);
        } else {
            try part.append_display(out);
        }
    }
    return;
}
```

- `...` is not a type and introduces no user-visible `Parts` value. It says
  that the remaining runtime arguments become separate fixed parameters of
  this instance.
- Each argument brings the concrete type and passing mode it already has with
  no expected type. This is exact capture, not general inference or constraint
  solving. A value, `&value`, `&var value`, and a borrow-returning expression
  remain respectively a value, shared borrow, mutable borrow, and returned
  borrow.
- Every type valid for an ordinary fixed runtime parameter is valid here under
  the same ownership and borrow rules. There is no borrow-type exception and
  no owner exception for fallible functions. A by-value owner still requires
  `move`; its expansion must consume it, and a fallible body writes the same
  `errdefer` required by any fixed owner parameter (ADR-0117).
- `comptime for parts |T, part|` is the only operation that opens the capture.
  `T` is a compile-time type value. `part` is an ordinary runtime binding with
  that concrete type and passing mode.
- The capture is not a tuple, array, or first-class value. It cannot be stored,
  returned, indexed, measured at runtime, reflected, or forwarded with a
  spread operation.
- One trailing capture is allowed on a body-bearing free function. Methods,
  contract requirements, and extern declarations do not accept one. The tail
  has at most 64 arguments.
- An instance is cached by function, explicit fixed static arguments, and the
  ordered `(passing mode, concrete type)` vector. Literal contents are not part
  of the key.
- Lowering gives the instance ordinary fixed parameters and emits the body once
  per captured argument. There is no runtime capture object, tag, type switch,
  or loop. Normal function-call and operation costs remain; the feature does
  not require forced inlining.

This subset deliberately excludes:

- implicit type argument inference, except exact trailing runtime capture
- compile-time value arguments beyond integers, bools, `Function`, and `Field`;
  a string static argument can join the same list when a concrete need appears
- generic methods or impl-level generic parameters
- generic structs outside the existing std container type spellings
- bounds, associated types, higher-kinded types, specialization, and reflection
- macro expansion or AST/token rewriting
- capture-as-a-whole spread/forwarding, constraints, and first-class capture values

## Consequences

- Public std wrappers and `std::testing::expect_equal<T>` can live in Kizu
  source while Go remains the explicit trap/storage/runtime boundary.
- Self-host compiler code can use reusable typed helpers without inventing
  per-type assertion families.
- Each distinct passing-mode/type vector can produce a distinct body, trading compile
  time and code size for direct fixed-type runtime code.
- Future widening should extend the explicit static argument list before adding
  implicit inference or a second generic syntax.

Rejected alternatives are:

| Alternative | Reason |
| --- | --- |
| A format-only compiler builtin or `f"..."` syntax | It privileges one library operation and still leaves other heterogeneous tails unsolved. |
| `fn append<Parts...>(parts: Parts...)` | It exposes a type-level pack that users cannot otherwise construct, pass, or operate on. The declaration promises more type machinery than Kizu has. |
| An erased `FormatArgs` array | It needs a runtime representation, tags, dispatch, and usually temporary storage even though every source argument is statically known. |
| Zig's anonymous tuple / `anytype` surface wholesale | It imports tuple reflection and a broader comptime type system than this use requires. |
| Go's `...any` surface wholesale | It erases types and moves formatting decisions to runtime reflection/type switches. |
| Excluding borrows or fallible owners | It makes a general parameter mechanism depend on implementation convenience. Internal fixed parameter names make normal alias and error-path checks possible. |
