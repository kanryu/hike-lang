# Hike Language Specification: UTF-8 String Architecture & Encoding Subsystem



## 1. Internal UTF-8 String Invariant



The `string` type in Hike is guaranteed to be an immutable sequence of valid **UTF-8 encoded bytes**.

* **Separation of Concerns:** The language maintains a strict boundary between `string` and `cstring`. While `cstring` represents a raw, null-terminated `*byte` pointer utilized for C-interop (FFI) and low-level I/O, `string` represents safe, managed, UTF-8 text.


* **Non-UTF-8 Encodings:** Any byte sequence originating from non-UTF-8 character encodings (such as Shift_JIS, EUC-JP, Windows-1252, or ISO-8859-1) must **never** be loaded directly into a `string`. They must be held as `cstring` or raw byte slices (`[]byte`) and decoded into `string` via a dedicated encoding implementation.

### 1.1 Runtime representation and ownership

The language-level `string` value is a fat value rather than a single C
pointer. It contains three fields:

```text
64-bit target:  { base_ptr: i8*, offset: i32, len: i32 }  // 16 bytes
wasm32 target:  { base_ptr: i8*, offset: i32, len: i32 }  // 12 bytes
```

`base_ptr` points to the beginning of the backing buffer's UTF-8 payload,
`offset` identifies the beginning of the current view relative to that pointer,
and `len` is the number of bytes in the view. String indexing and slicing are
byte-oriented; they do not operate on Unicode scalar values.

The backing allocation reserves an eight-byte header immediately before the
payload:

```text
payload - 8                    payload
┌────────────────────┬────────────────────┬─────────────────────────┐
│ capacity: uint32   │ refcount: int32    │ UTF-8 data ...            │
│ offset 0           │ offset 4           │ offset 8                 │
└────────────────────┴────────────────────┴─────────────────────────┘
```

Heap-created buffers start with a reference count of `1`. Static string
literals use `INT32_MIN` as an immortal marker and are never released. The
marker is deliberately not `-1`, so a reference-count underflow cannot be
mistaken for an immortal buffer.

### 1.2 Shared substring views and copy-on-write

Slicing a string does not copy its bytes. For example, `s[2:5]` creates a new
fat value that reuses `s`'s `base_ptr`, adjusts `offset`, and sets `len` to `3`.
The backing buffer's reference count is incremented. This makes substring
creation constant-time and avoids copying large UTF-8 buffers.

Because a substring may not end at the original buffer's NUL terminator,
internal string comparisons and concatenations use the view length explicitly
instead of relying on `strcmp` or `strlen`.

When a string view is modified through indexed assignment, the runtime checks
the backing buffer's reference count:

* With a reference count of `1`, the existing buffer is reused and the write
  is performed at the view's offset.
* With a reference count of `2` or greater, the visible string range is copied
  to a new buffer before the write. Other views therefore remain unchanged.
* Static literals and other non-unique buffers also use the copy-on-write path
  and are never modified in place.

Reference-count increments for aliases and decrements for assignments are
implemented. Complete compiler-wide lifetime analysis for every temporary and
shared immutable buffer, including releasing views at all scope exits, remains
future work. Static literal storage is intentionally retained for the whole
program lifetime.



---

## 2. Discouraged Operations: Direct Indexing and Slicing



Direct array indexing (`s[i]`) and slice operations (`s[low:high]`) on the `string` type are **strongly discouraged** for general text manipulation.

* **Byte-Level Semantics:** Indexing and slicing on `string` operate at the raw **byte** level, not at the character (Unicode scalar value / rune) or grapheme cluster level.


* **Risk of UTF-8 Invalidation:** Because UTF-8 is a variable-length encoding (1 to 4 bytes per character), arbitrary byte indexing or slicing can split multi-byte sequences. For instance, slicing through the middle of a 3-byte Japanese kanji character (e.g., `日`: `0xE6 0x97 0xA5`) yields invalid UTF-8 fragments, causing downstream runtime decoding failures.


* **Recommended Practice:** For character-by-character traversal or boundary-aware slicing, strings should be decoded into runes (`[]rune`) or traversed using standard library iterator constructs.



---

## 3. The `std/encoding` Interface Architecture



All character encoding classes must implement the standard `Encoding` interface declared in `std/encoding`.

```hike
package encoding

// Encoding defines the common contract implemented by all character set codecs
// (e.g., Shift_JIS, EUC-JP, GBK, ISO-8859 series).
type Encoding interface {
    Name() string
    Decode(src cstring, length int) (string, error, int)
    Encode(src string, length int) (cstring, error, int)
}

```

* **Interface vs. Concrete Method Signature:**
* While the `Encoding` interface defines the strict three-parameter / two-parameter contract without default values, concrete implementations (such as `ShiftJISEncoding`) **can and are strongly recommended to** define a default value of `-1` for the `length` parameter (`length int = -1`).
* This allows ergonomic call-site usage such as `enc.Decode(rawSJIS)` and `enc.Encode(utf8Text)` without sacrificing the ability to operate on fixed-size buffers.



Concrete implementations (such as `ShiftJISEncoding`) reside in designated category subpackages (e.g., `std/encoding/japanese`) and provide factory constructors (e.g., `NewShiftJIS()`).

---

## 4. Method Signatures and Return Value Semantics



Both `Decode` and `Encode` return a 3-value tuple containing the result, an error interface, and a metric representing the byte count processed.

### The `length` Parameter Semantics (`length = -1`)

For both `Decode` and `Encode`, the `length` parameter governs the input boundary:

* **Whole-Buffer Processing (`length < 0` / omitted with default `-1`):**
* When `length` is negative (typically `-1`), the library automatically determines the full input extent:
* For `Decode(src, -1)`: The decoder processes the entire null-terminated `cstring` until the terminating null byte (`\0`).
* For `Encode(src, -1)`: The encoder processes the entire string across its full byte length (`len(src)`).


* This is the standard mode for typical file-to-memory and string transformation tasks.


* **Bounded / Stream Processing (`length >= 0`):**
* The method processes strictly up to `length` bytes from `src`.
* If a null byte is encountered before `length` bytes in `Decode`, it is treated as a regular character rather than an early termination, making this mode safe for binary chunk buffers.



### `Decode(src cstring, length int = -1) (string, error, int)`

Converts an external byte stream (`cstring`) into an internal UTF-8 `string`.

| Return Index | Type | Identifier | Semantics |
| --- | --- | --- | --- |
| **1** | `string` | `dst` | The resulting valid UTF-8 string. Returns a partial string or empty string on failure. |
| **2** | `error` | `err` | `nil` if decoding succeeded completely; an `EncodingError` if an invalid lead/trail byte or truncated multi-byte sequence was encountered. |
| **3** | `int` | **`consumed`** | **The total number of source bytes successfully consumed from `src`.**<br> |

* **Role of `consumed` (3rd Return Value):**
* Essential for stream parsing and chunked I/O where incoming buffers contain partial character sequences.


* Allows callers to determine exactly where an encoding error occurred in the input stream and resume or discard accordingly.





### `Encode(src string, length int = -1) (cstring, error, int)`

Converts an internal UTF-8 `string` into a target-encoded byte buffer (`cstring`).

| Return Index | Type | Identifier | Semantics |
| --- | --- | --- | --- |
| **1** | `cstring` | `dst` | The allocated, target-encoded buffer (null-terminated for C safety). |
| **2** | `error` | `err` | `nil` if all runes were converted; an `EncodingError` if an unmappable character or malformed UTF-8 sequence was encountered. |
| **3** | `int` | **`produced`** | **The total number of encoded data bytes written to `dst` (excluding the terminating null byte).**<br> |

* **Role of `produced` (3rd Return Value):**
* In binary formats and network protocols, payload length is required independently of C null terminators.


* Allows precise tracking of bytes written without incurring redundant `strlen` passes over binary-unsafe payloads.





---

## 5. Concrete Usage Example (Shift_JIS Round-Trip)



```go
package main

import (
    "std/encoding/japanese"
)

extern func printf(fmt cstring, ...) int
extern func free(ptr *byte)

func main() int {
    // 1. Instantiate the codec via constructor
    enc := japanese.NewShiftJIS()

    // 2. Decode raw Shift_JIS input to UTF-8 string
    // rawSJIS: 0x93 0xFA 0x96 0x7B 0x8C 0xEA ("日本語")
    var rawSJIS cstring = getShiftJISBuffer()
    sjisLen := 6

    utf8Text, decErr, consumed := enc.Decode(rawSJIS, sjisLen)
    if decErr != nil {
        printf("[Error] Decoding failed after consuming %d bytes: %s\n", consumed, decErr.Error())
        return 1
    }
    printf("[Success] Decoded %d bytes into UTF-8: %s\n", consumed, utf8Text)

    // 3. Encode internal UTF-8 string into Shift_JIS cstring (length omitted, defaults to -1)
    sample := "日本語文字列変換の実証。"
    encodedData, encErr, produced := enc.Encode(sample)
    if encErr != nil {
        printf("[Error] Encoding failed: %s\n", encErr.Error())
        return 1
    }
    printf("[Success] Encoded %d bytes of Shift_JIS payload\n", produced)

    // Free the dynamically allocated cstring buffer when finished
    free((*byte)(encodedData))
    return 0
}

```
