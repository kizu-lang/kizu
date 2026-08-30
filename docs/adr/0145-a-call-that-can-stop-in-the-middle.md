# ADR-0145: 途中で止まれる呼び出しを先に入れる

## Status

Accepted.

## Context

ADR-0141 は `evented` な `Io` を却下ではなく**順番**だと書きました ——
中断できる runtime が先、`Io` がそれを表すのが次、`evented` はその実装。

その 1 つ目がありませんでした。`std::net::Poller` は「多数を待つ」を解きますが、
待った後に**呼び出しの途中から再開する**ことはできません。`read_into` の中で
byte が来ていないと分かったとき、thread を返す道が無いからです。返すには、
上に積まれた frame が何も知らないまま止まれる必要があります。

## Decision

`std::coro` を入れます。**coroutine は並行性ではありません** —— 同時に走るものは
無く、`resume` が止まるところまで走らせる間は他に何も起きません。増えるのは
止まれる場所です。

```kizu
var task = try coro::spawn(counting, 10, 65536);
defer task.deinit();
while task.resume() { }
```

stack は coroutine が持ちます(stackful)。だから止まった呼び出しは局所変数を
そのままの場所に保て、`std::net` の奥の read が上の frame を書き換えずに止まれます。
固定 stack の直下には読み書き不可の guard page を置き、native Kizu
関数はページを飛び越さない間隔で stack を probe します。guard を設定
できない spawn は失敗し、実行中の overflow は process を停止します。

context の切り替えは `ucontext` です。macOS では deprecated ですが動きます。

## Consequences

`std::coro` には channel も task も scheduler もありません。それらは ADR-0025 が
撤回した API の形で、そこで決めた順番 —— 走るものが先 —— に従っています。

**entry が受け取れるのは数 1 つです。** 関数 pointer は borrow を運べますが、
低レベル coroutine ABI は型付き state の ownership / lifetime adapter を持ちません。
実際の引数と結果を運ぶのは `Io.async` の仕事で、そこで形が変わります。

**終わっていない coroutine の cleanup は走りません。** 走らせることは、誰も続きを
頼んでいない呼び出しを再開することだからです。spawn した caller が終わりを決めます。

**stack overflow は catch しません。** 尽きた stack 上で cleanup や unwind を始めると
その実行自体の stack が無いため、guard への access で OS に process を止めさせます。

`test` block の中から関数 pointer を値として使えるようになりました。block の
合成名が module を持っていなかったので、名前が解決できていませんでした ——
`std::coro` を test から使おうとして踏みました。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `async fn` / `await` を入れる | function coloring。SPEC §15 が実装しないと決めている。stackful なら着色が要らない |
| thread を先に入れる | ADR-0025 が決めた順番の逆。thread は data race safety を要求し、それは動く実行系の上でしか書けない |
| architecture ごとの手書き context switch | `ucontext` が Darwin arm64 と Linux の両方で動く。手書きは deprecated が実際に消えたときの答えで、そのときの差し替え箇所は runtime.c の 1 節 |
| stackless(状態機械への変換) | 呼び出しの途中で止まるには全 frame の変換が要り、それが function coloring。`std::http` を手で状態機械にした経験がその形 |
| `resume` が失敗を返す | 終わった coroutine を resume するのは失敗ではない。false は「もう続きが無い」で、loop はそれで終わる |
| coroutine の外の `suspend` を失敗にする | 戻る先が無いだけで、caller にできることは何も無い。何もしなかった呼び出しの方が正しい |
| stack overflow を error として catch / unwind する | その処理を走らせる安全な stack がもう無い。保護違反で process を止める |
