# ADR-0104: `Map.at` / `at_mut` は borrow optional `?&V` を返す

Status: 採用

Issue: なし(ADR-0103 再評価条件「`map.at(key) -> ?&V` へ広げたくなったとき
recognizer を container 種別に general 化するか判断する」の履行)

## 背景

ADR-0103 で `?&T` は capture 条件限定の borrow payload として言語に入り、
`Array.at` / `at_mut` がその唯一の生産者だった。map には borrow access が
なく、value の in-place 更新は `get` → `insert` の 2 回 lookup を強いる。
また value type の copy 限定を外す将来の一歩(non-copy value)は、
copy out できない value を borrow で読む経路を前提とする。

機構は揃っていた。capture-borrow の ownership 機構は ADR-0100 / 0103 で
入り、runtime の `kizu_map_get` は最初から nullable value pointer を返して
いる。

## 決定

1. **`map.at(key) -> ?&V`、`map.at_mut(key) -> ?&var V`**。key があれば
   value borrow、なければ `null`。
2. **recognizer を container 種別に general 化する**。at/at_mut capture
   条件の recognizer は receiver 型で分岐する: `Array` は i64 index、
   `Map` は []u8 key。それ以外の規則(capture 条件限定、保存・`orelse`・
   signature の拒否、`at_mut` の mutable binding 要求)は ADR-0103 と同一。
3. **mutable borrow 中の map read は拒否する**。`at_mut` capture が map を
   mutable に borrow している間、`get` / `key_at` / `contains` / `len` は
   Array の read と同じく待つ。`insert` / `deinit` は従来どおり任意の
   borrow で拒否(rehash が borrow の指す storage を動かすため)。
4. **runtime は変更しない**。`map.at` は `map.get` と同じ `kizu_map_get`
   を呼び、load せず nullable pointer を `{i8, ptr}` に分岐なしで包む。

## 帰結

- copy value の in-place 更新が 1 回の lookup で書ける:
  `if counts.at_mut(key) |n| { n.* = n.* + 1; } else { try counts.insert(key, 1); }`。
- value type の copy 限定はこの ADR では変えない。non-copy value の解禁は
  この borrow access を前提に後続 ADR で扱う。
- `?&V` の一般開放はしない(ADR-0103 決定 3 のまま)。

## 却下した代替

- **`get_mut(key) -> ?V` のような copy 返し**: in-place 更新にならず、
  non-copy value への道にもならない。
- **`Box` / user 型への同時拡張**: `?&T` の生産者は container の std
  method に閉じたまま様子を見る。必要になったとき再評価する。

## 再評価の条件

- non-copy value を map に入れたくなったとき(`get` の copy out が書けなく
  なるので、`at` / `at_mut` を既定の access にする)。
- `&var` param 経由の receiver で `at_mut` を使いたくなったとき
  (現在は Array / Map とも local mutable binding 限定)。
