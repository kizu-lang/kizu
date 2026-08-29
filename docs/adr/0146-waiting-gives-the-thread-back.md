# ADR-0146: 待つことは thread を返すこと

## Status

Accepted.

## Context

ADR-0141 が置いた順番の 3 つ目です —— 中断できる runtime が先(ADR-0145)、
`Io` がそれを表すのが次、`evented` はその実装。

`std::net::Poller` は「多数を待つ」を解きましたが、待つ側の source は変わり
ました。caller が poller と token 表と sweep を書き、`read_ready_into` で
「今は無い」を受け取り、loop に戻ります。動きますが、**普通の read を書いた
プログラムはその形に書き直さないと多重化に乗れません。**

## Decision

`Io` に `async` を持たせ、`io::evented` をその実装として入れます。

```kizu
var loop = try io::loop_new();
defer loop.deinit();
let handle = io::evented(&var loop);

var future = try io::async<Job>(handle, allocator, serve, &var job, 262144);
defer future.deinit();
future.await();
```

`serve` は普通の関数です。`read_into` は普通の `read_into` で、`async` とも
`await` とも書かれていません。変わるのは `Io` だけで、**待ち方**がその実装の
違いのすべてです。blocking な `Io` は戻ってこないことで待ち、evented な `Io` は
thread を返して待ちます。

runtime 側でこれが要求するのは 1 箇所です。`std::net` の待ちは全て
`kizu_net_wait` を通るので、evented ならそこで poll の代わりに coroutine を
park します。

### async は並行性の約束ではない

`blocking()` の `async` は worker をその場で走らせ、終わった Future を返します。
走らせるものが他に無い実装にとってはそれが正直な答えで、Zig の `Io` も同じ
ことをします。**差が出るのは待つときだけ**です —— evented なら 2 つの worker が
互いの待ちの間に進み、blocking なら順番に走ります。

### worker は借りるのであって、貰わない

worker は `fn(Io, Allocator, &var A) -> void` です。closure が無いので capture
できるものは無く、Io と allocator は引数として渡ります。作業対象は caller が
**貸した** 1 つの値で、Future はそれに tie されます —— fixed-buffer allocator が
buffer に tie されるのと同じ規則で、Future は貸し手の frame を出られず、貸した
値は Future が生きている間は触れません。

貰わないので返すものもありません。cancel しても await しても、値は最初から
caller のものです。

Future は owner なので落とせません。解放は cancel を通り、cancel は worker を
終わらせてから戻ります。

### cancel は待ちに届く失敗

cancel は worker に旗を立て、park している待ちを `net::Error::Canceled` で
起こします。worker は自分が書いた `catch` / `defer` を通って戻るので、握って
いたものは落ちません。旗を無視して待ち直す worker は cancel できません ——
context を見ない goroutine と同じです。

## Consequences

`std::http` の `first` / `next`(ADR-0144)はそのままです。あちらは poller を
直接使う loop で、こちらは同じ多重化を worker ごとの直線 code で書く道です。
どちらも `Poller` の上にあります。

`Io` の tie 検査が入りました。capability は「何かに届く許可」なので、local
から作った capability はその frame を出られません —— allocator に既にあった
規則を、同じ述語で `Io` にも掛けています。

**Future は generic ではありません。** user 定義の generic struct が無いので
`Future<A>` は書けず、状態を Future に持たせる形は取れませんでした。貸す形に
したのはその制約からですが、結果として値が caller の手を離れないぶん正しく
なっています。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `async fn` / `await` を入れる | function coloring。SPEC §15 が実装しないと決めている |
| `evented` を単独で出し、`async` は後 | 走らせるものが 1 本しか無ければ、待ちで thread を返しても行く先が無い。blocking と区別が付かず、ADR-0025 が捕まえた形になる |
| Future が状態を所有し、`await` が返す | generic struct が無いので `Future<A>` を書けない。compiler が知る generic 型にする案は、貸す形で足りるので採らない |
| worker に i64 を 1 つ渡す(coroutine と同じ形) | 接続を渡せない。関数 pointer は borrow を運べず、global も無いので、worker は何にも届かない |
| worker が `E!void` を返す | Future が error set を運ぶ必要があり、error set に generic な型が要る。goroutine と同じく、報告先は貸された状態 |
| cancel は resume せず stack を捨てる | worker が握っていたものが落ちる。終わらせてから解放する |
| cancel を `?T` の null で表す | null は「今は無い」で `read_ready_into` が既に使っている。cancel された待ちは後で来ることも無いので失敗。Go / POSIX / Zig も失敗にしている |
| `Io` に threaded 実装も同時に入れる | ADR-0025 の順番。thread は data race safety を要求し、それは動く実行系の上でだけ書ける |
