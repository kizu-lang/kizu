# std::thread

A pool runs one function over a slice on several threads at once.

```kizu
fn square(part: &var []i64, index: i64) -> void {
    for 0..mem::count<i64>(part) |i| {
        part[i] = part[i] * part[i];
    }
    return;
}

var pool = try thread::init(io, allocator, thread::cpu_count(io));
defer pool.deinit(allocator);
let cells = values.as_mut_slice();
thread::each<i64>(&var pool, cells, 1024, square);
```

**これは並列です。** `std::coro` や `std::io::async` と違い、塊は複数の CPU で
同時に走ります。それでも data race が書けないのは、同時に走るものを使う呼び出しが
`each` 1 つだけだからです。`each` は slice を重ならない塊に切り、各塊を worker に
1 回ずつ渡し、全部の塊が終わってから返ります。

現在の runtime は native target だけが提供します。`wasm32-wasi` と
`wasm32-browser` は thread を起こせないため、`std::thread` に到達する program を
build 時に target 非対応として拒否します。

## API

```kizu
pub error Error { OutOfMemory, SpawnFailed, StackProtectionFailed }

pub struct Pool { }

pub fn cpu_count(io: Io) -> i64
pub fn init(io: Io, allocator: Allocator, threads: i64) -> std::thread::Error!std::thread::Pool
fn (self: Pool) deinit(allocator: Allocator) -> void

pub fn each<T>(
    pool: &var std::thread::Pool,
    data: &var []T,
    chunk: i64,
    worker: fn(&var []T, i64) -> void
) -> void

pub fn each_lane<T>(
    pool: &var std::thread::Pool,
    allocator: Allocator,
    data: &var []T,
    length: i64,
    stride: i64,
    worker: fn(&var []T, i64) -> void
) -> std::thread::Error!void
```

`cpu_count` は、この process がいま使える processor の数です。1 以上です。

`init` は**呼び出し側を数に含めて** `threads` 本の pool を作ります。起こすのは
`threads - 1` 本で、呼び出し側も `each` の中で塊を処理します。`threads` が 1 なら
thread を 1 本も起こさず、`each` は呼び出し側だけで走ります。1 から 1024 の外は
panic です。thread の stack(8 MiB と、その下の guard page)は `allocator` から
取り、`deinit` が同じ allocator へ返します。

thread は `init` で起こし、`each` の間は待たせておきます。1 回の `each` が短い
仕事では、thread を起こす時間のほうが仕事より長いためです。仕事の無くなった thread
は次の round を少しの間 spin して待ち、来なければ寝ます。

`each` は `data` を `chunk` 要素ずつに切ります。最後の塊だけは短いことがあります。
`worker(part, index)` の `index` は 0 から数えた塊の番号です。塊がどの thread で、
どの順に走るかは約束しません。`chunk` が 1 未満なら panic です。空の `data` では
worker を呼びません。

## 飛び飛びの要素: `each_lane`

`each_lane` は `data` を `length * stride` 要素の block の並びとして読みます。
block は `stride` 本の lane を持ち、lane `i` はその block の要素 `i`、`i + stride`、
…、`i + (length - 1) * stride` です。行優先の格子なら、ある軸に沿った lane は
`length` がその軸の大きさ、`stride` がそれより後ろの軸の大きさの積です。`stride` が
1 なら lane は行です。

```kizu
// 3 行 4 列の表を行優先で持つとき、列は 4 つおきの 3 要素
try thread::each_lane<i64>(&var pool, allocator, cells, 3, 4, running_total);
```

worker は `each` と同じ形で、lane を `length` 要素の普通の `&var []T` として
受け取ります。`stride` が 1 より大きい lane は、取った thread の領域へ集めて渡し、
worker が返ったら元の位置へ書き戻します。そのため他の thread がその要素を持つことは
ありません。領域は thread ごとに 1 つで、`allocator` から呼び出しの間だけ取り、返る
前に戻します。確保できなければ `OutOfMemory` です。`stride` が 1 の lane は集めずに
その場で渡すので、領域も取りません。

`index` は lane の番号で、block の順、block の中では `i` の順に 0 から数えます。
`length` か `stride` が 1 未満、または `data` の長さが block の整数倍でなければ
panic です。

## worker に届くもの

worker が触れるのは、渡された塊だけです。

- worker は top-level function なので、何も捕捉しません
- 書き換えられる global はありません
- 塊どうしは重なりません
- `T` は view と `Io` / `Allocator` を含められません。2 つの塊に同じ allocator が
  複製されると、2 つの thread が 1 つの allocator を同時に使うことになるためです

worker 自身が `std::mem::page_allocator()` や `std::io::blocking()` を作って使う
ことはできます。どちらも thread ごとの状態を持ちません。

worker の中の panic は process を止めます。worker は失敗を返せません。

## まだ無いもの

- 失敗を返す worker(`fn(&var []T, i64) -> E!void`)
- thread handle、channel、mutex、atomic
