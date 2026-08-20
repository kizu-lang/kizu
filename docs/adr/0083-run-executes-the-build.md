# ADR-0083: `run` は `build` した成果物を実行する

Status: 採用


## 背景

`kizu run` は tree-walking interpreter で AST を直接評価し、`kizu build` は
typed SSA IR を経由して LLVM に落としていた。共通なのは parse と check だけで、
その先は**同じ言語仕様を 2 回実装していた**。

v0.1 は interpreter-first と決めて始まり、native executable generation と full
LLVM backend を完了条件から外していた。当時は正しい判断だった。今は backend が
`examples/` の 82 本中 66 本を lowering できるところまで来ている。

二重実装が何を生んでいたかは、両経路を実際に突き合わせて分かった。link まで
成功する 66 本のうち **6 本が interpreter と違う答えを出していた**。

| example | interpreter | native |
| --- | --- | --- |
| `enum.kizu` | `Color::Green` | (enum 名が出ない) |
| `mutable_borrow.kizu` | `bob` | `alice` |
| `std_array.kizu` | 長さ `2` | 長さ `4` |

conformance の 366 ケースはすべて interpreter 経路でしか走らないため、これらは
緑のまま何ヶ月も残っていた。`mutable_borrow.kizu` は借用経由の変更が反映されて
おらず、メモリ安全を掲げる言語で借用の意味論が経路ごとに違っていた。

さらに negative example を native で走らせると、範囲外 index は trap するものの
**診断を一切出さずに落ちて**いた。interpreter は `index out of bounds` と言って
止まる。同じ失敗が経路によって別の見え方をしていた。

これは ADR-0082 が selfhost で踏んだ失敗と同型である。違いは、
第二実装が Kizu で書かれていたか Go で書かれていたかだけで、構造は同じ。

## 決定

### 1. `kizu run` は `kizu build --target native` と同じ成果物を実行する

`run` は lowering → LLVM → clang link を経て実行ファイルを作り、それを実行する。
`build` との差は「実行するかどうか」だけになる。同じソースが run と build で
違う挙動を示すことは、経路が 1 本しかないため原理的に起こらない。

interpreter へ落とす hidden fallback は置かない。native が lowering できない
機能は、`run` でも失敗する。

### 2. package も単一ファイルと同じ entry 規約にする

package の native entry は `<root>::cli_main` を探していたが、その名前の関数は
tree のどこにも無く、機構は一度も動いていなかった。root module の `fn main` を
entry とする。単一ファイルと同じ規約である。

### 3. 埋まっていない穴は conformance に `pending` として明示する

native が満たせないケースは manifest に理由付きで登録する。

```json
{ "name": "enum", "mode": "run", "pending": "native drops the enum name from print" }
```

`pending` を持つケースは「今も通らないこと」を検査する。**通るようになったら
テストが落ちる**ので、穴が埋まった変更は同じ変更で登録を消すことになる。
消える条件のない除外リストにはならない。

### 4. 正は conformance manifest であって interpreter ではない

期待値を持つのは manifest である。interpreter はもはや正ではなく、`kizu test`
が使う実行系として残るだけになる。この 1 用途が無くなった時点で
`internal/interp` は削除する。

**実施済み**: `kizu test` も同じ経路に移し、`internal/interp`(6,954 行)を
削除した。`test "name" { ... }` は `!void` を返す IR 関数に lower され、
entry が各 test を `try` で呼ぶ。test の診断も runtime に移った。

## 影響

- `examples/` の 82 本のうち `run` が通るのは 60 本。22 本を `pending` に登録した。
  内訳は lowering 未実装 16、結果が違う 6。
- negative example 14 本も `pending`。うち 6 本は診断が出ないか capability が
  効かないもので、範囲外アクセス自体は `llvm.trap()` で止まっている。
- `go test ./...` は 16 秒から 40 秒になった。run が clang link を通るため。
  120 秒の目標内に収まっている。
- `kizu run` は clang を必要とする。native path が host clang と libc を使う
  という既存の build policy がそのまま run にも及ぶ。

## 埋める順序

1. lowering 未実装 2 本(dyn contract method、Box borrow method)
2. `!u8` から返した整数 literal が `i64` になる 1 本

範囲外 index と slice の診断は ADR-0084 で埋めた。
並行 API 由来の未実装は ADR-0025 の撤回でなくなった。

進捗は `just backend-matrix` が表にする。
