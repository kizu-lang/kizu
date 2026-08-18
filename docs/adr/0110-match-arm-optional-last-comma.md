# ADR-0110: match の最後の arm の comma を省略可にする

Status: 採用

Issue: #1616

## 背景

SPEC §6.12 は「すべての `match` arm は terminal comma を必須」としていた
(ADR-0107 でも維持を確認)。comma を separator ではなく terminator として
扱い、statement の `;` 必須(§6)と同じ側に置く設計だった。

一方、§6 の一般規則は「comma-separated list は末尾カンマを許容」で、
struct literal・引数・type parameter は末尾 `,` を省略できる。
match だけが最後の要素の後ろに区切りを要求する形になり、
`match l { Low => "low", High => "high" }` のような 1 行の match が
`expected `,` after match arm` で落ちていた。複数行では自然に `,` を
書くため表面化せず、1 行にまとめたときだけ現れる。診断も「最後の arm
にも要る」ことを伝えられていなかった。

## 決定

最後の arm の `,` を省略可にする。ADR-0107 の「terminal comma は必須の
まま」の項を覆す。

- arm の区切りは他の comma-separated list と同じ規則になる:
  途中の arm は `,` で区切り、最後の arm の `,` は書いても省いてもよい。
- arm body の終端は「arm の comma、または match を閉じる `}`」になる。
  arm body の文法(expression / `return` / block、ADR-0093・ADR-0107)は
  変えない。
- parser は match arm の delimiter を struct field などと同じ
  「comma または閉じ `}`」の受理(consumeListDelimiter)に揃える。
  arm の間の `;` は従来どおり error のまま。

区切りの省略可否は構文ごとではなく言語全体で 1 つの規則にする。
どちらに揃えるかより、揃っていることを優先した(原理 7 の逆写し:
意味が同じ「brace 内の comma 区切り list」に同じ規則を課す)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 常に `,` を必須のまま維持し、診断だけ改善 | 実害は減るが「同じ形の list に規則が 2 つ」が残る |
| struct literal 側を comma 必須に揃える | 既存コードを壊す方向の統一。得るものがない |

## 影響

- parser: parseMatchArms の delimiter を consumeListDelimiter に変更
- SPEC §6.12 更新
- fmt は従来どおり複数行の arm に `,` を出力する(末尾 `,` は引き続き valid)
