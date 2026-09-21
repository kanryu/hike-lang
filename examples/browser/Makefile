HIKEC = go run ../../cmd/hikec
PYTHON = python
PORT ?= 8000

SRC = main.hike
TARGET = main.wasm

.PHONY: all build serve clean

all: build

build: $(TARGET)

$(TARGET): $(SRC)
	$(HIKEC) build -g -target wabt -wasm-mode=normal \
		--source-map-base=http://localhost:8000/ --no-source-map $(SRC) -o $(TARGET)

serve: build
	@echo "Starting local server at http://localhost:$(PORT) ..."
	$(PYTHON) -m http.server $(PORT)

clean:
	rm -f $(TARGET) $(TARGET).map *.ll runtime.js
