const fs = require('fs');
const path = require('path');

const wasmPath = process.argv[2];
const scriptPath = process.argv[3];
if (!wasmPath || !scriptPath) throw new Error('usage: node langwasm.js <module.wasm> <test-script.js>');

// hikec emits runtime.js next to the WASM module. Load that generated file so
// the E2E runner exercises the same host ABI as a real WASM application.
const runtimePath = path.join(path.dirname(wasmPath), 'runtime.js');
const runtimeModule = require(runtimePath);

// HikeRuntime uses fetch() because the browser runtime loads URLs. Provide a
// small file-backed fetch for Node without changing the generated browser API.
global.fetch = async (url) => {
  const filename = String(url).startsWith('file:') ? new URL(url) : url;
  const data = fs.readFileSync(filename);
  return {
    ok: true,
    status: 200,
    headers: { get: () => String(data.length) },
    arrayBuffer: async () => data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength),
  };
};

function writeString(memory, ptr, value) {
  const encoded = new TextEncoder().encode(value);
  new Uint8Array(memory.buffer, ptr, encoded.length).set(encoded);
  return encoded.length;
}

function readString(memory, ptr) {
  const data = new Uint8Array(memory.buffer);
  let end = ptr;
  while (end < data.length && data[end] !== 0) end++;
  return new TextDecoder().decode(data.subarray(ptr, end));
}

(async () => {
  const runtime = new runtimeModule.HikeRuntime({ wasmUrl: wasmPath });
  const exports = await runtime.load();
  const test = require(path.resolve(scriptPath));
  const result = await test({ runtime, exports, writeString, readString });
  if (result !== undefined) process.stdout.write(String(result));
})();
