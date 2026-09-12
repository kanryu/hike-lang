# Hike WebAssembly (Wasm) Guide

The Hike compiler provides native, first-class support for the `wasm32` WebAssembly target. By completely eliminating binary bloat, complex glue-code management, and external C runtime (libc) dependencies, Hike delivers an **ultra-compact footprint starting at just 2.56 KB** along with **fully automated JavaScript runtime (`runtime.js`) generation** in a single compilation step.

---

## 1. Comparison with Existing Toolchains

Traditional language stacks force significant trade-offs when targeting high-performance browser execution. Hike combines an intuitive, Go-like development workflow with a radically lightweight binary profile.

| Feature | Standard Go (`GOOS=js`) | Rust (`wasm-bindgen`) | C/C++ (Emscripten) | **Hike (`hikec`)** |
| --- | --- | --- | --- | --- |
| **Minimum Binary Size** | ~2.5 MB – 3.0 MB | ~5 KB – 20 KB | Tens of KB – Several MB | **2.56 KB+** |
| **External Libc Dependency** | None (bundled runtime) | None (in `no_std` mode) | Virtual POSIX / heavy libc | **Completely Zero (LLVM IR built-in)** |
| **JS Glue Code** | Requires `wasm_exec.js` | Multi-step (`wasm-pack`, etc.) | Heavy JS wrappers | **Auto-generated (`runtime.js`)** |
| **Build Toolchain** | `go build` | `cargo` + `wasm-bindgen` | `emcc` + complex linker flags | **Single `hikec build` command** |
| **Memory / String Model** | Heavyweight GC + Scheduler | Lifetimes / Ownership rules | Manual pointers (null-terminated) | **Lightweight Slices (`{ ptr, len, cap }`)** |

---

## 2. Core Architectural Highlights

### 2.1 Completely Libc-Free Architecture

Standard Wasm binaries often balloon in size because the language runtime silently depends on standard C library routines (`memcpy`, `strlen`, `strcmp`, etc.).
Hike replaces all low-level memory and string operations with internal **LLVM IR functions (`define internal`)**. Because no external libc functions are linked, the resulting binary contains zero POSIX emulation overhead.

### 2.2 Single-Command Dual Output

Without requiring secondary generators or Node.js-based CLI utilities, `hikec` directly emits both the WebAssembly binary and the matching JavaScript bridge file in a single invocation.

```bash
hikec build -target wasm32 main.hike -o main.wasm

```

Running this command produces two artifacts in the output directory:

* `main.wasm`: The optimized, standalone WebAssembly binary.
* `runtime.js`: A ready-to-use bridge class handling instantiation, linear memory allocation, and UTF-8 string encoding/decoding.

### 2.3 On-Demand Wasm

Unlike traditional Wasm setups that run a monolithic, run-to-completion `main()` process or monopolize the browser rendering loop, Hike treats the WebAssembly module as a resident, stateful computation engine.
JavaScript can invoke exposed Wasm functions on demand just like regular object methods in response to UI actions, network events, or timer ticks.

---

## 3. Implementation and Quickstart

### 3.1 Hike Source Code (`main.hike`)

Declare JavaScript DOM APIs via `extern func`, and export Hike methods to the host environment using `cfunc`.

```go
package main

// Host DOM and system APIs provided by JavaScript
extern func js_log(msg string, len int)
extern func js_set_text(id string, id_len int, text string, text_len int)
extern func js_append_text(id string, id_len int, text string, text_len int)
extern func js_set_badge_color(id string, id_len int, color string, color_len int)

func Log(msg string) {
	js_log(msg, len(msg))
}

func SetText(elementId string, text string) {
	js_set_text(elementId, len(elementId), text, len(text))
}

// Compute workload (recursive Fibonacci)
func Fib(n int) int {
	if n <= 1 {
		return n
	}
	return Fib(n-1) + Fib(n-2)
}

// -----------------------------------------------------------------------------
// Functions exported to the JavaScript host (cfunc)
// -----------------------------------------------------------------------------

// Invoked automatically by runtime.js upon instantiation
cfunc InitApp() {
	Log("Hike WebAssembly core initialized.")
	SetText("status-badge", "Running (Wasm Active)")
	js_set_badge_color("status-badge", 12, "#10b981", 7)
}

// Callable on-demand from JavaScript
cfunc RunComputation(n int) int {
	return Fib(n)
}

// Pure math helper
cfunc AddNumbers(a int, b int) int {
	return a + b
}

// Event handler mutating DOM state directly from Wasm
cfunc AppendLogMessage(actionType int) {
	if actionType == 1 {
		msg := "\n[Wasm Event] User clicked Action A: Memory layout validated."
		js_append_text("wasm-log-box", 12, msg, len(msg))
	} else {
		msg := "\n[Wasm Event] User clicked Action B: Slice buffer manipulation complete."
		js_append_text("wasm-log-box", 12, msg, len(msg))
	}
}

```

### 3.2 Compilation

```bash
hikec build -target wasm32 main.hike -o main.wasm

```

This compiles the source into `main.wasm` (~2.56 KB) and automatically generates `runtime.js` in the same directory.

---

## 4. Integration

### 4.1 In the Browser (`index.html`)

Import the generated `runtime.js` to instantiate and interact with the module immediately.

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Hike Wasm Demo</title>
</head>
<body>
    <div id="status-badge">Initializing...</div>
    <pre id="wasm-log-box">[Ready]</pre>

    <button id="btn-fib">Run Fib(35)</button>
    <button id="btn-add">Add 1234 + 5678</button>

    <script src="runtime.js"></script>
    <script>
        const runtime = new HikeConcurrentRuntime();
        let wasmExports = null;

        async function start() {
            // Load and instantiate the module (InitApp runs automatically)
            wasmExports = await runtime.load('main.wasm');

            document.getElementById('btn-fib').onclick = () => {
                // Pass BigInt for Hike's 64-bit int (i64)
                const result = wasmExports.RunComputation(35);
                console.log("Fib(35) =", result);// 9227465
            };

            document.getElementById('btn-add').onclick = () => {
                const sum = wasmExports.AddNumbers(1234, 5678);
                console.log("Sum =", sum);
            };
        }

        window.addEventListener('DOMContentLoaded', start);
    </script>
</body>
</html>

```

### 4.2 In Node.js

Run the identical `.wasm` binary on the server or in CLI workflows without building native addons (`node-gyp`).

```javascript
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Import the auto-generated runtime
import './runtime.js';

async function run() {
    const wasmBuffer = await fs.readFile(
        path.join(path.dirname(fileURLToPath(import.meta.url)), 'main.wasm')
    );
    const runtime = new HikeConcurrentRuntime();
    
    const { instance } = await WebAssembly.instantiate(wasmBuffer, runtime.getImportObject());
    const wasm = instance.exports;

    if (typeof wasm.InitApp === 'function') {
        wasm.InitApp();
    }

    const res = wasm.RunComputation(30);
    console.log(`Computed Fib(30) in Node.js: ${res}`);
}

run();

```

---

## 5. Type System Interoperability Specifications

Hike and JavaScript communicate according to the following conventions:

* **Standard Integers (`int`, `int64`, `uint64`)**
Represented as `i64` in WebAssembly. When passing or receiving these values in JavaScript, use native `BigInt` literals (e.g., `35n`).
* **Compact Integers (`int32`, `bool`, `byte`)**
Represented as `i32` in WebAssembly. These map directly to standard JavaScript `Number` or `Boolean` types without conversion.
* **Strings (`string`)**
Use `runtime.readString(ptr)` (or direct typed array view from `runtime.memory`) to decode strings from WebAssembly memory.
* **Dynamic Allocation (`malloc` / `calloc`)**
`runtime.js` provides a minimal 8-byte aligned bump allocator on top of `WebAssembly.Memory`. Memory automatically expands via `memory.grow` when the heap boundary is reached, eliminating any need for an external allocator package.