
# Hike (`hike-lang`)

A systems programming language with Go-like syntax that compiles to LLVM IR, generating C-ABI compliant shared libraries, standalone executables, and C/C++ headers.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![LLVM/Clang](https://img.shields.io/badge/Backend-LLVM%2FClang-blue?style=flat&logo=llvm)](https://llvm.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## Status & Environment

> **Note:** Hike is currently an experimental compiler under active development. Testing and verification have been performed primarily on **Windows using MinGW-w64 (`x86_64-w64-windows-gnu`) and Clang/LLVM**.

### Current project progress

The compiler currently supports LLVM IR generation, native and wasm32 builds,
generic type/function specialization, closures with escape analysis,
interfaces, collections, inline assembly, region allocation, and an automated
native/WASM test suite. Recent implementation work has also established a
length-aware string representation with shared substring views, reference
counting, and copy-on-write mutation.

The project remains experimental. Region allocation is an opt-in,
function-scoped arena strategy, and complete compiler-wide lifetime inference
for every temporary and shared string buffer is not finished. See the linked
design documents below for the exact implementation boundaries.

### Documentation overview

| Document | Summary |
| --- | --- |
| [`alloc-region.md`](alloc-region.md) | Region allocation, arena lifetime, escape promotion, diagnostics, examples, and limitations. |
| [`encoding.md`](encoding.md) | UTF-8 rules, string and buffer layouts, shared substring views, reference counting, and copy-on-write. |
| [`wasm.md`](wasm.md) | wasm32 target behavior, JavaScript runtime integration, exports, memory access, and testing. |
| [`concurrency.md`](concurrency.md) | Async tasks, channels, worker synchronization, closure transfer, and generated task bridges. |
| [`eventloop.md`](eventloop.md) | Event-loop abstractions built on channels, task invocation, and asynchronous result handling. |
| [`build-constraints-and-assembly.md`](build-constraints-and-assembly.md) | Build constraints and the inline assembly syntax and lowering rules. |
| [`without_cgo.md`](without_cgo.md) | C-ABI integration without cgo, `.syso` builds, and ownership rules at language boundaries. |
| [`gpu/webgpu/Hike.md`](gpu/webgpu/Hike.md) | WebGPU-oriented Hike integration and GPU programming notes. |

---

## Overview

Hike is an experimental systems language combining Go-style syntax and ergonomics with C-equivalent execution, direct C-ABI compatibility, and no garbage collection.

Rather than generating machine code or object files directly, the Hike compiler (`hikec`) acts strictly as a frontend that compiles source code into LLVM IR (`.ll`). Platform-specific binary formatting (PE/COFF, ELF), optimization passes (`-O3`), and linking are delegated entirely to Clang and LLVM.

The compiler builds standalone executables, C-compatible shared libraries (`.dll` / `.so`), and WebAssembly modules (`.wasm`). When exporting library functions, `hikec` automatically generates corresponding C/C++ header files (`.h`).

---

## Key Features

* **Go-Inspired Ergonomics**: Multi-return values, slices, structs, type inference (`:=`), and generic type parameters.
* **Zero Runtime Overhead**: No GC pauses, no always-on language scheduler, and standard C memory layout. Runtime support is emitted as internal LLVM functions, so unused facilities can be eliminated from the final binary; programs that do not use runtime-backed features need no runtime code.
* **Compile-Time Monomorphization**: Generic functions and types are fully specialized during compilation without dynamic dispatch penalties.
* **First-Class C-ABI Support**: Emits pure C-ABI binaries and automatically emits matching `.h` headers for C/C++ host integration.
* **2-Pass Stack Iterators**: Custom containers can provide zero-allocation `for-range` traversal using compile-time stack allocation (`alloca`).
* **Closures with Escape Analysis**: Lexical closures capture by reference. Variables escaping their stack lifetime are promoted to the heap, unified under a 2-word fat pointer ABI.
* **Built-in Module Management**: `hike.mod` handles package imports and directory tree remapping (`replace`).
* **Standalone WebAssembly Target**: Emits `wasm32-unknown-unknown` via Clang without requiring external WASI-SDK installations.
* **Optional Region Allocation**: `--alloc=region` groups eligible function-local allocations into bump arenas and releases them in O(1) at the region boundary.
* **Length-Aware Strings**: Native strings use a fat representation with a backing pointer, byte offset, and byte length; substring views share storage and writes use copy-on-write when necessary.
* **Source-Level DWARF Debugging**: Generates debug metadata for VS Code, GDB, and LLDB step debugging.

---

## Inline Assembly

Hike supports function-level LLVM inline assembly through the `__asm__ { ... }`
construct. The first line inside the block declares the operand mapping. The
following entries are the assembly template, output constraints, input
constraints, and clobber constraints:

```go
func encryptBlockFast(roundKeys *byte, dst *byte, src *byte) {
    __asm__{
        params: roundKeys, dst, src
        "movups (%2), %xmm0\n",
        "movups (%0), %xmm1\n",
        "pxor %xmm1, %xmm0\n",
        "movups %xmm0, (%1)\n",
        "",
        "r,r,r",
        "~{xmm0},~{xmm1},~{memory}"
    }
}
```

`%0`, `%1`, and `%2` refer to the operands listed after `params:`. They are
translated to LLVM inline-assembly operand references; other assembly text is
preserved. Register names such as `%xmm0` remain hardware register names.
Clobbers must declare registers and memory modified by the assembly. Native
instructions must be protected with build constraints such as `amd64`; a
portable implementation should be provided for targets such as wasm32.

The complete build-constraint and assembly rules are documented in
[`build-constraints-and-assembly.md`](build-constraints-and-assembly.md).

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

Hike supports explicit type declarations and local type inference via `:=`. Primitive types map to fixed-width representations: `int` (`i64`), `float64` (`double`), `byte` (`u8`), and `bool` (`i1`). The native `string` type is a length-aware fat value: `{ i8*, i32, i32 }`, occupying 16 bytes on 64-bit targets and 12 bytes on wasm32.

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

#### ConstGenerics

Hike also supports compile-time value parameters for generic types. A const
generic declaration must contain at least one type parameter, and const
parameters must appear after all type parameters. Const arguments are limited
to immediate primitive values or other const-generic parameters; ordinary
variables and expressions involving runtime values are rejected.

```go
type Matrix[T, Rows uint, Cols uint] struct {
    data [Rows * Cols]T
}

func MatrixValue() int {
    var m Matrix[int, 8, 8]
    return 8 * 8
}
```

`Matrix[int, 8, 8]` is materialized as a concrete compile-time
specialization. The dimensions are available to layout and indexing code, so
the resulting fixed-size matrix has no runtime dimension metadata.

```go
const Rows uint = 8
const Cols uint = 8
var m Matrix[int, Rows, Cols]
```

See the generic transformation and interface tests for additional examples of
type and const-parameter specialization.

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

func (d *CustomDictionary[K, V]) Delete(key K) {
    for i := 0; i < len(d.Entries); i = i + 1 {
        if d.Entries[i].Key == key {
            d.Entries[i] = d.Entries[len(d.Entries)-1]
            d.Entries = d.Entries[:len(d.Entries)-1]
            return
        }
    }
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

### 10. Concurrency, Channels & Streaming

`Async` schedules a closure for asynchronous execution and returns a typed one-shot task handle. The receive operator `<-` waits for completion and unpacks the return value or values.

```go
task := Async(func() int {
    return 40 + 2
})

result := <-task
```

Channels are typed synchronization queues. They can be used directly for producer/consumer pipelines, including staged streaming downloads:

```go
type Download struct {
    Blocks chan int
}

func (d *Download) InitIterator(buf *byte) int {
    return 0
}

func (d *Download) NextChannel(buf *byte) (chan int, bool) {
    return d.Blocks, true
}

func download() *Download {
    result := &Download{Blocks: make(chan int, 3)}
    Async(func() int {
        result.Blocks <- 10
        result.Blocks <- 20
        result.Blocks <- 30
        return 0
    })
    return result
}

func consume() int {
    total := 0
    stream := download()
    for block := range <-stream {
        total = total + block
    }
    return total
}
```

The `InitIterator` and `NextChannel` method pair is the `AsyncIterable[T]` protocol defined in `std/collections`. `for value := range <-stream` receives blocks in channel order and can stop early with `break`.

Current limitation: asynchronous range lowering resolves these methods on the concrete stream type. Interface-typed streams such as a value returned as `collections.AsyncIterable[int]` are not yet supported reliably and should remain concrete until interface dispatch is extended for this path.

---

### 11. First-Class Functions, Closures & Escape Analysis

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

## Direct C Interoperability (Zero-Overhead C-ABI)

Unlike Go, which requires `cgo`, preambles, wrapper generation, and runtime stack-switching overhead, Hike interacts with C at zero runtime cost.

Because `hikec` compiles directly to standard LLVM IR and delegates code generation to Clang, a function declaration without a body (`func ...`) is emitted as a standard external C-ABI symbol declaration (`declare`).

### 1. Standard C Runtime (libc)

You can call standard C library functions simply by declaring their signatures in your `.hike` source. The Clang backend automatically links the C runtime (libc / MinGW-w64 / MSVCRT) and resolves the symbols directly.

```go
package main

// Declare standard C library functions directly
func printf(format string, ...) int
func puts(str string) int

func main() int {
    puts("Hello directly from C libc!")
    printf("Formatted number: %d\n", 42)
    return 0
}

```

### 2. Third-Party C Libraries

To integrate third-party C libraries (e.g., SQLite, Raylib, OpenSSL):

1. **Declare the C API**: Write matching function signatures without bodies in your Hike code.


2. **Link via Clang**: Pass `-l<lib>` or direct paths to static (`.a` / `.lib`) or dynamic (`.so` / `.dll` / `.dylib`) libraries during build.



#### Example (`main.hike`)

```go
package main

// External library declaration (e.g., libcurl or a custom C library)
func my_c_library_init() int
func my_c_calculate(a int, b int) int

func main() int {
    if my_c_library_init() != 0 {
        return 1
    }
    res := my_c_calculate(10, 20)
    return 0
}

```

#### Build & Link Command

```bash
# 1. Emit LLVM IR
hikec emit-ir -o main.ll main.hike

# 2. Compile and link external C library with Clang
clang -O3 main.ll -L/path/to/libs -lmy_c_library -o app.exe

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



---


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

Hike compiles directly to `wasm32-unknown-unknown` through Clang without a
WASI SDK or an external libc. The compiler emits internal LLVM runtime
functions for memory, strings, maps, and other built-ins, then generates a
JavaScript runtime for the selected host mode.

The generated WASM modules are intended to run across WebAssembly host
environments, including web browsers, Node.js, and other WASM runtimes that
provide the standard WebAssembly JavaScript API. In a web browser, a module
can be used immediately by referencing the generated `runtime.js` and the
`.wasm` binary from an HTML page; the runtime handles instantiation and exposes
the Hike exports to the page's JavaScript.

### Build and run

```bash
# Build main.wasm and generate runtime.js beside it.
hikec build -target wasm32 -o main.wasm main.hike

# The same command can be executed through Node.js by the CLI runner.
hikec run -target wasm32 main.hike
```

`build` produces the `.wasm` module and automatically writes `runtime.js` in
the output directory. `run` builds a temporary module and starts Node.js with
the generated runtime. This is the same host model used by the automated WASM
tests.

### Normal and concurrent runtimes

Use `-wasm-mode normal` for ordinary exported functions and
`-wasm-mode concurrent` when the program uses asynchronous tasks, workers, or
directed JavaScript callbacks:

```bash
hikec build -target wasm32 -wasm-mode normal -o app.wasm main.hike
hikec build -target wasm32 -wasm-mode concurrent -o app.wasm main.hike
hikec emit-js -target wasm32 -wasm-mode concurrent -o runtime.js main.hike
```

The generated runtime is reusable from both browsers and Node.js. Browser code
can instantiate the module and call exported `cfunc` functions in response to
events. Node.js tests load the same generated runtime with `require` or ES
module import and invoke the exports directly. Each WASM test keeps its
JavaScript driver separate while sharing the runtime implementation.

### JavaScript and Hike strings

WASM host functions receive string data through the generated ABI bridge. Hike
code can expose byte-pointer and length parameters when a direct host buffer
interface is preferred, then construct a Hike string from that pair:

```go
cfunc TransformString(input *byte, length int) cstring {
    value := string(input, length)
    return cstring(value + " [wasm]")
}
```

The runtime provides UTF-8 encoding/decoding helpers and access to linear
memory. Hike `int` values use WebAssembly `i64` and should be passed from
JavaScript as `BigInt`; `int32`, `byte`, and `bool` use `i32`-compatible values.
Hike strings are length-aware fat values internally, while C-facing exports
are adapted to pointer-based ABI values by the compiler.

### JavaScript host example

```javascript
import { HikeRuntime } from "./runtime.js";
import fs from "node:fs/promises";

const runtime = new HikeRuntime();
const wasm = await runtime.load("main.wasm");
const result = wasm.AddNumbers(1234, 5678);
console.log(result);
```

For browser integration, instantiate the generated runtime after loading the
`.wasm` asset and call exported functions from event handlers. For Node.js,
the `tests/wasm` suite provides examples covering maps, strings, concurrent
workers, and JavaScript functions (`jfunc`).

The full host API, memory model, and integration examples are documented in
[`wasm.md`](wasm.md).

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
Usage: hikec <command> [options] <source.hike... | directory>

Commands:
  go        Compile a directory of .go.hike files into one .syso object
  emit-ir   Generate target LLVM IR (.ll) (default for a source input)
  emit-js   Generate the WebAssembly JavaScript runtime.js bridge
  build     Compile Hike source into a native or WebAssembly binary via Clang
  run       Build and immediately execute a native or WebAssembly program

Common options:
  -o <path> / -o=<path>
                  Output path. For emit-ir this is .ll; for emit-js it is
                  runtime.js; for build it is the executable or .wasm.
  -target <name> / --target=<name>
                  Target triple or shorthand: windows, windows-msvc, linux,
                  darwin, wasm32, or wasm64.
  -v / --verbose  Enable verbose logging.
  -vv / --vv      Enable detailed instruction-level logging.

Options for emit-ir:
  -header <path> / --header=<path>
                  Generate a C/C++ header for exported declarations.
  -wasm-mode <mode> / --wasm-mode=<mode>
                  WebAssembly runtime mode: normal or concurrent.
  --alloc=region / -alloc=region
                  Enable optional function-scoped region allocation.
  -cflags <flags> / -cflags=<flags>
                  Accepted for command compatibility; native linker flags are
                  applied by build rather than emit-ir.

Options for emit-js:
  -wasm-mode <mode> / --wasm-mode=<mode>
                  Generate the normal or concurrent JavaScript runtime.
  --alloc=region / -alloc=region
                  Compile the source with region mode enabled.

Options for build:
  -wasm-mode <mode> / --wasm-mode=<mode>
                  WebAssembly runtime mode: normal or concurrent.
  --alloc=region / -alloc=region
                  Enable optional function-scoped region allocation.
  -cflags <flags> / -cflags=<flags>
                  Extra flags passed directly to Clang.
  -g              Generate DWARF debug metadata and use a debug-friendly
                  native compilation configuration.

Options for run:
  -wasm-mode <mode> / --wasm-mode=<mode>
                  Forward the normal or concurrent mode to the WASM build.
  --alloc=region / -alloc=region
                  Forward region allocation mode to the build.
  -cflags <flags> / -cflags=<flags>
                  Forward additional Clang flags to the build.
  -g              Forward debug information generation to the build.

Options for go:
  -o <path> / -o=<path>
                  Output the aggregated `.syso` object path.
  -target <name> / --target=<name>
                  Target platform or triple for the generated object.
  -v / --verbose  Enable verbose logging.
  -vv / --vv      Enable detailed logging.

Unknown non-option arguments are treated as source files or directories. The
`emit-ir`, `emit-js`, and `build` commands require at least one input source.

```

---

## Roadmap

* [ ] Interface-valued `AsyncIterable` dispatch for `for value := range <-stream`


* [x] Optional function-scoped region allocation (`--alloc=region`)
* [ ] Complete lexical lifetime inference and release handling for every temporary/shared string buffer


* [ ] Package registry and remote dependency resolution


* [ ] Self-hosting compiler frontend in Hike



---

## License

MIT License
