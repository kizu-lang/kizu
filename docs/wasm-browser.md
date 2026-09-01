# wasm32-browser

`wasm32-browser` は WASI を偽装せず、JavaScript host に必要な境界だけを import する
WebAssembly target です。WAT は inspection 用、browser が読む artifact は binary
`.wasm` です。

```sh
kizu build --target wasm32-browser app.kizu
kizu build --target wasm32-browser --emit wasm -o app.wasm app.kizu
```

## Guest / host ABI

| 向き | 名前 | 型 | 意味 |
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

`main` 以外の Kizu 関数は暗黙に export しません(ADR-0135)。host との値の受け渡しを
関数ごとの特別な payload ABI に増やさず、program entry と byte stream に閉じます。

## JavaScript adapter

```js
import { instantiateKizu } from "./runtime/browser/kizu.mjs";

const decoders = { 1: new TextDecoder(), 2: new TextDecoder() };
const output = document.querySelector("#output");
const program = await instantiateKizu(await fetch("./app.wasm"), {
  write(stream, bytes) {
    const text = decoders[stream].decode(bytes, { stream: true });
    output.append(document.createTextNode(text));
    return true;
  },
});

const status = program.start();
```

adapter は `Response`、`WebAssembly.Module`、または
`WebAssembly.instantiate` が受け取る byte buffer を受理します。callback が例外を
投げた場合は隠さず `start()` の呼び出し元へ伝播します。

## Capability

browser host adapter が提供するのは stdout / stderr の byte write だけです。
`print`、blocking `std::io` の stdout / stderr、allocator と portable な std は使えます。
filesystem、`std::process`、stdin、socket / `std::net` / `std::http`、evented `std::io`、
`std::coro`、extern C は import を出して load 時に失敗させず、build 時に
`target wasm32-browser does not support ...` として拒否します。DOM、Fetch、WebSocket
などを暗黙 capability として guest に渡しません。

`tests/browser/smoke.html` は function call、aggregate、allocation、error と、DOM が
保持する output bytes が guest memory の上書き後も変わらないことを検査する page です。
広い corpus は `scripts/backend-matrix` の `browser` route が同じ adapter を使って測ります。
