.PHONY: all csyso hikesyso clean run

# 設定
CLANG     := clang
GO        := go
HIKE_BIN  := ..\..\bin\hikec.exe
SYSO_OUT  := ./fizzlib/fizzlib_windows_amd64.syso
C_SRC     := ./fizzlib/c/fizzlib.c

# デフォルトターゲット
all: csyso

# -----------------------------------------------------------------------------
# 1. Cソースから .syso をビルドするパス
# -----------------------------------------------------------------------------
csyso:
	@echo "==> Building .syso directly from C..."
	$(CLANG) -c $(C_SRC) -o $(SYSO_OUT)

# -----------------------------------------------------------------------------
# 2. Hikeコンパイラを動かして .syso を生成するパス
# -----------------------------------------------------------------------------
hikesyso:
	@echo "==> Building hikec compiler..."
	powershell -Command "Push-Location ../..; go build -o bin/hikec.exe ./cmd/hikec; Pop-Location"
	@echo "==> Running hikec go mode..."
	$(HIKE_BIN) go -v ./fizzlib

# -----------------------------------------------------------------------------
# 3. 成果物のクリーンアップ
# -----------------------------------------------------------------------------
clean:
	@echo "==> Removing .syso files..."
	powershell -Command "Get-ChildItem -Path ./fizzlib -Filter *.syso -File | Remove-Item -Force"

# -----------------------------------------------------------------------------
# 4. 実行
# -----------------------------------------------------------------------------
run:
	$(GO) run main.go