HIKEC = go run ../../../cmd/hikec
CLANG = clang
CUDA = C:/Program Files/NVIDIA GPU Computing Toolkit/CUDA/v11.7

.PHONY: all emit-ir clean

all: room.exe

emit-ir: main.ll

main.ll: main.hike
	$(HIKEC) emit-ir -o $@ $<

room.exe: main.ll room_opencl.c
	$(CLANG) -O2 main.ll room_opencl.c -I"$(CUDA)/include" -L"$(CUDA)/lib/x64" -lOpenCL -o $@

clean:
	rm -f main.ll room.exe room_yz*.bmp nul
