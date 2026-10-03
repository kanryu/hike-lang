HIKEC = go run ../../cmd/hikec
CLANG = clang
PYTHON = python
CURL = curl.exe

# Set ORT_ROOT to an extracted Windows ONNX Runtime package.
ORT_ROOT ?= C:/local/gnu/onnxruntime-win-x64-gpu_cuda12-1.30.0

MODEL_URL = https://huggingface.co/lokCX/4x-Ultrasharp/resolve/main/4x-UltraSharp.pth
ORT_DLL = $(ORT_ROOT)/lib/onnxruntime.dll

.PHONY: all download convert emit-ir build run clean

all: build

download: 4x-UltraSharp.pth

4x-UltraSharp.pth:
	$(CURL) -L --fail --retry 3 -o $@ $(MODEL_URL)

convert: 4x-UltraSharp.onnx

4x-UltraSharp.onnx: 4x-UltraSharp.pth export_model.py
	$(PYTHON) export_model.py --input $< --output $@

emit-ir: main.ll

main.ll: main.hike
	$(HIKEC) emit-ir -o $@ $<

build: main.ll 4x-UltraSharp.onnx upscale_bridge.c
	$(CLANG) -O2 main.ll upscale_bridge.c -I"$(ORT_ROOT)/include" -L"$(ORT_ROOT)/lib" -lonnxruntime -o hike-ai.exe
	cp "$(ORT_DLL)" onnxruntime.dll

run: build
	.\hike-ai.exe

clean:
	rm -f main.ll hike-ai.exe onnxruntime.dll icon256.bmp
