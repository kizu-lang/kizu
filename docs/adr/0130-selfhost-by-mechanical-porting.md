# ADR 0130: selfhost は機械移植で作り、Go は seed として残す

## Status

Accepted.

## Context

Kizu compiler を Kizu 自身で書くにあたって、Go 実装(`internal/` + `cmd/kizu`)から
どう移るかを決める必要がありました。移植は完了し、release する binary は
`compiler/` から build した Kizu compiler です。Go 実装はそれを build する seed で
あり、両実装の出力を突き合わせる oracle でもあります。

この ADR が残すのは、その進め方を選んだ理由と、却下した案です。移植の手順そのもの
(work unit の切り方、review loop、module ごとの進捗)は移植が終わった時点で
役目を終えたので消しました。移植中に見つかった language / std の不足は
`docs/language-gaps.md` が持ちます。

## Decision

Go 実装の挙動、処理順、diagnostic、package 境界を保ったまま機械的に移す。移植と
同時に language feature、compiler architecture、optimization、public API を
再設計しない。

内部表現(node の保持方法、error 回復時の placeholder、handle の形)は、render・
diagnostic・処理順が Go と同じなら Kizu-native に変えてよい。pass 構成と
diagnostic 文面は変えない。

両実装が食い違ったときの判断は Kizu 側が先に来る。Go は追従するか、追従できない
ならその gate を落とす。

利用者が通る経路は 1 つに保つ。Go を fallback にも feature flag にも戻さない。

### 却下

| 案 | 却下理由 |
| --- | --- |
| compiler を Kizu で一から再設計する | 移植と architecture 変更の差が見えず、Go 版との挙動差を説明できない |
| 汎用 Go-to-Kizu transpiler を維持する | Go の interface、GC、slice、pointer semantics を再実装する恒久的な第二 toolchain になる |
| Go と Kizu を長期間ともに shipping する | 言語機能を二度実装し、fallback が片方の欠陥を隠した以前の失敗を繰り返す |
| file ごとに規則なしで AI へ一括変換させる | ownership、module 境界、diagnostic の判断が file ごとに分岐する |
| 生成 text を golden として pin し、それを契約にする | 実装の形を固定し、挙動が正しくても変更できない。契約は `examples/` と `tests/behavior/` が持つ。両実装の出力を突き合わせる差分 corpus(`compiler/tests/`)は別物で、これは採用している — golden と違い、Go 側を変えれば `-update` で両方が一緒に動く |

## Consequences

- 逐語訳なので、Go が「文字列も pointer も自由に共有できる」前提で書いた場所は、
  所有権のある言語では copy か再検索になる。移植版が Go より遅い分の主な出どころが
  これで、cutover 後に表現を設計し直せば縮む
- `compiler/tests/` の差分 corpus は両実装が共有する。Go 側の挙動を変えたら
  `-update` で corpus を再生成し、移植側も同じ commit で追従させる
- Go をいつ削除するかは別の判断で、削除の判断と、その後 Kizu compiler を build する
  seed をどう供給するかの判断は同じ 1 つになる
