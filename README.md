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
