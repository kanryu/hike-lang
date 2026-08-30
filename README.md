# Hike (`hike-lang`) 🎋

A systems programming language with Go-like ergonomics that compiles via LLVM IR into zero-overhead, C-ABI compliant shared libraries with automatic C/C++ header generation.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![LLVM/Clang](https://img.shields.io/badge/Backend-LLVM%2FClang-blue?style=flat&logo=llvm)](https://llvm.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 📖 Overview

**Hike** is a systems programming language designed to combine the clean syntax and developer productivity of Go with the zero-overhead, predictable performance of C/C++. It completely eliminates garbage collection (GC) pauses, green-thread runtimes, and bloated runtime layers.

Operating as an expressive, high-level frontend that emits clean LLVM IR, Hike **hitchhikes** on the mature optimization and code generation pipelines of LLVM and Clang. It can build **standalone native executables** as well as **ultra-lightweight shared libraries (`.dll` / `.so`)** with auto-generated C/C++ headers for seamless cross-language integration.

---

## ✨ Key Features

* **Go-Inspired Ergonomics**: Multi-return values, slices, structs, type inference (`:=`), and clean syntax familiar to Go developers.
* **Dual Target Flexibility**: Compiles into standalone native executables or ultra-lightweight shared libraries (`.dll` / `.so`).
* **Zero-Cost Monomorphized Generics**: Full compile-time specialization of generic functions and types without dynamic dispatch penalties.
* **Zero Runtime Overhead**: No GC pauses, no background runtime scheduler, and C-equivalent memory layout and execution speed.
* **First-Class C-ABI Provider**: Emits pure C-ABI compliant binaries (POD structs, pointer passing, plain functions) and automatically generates matching C/C++ header files (`.h`).
* **Hitchhiking Clang/LLVM Infrastructure**: Delegates platform-specific binary formatting (PE/COFF, ELF, Mach-O) and deep optimization passes (`-O3`, SIMD vectorization) directly to Clang/LLVM.

---

## 🛠️ Architecture

```text[ .hike Source Code ]
           │
           ▼  (hikec: Go Frontend & Semantic Analysis)
  [ AST & Monomorphization ]
     ├───► Auto-Generated C/C++ Header (.h) [Optional / Library Mode]
     │
     ▼  (LLVM IR Codegen)
  [ Pure LLVM IR (.ll) ]
     │
     ▼  (Clang / LLVM Optimizer -O3)
  ┌─────────────────────────────────────────────────────────────┐
  │                                                             │
  ▼                                                             ▼
[ Standalone Executable ]                     [ Shared Library & Import Lib ]
(.exe / native binary)                        (.dll / .so / .dll.a)
  │                                             │
  ▼                                             ├──► Native C/C++ Applications
Direct CLI Execution                            └──► Python (ctypes) / Rust / Node.js
```

---

## 🚀 Quick Start

### 1. Requirements

* **Go**: 1.21+
* **LLVM / Clang**: 15.0+ (`clang` and `clang++` must be in your `PATH`)
* **Make** (MinGW / MSYS2 / Linux / macOS)
* **Python**: 3.8+ (for integration tests)

### 2. Build the Compiler

```bash
git clone [https://github.com/kanryu/hike-lang.git](https://github.com/kanryu/hike-lang.git)
cd hike-lang

# Run Unit Tests
go test ./...

# Build Compiler CLI
go build -o hikec ./cmd/hikec

```

### 3. Build a Standalone Executable

```bash
# Compile Hike code to LLVM IR
hikec main.hike -o main.ll

# Compile to native executable with Clang
clang -O3 main.ll -o app.exe

# Run
./app.exe

### 4. Run Shared Library Example

The `examples/shared` directory contains a complete end-to-end example demonstrating shared library generation and consumption from both Python (`ctypes`) and C++.

```bash
cd examples/shared

# Run Python ctypes test
make test

# Run C++ client test (with auto-generated header)
make testcpp

```

---


## 📚 Language Tour & Syntax Reference

Hike adopts Go's clean, minimalist syntax while compiling down to zero-overhead LLVM IR. Below is a comprehensive guide to the language features and syntax supported by Hike.

---

### 1. Variables, Types & Constants

Hike supports explicit type declarations as well as local type inference via `:=`.

```go
package main

// Primitive types: int (i64), float64 (double), byte (u8), bool (i1), string (i8*)
var globalCounter int = 0
const MaxLimit int = 1024

func DemoVariables() {
    // Explicit type declaration
    var a int = 42
    var b float64 = 3.14159
    var isEnabled bool = true
    var msg string = "Hello, Hike!"

    // Type inference (short declaration)
    count := 100
    ratio := 0.75
}

```

---

### 2. Pointers & Structs

Like C and Go, Hike provides plain old data (POD) structures and raw pointer arithmetic without GC tracking.

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

// Pointer passing avoids struct copying
func OffsetPoint(p *Point, dx float64, dy float64) {
    p.X = p.X + dx
    p.Y = p.Y + dy
}

```

---

### 3. Functions & Multiple Return Values

Functions are first-class constructs and support multiple return values, commonly used for status and result pairs.

```go
package main

// Multiple return values
func SafeDivide(a int, b int) (int, bool) {
    if b == 0 {
        return 0, false
    }
    return a / b, true
}

func DemoFunctions() {
    result, ok := SafeDivide(10, 2)
    if ok {
        // Proceed with result
    }
}

```

---

### 4. Control Flow

Hike supports standard Go control flow constructs, including `if` with initializers, three-clause `for`, range loops, and switches.

```go
package main

func DemoControlFlow(values [5]int) int {
    sum := 0

    // 1. If statement with optional initializer
    if n := len(values); n > 0 {
        sum = sum + 1
    }

    // 2. Standard 3-clause for loop
    for i := 0; i < 5; i = i + 1 {
        sum = sum + values[i]
    }

    // 3. For-range loop (over arrays/slices)
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

Hike features both fixed-size arrays and dynamic slice views.

```go
package main

func DemoArrays() {
    // Fixed-size array
    var arr [4]int
    arr[0] = 10
    arr[1] = 20

    // Array literal
    primes := [3]int{2, 3, 5}

    // Slice expression (view into memory)
    // Slice header contains: pointer, length, capacity
    sub := primes[1:3]
}

```

---

### 6. Zero-Cost Monomorphized Generics

Hike implements compile-time monomorphization for generic functions and structs. Each instantiation generates specialized, concrete LLVM IR code with zero runtime dispatch cost.

```go
package main

// Generic function constrained by type union
func Min[T int | float64](a T, b T) T {
    if a < b {
        return a
    }
    return b
}

func DemoGenerics() {
    // Automatically specializes Min__int and Min__float64
    minInt := Min(10, 20)
    minFloat := Min(3.14, 2.71)
}

```

---

### 7. C-ABI Interoperability & Shared Library Export

Any top-level function taking POD types or pointers is automatically eligible for C-ABI export. When compiled with `-header <name.h>`, `hikec` generates the corresponding C/C++ header.

#### Hike Implementation (`mathlib.hike`)

```go
package main

type Matrix2x2 struct {
    M00 float64
    M01 float64
    M10 float64
    M11 float64
}

// C-ABI Exported function
func HikeMatrixDeterminant(m *Matrix2x2) float64 {
    return (m.M00 * m.M11) - (m.M01 * m.M10)
}

```

#### Auto-Generated C/C++ Header (`mathlib.h`)

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

#### Native C++ Consumption (`main.cpp`)

```cpp
#include <iostream>
#include "mathlib.h"

int main() {
    Matrix2x2 mat = { 1.0, 2.0, 3.0, 4.0 };
    double det = HikeMatrixDeterminant(&mat);
    std::cout << "Determinant: " << det << std::endl; // => -2.0
    return 0;
}

```



## 💡 Example Walkthrough

### 1. Write Hike Code (`libcalc.hike`)

```go
package main

type Vector2D struct {
    X float64
    Y float64
}

// Zero-cost monomorphized generic function
func Add[T int | float64](a T, b T) T {
    return a + b
}

func HikeAddInt(a int, b int) int {
    return Add(a, b)
}

func HikeAddFloat(a float64, b float64) float64 {
    return Add(a, b)
}

// C-ABI exported function operating on struct pointers
func HikeDotProduct(v1 *Vector2D, v2 *Vector2D) float64 {
    return (v1.X * v2.X) + (v1.Y * v2.Y)
}

```

### 2. Compile & Generate C/C++ Header

```bash
# Generate LLVM IR and C/C++ Header simultaneously
hikec libcalc.hike -o libcalc.ll -header libcalc.h

# Build Shared Library with Clang
clang -shared -O3 -Wl,--export-all-symbols -Wl,--out-implib,libcalc.dll.a libcalc.ll -o libcalc.dll

```

Auto-generated `libcalc.h`:

```c
/*
 * ========================================================
 * Powered by Hike Language
 * Auto-generated C/C++ Header File
 * ========================================================
 */

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
#endif /* HIKE_LIBCALC_H */

```

### 3. Consume from C++ Host (`test_client.cpp`)

```cpp
#include <iostream>
#include "libcalc.h" // Auto-generated by hikec

int main() {
    std::cout << "=== Running C++ Host with Hike Generated Header ===" << std::endl;

    int64_t sumInt = HikeAddInt(400, 600);
    std::cout << "HikeAddInt(400, 600) = " << sumInt << std::endl;

    double sumFloat = HikeAddFloat(1.414, 1.732);
    std::cout << "HikeAddFloat(1.414, 1.732) = " << sumFloat << std::endl;

    Vector2D v1 = { 3.0, 4.0 };
    Vector2D v2 = { 2.0, 5.0 };
    double dot = HikeDotProduct(&v1, &v2);
    std::cout << "HikeDotProduct((3, 4), (2, 5)) = " << dot << std::endl;

    return 0;
}

```


## VS Code Source-Level Debugging

Hike natively generates LLVM DWARF debug metadata, enabling full source-level GUI debugging in Visual Studio Code (including breakpoints, step-by-step execution, variable inspection, and call stack unwinding).

### 1. Prerequisites

* **Visual Studio Code**
* **C/C++ Extension** (`ms-vscode.cpptools`)
* **GDB** or **LLDB** (e.g., via MSYS2 MinGW-w64 on Windows, or system packages on Linux/macOS)
* **Clang**

### 2. VS Code Configuration

Create the following configuration files under the `.vscode/` directory in your workspace.

#### `.vscode/tasks.json`
Automates building the Hike source code into an executable with debug symbols before launching the debugger:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Build Hike Debug Executable",
      "type": "shell",
      "command": "go run ../../cmd/hikec ${file} -g -o${fileDirname}/main.ll && clang -g -O0 ${fileDirname}/main.ll -o${fileDirname}/app.exe",
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

#### `.vscode/launch.json`

Launches the built executable via GDB/LLDB:

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

### 3. Debugging Workflow

1. Open any `.hike` file and set breakpoints by clicking in the gutter next to the line numbers.
2. Press **`F5`** to automatically compile and launch the debug session.
3. The following features are fully supported:
* **Step Over (`F10`)**: Advances execution line-by-line within the `.hike` source.
* **Step Into (`F11`)**: Steps directly into function definitions (`func`).
* **Step Out (`Shift + F11`)**: Returns to the caller function context.
* **Variables Panel (Locals)**: Real-time inspection and updates for function parameters and local variables.
* **Editor Hover**: Hover over any identifier in the editor to inspect its current value.
* **Call Stack**: Complete stack trace tracking across function call frames.


---

## 📊 Current Status & Roadmap

### ✅ Implemented

* **Frontend & Language Features**
* Lexer, recursive-descent parser, AST construction, and multi-pass semantic analysis (Sema)
* Primitive types (`int`, `float64`, `byte`, `bool`, `string`), pointers (`*T`), and type aliases
* Composite types (`struct`, fixed-size arrays, slices)
* Control flow structures (`if`, `for`, `for-range`, `switch`, `type-switch`)
* Type inference (`:=`) and multiple return values
* Compile-time monomorphized generics for functions and structs


* **Code Generation & Interoperability**
* LLVM IR backend code generator
* Automatic C/C++ header (`.h`) generation with proper calling conventions and include guards
* Shared library (`.dll`, `.so`) and import library (`.dll.a`) compilation workflows
* End-to-end integration verified on Windows (MinGW/Clang) and Linux via C++ hosts and Python (`ctypes`)



### 🚧 Limitations / Work in Progress

* **Standard Library**: Currently relies on core primitives and basic runtime builtins.
* **Dynamic Interfaces**: Dynamic interface dispatch (`vtable`) is under design.
* **Closures**: Full lexical scope variable capture in anonymous functions.
* **Memory Management Tooling**: Formalization of owner-cleans conventions and arena allocator helpers.
* **Integrated CLI Driver**: Native wrapper to invoke Clang directly from `hikec` without manual build scripts.

### 🔮 Future Goals

1. **Integrated Build Command**: One-stop build orchestration via `hikec build --shared mylib.hike`.
2. **Package & Module Management**: Dependency resolution and multi-module packaging.
3. **High-Performance Plugin Ecosystem**: Standardized plugin templates for game engines (Unreal Engine, Godot) and high-throughput servers.
4. **Self-Hosting**: Re-implementing the Hike compiler frontend in Hike itself.

---

## 📄 License

This project is open source and available under the [MIT License](https://www.google.com/search?q=LICENSE).

```

```
