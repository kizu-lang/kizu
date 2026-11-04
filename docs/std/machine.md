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

std::machine::answers<S, E, O>(
    allocator: Allocator,
    next: fn(S, E) -> S,
    outcome: fn(S, E) -> O,
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

拒否にも行き先と理由がある機械は `answers` で読みます。すべての組に `next` と
`outcome` を問うので、`moves` は状態の数 × 事象の数だけ並び、拒否も結果の名前つきの
遷移になります。`table` の遷移の結果は `applied` です。

`Table` の field は名前で読めます。`moves` は `Move { from, event, outcome, to }` の
配列で、`states`、`events`、`outcomes`、`ends` とともに宣言順です。「`Connected` へはこの事象でだけ入る」の
ような機械ごとの性質は、`moves` を test で歩いて確かめます。

`problems` は、問題 1 つにつき 1 行を返します。

| 行 | 意味 |
| --- | --- |
| `unreached <状態>` | `start` からどの遷移でも入れない |
| `stuck <状態>` | 入れるが、そこから `is_end` の状態へ行けない |
| `escape <終わり> <事象> <状態>` | 終わりの状態から別の状態へ移る |

`is_end` が常に偽の機械は、止まらない機械として `unreached` だけを確かめます。

`to_json` は `{ states, events, outcomes, start, ends, moves }` を 1 項目 1 行で書きます。file に
残して commit すれば、遷移の変更が差分の行に出ます。Lean の表は `scripts/machine-lean` が、
Quint の機械は `scripts/machine-quint` が、この JSON から作ります(`spec/README.md`)。

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

`problems` は `to` だけを見ます。拒否で終わりの状態へ移る機械では、まともな事象を
どれも受けない状態も `stuck` になりません。拒否の扱いは `moves` を test で確かめます。

検査するのは関数の答えです。関数が状態の外のデータを読むなら、そのデータの値ごとの
違いは表に現れません。
