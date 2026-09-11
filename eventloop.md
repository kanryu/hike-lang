# Event Loop Architecture in Hike: Main-Thread Synchronization and Two-Way Task Offloading

High-performance native applications (such as GUI tools, game engines, and multimedia runtimes) frequently interface with OS-level subsystems that enforce strict thread affinity. Frameworks like Win32, Cocoa, and OpenGL require UI manipulation and context bindings to execute exclusively on the process's designated OS main thread.

Hike provides native, zero-cost thread-affinity dispatching through its standard library module `std/eventloop`. By combining first-class fat-pointer closures, thread-safe channels, and compile-time generics, Hike enables worker threads to post arbitrary anonymous functions to the main thread and receive typed results synchronously with zero marshaling overhead.

---

## 1. The Thread Affinity Problem in Systems Languages

In concurrent runtimes, coordinating between background compute workers and a thread-bound coordinator traditionally introduces severe architectural friction:

* **M:N Green-Thread Limitations (e.g., Go)**: Runtimes that abstract OS threads across goroutines cannot inherently guarantee thread pinning without manual interventions like `runtime.LockOSThread()`. Developers must construct ad-hoc message pumps, risk cyclic deadlocks, and write boilerplate request/response structures.
* **Heavyweight Dispatchers (e.g., C# / Java)**: Systems often introduce reflection, boxing through generic object wrappers (`any` / `object`), and dynamic invocation queues, incurring runtime GC pressure and interface allocation costs.

Hike eliminates these issues at the language and compiler level. Because `eventloop.Run()` runs directly in the entry point `main()` on the primary OS thread, all tasks pulled from its internal queue are guaranteed by definition to execute in the main thread context.

---

## 2. API Design and Execution Matrix (`std/eventloop`)

The `std/eventloop` module provides a thread-safe abstraction over an encapsulated `chan func()` queue.

| Function | Execution Context | Caller Behavior | Semantics |
| --- | --- | --- | --- |
| `eventloop.Init(cap int)` | Main Thread | Synchronous setup | Initializes internal task channel with capacity `cap`.

 |
| `eventloop.Run()` | Main Thread | Blocking loop | Drains and executes posted closures until stopped.

 |
| `eventloop.Stop()` | Any Thread | Non-blocking dispatch | Posts a shutdown flag to cleanly terminate `Run()`.

 |
| `eventloop.Post(fn func())` | Any Thread | Fire & forget | Enqueues `fn` to run on the main thread.

 |
| `eventloop.InvokeCh[T](fn func() T)` | Any Thread | Non-blocking dispatch | Enqueues `fn` and immediately returns a typed `chan T`.

 |
| `eventloop.Invoke[T](fn func() T)` | Any Thread | Blocking wait | Enqueues `fn`, blocks caller, and returns type `T` directly.

 |

---

## 3. Guaranteed Main-Thread Execution Model

The core architecture relies on an inverted Actor-dispatch pattern:

```text
[Worker Thread (Pool)]                           [OS Main Thread]
       │                                                │
       │ (1) Creates closure: func()                    │
       │     (captures local context)                   │
       ├── eventloop.Post(task) ───────────────────────>│ [eventloop.Run()]
       │                                                │
       │ [Worker continues immediately]                 ├── (2) Pulls closure: task := <-queue
       │                                                │   (3) Executes task() on Main Thread
       ▼                                                ▼

```

Because tasks are dispatched as fat-pointer closures (`{ void(*)(void*), void* }`), arbitrary local state from the worker thread is automatically captured into heap environments and transmitted across the channel. The coordinator in `eventloop.Run()` only ever consumes closures via:

```go
func Run() {
    running = 1
    for running == 1 {
        task := <-queue
        task() // Guaranteed to execute on the main thread stack
    }
}

```

This guarantees thread affinity without requiring explicit message schemas or switch-case command decoders.

---

## 4. Two-Way Execution: Returning Results to Workers

When a worker thread requires access to a main-thread-exclusive resource (e.g., retrieving an OS window handle, querying device states, or inspecting GUI widgets), it must execute code on the main thread and capture the returned value.

Hike solves this with a micro-RPC mechanism built on temporary one-shot buffered channels and generics.

### Implementation Internals: `InvokeCh[T]`

```go
func InvokeCh[T](fn func() T) (chan T) {
    resCh := make(chan T, 1)
    queue <- func() {
        resCh <- fn() // Evaluated on Main Thread; transmits value back
    }
    return resCh      // Immediately returned to worker as a Future handle
}

func Invoke[T](fn func() T) T {
    resCh := InvokeCh[T](fn)
    return <-resCh    // Blocks calling thread until Main Thread signals completion
}

```

1. **One-Shot Channel Allocation**: The worker allocates a typed, buffered channel (`resCh := make(chan T, 1)`).


2. **Context Closure Capture**: The worker enqueues a wrapper closure into `queue`. This wrapper closure captures both the target lambda `fn` and the reply channel `resCh`.


3. **Execution & Reply**: When the main thread executes the wrapper closure, it invokes `fn()`, captures the result, and writes it directly into `resCh`.


4. **Resumption**: The worker thread awaits `resCh` using kernel-level synchronization (`WaitForSingleObject`), consuming 0% CPU while blocked.



### Pipelined Asynchronous Invocations

Separating `InvokeCh` from `Invoke` allows workers to post multiple distinct operations to the main thread in a pipeline, maximizing throughput before awaiting the results:

```go
// Worker Thread: Pipeline two main-thread requests concurrently
chA := eventloop.InvokeCh[int](func() int {
    return readNativeDeviceA()
})
chB := eventloop.InvokeCh[string](func() string {
    return readNativeDeviceB()
})

// Worker proceeds with independent parallel work here...

// Collect results sequentially using the Receive operator (<-)
valA := <-chA
valB := <-chB

```

### The `val := <-InvokeCh` Syntax Alignment

By pairing `eventloop.InvokeCh` with Hike's native receive operator `<-`, blocking synchronization is explicitly visible at the call site:

```go
// Immediate wait on main thread computation
devID := <-eventloop.InvokeCh[int](func() int {
    return mainThreadDeviceID
})

```

This preserves symmetry with Hike's task join operator `<-Async(...)`, creating a unified cognitive model across all inter-thread synchronization points.

---

## 5. End-to-End Example

The following example demonstrates a worker thread offloaded via `Async`, dispatching state mutations to the main loop and retrieving coordinated values.

```go
package main

import "std/eventloop"

func printf(format string, ...) int

var counter int = 100

func main() int {
    // 1. Initialize the event loop buffer
    eventloop.Init(16)

    // 2. Offload heavy background work to OS thread pool
    worker := Async(func() int {
        // Post pipeline operations to Main Thread
        chA := eventloop.InvokeCh[int](func() int {
            counter = counter + 5
            return counter
        })
        chB := eventloop.InvokeCh[int](func() int {
            counter = counter * 2
            return counter
        })

        // Synchronously extract computed values from Main Thread
        a := <-chA
        b := <-chB

        // Signal Main Thread to terminate loop
        eventloop.Stop()
        return a + b
    })

    // 3. Main Thread runs event loop (blocks here)
    eventloop.Run()

    // 4. Join background worker and print result
    total := <-worker
    printf("PIPELINE_TOTAL=%d\n", total) // Outputs: PIPELINE_TOTAL=315
    return 0
}

```

---

## 6. Low-Level LLVM Code Generation

When compiling calls to `eventloop.InvokeCh[T]`, the Hike compiler specializes the function for each concrete return type `T` and emits optimized LLVM IR:

### Specialized Instantiation for Primitive `int`

```llvm
define i8* @eventloop_InvokeCh__int({ i8*, i8* } %fn_arg.1) {
entry:
  ; 1. Allocate typed one-shot return channel (elem_size: 8 bytes, cap: 1)
  %v3 = call i8* @__hike_chan_make(i64 8, i64 1)
  
  ; 2. Pack user function and return channel into wrapper closure environment
  %.b21 = call i8* @malloc(i64 16)
  %v19 = bitcast i8* %.b21 to i8**
  store i8* %v3, i8** %v19 ; Store channel pointer
  ; ...
  
  ; 3. Enqueue fat pointer into global eventloop_queue
  %v5 = load i8*, i8** @eventloop_queue
  call void @__hike_chan_send(i8* %v5, i8* %.b23)
  
  ; 4. Return channel pointer to caller
  ret i8* %v3
}

```

### Main Thread Thunk Invocation

On the receiving end in `eventloop.Run()`, the loop receives the 16-byte closure `{ i8* fn_ptr, i8* env_ptr }` and invokes it through an indirect call:

```llvm
  %v6 = load { i8*, i8* }, { i8*, i8* }* %task.5
  %v7 = extractvalue { i8*, i8* } %v6, 0 ; Function pointer
  %v8 = extractvalue { i8*, i8* } %v6, 1 ; Environment pointer
  %.b16 = bitcast i8* %v7 to void (i8*)*
  call void %.b16(i8* %v8)              ; Executes directly on Main Thread stack

```

Through this interaction, `std/eventloop` delivers a lock-free, zero-allocation dispatching bridge between unconstrained OS worker pools and thread-affinity-bound native coordinators.