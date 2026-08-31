# ==============================================================================
# Hike WebAssembly Example Makefile
# ==============================================================================

HIKEC  = go run ../../cmd/hikec
CLANG  = clang
NODE   = node

SRC    = main.hike
LL     = main.ll
WASM   = app.wasm
RUNNER = run_wasm.js

.PHONY: all build run clean

all: build run

build: $(WASM)

$(LL): $(SRC)
	$(HIKEC) -target wasm32 -o $(LL) $(SRC)

$(WASM): $(LL)
	$(CLANG) --target=wasm32-unknown-unknown -O2 -nostdlib -Wl,--no-entry -Wl,--export-all -Wl,--allow-undefined $(LL) -o $(WASM)

run: $(WASM)
	$(NODE) $(RUNNER)

clean:
	rm -f $(LL) $(WASM) main.hike.ll