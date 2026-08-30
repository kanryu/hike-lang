#include <iostream>
#include "libcalc.h"

int main() {
    std::cout << "=== Running C++ Host with Hike Generated Header ===" << std::endl;

    int64_t sumInt = HikeAddInt(400, 600);
    std::cout << "HikeAddInt(400, 600) = " << sumInt << std::endl;

    double sumFloat = HikeAddFloat(1.414, 1.732);
    std::cout << "HikeAddFloat(1.414, 1.732) = " << sumFloat << std::endl;

    Vector2D v1 = { 3.0, 4.0 };
    Vector2D v2 = { 2.0, 5.0 };
    double dot = HikeDotProduct(&v1, &v2);
    std::cout << "HikeDotProduct((3, 4), (2, 5)) = " << dot << std::endl;

    return 0;
}