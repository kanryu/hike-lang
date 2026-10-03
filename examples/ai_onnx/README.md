# Hike 4x-UltraSharp ONNX example

This project downloads `4x-UltraSharp.pth` from Hugging Face, converts it to
`4x-UltraSharp.onnx` once with Python, and then runs the ONNX model from a Hike
program on Windows. The input `icon64.bmp` is converted from 24-bit BGR BMP
pixels to an RGB `CHW` float tensor, passed through ONNX Runtime, and written
as the 4x larger `icon256.bmp`.

The application follows the same thin C wrapper plus Hike orchestration pattern
as the OpenCL room example:

```text
4x-UltraSharp.pth -> export_model.py -> 4x-UltraSharp.onnx
                                           |
icon64.bmp  -> main.hike -> upscale_bridge.c -> onnxruntime.dll
                                           |
                              icon256.bmp (4x width and height)
```

`main.hike` owns BMP parsing, RGB/CHW conversion, tensor allocation, result
conversion, and BMP encoding. `upscale_bridge.c` owns only the ONNX Runtime C
API session and tensor invocation.

## Requirements

- Windows and Clang
- Python with `torch` and `onnx`
- `curl.exe`
- `unzip`

Install the Python packages in the environment used for export:

```powershell
python -m pip install torch onnx
```

The Makefile downloads the official ONNX Runtime GPU package from the
Microsoft GitHub release and extracts it into the sample directory. The
archive contains `onnxruntime_c_api.h`, `onnxruntime.lib`, and
`onnxruntime.dll`:

```text
https://github.com/microsoft/onnxruntime/releases/download/v1.30.0/onnxruntime-win-x64-gpu_cuda12-1.30.0.zip
```

## Build and run

The sample includes `icon64.bmp` as its input image. Run:

```powershell
cd examples/ai_onnx
make download
make convert
make run
```

The final image is written to `icon256.bmp`. The sample expects the
exported model to have an input named `input`, an output named `output`, and a
4x spatial scale, which is exactly how `export_model.py` generates it.
