# ADR-0105: `Arena.at` が borrow read accessor(`get` は copy の綴り)

Status: 採用

Issue: なし(accessor 命名の非対称の解消)

## 背景

ADR-0102/0103/0104 を経て、container accessor の命名は役割で分かれていた:
`get` は copy を返す不在 API(`?T`)、`at` / `at_mut` は borrow accessor。
Array と Map はこの表に従うが、Arena だけは borrow を返す read accessor が
`get` を名乗っていた:

| container | copy read | borrow read | borrow write |
|---|---|---|---|
| Array | `get -> ?T` | `at -> ?&T` | `at_mut -> ?&var T` |
| Map | `get -> ?V` | `at -> ?&V` | `at_mut -> ?&var V` |
| Arena | —(owner は copy 不可) | `get -> &T` | `at_mut -> ?&var T` |

`users.at(handle)` と書いてしまう非対称であり、`get` の意味(copy)とも
矛盾していた。

## 決定

1. **`Arena.get(handle)` を `Arena.at(handle)` に改名する。** シグネチャは
   明示的に `&T` とする。Array / Map の `at` が `?` を付けるのは呼び出し側が
   計算した index が不在でありうるからで、handle にその不在は無い —— 名指す
   先が無い handle は壊れた program なので、範囲外 index と同じく trap する
   (ADR-0134)。optional の有無は不在意味論の差で、`at` の意味
   「位置への borrow read」は 3 container で揃う。
2. **`get` は copy accessor 専用の綴りとする。** Arena に copy accessor は
   ない(element は owner)ので、Arena から `get` は消える。
3. **IR op も `arena.get` から `arena.at` に改名する。** op 名は surface
   method を鏡写しにする(`arena.at_mut` と同じ規約)。runtime 記号
   `kizu_arena_get` は `at` / `at_mut` が共有する handle 解決の primitive
   なので改名しない。
4. **`at` の結果は通常の shared borrow と同じ lifetime 規則に従う。** 直接の
   field / method / match read と local binding を許可し、binding は arena を
   最終使用まで borrow する。borrow parameter を根に持つ arena からは `&T` を
   返せるが、local arena を根に持つ結果は frame から escape できない。

## 却下した代替

- **`get` / `get_mut` 命名に寄せる**: Array / Map は copy の `get` と
  borrow の `at` を両方持ち、統合すると copy / borrow の区別が名前から
  消える(原理 7)。
- **`&var` を `&mut` に改名して `_mut` suffix と揃える**: binding の
  mutability keyword は `var`(`var x` ↔ `&var T`)で、型綴りを `&mut` に
  すると言語内の mutability 語が 2 つに割れる。`_mut` は型綴りではなく
  method 命名の慣用 suffix として扱う。
- **`Arena.at -> ?&T` capture 限定に揃える**: 表の形は完全に揃うが、
  不在のない読み取りに毎回 capture の 2 行を課す。しかも optional が表すのは
  不在ではなく取り違えになり、意味が合わない(ADR-0134)。

## 帰結

- 読み取りの綴りが 3 container で `at` に揃い、`users.at(handle)` という
  自然な推測が通る。
- checker 内だけの「borrow-like T」という例外を持たず、signature、call、return、
  local lifetime のすべてを `&T` の既存規則でレビューできる。
- breaking change だが、`Arena.get` の使用箇所は examples / tests のみ。
- 却下判断により `at` / `at_mut` / `&var` / `_mut` suffix の組は言語の
  確定綴りになる。
- この ADR を書いた時点の決定 1 の根拠は「handle の存在は静的 provenance 検査が
  保証する」だったが、それは成立していなかった。既知の出自どうしの取り違えしか
  止まらず、field や container を経由した handle は素通りしていた。いまの根拠は
  静的な保証ではなく、取り違えを実行時に止めること(ADR-0134)である。
