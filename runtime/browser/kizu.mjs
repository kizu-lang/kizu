// instantiateKizu attaches the explicit browser host boundary to one compiled
// Kizu module. The callback is synchronous because an imported WebAssembly
// function cannot suspend without changing the guest ABI.
export async function instantiateKizu(source, options = {}) {
  if (typeof options.write !== "function") {
    throw new TypeError("instantiateKizu requires a write(stream, bytes) callback");
  }

  let memory;
  const imports = {
    kizu: {
      write(stream, pointer, length) {
        if (memory === undefined) {
          throw new WebAssembly.RuntimeError("kizu.write ran before memory was exported");
        }
        if (stream !== 1 && stream !== 2) {
          throw new WebAssembly.RuntimeError(`unknown Kizu output stream ${stream}`);
        }

        // Guest memory is borrowed only for this import call. Copy before the
        // callback can retain the bytes or a later memory.grow detaches them.
        const bytes = new Uint8Array(memory.buffer, pointer, length).slice();
        const accepted = options.write(stream, bytes);
        if (accepted !== null && typeof accepted === "object" &&
            typeof accepted.then === "function") {
          throw new TypeError("Kizu browser write callbacks must be synchronous");
        }
        return accepted === false ? 1 : 0;
      },
    },
  };

  let input = source;
  if (typeof Response !== "undefined" && source instanceof Response) {
    input = await source.arrayBuffer();
  }
  const instantiated = source instanceof WebAssembly.Module
    ? await WebAssembly.instantiate(source, imports)
    : await WebAssembly.instantiate(input, imports);
  const instance = instantiated instanceof WebAssembly.Instance
    ? instantiated
    : instantiated.instance;

  memory = instance.exports.memory;
  if (!(memory instanceof WebAssembly.Memory)) {
    throw new TypeError("Kizu browser module must export memory");
  }
  if (typeof instance.exports.kizu_start !== "function") {
    throw new TypeError("Kizu browser module must export kizu_start");
  }

  return Object.freeze({
    instance,
    memory,
    start() {
      return instance.exports.kizu_start();
    },
  });
}
