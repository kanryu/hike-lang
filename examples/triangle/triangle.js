const statusElement = document.getElementById("status");
const canvas = document.getElementById("gpu-canvas");
const canvasInfoElement = document.getElementById("canvas-info");
const DEBUG_WEBGPU = true;

function updateCanvasInfo() {
  if (canvasInfoElement) {
    canvasInfoElement.textContent = `Canvas ID: ${canvas.id} · framebuffer: ${canvas.width} × ${canvas.height}`;
  }
}

updateCanvasInfo();

function gpuLog(event, details = {}) {
  if (!DEBUG_WEBGPU) return;
  console.log(`[Hike WebGPU] ${event}`, details);
}

function gpuWarn(event, details = {}) {
  console.warn(`[Hike WebGPU] ${event}`, details);
}

let wasm;
let wasmMemory;
let heap = 65536;
const gpuContexts = new Map();
let nextGPUHandle = 1;
let frameCount = 0;

function memoryView() {
  return new Uint8Array(wasmMemory.buffer);
}

function decodeString(pointer, length) {
  return new TextDecoder().decode(memoryView().subarray(Number(pointer), Number(pointer) + Number(length)));
}

function malloc(size) {
  const pointer = heap;
  heap += (Number(size) + 15) & ~15;
  const requiredPages = Math.ceil(heap / 65536);
  const currentPages = wasmMemory.buffer.byteLength / 65536;
  if (requiredPages > currentPages) wasmMemory.grow(requiredPages - currentPages);
  return pointer;
}

function createTrianglePipeline(device, format) {
  gpuLog("createShaderModule:begin", { format });
  const shader = device.createShaderModule({ code: `
struct Uniforms { angle: f32 }
@group(0) @binding(0) var<uniform> uniforms: Uniforms;

struct VertexOutput {
  @builtin(position) position: vec4<f32>,
  @location(0) color: vec3<f32>,
}

@vertex
fn vs(@location(0) position: vec3<f32>, @location(1) color: vec3<f32>) -> VertexOutput {
  let c = cos(uniforms.angle);
  let s = sin(uniforms.angle);
  // Rotate around the view axis so the triangle remains equilateral on the
  // framebuffer instead of becoming edge-on during a Y-axis rotation.
  let rotated = vec3<f32>(
    c * position.x - s * position.y,
    s * position.x + c * position.y,
    position.z
  );
  var output: VertexOutput;
  output.position = vec4<f32>(rotated.x, rotated.y, rotated.z, 1.0);
  output.color = color;
  return output;
}

@fragment
fn fs(input: VertexOutput) -> @location(0) vec4<f32> {
  return vec4<f32>(input.color, 1.0);
}` });
  gpuLog("createShaderModule:returned", { shader });

  gpuLog("createRenderPipeline:begin");
  const pipeline = device.createRenderPipeline({
    layout: "auto",
    vertex: {
      module: shader,
      entryPoint: "vs",
      buffers: [{
        arrayStride: 24,
        attributes: [
          { shaderLocation: 0, offset: 0, format: "float32x3" },
          { shaderLocation: 1, offset: 12, format: "float32x3" },
        ],
      }],
    },
    fragment: { module: shader, entryPoint: "fs", targets: [{ format }] },
    primitive: { topology: "triangle-list", cullMode: "none" },
  });
  gpuLog("createRenderPipeline:returned", { pipeline });

  const vertices = new Float32Array([
     0.0,       0.80, 0.0, 1.0, 0.2, 0.3,
    -0.69282, -0.40, 0.0, 0.2, 1.0, 0.4,
     0.69282, -0.40, 0.0, 0.2, 0.5, 1.0,
  ]);
  const vertexBuffer = device.createBuffer({ size: vertices.byteLength, usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST });
  gpuLog("createBuffer:vertex", { byteLength: vertices.byteLength, vertexBuffer });
  device.queue.writeBuffer(vertexBuffer, 0, vertices);
  gpuLog("queue.writeBuffer:vertex:returned", { bytes: vertices.byteLength });

  const uniformBuffer = device.createBuffer({ size: 16, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  gpuLog("createBuffer:uniform", { byteLength: 16, uniformBuffer });
  const bindGroup = device.createBindGroup({
    layout: pipeline.getBindGroupLayout(0),
    entries: [{ binding: 0, resource: { buffer: uniformBuffer } }],
  });
  gpuLog("createBindGroup:returned", { bindGroup });
  return { pipeline, vertexBuffer, uniformBuffer, bindGroup };
}

function webgpu_create_context(canvasPointer, canvasLength, width, height) {
  gpuLog("wasm->webgpu_create_context:begin", {
    canvasPointer: Number(canvasPointer),
    canvasLength: Number(canvasLength),
    width: Number(width),
    height: Number(height),
  });
  const handle = nextGPUHandle++;
  const state = { ready: false, angle: 0, width: Number(width), height: Number(height) };
  gpuContexts.set(handle, state);
  const canvasId = decodeString(canvasPointer, canvasLength);
  const target = document.getElementById(canvasId);
  gpuLog("webgpu_create_context:canvas", { handle, canvasId, found: Boolean(target) });
  if (!target) {
    state.error = `Canvas element not found: ${canvasId}`;
    statusElement.textContent = state.error;
    return handle;
  }
  if (!navigator.gpu) {
    state.error = "WebGPU is not available in this browser or context";
    statusElement.textContent = state.error;
    return handle;
  }

  gpuLog("navigator.gpu.requestAdapter:begin", { handle });
  state.initialization = (async () => {
    const adapter = await navigator.gpu.requestAdapter();
    if (!adapter) throw new Error("No WebGPU adapter was found");
    gpuLog("navigator.gpu.requestAdapter:resolved", { handle, adapter });
    const device = await adapter.requestDevice();
    gpuLog("adapter.requestDevice:resolved", { handle, device });
    device.addEventListener("uncapturederror", (event) => {
      gpuWarn("GPUUncapturedError", { handle, error: event.error });
    });
    device.lost.then((info) => {
      gpuWarn("GPUDevice.lost", { handle, reason: info.reason, message: info.message });
    });
    const gpuContext = target.getContext("webgpu");
    if (!gpuContext) throw new Error("Could not create a WebGPU canvas context");
    gpuLog("canvas.getContext:webgpu:returned", { handle, gpuContext });
    const format = navigator.gpu.getPreferredCanvasFormat();
    gpuLog("navigator.gpu.getPreferredCanvasFormat:returned", { handle, format });
    gpuContext.configure({ device, format, alphaMode: "opaque" });
    gpuLog("GPUCanvasContext.configure:returned", { handle, format });
    state.device = device;
    state.canvas = target;
    state.gpuContext = gpuContext;
    state.resources = createTrianglePipeline(device, format);
    state.ready = true;
    gpuLog("webgpu_create_context:ready", { handle, format });
    statusElement.textContent = "WebGPU active — rotating triangle";
  })().catch((error) => {
    state.error = error instanceof Error ? error.message : String(error);
    statusElement.textContent = `WebGPU initialization failed: ${state.error}`;
    gpuWarn("webgpu_create_context:failed", { handle, error });
    console.error("WebGPU initialization failed", error);
  });
  gpuLog("wasm->webgpu_create_context:return", { handle });
  return handle;
}

function webgpu_resize_context(handle, width, height) {
  gpuLog("wasm->webgpu_resize_context:begin", { handle: Number(handle), width: Number(width), height: Number(height) });
  const state = gpuContexts.get(Number(handle));
  if (!state) {
    gpuWarn("webgpu_resize_context:unknown-handle", { handle: Number(handle) });
    return;
  }
  state.width = Number(width);
  state.height = Number(height);
  if (state.canvas) {
    state.canvas.width = state.width;
    state.canvas.height = state.height;
    updateCanvasInfo();
  }
  gpuLog("wasm->webgpu_resize_context:return", { handle: Number(handle), width: state.width, height: state.height });
}

function webgpu_destroy_context(handle) {
  gpuLog("wasm->webgpu_destroy_context:begin", { handle: Number(handle) });
  const state = gpuContexts.get(Number(handle));
  if (state?.device) state.device.destroy();
  gpuContexts.delete(Number(handle));
  gpuLog("wasm->webgpu_destroy_context:return", { handle: Number(handle) });
}

function webgpu_render_triangle(handle, angle) {
  const state = gpuContexts.get(Number(handle));
  gpuLog("wasm->webgpu_render_triangle:begin", {
    frame: frameCount,
    handle: Number(handle),
    angle: Number(angle),
    ready: Boolean(state?.ready),
    error: state?.error || null,
  });
  if (!state || !state.ready || state.error) {
    gpuWarn("webgpu_render_triangle:skipped", { frame: frameCount, handle: Number(handle), reason: state?.error || "not-ready" });
    return;
  }
  state.angle = Number(angle);
  const { device, gpuContext, resources } = state;
  device.queue.writeBuffer(resources.uniformBuffer, 0, new Float32Array([state.angle]));
  gpuLog("queue.writeBuffer:uniform:returned", { frame: frameCount, angle: state.angle });
  const encoder = device.createCommandEncoder();
  gpuLog("createCommandEncoder:returned", { frame: frameCount, encoder });
  const currentTexture = gpuContext.getCurrentTexture();
  gpuLog("GPUCanvasContext.getCurrentTexture:returned", { frame: frameCount, currentTexture });
  const pass = encoder.beginRenderPass({
    colorAttachments: [{
      view: currentTexture.createView(),
      clearValue: { r: 0.025, g: 0.05, b: 0.11, a: 1 },
      loadOp: "clear",
      storeOp: "store",
    }],
  });
  gpuLog("beginRenderPass:returned", { frame: frameCount, pass });
  pass.setPipeline(resources.pipeline);
  pass.setBindGroup(0, resources.bindGroup);
  pass.setVertexBuffer(0, resources.vertexBuffer);
  pass.draw(3);
  pass.end();
  gpuLog("GPURenderPassEncoder.end:returned", { frame: frameCount });
  const commandBuffer = encoder.finish();
  gpuLog("GPUCommandEncoder.finish:returned", { frame: frameCount, commandBuffer });
  device.queue.submit([commandBuffer]);
  gpuLog("GPUQueue.submit:returned", { frame: frameCount });
}

const imports = {
  env: {
    memory: new WebAssembly.Memory({ initial: 256, maximum: 512 }),
    malloc,
    calloc: (count, size) => {
      const pointer = malloc(Number(count) * Number(size));
      memoryView().fill(0, pointer, pointer + Number(count) * Number(size));
      return pointer;
    },
    free: () => {},
    memcpy: (destination, source, length) => {
      memoryView().copyWithin(Number(destination), Number(source), Number(source) + Number(length));
      return destination;
    },
    strlen: (pointer) => {
      let length = 0;
      const memory = memoryView();
      while (memory[Number(pointer) + length] !== 0) length++;
      return BigInt(length);
    },
    webgpu_create_context,
    webgpu_resize_context,
    webgpu_destroy_context,
    webgpu_render_triangle,
  },
};

async function start() {
  gpuLog("wasm-load:begin", { url: "main.wasm" });
  const response = await fetch("main.wasm");
  gpuLog("wasm-load:response", { ok: response.ok, status: response.status, contentLength: response.headers.get("content-length") });
  const bytes = await response.arrayBuffer();
  gpuLog("wasm-load:bytes", { byteLength: bytes.byteLength });
  wasmMemory = imports.env.memory;
  const result = await WebAssembly.instantiate(bytes, imports);
  wasm = result.instance;
  // The current Hike wasm linker defines and exports its own memory. Reading
  // the separately-created import memory loses static data such as strings.
  wasmMemory = wasm.exports.memory;
  const heapBaseExport = wasm.exports.__heap_base;
  if (heapBaseExport !== undefined) {
    heap = Number(heapBaseExport.value ?? heapBaseExport);
  }
  gpuLog("WebAssembly.instantiate:returned", {
    exports: Object.keys(wasm.exports),
    imports: WebAssembly.Module.imports(result.module),
    memoryBytes: wasmMemory ? wasmMemory.buffer.byteLength : 0,
    heapBase: heap,
  });
  gpuLog("wasm.exports.InitApp:begin");
  wasm.exports.InitApp();
  gpuLog("wasm.exports.InitApp:return");
  statusElement.textContent = "Initializing WebGPU device…";

  let startTime;
  function frame(time) {
    frameCount++;
    if (startTime === undefined) startTime = time;
    gpuLog("frame:begin", { frame: frameCount, time, angle: (time - startTime) * 0.001 });
    if (wasm.exports.RenderFrame) {
      wasm.exports.RenderFrame((time - startTime) * 0.001);
      gpuLog("wasm.exports.RenderFrame:return", { frame: frameCount });
    }
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
}

start().catch((error) => {
  console.error(error);
  statusElement.textContent = `WebGPU startup failed: ${error.message}`;
});
