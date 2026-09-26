# ADR-0151: C の callback は呼び出し規約を型で名乗る

## 背景

C の library は callback を関数 pointer で受け取る(`qsort` の比較関数、event
handler、`void *` と組の userdata)。Kizu には `fn(...) -> T` があるが、それは
Kizu 関数の address で、`extern "c" fn` の引数に置けなかった。C の関数の名前は
`fn(...)` 型の値として検査を通り、`unsafe` なしで C を呼べる穴にもなっていた。

## 決定

C の呼び出し規約を持つ関数の address は `extern "c" fn(...) -> T` という別の型に
する。`extern "c" fn` と `export "c" fn` の名前がこの型の値で、`fn(...)` の値には
ならない。

* 引数と結果は C が名指しできる型だけ。型自身も C が名指しできる型に加わるので、
  `extern "c" fn` の引数・結果と `extern "c" struct` の field に置ける。
* 呼び出しは直接の extern 呼び出しと同じく `unsafe`(`extern_call`)が要る。
  指す先が C かもしれない。
* `void *` で受け渡す API のために、raw pointer との間は `cast` で行き来する
  (`ptr_cast`)。null 可能性は両辺で揃える。
* 関数 pointer は null にならないので、`?fn` / `?extern "c" fn` は null を不在に
  使う(ADR-0133 の niche)。`?extern "c" fn` がそのまま C の null 可能 callback になる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `fn(...)` をすべて C 呼び出し規約にする | 今の `fn` は C ABI に載らない型(view、owner、error union)も渡す。型で分けないと境界の検査ができない |
| closure と自動 trampoline で userdata を隠す | Kizu は closure を持たない。userdata は C API の引数として source に見えるほうがよい |
| `ptr_from_fn(f)` のような専用 builtin | raw pointer 間の `cast` と同じ操作。unsafe の種類も同じ `ptr_cast` で足りる |
| 公開しない C 呼び出し規約の関数(`callconv` 相当) | 今は `export "c"` で足りる。symbol の衝突が問題になってから考える |
