// 1. GetFizzFileSize
int c_GetFizzFileSize(const char* ptr, long long len) {
    (void)ptr;
    return (int)len + 100;
}

// 1b. C stringのNUL終端までを走査する。
int c_StringLength(const char* value) {
    int length = 0;
    while (value != (void*)0 && value[length] != '\0') {
        length++;
    }
    return length;
}

// 1c. 受け取ったcharポインターをそのまま返す。
const char* c_EchoString(const char* value) {
    return value;
}

// 1d. CRTのprintfを使わず、Windowsの標準出力ハンドルへ直接書き出す。
typedef void* HANDLE;
typedef unsigned long DWORD;
typedef int BOOL;

__declspec(dllimport) HANDLE __stdcall GetStdHandle(DWORD standardHandle);
__declspec(dllimport) BOOL __stdcall WriteFile(
    HANDLE handle,
    const void* buffer,
    DWORD bytesToWrite,
    DWORD* bytesWritten,
    void* overlapped);

int c_PrintString(const char* value) {
    static const char newline[] = "\r\n";
    int length = 0;
    DWORD written = 0;
    HANDLE stdoutHandle = GetStdHandle((DWORD)-11); // STD_OUTPUT_HANDLE

    while (value != (void*)0 && value[length] != '\0') {
        length++;
    }
    if (stdoutHandle == (void*)0 || stdoutHandle == (void*)-1) {
        return -1;
    }
    if (length > 0 && !WriteFile(stdoutHandle, value, (DWORD)length, &written, (void*)0)) {
        return -1;
    }
    if (!WriteFile(stdoutHandle, newline, 2, &written, (void*)0)) {
        return -1;
    }
    return length;
}

// 2. ReadFizzFile
int c_ReadFizzFile(const char* fn_ptr, long long readSize, char* buf) {
    (void)fn_ptr;
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
    (void)fn_ptr;
    (void)fn_len;
    static char value[] = "fizzbuzz";
    *outLen = 8;
    return value;
}
