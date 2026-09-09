#include <stdio.h>
#include <stdint.h>
#include <assert.h>
#include "libcalc.h"

int main() {
    printf("=== Running C Client for Hike Shared Library ===\n");

    int64_t a = HikeAddInt(100, 200);
    printf("[C] HikeAddInt(100, 200) = %lld\n", (long long)a);
    assert(a == 300);

    double f = HikeAddFloat(1.5, 2.5);
    printf("[C] HikeAddFloat(1.5, 2.5) = %f\n", f);
    assert(f == 4.0);

    int64_t doubled = HikeAddAndDouble(25, 25);
    printf("[C] HikeAddAndDouble(25, 25) = %lld\n", (long long)doubled);
    assert(doubled == 100);

    int64_t diff = HikeAbsDiff(10, 50);
    printf("[C] HikeAbsDiff(10, 50) = %lld\n", (long long)diff);
    assert(diff == 40);

    Vector2D v1 = { 3.0, 4.0 };
    Vector2D v2 = { 1.0, 2.0 };
    double dot = HikeDotProduct(&v1, &v2);
    printf("[C] HikeDotProduct = %f\n", dot);
    assert(dot == 11.0);

    printf("[C] All C tests passed successfully!\n");
    return 0;
}