#include <iostream>
#include <cassert>
#include "libcalc.h"

int main() {
    std::cout << "=== Running C++ Client with libcalc.h ===" << std::endl;

    int64_t sumInt = HikeAddInt(400, 600);
    std::cout << "[C++] HikeAddInt(400, 600) = " << sumInt << std::endl;
    assert(sumInt == 1000);

    double sumFloat = HikeAddFloat(1.414, 1.732);
    std::cout << "[C++] HikeAddFloat(1.414, 1.732) = " << sumFloat << std::endl;

    Vector2D v1 = { 3.0, 4.0 };
    Vector2D v2 = { 2.0, 5.0 };
    double dot = HikeDotProduct(&v1, &v2);
    std::cout << "[C++] HikeDotProduct((3, 4), (2, 5)) = " << dot << std::endl;
    assert(dot == 26.0);

    int64_t doubled = HikeAddAndDouble(15, 35);
    std::cout << "[C++] HikeAddAndDouble(15, 35) = " << doubled << std::endl;
    assert(doubled == 100);

    int64_t diff = HikeAbsDiff(100, 30);
    std::cout << "[C++] HikeAbsDiff(100, 30) = " << diff << std::endl;
    assert(diff == 70);

    std::cout << "[C++] All C++ tests passed successfully!" << std::endl;
    return 0;
}