// Runs the browser host ABI in JavaScript for broad backend-matrix coverage.
// Real browser loading is covered separately by tests/browser/smoke.html.
import { readFile } from "node:fs/promises";
import process from "node:process";

import { instantiateKizu } from "../runtime/browser/kizu.mjs";

if (process.argv.length !== 3) {
  process.stderr.write("usage: node scripts/run-browser-wasm.mjs <file.wasm>\n");
  process.exitCode = 2;
} else {
  const source = await readFile(process.argv[2]);
  const chunks = [];
  const program = await instantiateKizu(source, {
    write(stream, bytes) {
      chunks.push({ stream, bytes });
    },
  });
  const status = program.start();
  for (const chunk of chunks) {
    const output = chunk.stream === 2 ? process.stderr : process.stdout;
    output.write(chunk.bytes);
  }
  process.exitCode = status;
}
