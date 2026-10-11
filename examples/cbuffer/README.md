# cbuffer example

This example creates a `cbuffer` from an ordinary Hike `[]byte` through the
compiler built-in `cbuffer(data, length, release)` function, reads a byte
through cbuffer indexing, and verifies that a range expression produces an
ordinary Hike slice copy. The nil release pointer uses the runtime's default
The release callback is a no-op because the original Hike slice retains
ownership of the allocation. The test verifies that the original slice remains
usable after `cbuffer.Close()`.

Run the end-to-end checks with:

```text
make test
```

This builds and runs the native sample, validates the Wasm binary with WABT,
and executes the Wasm sample with the Node.js host harness.
