typedef unsigned long long size_t;

// 外部CRTに依存しない自前の簡易アロケータ
static char g_heap[4096];
static int g_heap_offset = 0;

static void* local_malloc(size_t size) {
    if (g_heap_offset + (int)size > (int)sizeof(g_heap)) {
        return (void*)0;
    }
    void* p = (void*)&g_heap[g_heap_offset];
    g_heap_offset += ((int)size + 7) & ~7; // 8バイト整列
    return p;
}

static unsigned int g_seed = 123456789;
static int simple_rand(void) {
    g_seed = (1103515245 * g_seed + 12345) & 0x7fffffff;
    return (int)g_seed;
}

// 1. GetFizzFileSize
int c_GetFizzFileSize(const char* ptr, long long len) {
    return (simple_rand() % 20) + 1;
}

// 2. ReadFizzFile
int c_ReadFizzFile(const char* fn_ptr, long long fn_len, long long readSize, char* buf) {
    if (readSize <= 0 || buf == (void*)0) {
        return 0;
    }

    for (long long i = 0; i < readSize; i++) {
        buf[i] = (char)(65 + (i % 26));
    }

    return (int)readSize;
}

// 3. GetMetaData
char* c_GetMetaData(const char* fn_ptr, long long fn_len, long long* outLen) {
    int isBuzz = (simple_rand() % 2) == 0;
    if (isBuzz) {
        const char* str = "fizzbuzz";
        *outLen = 8;
        char* p = (char*)local_malloc(9);
        for (int i = 0; i < 9; i++) p[i] = str[i];
        return p;
    } else {
        const char* str = "fizz";
        *outLen = 4;
        char* p = (char*)local_malloc(5);
        for (int i = 0; i < 5; i++) p[i] = str[i];
        return p;
    }
}

// 4. 解放用スタブ
void c_free(void* ptr) {
    // PoC用ダミー解放
    (void)ptr;
}