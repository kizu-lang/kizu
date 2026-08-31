# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## wasm32-wasi backend

現在の `kizu build --target wasm32-wasi` は WAT を生成し、`just backend-matrix` では
162 examples 中 142 件が native と同じ出力で動く。残り 20 件の最初の失敗は net poller
12 件、net listen 4 件、extern C allocator 2 件、coro runtime と event loop が各 1 件。
example ごとの例外を足さず、共通
runtime と portable std の順に backend の対象を広げる。

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

- `scripts/backend-matrix` が backend lowering failure、output mismatch、target 非対応を
  分けて報告し、新しい example が分類なしで落ちないようにする。
- portable example は `kizu run` と wasm の出力を一致させ、WASI-dependent example は
  isolated な host capability 付きで再現可能にする。
- `--opt` の wasm execution も同じ oracle に含め、`std_string_join_trim.kizu` が
  `wasmtime` で停止しない既知の optimizer / backend mismatch を直す。
- `just wasi-smoke`、`just backend-matrix`、`just selfhost`、`go test ./...` を通し、README の
  matrix と target limitation を実測値へ更新する。
- WASI の portable example について backend lowering failure と未分類の output mismatch を
  0 にする。

### W6. binary `.wasm` encoding

- WAT writer と binary encoder が別々に Kizu IR を解釈しないよう、type、import、function、
  table、memory、global、export、element、code、data を 1 つの WebAssembly module 表現へ
  lower する。WAT と binary はその同じ表現の 2 renderer にする。
- section ordering、index space、function body、data / element segment、signed / unsigned LEB128
  を deterministic に encode し、同じ入力から byte-for-byte 同じ `.wasm` を生成する。
- 現在 WAT を stdout に出す `build --target wasm32-wasi` を残しながら、binary artifact と
  inspection 用 WAT をどう明示的に選ぶか、CLI の output contract を決める。binary を
  terminal に暗黙出力しない。
- encoder の単体構造を pin するだけで完了にせず、生成 binary を `wasmtime` で validate / run
  し、同じ module の WAT route と observable behavior が一致することを検証する。

### W7. browser target

- browser target の CLI spelling、entry / export、host import、memory ownership、JavaScript との
  string / buffer 受け渡し ABI を明示する。WASI `_start` / `fd_write` を browser に偽装しない。
- portable core は W1〜W3 の同じ WebAssembly module lowering を使い、target 差は host import、
  entry / export、利用可能 capability だけに閉じる。
- stdout、filesystem、process、socket の無い browser で `Io` と std API のどこまでを host
  adapter が提供するかを文書化する。未提供 API は compile / build 時に target 非対応とする。
- generated `.wasm` と必要最小限の JavaScript host adapter を実 browser で読み込み、function
  call、aggregate、allocation、error、DOM へ渡す byte buffer を conformance output で検証する。
- `wasm32-wasi` と browser の portable coverage、target 非対応、host-dependent coverage を
  README の matrix で分けて実測する。両 target に未分類の lowering failure / output mismatch が
  なくなったら、この章を削除する。

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
