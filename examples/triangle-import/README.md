# TriangleImport

This example has the same rotating WebGPU triangle goal as
[`../triangle`](../triangle), but the application contains only rendering
logic. WebGPU resource management and the browser bridge are imported from the
reusable `gpu/webgpu` Hike module:

```hike
import "github.com/kanryu/hike-gpu-webgpu/gpu/webgpu"
```

The local `hike.mod` declares the WebGPU module with `require`. The checked-out
copy under `.hike/deps` is the same layout produced by `hikec get`.

## External library setup

TriangleImport is an example project for importing an external Hike library.
The WebGPU package is intentionally not vendored into the application source,
so the project cannot be built until the required module has been installed
under `.hike/deps`.

The dependency setup supports these three forms:

1. Download every module declared by `hike.mod` in declaration order:

   ```bash
   hikec get
   ```

2. Download one module, using the version recorded in `hike.mod` (or
   `latest` when no version is recorded):

   ```bash
   hikec get github.com/kanryu/hike-gpu-webgpu
   ```

3. Download one exact version from its source archive:

   ```bash
   hikec get github.com/kanryu/hike-gpu-webgpu@v0.1.0
   ```

All three forms install the module at
`.hike/deps/github.com/kanryu/hike-gpu-webgpu`. Run one of them before
`make build`; otherwise the imported `webgpu` package cannot be resolved and
the project will not compile. When a module path or an explicit release
version is supplied, the existing module directory is removed first and the
requested source is installed again. The matching `require` entry in
`hike.mod` is also rewritten to the requested version.

Build and serve the example with:

```bash
make build
make serve
```

Open `http://localhost:8000/` in a WebGPU-capable browser. The module keeps
browser-owned WebGPU objects behind opaque integer handles and exposes generic
context, buffer, shader, pipeline, bind-group, draw, resize, and close
operations rather than triangle-specific functions.
