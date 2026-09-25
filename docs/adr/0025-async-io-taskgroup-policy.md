# ADR-0025: thread は重ならない塊を join まで走らせる 1 つの呼び出しから入れる

## 背景

最初の並行 API(`std::task::Group`、`Channel<T>`、`Mutex<T>`、`Atomic<T>` など
8 個の型と `std::io::threaded()`)は、checker rule だけがあって IR lowering も
runtime も無かった。`kizu check` は通り、`kizu run` は落ちた。安全規則が実行で
反証されないまま、同じ規則が 2 つの checker に 14 関数で手書きされていた。
これを撤回し、**実行系を先に作り、安全規則は動く thread の上でだけ書く**順番にした。

## 決定

thread API は `std::thread::Pool` の round だけにする(SPEC §15.3)。
`each<T, E>` は `&var []T` を重ならない塊に切って pool の thread と呼び出し側で
同時に処理し、全部の塊が終わってから返る。`each_lane` は一定間隔の要素の列(lane)を
thread ごとの領域へ集めて連続した `&var []T` で渡し、返ったら書き戻す。

- thread は `init` で起こし、round の間は少し spin してから寝かせる。塊は round の
  tag 付きの ticket から数個ずつ取り、round は全部の塊が終わった時点で終わる
- stack と lane の領域は渡された allocator から取る。stack には guard page を置く
- worker は `fn(&var []T, i64) -> E!void`。最初の失敗が round の戻り値になり、残りの
  塊は始めない。backend が set ごとの thunk で Kizu の ABI で呼び、error code を受け取る
- `T` は view と `Io` / `Allocator` を含めない(`std::io::spawn` と同じ述語)
- wasm target は build 時に拒否する

## なぜこの形か

data race を型で防ぐ条件が、Kizu にはもう揃っている。closure と捕捉が無く、
書き換えられる global も無いので、worker が触れるのは引数だけである。借用は
呼び出しの間だけ生き、`each` は join してから返るので、thread 境界を越える借用の
規則を新しく書く必要が無い。足した規則は要素型の述語 1 つで、既存のものを使う。

## 却下した案

| 案 | 理由 |
| --- | --- |
| 撤回した 8 個の型を戻す | 規則だけが先に立つ状態に戻る。1 つずつ動かしてから足す |
| `async fn` / `await` | function coloring を作る。待ちは `Io` の実装が持つ(ADR-0039) |
| Rust の `Send` / `Sync` trait | 利用者が読めない手書きの whitelist になる。規則は要素型の述語 1 つにする |
| Zig 風の `spawn` と handle の `join` | 「必ず join する」と「借用を thread へ持ち出さない」を checker に新しく足す必要がある |
| `each` のたびに thread を起こす | 1 回の round が短いと、thread を起こす時間が仕事を上回る |
| round の終わりを「全 helper が一度入って抜けた」で判定する | OS に止められた helper が仕事を持たないまま round 全体を止める。thread が空いている core より多いと 16 thread で 4 倍遅く、8 thread でも伸びが止まった |
| round の間は condition variable でだけ待つ | 起こす時間が短い round を上回る。24 × 24 × 24 の格子を 3 方向の lane で回す計測で、8 thread の速さが 1 thread の 2.5 倍から 4.5 倍になった |
| 飛び飛びの要素を指す `Lane<T>` view 型 | borrow は struct field に置けない。checker が知る view 型と SPEC の規則が増える。std が集めて連続した `&var []T` で渡し、書き戻せば足りる |
| 入力を共有して別 buffer に書く `each_from` | buffer が 1 本余計に要る。集めて書き戻す形で同じことができる |
| worker に `Allocator` を渡す | fixed buffer と `allocator_from` の allocator は thread 間で共有できない |
| wasm では 1 thread で走らせる | 隠れた fallback になる。並列を頼んだ program が黙って直列になる |
| 失敗の型を set なしの `!void` にする | 呼び出し側が `catch` できず、set を宣言した関数から `try` できない。型引数を set に取れるようにした |
| 失敗しない worker(`-> void`)も残す | 同じことに 2 つの書き方ができる |
| runtime の C から worker を slice の値渡しで呼ぶ | target の aggregate ABI に頼る。slice は型によらず同じ {ptr, len} なので thunk 1 つで済む |
