HIKEC = go run ../../cmd/hikec
PYTHON = python
PORT ?= 8000

SRC = main.hike
WASM = main.wasm

.PHONY: all build serve clean

all: build

build: $(WASM)

$(WASM): $(SRC)
	$(HIKEC) build -target wasm32 $(SRC) -o $(WASM)

serve: build
	$(PYTHON) -m http.server $(PORT)

clean:
	rm -f $(WASM) runtime.js
