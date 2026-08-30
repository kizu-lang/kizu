# TODO: std::http / std::net の残り

evented server(ADR-0136〜0146)まで入った時点で残っているものです。番号は
優先順ではなく識別子。完了したものは ADR と git log が持つのでここには残しません。

## 1. `max_requests` の既定を再検討する

現在値 1 は変えない。TaskSet server で connection ごとの memory、idle timeout、
fairness と keep-alive の throughput を測ってから決める。

## 2. 抱える数の上限 (#1083)

max connections / max in-flight。どちらも「何本まで抱えるか」で、抱えたものを
捨てる規則が要る。まだ誰も困っていないので保留。

serve loop 自体は `first` / `next` で入った(ADR-0144)。`serve` は作らず、loop は
caller のもの。停止は `break`。期限の掃除は `next` の中なので、書かなくても塞がる。

## 3. protocol の穴 (#1082)

| | 大きさ | 備考 |
| --- | --- | --- |
| trailer を header に足す | 小 | 今は消費して捨てる |
| upgrade (101 / WebSocket) | 小 | `Framing::Raw` は既にある |
| pipelining | 中 | 先読みは順に処理、答えは重ねない |
| multipart / form-data | 中 | |
| compression | 大 | 圧縮 library が要る。別の話 |
| `Date` header | — | std に暦が無い。暦が先 |
| HTTP/2 / HTTP/3 | 大 | |

## 4. TLS / HTTPS (#1081)

未着手。provider 境界の設計から。独立していていつでも始められる代わりに
一番大きい。

## 5. middleware (#1085)

pattern routing (`route.kizu`) は入った。middleware は closure / function value が
無いので設計を固定できない。言語待ちで、下の 6 が前提。

## 6. 関数 pointer が borrow を運べない

```kizu
fn drive(worker: fn(&string::String) -> i64, held: &string::String) -> i64 {
    return worker(held);
}
```

```text
error: type error: `worker` argument 1 expects &std::string::String, got std::string::String
```

pointer 型は `&String` を正しく持っているのに、借用済みの引数が borrow と
認識されない。**SPEC §7 が「`fn(...) -> T` を通した呼び出しに `unsafe` は要らない」と
言っている機能の穴。**

原因: `internal/types/checker.go` の `checkFuncPointerCall` が、直接呼び出しの
borrow 処理(`requireMutableBorrowArg` / `coerceReturnedBorrowArgument`)を
通っていない。`compiler/` 側にも同じ穴があるはず。

直せば middleware (#1085) の設計余地が広がる。evented とは独立。
