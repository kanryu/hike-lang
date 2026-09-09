HIKEC = go run ../../cmd/hikec
CLANG = clang
CXX = clang++
PYTHON = python

SRC = libcalc.hike
LL = libcalc.ll
HEADER = libcalc.h
TARGET = libcalc.dll
IMPLIB = libcalc.lib
CTARGET = c_client.exe
CPPTARGET = cpp_client.exe

CFLAGS = -O3
RM = rm -f

.PHONY: all clean test testc testcpp testall

all: $(TARGET) $(HEADER)

$(LL) $(HEADER): $(SRC)
	$(HIKEC) $(SRC) -o $(LL) -header $(HEADER)

$(TARGET) $(IMPLIB): $(LL)
	$(CLANG) -shared -O3 -Wl,-export-all-symbols -Wl,-implib:$(IMPLIB) $(LL) -o $(TARGET)

test: $(TARGET)
	$(PYTHON) test_client.py

$(CTARGET): test_c.c $(TARGET) $(IMPLIB) $(HEADER)
	$(CLANG) $(CFLAGS) test_c.c $(IMPLIB) -o $(CTARGET)

testc: $(CTARGET)
	./$(CTARGET)

$(CPPTARGET): test_client.cpp $(TARGET) $(IMPLIB) $(HEADER)
	$(CXX) test_client.cpp $(IMPLIB) -o $(CPPTARGET)

testcpp: $(CPPTARGET)
	./$(CPPTARGET)

testall: test testc testcpp

clean:
	$(RM) *.ll *.dll *.lib *.exp *.a *.so libcalc.h $(CTARGET) $(CPPTARGET)