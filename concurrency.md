# Concurrency in Hike: Thread-Pool Asynchrony and Continuation Model

Hike provides a lightweight, Cgo-free concurrency model compiled to native machine code via LLVM IR. Unlike runtimes that impose heavy M:N green-thread schedulers (e.g., Go, Erlang) or runtimes that require explicit serialization across threads (e.g., Node.js worker threads), Hike marries C-native execution efficiency with minimal syntactic overhead. Concurrency in Hike operates on top of Windows OS-native thread pool primitives without polluting function signatures or introducing the "function coloring problem."

---

## 1. Core Primitives: `Async` vs. `<-`

Hike exposes concurrency through two fundamental primitives: the `Async` keyword and the receive operator `<-`.

| Syntax | Execution Mode | Caller State | Return Value |
| --- | --- | --- | --- |
| `task := Async(fn)` | Non-blocking offload | Continues execution immediately | `Task` handle (One-shot channel) |
| `res := <-task` | Blocking join | Suspends execution until task completion | Unpacked return value(s) |
| `res := <-Async(fn)` | Inline offload & wait | Suspends execution on current thread | Unpacked return value(s) |

### Non-Blocking Dispatch: `Async(...)`

Calling `Async` without a prefix arrow schedules the target function onto the OS thread pool. The calling thread does not suspend; it receives a `Task` handle immediately and proceeds to the next statement.

```go
// Dispatched immediately to the thread pool; main thread does not block
task := Async(func() (string, error) {
    return fetchRemoteData("https://api.internal/heavy-assets", 250)
})

```

### Blocking Synchronization: `<-task`

The receive operator `<-` acts on a `Task` handle. It suspends the executing thread using kernel-level synchronization events until the target task finishes. Once signaled, the runtime extracts the returned data from the task buffer and unpacks it directly into local variables.

```go
// Blocks until task finishes, then unpacks values directly
res, err := <-task

```

### Immediate Join: `<-Async(...)`

Combining both forms allows offloading a computation to a background worker while synchronously awaiting its result on the calling thread. The function itself remains a standard, non-colored synchronous function, but its execution is isolated to a worker thread context.

---

## 2. The Task Handle as a Multi-Valued One-Shot Channel

In traditional systems programming, collecting multiple return values across thread boundaries requires manually allocating a heap struct, defining mutexes, signaling condition variables, and unboxing pointers.

In Hike, the handle returned by `Async(...)` functions as a **typed, one-shot channel**:

1. **First-Class Task Objects**: An arrowless `Async` expression evaluates to an opaque task reference (`%struct.__hike_task*`).


2. **Zero Serialization Overhead**: Because Hike executes in a unified native address space, data pointers, slices, and structs pass to and from tasks without intermediate serialization or marshaling.


3. **Native Multi-Value Unpacking**: When awaiting a task returning multiple values (e.g., `(string, error)` or `(int, error)`), the compiler extracts each element directly into target assignment registers:



```go
// Directly unpacks string and error without wrapper types or manual casting
data, err := <-task
if err != nil {
    // Handle error
}

```

---

## 3. Background Orchestration and Continuation Pattern

A major design advantage of Hike is the ability to nest `<-Async` within an outer `Async` block. This establishes an asynchronous continuation pattern:

1. The main thread schedules an orchestration routine via `Async(runWorker)`.
2. The main thread immediately reclaims control to run UI loops, accept network connections, or dispatch other tasks.
3. The worker thread runs `runWorker()`, executes an inner `<-Async(...)` to await a heavy subtask, and immediately resumes linear execution within that same scope to validate and consume the results.

### Code Pattern

```go
package main

import (
    "fmt"
    "time"
)

func runConfigTask() {
    fmt.Println("[Worker] Starting background task...")

    // Inner wait: Suspends this worker thread, NOT the main thread
    data, err := <-Async(func() (string, error) {
        return fetchRemoteData("https://api.internal/config", 80)
    })

    // Continuation: Sequential, local inspection directly inside the worker
    fmt.Println("[Worker] Task finished; evaluating results in the same function scope.")
    if err != nil {
        fmt.Printf("[Worker] Error: %v\n", err)
        return
    }
    fmt.Printf("[Worker] Success: %s\n", data)
}

func main() {
    // 1. Offload the orchestrator to a worker thread
    taskConfig := Async(func() {
        runConfigTask()
    })

    // 2. Control returns to main thread immediately
    fmt.Println("[Main] Task dispatched. Main thread continues without blocking.")

    // 3. Main thread performs independent work concurrently
    for i := 1; i <= 3; i++ {
        time.Sleep(50 * time.Millisecond)
        fmt.Printf("[Main] Loop iteration %d/3...\n", i)
    }

    // 4. Join before exit
    <-taskConfig
}

```

### Execution Flow

```text
[Main Thread]                   [Worker Thread A]              [Worker Thread B]
      │                                 │                              │
      ├── Async(runConfigTask) ────────>│ (starts runConfigTask)       │
      │                                 │                              │
      ├── [Main continues immediately]  ├── <-Async(fetchRemoteData) ─>│ (runs fetch)
      │   (runs loop 1, 2, 3...)        │   [Worker A sleeps]          │
      │                                 │   [No CPU burn]              │
      │                                 │<── Event Signaled ───────────┤ (done)
      │                                 │                              │
      │                                 ├── [Continuation resumes]     x [Worker B freed]
      │                                 │   (inspects data & err)      
      │                                 │                              
      ├── <-taskConfig [Join] ──────────┤                              
      │   (resumes)                     x [Worker A freed]             
      ▼                                                                

```

---

## 4. Low-Level Architecture and Runtime Subsystem

Hike avoids external C runtime (CRT) threading libraries and green-thread schedulers, relying instead on Windows kernel synchronization mechanisms compiled directly into LLVM IR.

### Windows Kernel Integration

The runtime declares direct imports from `kernel32.dll`:

* `QueueUserWorkItem`: Dispatches tasks to the OS-managed worker thread pool (`WT_EXECUTEDEFAULT = 0`).


* `CreateEventA` / `SetEvent`: Provides manual/auto-reset kernel event handles for completion notification.


* `WaitForSingleObject`: Puts awaiting threads into an alertable, zero-CPU kernel sleep state until task termination.


* `CloseHandle`: Cleans up synchronization primitives upon join.



### Internal Task Layout

Each asynchronous task is tracked by a 40-byte control structure:

```llvm
%struct.__hike_task = type { 
    void (i8*, i8*)*, ; Function pointer (Thunk)
    i8*,              ; Captured environment pointer
    i8*,              ; Return value buffer pointer
    i32,              ; Completion flag (0 = active, 1 = completed)
    i8*               ; Win32 Event HANDLE
}

```

### Dynamic Thunk Synthesis

Because target functions can return arbitrary combinations of scalar values, strings, interfaces, and structs, the compiler's LLVM emitter (`emitter.go`) dynamically synthesizes custom bridge thunks per return signature:

```llvm
define internal void @__hike_async_thunk_1(i8* %wrapper_env, i8* %buf) {
entry:
  %env_arr = bitcast i8* %wrapper_env to i8**
  %p_fn = getelementptr inbounds i8*, i8** %env_arr, i64 0
  %fn_raw = load i8*, i8** %p_fn
  %p_env = getelementptr inbounds i8*, i8** %env_arr, i64 1
  %real_env = load i8*, i8** %p_env

  ; Call the user function and store return values into the task buffer
  %typed_fn = bitcast i8* %fn_raw to { i8*, { i8*, i8* } } (i8*)*
  %res = call { i8*, { i8*, i8* } } %typed_fn(i8* %real_env)
  %typed_buf = bitcast i8* %buf to { i8*, { i8*, i8* } }*
  store { i8*, { i8*, i8* } } %res, { i8*, { i8*, i8* } }* %typed_buf

  call void @free(i8* %wrapper_env)
  ret void
}

```

### Elimination of Function Coloring

In languages using `async/await`, functions must be annotated with `async`, forcing all upstream callers to also become `async` and wrapping types inside futures or promises. In Hike:

* Functions like `fetchRemoteData` and `computeHash` remain standard, sequential routines.
* The decision to execute concurrently belongs entirely to the call site.
* The compiler handles closure environment captures, return buffer allocations, thread scheduling, kernel event signaling, and register unpacks automatically at the IR level.