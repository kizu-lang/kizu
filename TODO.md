# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## std::rand と model-based testing

`kizu test` だけで完結する MBT を std に置きます。model は Kizu で書き、runner の
中核は「Cmd 列を再生して step で突き合わせる」だけにして、乱択生成器はその列の
供給源の 1 つに留めます。Quint の ITF trace は `std::json` で読めるので、後から
`run_trace` を足せば連携できますが、std に外部 tool への依存は入れません。
seed の再現(`kizu test --seed`、`std::testing::seed()`)と `run_model` /
`check`(`docs/std/testing.md`)は入った。

## 8. dogfood

言語の顔になる例を `examples/` に置く: 帳簿(送金の列に対して残高の総和保存・
再送の冪等性)、契約の状態機械(遷移表を仕様にする)。金額は float でなく最小単位の
`i64` で持つ。`std::testing::run_model` は消した —— 呼び出しが positional 7 引数、
食い違いの説明を出せず、列の表示も毎回自作だった。仕様を Quint / Lean のような
仕様言語で書き、その trace を `std::json` で読んで実装を検査する形を検討中。

## std::http / std::net の残り

evented server(ADR-0136〜0146)まで入った時点で残っているものです。

## 2. 抱える数の上限 (#1083)

TaskSet の visible accept loop は connection を無制限に accept / spawn できる。
実測では worker が約 269 KiB/connection を使うため、`max_requests` では代わりに
ならない。max connections / max in-flight の数え方、上限時に accept を待つか明示的に
断るか、完了 worker を caller がどう観測するかを #1083 で決める。

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

pattern routing (`route.kizu`) は入った。関数 pointer は borrow parameter を運べる。
closure は無いので、状態を明示引数にする composition と、呼び出しを隠す登録簿の
どちらを middleware と呼ぶかを #1085 で決める。
