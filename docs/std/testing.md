# std::testing

test 用の assertion です。`test` 宣言そのものは言語の構文なので SPEC §14.5 に
あります。

```text
std::testing::expect(condition: bool) -> void
std::testing::expect_equal<T>(expected: T, actual: T) -> void
std::testing::fail(message: []u8) -> void
std::testing::failing_io() -> Io
```

`expect` は test assertion 用の void helper です。
condition failure は `std::internal::builtin::test_fail` 経由で runtime error として停止し、
test source は assertion ごとの `try` を書きません。
`fail` は渡された `[]u8` を diagnostic として `runtime error:` を出し、その場で
停止します。`expect` と同じ trap 境界なので、error として捕まえて続行することは
できません。到達しないはずの branch を落とすのに使います。
`expect_equal<T>` は明示 static 引数付きの generic assertion です。
failure は `expected ... got ...` 形式の diagnostic を出し、assertion ごとの `try` は不要です。
static 引数が type だけなので、caller は `expect_equal<i64>(1, actual)` のように
期待型を明示します。type argument inference と per-type `expect_equal_i64` family は
導入しません。

`failing_io` はすべての操作を拒否する `Io` です。プログラムの error 経路は何かが
失敗したときしか走らず、本物の `Io` に失敗を頼むことはできないので、失敗する
capability を渡す方を明示します。`examples/negative/fs_failing_io.kizu` と
`examples/negative/std_io_failing_write.kizu` がその形です。

`test` 宣言の構文と `kizu test` の挙動は SPEC §14.5 にあります。
