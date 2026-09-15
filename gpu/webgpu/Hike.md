# Hike WebGPU Module

This directory is structured as the standalone Hike module published at:

<https://github.com/kanryu/hike-gpu-webgpu>

The intended import path is:

```hike
import "github.com/kanryu/hike-gpu-webgpu/webgpu"
```

The module exposes a small typed wrapper around a browser-provided WebGPU
bridge. `webgpu.NewContext` creates an opaque context for a canvas element,
`RenderTriangle` draws a vertex-colored triangle with a rotation angle, and
`Close` releases the associated browser resources.

## Dependency usage

After publishing this directory as its own repository, a Hike application can
declare the dependency in `hike.mod`:

```text
module my-project

hike 0.1.0

require github.com/kanryu/hike-gpu-webgpu v0.1.0
```

The planned `hike get` command will clone required Git modules into the local
Hike module cache. Builds then resolve the package by its module import path.
Until that command is available, development projects can use a local
replacement:

```text
replace github.com/kanryu/hike-gpu-webgpu => ../hike-gpu-webgpu
```

## Browser bridge

The four `webgpu_*` external functions are intentionally host-defined. A
browser runner supplies them by creating a WebGPU adapter/device, configuring
the canvas context, uploading the rotating triangle's vertex data, and
submitting the render pass. This keeps WebGPU JavaScript objects out of Hike's
wasm32 memory model while allowing the Hike API to remain stable.
