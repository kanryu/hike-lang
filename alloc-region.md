# Region Allocation

Region allocation is best suited to tasks that repeatedly perform a bounded,
regular computation and then discard the task's temporary data as a whole.
Examples include request-local processing, compiler passes, image or signal
processing stages, batch transformations, and loop-driven numerical work. In
these workloads, most objects have a similar lifetime and releasing one arena
at the end of the computation is both simple and efficient.

It is a poor fit for applications whose data forms a long-lived, constantly
changing object graph, such as GUI applications. GUI widgets, event handlers,
models, caches, and other objects commonly outlive one another and have
independent lifetimes. A single function-level region cannot express those
relationships efficiently; ordinary heap allocation and explicit ownership
are more appropriate there.

Hike provides an optional arena-based memory-management mode for compiler-
generated allocations. It is enabled with `--alloc=region`:

```sh
hikec build --alloc=region -o app main.hike
hikec run --alloc=region main.hike
```

The default allocation mode remains heap allocation. Region mode is therefore
an opt-in compiler configuration, not a source-language default.

## How it works

When region mode is enabled, the compiler creates one arena for each lowered
function that contains compiler-generated allocations. The generated sequence
is conceptually:

```text
region = region_begin()
value  = region_alloc(region, size)
...
region_end(region)
```

The arena is a bump allocator. Allocations are aligned, advance a single
cursor, and do not require an individual `free`. Ending the region releases
the arena buffer and its control block in constant time, rather than walking
every object allocated inside it.

The current implementation uses a 64 KiB initial arena buffer. If an
allocation does not fit, the runtime falls back to a regular heap allocation.
Values that escape the function, such as returned pointers, are promoted back
to the ordinary heap so they remain valid after the function's region ends.

Region boundaries are currently conservative function-level boundaries. The
compiler groups eligible allocations in a function into that function's
region; finer-grained lexical region inference is future work.

## Example

The following program allocates a structure repeatedly inside a function. The
temporary structures are released together when `exercise` returns:

```hike
package main

import "std/alloc/region"

type Point struct {
    x int
    y int
}

func exercise() int {
    total := 0
    for i := 1; i <= 4; i = i + 1 {
        p := &Point{x: i, y: i * 2}
        total = total + p.x + p.y
    }
    return total
}

func main() int {
    beforeActive := region.ActiveCount()
    beforeBegin := region.BeginCount()
    beforeEnd := region.EndCount()
    beforeAllocated := region.AllocatedBytes()
    beforeReleased := region.ReleasedBytes()

    result := exercise()

    // The region created by exercise must already be closed here.
    if region.ActiveCount() != beforeActive { return 1 }
    if region.BeginCount() - beforeBegin != region.EndCount() - beforeEnd {
        return 2
    }
    if region.ReleasedBytes() - beforeReleased <
        region.AllocatedBytes() - beforeAllocated {
        return 3
    }
    if result != 30 { return 4 }
    return 0
}
```

Build and run it in region mode:

```sh
hikec run --alloc=region main.hike
```

The same pattern applies to primitive and structure pointers, closure capture
environments, and repeated allocations in loops. A returned pointer is kept
alive by promotion, so this function is also valid:

```hike
type Counter struct { value int }

func makeCounter() *Counter {
    return &Counter{value: 42}
}

func main() int {
    c := makeCounter()
    return c.value
}
```

## Region runtime statistics

The `std/alloc/region` module exposes diagnostic counters:

| API | Meaning |
| --- | --- |
| `ActiveCount()` | Number of currently active regions |
| `BeginCount()` | Total number of regions started |
| `EndCount()` | Total number of regions ended |
| `AllocatedBytes()` | Bytes allocated from arena cursors |
| `ReleasedBytes()` | Arena bytes released by `region_end` |

These APIs are available only when compiling with `--alloc=region`. Importing
the region module or calling its APIs without that option is a compile-time
error:

```text
region allocation API requires --alloc=region
```

Tests should check counters before and after the function that performs the
work. This makes the lifetime boundary explicit and avoids relying only on
the function's returned value.

## Ownership and limitations

Region allocation and string reference counting are separate mechanisms. A
region releases its arena as a whole; it does not run individual destructors
or traverse each allocation. String buffers that are shared through aliases
or substring views still follow the string reference-counting rules described
in [`encoding.md`](encoding.md).

The following limitations are intentional in the current implementation:

- Region inference is function-scoped and conservative rather than a complete
  Tofte--Talpin-style constraint solver.
- Oversized allocations use the heap fallback path.
- Returned or otherwise escaping allocations are promoted to the heap.
- Complete lifetime analysis for every temporary and shared immutable string
  buffer is not yet implemented. In particular, automatic release at every
  lexical scope exit remains future work.
- The region statistics are diagnostic counters and are not a general-purpose
  manual region API.

When region mode is disabled, normal heap allocation is used and region APIs
are rejected during compilation.
