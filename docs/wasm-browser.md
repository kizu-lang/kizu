# wasm32-browser

`wasm32-browser` は WASI を偽装せず、JavaScript host に必要な境界だけを import する
WebAssembly target です。WAT は inspection 用、browser が読む artifact は binary
`.wasm` です。

```sh
kizu build --target wasm32-browser app.kizu
kizu build --target wasm32-browser --emit wasm -o app.wasm app.kizu
```

## Guest / host ABI

target が常に持つ境界は次です。

| 向き | 名前 | WebAssembly 型 | 意味 |
| --- | --- | --- | --- |
| import | `kizu.write` | `(stream: i32, ptr: i32, len: i32) -> i32` | bytes を同期的に渡す。0 は受理、非 0 は拒否 |
| export | `memory` | `WebAssembly.Memory` | guest の linear memory |
| export | `kizu_start` | `() -> i32` | `main` を 1 回実行し、status を返す |

`stream` は 1 が stdout、2 が stderr です。`ptr..ptr+len` は import call の間だけ
借りられます。host が call 後も bytes を使うなら、その場で copy しなければなりません。
標準 adapter [`runtime/browser/kizu.mjs`](../runtime/browser/kizu.mjs) は copy した
`Uint8Array` だけを callback に渡します。callback は同期関数で、`false` を返すと
`std::io` の書き込みは `WriteFailed` になります。`print` は error を返さない言語組み込み
なので拒否を観測しません。書き込み失敗を処理する program は明示的な `std::io` を使います。

`kizu_start` は正常終了と `ExitStatus::Success` を 0、未捕捉 error と
`ExitStatus::Failure` を 1、`ExitStatus::Specific(code)` をその `u8` に写します。
未捕捉 error は stderr に診断を書いてから返ります。panic は stderr に診断を書き、
status に潰さず WebAssembly trap にします。page や JavaScript process を終了しません。

追加の browser capability は source で明示します。

```kizu
extern "browser" fn set_title(text: []u8) -> void
extern "browser" fn begin_request(handle: u32, path: []u8) -> void
extern "browser" fn read_response(handle: u32, destination: []u8) -> usize

export "browser" fn request_ready(handle: u32, status: i32) -> void {
    var storage = [256]u8{};
    let writable = storage.as_mut_bytes();
    // SAFETY: writable はこの同期 call 中に 256 bytes の有効な storage を指す。
    let length = unsafe read_response(handle, writable);
    // status / bytes を Kizu の値または error にここで明示的に写す。
}
```

`extern "browser" fn name` は `(import "host" "name" ...)`、
`export "browser" fn name` は instance の `exports.name` になります。module path は host 名に
含めず、同じ末尾名を異なる signature で宣言すると build error です。通常の `fn` / `pub fn`
は暗黙に export しません(ADR-0135)。`main`、`memory`、`kizu_start` は予約済みです。

ABI は scalar / raw pointer と import parameter の byte view だけです。完全な定義は
SPEC §12.1 にあります。

| Kizu | JavaScript / WebAssembly |
| --- | --- |
| `i8`〜`u32`、`usize`、`isize` | `number` (`i32`) |
| `i64` / `u64` | `bigint` (`i64`) |
| `bool` | `number` (`i32`, 0 / 1) |
| raw pointer / nullable raw pointer | `number` (`i32` address) |
| import parameter の `[]u8` | `pointer, length` の 2 引数 |

JavaScript の `i32` は signed `number` として見えるため、`u32` / `usize` を host で符号なしに
読むときは `value >>> 0` を使います。`u64` の bit pattern は
`BigInt.asUintN(64, value)` で読めます。`i8` / `i16` / `u8` / `u16` は宣言幅に正規化されます。

aggregate、owner、safe borrow、error union は境界を通りません。失敗は status と handle を
scalar で callback に渡し、callback body が必要な Kizu error に変換します。これにより
error payload 用の関数別 ABI や hidden allocation はありません。

## JavaScript adapter

```js
import { instantiateKizu } from "./runtime/browser/kizu.mjs";

const decoders = { 1: new TextDecoder(), 2: new TextDecoder() };
const output = document.querySelector("#output");
const responses = new Map();
const program = await instantiateKizu(await fetch("./app.wasm"), {
  write(stream, bytes) {
    const text = decoders[stream].decode(bytes, { stream: true });
    output.append(document.createTextNode(text));
    return true;
  },
  host: {
    set_title(host, pointer, length) {
      const bytes = host.readBytes(pointer, length);
      document.title = new TextDecoder().decode(bytes);
    },
    begin_request(host, handle, pointer, length) {
      const path = new TextDecoder().decode(host.readBytes(pointer, length));
      fetch(path).then(async (response) => {
        responses.set(handle, new Uint8Array(await response.arrayBuffer()));
        host.callExport("request_ready", handle, response.ok ? 0 : response.status);
      });
    },
    read_response(host, handle, pointer, capacity) {
      const bytes = responses.get(handle);
      if (bytes === undefined || bytes.byteLength > (capacity >>> 0)) return 0;
      responses.delete(handle);
      return host.writeBytes(pointer, bytes);
    },
  },
});

const status = program.start();
```

adapter は `Response`、`WebAssembly.Module`、または
`WebAssembly.instantiate` が受け取る byte buffer を受理します。戻り値は `instance`、
`exports`、`memory`、`start()` を持ちます。host function の第 1 引数は次の操作を持つ
固定 context です。

| 操作 | 契約 |
| --- | --- |
| `readBytes(pointer, length)` | bounds を検査し、host-owned `Uint8Array` copy を返す |
| `writeBytes(pointer, bytes)` | bounds を検査し、`ArrayBuffer` / view を guest memory へ copy して長さを返す |
| `callExport(name, ...args)` | source が明示 export した callback を呼ぶ |
| `memory` | 現在の `WebAssembly.Memory` |

import callback 自体は同期でなければならず、`Promise` を返すと `TypeError` です。Fetch、
timer、event listener は callback を予約してから import を返し、完了後に
`host.callExport(...)` で再入します。import callback や export callback が投げた例外は
隠しません。

## Capability

`print`、blocking `std::io` の stdout / stderr、allocator と portable な std は使えます。
DOM、Fetch、WebSocket、timer などは compiler 組み込みではありませんが、必要な操作を
`extern "browser"` と adapter の `host` object に明示すれば使えます。つまり Kizu code
が JavaScript の global API を直接呼ぶのではなく、source に見える typed boundary を JS が
実装します。

filesystem、`std::process`、stdin、socket / `std::net` と `std::http` の network 経路、evented `std::io`、
`std::coro`、extern C は、対応する browser adapter を暗黙に仮定せず build 時に
`target wasm32-browser does not support ...` として拒否します。`std::http` を browser Fetch
に自動変換するわけではありません。必要なら今は `extern "browser"` で Fetch capability を
明示します。

`tests/browser/smoke.html` は function call、aggregate、allocation、error と、DOM が
保持する output bytes が guest memory の上書き後も変わらないことを検査する page です。
`tests/browser/host_interface.html` は DOM 更新、bounds-checked memory copy、非同期 callback、
small integer ABI を実 Chrome で検査します。
広い corpus は `scripts/backend-matrix` の `browser` route が同じ adapter を使って測ります。
