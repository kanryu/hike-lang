let wasmInstance = null;
let sharedMemory = null;
let nextEventId = 1;
const eventStates = new Map();

// JavaScriptホスト側で許可するヒープ容量 (KB単位)
// 128KBの初期メモリ内で安全に運用できる 32KB を許可
const CONFIG_HEAP_KB = 32;

self.onmessage = async (e) => {
    if (e.data.type === 'init') {
        const { wasmBytes, memory } = e.data;
        sharedMemory = memory;

        const importObject = {
            env: {
                memory: sharedMemory,

                // --- 1. WASM起動時のメモリハンドシェイク (ここが必須) ---
                requestHeapSizeKB: () => {
                    self.postMessage({
                        type: 'log',
                        message: `[Host] WASMからのメモリ要求を受信: ${CONFIG_HEAP_KB} KB を許可`
                    });
                    return CONFIG_HEAP_KB;
                },

                // --- 2. DOM コールバック ---
                displayResult: (sum, mul) => {
                    self.postMessage({ type: 'result', sum: Number(sum), mul: Number(mul) });
                },

                // --- 3. 32-bit ヒープアロケータ ---
                malloc: (size) => {
                    const mem = (wasmInstance && wasmInstance.exports && wasmInstance.exports.memory) 
                                ? wasmInstance.exports.memory 
                                : sharedMemory;

                    if (!self.heapOffset) {
                        if (wasmInstance && wasmInstance.exports && wasmInstance.exports.__heap_base) {
                            self.heapOffset = Number(wasmInstance.exports.__heap_base.value || wasmInstance.exports.__heap_base);
                        } else {
                            self.heapOffset = 65536; // 64KB (1ページ目の直後)
                        }
                    }

                    const ptr = self.heapOffset;
                    const allocSize = (Number(size) + 15) & ~15; // 16バイトアライン
                    self.heapOffset += allocSize;

                    // リニアメモリを超える場合は拡張
                    if (mem && self.heapOffset > mem.buffer.byteLength) {
                        const neededBytes = self.heapOffset - mem.buffer.byteLength;
                        const neededPages = Math.ceil(neededBytes / 65536);
                        try {
                            mem.grow(neededPages + 1);
                            self.postMessage({
                                type: 'log',
                                message: `[Host] WASMリニアメモリを拡張 (+${neededPages + 1} pages)`
                            });
                        } catch (err) {
                            console.error("Failed to grow memory:", err);
                        }
                    }

                    return ptr;
                },

                calloc: (num, size) => {
                    const total = Number(num) * Number(size);
                    const ptr = importObject.env.malloc(total);
                    const mem = (wasmInstance && wasmInstance.exports && wasmInstance.exports.memory) 
                                ? wasmInstance.exports.memory 
                                : sharedMemory;
                    if (mem) {
                        new Uint8Array(mem.buffer, ptr, total).fill(0);
                    }
                    return ptr;
                },

                free: (ptr) => { /* no-op */ },

                // --- 4. POSIX / WASM 抽象スレッド同期 API ---
                hike_thread_spawn: (fnIdx, paramPtr) => {
                    const table = (wasmInstance && wasmInstance.exports) 
                                  ? (wasmInstance.exports.__indirect_function_table || wasmInstance.exports.table) 
                                  : null;
                    if (table) {
                        const thunk = table.get(fnIdx);
                        if (thunk) {
                            thunk(paramPtr);
                            return 0;
                        }
                    }
                    console.error("hike_thread_spawn: table or function index not found", fnIdx);
                    return -1;
                },

                hike_event_create: () => {
                    const id = nextEventId++;
                    eventStates.set(id, { signaled: false });
                    return id;
                },

                hike_event_signal: (eventId) => {
                    const ev = eventStates.get(eventId);
                    if (ev) ev.signaled = true;
                },

                hike_event_wait: (eventId, timeoutMs) => {
                    return 0;
                },

                hike_event_destroy: (eventId) => {
                    eventStates.delete(eventId);
                },

                hike_sleep_ms: (ms) => {
                    if (ms > 0 && sharedMemory) {
                        const i32 = new Int32Array(sharedMemory.buffer, 0, 1);
                        Atomics.wait(i32, 0, Atomics.load(i32, 0), Number(ms));
                    }
                },

                hike_now_ns: () => {
                    return BigInt(Math.floor(performance.now() * 1000000));
                }
            }
        };

        try {
            self.postMessage({ type: 'log', message: 'WASMモジュールをインスタンス化中...' });
            const { instance } = await WebAssembly.instantiate(wasmBytes, importObject);
            wasmInstance = instance;

            self.postMessage({ type: 'log', message: 'Hike main() を実行中...' });
            if (instance.exports.main) {
                instance.exports.main(0, 0);
            }
        } catch (err) {
            self.postMessage({ type: 'log', message: 'ランタイム実行例外: ' + err });
        }
    }
};