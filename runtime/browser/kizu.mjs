// instantiateKizu attaches the explicit browser host boundary to one compiled
// Kizu module. The callback is synchronous because an imported WebAssembly
// function cannot suspend without changing the guest ABI.
export async function instantiateKizu(source, options = {}) {
  if (typeof options.write !== "function") {
    throw new TypeError("instantiateKizu requires a write(stream, bytes) callback");
  }
  if (options.host !== undefined &&
      (options.host === null || typeof options.host !== "object")) {
    throw new TypeError("instantiateKizu host must be an object of functions");
  }

  let memory;
  let instance;
  const hostContext = Object.freeze({
    get memory() {
      return requireMemory();
    },
    readBytes(pointer, length) {
      const range = checkedRange(pointer, length);
      return new Uint8Array(requireMemory().buffer, range.pointer, range.length).slice();
    },
    writeBytes(pointer, sourceBytes) {
      const bytes = byteView(sourceBytes);
      const range = checkedRange(pointer, bytes.byteLength);
      new Uint8Array(requireMemory().buffer, range.pointer, range.length).set(bytes);
      return range.length;
    },
    callExport(name, ...args) {
      if (instance === undefined) {
        throw new WebAssembly.RuntimeError(
          `browser export ${String(name)} was called before instantiation`,
        );
      }
      const callback = instance.exports[name];
      if (typeof callback !== "function") {
        throw new TypeError(`Kizu browser module does not export function ${String(name)}`);
      }
      return callback(...args);
    },
  });

  const host = Object.create(null);
  for (const [name, callback] of Object.entries(options.host ?? {})) {
    if (typeof callback !== "function") {
      throw new TypeError(`instantiateKizu host.${name} must be a function`);
    }
    host[name] = (...args) => {
      const result = callback(hostContext, ...args);
      requireSynchronous(result, `Kizu browser host.${name}`);
      return result;
    };
  }

  const imports = {
    kizu: {
      write(stream, pointer, length) {
        if (stream !== 1 && stream !== 2) {
          throw new WebAssembly.RuntimeError(`unknown Kizu output stream ${stream}`);
        }

        // Guest memory is borrowed only for this import call. Copy before the
        // callback can retain the bytes or a later memory.grow detaches them.
        const bytes = hostContext.readBytes(pointer, length);
        const accepted = options.write(stream, bytes);
        requireSynchronous(accepted, "Kizu browser write callbacks");
        return accepted === false ? 1 : 0;
      },
    },
    host,
  };

  let input = source;
  if (typeof Response !== "undefined" && source instanceof Response) {
    input = await source.arrayBuffer();
  }
  const instantiated = source instanceof WebAssembly.Module
    ? await WebAssembly.instantiate(source, imports)
    : await WebAssembly.instantiate(input, imports);
  instance = instantiated instanceof WebAssembly.Instance
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
    exports: instance.exports,
    start() {
      return instance.exports.kizu_start();
    },
  });

  function requireMemory() {
    if (!(memory instanceof WebAssembly.Memory)) {
      throw new WebAssembly.RuntimeError("Kizu memory is not available");
    }
    return memory;
  }

  function checkedRange(pointer, length) {
    const normalizedPointer = wasmU32(pointer, "pointer");
    const normalizedLength = wasmU32(length, "length");
    const end = normalizedPointer + normalizedLength;
    const size = requireMemory().buffer.byteLength;
    if (!Number.isSafeInteger(end) || end > size) {
      throw new WebAssembly.RuntimeError(
        `Kizu memory range ${normalizedPointer}..${end} exceeds ${size} bytes`,
      );
    }
    return { pointer: normalizedPointer, length: normalizedLength };
  }
}

function requireSynchronous(value, label) {
  if (value !== null && (typeof value === "object" || typeof value === "function") &&
      typeof value.then === "function") {
    throw new TypeError(`${label} must be synchronous`);
  }
}

function wasmU32(value, label) {
  if (typeof value !== "number" || !Number.isInteger(value) ||
      value < -0x80000000 || value > 0xffffffff) {
    throw new TypeError(`Kizu memory ${label} must be a WebAssembly i32`);
  }
  return value >>> 0;
}

function byteView(value) {
  if (value instanceof ArrayBuffer) {
    return new Uint8Array(value);
  }
  if (ArrayBuffer.isView(value)) {
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  }
  throw new TypeError("Kizu host writeBytes source must be an ArrayBuffer or view");
}
