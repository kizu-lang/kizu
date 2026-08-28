# std::coro

A coroutine is a call that can stop in the middle and be told to go on.

```kizu
fn counting(start: i64) -> void {
    print(start);
    coro::suspend();
    print(start + 1);
    return;
}

var task = try coro::spawn(counting, 10, 65536);
defer task.deinit();
while task.resume() { }
```

**これは並行性ではありません。** 同時に走るものはありません。`resume` は
coroutine を止まるところまで走らせ、その間は他に何も起きません。増えるのは
**止まれる場所**です —— 呼び出しの先頭ではなく、途中で。

止まる場所が呼び出しの途中でよいことが、evented な `Io` に要るものです。byte が
まだ来ていない read は、**上に積まれた呼び出しがそれを予期して書かれていなくても**
thread を返せる必要があります(ADR-0141)。

## API

```kizu
pub error Error { OutOfMemory, Finished }

pub struct Coroutine { }

pub fn spawn(entry: fn(i64) -> void, arg: i64, stack_bytes: i64)
    -> std::coro::Error!std::coro::Coroutine
fn (self: &var Coroutine) resume() -> bool
fn (self: &Coroutine) finished() -> bool
fn (self: Coroutine) deinit() -> void

pub fn suspend() -> void
```

`spawn` は**何も走らせません**。coroutine が走る stack を確保して、どこから
始めるかを覚えるだけです。最初の `resume` が走らせます。

`resume` は止まるか返るまで走らせ、**まだ続きがあるか**を答えます。終わった
coroutine は何度聞かれても false を答えるので、それで終わる loop が書けます。

`stack_bytes` は下限(16 KiB)まで引き上げられ、上限(64 MiB)を超えると
`OutOfMemory` です。何も走らせられない数を渡した caller は、考えていなかった
だけであって「何も要らない」と言ったわけではありません。

## entry が渡せるのは数 1 つです

```kizu
fn entry(arg: i64) -> void
```

関数 pointer は borrow を運べません(`docs/language-gaps.md`)。なので coroutine に
渡るのは**数**で、その意味を知っているのは渡した側だけです。

これは今の形の限界です。`Io.async` が実際の引数と結果を運べるようにするのが
次の段で、そこで形が変わります。

## stack は coroutine のものです

各 coroutine が自分の stack を持ちます。だから止まった呼び出しは局所変数を
そのままの場所に保てます。**stackful** と呼ばれるのはこれで、`std::net` の奥の
呼び出しが、上の frame が何も知らないまま止まれる理由です。

`deinit` が stack を解放します。**終わっていない coroutine は、その stack が
まだ持っていたものを道連れにします** —— 誰もその cleanup を走らせられません。
走らせることは、誰も続きを頼んでいない呼び出しを再開することだからです。
だから spawn した caller が、終わりを決める caller です。

## 無いもの

channel も task も scheduler もありません。それらは ADR-0025 が撤回した API の
形で、そこで決めた順番 —— **走るものが先** —— に従っています。

`suspend` を coroutine の外で呼ぶと何もしません。戻る先が無く、caller に
できることが何も無い失敗を作るのは、何もしなかった呼び出しより悪いからです。

## 実装

context の切り替えは `ucontext`(`getcontext` / `makecontext` / `swapcontext`)
です。macOS では deprecated ですが動きます。architecture ごとの手書き switch に
差し替えるときは、`runtime.c` の coroutine 節が置き換え箇所です。

thread はまだありません。現在走っている coroutine は 1 プロセスに 1 つで、thread が
入ればそれは thread ごとになります。
