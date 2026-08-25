# ADR-0129: Allocator を user 実装に開く。安全性は tie が持つ

Status: 採用

Issue: なし(ADR-0092 決定 1 の改訂)

## 背景

ADR-0092 決定 1 は user-defined allocator を「当面入れない」とした。根拠は
原理 8(可逆性の非対称)で、開く条件は 1 つだけだった:

> 「自作 allocator でなければ満たせない実需」(特殊メモリ、workload 特化戦略)は
> 現ユーザーに存在しない。

その実需が出た。selfhost compiler は `kizu ir compiler` 1 回で 1,780 万回の
確保と 1,758 万回の解放を出し、libc malloc への往復が profile の 20% を
占める。既存の 2 factory はどちらも受けられない —— `page_allocator` は
その libc であり、`fixed_buffer` の bump は解放が no-op なので同じ workload で
1,591 MB 積み上がる(peak-live は 422 MB)。

延期のもう 1 つの根拠(決定 3、「copy 型の `Allocator` が user の buffer を
borrow する形は borrow field 禁止と衝突する」)は ADR-0099 の tie で解消済み。

## 決定

### 1. `Allocator` は user が実装できる

std factory だけが作れる opaque capability という制限を外す。user は確保と
解放を `unsafe fn` の free function で書き、std の
`allocator_from<T, alloc: Function, free: Function>(state: &var T)` が名前で
受けて `Allocator` にまとめる(署名は SPEC §14.3)。

`Function` static parameter は SPEC §13 が既に持つ形で、まさに「std wrapper が
名前で受けた関数を trusted primitive に転送する」ためのものである。closure では
なく、局所を捕捉せず、runtime データに格納できない。**言語に足すものは無い。**

2 関数が unsafe なのは raw pointer を返すからで、SPEC §12 の既存の規則が
そのまま適用される。新しい unsafe の種類は増えない。

### 2. 安全性は tie が持つ。contract 化はしない

`Allocator` を user-facing `contract` にはしない。ADR-0092 決定 1 が守った
「container が user state を参照する形」の危険は、参照を禁じることではなく
tie で寿命を追うことで受ける。

`allocator_from` は `&var T` を取り `Allocator` を返す —— ADR-0099 が定めた
tied allocator の署名そのものである。したがって **checker に新しい規則は
要らない**。既存の recognizer が署名の形で拾い、tie 規則は ADR-0099 が持つ。

### 3. `Allocator` の runtime 表現を広げる

ADR-0092 決定 2 が「第二の allocator 種を実際に入れる変更が、その時に
handle を導入する」として延期した handle 実体化を、ここで履行する。
`Allocator` は 1 pointer から state pointer + 関数 pointer 2 本になる。

確保・解放ごとに間接呼び出しが 1 回増える。同じ malloc の上で測ると
`ir compiler` で +1.5%、`build --emit-llvm compiler` で ±0% だった。呼び先が
実行中ずっと同じなので分岐予測が当たる。

それでも開く根拠は性能ではない。Kizu は全ての owner に allocator を要求する。
要求する側が選択肢を 2 つしか持たないのは非対称であり、**明示 allocator と
いう既に払っているコストに見返りを与える**のが決定の理由である。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 現状維持し、size class allocator を runtime.c に埋める | 同じ性能を SPEC 変更なしで取れる。だが明示 allocator のコストは払わせたまま、選択肢は 2 つのままになる |
| std が基本形(汎用 / bump / pool / stack / ring)を全部提供する | 基本形は 5〜6 個で足し切れる。足し切れないのは std が知り得ない確保先(mmap した file、共有メモリ、hugepage、組込みの固定領域)で、これは列挙できる集合ではない |
| `contract Allocator`(Zig 式の全面開放) | tie を通らない経路ができ、ADR-0092 が挙げた「user state の寿命を誰も追跡しない dangling allocator」がそのまま残る |
| user allocator を global に 1 つだけ登録(Rust の `#[global_allocator]` 相当) | 間接呼び出しは同じで、しかも同一プログラム内で 2 つの戦略を使い分けられない。freestanding の実体供給(ADR-0092 決定 4)は別機構として残す |
| bump に in-place 成長を足して `fixed_buffer` で済ませる | 実測で in-place が当たるのは realloc 538 万回中 9 万回、1.7%。伸ばす対象が「最後の確保」であることが稀 |
| 解放するブロックの class を header word で持つ | 64 byte の確保に 16 byte で 25% 損なう。runtime の解放 14 箇所のうち 13 箇所は size を知っているので、`free` に size を渡せば header は要らない |
| runtime の関数値(function type)で 2 関数を渡す | Kizu に関数型が無い。足せば隠れた制御フローが言語に入る。`Function` static parameter が同じことを compile time に済ませる |
| user allocator を safe に書けるようにする | 確保は raw pointer を返す操作で compiler が証明できない。印のない unsafe を作らない(ADR-0089) |
