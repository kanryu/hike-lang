HIKEC = go run ../../cmd/hikec
PORT ?= 8000

.PHONY: build serve clean

build:
	$(HIKEC) build -target wasm32 -go-hike -o wasm-hikec.wasm ../../cmd/wasm-hikec/main_wasm.go

serve: build
	python -m http.server $(PORT)

clean:
	rm -f wasm-hikec.wasm runtime.js main.wat
