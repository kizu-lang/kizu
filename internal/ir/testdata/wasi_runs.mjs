// Runs a batch of cases through one WASI module, for the replay tests in
// internal/ir (runtime_mbt_test.go). It reads
//   {"wasm": "<path>", "runs": [["arg", ...], ...]}
// on stdin and writes one JSON array: for each run its exit code (or
// "trap"), stdout and stderr.
//
// The module's main runs the cases its arguments name one after another and
// prints a line "=" after each. Instantiating costs more than most runs, so
// one instance takes every run until one stops the program; that run is
// recorded with how it stopped, and a fresh instance goes on with the next.
import { WASI } from "node:wasi";
import { closeSync, openSync, readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const SEPARATOR = "=\n";

const request = JSON.parse(readFileSync(0, "utf8"));
const module = await WebAssembly.compile(readFileSync(request.wasm));
const dir = mkdtempSync(join(tmpdir(), "kizu-wasi-"));
const outPath = join(dir, "out");
const errPath = join(dir, "err");
const results = [];
let next = 0;
while (next < request.runs.length) {
  // A batch is bounded: every instance copies its arguments in.
  const batch = request.runs.slice(next, next + 64);
  const out = openSync(outPath, "w");
  const err = openSync(errPath, "w");
  const wasi = new WASI({
    version: "preview1",
    args: ["cases.wasm", ...batch.flat()],
    env: {},
    stdout: out,
    stderr: err,
    returnOnExit: true,
  });
  let code;
  try {
    const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
    code = wasi.start(instance);
  } catch (error) {
    code = "trap";
  }
  closeSync(out);
  closeSync(err);
  const pieces = readFileSync(outPath, "utf8").split(SEPARATOR);
  const finished = pieces.slice(0, -1);
  for (const stdout of finished) {
    results.push({ code: 0, stdout, stderr: "" });
  }
  next += finished.length;
  if (code === 0 && finished.length === batch.length) {
    continue;
  }
  // The run after the last separator is the one that stopped the program.
  results.push({ code, stdout: pieces[pieces.length - 1], stderr: readFileSync(errPath, "utf8") });
  next += 1;
}
rmSync(dir, { recursive: true, force: true });
process.stdout.write(JSON.stringify(results));
