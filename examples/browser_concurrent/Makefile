# ==============================================================================
# Hike Browser Concurrent Example Makefile
# ==============================================================================

HIKEC       ?= go run ../../cmd/hikec
PORT        ?= 8000
PYTHON := $(subst \,/,$(shell where.exe python 2>/dev/null | head -n 1))
ifeq ($(PYTHON),)
PYTHON := python
endif

TARGET      ?= wasm32

.PHONY: all build dev serve clean

all: build

# 1. WASMバイナリのビルド
build: main.wasm

main.wasm: main.hike
	$(HIKEC) build -target $(TARGET) -o main.wasm main.hike

# 2. ビルドしてそのまま開発サーバーを立ち上げる
dev: build serve

# 3. SharedArrayBuffer用ヘッダー（COOP/COEP）を付与した開発サーバーの起動
serve:
	@$(PYTHON) server.py $(PORT)

# 4. 生成バイナリおよび中間IRのクリーンアップ
clean:
	rm -f main.wasm *.ll *.js