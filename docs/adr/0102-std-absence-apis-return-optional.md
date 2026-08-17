# ADR-0102: std の不在 API は `?T` を返す

Status: 採用

Issue: なし(ADR-0101 再評価条件「std の不在系 API を `?T` に移行するとき」
の履行)

## 背景

ADR-0101 で `?T` が入り、不在を error union に相乗りさせる理由が消えた。
しかし std には `?T` 以前の形が残っていた: `mem::byte_at` / `mem::slice` /
`map::get` / `array::pop` / `array::get` は miss を error で返し、
`process::env` は EnvNotFound を返し、`bits::last_slash_before` は `-1`
番兵を返していた。どれも原理 7(意味が違うなら違うものにする)と原理 1
(番兵は不在を数値で隠す傷)に反する。

移行にはまず「optional か error か」の基準が要る。Zig / Rust の std は
同じ結論に収束しており(検索 miss は optional、理由のある失敗は error、
契約違反は panic)、kizu もこれを採る。

移行を始めると ADR-0101 の element 制限(scalar / enum のみ)に
`map::get -> ?V`(generic)、`process::env -> ?[]u8`(view)が阻まれ、
`dirname` の miss 早期 return は capture の入れ子を強いた。制限を守った
まま形だけ寄せる中間案は 2 度の breaking change になるので、制限側を
解いた。

## 決定

1. **基準を `docs/style.md` に置く**。期待される不在は `?T`、理由のある
   失敗は `E!T`、契約違反は `_or_panic` 系。番兵値で不在を表さない。
   これは原理ではなくコードスタイル(API の形の慣習)として文書化する。
2. **`?T` の element 規則を `!T` の成功型規則に揃える**(原理 6: 新しい
   区別を作らない)。parse できる型なら element にでき、閉じるのは
   `??T` / `?!T`(optional を包めるのは error union だけ)のみ。generic
   実体化が作る綴り(`Array<!i64>` の `pop()` = `?!i64`)も置換後の
   再検査で同じ規則により拒否する。
3. **owner / view payload は「生まれた場所で消費」**。payload の階級は
   型が運ぶ: owner payload は capture / `orelse` の結果に消費義務が付き、
   view payload は view の借用規則に従う(`?[]u8` は `![]u8` と同じく
   view-carrying)。optional の中の payload は move / borrow 追跡から
   見えないので、owner / view を包んだ optional の let / var 保存と
   引数渡しは拒否する。保存できるのは copy element の optional だけ。
4. **`orelse return / break / continue`(guard 形)を追加する**。null の
   arm が enclosing 関数・loop を離れるので、式自体は常に payload を
   生む。miss の早期 return を capture の入れ子なしで書ける
   (`let slash = last_slash_before(p, end) orelse return ".";`)。
   exit の義務は statement 形と同じ(return は errdefer / owner 検査、
   break / continue は loop 内配置検査)。
5. **std を移行する**:
   - `mem::byte_at -> ?u8`、`mem::slice -> ?[]u8`(`mem::Error` は削除)
   - `array::pop -> ?T`、`array::get -> ?T`(`Error::Empty` は削除。
     `at` / `at_mut` は `!&T` のまま — 下記)
   - `map::get -> ?V`(`Error::Missing` は削除)
   - `process::env -> ?[]u8`(`EnvNotFound` / `InvalidEnvName` は削除。
     lookup できない名前も「値が無い」。`env_or_empty` は
     `env(n) orelse ""` で書けるので削除)
   - `bits::last_slash_before -> ?i64`(`-1` 番兵を廃止)
6. **runtime の境界**: array / map 系は C が nullable pointer を返し
   LLVM emitter が `{i8, T}` を組む(C 変更なし)。`process_env` は
   C 側に `KizuOptSliceU8` を追加し、hosted out-pointer ABI を optional
   結果にも適用する。

## 見送り

- **`array::at` / `at_mut` の `?&T` 化**: borrow payload の capture は
  「branch scope に限定された borrow の活性化」という新しい形で、
  try 形に結びついた 4 箇所の recognizer の再設計が要る。miss を値で
  受けたい読み出しは `get` / `get_or_panic` が既に持つので、急がない。
  `?&T` が必要になったときに capture-borrow として設計する。
- **struct field / union payload / static argument / `&?T` の解放**:
  storage 位置は決定 3 の「生まれた場所で消費」と衝突する。copy element
  に限って開ける余地はあるが、必要が出てから。

## 却下した代替

- **element 制限を守った第 1 弾だけで止める**: 却下。`map::get` /
  `array::pop` という最頻出 API が移行できず、基準(style.md)と実装が
  矛盾したまま残る。
- **owner / view payload の全面 flow 追跡**: 却下。optional 内部の
  payload を move 追跡に見せる大工事になる。「生まれた場所で消費」の
  制限で健全性は閉じ、std の全用途はこれで書けた。
- **基準を `docs/principles.md` に置く**: 却下。原理は設計判断の根拠、
  これは API の形の慣習であり、層が違う。
