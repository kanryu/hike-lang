# Area Allocation

## Concept

Area memory is a lexical, temporary allocation scope. An area block provides a
short-lived allocation region for intermediate data that does not need to
survive the end of the block. Allocations made in the area are released in one
operation when the block ends.

Area memory is separate from the ordinary heap allocation path. This makes the
lifetime of temporary data explicit in the source program and avoids requiring
individual deallocation for every value created inside the block. Releasing an
area is zero-cost with respect to the number of objects it contains: the
runtime discards the area frame and buffer in O(1), without walking or freeing
each allocation separately.

An area block is not a function and does not introduce a callable function
value. It is an independent statement in the AST and HIR.

## Syntax

An area may be declared with the default capacity:

```hike
area() {
    temporary := make([]int, 32)
}
```

It may also specify a capacity in KiB:

```hike
area(64) {
    temporary := make([]byte, 4096)
}
```

The argument is multiplied by 1024 during lowering. When omitted, the runtime
uses its default area capacity.

Area blocks may appear in ordinary functions, receiver methods, and anonymous
functions. They are lexical scopes, not nested function declarations.

## Lifetime and escaping values

Values allocated in an area become invalid when control leaves the area block.
This includes dynamically allocated arrays, slices, strings, and their backing
storage. An area value must not be assigned to an outer variable and then used
after the area has ended.

To preserve data outside the area, the program must explicitly call the
`deepcopy` builtin:

```hike
var result string

area(64) {
    value := string([]byte{'a', 'r', 'e', 'a'})
    result = deepcopy(value)
}

// result remains valid here because deepcopy allocated ordinary heap storage.
```

Directly copying an area-backed value into a variable declared outside the
area is a compile-time error. This rule applies to strings, slices, pointers,
and structs containing area-sensitive members:

```hike
var result string

area() {
    value := "temporary"
    result = value // compile-time error
}
```

The same restriction applies to slices and pointer-based structures:

```hike
var items []int
var saved *Item

area() {
    values := []int{1, 2, 3}
    item := &Item{}
    items = values // compile-time error
    saved = item   // compile-time error
}
```

Use `deepcopy` explicitly when the value must outlive the area:

```hike
area() {
    values := []int{1, 2, 3}
    items = deepcopy(values)
    saved = deepcopy(item)
}
```

The compiler rejects these shallow copies because the copied values would still
refer to storage that is released when the area ends.

`deepcopy` copies string data, slice contents, arrays, structs, and pointer
targets into ordinary heap storage. Pointer members are followed recursively.
Function values cannot be deep-copied, and cyclic object graphs are rejected
because they require runtime graph tracking rather than a finite static copy.

## Nested areas

Areas may be nested. A nested area does not create an unrelated application
heap; it reserves a portion of the remaining parent area. The current runtime
uses half of the parent's remaining capacity for each nested area.

```hike
area(128) {
    outer := make([]byte, 16)

    area() {
        inner := make([]byte, 16)
    }
}
```

When the nested block ends, its allocations are invalidated and the parent
area cursor is restored in O(1). When the outer block ends, the whole outer
area is released in O(1), regardless of how many allocations were made inside
it.

## Native and WebAssembly implementations

The HIR represents an area with `AreaBegin`, `AreaAlloc`, and `AreaEnd`
instructions. The backends lower these instructions to target-specific
runtime operations.

### Native LLVM

The native runtime stores an area frame and its allocation buffer in ordinary
runtime-managed memory. A root area allocates a separate buffer; nested areas
reserve a subrange of the parent buffer. Ending a root area releases its
buffer, while ending a nested area restores the parent cursor.

### WebAssembly

The WebAssembly backend uses the module's linear memory and its runtime
allocator. Area frames use the same parent/subrange model as the native
implementation, but all addresses are wasm32 addresses and capacity arithmetic
uses 32-bit values.

The two implementations have the same source-level lifetime semantics, but
their underlying allocation mechanisms and address widths are different.

## Thread safety and reuse

An active area frame owns its allocation cursor and does not use a shared area
cursor or a process-wide temporary allocation lock. Consequently, areas active
on different worker threads do not share their temporary allocations and can
be created, used, and released independently. The underlying platform
allocator remains responsible for making independent frame and buffer
allocation safe for concurrent callers.

This gives area cleanup two important properties:

- It is thread-safe because each active area has independent frame and cursor
  state.
- It is zero-cost with respect to allocation count because cleanup is a cursor
  restoration or a constant-time frame/buffer release, rather than a per-object
  destructor pass.

Two areas that are active at the same time must not be treated as the same
storage. After the first area ends, however, its buffer is returned to the
allocator. A later area may receive the same address. Therefore, sequential
areas may visibly reuse the same memory address:

```hike
area(64) {
    first := string([]byte{'f', 'i', 'r', 's', 't'})
    // log the backing address here
}

area(64) {
    second := string([]byte{'s', 'e', 'c', 'o', 'n', 'd'})
    // the allocator may reuse the address released above
}
```

Address reuse does not make the first value valid again. All references into
the first area become invalid at the end of its block, even if a later area
happens to receive the same address.

## Region incompatibility

The region memory model and area memory model cannot be enabled together. Their
allocation and lifetime rules are different, and their interaction is
undefined. Using `area` while compiling with `--alloc=region` is therefore a
compile-time error.
