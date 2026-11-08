# unsafecap Quint モデル: 食い違いの記録

`spec/unsafecap/` のモデル(SPEC §12 の `unsafe` マーカー規則)と、`kizu check` の
判定(`internal/types` の marker 検査、診断の種類名は `internal/unsafecap`)を
突き合わせた結果です。範囲と方法は `spec/unsafecap/README.md` にあります。
checker のコードは変えていません。どちらが正しいかは仕様判断なので、ここでは決めません。

## 結論(2026-11-08、`verify/unsafecap-quint` の基点 a028e69a)

* **安全側の重大所見が 1 件あります(F1)。** `unsafe struct` の field を `&var` で
  貸すと、`unsafe` 無しで、しかも宣言 file の外からでも書き換えられます。SPEC §12 が
  閉じると書いている穴そのものです。Go 実装と `compiler/` の両方に同じ穴があります。
* 安全側(拒否はする)の診断の食い違いが 2 件あります(F2, F3)。
* それ以外の生成プログラムでは、受理 / 拒否と診断クラスがすべてモデルと一致しました
  (規模は README の「規模と結果」)。
* 「実装が許可してはいけないものを許可する」側の食い違いは F1 だけです。`spec` の
  全 6435 本で 0 件、`&var b.n` を生成に足した `specBorrow` の全 6406 本で
  「モデルは拒否、checker は受理」が 76 本、すべて `field_borrow` を含みます。
* checker に 1 か所ずつバグを入れた 24 種は、すべて生成 fixture の再生だけで落ちます
  (末尾「変異による確認」)。

---

## ⚠️ F1. 【重大・実装が許可してしまう】`&var` で貸した `unsafe struct` の field は、marker 無しで、別 file からも書き換えられる

### 最小再現(同じ file、marker 無し)

```kizu
extern "c" fn source() -> ptr<i64>

/// data points to n live i64 values.
unsafe struct Buf {
    data: ptr<i64>,
    n: i64,
}

fn setn(x: &var i64) -> void {
    x = 4096;
}

fn grow(b: &var Buf) -> void {
    setn(&var b.n);          // check: ok  —  b.n = 4096; なら「requires `unsafe`」
}

fn main() {}
```

同じ穴の別の綴り(どれも `check: ok`):

```kizu
let r = &var b.n;            // let で借りて
r.* = 4096;                  // 書く

b.inner.bump();              // field に対する `fn (self: &var Inner) bump()` の呼び出し
```

### 最小再現(別 file、`unsafe` を付けても拒否されるはずのもの)

```
src/buf/buf.kizu     /// ... unsafe struct Buf { data: ptr<i64>, len: usize }
src/buf/other.kizu   fn setu(x: &var usize) -> void { x = 7; }
                     fn poke(b: &var Buf) -> void { setu(&var b.len); }   // check: ok
```

同じ `other.kizu` に `unsafe b.len = 2;` と書くと
`write to field \`xf::buf::Buf.len\` is confined to its declaration file` で拒否されます。

### marker を書くと「未使用」になる

```kizu
// SAFETY: ...
unsafe setn(&var b.n);       // error: `unsafe` covers no operation that needs it
```

SPEC に従って marker を書く書き手は拒否され、書かない書き手は通ります。

### SPEC の該当箇所

* §12「`unsafe struct` の構築とfieldへの書き込みは、その宣言があるfileの中だけに
  閉じます。別fileでは `unsafe` を付けても拒否します。これにより、不変条件を作り
  変えられる監査範囲はmoduleの大きさにかかわらず1fileに固定されます。」
* §12 ルール「`unsafe struct` の構築と field への書き込みには `unsafe` が要る」
* §12 の種類表「`struct_invariant` | `unsafe struct` の構築 / field write」
* ADR-0089 §4 は、`unsafe` が 1 文字も無い `set_len(b: &var Buf, n: usize) { b.len = n; }`
  が `read` の正しさを壊せることを `unsafe struct` 導入の理由に挙げています。
  `setn(&var b.n)` はその `set_len` を、`unsafe struct` の下でもう一度書けるようにします。

### どこで起きているか

両実装とも、`struct_invariant` を課すのは **構築** と **代入文の左辺の field** だけです。

* Go: `internal/types/unsafe.go` の `requireUnsafeStructInvariant` の呼び出し元は
  struct literal(`checkStructLiteralExpr`)と代入先(`checker.go` の field 代入)のみ。
  `&var` の対象は `checkBorrowTargetShape`(`checker.go`)が「local か field path」
  であることしか見ません。
* `compiler/`: `require_unsafe_struct_invariant` の呼び出し元は
  `body_expression_access.kizu`(構築)と `body_statement_let_assign.kizu`(代入先)のみ。
  `check_borrow_target_shape`(`body_borrow.kizu`)は Go と同じ形の検査です。
  selfhost binary はこの環境で build できなかった(clang 無し)ので、`compiler/` 側は
  ソースを読んでの確認です。`KIZU_MBT_BIN` で当てれば `TestUnsafeModelKnownDivergences`
  がそのまま確かめます。

### モデルでの再現

`spec/unsafecap/unsafecap.qnt` の `asImplemented`(`&var b.n` を書き込みとみなさない、
つまり今の checker の振る舞い)では、`quint run --main asImplemented --invariant
invariantAudited` が数百ミリ秒で反例を出します。SPEC どおりの `specBorrow` では
出ません。`internal/unsafecap/mbt_test.go` の `mbtKnownDivergences` が 3 形
(同 file 無印、別 file 無印、同 file 有印)を固定しており、直ると test が落ちて
既知形へ移すよう促します。

### 判断を要すること(決めていません)

* `&var` で unsafe struct の field path を貸すこと(method の receiver としての暗黙の
  `&var` を含む)を field write とみなし、marker と file 境界を課すか。
* それとも、unsafe struct の field を `&var` で貸すこと自体を禁じるか。
* `&` での共有の貸し出しは読みなので、どちらでも対象外のはずです。

---

## F2. 【安全側・診断】`let r = &var p.*;`(marker 無し)は missing marker でなく形の誤りとして拒否される

```kizu
fn g(p: ptr<i64>) -> i64 {
    let r = &var p.*;
    print(r.*);
    return 0;
}
```

```
error: type error: a raw pointer's value is borrowed by an `unsafe` binding: `let v = unsafe &var p.*;`
```

SPEC §12 は「`unsafe` は `p.*` と同じ `ptr_deref`」と書いているので、モデルは
`unsafe.missing_marker`(help が `ptr_deref` を名指す)を予測します。checker は
`unsafe &var p.*` を 1 つの形として認識するため、marker が無いと形の誤りになります。
拒否はするので安全性の問題ではありません。ハーネスはこの文面を `Missing:ptr_deref`
に分類しています(`mbtClassify`)。

## F3. 【安全側・診断】marker を 2 つ重ねた `let` 初期化子 / `defer` は、未使用 marker でなく形の誤りになる

```kizu
// SAFETY: ...
let v = unsafe unsafe view_from_ptr(p, 1);   // `view_from_ptr` must be bound with `let name = view_from_ptr(p, count)`
// SAFETY: ...
let r = unsafe unsafe &var p.*;              // a raw pointer's value is borrowed by an `unsafe` binding
// SAFETY: ...
defer unsafe unsafe ev();                    // defer expects a void call
```

SPEC §12「内側の `unsafe` が唯一の操作を覆っている場合、外側のマーカーは未使用になる」
から、モデルは `unsafe.unused_marker` を予測します。1 つ目の文面は `let` で束縛している
のに「`let` で束縛せよ」と言うので、読み手には分かりにくいはずです。生成器はこの 3 形で
marker を 1 つまでに絞り、2 重の形は `mbtKnownDivergences` で固定しています。

---

## 仕様の判断を要しないが、記録しておく観察

* **外側の `unsafe` は、式の中の block の文まで覆う。**
  `unsafe if c { let t = ptr_read(p); t } else { 0 }` は、内側の文に marker も
  `// SAFETY:` も無いまま受理されます。§12「覆った式の中の**すべて**の未証明操作を
  覆います」の文面どおりで、モデルも同じに扱っています。一方 `// SAFETY:` は文単位で
  「外側の文の comment は内側の文には届かない」ので、内側の文の未証明操作の理由は
  外側の文の comment にしか書かれていないことになります。意図どおりかは SPEC が決める
  ことです。
* **`fn() -> T` を `unsafe fn() -> T` の位置に渡せない。** 安全な関数を unsafe な
  関数 pointer 型に束縛すると型不一致になります(`let f: unsafe fn() -> i64 = sf;`)。
  逆向き(unsafe fn を `fn` 型に、extern を `fn` 型に)の拒否はモデルの
  `noLaundering` が要求するもので、そちらは一致しています。安全な方向の部分型が無いのは
  厳しいだけで、安全性の問題ではありません。モデルは型の一致だけを受理します。
* **どの誤りを先に報告するかは SPEC に無い。** モデルは違反する規則の集合を予測し、
  checker の最初の報告がその要素なら一致とみなします(ownership と同じ)。
* **`Function` static parameter は std 専用。** 名前で関数を渡す経路(callee の
  `unsafe fn` を `Function` 引数経由で呼ぶ)は利用者が書けないので、範囲外にしました。

---

## 変異による確認

生成テストが checker の誤りを実際に捕まえるかを、`internal/types` を 1 か所ずつ
書き換えて `go test ./internal/unsafecap` を回し、元に戻して確かめました(手元のみ、
書き換えは checked-in していません)。`known` は `TestUnsafeModelKnownShapes`、
`traces` は `TestUnsafeModelTraces` 単独の結果です。

| 変異 | known | traces(328 本) |
| --- | --- | --- |
| `unsafe fn` の本体を使用済みの marker で覆う | 落ちる | 49 本食い違い |
| 未使用 marker を報告しない | 落ちる | 16 |
| `// SAFETY:` を要求しない | 落ちる | 16 |
| file 境界を marker の要求に替える | 落ちる | 3 |
| `unsafe fn` pointer 経由の呼び出しを課金しない | 落ちる | 9 |
| `extern "c" fn` pointer 経由の呼び出しを課金しない | 落ちる | 17 |
| unsafe fn の値を安全な `fn` 型にする(laundering) | 落ちる | 7 |
| `unsafe struct` の構築を課金しない | 落ちる | 3 |
| `unsafe struct` の field 書き込みを課金しない | 落ちる | 25 |
| extern の直接呼び出しを課金しない | 落ちる | 8 |
| `unsafe fn` の直接呼び出しを課金しない | 落ちる | 8 |
| marker の無い `ptr_read` を受理 | 落ちる | 7 |
| 代入先の marker が値も覆う | 落ちる | 18 |
| 11 種それぞれ「その種類だけ marker 無しで受理」(`requireUnsafeCapabilityAt`) | 11 種とも落ちる | 4〜16 |

### 見つかったハーネスの穴(直した)

引き継いだ時点の fixture(285 本)では、`ptr_read` を marker 無しで受理する 2 つの
変異が `TestUnsafeModelTraces` を通っていました。生成器が「marker の無い `ptr_read`
だけが誤り」のプログラムを全 5544 本中 0 本しか作っておらず、`ptr_read` を含む誤りは
いつも他の誤りと重なっていたためです。比較は「checker の最初の報告が予測集合に入るか」
なので、飛ばされた規則が他の誤りの陰に隠れます。生成器に `slip`(修復した文から marker
を 1 か所落とす)を足し、11 種すべてに単一の missing marker のプログラムが入るように
しました(spec/unsafecap/README.md「モデルベーステスト」)。

この比較の仕方そのもの(複数の誤りでは最初の 1 つしか見ない)は残っています。報告順は
SPEC に無いので、モデル側で順序を決めることはしていません。
