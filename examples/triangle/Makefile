HIKEC = go run ../../cmd/hikec
CLANG = clang
PYTHON = python
PORT ?= 8000

SRC = main.hike
LL = main.ll
WASM = main.wasm

.PHONY: all build serve clean

all: build

build: $(WASM)

$(LL): $(SRC) ../../gpu/webgpu/webgpu/webgpu.hike ../../gpu/webgpu/hike.mod
	$(HIKEC) emit-ir -target wasm32 -o $(LL) $(SRC)

$(WASM): $(LL)
	$(CLANG) --target=wasm32-unknown-unknown -O2 -nostdlib -Wl,--no-entry -Wl,--export-all -Wl,--allow-undefined $(LL) -o $(WASM)

serve: build
	$(PYTHON) -m http.server $(PORT)

clean:
	rm -f $(LL) $(WASM)
