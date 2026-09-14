package e2e_test

import "testing"

// std/math の定数、基本関数、指数対数、三角関数、特殊関数を検証する。
func TestE2EStd_Math(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/math"

func printf(format string, ...) int

func main() int {
    s, c := math.SinCos(math.Pi / 2.0)
    printf("CONST=%.6f,%.6f,%.6f BASIC=%.1f,%.1f,%.1f,%.1f ",
        math.Pi, math.E, math.Sqrt2, math.Abs(-3.5), math.Sqrt(9.0),
        math.Pow(2.0, 3.0), math.Hypot(3.0, 4.0))
    printf("TRIG=%.1f,%.1f,%.1f,%.1f,%.1f,%.1f ",
        math.Sin(math.Pi / 2.0), math.Cos(0.0), math.Tan(0.0),
        math.Asin(1.0), math.Acos(1.0), math.Atan(0.0))
    printf("EXPLOG=%.1f,%.1f,%.6f,%.6f,%.1f,%.1f ",
        math.Exp2(3.0), math.Pow10(2), math.Expm1(1.0),
        math.Log1p(1.0), math.Log2(8.0), math.Logb(8.0))
    printf("HYPER=%.1f,%.1f,%.1f,SPECIAL=%.1f,%.1f ",
        math.Sinh(0.0), math.Cosh(0.0), math.Tanh(0.0),
        math.Acosh(1.0), math.Asinh(0.0))
    printf("ROUND=%.1f,%.1f,%.1f,%.1f,%.1f,%.1f ",
        math.Floor(2.9), math.Ceil(2.1), math.Round(2.5),
        math.Trunc(-2.9), math.Mod(7.0, 3.0), math.Dim(5.0, 3.0))
    printf("MISC=%.1f,%.1f,%d,%d\n",
        math.Min(2.0, 3.0), math.Max(2.0, 3.0),
        s > 0.99 && s < 1.01 && c > -0.01 && c < 0.01,
        math.Signbit(-1.0))
    return 0
}
`,
		ExpectedOut: "CONST=3.141593,2.718282,1.414214 BASIC=3.5,3.0,8.0,5.0 TRIG=1.0,1.0,0.0,1.6,0.0,0.0 EXPLOG=8.0,100.0,1.718282,0.693147,3.0,3.0 HYPER=0.0,1.0,0.0,SPECIAL=0.0,0.0 ROUND=2.0,3.0,3.0,-2.0,1.0,2.0 MISC=2.0,3.0,1,1",
		ExpectedExit: 0,
	})
}
