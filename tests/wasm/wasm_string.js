module.exports = async ({ runtime, exports, writeString, readString }) => {
  const input = 'Wasmからこんにちは';
  const encoded = new TextEncoder().encode(input);
  const inputPtr = runtime.malloc(encoded.length + 1);
  new Uint8Array(runtime.memory.buffer, inputPtr, encoded.length + 1).fill(0);
  writeString(runtime.memory, inputPtr, input);

  const stringResult = exports.TransformString(inputPtr, encoded.length);
  const cstringResult = exports.TransformCString(inputPtr, encoded.length);
  const jfuncResult = exports.main(0, 0);
  return `WASM_STRING=${readString(runtime.memory, stringResult)}\n` +
    `WASM_CSTRING=${readString(runtime.memory, cstringResult)}\n` +
    `WASM_JFUNC=${jfuncResult}\n`;
};
