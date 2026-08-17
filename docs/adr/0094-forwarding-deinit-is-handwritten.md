# ADR-0094: 転送 deinit は手書きする(`= fields;` を削除)

Status: 採用

Issue: #1559

Supersedes: ADR-0091 決定 2 の生成宣言部分

## 背景

ADR-0091 は owner aggregate に deinit を必須とし、転送だけの body を
`fn (self: T) deinit() -> void = fields;` の 1 行で生成できるようにした。
#1559 は当初この綴りだけの再検討だったが、検討の結果、綴りではなく
構文そのものが問題だと分かった。

`= fields;` は読者の 3 つの質問すべてに言語内の答えを持たない。

- どういう文法か — RHS は式でも値でもない、この位置専用の marker
- `fields` はどこから来た語か — 定義がどこにもなく、grep でも LSP でも
  辿れない。SPEC の当該段落を暗記していないと読めない
- この関数は何をするか — 宣言から動作が導出できない

つまり儀式である。C++ の `= default;` が読めるのは長年の露出があるからで、
Kizu は継承できない。さらに `fields` という語は union では嘘になり
(consume するのは field ではなく active な owner payload)、struct でも
不正確だった(non-owner field は触らない)。SPEC は struct の生成しか
定義しておらず、union への適用は未定義のまま使われていた。

## 決定

生成構文を言語から削除し、転送 deinit は手書きする。

ADR-0091 の却下理由「集約の数だけ定型が量産される」は原理 10 の過剰適用
だった。原理 10 が禁じるのは **call site の数だけ**増える量産であり、
手書き転送 deinit は定義側に型ごと 1 回置くものである。`= fields;` との
差は型ごと 1 行 vs 数行の定数倍で、使用箇所に比例するコストではない。
あの議論で桁が変わっていたのは分解 consume(Austral 流、call site 量産)と
自動 Drop(hidden)で、両者の却下は本 ADR 後も変わらない。

安全性も生成に依存していない。完全性検査(ADR-0091 決定 1)が手書き body
でも全 owner field / payload の consume を強制するため、field を追加して
deinit を直し忘れれば compile error になる。生成が買っていたのはタイプ量
だけで、その対価が儀式化だった。ADR-0093 で `Tag => return` arm が手書き
可能になり、生成が出す全形は手で書ける。

将来 AST 量産(型数十個の転送 deinit)で手書きが痛くなった場合の逃げ道は
どちらも additive である: LSP code action / fmt 補助で**実ソースとして**
body を生成する(言語表面はゼロのまま)、または儀式でない綴りを改めて
設計して sugar を足す。残して後で消す方向だけが breaking であり、
原理 8(迷ったら閉じたまま出す)は削除を選ぶ。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `= fields;` 維持 | 儀式。union で嘘、struct でも不正確。C++ 族という以上の根拠がない |
| 別の語へ改名(`= owners;` 等) | 語を変えても「定義のない語を暗記する」構造は残る |
| attribute / derive 形式 | attribute 機構が Kizu になく、一点物がより大きくなる。signature が source から消える |

## 影響

- SPEC §8: 生成構文の定義を削除。手書き deinit の規則だけが残る
- `internal/parser`: `parseFieldsBody` 削除。`= fields;` は parse error に戻る
- `internal/ast`: 展開(`ExpandFieldsDeinit`)と生成系を削除。owner 性判定
  (`DeinitOwners` / `OwnerType` / `CleanupMethodName`)は checker が使うため
  `deinit_owners.go` として残す
- `internal/project`: loader の展開呼び出しを削除
- `.kizu` の既存使用 5 箇所を手書き body 化(breaking、repo 内で完結)

## 再評価条件

- Kizu で書かれた実プログラムが転送 deinit を数十個規模で持ち、手書きが
  実測で開発の妨げになった時、tooling 生成(実ソース出力)を先に検討し、
  それでも足りなければ儀式でない宣言構文を設計する
