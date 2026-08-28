# ADR-0141: 多数を待つことは値であって、切り替えるものではない

## Status

Accepted.

## Context

HTTP server は 1 接続ずつしか捌けませんでした。1 本を待っている間、他の声が
聞こえないからです。keep-alive の既定を 1 にしたのも、accept deadline を入れたのも、
この 1 つの事実の帰結です(ADR-0137、ADR-0139)。

SPEC §15.1 と ADR-0039 は `std::io::evented()` を将来の実装候補として挙げて
いました。読み方は 2 つあります。

1. `Io` の実装を差し替えると、`read_into` が裏で多重化される
2. 多数を待つための値が別にあり、待っていることが source に見える

1 を選ぶと `read_into` の**途中で中断して再開する**ことになります。coroutine が
要り、function coloring が生えます。SPEC §15 はどちらも持ちません。

さらに、1 は ADR-0025 が捕まえた形そのものです —— `blocking()` と `threaded()` が
runtime で同じ値を返し、区別が存在しないまま checker rule だけが 1338 行あった
状態。「Io を差し替えると挙動が変わる」は、差し替えた先が無いときに最も書きやすい嘘です。

## Decision

`std::net::Poller` を値として持ちます。`io::evented()` は作りません。

```kizu
var poller = try net::poller_new(handle, 64);
try poller.watch_stream(handle, &conn, token, net::Interest::Read);
let count = try poller.wait(handle, net::deadline_in_millis(1000));
```

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

`std::http` はまだこの上に載っていません。`accept` が head を blocking で読むので、
載せるには head 読みを再開可能にする必要があります —— `Exchange` が既に `pending`
を持っているので、状態を置く場所はあります。

**接続を collection に持てないことが分かりました。** `Array<T>` / `Arena<T>` は
要素が `deinit(allocator)` で解放されることを要求し、socket は descriptor を
解放するので `deinit()` です。汎用の evented server と `examples/net_poller.kizu`
の間に立っているのはこれだけで、次に直すものです
(`docs/language-gaps.md`)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `std::io::evented()` —— Io を差し替えると read が多重化される | `read_into` の途中で中断して再開する必要があり、coroutine と function coloring が生える。SPEC §15 が持たないもの。加えて ADR-0025 が捕まえた「差し替えた先が無いのに差し替えられるふりをする」形そのもの |
| `async fn` / `await` を入れて poller を隠す | function coloring。SPEC §15 が明示的に実装しないと決めている |
| `wait` が event の配列を返す | Kizu に shift 演算子が無く、i64 を byte に詰め直す経路になる。`wait` が個数を返し `ready(i)` が 1 つを返す形なら packing が要らない |
| poller の handle を fd にする | kqueue / epoll の fd だけでは event buffer を持てない。handle は runtime が確保した構造体で、Kizu の owner が private に持ち、consuming deinit が唯一の解放経路 |
| kqueue の 2 event を 1 つに畳んで epoll に合わせる | host が持っていない順序を発明することになる。渡された flag に従う caller はどちらでも正しく動く |
| `capacity` を超えた分を error にする | 溢れた descriptor はまだ ready なので、次の `wait` が即座に返す。失われないものを失敗として報告しない |
| read readiness だけで出す | write readiness は、読むのをやめた peer への write が block する代わりに「書けない」と分かる形。timeout で塞いだ穴と同じ性質のもので、後から足すと `watch_stream` の signature が変わる |
