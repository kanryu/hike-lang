# Hike as a Better C for GPGPU Programming

This example demonstrates a practical role for Hike as a modern, more
expressive alternative to C at the host-program boundary of a GPGPU
application.

The simulation described in [README.md](README.md) and implemented in
[main.hike](main.hike) has a direct correspondence between specification and
source code:

- a `64 x 64 x 64` temperature grid;
- 1200 simulation steps;
- a ceiling-mounted air conditioner emitting cold air;
- snapshots every 300 steps; and
- 24-bit BMP output of the `Y-Z` cross-section.

After reading the README, the structure of `main.hike` makes the execution
flow immediately apparent. There is no framework ceremony or unnecessary
layer of indirection between the simulation description and its implementation.

## C-level control without C-level noise

Hike preserves the capabilities that make C effective for systems and GPGPU
work:

- raw pointers such as `*byte` and `*float32`;
- explicit `cstring` and length arguments at ABI boundaries;
- direct calls to C runtime functions such as `fopen`, `fwrite`, `fclose`, and
  `printf`; and
- direct linking to the OpenCL bridge implemented in
  [room_opencl.c](room_opencl.c).

The boundary with the operating system, GPU driver, and OpenCL runtime remains
explicit. Hike does not hide the data layout or impose a managed runtime on
the application. The C file is limited to OpenCL context creation, buffer
management, kernel compilation, kernel dispatch, and result download, while
the application-level orchestration remains in Hike.

## Slices reduce memory-management overhead

In C, the equivalent code would typically require explicit allocation,
`NULL` checks, byte-size calculations, and carefully coordinated cleanup paths.
Hike expresses the same buffers directly:

```hike
field := make([]float32, cells)
header := make([]byte, 54)
row := make([]byte, rowSize)
```

The slice carries its length, so operations such as `len(header)`,
`len(row)`, and indexed access remain tied to the buffer being operated on.
This removes a large class of bookkeeping errors while still allowing the
first element to be passed to C with `&field[0]` or `&row[0]`.

The result is not a claim that Hike eliminates all ownership decisions. A
program crossing a C ABI must still understand who owns external resources,
when a pointer is valid, and when an OpenCL object must be released. Hike
reduces the routine allocation and buffer-management burden without hiding
those important boundaries.

## Self-describing buffers and raw strings

The BMP writer can determine its own buffer sizes and pass them directly to
`fwrite`:

```hike
fwrite(&header[0], 1, len(header), file)
fwrite(&row[0], 1, len(row), file)
```

Keeping the buffer and its length together makes accidental size mismatches
less likely than manually maintaining a pointer and a separate integer in C.

The OpenCL kernel is also defined directly in Hike as a multiline raw string:

```hike
const kernelSource = `__kernel void step(__global const float *src,
    __global float *dst) {
    // OpenCL kernel implementation
}`
```

The source is passed to the bridge as a C string together with
`len(kernelSource)`. This is substantially easier to review and maintain than
an escaped C string assembled from many quoted lines.

## Why this is simpler than a C++ escalation

C++ can address many of C's pain points, but it also introduces a large
language surface: class hierarchies, implicit construction and destruction,
templates, overload resolution, and metaprogramming. Those features can be
valuable, but they can also obscure a small systems program.

This example keeps the procedural structure and predictable data layout of C
while adding a compact set of conveniences:

- slices for ordinary contiguous buffers;
- built-in length operations;
- concise function and control-flow syntax; and
- multiline raw strings for embedded source code.

Hike therefore occupies a useful middle ground: direct enough for driver and
GPU integration, but concise enough that the source remains close to the
problem statement.

## Trade-offs

Hike is not a replacement for every C use case. This example still relies on:

- a small C bridge for the OpenCL C API;
- a platform OpenCL development kit and runtime;
- Clang and the platform linker; and
- explicit ABI declarations in Hike.

Those dependencies are appropriate for a GPGPU application. They also make
the comparison honest: Hike provides the application language and memory-safe
buffer conveniences, while C remains available at the narrow hardware-facing
boundary where it is most useful.

## Conclusion

The room simulation shows how Hike can function as a “Better C” for GPGPU
programming. It keeps low-level control, predictable layout, and direct
interoperability, while removing repetitive allocation bookkeeping and making
embedded GPU source readable. The result is a short program whose structure
can be understood directly from its README and whose important hardware
boundaries remain visible in the code.
