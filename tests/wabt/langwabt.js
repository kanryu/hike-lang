const fs = require('fs');
const path = require('path');

const wasmPath = process.argv[2];
const scriptPath = process.argv[3];
if (!wasmPath || !scriptPath) throw new Error('usage: node langwabt.js <module.wasm> <test-script.js>');

const runtimePath = path.join(path.dirname(wasmPath), 'runtime.js');
const runtimeModule = require(runtimePath);

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

(async () => {
  const runtime = new runtimeModule.HikeRuntime({ wasmUrl: wasmPath });
  const exports = await runtime.load();
  const test = require(path.resolve(scriptPath));
  const result = await test({ runtime, exports });
  if (result !== undefined) process.stdout.write(String(result));
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
