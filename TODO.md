# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## 浮動小数点の残り

型・演算・cast・literal(PR 1)は入った。残りは 3 つ。

## 9. `print` を std に畳む

`print` は backend に型ごとの分岐(int / bool / bytes / enum 名前表 / error 表)を
持つ。`std::fmt::print<T>` を `comptime match` と `std::meta` で書き、backend には
「bytes を stdout に書く」primitive だけを残す。`Io` 無しで書く点は debug 用の例外として
docs に明記する。前提として `std::meta` が enum の variant 名を取れること。

## 10. float の文字列変換と `print(f64)`

最短往復表現の parse / print を `std` に 1 本書く(bignum 込み)。compiler の literal
変換もそれに切り替え、`typ::parse_float_literal` の範囲制限(19 桁、10^±22)を外す。
`std::testing::expect_equal<f64>` もこの後。

## 11. wasm backend の f32 / f64

今は build 時に target 非対応として拒否する。局所変数の型付けが i64 前提なので、
float の local / param / result を型で持つ変更が backend 全体に及ぶ。

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
