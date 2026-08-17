# ADR-0103: `array::at` / `at_mut` は borrow optional `?&T` を返す

Status: 採用

Issue: なし(ADR-0102 見送り「`?&T` が必要になったときに capture-borrow
として設計する」の履行)

## 背景

ADR-0102 で std の不在 API は `?T` に揃ったが、`at` / `at_mut` だけが
`!&T` のまま残り、範囲外(= 期待される不在)を error union で返していた。
これは style.md の基準(期待される不在は `?T`)と矛盾する。

実害もあった。non-copy element の iteration が書けない:
`get -> ?T` は copy element 限定で、`at` は try 形の let-initializer
限定なので `while ... |x|` を駆動できず、`while i < a.len()` +
「失敗しないはずの `try`」という形を強いていた。

一方、前提は 0102 当時から変わった。container view capture
(`map.key_at` の `?[]u8`)で「capture 条件が container を借用し、
capture の最終使用まで mutation を待たせる」機構が ownership checker に
入り、borrow payload の capture はその上の一歩になった。

## 決定

1. **`array.at(i) -> ?&T`、`array.at_mut(i) -> ?&var T`**。範囲内なら
   element borrow、範囲外なら `null`。`Error::OutOfBounds` は `at` から
   消える(`set` / `truncate` は残る)。
2. **borrow optional は capture 条件限定**。`if array.at(i) |elem|` /
   `while array.at(i) |elem|` の capture が element borrow そのものに
   なり、旧 `let elem = try array.at(i)` の positional recognizer を
   条件位置に移す。capture の scope の間 array は borrow され
   (`at` は shared、`at_mut` は mutable)、解放は capture の最終使用。
3. **それ以外の `?&T` の出現はすべて拒否**。binding への保存(ownership
   の stored-optional 拒否)、`orelse`、user 関数の param / return
   signature(types 層で拒否、std wrapper のみ例外)。element borrow が
   array の変更より長生きする経路を positional に閉じる。
4. **runtime は変更しない**。`kizu_array_get` の nullable pointer を
   LLVM emitter が `{i8, ptr}` に分岐なしで包む。旧 `!&T` の
   error-union 経路は削除。

## 帰結

- non-copy element の iteration が copy と同じ形になる:
  `while tokens.at(i) |token|`。
- try 形 let-initializer(`let x = try a.at(0)`)は削除。単発 access も
  capture で書く。breaking change だが、`at` の使用箇所は examples だけ
  だった。
- `?&T` は ADR-0101 の「optional を包めるのは error union だけ」の
  例外ではなく、§7 の payload 階級に「borrow payload = capture 条件
  限定」として加わる。

## 却下した代替

- **`!&T` を維持**: 範囲外は期待される不在で、error にする理由がない。
  style.md の基準と std 自身が矛盾し続ける。
- **`?&T` を一般の型として解放**(保存・受け渡し可): optional 内部の
  borrow は move / borrow 追跡から見えず、全面 flow 追跡が要る。
  0102 決定 3 の「生まれた場所で消費」と同じ理由で positional 制限を採る。

## 再評価の条件

- `map` の値 borrow(`map.at(key) -> ?&V`)や `Box` / user 型へ
  borrow optional を広げたくなったとき、recognizer を container 種別に
  general 化するか判断する。
