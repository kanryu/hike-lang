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

# 1. 整数演算
calc.HikeAddInt.argtypes = [ctypes.c_int64, ctypes.c_int64]
calc.HikeAddInt.restype = ctypes.c_int64
res_int = calc.HikeAddInt(100, 250)
print(f"[Python -> Hike SharedLib] HikeAddInt(100, 250) = {res_int}")

# 2. 浮動小数点演算
calc.HikeAddFloat.argtypes = [ctypes.c_double, ctypes.c_double]
calc.HikeAddFloat.restype = ctypes.c_double
res_float = calc.HikeAddFloat(3.1415, 2.7182)
print(f"[Python -> Hike SharedLib] HikeAddFloat(3.1415, 2.7182) = {res_float}")

# 3. 構造体ポインタ渡し (Vector2D 内積計算)
class Vector2D(ctypes.Structure):
    _fields_ = [("X", ctypes.c_double), ("Y", ctypes.c_double)]

calc.HikeDotProduct.argtypes = [ctypes.POINTER(Vector2D), ctypes.POINTER(Vector2D)]
calc.HikeDotProduct.restype = ctypes.c_double

v1 = Vector2D(3.0, 4.0)
v2 = Vector2D(2.0, 5.0)
dot = calc.HikeDotProduct(ctypes.byref(v1), ctypes.byref(v2))
print(f"[Python -> Hike SharedLib] HikeDotProduct((3, 4), (2, 5)) = {dot}")