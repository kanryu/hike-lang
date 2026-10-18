.PHONY: all build-hikec build-hikepack generate archive package clean run

# 設定
CLANG     := clang
LLVM_AR   := llvm-ar
GO        := go
HIKE_BIN  := ..\..\bin\hikec.exe
HIKEPACK_BIN := ..\..\bin\hikepack.exe
SYSO_OUT  := ./fizzlib/fizzlib_windows_amd64.syso
C_SRC     := ./fizzlib/c/fizzlib.c
FIZZ_OBJ  := ./fizzlib/fizzlib.o
FIZZ_ARCHIVE := ./fizzlib/libfizzlib.a

# デフォルトターゲット
all: run

# -----------------------------------------------------------------------------
# 1. Cソースからオブジェクト／静的アーカイブをビルドするパス
# -----------------------------------------------------------------------------
csyso: $(SYSO_OUT)

$(SYSO_OUT): $(C_SRC)
	@echo "==> Building direct C .syso for the stdout ABI check..."
	$(CLANG) -c $(C_SRC) -o $(SYSO_OUT) --target=x86_64-w64-windows-gnu

archive: $(FIZZ_ARCHIVE)

$(FIZZ_OBJ): $(C_SRC)
	@echo "==> Compiling external C object..."
	$(CLANG) -c $(C_SRC) -o $(FIZZ_OBJ) --target=x86_64-w64-windows-gnu

$(FIZZ_ARCHIVE): $(FIZZ_OBJ)
	@echo "==> Creating static archive..."
	$(LLVM_AR) rcs $(FIZZ_ARCHIVE) $(FIZZ_OBJ)

# -----------------------------------------------------------------------------
# 2. Hikeコンパイラを動かしてGoラッパーとアセンブリを生成するパス
# -----------------------------------------------------------------------------
build-hikec:
	@echo "==> Building hikec compiler..."
	powershell -Command "Push-Location ../..; go build -o bin/hikec.exe ./cmd/hikec; Pop-Location"

generate: build-hikec
	@echo "==> Generating Go bindings and Windows ABI stubs..."
	$(HIKE_BIN) go -v ./fizzlib

build-hikepack:
	@echo "==> Building hikepack..."
	powershell -Command "Push-Location ../..; go build -o bin/hikepack.exe ./cmd/hikepack; Pop-Location"

package: build-hikepack generate archive
	@echo "==> Packaging libfizzlib.a for the Go linker..."
	$(HIKEPACK_BIN) syso -v -target windows .

# -----------------------------------------------------------------------------
# 3. 成果物のクリーンアップ
# -----------------------------------------------------------------------------
clean:
	@echo "==> Removing generated bindings, archive objects, and linker package..."
	powershell -Command "@( '.\\fizzlib\\fizzlib.go', '.\\fizzlib\\stub_windows_amd64.s', '.\\fizzlib\\fizzlib.ll', '.\\fizzlib\\fizzlib_windows_amd64.syso', '.\\fizzlib\\fizzlib.o', '.\\fizzlib\\libfizzlib.a', '.\\fizzlib\\hikepack_archive_windows.go' ) | Where-Object { Test-Path -LiteralPath $$_ } | Remove-Item -Force; if (Test-Path -LiteralPath '.\\fizzlib_native') { Remove-Item -LiteralPath '.\\fizzlib_native' -Recurse -Force }"

# -----------------------------------------------------------------------------
# 4. 実行
# -----------------------------------------------------------------------------
run: generate csyso
	$(GO) run -ldflags "-linkmode=external -extldflags=-lkernel32" .
