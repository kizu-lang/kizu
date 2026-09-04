# std の境界

std は Kizu で書かれた package です(`lib/kizu/std/src/`)。compiler は利用者の
package と同じ loader でそれを読み、`internal/stdlib` が持つのは「ツリーがどこに
あるか」だけです。

この文書が持つのは 2 つだけです —— **どこまでが trusted primitive か**と、
**新しい std API を足すときに何が要るか**。各 module の API は
[docs/std/](std/README.md)、設計判断は `docs/adr/`、言語側の契約は `SPEC.md` です。

## trusted primitive の境界

public な `std::...` は Kizu source で書きます。Kizu で書けないもの —— OS、process、
file、allocator、backend の境界 —— だけが `std::internal::builtin::*` の primitive に
なり、Go 実装(および `compiler/` の対応する module)が提供します。primitive は
std からしか届かず、user code からの直接呼び出しは拒否されます。

| 領域 | trusted primitive | 上に載る Kizu API |
| --- | --- | --- |
| memory | `mem_page_allocator`、`mem_fixed_buffer`、`mem_allocator_from`、`mem_len` | `std::mem` |
| storage | `array_*`、`map_*`、`arena_*`、`box_*` | `std::array`、`std::map`、`std::arena`、`std::mem::Box` |
| host I/O | `io_*`、`fs_*`、`net_*`、`process_*` | `std::io`、`std::fs`、`std::net`、`std::process` |
| 実行 | `coro_*`、`task_*` | `std::coro`、`std::io` の `async` / `TaskSet` |
| trap | `panic`、`test_fail`、`test_fail_equal<T>` | `std::json` の誤用 trap、`std::testing` |
| test | `test_seed`、`test_seed_set` | `std::testing::seed`(`kizu test --seed`) |
| diagnostic | `print_line` | `std::fmt::print`(builtin `print` の body、SPEC §14.1) |
| float | `f64_bits`、`f64_from_bits` | `std::float` |

規則:

- primitive は小さく、明示的で、capability の形を保つ。検証・error の整形・
  capability の可視性は Kizu 側の wrapper が持つ。
- Kizu で書けるようになった primitive は消す。消すときは example と conformance で
  挙動が変わらないことを見せる。
- Go 側の分岐は `std::internal::builtin::*` の名前を使う。public な `std::...` の
  名前で分岐しない。
- hidden な global allocator / global runtime / 暗黙の blocking I/O / 無言の
  fallback を持たない(`docs/principles.md` 原理 4)。

`Allocator` は可視な opaque capability で、user 実装も書けます(ADR-0129)。確保した
allocator と解放に渡す allocator が同じであることは tie 検査が持ちます
(ADR-0132、SPEC §14.3)。API としての形は [docs/std/mem.md](std/mem.md) です。

## source layout

```text
lib/kizu/std/
  kizu.toml
  src/
    internal/builtin/   std からのみ届く primitive 宣言
    <module>/           1 directory = 1 module (array, http, json, ...)
    path/internal/      std::path からのみ届く下位 module
```

`internal` directory の下の module は、その directory がぶら下がる部分木からだけ
届きます。可視性の規則はこれだけで、manifest は export を列挙しません。

compiler が予約する root namespace は `std` です。利用者の package を `std` と
名付けることはできません。

## 新しい std API を足すとき

同じ変更に次が全部要ります。

- [docs/std/](std/README.md) の該当 module に、署名・所有権・error・capability・
  観測できる挙動を書く。
- `examples/` に positive な実例を 1 つ。安全境界や error 境界があれば
  `examples/negative/` に拒否例を 1 つ。
- その example の末尾に、実行すると何が出るかの case block を書く。
- 新しい trusted primitive を足したなら、上の表に行を足す。
- ADR は、理由や却下案が残る**別の設計判断**があるときだけ書く。API の契約や
  実装コメントの写しを ADR にしない。
