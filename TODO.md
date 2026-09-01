# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## wasm32-wasi backend

現在の `kizu build --target wasm32-wasi` は既定で WAT、`--emit wasm -o` で binary を
生成する。`just backend-matrix` では両方とも 162 examples 中 142 件が native と同じ
出力で動く。残り 20 件は target 非対応として build 時に拒否し、内訳は std::net 16 件、
extern C allocator 2 件、coro runtime と event loop が各 1 件。example ごとの例外を足さず、
共通 runtime と portable std の順に backend の対象を広げる。

この章の完了は、既存の `wasm32-wasi` target と browser target で portable な言語機能と
std API が native と同じ observable behavior を持ち、compiler が browser の読める binary
`.wasm` を直接生成できること。host が提供しない API は backend の未実装と混ぜず、target
非対応として明示的に分類する。

Go seed (`internal/wasm`) と shipping Kizu compiler (`compiler/src/internal/wasm`) は各段階で
同時に更新する。検証は内部生成文字列の構造 pin ではなく、`wasmtime` で example の
宣言出力を実行して行う。

### W4. WASI host boundary

- net / HTTP / evented Io / coro は、既存 `wasm32-wasi` host ABI で ownership と待機を
  隠さず表せるものだけを実装する。表せないものは成功したふりをせず target 非対応にする。
- unsafe raw pointer と extern C は WASI の安全な import / memory 境界を定義するまで
  target 非対応にする。

### W5. coverage を閉じる

- portable example は `kizu run` と wasm の出力を一致させ、WASI-dependent example は
  isolated な host capability 付きで再現可能にする。
- `--opt` の wasm execution も同じ oracle に含め、`std_string_join_trim.kizu` が
  `wasmtime` で停止しない既知の optimizer / backend mismatch を直す。
- `just wasi-smoke`、`just backend-matrix`、`just selfhost`、`go test ./...` を通し、README の
  matrix と target limitation を実測値へ更新する。

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
