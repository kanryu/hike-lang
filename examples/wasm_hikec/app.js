/* Browser-side HikeC -> WAT -> Wabt.js -> WASM pipeline. */
let wabt;
let compilerRuntime;
const $ = (id) => document.getElementById(id);
const status = (message) => { $("status").textContent = message; };

async function loadWabt() {
  if (typeof WabtModule !== "function") throw new Error("Wabt.js was not loaded");
  wabt = await WabtModule();
}

async function loadCompiler() {
  if (globalThis.HikeWasmC && typeof HikeWasmC.compile === "function") return;
  if (typeof HikeRuntime !== "function") throw new Error("runtime.js was not loaded");
  globalThis.__hikeWasmC = {};
  compilerRuntime = new HikeRuntime({
    wasmUrl: new URL("wasm-hikec.wasm", document.baseURI).href,
  });
  const exports = await compilerRuntime.load();
  globalThis.HikeWasmC = {
    async compile(source) {
      globalThis.__hikeWasmC.source = source;
      globalThis.__hikeWasmC.wat = "";
      exports.main();
      return { wat: globalThis.__hikeWasmC.wat, imports: compilerRuntime.imports() };
    },
  };
}

async function compileAndRun() {
  status("Compiling Hike source to WAT...");
  const result = await HikeWasmC.compile($("source").value, { target: "wabt" });
  if (result.error) throw new Error(result.error);
  if (typeof result.wat !== "string") throw new Error("compiler did not return WAT");
  $("wat").textContent = result.wat;

  status("Assembling WAT with Wabt.js...");
  const parsed = wabt.parseWat("hike-input.wat", result.wat, { multi_value: true });
  parsed.validate();
  const binary = parsed.toBinary({ log: false, write_debug_names: true });
  parsed.destroy();
  const instance = await WebAssembly.instantiate(binary.buffer, result.imports || { env: {} });
  const value = typeof instance.instance.exports.main === "function"
    ? instance.instance.exports.main() : "(no main export)";
  status(`WASM executed successfully\nmain() = ${String(value)}\nbytes = ${binary.buffer.byteLength}`);
}

$("run").addEventListener("click", () => compileAndRun().catch((error) => status(`Error: ${error.message}`)));
Promise.all([loadWabt(), loadCompiler()])
  .then(() => status("Ready. Press Compile and run."))
  .catch((error) => status(`Error: ${error.message}`));
