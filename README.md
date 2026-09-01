
# Hike (`hike-lang`)

A systems programming language with Go-like syntax that compiles to LLVM IR, generating C-ABI compliant shared libraries, standalone executables, and C/C++ headers[cite: 3].

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![LLVM/Clang](https://img.shields.io/badge/Backend-LLVM%2FClang-blue?style=flat&logo=llvm)](https://llvm.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## Status & Environment

> **Note:** Hike is currently an experimental compiler under active development. Testing and verification have been performed primarily on **Windows using MinGW-w64 (`x86_64-w64-windows-gnu`) and Clang/LLVM**.

---

## Overview

Hike is an experimental language designed to combine Go-style syntax and ergonomics with direct C-ABI compatibility and no garbage collection[cite: 3].

Rather than generating machine code or object files directly, the Hike compiler (`hikec`) acts strictly as a frontend that compiles source code into clean LLVM IR (`.ll`)[cite: 3]. Platform-specific binary formatting (PE/COFF, ELF), optimization passes (`-O3`), and linking are delegated entirely to Clang and LLVM[cite: 3].

The compiler can output standalone executables, C-compatible shared libraries (`.dll` / `.so`), or WebAssembly modules (`.wasm`)[cite: 3]. When exporting library functions, `hikec` can automatically generate corresponding C/C++ header files (`.h`)[cite: 3].

---

## Features

* **Go-Style Syntax**: Supports type inference (`:=`), multiple return values, structs, and slice views[cite: 3].
* **No Runtime Garbage Collector**: No GC pauses and no background scheduler[cite: 3]. Memory layout follows standard C conventions[cite: 3].
* **Monomorphized Generics**: Generic functions and structs are specialized at compile time[cite: 3].
* **C-ABI Export & Header Generation**: Top-level functions using POD types or pointers export cleanly to C-ABI, with matching `.h` headers emitted on demand[cite: 3].
* **Stack-Based Iteration**: Map and container iteration protocols allocate state on the stack (`alloca`) rather than requiring dynamic heap allocation[cite: 3].
* **Closures with Escape Analysis**: Supports anonymous functions and closures[cite: 3]. Variables that outlive their scope are promoted to the heap, represented via a 2-word fat pointer `{fn_ptr, env_ptr}`[cite: 3].
* **Delegated Toolchain Pipeline**: Emits LLVM IR, allowing users to pass standard Clang/LLVM linker flags, link native `.rc` resources, or run LTO directly[cite: 3].
* **Standalone WebAssembly Target**: Compiles to `wasm32-unknown-unknown` via Clang `-nostdlib` without requiring third-party WASI toolchains[cite: 3].

---

## Architecture

```text
[ .hike Source Files ]
           │
           ▼  (hikec: Frontend, Typecheck, Monomorphization)
  [ AST & Desugaring ]
     ├───► C/C++ Header (.h) [Optional via -header]
     │
     ▼  (LLVM IR Codegen)
  [ LLVM IR (.ll) ]
     │
     ▼  (Clang / LLVM)
  ┌─────────────────────────────────────────────────────────────┐
  │                                                             │
  ▼                                                             ▼
[ Native Executable ]                         [ Shared Library & Import Lib ]
(.exe / ELF binary)                           (.dll / .so / .dll.a)
                                                ├──► C / C++ Applications
                                                └──► Python (ctypes) / Node.js

```

---

## Requirements & Building

### Prerequisites

* **Go**: 1.21+


* **LLVM / Clang**: 15.0+ (`clang` and `lld` in your `PATH`)


* **Windows Toolchain**: MinGW-w64 GCC runtime (`x86_64-w64-windows-gnu`)


* **Make** (MinGW / MSYS2 / Linux)


* **Python**: 3.8+ (for integration test scripts)



### Build the Compiler

```bash
git clone [https://github.com/kanryu/hike-lang.git](https://github.com/kanryu/hike-lang.git)
cd hike-lang

# Run test suite
go test ./...

# Build the CLI driver
go build -o hikec.exe ./cmd/hikec

```

---

## Quick Examples

### 1. Standalone Executable

```go
package main

func printf(format string, ...) int

func main() int {
    msg := "Hello from Hike"
    printf("%s\n", msg)
    return 0
}

```

Compile and run using the built-in driver:

```bash
hikec run main.hike

```

Or emit LLVM IR and compile with Clang directly:

```bash
hikec emit-ir -o main.ll main.hike
clang -O3 main.ll -o app.exe
./app.exe

```

---

### 2. Exporting a Shared Library with C Header

#### Hike Implementation (`mathlib.hike`)

```go
package main

type Matrix2x2 struct {
    M00 float64
    M01 float64
    M10 float64
    M11 float64
}

func HikeMatrixDeterminant(m *Matrix2x2) float64 {
    return (m.M00 * m.M11) - (m.M01 * m.M10)
}

```

#### Build Command (Windows MinGW / Clang)

```bash
# 1. Emit IR and generate C header
hikec emit-ir -header mathlib.h -o mathlib.ll mathlib.hike

# 2. Compile shared library and generate import library for C++
clang -shared -O3 -Wl,--export-all-symbols -Wl,--out-implib,libmathlib.dll.a mathlib.ll -o mathlib.dll

```

The generated `mathlib.h` contains standard C declarations:

```c
#ifndef HIKE_MATHLIB_H
#define HIKE_MATHLIB_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

#ifndef HIKE_API
  #if defined(_WIN32) || defined(__CYGWIN__)
    #define HIKE_API __declspec(dllimport)
  #else
    #define HIKE_API extern
  #endif
#endif

typedef struct Matrix2x2 {
    double M00;
    double M01;
    double M10;
    double M11;
} Matrix2x2;

HIKE_API double HikeMatrixDeterminant(Matrix2x2* m);

#ifdef __cplusplus
}
#endif
#endif

```

#### Consuming from C++ (`main.cpp`)

```cpp
#include <iostream>
#include "mathlib.h"

int main() {
    Matrix2x2 mat = { 1.0, 2.0, 3.0, 4.0 };
    double det = HikeMatrixDeterminant(&mat);
    std::cout << "Determinant: " << det << std::endl;
    return 0;
}

```

---

## Language Reference

### Variables & Basic Types

Primitive types map directly to native machine representations: `int` (`i64`), `float64` (`double`), `byte` (`u8`), `bool` (`i1`), and `string` (`i8*`).

```go
package main

var globalCounter int = 0
const MaxLimit int = 1024

func Demo() {
    var a int = 42
    var b float64 = 3.14159
    
    // Type inference
    count := 100
    ratio := 0.75
}

```

### Structs & Pointers

Structs use flat C memory layouts without hidden metadata or GC headers.

```go
package main

type Point struct {
    X float64
    Y float64
}

func OffsetPoint(p *Point, dx float64, dy float64) {
    p.X = p.X + dx
    p.Y = p.Y + dy
}

```

### Monomorphized Generics

Generic functions and types use compile-time monomorphization:

```go
package main

func Min[T int | float64](a T, b T) T {
    if a < b {
        return a
    }
    return b
}

func main() int {
    minInt := Min(10, 20)       // Emits Min__int
    minFloat := Min(3.14, 2.71) // Emits Min__float64
    return 0
}

```

### Map Behavior Protocol & 2-Pass Stack Iterator

Custom collections can implement the Map Behavior protocol to enable indexing syntax (`m[k]`, `m[k] = v`, `len(m)`, `delete(m, k)`) and `for-range` loops.

To avoid dynamic heap allocations during `for-range` traversal, the compiler uses a 2-pass stack allocation protocol:

1. Calls `InitIterator(nil)` to query required state buffer size.


2. Allocates the buffer on the stack frame via `alloca`.


3. Calls `InitIterator(buf)` to initialize the state.


4. Calls `Next(buf)` on each iteration step.



```go
type DictIterator struct {
    Index int
}

func (d *CustomDictionary[K, V]) InitIterator(buf *byte) int {
    if buf == nil {
        return 8 // Size of DictIterator state
    }
    it := (*DictIterator)(buf)
    it.Index = 0
    return 0
}

func (d *CustomDictionary[K, V]) Next(buf *byte) (*K, *V, bool) {
    it := (*DictIterator)(buf)
    if it.Index >= len(d.Entries) {
        return nil, nil, false
    }
    entry := &d.Entries[it.Index]
    it.Index = it.Index + 1
    return &entry.Key, &entry.Value, true
}

```

### Functions & Closures

Function values compile to an LLVM fat pointer: `{ i8* fn_ptr, i8* env_ptr }`.

* Stateless functions pass `null` as `env_ptr`.


* Closures pass a pointer to their captured environment.


* Variables that escape the stack lifetime are promoted to the heap (`malloc`).



```go
package main

func makeAdder(base int) func(int) int {
    return func(n int) int {
        return base + n // 'base' escapes and is promoted to heap
    }
}

func main() int {
    add100 := makeAdder(100)
    return add100(42) // 142
}

```

---

## Module Management (`hike.mod`)

Module boundaries and import path mappings are handled through `hike.mod`.

```text
module my-project

hike 0.1.0

# Map import paths to local directory trees
replace std/encoding/json => ../../std/encoding/json

```

---

## WebAssembly Support

Hike compiles directly to `wasm32-unknown-unknown` without WASI-SDK:

```bash
# 1. Generate wasm32 IR
hikec -target wasm32 -o main.ll main.hike

# 2. Compile to standalone .wasm with Clang
clang --target=wasm32-unknown-unknown -O2 -nostdlib -Wl,--no-entry -Wl,--export-all -Wl,--allow-undefined main.ll -o app.wasm

# 3. Run with Node.js
node run_wasm.js

```

---

## Debugging

Passing `-g` instructs `hikec` to emit LLVM DWARF metadata. This enables source-level step debugging in VS Code using GDB or LLDB:

```json
// .vscode/tasks.json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Build Hike Debug Executable",
      "type": "shell",
      "command": "hikec ${file} -g -o main.ll && clang -g -O0 main.ll -o app.exe"
    }
  ]
}

```

---

## CLI Reference

```text
Usage: hikec <command> [options] <source.hike...>

Commands:
  emit-ir   Compiles Hike code into LLVM IR (.ll) (default)
  build     Invokes Clang to compile Hike code to a binary (.exe / .wasm)
  run       Compiles and runs a Hike program immediately

Options:
  -o <path>       Output path
  -target <name>  Target platform (windows, linux, darwin, wasm32)
  -header <path>  Export C/C++ header (.h)
  -g              Emit DWARF debug metadata
  -v, --verbose   Enable verbose logs

```

---

## Roadmap

* [ ] Dynamic interface dispatch (`vtable`)


* [ ] Memory management utilities (Arena allocator abstractions)


* [ ] Integrated package resolution


* [ ] Self-hosting frontend



---

## License

MIT License
