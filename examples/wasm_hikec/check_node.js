/* Node.js smoke test for the browser-side Wasm-HikeC bootstrap. */
const fs = require("fs");
const path = require("path");
const { HikeRuntime } = require("./runtime.js");

const root = __dirname;
const wasmPath = path.join(root, "wasm-hikec.wasm");
const source = `package main

func main() int { return 42 }
`;

global.fetch = async () => {
  const bytes = fs.readFileSync(wasmPath);
  return {
    ok: true,
    status: 200,
    headers: { get: () => String(bytes.length) },
    arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
  };
};

globalThis.__hikeWasmC = { source, echoSource: false };

(async () => {
  const runtime = new HikeRuntime({
    wasmUrl: wasmPath,
    onLog: (event, details) => console.error(`[runtime] ${event}`, details || ""),
  });
  const exports = await runtime.load();
  exports.main();
  if (typeof globalThis.__hikeWasmC.wat !== "string" || !globalThis.__hikeWasmC.wat.startsWith("(module")) {
    throw new Error(`compiler did not publish WAT: ${typeof globalThis.__hikeWasmC.wat} error=${globalThis.__hikeWasmC.error || ""}`);
  }
  console.log(`OK compile wat=${globalThis.__hikeWasmC.wat.length} bytes=${fs.statSync(wasmPath).size}`);
})().catch((error) => {
  console.error(error.stack || error);
  process.exitCode = 1;
});
