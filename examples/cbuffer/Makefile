HIKEC ?= go run ../../cmd/hikec
NATIVE ?= cbuffer-e2e.exe
WASM ?= cbuffer-e2e.wasm
WABT_VALIDATE ?= wasm-validate

.PHONY: all build test test-native test-wabt test-wasm run clean MakeClean

all: test

build: $(NATIVE) $(WASM)

$(NATIVE): main.hike
	$(HIKEC) build -target windows -o $@ $<

$(WASM): main.hike
	$(HIKEC) build -target wasm32 -o $@ $<

test: test-native test-wabt test-wasm

test-native: $(NATIVE)
	./$(NATIVE)

test-wabt: $(WASM)
	$(WABT_VALIDATE) $(WASM)

test-wasm: $(WASM) test_wasm.js
	node test_wasm.js $(WASM)

run: test-native

MakeClean:
	rm -f $(NATIVE) $(WASM)

clean: MakeClean
