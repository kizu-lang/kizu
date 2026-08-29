# ADR-0141: 多数を待つことは値であって、切り替えるものではない

## Status

Accepted.

## Context

HTTP server は 1 接続ずつしか捌けませんでした。1 本を待っている間、他の声が
聞こえないからです。keep-alive の既定を 1 にしたのも、accept deadline を入れたのも、
この 1 つの事実の帰結です(ADR-0137、ADR-0139)。

SPEC §15.1 と ADR-0039 は `std::io::evented()` を将来の実装候補として挙げて
いました。当時の選択肢は 2 つでした。

1. `Io` の実装を差し替えると、`read_into` が裏で多重化される
2. 多数を待つための値が別にあり、待っていることが source に見える

1 を選ぶと `read_into` の**途中で中断して再開する**ことになります。当時はその
coroutine runtime が無く、名前だけの mode を先に出すことはできませんでした。

さらに、1 は ADR-0025 が捕まえた形そのものです —— `blocking()` と `threaded()` が
runtime で同じ値を返し、区別が存在しないまま checker rule だけが 1338 行あった
状態。「Io を差し替えると挙動が変わる」は、差し替えた先が無いときに最も書きやすい嘘です。

## Decision

`std::net::Poller` を値として持ちます。readiness、token、接続 state を caller が
扱う道は、`Io` の hidden mode にしません。

```kizu
var poller = try net::poller_new(handle, 64);
try poller.watch_stream(handle, &conn, token, net::Interest::Read);
let count = try poller.wait(handle, net::deadline_in_millis(1000));
```

その後、中断できる runtime を先に入れて `io::evented()` も実装しました
(ADR-0145、ADR-0146)。こちらは `io::async` / `io::spawn` で worker を作る場所が
source に見え、普通の read を接続ごとの直線 code のまま書く別の道です。

`token` は caller のものです。std::net は一度も読みません —— 「どの接続か」を
知っているのは caller だけなので、それを表す値を預かって返すだけです。

host が言った通りを渡します。kqueue は filter ごとに 1 event、epoll は 1 event に
両方の bit。read と write の両方が立った descriptor は host によって 1 回とも
2 回とも来ます。

## Consequences

待っていることが source にあります(原理 2)。「この読みは多重化されているのか」
を型や runtime 設定から推理する必要がありません。

`Poller` は並行性ではなく**多重化**です。1 thread が多数の接続を扱えるように
しますが、2 つのことを同時にはしません。thread は別の作業で、ADR-0025 が残した
順番(実行系が先)はそちらにも掛かります。

### `evented` が戻ってきた条件

この判断は「evented な Io は間違い」ではなく、**当時の `Io` には中断が無かった**
というだけでした。置いた順番は —— 中断できる runtime が先、`Io` がそれを表すのが
次、`evented` はその実装。3 つとも入りました(ADR-0145、ADR-0146)。

`Poller` はその内側の道具になり、値としても残ります。`std::http` の
`first` / `next`(ADR-0144)は poller を直接使う loop で、`evented` は同じ
多重化を worker ごとの直線 code で書く道です。

### 待つのと書くのは別の問題でした

`Poller` は「**入力を待つ**」を解きました。喋っていない接続に thread が座ること
はもうありません。**「出力を出す」は解きませんでした** —— `write_all` は終わる
まで返らないので、読むのをやめた peer 1 人が loop を deadline いっぱい止めます
(実測 1000 ms)。

`write_some` を足しました。今入る分だけ送って数を返し、残りは caller が持って
wait に戻ります。0 は「今は書けない」で、error でも終端でもありません。同じ
測定が **0 ms** になります。

`write_all` は残します。送らなければならない message —— 1 度きりの response の
head —— には、終わるか失敗するかの契約の方が正しいからです。

`std::http` は `first` / `next` でこの Poller path に載りました。さらに
`accept_connection` + TaskSet は、worker が `Exchange` を所有して普通の
`read_head` を待つ path です。

**接続を collection に持てないことがここで分かりました。** `Array<T>` /
`Arena<T>` が要素に `deinit(allocator)` を要求していたためで、ADR-0142 が
外しました。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 中断できる runtime より先に `std::io::evented()` を作る | 当時は `read_into` の途中で再開できず、`blocking()` と同じ挙動の名前だけの mode になる。中断できる runtime が先に入ったため、その順番を満たして現在は実装済み |
| `async fn` / `await` を入れて poller を隠す | function coloring。SPEC §15 が明示的に実装しないと決めている |
| `wait` が event の配列を返す | Kizu に shift 演算子が無く、i64 を byte に詰め直す経路になる。`wait` が個数を返し `ready(i)` が 1 つを返す形なら packing が要らない |
| poller の handle を fd にする | kqueue / epoll の fd だけでは event buffer を持てない。handle は runtime が確保した構造体で、Kizu の owner が private に持ち、consuming deinit が唯一の解放経路 |
| kqueue の 2 event を 1 つに畳んで epoll に合わせる | host が持っていない順序を発明することになる。渡された flag に従う caller はどちらでも正しく動く |
| `capacity` を超えた分を error にする | 溢れた descriptor はまだ ready なので、次の `wait` が即座に返す。失われないものを失敗として報告しない |
| `write_all` を `write_some` で置き換える | 送らなければならない message には「終わるか失敗するか」が正しい契約。両方あるのは意味が 2 つあるから(原理 7) |
| `write_some` に write deadline を掛ける | 待たないものに期限は無い。待つのは poller で、期限はその wait に掛かる |
| `write_some` の「今は書けない」を error にする | 失敗ではない。0 byte 書けたという事実で、caller の loop はそのまま次の wait に行く |
| read readiness だけで出す | write readiness は、読むのをやめた peer への write が block する代わりに「書けない」と分かる形。timeout で塞いだ穴と同じ性質のもので、後から足すと `watch_stream` の signature が変わる |
