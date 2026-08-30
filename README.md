# Hike (`hike-lang`) 🎋

A systems programming language with Go-like ergonomics that compiles via LLVM IR into zero-overhead, C-ABI compliant shared libraries with automatic C/C++ header generation.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![LLVM/Clang](https://img.shields.io/badge/Backend-LLVM%2FClang-blue?style=flat&logo=llvm)](https://llvm.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 📖 Overview

**Hike** is a systems programming language designed to combine the clean syntax and developer productivity of Go with the zero-overhead, predictable performance of C/C++. It completely eliminates garbage collection (GC) pauses, green-thread runtimes, and bloated runtime layers.

Operating as an expressive, high-level frontend that emits clean LLVM IR, Hike **hitchhikes** on the mature optimization and code generation pipelines of LLVM and Clang. It serves as a first-class shared library (`.dll` / `.so`) provider that seamlessly integrates into existing C, C++, Python, Rust, and game engine ecosystems without manual binding glue.

---

## ✨ Key Features

* **Go-Inspired Ergonomics**: Multi-return values, slices, structs, type inference (`:=`), and clean syntax familiar to Go developers.
* **Zero-Cost Monomorphized Generics**: Full compile-time specialization of generic functions and types without dynamic dispatch penalties.
* **Zero Runtime Overhead**: No GC pauses, no background runtime scheduler, and C-equivalent memory layout and execution speed (~90KB output binaries).
* **First-Class C-ABI Provider**: Emits pure C-ABI compliant binaries (POD structs, pointer passing, plain functions) and automatically generates matching C/C++ header files (`.h`).
* **Hitchhiking Clang/LLVM Infrastructure**: Delegates platform-specific binary formatting (PE/COFF, ELF, Mach-O) and deep optimization passes (`-O3`, SIMD vectorization) directly to Clang/LLVM.

---

## 🛠️ Architecture

```text
  [ .hike Source Code ]
           │
           ▼  (hikec: Go Frontend & Semantic Analysis)
  [ AST & Monomorphization ]
     ├───► Auto-Generated C/C++ Header (.h)
     │
     ▼  (LLVM IR Codegen)
  [ Pure LLVM IR (.ll) ]
     │
     ▼  (Clang / LLVM Optimizer -O3)
  [ Shared Library (.dll / .so) + Import Library (.dll.a) ]
     │
     ├───► Linked by Native C / C++ Applications (Zero Glue)
     └───► Loaded by Python (ctypes) / Rust / Node.js


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

### 3. Run Shared Library Example

The `examples/shared` directory contains a complete end-to-end example demonstrating shared library generation and consumption from both Python (`ctypes`) and C++.

```bash
cd examples/shared

# Run Python ctypes test
make test

# Run C++ client test (with auto-generated header)
make testcpp

```

---

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
