import { readFile } from "node:fs/promises";
import process from "node:process";

import { instantiateKizu } from "../runtime/browser/kizu.mjs";

if (process.argv.length !== 3) {
  process.stderr.write("usage: node scripts/run-browser-host.mjs <file.wasm>\n");
  process.exitCode = 2;
} else {
  const source = await readFile(process.argv[2]);
  const decoder = new TextDecoder();
  const encoder = new TextEncoder();
  const responses = new Map();
  let heading = "";
  let result = "";
  let hostByte;
  let finish;
  const completed = new Promise((resolve) => {
    finish = resolve;
  });

  const program = await instantiateKizu(source, {
    write() {},
    host: {
      set_heading(host, pointer, length) {
        heading = decoder.decode(host.readBytes(pointer, length));
        try {
          host.readBytes(-1, 1);
          throw new Error("out-of-bounds host read was accepted");
        } catch (error) {
          if (!(error instanceof WebAssembly.RuntimeError)) {
            throw error;
          }
        }
      },
      begin_message(host, handle, pointer, length) {
        const request = decoder.decode(host.readBytes(pointer, length));
        queueMicrotask(() => {
          responses.set(handle, encoder.encode(request.toUpperCase()));
          host.callExport("message_ready", handle, 0);
        });
      },
      read_message(host, handle, pointer, capacity) {
        const response = responses.get(handle);
        if (response === undefined || response.byteLength > (capacity >>> 0)) {
          return 0;
        }
        try {
          host.writeBytes(-1, response);
          throw new Error("out-of-bounds host write was accepted");
        } catch (error) {
          if (!(error instanceof WebAssembly.RuntimeError)) {
            throw error;
          }
        }
        responses.delete(handle);
        return host.writeBytes(pointer, response);
      },
      set_result(host, pointer, length) {
        result = decoder.decode(host.readBytes(pointer, length));
        finish();
      },
      flip_byte(host, value) {
        hostByte = value;
        return 511;
      },
    },
  });

  const byte = program.exports.byte_roundtrip(255);
  const boolean = program.exports.bool_roundtrip(2);
  const status = program.start();
  await completed;
  if (status !== 0 || heading !== "Kizu browser host ready" ||
      result !== "BROWSER CALLBACK" || hostByte !== -1 || byte !== 255 || boolean !== 1) {
    throw new Error(JSON.stringify({ status, heading, result, hostByte, byte, boolean }));
  }

  const asyncImport = await instantiateKizu(source, {
    write() {},
    host: {
      set_heading() {},
      begin_message() {
        return Promise.resolve();
      },
      read_message() {
        return 0;
      },
      set_result() {},
      flip_byte() {
        return 0;
      },
    },
  });
  try {
    asyncImport.start();
    throw new Error("Promise-returning browser import was accepted");
  } catch (error) {
    if (!(error instanceof TypeError) ||
        !String(error.message).includes("host.begin_message must be synchronous")) {
      throw error;
    }
  }
  process.stdout.write(`${heading}\n${result}\n${hostByte}/${byte}\nbool=${boolean}\n`);
}
