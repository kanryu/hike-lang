HIKEC = go run ../../cmd/hikec
TARGET = windows
OUTPUT = resample.exe

.PHONY: all emit-ir build run clean

all: build

emit-ir:
	$(HIKEC) emit-ir -target $(TARGET) -o main.ll main.hike

.hike:
	$(HIKEC) get

build: .hike $(OUTPUT)

$(OUTPUT): main.hike hike.mod .hike/deps/github.com/kanryu/hike-images-vips/hike.mod
	$(HIKEC) build -target $(TARGET) main.hike -o $(OUTPUT)

run: build source_720p.svg
	./$(OUTPUT)

clean:
	rm -f main.ll $(OUTPUT) output_1080p.png output_thumbnail_64.png output_rotate_flip.png *.dll
