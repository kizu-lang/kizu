# std::testing

test 用の assertion です。`test` 宣言そのものは言語の構文なので SPEC §14.5 に
あります。

```text
std::testing::expect(condition: bool) -> void
std::testing::expect_equal<T>(expected: T, actual: T) -> void
std::testing::fail(message: []u8) -> !void
```

`expect` は test assertion 用の void helper です。
condition failure は `std::internal::builtin::test_fail` 経由で runtime error として停止し、
test source は assertion ごとの `try` を書きません。
`fail` は caller-provided `[]u8` を通常の `!void` error として返します。
unreachable branch など、呼び出し側の error-union 経路へ明示的に戻したい場合に使います。
`expect_equal<T>` は明示 static 引数付きの generic assertion です。
failure は `expected ... got ...` 形式の diagnostic を出し、assertion ごとの `try` は不要です。
static 引数が type だけなので、caller は `expect_equal<i64>(1, actual)` のように
期待型を明示します。type argument inference と per-type `expect_equal_i64` family は
導入しません。

`test` 宣言の構文と `kizu test` の挙動は SPEC §14.5 にあります。
