# Browser HikeC / Wabt.js example

This example demonstrates the browser-side pipeline:

```text
Hike source form -> Wasm-HikeC.compile() -> WAT -> Wabt.js -> WebAssembly -> main()
```

Wabt.js is loaded from the pinned jsDelivr URL:

```text
https://cdn.jsdelivr.net/npm/wabt@1.0.39/index.js
```

The page automatically loads `wasm-hikec.wasm`. Build it from the repository's
compiler packages through Go-Hike mode and the wasm32 target:

```bash
make build
```

The build command is equivalent to:

```bash
go run ../../cmd/hikec build -target wasm32 -go-hike \
  -o wasm-hikec.wasm ../../cmd/wasm-hikec/main_wasm.go
```

The resulting module registers `globalThis.HikeWasmC` with this API:

```js
const result = await HikeWasmC.compile(source, { target: "wabt" });
// result = { wat: "(module ...)", runtimeJS: "..." }
```

`runtimeJS` is returned with every successful compilation and is evaluated as the generated Hike
runtime and loaded alongside the newly assembled module. Serve this directory
over HTTP because browsers block WASM and CDN loading from `file://` URLs:

```bash
make serve
# open http://localhost:8000/
```

The current repository build command can produce the bootstrap module and its
runtime with:

```bash
make build
```

The browser compiler accepts source text directly and does not read source
files. Package imports are not yet available in this in-memory entry point;
single-file Hike programs can be compiled directly.
