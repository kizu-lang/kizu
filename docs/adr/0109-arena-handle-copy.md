# ADR-0109: `std::arena::Handle<T>` を copy 型にする

Status: 採用

Issue: #1605

## 背景

ADR-0108 で copy 判定は構造から導出されるようになったが、`Handle<T>` は
compiler builtin のため導出の対象外で、user code では move-only のままだった。
SPEC §10 は handle を「arena 内の値を指す opaque な ID」と説明するのに、
`let h2 = h;` や user 関数への 2 回渡し、struct field への格納後の再利用が
すべて move error になる。所有しているのは arena であって handle ではない
ので、この move は何も解放せず何も守らない。実害は「同じ値を 2 箇所から
参照できない」ことに出る: 子を 2 つの親が指す DAG、field に置いた handle の
手元での再利用、handle を受け取る helper の複数回呼び出しが書けない。

## 決定

**`Handle<T>` を copy 型にする。** 判定は types / ownership 両 checker の
`isPlainDataType` に builtin として加え、ADR-0108 の構造伝播にそのまま乗せる。
handle だけを field に持つ struct が copy になり、`Array` の copy element や
`Map` value にも handle が通る。

複製は元と同じ arena 由来(provenance)を引き継ぐ。既知 handle への検査 —
別 arena との取り違え(ADR-0098)、arena `deinit` 後の使用、frame escape —
は複製にもそのまま適用される。安全性は arena の生存が担保しており
(SPEC §10)、handle の個数に依存しない。

## 却下した代案

* **move-only のまま明示的な複製 API(`h.dup()`)を置く**: handle の個数を
  追跡したい場合には意味があるが、arena は削除を持たないため追跡して
  得られるものがない。儀式だけが残る。
* **生の `i64` index に退化させる**: `Handle<Node>` と `Handle<Other>` の
  型区別を失い、handle を導入した理由そのものを捨てる。
