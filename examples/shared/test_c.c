#include <stdio.h>
#include "libcalc.h"

int main() {
    printf("[Step 1] Main started successfully.\n");
    fflush(stdout);

    printf("[Step 2] Calling HikeAddInt...\n");
    fflush(stdout);
    int64_t a = HikeAddInt(100, 200);
    printf("[Step 3] HikeAddInt result = %lld\n", (long long)a);
    fflush(stdout);

    return 0;
}