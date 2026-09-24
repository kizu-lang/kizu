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
```

`cpu_count` は、この process がいま使える processor の数です。1 以上です。

`init` は**呼び出し側を数に含めて** `threads` 本の pool を作ります。起こすのは
`threads - 1` 本で、呼び出し側も `each` の中で塊を処理します。`threads` が 1 なら
thread を 1 本も起こさず、`each` は呼び出し側だけで走ります。1 から 1024 の外は
panic です。thread の stack(8 MiB と、その下の guard page)は `allocator` から
取り、`deinit` が同じ allocator へ返します。

thread は `init` で起こし、`each` の間は待たせておきます。1 回の `each` が短い
仕事では、thread を起こす時間のほうが仕事より長いためです。

`each` は `data` を `chunk` 要素ずつに切ります。最後の塊だけは短いことがあります。
`worker(part, index)` の `index` は 0 から数えた塊の番号です。塊がどの thread で、
どの順に走るかは約束しません。`chunk` が 1 未満なら panic です。空の `data` では
worker を呼びません。

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
- 飛び飛びの位置にある要素を塊にすること(行列の列など)。std が塊を集めて
  worker に連続した `&var []T` として渡し、終わったら書き戻す形にします
- thread handle、channel、mutex、atomic
