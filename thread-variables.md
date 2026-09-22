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
