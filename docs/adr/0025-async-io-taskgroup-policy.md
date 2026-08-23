# ADR-0025: 並行 API を撤回し、thread は実行系を先に作ってから入れる

Status: 改訂（当初の TaskGroup / channel API は撤回。thread 自体は入れる）

## 背景

この ADR は当初「v0.1 で `Io` capability と `Task` / `TaskGroup` の API 形状と
checker rule を固定する」と決めた。実 OS thread と event loop は「後続で扱う」と
明記したうえで、その後続機能の安全規則だけを先に書いた。

結果として次の状態になった。

| 層 | 実体 |
|---|---|
| types checker | 744 行 / 37 関数 |
| ownership checker | 594 行 / 33 関数 |
| `std/src/{task,channel,thread,sync,atomic}.kizu` | 61 行（全部 builtin への転送） |
| IR lowering | 0 |
| runtime | 0 |

`kizu check` は通り、`kizu run` は `ir error: unsupported callee
std::builtin::task_group` で落ちた。v0.1 conformance 200 件のうち 71 件が並行
関連で、その 55 件が「これは書けない」という negative case、動くはずの 11 件は
全部 `pending`（XFAIL）だった。1 行も実行されていない。

安全規則が実行によって反証されない状態が続いた。実際、次の欠陥は
gate をすべて通過したまま残っていた。

- `std::io::blocking()` と `std::io::threaded()` は native runtime で同じ値
  （`KIZU_IO_WORKING`）を返す。runtime の区別は存在しなかった。
- `TaskGroup` / `Queue` / `Partition` / `LocalBuffer` は unqualified な予約名で、
  ユーザーが `fn TaskGroup(...)` を定義して呼ぶと
  `type error: use std::task::Group(io)` になった。
- ADR-0025 は「Rust の `Send` trait は採用しない」と書きながら、同じ規則を
  `rejectThreadBoundary*` 7 関数と `rejectConcurrencyBoundary*` 7 関数として
  2 つの checker に手書きで二重実装していた。ユーザーは書けず、拡張もできない。
- SPEC.md は `TaskGroup` を「interpreter 上の structured task model」「`threaded()`
  は goroutine で実行」と説明していた。その interpreter は ADR-0083 で
  削除済み。仕様書が存在しない実装を説明していた。

## 決定

並行 API を言語から撤回する。thread は撤回しない。

### 撤回するもの

```text
std::task::Group / Task<T> / Queue
std::task::parallel_for / parallel_map / partition_mut / LocalBuffer
std::channel::Channel<T>
std::thread::scoped<T>
std::sync::Mutex<T>
std::atomic::Atomic<T>
std::io::threaded()
```

`std::io::threaded()` も一緒に撤回する。唯一の消費者だった `TaskGroup` が消え、
`blocking()` と区別が付かない名前だけが残るため。

`Io` capability そのものは残す。`std::fs` / `std::io` / `std::process` は
引き続き `Io` を第 1 引数に取り、失敗を `!T` で返す。

### thread は入れる

Kizu は並列処理のために thread を入れる。これは撤回対象ではなく、確定した
方針である。ただし順番を逆にしない。

**実行系が先。lowering と runtime が動いてから checker rule を書く。**

規則を実行より先に固定したことが今回の失敗の原因だった。次は、動く thread の
上でしか安全規則を書かない。

### 戻すときの制約

形は未定だが、次の 2 つは決まっている。

1. **Zig を参照する。** ADR-0039 の方向（hidden global runtime を持たない、
   `Io` を明示的に渡す、allocator を明示する、function coloring を作らない）を
   継続する。`async fn` 中心の設計には寄せない。
2. **memory race safety は譲らない。** Zig は data race を型で防がない。Kizu は
   防ぐ。safe Kizu で data race を書ける API は、どれだけ便利でも採用しない。

この 2 番目が Kizu が Zig から離れる点であり、並行 API の設計を評価する基準に
なる。「Zig にこう書ける」は理由にならない。「safe code で race が書けない」が
条件である。

API の個数は最小から始める。今回撤回した 8 個の型を一度に戻さない。

### 受け入れ条件

thread を戻すとき、次を満たす必要がある。これは checker rule の一覧ではなく、
**実行で示すべきこと**の一覧である。撤回前の失敗は「規則は書いたが動かなかった」
ことだったので、各行は動く thread の上での実行によってのみ満たされたとみなす。

| 満たすこと | 何で示すか |
| --- | --- |
| safe code で data race が書けない | race を作ろうとする negative example が reject される。加えて、通る example が実際に並列実行される |
| spawn した仕事は必ず join される | scope を抜ける時点で未 join が残らない。leak を作る negative example |
| borrow が thread 境界を越えて生き延びない | 境界を越えようとする negative example。規則は 1 つの再帰述語で書く（7 関数 × 2 checker にしない） |
| 共有可変状態は明示型を通る | 暗黙共有を作る negative example |
| worker の失敗が呼び出し側に届く | `!T` を返す worker の example が、失敗を実行時に伝播する |
| cancel / cleanup が二重解放も leak も起こさない | 実行 example。`deinit` / `defer` / `errdefer` と組み合わせた形で |
| allocator が明示される | hidden global allocator を使わない（ADR-0041） |
| `Io` が明示的に渡る | hidden global runtime を持たない（ADR-0039） |
| backend が lowering を持つ | `kizu check` が通った並行 program は `kizu run` でも通る。`pending` で埋めない |

最後の行が撤回の直接の理由なので、これを最初に満たす。API を 1 つ決めたら、
checker rule より先に IR lowering と runtime を作り、実行できる状態にしてから
安全規則を足す。

`docs/memory-safety.md` の Regression Coverage 表には、行が実際に example で
裏付けられた時点で追加する。空欄の行を先に置かない。

## 影響

- 2 つの checker から 1,869 行（正味）、conformance から 79 件、example 79 件、
  std module 5 個が消える
- `kizu check` が通った program は `kizu run` でも並行を理由に落ちなくなる
- pending（XFAIL）は 19 件から 3 件になる
- 予約名 `TaskGroup` / `Queue` / `Partition` / `LocalBuffer` が解放される
- ADR-0039 の `Io` interface 方針は残る。`TaskGroup` を前提にした記述だけ外す
