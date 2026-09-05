# std::testing

test 用の assertion です。`test` 宣言そのものは言語の構文なので SPEC §14.5 に
あります。

```text
std::testing::expect(condition: bool) -> void
std::testing::expect_equal<T>(expected: T, actual: T) -> void
std::testing::fail(message: []u8) -> void
std::testing::seed() -> i64
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
期待型を明示します。`expect_equal<f64>` の失敗は両値を `std::float::append` の
最短往復表現で綴ります(`docs/std/float.md`)。type argument inference と per-type `expect_equal_i64` family は
導入しません。

`seed` は乱択 test が列を引く seed です。`kizu test --seed N` で与えた値、
無ければ runtime がその run のために選んだ値を返し、一度呼ばれた後の失敗は
すべて `note: seed N (rerun with \`kizu test --seed N\`)` を出すので、失敗した
run を同じ flag で再生できます。`std::rand::new(testing::seed())` の形で使います
(`docs/std/rand.md`)。seed の受け渡しは runtime が持つので、`wasm32-*` target は
`std::testing::seed` を拒否します。

`failing_io` はすべての操作を拒否する `Io` です。プログラムの error 経路は何かが
失敗したときしか走らず、本物の `Io` に失敗を頼むことはできないので、失敗する
capability を渡す方を明示します。`examples/negative/fs_failing_io.kizu` と
`examples/negative/std_io_failing_write.kizu` がその形です。

`test` 宣言の構文と `kizu test` の挙動は SPEC §14.5 にあります。
