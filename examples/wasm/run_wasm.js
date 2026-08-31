const fs = require('fs');

const wasmBuffer = fs.readFileSync('app.wasm');

(async () => {
  let wasmMemory;
  // 簡易ヒープ開始アドレス（静的データ領域との衝突を避けるため64KB以降を使用）
  let heapPtr = 65536;

  function getMemoryView() {
    return new Uint8Array(wasmMemory.buffer);
  }

  function readCString(ptr) {
    if (!wasmMemory || ptr === 0) return '';
    const mem = getMemoryView();
    let end = ptr;
    while (end < mem.length && mem[end] !== 0) {
      end++;
    }
    return new TextDecoder('utf-8').decode(mem.subarray(ptr, end));
  }

  const importObject = {
    env: {
      // 1. 出力（i64 を返すため BigInt を返却）
      printf: (formatPtr, ...args) => {
        const str = readCString(formatPtr);
        process.stdout.write(str);
        return BigInt(str.length);
      },

      // 2. メモリ管理（ポインタは i32）
      malloc: (size) => {
        const s = Number(size);
        const ptr = heapPtr;
        heapPtr += (s + 7) & ~7; // 8バイトアライメント
        return ptr;
      },
      calloc: (num, size) => {
        const total = Number(num) * Number(size);
        const ptr = heapPtr;
        heapPtr += (total + 7) & ~7;
        const mem = getMemoryView();
        mem.fill(0, ptr, ptr + total);
        return ptr;
      },
      free: (ptr) => {
        // スタブ
      },

      // 3. 文字列・メモリ操作
      strlen: (ptr) => {
        const mem = getMemoryView();
        let len = 0;
        while (mem[ptr + len] !== 0) {
          len++;
        }
        return BigInt(len);
      },
      strcmp: (ptrA, ptrB) => {
        const mem = getMemoryView();
        let i = 0;
        while (true) {
          const a = mem[ptrA + i];
          const b = mem[ptrB + i];
          if (a !== b) return a - b;
          if (a === 0) return 0;
          i++;
        }
      },
      memcpy: (dst, src, len) => {
        const mem = getMemoryView();
        const length = Number(len);
        mem.copyWithin(dst, src, src + length);
        return dst;
      },
      memcmp: (ptrA, ptrB, len) => {
        const mem = getMemoryView();
        const length = Number(len);
        for (let i = 0; i < length; i++) {
          const a = mem[ptrA + i];
          const b = mem[ptrB + i];
          if (a !== b) return a - b;
        }
        return 0;
      },
    },
  };

  const { instance } = await WebAssembly.instantiate(wasmBuffer, importObject);
  wasmMemory = instance.exports.memory;

  if (instance.exports.main) {
    const exitCode = instance.exports.main(0, 0);
    process.exit(Number(exitCode));
  }
})();