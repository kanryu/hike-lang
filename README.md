
# Hike (`hike-lang`)

A systems programming language with Go-like syntax that compiles to LLVM IR, generating C-ABI compliant shared libraries, standalone executables, and C/C++ headers.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![LLVM/Clang](https://img.shields.io/badge/Backend-LLVM%2FClang-blue?style=flat&logo=llvm)](https://llvm.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## Status & Environment

> **Note:** Hike is currently an experimental compiler under active development. Testing and verification have been performed primarily on **Windows using MinGW-w64 (`x86_64-w64-windows-gnu`) and Clang/LLVM**.

---

## Overview

Hike is an experimental systems language combining Go-style syntax and ergonomics with C-equivalent execution, direct C-ABI compatibility, and no garbage collection.

Rather than generating machine code or object files directly, the Hike compiler (`hikec`) acts strictly as a frontend that compiles source code into LLVM IR (`.ll`). Platform-specific binary formatting (PE/COFF, ELF), optimization passes (`-O3`), and linking are delegated entirely to Clang and LLVM.

The compiler builds standalone executables, C-compatible shared libraries (`.dll` / `.so`), and WebAssembly modules (`.wasm`). When exporting library functions, `hikec` automatically generates corresponding C/C++ header files (`.h`).

---

## Key Features

* **Go-Inspired Ergonomics**: Multi-return values, slices, structs, type inference (`:=`), and generic type parameters.
* **Zero Runtime Overhead**: No GC pauses, no background scheduler, and standard C memory layout.
* **Compile-Time Monomorphization**: Generic functions and types are fully specialized during compilation without dynamic dispatch penalties.
* **First-Class C-ABI Support**: Emits pure C-ABI binaries and automatically emits matching `.h` headers for C/C++ host integration.
* **2-Pass Stack Iterators**: Custom containers can provide zero-allocation `for-range` traversal using compile-time stack allocation (`alloca`).
* **Closures with Escape Analysis**: Lexical closures capture by reference. Variables escaping their stack lifetime are promoted to the heap, unified under a 2-word fat pointer ABI.
* **Built-in Module Management**: `hike.mod` handles package imports and directory tree remapping (`replace`)[cite: 2, 3].
* **Standalone WebAssembly Target**: Emits `wasm32-unknown-unknown` via Clang without requiring external WASI-SDK installations.
* **Source-Level DWARF Debugging**: Generates debug metadata for VS Code, GDB, and LLDB step debugging.

---

## Architecture

```text
[ .hike Source Code ]
           │
           ▼  (hikec: Go Frontend, Typecheck, Monomorphization)
  [ AST & Desugaring ]
     ├───► Auto-Generated C/C++ Header (.h) [Optional via -header]
     │
     ▼  (LLVM IR Codegen)
  [ Pure LLVM IR (.ll) ]
     │
     ▼  (Clang / LLVM Optimizer -O3)
  ┌─────────────────────────────────────────────────────────────┐
  │                                                             │
  ▼                                                             ▼
[ Standalone Executable ]                     [ Shared Library & Import Lib ]
(.exe / ELF binary)                           (.dll / .so / .dll.a)
                                                ├──► Native C/C++ Applications
                                                └──► Python (ctypes) / Node.js

```

---

## Requirements & Building

### Prerequisites

* **Go**: 1.21+


* **LLVM / Clang**: 15.0+ (`clang` and `lld` in your `PATH`)


* **Windows Toolchain**: MinGW-w64 GCC runtime (`x86_64-w64-windows-gnu`)


* **Make** (MinGW / MSYS2 / Linux / macOS)


* **Python**: 3.8+ (for integration test suites)



### Building the Compiler

```bash
git clone https://github.com/kanryu/hike-lang.git
cd hike-lang

# Run Unit Tests
go test ./...

# Build Compiler CLI Driver
go build -o hikec.exe ./cmd/hikec

```

---

## Language Tour & Syntax Reference

### 1. Variables, Types & Constants

Hike supports explicit type declarations and local type inference via `:=`. Primitive types map to fixed-width representations: `int` (`i64`), `float64` (`double`), `byte` (`u8`), `bool` (`i1`), and `string` (`i8*`).

```go
package main

var globalCounter int = 0
const MaxLimit int = 1024

func DemoVariables() {
    var a int = 42
    var b float64 = 3.14159
    var isEnabled bool = true
    var msg string = "Hello, Hike!"

    // Type inference
    count := 100
    ratio := 0.75
}

```

---

### 2. Pointers & Structs

Structs follow C memory layouts without hidden metadata or GC headers. Raw pointer operations do not incur runtime tracking.

```go
package main

type Point struct {
    X float64
    Y float64
}

type Rectangle struct {
    TopLeft     Point
    BottomRight Point
}

func CreatePoint(x float64, y float64) Point {
    return Point{X: x, Y: y}
}

// Pass by pointer to avoid copying
func OffsetPoint(p *Point, dx float64, dy float64) {
    p.X = p.X + dx
    p.Y = p.Y + dy
}

```

---

### 3. Functions & Multiple Return Values

Functions are first-class constructs and support multiple return values.

```go
package main

func SafeDivide(a int, b int) (int, bool) {
    if b == 0 {
        return 0, false
    }
    return a / b, true
}

func DemoFunctions() {
    result, ok := SafeDivide(10, 2)
    if ok {
        // Use result
    }
}

```

---

### 4. Control Flow

Hike supports `if` statements with short variable declarations, standard 3-clause `for` loops, `for-range` iterations, and `switch` statements.

```go
package main

func DemoControlFlow(values [5]int) int {
    sum := 0

    // 1. If with initializer
    if n := len(values); n > 0 {
        sum = sum + 1
    }

    // 2. 3-clause for loop
    for i := 0; i < 5; i = i + 1 {
        sum = sum + values[i]
    }

    // 3. for-range loop over fixed arrays
    for idx, val := range values {
        if val < 0 {
            continue
        }
        sum = sum + val
    }

    // 4. Switch statement
    status := 200
    switch status {
    case 200:
        sum = sum + 10
    case 404, 500:
        sum = sum - 1
    default:
        sum = 0
    }

    return sum
}

```

---

### 5. Arrays & Slices

Fixed-size arrays allocate contiguous memory inline. Slices provide dynamic views backed by a three-word header: pointer, length, and capacity.

```go
package main

func DemoArrays() {
    // Fixed-size array
    var arr [4]int
    arr[0] = 10
    arr[1] = 20

    primes := [3]int{2, 3, 5}

    // Slice expression
    sub := primes[1:3]
}

```

---

### 6. Zero-Cost Monomorphized Generics

Generic functions and struct definitions are specialized into concrete implementations at compile time, eliminating runtime dispatch overhead.

```go
package main

// Generic function with type union constraint
func Min[T int | float64](a T, b T) T {
    if a < b {
        return a
    }
    return b
}

// Generic struct
type Pair[K, V] struct {
    Key   K
    Value V
}

func DemoGenerics() {
    minInt := Min(10, 20)           // Specializes Min__int
    minFloat := Min(3.14, 2.71)     // Specializes Min__float64

    p := Pair[string, int]{Key: "hike", Value: 1}
}

```

---

### 7. Generic Hash Map (`std/maps`) & Indexing Sugar

Hike provides a generic hash map implementation (`std/maps`) with syntax sugar for indexing, membership testing, deletion, and `for-range` traversal.

```go
package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    // Initialize map with initial bucket capacity
    hmap := maps.New[string, int](8)

    // Subscript assignment sugar
    hmap["Tokyo"] = 1400
    hmap["Osaka"] = 880
    hmap["Nagoya"] = 230

    // Comma-ok lookup idiom
    if val, ok := hmap["Osaka"]; ok {
        printf("Osaka population: %d\n", val)
    }

    // for-range traversal (stack-allocated iterator)
    for city, population := range hmap {
        printf("  - %s: %d\n", city, population)
    }

    // Removal and length query
    delete(hmap, "Nagoya")
    printf("Remaining entries: %d\n", len(hmap))

    return 0
}

```

---

### 8. Custom Subscripting & 2-Pass Stack Iterator Protocol

Any user-defined struct can implement the **Map Behavior** protocol to enable indexing syntax (`obj[k]`, `obj[k] = v`, `len(obj)`, `delete(obj, k)`) and `for-range` loops.

To eliminate heap allocations during `for-range` iterations over custom containers, Hike uses a 2-pass protocol:

1. **Pass 1 (Size Probe)**: `InitIterator(nil)` returns the state buffer byte size.


2. **Stack Allocation**: The compiler issues an `alloca` instruction on the caller's stack frame.


3. **Pass 2 (Initialization)**: `InitIterator(buf)` initializes the allocated state buffer in-place.


4. **Iteration**: `Next(buf)` runs on each step, returning pointers to the current key/value and a continuation flag.



```go
package main

type Entry[K, V] struct {
    Key   K
    Value V
}

type CustomDictionary[K, V] struct {
    Entries []Entry[K, V]
}

func (d *CustomDictionary[K, V]) Set(key K, val V) {
    for i := 0; i < len(d.Entries); i = i + 1 {
        if d.Entries[i].Key == key {
            d.Entries[i].Value = val
            return
        }
    }
    d.Entries = append(d.Entries, Entry[K, V]{Key: key, Value: val})
}

func (d *CustomDictionary[K, V]) Get(key K) (V, bool) {
    for i := 0; i < len(d.Entries); i = i + 1 {
        if d.Entries[i].Key == key {
            return d.Entries[i].Value, true
        }
    }
    var zero V
    return zero, false
}

func (d *CustomDictionary[K, V]) Len() int {
    return len(d.Entries)
}

// --- 2-Pass Iterator Methods ---

type DictIterator struct {
    Index int
}

func (d *CustomDictionary[K, V]) InitIterator(buf *byte) int {
    if buf == nil {
        return 8 // sizeof(DictIterator)
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

func DemoMapBehavior() {
    var dict CustomDictionary[string, int]

    // Uses indexing sugar
    dict["itemA"] = 100
    dict["itemB"] = 200

    // Traverses with zero heap allocation
    for k, v := range dict {
        // ...
    }
}

```

---

### 9. First-Class Functions, Closures & Escape Analysis

Functions can be passed as values, returned from factories, or defined inline as closures.

* **Reference Capturing**: Captured variables maintain reference semantics across calls.


* **Automatic Heap Promotion**: Variables that escape their stack scope (such as parameters returned inside a closure) are automatically promoted to heap allocation (`malloc`).


* **Fat Pointer ABI**: Function values compile to a two-word structure:

$$\text{FuncValue} \implies \{ \text{i8* fn\_ptr},\, \text{i8* env\_ptr} \}$$



Top-level and stateless functions carry `env_ptr = null`. Closures carry a pointer to the captured environment.



```go
package main

// 'base' is promoted to heap by escape analysis
func makeAdder(base int) func(int) int {
    return func(n int) int {
        return base + n
    }
}

func main() int {
    add100 := makeAdder(100)
    result := add100(42) // => 142

    counter := 0
    increment := func() int {
        counter = counter + 1 // Mutates outer variable
        return counter
    }

    increment()
    increment()
    finalCount := increment() // finalCount == 3

    return 0
}

```

---

## C-ABI Export & Shared Library Generation

Any top-level function taking POD types or pointers can be exported to C-ABI. Passing `-header <name.h>` instructs `hikec` to emit a C/C++ header matching the exported functions.

### Hike Implementation (`libcalc.hike`)

```go
package main

type Vector2D struct {
    X float64
    Y float64
}

func Add[T int | float64](a T, b T) T {
    return a + b
}

func HikeAddInt(a int, b int) int {
    return Add(a, b)
}

func HikeAddFloat(a float64, b float64) float64 {
    return Add(a, b)
}

func HikeDotProduct(v1 *Vector2D, v2 *Vector2D) float64 {
    return (v1.X * v2.X) + (v1.Y * v2.Y)
}

```

### Compilation Commands

```bash
# 1. Emit LLVM IR and C/C++ header
hikec emit-ir -header libcalc.h -o libcalc.ll libcalc.hike

# 2. Build shared library and import library using Clang
clang -shared -O3 -Wl,--export-all-symbols -Wl,--out-implib,libcalc.dll.a libcalc.ll -o libcalc.dll

```

### Auto-Generated Header (`libcalc.h`)

```c
#ifndef HIKE_LIBCALC_H
#define HIKE_LIBCALC_H

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

typedef struct Vector2D {
    double X;
    double Y;
} Vector2D;

HIKE_API int64_t HikeAddInt(int64_t a, int64_t b);
HIKE_API double HikeAddFloat(double a, double b);
HIKE_API double HikeDotProduct(Vector2D* v1, Vector2D* v2);

#ifdef __cplusplus
}
#endif
#endif

```

### C++ Host Client (`main.cpp`)

```cpp
#include <iostream>
#include "libcalc.h"

int main() {
    int64_t sumInt = HikeAddInt(400, 600);
    double sumFloat = HikeAddFloat(1.414, 1.732);

    Vector2D v1 = { 3.0, 4.0 };
    Vector2D v2 = { 2.0, 5.0 };
    double dot = HikeDotProduct(&v1, &v2);

    std::cout << "SumInt: " << sumInt << std::endl;
    std::cout << "Dot: " << dot << std::endl;
    return 0;
}

```

Compile and link C++ directly:

```bash
clang++ -O3 main.cpp libcalc.dll.a -o client.exe
./client.exe

```

---

## Module Management (`hike.mod`)

Hike resolves local dependencies and package roots via `hike.mod` in the project root.

```text
module my-project

hike 0.1.0

# Remap import path to local directory
replace std/encoding/json => ../../std/encoding/json

```

`README.md`の既存の書式（`Working with JSON`や`Generic Hash Map`などのセクション構成）に合わせ、誇張表現を避けて機能とシグネチャ、使用例を端的にまとめた追加セクションです。

`## Working with JSON (std/encoding/json)`の直前（または直後）への配置を想定しています。

---

```markdown
## Working with Slices (`std/slices`)

The standard library provides generic collection operations for slices, conforming to standard functional and Go-like semantics. All operations are monomorphized at compile time with zero overhead.

### API Reference

* `Filter[T](s []T, predicate func(item T) bool) []T`: Returns a new slice containing elements that satisfy the predicate.
* `Map[T, U](s []T, transform func(item T) U) []U`: Returns a new slice where each element is mapped via `transform`.
* `IndexFunc[T](s []T, predicate func(item T) bool) int`: Returns the index of the first element satisfying the predicate, or `-1` if not found.
* `Find[T](s []T, predicate func(item T) bool) (T, bool)`: Returns the first matching element and a boolean flag indicating success.
* `SortFunc[T](s []T, cmp func(a T, b T) int)`: Performs an in-place quicksort using a three-way comparison function (`a < b` returns negative, `a == b` returns 0, `a > b` returns positive).
* `SortBy[T](s []T, less func(a T, b T) bool)`: Performs an in-place quicksort using a boolean comparison predicate (`a < b` returns true).

### Example

```go
package main

import (
    "std/slices"
)

func printf(format string, ...) int

func main() int {
    nums := []int{5, 2, 8, 1, 9, 4}

    // 1. Filter elements
    evens := slices.Filter[int](nums, func(x int) bool {
        return x % 2 == 0
    })

    // 2. Map elements
    mapped := slices.Map[int, int](evens, func(x int) int {
        return x * 10
    })

    // 3. Search elements
    val, ok := slices.Find[int](nums, func(x int) bool {
        return x > 5
    })
    idx := slices.IndexFunc[int](nums, func(x int) bool {
        return x > 5
    })
    printf("Find: %d (ok: %d), Index: %d\n", val, ok, idx)

    // 4. In-place sorting
    slices.SortFunc[int](nums, func(a int, b int) int {
        return a - b // Ascending
    })

    slices.SortBy[int](nums, func(a int, b int) bool {
        return a > b // Descending
    })

    return 0
}

```


---

## Working with JSON (`std/encoding/json`)

The standard library provides DOM parsing, traversal, mutation, serialization, and file I/O.

```go
package main

import (
    "std/encoding/json"
)

func printf(format string, ...) int

func main() int {
    // 1. Read file content
    content := json.ReadFile("data.json")
    if len(content) == 0 {
        printf("Failed to read data.json\n")
        return 1
    }

    // 2. Parse DOM tree
    doc := json.Parse(content)
    if doc == nil {
        printf("Failed to parse JSON\n")
        return 1
    }

    // 3. Access fields
    nameVal := doc.Get("name")
    verVal := doc.Get("version")
    if nameVal != nil {
        printf("Name: %s\n", nameVal.AsString())
    }
    if verVal != nil {
        printf("Version: %d\n", verVal.AsInt())
    }

    // 4. Mutate DOM
    doc.Set("modified_by", json.NewString("hikec"))
    newStats := json.NewObject()
    newStats.Set("active_threads", json.NewNumber(8.0))
    doc.Set("stats", newStats)

    // 5. Serialize and write back
    outStr := json.Stringify(doc)
    json.WriteFile("output.json", outStr)

    return 0
}

```

---

## WebAssembly Support

Hike compiles directly to `wasm32-unknown-unknown` using Clang without requiring WASI-SDK.

### Build Pipeline

```bash
# 1. Generate wasm32 IR
hikec -target wasm32 -o main.ll main.hike

# 2. Compile to standalone .wasm with Clang
clang --target=wasm32-unknown-unknown -O2 -nostdlib -Wl,--no-entry -Wl,--export-all -Wl,--allow-undefined main.ll -o app.wasm

# 3. Execute in Node.js host
node run_wasm.js

```

A minimal JavaScript runner binds memory and basic C symbols (`printf`, `malloc`, `memcpy`). Complete scripts are located in `examples/wasm`.

---

## Source-Level Debugging in VS Code

Passing `-g` instructs `hikec` to emit LLVM DWARF metadata, enabling source-level breakpoints, single-stepping, and variable inspection in VS Code using GDB or LLDB.

### `.vscode/tasks.json`

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Build Hike Debug Executable",
      "type": "shell",
      "command": "hikec ${file} -g -o${fileDirname}/main.ll && clang -g -O0 ${fileDirname}/main.ll -o${fileDirname}/app.exe",
      "options": {
        "cwd": "${fileDirname}"
      },
      "group": {
        "kind": "build",
        "isDefault": true
      },
      "problemMatcher": ["$gcc"]
    }
  ]
}

```

### `.vscode/launch.json`

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Hike Program (F5)",
      "type": "cppdbg",
      "request": "launch",
      "program": "${fileDirname}/app.exe",
      "args": [],
      "stopAtEntry": false,
      "cwd": "${fileDirname}",
      "environment": [],
      "externalConsole": false,
      "MIMode": "gdb",
      "miDebuggerPath": "gdb",
      "preLaunchTask": "Build Hike Debug Executable",
      "setupCommands": [
        {
          "description": "Enable pretty-printing for gdb",
          "text": "-enable-pretty-printing",
          "ignoreFailures": true
        }
      ]
    }
  ]
}

```

Pressing **`F5`** compiles the active `.hike` file and launches the debug session, supporting breakpoints, Step Over (`F10`), Step Into (`F11`), and variable inspection.

---

## CLI Reference (`hikec`)

```text
Usage: hikec <command> [options] <source.hike...>

Commands:
  emit-ir   Compiles Hike code into target LLVM IR (.ll) (default)
  build     Invokes Clang to compile Hike code directly to an executable or .wasm
  run       Builds into a temporary binary and executes it immediately

Options:
  -o <path>       Output binary or IR path
  -target <name>  Compilation target (windows, linux, darwin, wasm32)
  -header <path>  Export C/C++ header (.h)
  -g              Emit DWARF debug metadata
  -v, --verbose   Enable verbose logs

```

---

## Roadmap

* [ ] Dynamic interface dispatch (`vtable`)


* [ ] Memory management utilities (Arena allocator integrations)


* [ ] Package registry and remote dependency resolution


* [ ] Self-hosting compiler frontend in Hike



---

## License

MIT License

