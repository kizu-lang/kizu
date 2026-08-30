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

var future = try io::async<Job>(handle, allocator, serve, &var job);
defer future.deinit(allocator);
future.await();
```

`serve` は普通の関数です。`read_into` は普通の `read_into` で、`async` とも
`await` とも書かれていません。変わるのは `Io` だけで、**待ち方**がその実装の
違いのすべてです。blocking な `Io` は戻ってこないことで待ち、evented な `Io` は
thread を返して待ちます。

worker stack は `async` が allocator から一度だけ確保し、実行中には伸ばしません。
大きさは std が 256 KiB に固定します。最終的な frame size は backend が決めるため、
caller ごとに同じ推測値を渡させても新しい情報にはなりません。確保する呼び出しと
allocator は source に残し、byte 数だけを定義側へ畳みます。

stack の直下は読み書き不可の guard page で、native Kizu 関数は guard を
飛び越さない間隔で stack を probe します。guard の設定失敗は
`StackProtectionFailed` として `async` / `spawn` から返し、保護なしの fallback は
持ちません。実行中の overflow は catch せず、OS の保護違反で process を止めます。

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
値は Future が生きている間は触れません。Future が保持する Io と、storage を取る
allocator も shared に tie され、それぞれの backing state より先には解放できません。

貰わないので返すものもありません。cancel しても await しても、値は最初から
caller のものです。

Future は owner なので落とせません。解放は `deinit(allocator)` から cancel を通り、
worker を終わらせてから `async` で名指した allocator へ stack を返します。

### N 個の渡し切る仕事は TaskSet が所有する

Future は caller が結果を読み戻す 1 state を借ります。HTTP connection のように
caller が worker へ渡し切る state は `TaskSet` が N 個所有します。

```kizu
var tasks = try io::task_set_new(handle, allocator);
defer tasks.deinit(allocator);
try io::spawn<Job>(&var tasks, work, move job);
```

TaskSet は構築時の Io と allocator を保持し、各 worker に同じ 2 つを渡します。
`spawn` は struct state を値で worker へ move し、worker の通常の ownership 検査が
全経路の cleanup を要求します。state に view / borrow や別の Io / Allocator は
入れられません。TaskSet 自身は保持した capability に tie されます。

set、state、stack の確保は `task_set_new` に渡した allocator から取り、解放でも
同じ allocator を source に書きます。完了した worker は set が回収し、set の
`deinit` は残りを cancel して各 worker の `defer` を通してから解放します。

### cancel は待ちに届く失敗

cancel は worker に旗を立て、park している待ちを `net::Error::Canceled` で
起こします。worker は自分が書いた `catch` / `defer` を通って戻るので、握って
いたものは落ちません。旗を無視して待ち直す worker は cancel できません ——
context を見ない goroutine と同じです。

## Consequences

`std::http` には 2 つの道があります。`first` / `next`(ADR-0144)は server が接続
state を持つ poller loop、`accept_connection` + TaskSet は worker が Exchange を
所有して普通の `read_head` を書く loop です。どちらも 1 thread 上の多重化です。

`Io` の tie 検査が入りました。capability は「何かに届く許可」なので、local
から作った capability はその frame を出られません —— allocator に既にあった
規則を、同じ述語で `Io` にも掛けています。

**Future も TaskSet も generic ではありません。** Future は state を借りるので
結果を caller が読み戻せます。TaskSet は runtime が type-erased bytes を所有し、
LLVM の型付き thunk が worker の `fn(Io, Allocator, A)` ABI へ戻します。公開型を
`Future<A>` / `TaskSet<A>` に増やさず、用途の差を ownership の差にしました。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `async fn` / `await` を入れる | function coloring。SPEC §15 が実装しないと決めている |
| `evented` を単独で出し、`async` は後 | 走らせるものが 1 本しか無ければ、待ちで thread を返しても行く先が無い。blocking と区別が付かず、ADR-0025 が捕まえた形になる |
| Future が状態を所有し、`await` が返す | 結果を読み戻す用途は borrow で足りる。渡し切る N 個の仕事は TaskSet が所有し、1 Future に 2 つの責任を持たせない |
| `spawn` ごとに Io / allocator を渡す | worker が helper の frame を越えて capability を保持し得る。TaskSet 構築時に固定すれば、set の lifetime へ一度だけ tie できる |
| `async` / `spawn` ごとに stack byte 数を渡す | backend が決める frame size を caller は導けず、同じ推測値が call site ごとに増える。確保は allocator を取る呼び出しに見えているので、byte 数は std の定義側に畳む |
| worker stack を実行中に伸ばす | 開始後の見えない再確保と失敗を作り、live な borrow を含む stack の移動には compiler と runtime が共有する stack map も要る。開始時に固定量を確保する |
| stack overflow を worker の error にする | overflow 後に catch / cleanup を走らせる stack が無い。guard で process を止める |
| worker に i64 を 1 つ渡す(coroutine と同じ形) | 接続を渡せない。関数 pointer は borrow を運べず、global も無いので、worker は何にも届かない |
| worker が `E!void` を返す | Future が error set を運ぶ必要があり、error set に generic な型が要る。goroutine と同じく、報告先は貸された状態 |
| cancel は resume せず stack を捨てる | worker が握っていたものが落ちる。終わらせてから解放する |
| cancel を `?T` の null で表す | null は「今は無い」で `read_ready_into` が既に使っている。cancel された待ちは後で来ることも無いので失敗。Go / POSIX / Zig も失敗にしている |
| `Io` に threaded 実装も同時に入れる | ADR-0025 の順番。thread は data race safety を要求し、それは動く実行系の上でだけ書ける |
