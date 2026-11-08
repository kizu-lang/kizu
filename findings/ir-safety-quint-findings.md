# internal/ir Quint モデル: 食い違いの記録

`spec/ir/` のモデルと `internal/ir` の lowerer / optimizer を突き合わせた結果です。
範囲と方法は `spec/ir/README.md` にあります。本体のコードは変えていません。

## 結論(2026-11-08、`verify/ir-safety-quint` の基点 24b0b09a)

* **slots**: 食い違いなし(checked-in 283 本、スイープ 772 本)。
* **defer**: 食い違いなし(checked-in 187 本 / 2,992 回、スイープ 589 本 / 9,424 回)。
* **bounds**: 実行結果の食い違いはなし。範囲外のアクセスはすべて止まるか `set` が
  失敗しました。その代わり、**モデルが出力を予測するプログラムを compiler が build
  できない**ものが 1 種類ありました(F1)。コンパイラ側のバグと切り分けられ、
  issue #1923 として起票、PR #1924 で両実装とも修正済みです。再現は解消し、
  `knownDivergences` の登録も外しました。

## F1. wasm `--opt` で、2 回目の `EliminateBoundsChecks` が既存の値と同じ名前を作る【解決済み: #1924】

### 最小再現

```kizu
import std::array;
import std::mem;

fn case0(a: &var array::Array<i64>, k: i64) -> !i64 {
    var s = 0;
    a.set(2, s) catch return -7;
    s = s + a.get_or_panic(2);
    s = s + a.get_or_panic(k);
    s = s + a.get_or_panic(k);
    return s;
}

fn main() -> !void {
    let allocator = mem::page_allocator();
    var a = array::new<i64>(allocator);
    defer a.deinit(allocator);
    try a.append(allocator, 10);
    print(try case0(&var a, 0));
    return;
}
```

```
$ kizu build --target wasm32-wasi --emit wasm --opt -o f.wasm f1.kizu
error: ir verify error: case0: block catch.ok.1 reads %bce1.at before it is defined
$ kizu build --target wasm32-wasi --emit wasm -o f.wasm f1.kizu    # --opt なし: 成功
$ kizu ir --opt f1.kizu                                             # native の最適化: 成功
```

ループの形(`while i < a.len() { a.set(i, 1) catch return -7; a.set(i, 2) catch return -7; i = i + 1; }`
の後に `a.get_or_panic(0)` を 2 回)では `reads %bce2.at, which is defined in catch.ok.4,
and catch.ok.4 does not dominate while.end.3` になります。どちらも
`internal/ir/runtime_mbt_test.go` の `knownDivergences` に入れてあります。

### 根拠

pass を 1 つずつ当てて `Verify` すると、壊れるのは `ReshapeForEngine` の中の 2 回目の
`EliminateBoundsChecks`(`EliminateCommonSubexpressions` の後)です。

* `applyBoundsPlan`(bounds.go)は答えたアクセスを `%bce<site>.at` / `.ref` に書き換え、
  `site` を **呼び出しごとに 0 から** 数えます。
* 1 回目で `%bce1.` `%bce2.` を使った関数で、2 回目が新たに答えたアクセス
  (CSE で同じ index になった 2 つ目の `get_or_panic`)にも `%bce1.` / `%bce2.` を付けます。
* 同じ SSA 名の定義が 2 つになり、`Verify` が使用側の支配関係を拒否します。

native(`Optimize` で `EliminateBoundsChecks` 1 回)では起きません。selfhost の
`compiler/src/internal/ir/bounds.kizu` の `apply_bounds_plan` も `var site = 0;` から数える
同じ形ですが、この環境では selfhost binary を build できず、実行では確かめていません。

### 判断に要るもの

この program は長さ 1 の Array に `set(2, ..)` するので `-7` を出力するはずで、`--opt` なしの
wasm はその通り `-7` を出します(同じ case 関数の形 `write C2, read C2, read K, read K` を
モデルは全 20 入力について予測しています)。`Verify` が止めているので誤ったコードは出ていませんが、利用者からは
有効な program が wasm `--opt` でだけ build できない状態です。直すかどうか、直すなら
Go 実装と `compiler/` の両方(AGENTS.md)をどう直すかは判断を待っています。

## 仕様の判断を要しないが、記録しておく観察

* **`for 0..e` の上限を何回評価するかは SPEC §6.11 に書かれていません。** モデルと
  生成プログラムは `let n = a.len(); for 0..n` と 1 回読む形にして、この問いを避けています。
* **`set` の範囲外は panic でなく `std::array::Error::OutOfBounds`** です(docs/std/array.md)。
  optimizer が答えた `set` は `at_mut` と store になり error を返さなくなるので、誤った
  消去は「error にならずに書き込む」として観測されます(変異 B1 / B2 で確認)。
