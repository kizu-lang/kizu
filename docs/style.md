# Kizu コードスタイル

本書は Kizu でコードを書くときの慣習です。`SPEC.md` は言語の定義、
`docs/principles.md` は設計原理、本書はそれらの下で API の形をどう選ぶかの
指針です。std は本書に従い、ユーザーコードにも同じ形を勧めます。

## 不在・失敗・バグの使い分け

結果が「無い」ことをどう返すかは 3 分類で決めます。

| 分類 | 意味 | 形 |
| --- | --- | --- |
| 期待される不在 | 「無い」がその問いへの正常な答え | `?T` |
| 理由のある失敗 | 操作が失敗し、理由が呼び側の判断材料になる | `E!T` |
| 契約違反 | 呼び側のバグ。正常系に「無い」は存在しない | `_or_panic` 系 |

判定は次で行います。

- 呼び側の典型反応が **branch / `orelse`** なら optional、**`try` で伝播**なら
  error。
- 失敗の種類が「無い」の 1 つだけなら optional。複数あって区別が意味を持つなら
  error。
- 検索・問い合わせの miss(map の lookup、環境変数、iterator の尽き)は
  optional。外界の失敗(I/O、OutOfMemory)や入力の破損は error。
- 操作は失敗しうるが、成功しても無いことがあるなら `E!?T` に合成する
  (fallible stream の `next() -> E!?u8`)。

番兵値(`-1`、空 slice の流用)で不在を表しません。不在は型に見せます
(principles 1: 傷を隠さない)。

panic 系は「呼び側が事前条件を保証済みで、検査コストを払わない」ことを名前で
grep できるようにする明示語です(principles 3)。既定は検査付きの `?T` / `E!T`
で、`_or_panic` は添え物です。

std の例:

- `map::get` の miss、`array::pop` の空、`process::env` の未定義 → optional。
- `fs::read_file` の I/O 失敗、`array::append` の OutOfMemory → error。
- `array::get_or_panic` → 添字を呼び側が保証するときの明示的な panic。
