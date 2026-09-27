# ADR-0152: raw pointer の指す値は local borrow として借りる

## 背景

C host が opaque pointer で持つ Kizu の値は raw memory にある。触るには
`ptr_read` で値ごと取り出し、処理し、`ptr_write` で書き戻すしかなかった。
関数ごとに同じ往復が並び(原理 10)、取り出した値は owner なので、
`ptr_write` の後に `mem::leak` で帳尻を合わせる必要もあった。

## 決定

* `let v = unsafe &var p.*;` / `let v = unsafe &p.*;` で、raw pointer の指す値を
  local borrow として束縛する。綴りは既存の local borrow(`let r = &var x;`)と
  `p.*` の組み合わせで、新しい名前も新しい unsafe の種類(`ptr_deref`)も足さない。
  raw view(`mut_view_from_ptr`)と同じく、どの binding も借りていないので返せず
  field にも入らない。
* `ptr_write(p, value)` の owner は `move` で渡す。call の owner 引数と同じ規則で、
  pointee が持ち主になるので `mem::leak` は要らない。

`&var p.*` は pointer をそのまま `&var T` にするので、束縛を通した書き込みは
pointee に届く。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `mut_ref_from_ptr(p)` のような builtin | 既存の borrow 構文で綴れる。名前を増やさない(原理 6) |
| `with_ptr(p) \|v\| { ... }` の block | closure と同じ形。Kizu は closure を持たない |
| `p.*` への method 呼び出しだけを許す | 一度に 1 操作しかできず、複数の操作は結局読み書きの往復に戻る |
| call の引数に `&var p.*` を直接書く | 束縛 1 行で足りる。argument 経路の lowering を増やさない(原理 11) |
