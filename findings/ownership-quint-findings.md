# ownership Quint モデル: 食い違いの記録

`spec/ownership/` のモデルと `internal/ownership` の checker を突き合わせた結果です。
範囲と方法は `spec/ownership/README.md` にあります。checker のコードは変えていません。

## 結論(2026-11-08、`verify/ownership-quint` の基点 24b0b09a)

**checker 側の食い違いは見つかりませんでした。** checked-in の 490 本と、一回限りの
相異なる 2,880 本(入れ子ループ、`while`、ループを越える view / borrow の読み、
無関係な owner の `defer` を含む)のすべてで、受理 / 拒否と診断クラスがモデルと一致
しました。#1913 の修正(eb407c46)前の checker では同じハーネスが 230 本の食い違いを
出すので、一致はハーネスが鈍いせいではありません。

## F1. モデル側の誤り: ループ本体の `defer` は、ターン終了時に借用中なら拒否される(訂正済み)

最初のモデルは、ループ本体に登録した `defer s.deinit(allocator);` を「ループ内の
consume(`LoopConsume`)」としてだけ扱い、それがターンの終わりに **走る時点で `s` が
借用中か** を見ていませんでした。checker はそこで `Borrowed` を報告します。

```kizu
var s = try string::from_bytes(allocator, "ab");
let v0 = s.as_bytes();
for 0..2 |i1| {
    defer s.deinit(allocator);
}                      // <- 本体の defer がここで走る。v0 はまだ後で読まれる
print(v0);
```

```
error: string error: `String.deinit` cannot run while string is borrowed
```

モデルは `{LoopConsume, UseAfterMove}` を予測し、`Borrowed` を含めていませんでした。

**判断: 実装が正しい。** SPEC §6.3.1 は deferred call の owner について「block exit で
実行する時点で move 済み、deinit 済み、borrow 中なら拒否します」と書いており、
ループ本体も lexical block です。モデルの `checkStmt` の `LoopEnd` に、本体の defer が
走る位置での `borrowedAt` を足しました(`ownership_test.qnt` の
`deferInLoopWhileViewedTest`)。どのプログラムもすでに `LoopConsume` で拒否される
ので、受理 / 拒否は変わらず、変わったのは許す診断クラスだけです。

## 仕様の判断を要しないが、記録しておく観察

* **どの誤りを先に報告するかは SPEC に無い。** 誤りが重なるプログラムで checker は
  1 つ目だけを出します。モデルは違反する規則の集合を予測し、報告がその要素であれば
  一致とみなしています。順序を約束にするなら SPEC が決めることです。
* **`defer` の二重登録は登録の時点で拒否される**(`already has a registered defer
  cleanup`)。SPEC §6.3.1 の文面からは「出口で 2 回目の deinit が consume 済みの owner
  に当たる」とも読めますが、どちらで止めても受理 / 拒否は同じです。モデルは独立した
  クラス `DoubleDefer` として扱っています。
* **`move` 後の借用は `moved value \`s\` was borrowed`** と報告されます(使用は
  `... was used`)。どちらも `UseAfterMove` に分類しています。
