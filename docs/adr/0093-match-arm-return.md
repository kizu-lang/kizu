# ADR-0093: statement match の arm body に `return` を許す

Status: 採用

## 背景

owner-payload union の deinit 契約(ADR-0075)は wildcard 禁止・全 variant
明示を要求するが、arm body は expression のみで void の無操作 expression が
存在しないため、payload なし variant を持つ union の deinit を手で書く合法な
形がなかった。`= fields;`(ADR-0091)は `Tag => return` の arm を AST 生成で
使っており、生成でしか書けない形が言語に残っていた。

## 決定

statement 位置の `match` の arm body を「expression または `return` 文」に
広げる。`Tag => return,` / `Tag => return expr,` は囲む関数からの早期 return
で、意味論・検査(consume 強制、borrow escape、defer/errdefer)・lowering は
既存の `return` そのもの。expression 位置の `match` には入れない。
`break` / `continue` は必要が出た時に additive に検討する。

Rust / Zig / Go / C はいずれも arm / case 内の return を許しており、
「arm は式のみ」の制限側が独自だった。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| deinit の match だけ non-owner variant の省略を許す | 文脈限定の exhaustive 例外という hidden special case |
| arm に block `{ ... }` を許す | この必要に対して過大。「書き方の自由度を増やしすぎない」に反する |
| 現状維持(mixed union は `= fields;` 専用) | 生成でしか書けない AST 形が言語に残り続ける |

## 影響

- parser: arm body の `return` 受理(terminator は arm の comma)
- SPEC §6.12 に追記
- checker / IR は変更なし(生成 body で実証済みの既存経路)
