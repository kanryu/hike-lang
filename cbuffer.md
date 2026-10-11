# `cbuffer`

`cbuffer` is a Hike compiler built-in type for managing a byte buffer whose
storage is owned by native code. It is intended for C libraries and other
foreign-function interfaces that return a raw pointer and require a library-
specific release operation.

The type combines direct byte access with explicit ownership. It is not an
ordinary Hike `[]byte`, and it must not be treated as one when deciding who
owns the underlying memory.

## Why `cbuffer` exists

A native API commonly returns two separate values:

```text
pointer + length
```

The pointer may refer to memory allocated by `malloc`, `sqlite3_malloc`, an
image library, or an operating-system API. Converting that pair directly into
a Hike slice would hide the allocation origin and could lead to an invalid
`free`, a double free, or use-after-free.

Copying every native buffer into a Hike slice is safe, but is unnecessarily
expensive for large binary values. `cbuffer` keeps the native allocation and
stores the operation needed to release it.

## Construction

The built-in constructor has three arguments:

```hike
buffer := cbuffer(startPointer, length, release)
```

* `startPointer` is a `*byte` pointing to the first byte of the native buffer.
* `length` is the number of bytes and is an `int`.
* `release` is a Hike `func()` value. It is called by `Close` to release the
  allocation. A `nil` release value selects the runtime's default `free`
  operation.

The constructor does not copy the data. The caller must ensure that the
pointer is valid until the buffer is closed. For a native allocation, pass an
anonymous release function that calls the allocator-specific free function:

```hike
extern func sqlite3_column_blob(stmt *byte, column int) *byte
extern func sqlite3_column_bytes(stmt *byte, column int) int
extern func sqlite3_malloc(size int) *byte
extern func sqlite3_free(memory *byte)

borrowed := sqlite3_column_blob(stmt.handle, 0)
length := sqlite3_column_bytes(stmt.handle, 0)
owned := sqlite3_malloc(length)
for i := 0; i < length; i++ {
    owned[i] = borrowed[i]
}
release := func() { sqlite3_free(owned) }
buffer := cbuffer(owned, length, release)
defer buffer.Close()
```

The pointer passed to `cbuffer` must be the owned allocation. Do not pass the
borrowed pointer returned by `sqlite3_column_blob` directly, because SQLite
owns that pointer and it must not be released with `sqlite3_free`.

## Ownership and `Close`

`cbuffer` has explicit ownership and does not participate in Hike slice
reference counting. The owner must call:

```hike
buffer.Close()
```

`Close` invokes the stored release callback, or the default allocator release
when no callback was supplied. Closing the same value more than once is safe:
the runtime records a closed state and performs the release operation only
once.

There is no ownership transfer when a `cbuffer` is copied as a value. Keep one
clear owner responsible for closing it, and do not independently free the same
native pointer elsewhere.

After a buffer is closed, `Len()` returns zero and `Ptr()` returns `nil`. A
closed buffer must not be used to access native data.

## Methods

### `Len`

Returns the current length of the buffer:

```hike
size := buffer.Len()
```

The length becomes zero after `Close`.

### `Ptr`

Returns the native start pointer:

```hike
nativePointer := buffer.Ptr()
```

This is intended for passing the buffer back to a C function. The pointer is
`nil` after `Close`.

### `Close`

Releases the native allocation and marks the buffer closed:

```hike
buffer.Close()
```

Call `Close` as soon as the native value is no longer needed. In particular,
close a returned `cbuffer` before closing the native object that supplied its
release function, if that release function depends on the native object.

## Indexing and slicing

Indexing provides byte access without creating a Hike slice:

```hike
first := buffer[0]
```

The address form `&buffer[0]` exposes the native data pointer and is useful for
FFI calls. It does not transfer ownership.

A range expression is deliberately different from a native pointer view:

```hike
part := buffer[1:3]
```

This operation creates a new, ordinary Hike-owned `[]byte` and copies the
selected bytes into it. The resulting slice has its own lifetime and remains
valid after the original `cbuffer` is closed. The `cbuffer` itself has no
reference count and is not retained by the slice.

Invalid ranges and accesses to a closed buffer produce an empty result or
otherwise follow the compiler/runtime's normal bounds behavior; they never
extend the lifetime of the native allocation.

## Release callbacks

Use a release callback that matches the allocator used by the native library.
For example:

```hike
extern func sqlite3_malloc(size int) *byte
extern func sqlite3_free(memory *byte)

memory := sqlite3_malloc(size)
release := func() { sqlite3_free(memory) }
buffer := cbuffer(memory, size, release)
defer buffer.Close()
```

Do not use the C `free` function for memory allocated by a library that
requires a different deallocator. Conversely, do not provide a release
callback for memory still owned by a Hike slice or by a native API that
retains ownership.

## SQLite BLOB implementation example

Suppose the SQLite binding exposes the native column functions as pointer-
returning functions:

```hike
extern func sqlite3_column_blob(stmt *byte, column int) *byte
extern func sqlite3_column_bytes(stmt *byte, column int) int
extern func sqlite3_malloc(size int) *byte
extern func sqlite3_free(memory *byte)
```

`sqlite3_column_blob` returns a borrowed SQLite pointer. Therefore it must not
be passed directly to `cbuffer` with a release callback: SQLite may invalidate
it after the next statement operation, and `sqlite3_free` must not be called on
that borrowed pointer. The binding first makes an owned SQLite allocation,
copies the bytes, and then constructs the `cbuffer`:

```hike
func (stmt *Stmt) ColumnBlob(column int) cbuffer {
    length := sqlite3_column_bytes(stmt.handle, column)
    if length <= 0 { return cbuffer(nil, 0, nil) }

    borrowed := sqlite3_column_blob(stmt.handle, column)
    if borrowed == nil { return cbuffer(nil, 0, nil) }

    owned := sqlite3_malloc(length)
    if owned == nil { return cbuffer(nil, 0, nil) }
    for i := 0; i < length; i++ {
        owned[i] = borrowed[i]
    }

    // The anonymous function captures the owned pointer and supplies the
    // allocator-specific release operation to the cbuffer runtime.
    release := func() { sqlite3_free(owned) }
    return cbuffer(owned, length, release)
}
```

The caller receives an owned `cbuffer` and closes it when the value is no
longer needed:

```hike
blob := query.ColumnBlob(0)
defer blob.Close()

printf(cstring("blob length=%d first=%d\n"), blob.Len(), blob[0])
```

This pattern preserves the correct SQLite allocator/deallocator pair while
ensuring that the returned buffer remains valid after the statement advances.

## Runtime representation

The compiler lowers `cbuffer` to a runtime value containing:

```text
data pointer
length
release function value
closed flag
```

The exact ABI is target-specific, but the ownership semantics are the same on
native LLVM targets and Wasm. The runtime stores the release callback rather
than guessing how the pointer should be freed.

## Best practices

1. Treat every `cbuffer` as an explicitly owned resource.
2. Pair construction with `Close`, preferably immediately with `defer`.
3. Match the release callback to the allocator that produced the pointer.
4. Use `Ptr()` only for a native call and never use it after `Close`.
5. Use a range expression when an independent Hike `[]byte` is required.
6. Do not free the pointer manually in addition to calling `Close`.
7. Do not return a borrowed native pointer as a `cbuffer` unless the callback
   and lifetime are genuinely owned by the returned value.

