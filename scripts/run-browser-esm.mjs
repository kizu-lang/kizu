// Runs an emitted browser ESM directory under Node while preserving the
// browser-facing entry: app.mjs still fetches app.wasm relative to itself.
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import process from "node:process";

if (process.argv.length !== 3) {
  process.stderr.write("usage: node scripts/run-browser-esm.mjs <app.mjs>\n");
  process.exitCode = 2;
} else {
  const browserFetch = globalThis.fetch;
  let fileFetches = 0;
  globalThis.fetch = async (source, options) => {
    const url = source instanceof URL ? source : new URL(source);
    if (url.protocol === "file:") {
      fileFetches += 1;
      return new Response(await readFile(url), { status: 200 });
    }
    if (browserFetch === undefined) {
      throw new Error(`cannot fetch ${url}`);
    }
    return browserFetch(source, options);
  };

  const entry = pathToFileURL(resolve(process.argv[2]));
  const { instantiate } = await import(entry.href);
  if (fileFetches !== 0) {
    throw new Error("importing app.mjs fetched app.wasm");
  }
  const chunks = [];
  const program = await instantiate({
    write(stream, bytes) {
      chunks.push({ stream, bytes });
    },
  });
  if (fileFetches !== 1 || chunks.length !== 0) {
    throw new Error("instantiation fetched or entered the Kizu program unexpectedly");
  }
  const status = program.start();
  for (const chunk of chunks) {
    const output = chunk.stream === 2 ? process.stderr : process.stdout;
    output.write(chunk.bytes);
  }
  process.exitCode = status;
}
