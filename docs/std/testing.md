# std::testing

test 用の assertion です。`test` 宣言そのものは言語の構文なので SPEC §14.5 に
あります。

```text
std::testing::expect(condition: bool) -> void
std::testing::expect_equal<T>(expected: T, actual: T) -> void
std::testing::fail(message: []u8) -> void
std::testing::seed() -> i64
std::testing::failing_io() -> Io
std::testing::run_model<Cmd, M, S>(
    allocator: Allocator,
    rng: &var std::rand::Rng,
    steps: i64,
    gen: fn(&var std::rand::Rng, &M) -> Cmd,
    init_model: fn(Allocator) -> !M,
    init_sut: fn(Allocator) -> !S,
    step: fn(Allocator, &var M, &var S, &Cmd) -> !bool,
) -> !?std::array::Array<Cmd>
std::testing::check<T>(
    rng: &var std::rand::Rng,
    cases: i64,
    gen: fn(&var std::rand::Rng) -> T,
    holds: fn(&T) -> bool,
) -> ?T
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

## model-based testing

`run_model` は model(こうあるべき、と素朴に書いたもの)と実物(`S`)を同じ
command 列で 1 手ずつ進め、両者が食い違った列のうち最短のものを返します。
全手一致なら `null` です。`gen` は rng と model の状態から次の command を作り、
`init_model` / `init_sut` は run ごとに新しい値を作り、`step` は両者を 1 手進めて
一致していれば `true` を返します。食い違いを見つけた列は replay で縮めます ——
失敗する最短 prefix を二分探索し、次に落としても失敗が保てる command を 1 つずつ
落とします。replay のたびに `init_*` で作り直すので、storage を持つ model / 実物は
replay ごとに作られ解放されます(`Box` 越しに 1 つの cleanup として)。

`Cmd` は列に copy されるので copy 型に限ります(owner は `unsupported`)。`M` / `S` は
owner でもかまいません。`gen` などは関数 pointer です —— closure が無いので
top-level fn を渡します。乱数は呼び出し側が `rng` を持つので、`rand::new(testing::seed())`
から作れば失敗時の `note: seed N` で同じ run を再生できます。返った列は呼び出し側が
`print` して `fail` します。

```kizu
if try testing::run_model<Cmd, Model, Sut>(
    allocator, &var rng, 200, gen, init_model, init_sut, step) |trace| {
    var i = 0;
    while trace.get(i) |cmd| {
        print(cmd);
        i = i + 1;
    }
    trace.deinit(allocator);
    testing::fail("model and system diverged");
}
```

`check` は列でなく値の性質のためのものです。`gen` で `cases` 個の値を引き、`holds`
が最初に `false` を返した値を返します(全部通れば `null`)。値の shrink は
しません。`tests/behavior/src/std_testing_model/` がどちらの形も示します。
`examples/model_calendar.kizu` は日数 counter を model に `std::date` の暦を検査し、
手で数えた曜日の bug を 1 手の列に縮めます。
