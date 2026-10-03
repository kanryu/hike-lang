# 俳句/OpenCL room cooling sample

`main.hike` is the sample program. It controls an OpenCL simulation of a
`64 x 64 x 64` room initially filled with temperature `1.0`. An air
conditioner is placed at the center of the north ceiling edge and emits air at
temperature `0.0` toward the room. The GPU kernel advances diffusion and
advection for 1200 steps.

The small `room_opencl.c` file is only an ABI bridge for the OpenCL C API.
The simulation loop, result download request, and bitmap generation are in
`main.hike`.

The output `room_yz.bmp` is a vertical `Y-Z` slice through the air conditioner
and its blowing direction. Temperature `1.0` is red, temperature `0.0` is
blue, and intermediate temperatures are blended.

## Build on Windows with CUDA 11.7

The Makefile uses the CUDA installation from:

```text
C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v11.7
```

Run from this directory:

```powershell
make
```

Or run the commands directly:

```powershell
go run ../../../cmd/hikec emit-ir -o main.ll main.hike
clang -O2 main.ll room_opencl.c `
  -I"C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v11.7\include" `
  -L"C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v11.7\lib\x64" `
  -lOpenCL -o room.exe
.\room.exe
```

The executable prints progress every 100 steps and writes the final
`room_yz.bmp`. It also writes `room_yz_0300.bmp`, `room_yz_0600.bmp`, and
`room_yz_0900.bmp` so the cooling and circulation can be viewed over time.
