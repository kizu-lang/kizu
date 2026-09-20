# ADR-0061: defer は void call を 1 つ登録する

## Status

Accepted. 当初は cleanup method call(`x.deinit(...)`)だけを受けていたが、
call 一般に広げた。`errdefer` と owner aggregate の契約は ADR-0075。

## Context

Kizu の owner(`Array`、`String`、`Map`、`Box`、`Arena`、`deinit` を持つ user
struct)は明示 cleanup を要る。return 地点ごとに手書きするのは漏れやすく、暗黙の
destructor は runtime の仕事を隠して RAII に寄る。Zig 風の `defer` が cleanup を
見える形のまま 1 箇所に書かせる。

当初は `defer` の対象を「cleanup method call」に限った。C の handle を閉じる
`errdefer unsafe destroy(p)` や、`&var` を取る関数を出口で呼ぶ用途が出て、
「cleanup method」という綴りの制限が、defer の意味(void call を出口に登録する)
より狭いことが分かった。

## Decision

`defer <call>;` / `errdefer <call>;` は **`void` を返す call を 1 つ** 現在の
lexical block に登録する。block を出るとき(通常 exit、`return`、error return)に
登録順の逆順で走る。function body も block。

call の引数は通常の call と同じ passing mode で扱う(SPEC §6.3.1):

| 引数 | 登録時 | 出口 |
|---|---|---|
| copy | 値を読む(ADR-0132) | その値で呼ぶ |
| `&T` / `&var T` | 名前を参照できること | live で borrow 衝突がないことを再検査 |
| by-value receiver / `move x` | 名前を参照できること | consume。1 call につき owner 1 つまで |

`errdefer` は owner が consume されたら退役する(ADR-0114)。

拒否するもの: `void` 以外を返す call(`!void` を含む)、`try` / `catch` を含む式、
`print` などの署名を持たない builtin form、call でない文(`defer let`、`defer { }`)。

## Rejected

| 案 | 却下理由 |
|---|---|
| cleanup method call(`deinit`)に限る(当初の決定) | extern の destroy や `&var` を取る関数が書けない。制限は綴りの話で、ownership 規則は引数の passing mode が既に持っている |
| extern / `unsafe fn` だけ例外で許す | 特例が増えるだけ。一般規則で同じことが書ける |
| `defer { ... }` block | 出口に任意の制御フローを置く。call 1 つなら読む人が出口で何が起きるか 1 行で分かる |
| 引数を出口で評価する(Zig) | `defer x.deinit(allocator)` の allocator が途中で差し替わる。Go と同じく登録時評価にした(ADR-0132) |
| 1 call で複数 owner を consume | 片方だけ先に consume されたときに retire を分けられない。`defer` を 2 つ書けば足りる |
| Drop / RAII / 自動 cleanup | 見えない runtime の仕事。原理違反 |
