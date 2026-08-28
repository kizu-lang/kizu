# ADR-0142: 汎用の cleanup は「解放が何を名指すか」を聞く

## Status

Accepted.

## Context

`Array<net::TcpStream>` が書けませんでした。

```text
type error: `std::net::TcpStream.deinit` expects 0 args, got 1
```

`Array.deinit` の element cleanup は Kizu source で、こう書かれていました。

```kizu
comptime if std::meta::is_owner<T>() {
    while self.len() > 0 {
        let item = self.pop_or_panic();
        item.deinit(allocator);      // ← arity の決め打ち
    }
} else {}
```

checker の特別な規則ではなく、この 1 行が T = TcpStream で monomorphize された
結果でした。

ADR-0132 は「解放は allocator を名指す」と決めましたが、それは
**memory を解放する owner の話**です。socket は descriptor を解放するので
名指すものがありません。汎用の cleanup はその差を知らずに allocator を渡して
いました。

これは evented server を書く上での実際の壁でした —— 接続を collection に
持てないので、`Poller` があっても数を固定した example しか書けません(ADR-0141)。

## Decision

`std::meta::release_names_allocator<T>()` を足し、container の element cleanup を
その分岐にします。

```kizu
comptime if std::meta::release_names_allocator<T>() {
    item.deinit(allocator);
} else {
    item.deinit();
}
```

答えの出どころは宣言された `deinit` の parameter list です。宣言していない owner
—— field を通してだけ cleanup を負う型 —— は derived deinit が allocator を渡すので
true です。

`array` / `arena` / `map` / `box` の 4 つが分岐します。

## Consequences

`Array<net::TcpStream>` が書けるようになり、`examples/net_poller.kizu` は接続を
collection に持って token を index にできます。汎用の evented server を止めて
いたものが外れました。

IR も LLVM も触っていません。loop が Kizu source なので、monomorphize の結果が
変わるだけです。

**derived deinit はまだ allocator を渡します。** socket を field に持つ struct は
自前の deinit が要ります —— `std::http::Exchange` が実際そうしています。同じ述語を
`DeriveDeinit` に通せば消えますが、それは別の変更です
(`docs/language-gaps.md`)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `TcpStream.deinit(allocator)` にして allocator を無視する | 「確保は明示 allocator」(原理 4)。allocator を取る API は memory を触ると読まれるので、取って無視するのは嘘。しかも container の中だけの都合で std の公開 API が歪む |
| 利用者が wrapper struct で包む | 同じ嘘を利用者側に押し付けるだけ。しかも全員が同じ wrapper を書くことになり、原理 10 が畳めと言う定型 |
| owner map の bool を再利用する(`Map<[]u8, bool>` の値が今は常に true) | `ast.OwnerType` が値をそのまま返しているので、false を入れると「owner ではない」になる。全消費者の意味が静かに変わる |
| container の deinit を 2 つ用意する(`deinit` と `deinit_shallow`) | 呼ぶ側が要素型に応じて選ぶことになる。型が答えられることを人間に選ばせない(原理 5) |
| element cleanup を runtime op に移す | 今は Kizu source なので monomorphize で済む。runtime に移すと arity 分岐が C に降りてきて、IR と LLVM を触ることになる |
