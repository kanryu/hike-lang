HIKEC ?= go run ../../cmd/hikec
PYTHON := $(subst \,/,$(shell where.exe python 2>/dev/null | head -n 1))
ifeq ($(PYTHON),)
PYTHON := python
endif

TARGET = shift_jis_example.exe
SRCS = main.hike

.PHONY: all run clean

all: $(TARGET) shift_jis.txt

# 1. サンプル入力用 Shift_JIS ファイルの自動生成
shift_jis.txt:
	@$(PYTHON) -c "open('shift_jis.txt', 'wb').write('日本語文字列変換の実証。'.encode('shift_jis'))"
	@echo "Generated input Shift_JIS file: shift_jis.txt"

# 2. Hike プログラムのコンパイル
$(TARGET): $(SRCS)
	$(HIKEC) build -o $(TARGET) $(SRCS)

# 3. 実行および結果の相互検証
run: all
	@echo "=== [1] 実行 ==="
	./$(TARGET)
	@echo ""
	@echo "=== [2] 出力 UTF-8 ファイルの確認 ==="
	@$(PYTHON) -c "with open('utf-8_out.txt', 'r', encoding='utf-8') as f: print(f.read())"
	@echo ""
	@echo "=== [3] 出力 Shift_JIS ファイルのバイナリ検証 ==="
	@$(PYTHON) -c "with open('shift_jis_out.txt', 'rb') as f: print('Decoded from output file:', f.read().decode('shift_jis'))"
	@echo "=== 検証完了 ==="

clean:
	rm -f $(TARGET) shift_jis.txt utf-8_out.txt shift_jis_out.txt