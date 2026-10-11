const fs = require("fs");

const wasmPath = process.argv[2] || "cbuffer-e2e.wasm";
const wasmBuffer = fs.readFileSync(wasmPath);
let wasmMemory;
let heapPtr = 70000;

function memoryView() {
  return new Uint8Array(wasmMemory.buffer);
}

function readCString(ptr) {
  const memory = memoryView();
  let end = ptr;
  while (end < memory.length && memory[end] !== 0) end++;
  return new TextDecoder().decode(memory.subarray(ptr, end));
}

function printf(formatPtr, ...args) {
  let format = readCString(formatPtr);
  for (const arg of args) format = format.replace(/%d/, String(Number(arg)));
  process.stdout.write(format);
  return format.length;
}

const imports = {
  env: {
    malloc(size) {
      const ptr = heapPtr;
      heapPtr += (Number(size) + 7) & ~7;
      return ptr;
    },
    calloc(count, size) {
      const total = Number(count) * Number(size);
      const ptr = heapPtr;
      heapPtr += (total + 7) & ~7;
      memoryView().fill(0, ptr, ptr + total);
      return ptr;
    },
    free(_ptr) {},
    printf,
  },
};

(async () => {
  const { instance } = await WebAssembly.instantiate(wasmBuffer, imports);
  wasmMemory = instance.exports.memory;
  const exitCode = instance.exports.main(0, 0);
  process.exit(Number(exitCode));
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
