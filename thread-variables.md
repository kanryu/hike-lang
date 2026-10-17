# Threadable and Concurrent Module Variables

Threadable and concurrent module variables provide a simple and predictable
foundation for programs that use multiple threads. They make the intended
sharing model explicit at the declaration site and remove the need for every
caller to build the same storage and synchronization scheme manually.

## Threadable module variables

A `threadable` block declares variables whose storage belongs to the current
thread. Every thread receives an independent instance of the variables. A
write performed by one thread is therefore not visible through another
thread's threadable instance.

```hike
var threadable(64) {
    requestCount int
    workerName string
}

func worker(id int) int {
    requestCount = id
    return requestCount
}
```

The size argument is specified in kilobytes. The argument may be omitted when
the backend's default allocation is appropriate:

```hike
var threadable() {
    scratch []byte
}
```

Threadable storage is released with the lifetime of the thread. It must not be
used as a mechanism for sharing data between threads. Data that must be shared
belongs in a `concurrent` block instead.

## Concurrent module variables

A `concurrent` block declares variables that may be accessed by multiple
threads. The backend must enforce safe access for every read and write:

- scalar values use atomic operations where the target supports them;
- composite values use an implicit lock around the access;
- callers do not need to acquire or release a separate user-visible mutex for
  these module variables.

```hike
var concurrent(64) {
    completed int
    results []int
}

func recordResult(value int) {
    completed = completed + 1
    // Access to results is synchronized by the concurrent variable model.
    results = append(results, value)
}
```

The size argument is also expressed in kilobytes and may be omitted:

```hike
var concurrent() {
    sharedState State
}
```

The exact storage mechanism is backend-specific. Native code may use
thread-local storage and atomic or locked operations provided by the target
runtime. WebAssembly uses the memory model available to the selected runtime;
threadable data is placed in a worker-local region, while concurrent data is
placed in shared memory and accessed using the required synchronization
operations.

## The `mapped` modifier

`threadable` and `concurrent` blocks may declare a mapped-variable handle by
adding `mapped(name)` to the declaration. The handle is a compiler-managed
name for the variable group and is reserved for mapped persistence operations:

```hike
var concurrent(64, mapped(state)) {
    count int
    ratio float64
    text string
    payload []byte
}
```

Mapped state is serialized and deserialized through the standard
`std/memorymap` API:

```hike
import "std/memorymap"

serializeError := memorymap.MappedSerialize(state, "state.hmb")
deserializeError := memorymap.MappedDeserialize(state, "state.hmb")
```

The mapped handle is not a general value. It may only appear as the first
argument of `MappedSerialize` or `MappedDeserialize`; assigning it, printing
it, passing it to another function, or using it in an expression is a compile
error.

The native implementation uses memory-mapped persistence data. The fixed
variable image is followed by the variable payload buffers, and the file
terminates with a relocation recipe describing pointer corrections. During
deserialization, the image is loaded as one contiguous region and the
relocation recipe restores pointers relative to the new base address. This
avoids type-by-type reconstruction and keeps large structures containing maps,
slices, strings, and nested values fast to initialize.

### Metadata embedded in the process binary

When a mapped variable is compiled, the compiler emits its traversal metadata
into the native process binary. The metadata is generated after lowering has
finished, from the semantically resolved declarations, and is stored as a flat
array of four `i64` values per row:

```text
id, parent_id, pos, chunk_size
```

`pos` is the byte offset of the first element from the parent buffer, and
`chunk_size` is the stride for subsequent elements. The row kind is encoded by
the ID range, so a parallel kind table is not required:

| ID range | Meaning |
| --- | --- |
| `0` | Root of the mapped variable graph |
| `0x1000+` | Compact-dict map header buffer |
| `0x2000+` | Compact-dict entry buffer |
| `0x3000+` | String or slice payload buffer |
| `0x4000+` | Pointer/reference location |

For example, this mapped state contains maps, strings, slices, and nested
values at several levels:

```hike
type Leaf struct {
    Name string
    Scores []int
}

type Branch struct {
    Title string
    Tags []string
    Leaves []Leaf
    Rows [][]int
}

var concurrent(256, mapped(state)) {
    catalog map[string]Branch
    groups map[string][]Branch
    labels []string
    matrix [][]int
}
```

The following table shows the rows emitted for this declaration. The binary
stores the rows as `id`, `parent_id`, `pos`, and `chunk_size`; the final column
is a source-level comment explaining which variable element produced the row.

| id | parent_id | pos | chunk_size | comment |
| ---: | ---: | ---: | ---: | --- |
| `0` | `0` | `0` | `0` | Root of `state` |
| `0x4000` | `0` | `0` | `0` | Pointer in `catalog` to its map value |
| `0x1000` | `0x4000` | `0` | `0` | `catalog` compact-dict header buffer |
| `0x4001` | `0x1000` | `0` | `0` | Pointer from the map header to its entry buffer |
| `0x2000` | `0x4001` | `0` | `40` | `catalog` compact-dict entries; 40-byte entry stride |
| `0x4002` | `0x2000` | `24` | `40` | `catalog` value `Branch.Title` string pointer |
| `0x3000` | `0x4002` | `0` | `0` | Payload for `catalog` value `Title` |
| `0x4003` | `0x2000` | `16` | `40` | `catalog` value `Branch.Tags` slice pointer |
| `0x3001` | `0x4003` | `0` | `16` | Payload for each `Tags` string element |
| `0x4004` | `0x3001` | `0` | `16` | Pointer to each string in `Tags` |
| `0x3002` | `0x4004` | `0` | `0` | Payload for each `Tags` string |
| `0x4005` | `0x2000` | `32` | `40` | `catalog` value `Branch.Leaves` slice pointer |
| `0x3003` | `0x4005` | `0` | `16` | Payload for each `Leaf` element in `Leaves` |
| `0x4006` | `0x3003` | `0` | `16` | `Leaf.Name` string pointer inside `Leaves` |
| `0x3004` | `0x4006` | `0` | `0` | Payload for each `Leaf.Name` string |
| `0x4007` | `0x3003` | `16` | `16` | `Leaf.Scores` slice pointer inside `Leaves` |
| `0x3005` | `0x4007` | `0` | `8` | Payload for each integer in `Leaf.Scores` |
| `0x4008` | `0x2000` | `48` | `40` | `catalog` value `Branch.Rows` nested-slice pointer |
| `0x3006` | `0x4008` | `0` | `16` | Payload for each inner `Rows` slice |
| `0x4009` | `0x3006` | `0` | `16` | Pointer to each inner slice in `Rows` |
| `0x3007` | `0x4009` | `0` | `8` | Payload for each integer in `Rows` |
| `0x400A` | `0` | `8` | `0` | Pointer in `groups` to its map value |
| `0x1001` | `0x400A` | `0` | `0` | `groups` compact-dict header buffer |
| `0x400B` | `0x1001` | `0` | `0` | Pointer from `groups` header to entry buffer |
| `0x2001` | `0x400B` | `0` | `40` | `groups` compact-dict entries |
| `0x400C` | `0x2001` | `24` | `40` | `groups` value `[]Branch` slice pointer |
| `0x3008` | `0x400C` | `0` | `64` | Payload for each `Branch` in `groups` |
| `0x400D` | `0x3008` | `0` | `64` | `Branch.Title` pointer inside `groups` payload |
| `0x3009` | `0x400D` | `0` | `0` | Payload for each group `Title` string |
| `0x400E` | `0x3008` | `16` | `64` | `Branch.Tags` pointer inside `groups` payload |
| `0x300A` | `0x400E` | `0` | `16` | Payload for each group tag string |
| `0x400F` | `0x300A` | `0` | `16` | Pointer to each group tag string |
| `0x300B` | `0x400F` | `0` | `0` | Payload for each group tag |
| `0x4010` | `0x3008` | `32` | `64` | `Branch.Leaves` pointer inside `groups` payload |
| `0x300C` | `0x4010` | `0` | `16` | Payload for each group `Leaf` element |
| `0x4011` | `0x300C` | `0` | `16` | Group `Leaf.Name` pointer |
| `0x300D` | `0x4011` | `0` | `0` | Payload for each group leaf name |
| `0x4012` | `0x300C` | `16` | `16` | Group `Leaf.Scores` pointer |
| `0x300E` | `0x4012` | `0` | `8` | Payload for each group leaf score |
| `0x4013` | `0x3008` | `48` | `64` | `Branch.Rows` pointer inside `groups` payload |
| `0x300F` | `0x4013` | `0` | `16` | Payload for each group inner `Rows` slice |
| `0x4014` | `0x300F` | `0` | `16` | Pointer to each group inner row |
| `0x3010` | `0x4014` | `0` | `8` | Payload for each group row integer |
| `0x4015` | `0` | `16` | `0` | Pointer in `labels` to its string payload |
| `0x3011` | `0x4015` | `0` | `16` | Payload for each `labels` string element |
| `0x4016` | `0x3011` | `0` | `16` | Pointer to each string in `labels` |
| `0x3012` | `0x4016` | `0` | `0` | Payload for each `labels` string |
| `0x4017` | `0` | `32` | `0` | Pointer in `matrix` to its outer slice payload |
| `0x3013` | `0x4017` | `0` | `16` | Payload for each inner slice in `matrix` |
| `0x4018` | `0x3013` | `0` | `16` | Pointer to each inner `matrix` slice |
| `0x3014` | `0x4018` | `0` | `8` | Payload for each integer in `matrix` |

During deserialization, the runtime follows `parent_id`, reads each buffer's
length from its native representation, and applies the relocation rows to the
new mapped base address. This lets the same type-independent table traverse a
map value containing further slices and strings without requiring the runtime
to know the source-level `Branch` or `Leaf` type names.

Mapped variables remain writable after they have been mapped. For a mapped
`concurrent` block, serialization and deserialization may be called inside the
block's `lock` scope. These operations are compiler-recognized persistence
operations and are the only permitted use of the mapped handle in that scope;
ordinary function calls remain forbidden inside a lock block.

The native mapped implementation is target-specific. WebAssembly has no OS
memory-mapping facility, so the mapped modifier and mapped persistence API are
not available for WebAssembly targets. Use explicit serialization through the
host environment when a WebAssembly program needs durable data.

## Naming rule

Every variable declared inside a `threadable` or `concurrent` block must begin
with a lowercase letter. An uppercase initial letter is a syntax error:

```hike
var threadable(64) {
    WorkerCount int // syntax error
}

var concurrent(64) {
    SharedState State // syntax error
}
```

The valid form is:

```hike
var threadable(64) {
    workerCount int
}

var concurrent(64) {
    sharedState State
}
```

This restriction prevents these storage objects from acquiring package-level
export semantics accidentally and keeps their visibility predictable.

## Module-private visibility

Threadable and concurrent variables are private to the module that declares
them. A different module cannot access them directly, even if it uses the same
identifier:

```hike
// module workers
var threadable(64) {
    workerCount int
}
```

```hike
// module client
import "workers"

func read() int {
    return workers.workerCount // rejected: module-private variable
}
```

Communication between modules must use an explicit public function or another
public API. This makes ownership and synchronization boundaries visible in the
program instead of relying on accidental access to storage internals.

## Choosing between the two models

Use `threadable` when each worker needs private scratch state, counters, or
temporary buffers. Use `concurrent` when all workers must observe the same
state. The two models describe different ownership rules and should not be
treated as interchangeable storage classes.

## Coordinated updates with locks

Atomic access is guaranteed per variable. It does not make a sequence of
accesses to several variables atomic as a group. Use a `lock` block when a
consistent multi-variable update is required:

```hike
lock {
    balance = balance - 100
    version = version + 1
}
```

The following restrictions apply to lock blocks:

- A lock block must not contain another lock block.
- Function calls are not permitted inside a lock block.
- The block may only protect accesses to concurrent module variables.

These restrictions prevent recursive locking, hidden lock acquisition, and
deadlocks caused by calls that acquire locks indirectly. Lock blocks should be
short and contain only the operations required to preserve the intended
invariant.
