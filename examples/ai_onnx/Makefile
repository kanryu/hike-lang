HIKEC = go run ../../cmd/hikec
CLANG = clang
PYTHON = python
CURL = curl.exe
UNZIP = unzip

ORT_VERSION = 1.30.0
ORT_PACKAGE = onnxruntime-win-x64-gpu_cuda12-$(ORT_VERSION)
ORT_URL = https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/$(ORT_PACKAGE).zip
ORT_ARCHIVE = $(ORT_PACKAGE).zip
ORT_ROOT = $(ORT_PACKAGE)

MODEL_URL = https://huggingface.co/lokCX/4x-Ultrasharp/resolve/main/4x-UltraSharp.pth
ORT_DLL = $(ORT_ROOT)/lib/onnxruntime.dll

.PHONY: all download runtime convert emit-ir build run clean

all: build

download: 4x-UltraSharp.pth $(ORT_DLL)

4x-UltraSharp.pth:
	$(CURL) -L --fail --retry 3 -o $@ $(MODEL_URL)

$(ORT_ARCHIVE):
	$(CURL) -L --fail --retry 3 -o $@ $(ORT_URL)

runtime: $(ORT_DLL)

$(ORT_DLL): $(ORT_ARCHIVE)
	$(UNZIP) -q -o $< -d .

convert: 4x-UltraSharp.onnx

4x-UltraSharp.onnx: 4x-UltraSharp.pth export_model.py
	$(PYTHON) export_model.py --input $< --output $@

emit-ir: main.ll

main.ll: main.hike
	$(HIKEC) emit-ir -o $@ $<

build: main.ll 4x-UltraSharp.onnx upscale_bridge.c $(ORT_DLL)
	$(CLANG) -O2 main.ll upscale_bridge.c -I"$(ORT_ROOT)/include" -L"$(ORT_ROOT)/lib" -lonnxruntime -o hike-ai.exe
	cp "$(ORT_DLL)" onnxruntime.dll

run: build
	.\hike-ai.exe

clean:
	rm -f main.ll hike-ai.exe onnxruntime.dll icon256.bmp
