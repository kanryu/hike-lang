HIKEC = go run ../../cmd/hikec
CLANG = clang
PYTHON = python

SRC = libcalc.hike
LL = libcalc.ll

# OS判定によるターゲット拡張子と削除コマンドの設定
ifeq ($(OS),Windows_NT)
    TARGET = libcalc.dll
    CLANG_SHARED_FLAGS = -shared -O3 -Wl,--export-all-symbols
    RM = rm -f
else
    TARGET = libcalc.so
    CLANG_SHARED_FLAGS = -shared -fPIC -O3
    RM = rm -f

endif

.PHONY: all clean test

# デフォルトターゲット: hikecでLLVM IRを生成し、clangで共有ライブラリ(DLL/SO)をビルド
all: $(TARGET)

$(LL): $(SRC)
	$(HIKEC) $(SRC) -o $(LL)

$(TARGET): $(LL)
	$(CLANG) $(CLANG_SHARED_FLAGS) $(LL) -o $(TARGET)

# テスト実行: ビルド後にPythonクライアントを実行
test: $(TARGET)
	$(PYTHON) test_client.py

# クリーンアップ: 中間LLファイル、DLL、SOファイルを削除
clean:


	$(RM) *.ll *.dll *.so



