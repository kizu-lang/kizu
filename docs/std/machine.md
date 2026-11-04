# std::machine

有限状態機械を、それを動かす遷移関数から読み取る module です。機械の定義は
プログラムの遷移関数 1 つだけで、表も検査も JSON もその関数を呼んで作るので、
実装とずれません。

```text
std::machine::table<S, E>(
    allocator: Allocator,
    step: fn(S, E) -> ?S,
    start: S,
    is_end: fn(S) -> bool,
) -> std::mem::Error!Table

table.problems(allocator: Allocator) -> std::mem::Error!Array<String>
table.to_json(allocator: Allocator, out: &var String) -> std::mem::Error!void
table.deinit(allocator: Allocator) -> void
```

`table` は `S` と `E` のすべての組で `step` を呼びます。組は enum の宣言から作るので、
後から足した variant も歩きます。`null` は拒否で、表に載りません。payload を持つ型は
compile error です。組は実行時に数えられる有限個なので、`problems` の答えはこの関数の
全数の検査です。

`Table` の field は名前で読めます。`moves` は `Move { from, event, to }` の配列で、
`states`、`events`、`ends` とともに宣言順です。「`Connected` へはこの事象でだけ入る」の
ような機械ごとの性質は、`moves` を test で歩いて確かめます。

`problems` は、問題 1 つにつき 1 行を返します。

| 行 | 意味 |
| --- | --- |
| `unreached <状態>` | `start` からどの遷移でも入れない |
| `stuck <状態>` | 入れるが、そこから `is_end` の状態へ行けない |
| `escape <終わり> <事象> <状態>` | 終わりの状態から別の状態へ移る |

`is_end` が常に偽の機械は、止まらない機械として `unreached` だけを確かめます。

`to_json` は `{ states, events, start, ends, moves }` を 1 項目 1 行で書きます。file に
残して commit すれば、遷移の変更が差分の行に出ます。Lean や Quint の仕様は、この JSON を
外の道具で変換して作ります。

```kizu
fn step(job: Job, signal: Signal) -> ?Job { ... }
fn is_end(job: Job) -> bool { ... }

test "the job machine is sound" {
    let allocator = mem::page_allocator();
    let table = try machine::table<Job, Signal>(allocator, step, Job::Waiting, is_end);
    defer table.deinit(allocator);
    var problems = try table.problems(allocator);
    defer problems.deinit(allocator);
    if problems.at(0) |line| {
        let bytes = line.as_bytes();
        testing::fail(bytes);
    }
}
```

検査するのは関数の答えです。関数が状態の外のデータを読むなら、そのデータの値ごとの
違いは表に現れません。

## まだ無いもの

- 拒否の種類や結果の出力を表に載せること。`step` は状態だけを返します
