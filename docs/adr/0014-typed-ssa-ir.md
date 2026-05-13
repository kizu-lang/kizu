# ADR-0014: Kizu IR は typed SSA IR にする

Status: 採用

## 背景

Kizu は最終的に高い性能を出しやすい compiler backend を持ちたい。
また、ビルド時間とキャッシュサイズを制御しながら、最適化しやすい IR が必要である。

候補には basic block IR、SSA IR、stack-machine IR、tree IR がある。

## 決定

Kizu IR は typed SSA IR にする。

理由:

- 最適化しやすい
- データ依存が見えやすい
- dead code elimination しやすい
- constant folding しやすい
- LLVM IR に lowering しやすい
- move / borrow 検査後の値状態を表現しやすい

ただし、初期実装は minimal typed SSA IR にする。

初期の最適化は次に限定する。

- constant folding
- dead code elimination
- simple copy propagation

## キャッシュ方針

SSA IR は強力だが、巨大な中間生成物を無制限に保存しない。

- IR artifact は必要なものだけ保存する
- debug dump は明示 opt-in にする
- cache key は input hash、compiler version、target、optimization level を含める
- cache size は上限を持つ

## 影響

- Phase 8 は typed SSA IR を実装する
- Phase 9 の LLVM IR backend は typed SSA IR から lowering する
- IR dump command を将来追加する
- SSA の phi node と control-flow lowering が実装課題になる
