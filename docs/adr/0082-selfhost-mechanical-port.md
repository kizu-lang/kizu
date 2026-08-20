# ADR-0082: selfhost は Go 実装を機械移植し、shipping 経路を一つに保つ

Status: 採用

Supersedes: selfhost を独立に育てる判断と、言語安定まで再開しない判断

## 背景

自己コンパイル backend を削除し、selfhost は 164,011 行から 73,260 行に
なった。それでもなお、次の状態が残っていた。

| | 行数 |
| --- | ---: |
| selfhost | 73,260 |
| Go 実装(test 除く) | 42,738 |
| Go テスト | 29,561 |

**第二実装が参照実装より大きく、しかも機能は劣っていた。** `cmd/kizu` の
テストファイル 52 個のうち 48 個が selfhost に依存しており、テスト労力の大半が
言語ではなく第二実装に向いていた。言語機能を足すたびに 2 回書く必要があり、
comptime はその途中で止まっていた(Go は実装済み、selfhost は check 段のみ)。

一日で見つかった不具合の質も、この構造を示していた。手書き LLVM がソースと違う
意味論を持ち、存在しない entry を叩く gate が `err != nil` を成功と誤認して
緑を保ち、`stage2` の挙動がソースと乖離していた。いずれも**二重実装そのものが
生む失敗モード**である。

失敗の原因は Kizu で compiler を書くことではなく、別の構造と判断を持つ実装を
長く並走させたことだった。一方、現在は multi-module、owner container、recursive
AST、error union、filesystem、path、process、I/O が Kizu source から使える。
selfhost に必要な能力を先に予想して足す段階から、Go 実装を実際に移して不足を
見つける段階へ進める。

移植は clean-room rewrite にしない。Go の selfhost で成功した方法と同じく、現在
動いている compiler の構造と挙動を保つ機械移植にする。AI は変換量を増やせるが、
ownership や package 境界を file ごとに発明させれば、以前の第二実装と同じ分岐を
高速に作るだけになる。変換規則と field ごとの ownership を先に固定する。

## 決定

cutover までは `internal/` + `cmd/kizu` を唯一の shipping 実装とし、Kizu source の
compiler は `compiler/` に non-shipping の移植先として置く。移植先は Go の構造と
挙動を保つ機械移植であり、独立した言語仕様や architecture を持たない。

移植規則と field ごとの ownership 判断を先に共有し、その範囲内だけを AI で並列化
する。実際の規則と完了条件は一時文書 `docs/selfhost-porting.md` と
`docs/selfhost-ownership.tsv` が持つ。

cutover は shipping 経路の切り替えと Go compiler source の削除を同時に行う。
移植後に Go fallback、side-by-side oracle、移植専用の文書や workflow を残さない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| compiler を Kizu で一から再設計する | 移植と architecture 変更の差が見えず、Go 版との挙動差を説明できない |
| 汎用 Go-to-Kizu transpiler を維持する | Go の interface、GC、slice、pointer semantics を再実装する恒久的な第二 toolchain になる |
| Go と Kizu を長期間ともに shipping する | 言語機能を二度実装し、fallback が片方の欠陥を隠した以前の失敗を繰り返す |
| file ごとに規則なしで AI へ一括変換させる | ownership、module 境界、diagnostic の判断が file ごとに分岐する |
| 生成 IR や内部 AST の文字列一致を gate にする | 実装の形を固定し、挙動が正しくても変更できない。examples と behavior test が契約を持つ |
