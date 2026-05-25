# ADR-0071: unsafe capability blocks

Status: 採用

## 背景

Kizu には raw pointer、C ABI call、volatile operation のように、コンパイラが
memory safety を証明しない低レベル操作が必要です。従来の `unsafe { ... }` は
境界を明示できますが、block 内でどの種類の unsafe operation を許しているかを
コード上で区別できません。

レビュー可能性を高めるには、`@unsafe` boundary を「全部許可」ではなく
capability ごとに宣言できる必要があります。

## 決定

`unsafe { ... }` を採用せず、`@unsafe(capability, ...) { ... }` を `@unsafe`
boundary として採用します。

```kizu
@unsafe(ptr_int_cast, volatile) {
    let p = ptr_from_int<ptr<u32>>(addr);
    volatile_write(p, value);
}
```

`@unsafe` は compiler directive statement です。user-defined function、
attribute、通常の builtin call ではありません。`@` namespace の最小 subset として
予約し、一般的な builtin / attribute system は #610 で別途扱います。

Capability は compiler-reserved identifier です。未知の capability は compile error
にします。Capability list は 1 個以上の identifier を comma-separated で書きます。

初期 capability set:

- `ptr_read`: `ptr_read(p)`
- `ptr_write`: `ptr_write(p, value)`
- `ptr_deref`: `p.*`, `p.* = value`, `p.*.field`
- `ptr_cast`: raw pointer 間の明示 cast
- `ptr_int_cast`: `ptr_from_int<ptr<...>>(value)` / `int_from_ptr<usize>(value)`
- `extern_call`: `extern "c" fn` call
- `unsafe_call`: `@requires_unsafe() fn` call
- `volatile`: volatile read/write primitive

`atomic` と `unchecked_index` は初期 capability set には含めません。
`volatile` は atomic ではなく、thread synchronization を表しません。

Nested `@unsafe` は lexical に capability を追加します。Capability の revoke は
提供しません。実装や利用者は nesting を積極的な feature として使う必要はありません。

`@unsafe` 内でも type check、move check、borrow check は無効化しません。

## Caller Obligation

`unsafe fn` は採用しません。呼び出し側に memory safety obligation を要求する
関数は `@requires_unsafe() fn` で宣言します。

関数本体で unsafe operation を使う場合は、通常の `fn` の中に局所的な
`@unsafe(...) { ... }` を書きます。`@requires_unsafe() fn` の呼び出しは
`@unsafe(unsafe_call)` 内でのみ許可します。

## 置き換える判断

この ADR は ADR-0007 の `unsafe { ... }` / `unsafe fn` surface を置き換えます。
ADR-0007 の「unsafe は compiler check を全面的に無効化しない」という判断は維持します。

## 影響

- `@unsafe` boundary が、許可する操作の種類までコード上で見える
- safe wrapper は内部で必要最小限の capability だけを宣言できる
- checker は broad `unsafe bool` ではなく capability context を持つ
- formatter、parser、selfhost frontend も `@unsafe` surface に揃える
- LSP は `@unsafe(...)` 内で capability completion を提供できる
