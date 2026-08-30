HIKEC = go run ../../cmd/hikec
CLANG = clang
CXX = clang++
PYTHON = python

SRC = libcalc.hike
LL = libcalc.ll
HEADER = libcalc.h
TARGET = libcalc.dll
IMPLIB = libcalc.dll.a
CPPTARGET = cpp_client.exe

CXXFLAGS = -static-libstdc++ -static-libgcc
RM = rm -f

.PHONY: all clean test testcpp

all: $(TARGET) $(HEADER)

$(LL) $(HEADER): $(SRC)
	$(HIKEC) $(SRC) -o $(LL) -header $(HEADER)

$(TARGET) $(IMPLIB): $(LL)
	$(CLANG) -shared -O3 -Wl,--export-all-symbols -Wl,--out-implib,$(IMPLIB) $(LL) -o $(TARGET)

test: $(TARGET)
	$(PYTHON) test_client.py

cpp_client.exe: test_client.cpp $(TARGET) $(IMPLIB) $(HEADER)
	$(CXX) $(CXXFLAGS) test_client.cpp $(IMPLIB) -o cpp_client.exe

testcpp: cpp_client.exe
	./cpp_client.exe

clean:
	$(RM) *.ll *.dll *.dll.a *.so libcalc.h $(CPPTARGET)
