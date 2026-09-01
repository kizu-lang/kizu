import { instantiateKizu } from "../../lib/kizu/browser/app.mjs";

const decoder = new TextDecoder();
const encoder = new TextEncoder();
const responses = new Map();
const wasm = new URLSearchParams(window.location.search).get("wasm");

try {
  if (wasm === null) {
    throw new Error("the wasm query parameter is required");
  }

  const program = await instantiateKizu(await fetch(wasm), {
    write() {},
    host: {
      set_heading(host, pointer, length) {
        document.querySelector("#heading").textContent =
          decoder.decode(host.readBytes(pointer, length));
      },
      begin_message(host, handle, pointer, length) {
        const request = decoder.decode(host.readBytes(pointer, length));
        setTimeout(() => {
          responses.set(handle, encoder.encode(request.toUpperCase()));
          host.callExport("message_ready", handle, 0);
        }, 0);
      },
      read_message(host, handle, pointer, capacity) {
        const response = responses.get(handle);
        if (response === undefined || response.byteLength > (capacity >>> 0)) {
          return 0;
        }
        responses.delete(handle);
        return host.writeBytes(pointer, response);
      },
      set_result(host, pointer, length) {
        const result = decoder.decode(host.readBytes(pointer, length));
        document.querySelector("#result").textContent = result;
        window.kizuHost.result = result;
        window.kizuHost.ready = true;
      },
      flip_byte(host, value) {
        window.kizuHost.hostByte = value;
        return 511;
      },
    },
  });

  window.kizuHost = { ready: false };
  const byte = program.exports.byte_roundtrip(255);
  window.kizuHost.byte = byte;
  document.querySelector("#byte").textContent = `${window.kizuHost.hostByte}/${byte}`;
  const boolean = program.exports.bool_roundtrip(2);
  window.kizuHost.boolean = boolean;
  document.querySelector("#boolean").textContent = String(boolean);
  const status = program.start();
  window.kizuHost.status = status;
  document.querySelector("#status").textContent = String(status);
} catch (error) {
  document.querySelector("#status").textContent = "error";
  document.querySelector("#error").textContent = String(error.stack ?? error);
  window.kizuHost = { ready: true, error: String(error.stack ?? error) };
}
