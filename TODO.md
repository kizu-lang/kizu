# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## WebAssembly application path

WASI default / optimized WAT と binary は 162 examples 中 142 件、browser binary は
135 件が native と同じ observable behavior を持つ。残りは host boundary が持たない
capability として build 時に拒否する。backend の portable lowering、binary encoder、WASI
runtime、browser host ABI、複数 module package の target build は入った。同じ package が
target 別 host adapter を source 上で選ぶ経路が残っている。

### W7. target-selected adapter

- 既存の `comptime if` から native / WASI / browser を問う compiler-defined
  `std::target` 述語を追加する。選ばれた branch だけを type / ownership / IR
  の各 phase が扱う。
- target に適合する entry / explicit export からだけ到達可能性を閉じ、native-only
  filesystem adapter や browser-only host adapter を反対 target の backend へ渡さない。
- native は file I/O、browser は明示 host input を使い、同じ portable core の結果を
  保つ package example と conformance test を追加する。

この章は、利用者が 1 package の portable core と target 別 adapter を source 上で
明示し、native / WASI / browser の artifact をそれぞれ直接 build できたら削除する。

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
