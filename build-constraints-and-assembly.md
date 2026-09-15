


# Build Constraints and Inline Assembly in Hike

Hike combines Go-compatible source ergonomics with zero-overhead LLVM native code generation. To deliver ultra-compact WebAssembly binaries alongside maximum hardware-accelerated native performance, Hike provides two complementary features: **Build Constraints** and **Function-Level Inline Assembly**.

---

## 1. Build Constraints

Hike determines which files to include in a compilation unit using source directives and file-naming conventions.

### Directives (`//go:build` and `//hike:build`)

Place build directives at the very top of the file, followed by a blank line before package declaration. Hike supports standard Go syntax as well as the native `//hike:build` alias.

```go
//hike:build (linux && amd64) || (darwin && amd64)

package crypto

```

#### Supported Operators

* `&&`: Logical AND
* `||`: Logical OR
* `!`: Logical NOT
* `( ... )`: Precedence grouping

### File Name Conventions

Files with target-specific suffixes are automatically included or ignored based on the target configuration passed to `-target`:

| Suffix Pattern | Example | Included When |
| --- | --- | --- |
| `_GOOS.hike` | `rand_linux.hike` | Target OS matches |
| `_GOARCH.hike` | `aes_amd64.hike` | Target architecture matches |
| `_GOOS_GOARCH.hike` | `sys_linux_amd64.hike` | Both OS and architecture match |
| `_test.hike` | `crypto_test.hike` | Compiling in test mode (`hike test`) |

### Predefined Tags

The compiler automatically defines tags based on the `-target` triple:

* **OS tags:** `linux`, `darwin`, `windows`, `wasm`, etc.
* **Arch tags:** `amd64`, `arm64`, `wasm32`, etc.
* **Meta tags:** `unix` (enabled on `linux`, `darwin`, `freebsd`, etc.) and `cgo` / `!cgo`.

---

## 2. Inline Assembly (`**asm**`)

Hike provides direct hardware access via LLVM inline assembly without requiring vector types (such as `__m128i`) in the Hike type system.

### Design Principles

* **Function-Level Scope:** The `**asm**` call occupies the function body. The function acts as a clean, zero-overhead wrapper.
* **Direct Operand Passing:** All input and output operands are mapped directly to function arguments.
* **Target Enforcement:** Architecture-specific instructions (such as AES-NI, PCLMULQDQ, RDRAND, RDSEED, SHA extensions) require appropriate build tags (e.g., `amd64`). Compiling them for incompatible targets like `wasm32` triggers a compile-time error.

### Syntax

```go
**asm**(
    "assembly template",
    "output constraints",
    "input constraints",
    operands...
)

```

#### Operand Replacement Rules

* `%0`, `%1`, ... are automatically converted to LLVM positional tokens (`$0`, `$1`, ...).
* Hardware register names with leading `%` (e.g., `%xmm0`, `%rax`, `%ecx`) are preserved as-is.

---

## 3. Practical Walkthrough: Hardware-Accelerated AES with Pure Fallback

The following pattern provides portable Pure Hike execution on WebAssembly while leveraging hardware AES-NI instructions on `amd64`.

### Step 1: Write Portable Implementation (`aes_wasm.hike`)

```go
//go:build wasm

package aes

// Pure software fallback for WebAssembly (<1KB footprint)
func EncryptBlock(roundKeys *byte, dst *byte, src *byte) {
    // Standard software implementation using S-Box
    // Zero dependencies, zero libc, perfectly portable
}

```

### Step 2: Write Hardware-Accelerated Implementation (`aes_amd64.hike`)

```go
//go:build amd64

package aes

// Native amd64 execution using AES-NI instructions
func EncryptBlock(roundKeys *byte, dst *byte, src *byte) {
    **asm**(
        // Load plaintext into %xmm0 and XOR with round 0 key
        "movups (%2), %%xmm0 \n"
        "movups (%0), %%xmm1 \n"
        "pxor   %%xmm1, %%xmm0 \n"

        // Rounds 1-9
        "aesenc 16(%0), %%xmm0 \n"
        "aesenc 32(%0), %%xmm0 \n"
        "aesenc 48(%0), %%xmm0 \n"
        "aesenc 64(%0), %%xmm0 \n"
        "aesenc 80(%0), %%xmm0 \n"
        "aesenc 96(%0), %%xmm0 \n"
        "aesenc 112(%0), %%xmm0 \n"
        "aesenc 128(%0), %%xmm0 \n"
        "aesenc 144(%0), %%xmm0 \n"

        // Last round (no MixColumns)
        "aesenclast 160(%0), %%xmm0 \n"

        // Store ciphertext back to dst
        "movups %%xmm0, (%1) \n",
        "",        // No outputs (writes directly to memory via dst pointer)
        "r,r,r",   // Operands passed in general-purpose registers
        roundKeys, dst, src
    )
}

```

### Step 3: Compile

Build for WebAssembly (pure fallback selected automatically):

```bash
hike build -target wasm32-unknown-unknown -o crypto.wasm .

```

Build for host native execution (AES-NI path selected automatically):

```bash
hike build -target x86_64-unknown-linux-gnu -o crypto .

```

This bifurcated model enables developers to deliver minimal-footprint WebAssembly binaries without leaving hardware performance on the table for native targets.
