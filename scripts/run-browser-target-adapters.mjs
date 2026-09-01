import { readFile } from "node:fs/promises";
import process from "node:process";

import { instantiateKizu } from "../runtime/browser/kizu.mjs";

if (process.argv.length !== 3) {
  process.stderr.write(
    "usage: node scripts/run-browser-target-adapters.mjs <file.wasm>\n",
  );
  process.exitCode = 2;
} else {
  const source = await readFile(process.argv[2]);
  const input = new TextEncoder().encode("kizu fs fixture\n");
  const chunks = [];
  const program = await instantiateKizu(source, {
    write(stream, bytes) {
      chunks.push({ stream, bytes });
    },
    host: {
      read_input(host, pointer, capacity) {
        if (input.byteLength > (capacity >>> 0)) {
          return 0;
        }
        return host.writeBytes(pointer, input);
      },
    },
  });
  if (program.exports.input_capacity() !== 64) {
    throw new Error("browser adapter exported the wrong input capacity");
  }
  const status = program.start();
  for (const chunk of chunks) {
    const output = chunk.stream === 2 ? process.stderr : process.stdout;
    output.write(chunk.bytes);
  }
  process.exitCode = status;
}
