import { instantiateKizu } from "../../lib/kizu/browser/app.mjs";

const decoder = new TextDecoder();
const chunks = [];
const wasm = new URLSearchParams(window.location.search).get("wasm");

try {
  if (wasm === null) {
    throw new Error("the wasm query parameter is required");
  }
  const program = await instantiateKizu(await fetch(wasm), {
    write(stream, bytes) {
      chunks.push({ stream, bytes });
    },
  });
  const status = program.start();
  const before = renderChunks(chunks);

  // The host callback owns each retained chunk. Overwriting all guest memory
  // after return must therefore leave the DOM-bound bytes unchanged.
  new Uint8Array(program.memory.buffer).fill(0);
  const after = renderChunks(chunks);
  const copied = before.stdout === after.stdout && before.stderr === after.stderr;

  document.querySelector("#status").textContent = String(status);
  document.querySelector("#copied").textContent = copied ? "yes" : "no";
  document.querySelector("#stdout").textContent = after.stdout;
  document.querySelector("#stderr").textContent = after.stderr;
  window.kizuSmoke = { ready: true, status, copied, ...after };
} catch (error) {
  document.querySelector("#status").textContent = "error";
  document.querySelector("#stderr").textContent = String(error.stack ?? error);
  window.kizuSmoke = { ready: true, error: String(error.stack ?? error) };
}

function renderChunks(retained) {
  let stdout = "";
  let stderr = "";
  for (const { stream, bytes } of retained) {
    const text = decoder.decode(bytes);
    if (stream === 2) {
      stderr += text;
    } else {
      stdout += text;
    }
  }
  return { stdout, stderr };
}
