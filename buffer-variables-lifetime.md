# Buffer Variables and Lifetime

This document defines the ownership and lifetime model for Hike string and
slice variables. It is intentionally separate from the encoding subsystem:
encoding specifications describe byte and character conversion, while this
document describes managed buffer representation and lifetime.

## 1. Buffer variables

String and slice variables are views over backing buffers. The variable value
contains enough information to locate the visible range, while allocation
metadata is shared by all views of the same buffer.

External `cstring` values are not Hike-managed buffers. A `cstring` is a C
ABI pointer to a null-terminated byte sequence and does not participate in
Hike retain/release operations.

## 2. Fat-pointer and buffer layouts

### 2.1 String values

An Hike `string` is an immutable UTF-8 byte view:

```text
string = { base_ptr: i8*, byte_offset: int32, byte_length: int32 }
```

On a 64-bit target this value occupies 16 bytes. On a 32-bit target, the
pointer is 4 bytes and the value occupies 12 bytes. Its backing buffer stores
an 8-byte header immediately before the payload on both targets:

```text
payload - 8: capacity: uint32
payload - 4: reference count: int32
payload:     UTF-8 byte data
```

String literals use the immortal reference-count marker `INT32_MIN` and encode
their initial view offset as `-1` (`~0`). Literal storage is immutable and
remains allocated for the lifetime of the program.

### 2.2 Slice values

An Hike slice is an element view:

```text
slice = { owner_ptr: i8*, element_offset: int32, length: int32 }
```

Both string and slice view fields use the same language-level `int32`
notation. In LLVM IR, the corresponding fields are represented as `i32`:

```text
string -> { i8*, i32, i32 }
slice  -> { i8*, i32, i32 }
```

`element_offset` and `length` are always `int32`; they do not widen with the
target pointer size. Therefore, the value occupies 16 bytes on a 64-bit
target and 12 bytes on a 32-bit target, including wasm32. The backing
allocation reserves a 16-byte header before `owner_ptr` on both targets:

```text
owner_ptr - 16: capacity: i32
owner_ptr - 12: reference count: i32
owner_ptr -  8: reserved metadata: 8 bytes
owner_ptr:      element storage
```

Capacity is stored only in the backing allocation, not in each slice view.
Subslices and aliases therefore share one authoritative capacity and one
reference count. Slice offsets are measured in elements; string offsets are
measured in bytes.

### 2.3 32-bit target details

The fat-pointer layout is width-aware only for the pointer field, while its
field order and the two `int32` view fields are identical on 32-bit and
64-bit targets:

| Target | Pointer width | Offset/length width | String value | Slice value |
| --- | ---: | ---: | ---: | ---: |
| 64-bit | 8 bytes | 4 bytes (`int32`) | 16 bytes | 16 bytes |
| 32-bit | 4 bytes | 4 bytes | 12 bytes | 12 bytes |

On a 32-bit target, `owner_ptr` and `base_ptr` are 32-bit pointers. On both
target widths, slice `element_offset` and `length` are explicitly `int32`.
The slice header is still 16 bytes: capacity and reference count occupy 4
bytes each, while the remaining 8 bytes are reserved. Therefore `owner_ptr`
is still the payload address at allocation-base-plus-16, and the metadata
addresses remain `owner_ptr - 16` and `owner_ptr - 12`.

The 32-bit representation does not put capacity back into the fat pointer.
Only the pointer and the two view fields become smaller; all views continue to
share the same backing-buffer metadata and reference count.

## 3. Literal, borrowed, and owned states

### 3.1 Literal buffers

Literal buffers are immutable and immortal. They can be exposed through
string or slice views without copying the literal data. Release operations
must leave literal storage untouched.

### 3.2 Borrowed buffers

Borrowed variables are temporary views whose use is confined to the current
expression or function. The following operations do not extend the backing
buffer's lifetime by themselves:

* `len` and `cap` queries;
* indexing and slicing;
* conditions and other read-only expressions.

When the compiler can prove that a value does not escape, it may omit the
corresponding retain/release pair.

### 3.3 Owned and escaping buffers

Owned or escaping variables require normal lifetime management. Returning a
view, assigning it to an escaping location, capturing it in a closure, or
passing it to an unknown operation keeps the backing buffer alive.

These uses retain the shared buffer and release it at the corresponding
lifetime boundary. When a function returns a managed value, the returned
value is retained before the callee releases its managed parameter
references.

## 4. Offset conventions

The negative addresses immediately before the owner pointer are metadata
space, not payload offsets:

```text
owner_ptr - 16  -> capacity
owner_ptr - 12  -> reference count
```

Negative values in a string fat pointer's offset field encode a literal view
using the bitwise complement of its logical byte offset. Thus `-1` (`~0`) is
the original literal view and `-3` (`~2`) is a literal view beginning at byte
offset `2`. A negative offset must be decoded before address arithmetic and
must never be treated as a normal payload offset. Derived views preserve the
negative encoding while they remain backed by the literal.

Dynamic slice allocations currently use non-negative element offsets. Their
literal or static-storage encoding remains reserved until static slice data
can be emitted without changing the element initialization rules. Borrowed
slice lifetime is controlled by compiler escape analysis and retain/release
placement rather than by a negative offset marker.

## 5. Benefits

This representation provides the following benefits:

* view creation and slicing remain constant-time and do not copy backing data;
* capacity and reference-count metadata are shared instead of duplicated in
  every slice value; and
* compiler-proven borrows avoid unnecessary retain/release traffic while
  escaping values remain safe through reference counting.
