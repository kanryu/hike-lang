import ctypes
import os
import sys

# OSに応じた共有ライブラリ名の解決
base_dir = os.path.dirname(os.path.abspath(__file__))
if sys.platform == "win32":
    lib_path = os.path.join(base_dir, "libcalc.dll")
else:
    lib_path = os.path.join(base_dir, "libcalc.so")

calc = ctypes.CDLL(lib_path)

# 1. 整数加算 (cfunc)
calc.HikeAddInt.argtypes = [ctypes.c_int64, ctypes.c_int64]
calc.HikeAddInt.restype = ctypes.c_int64
res_int = calc.HikeAddInt(100, 250)
print(f"[Python] HikeAddInt(100, 250) = {res_int}")
assert res_int == 350, f"Expected 350, got {res_int}"

# 2. 浮動小数点加算 (cfunc)
calc.HikeAddFloat.argtypes = [ctypes.c_double, ctypes.c_double]
calc.HikeAddFloat.restype = ctypes.c_double
res_float = calc.HikeAddFloat(3.1415, 2.7182)
print(f"[Python] HikeAddFloat(3.1415, 2.7182) = {res_float:.4f}")
assert abs(res_float - 5.8597) < 1e-4

# 3. 構造体ポインタ渡し (Vector2D 内積)
class Vector2D(ctypes.Structure):
    _fields_ = [("X", ctypes.c_double), ("Y", ctypes.c_double)]

calc.HikeDotProduct.argtypes = [ctypes.POINTER(Vector2D), ctypes.POINTER(Vector2D)]
calc.HikeDotProduct.restype = ctypes.c_double

v1 = Vector2D(3.0, 4.0)
v2 = Vector2D(2.0, 5.0)
dot = calc.HikeDotProduct(ctypes.byref(v1), ctypes.byref(v2))
print(f"[Python] HikeDotProduct((3, 4), (2, 5)) = {dot}")
assert dot == 26.0, f"Expected 26.0, got {dot}"

# 4. Hike内部からのcfunc呼び出し検証 (HikeAddAndDouble -> HikeAddInt * 2)
calc.HikeAddAndDouble.argtypes = [ctypes.c_int64, ctypes.c_int64]
calc.HikeAddAndDouble.restype = ctypes.c_int64
res_double = calc.HikeAddAndDouble(30, 20)
print(f"[Python] HikeAddAndDouble(30, 20) = {res_double}")
assert res_double == 100, f"Expected 100, got {res_double}"

# 5. Hike内部からのC関数(abs)呼び出し検証 (HikeAbsDiff)
calc.HikeAbsDiff.argtypes = [ctypes.c_int64, ctypes.c_int64]
calc.HikeAbsDiff.restype = ctypes.c_int64
res_diff = calc.HikeAbsDiff(15, 45)
print(f"[Python] HikeAbsDiff(15, 45) = {res_diff}")
assert res_diff == 30, f"Expected 30, got {res_diff}"

print("[Python] All shared library tests passed successfully!")