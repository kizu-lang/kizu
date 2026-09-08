# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## std::http / std::net の残り

evented server(ADR-0136〜0146)まで入った時点で残っているものです。

## 3. protocol の穴 (#1082)

| | 大きさ | 備考 |
| --- | --- | --- |
| multipart / form-data | 中 | |
| compression | 大 | 圧縮 library が要る。別の話 |
| HTTP/2 / HTTP/3 | 大 | |

## 4. TLS / HTTPS (#1081)

未着手。provider 境界の設計から。独立していていつでも始められる代わりに
一番大きい。

## 5. middleware (#1085)

pattern routing (`route.kizu`) は入った。関数 pointer は borrow parameter を運べる。
closure は無いので、状態を明示引数にする composition と、呼び出しを隠す登録簿の
どちらを middleware と呼ぶかを #1085 で決める。
