.PHONY: all rust generate archive package run clean

GO := go
HIKEC := ..\..\bin\hikec.exe
HIKEPACK := ..\..\bin\hikepack.exe
RUSTLIB := rustlib\target\x86_64-pc-windows-gnullvm\release\libfizzrust.a

all: run

rust:
	powershell -Command "Push-Location rustlib; $$rustToolchain = 'C:\\Users\\boset\\.rustup\\toolchains\\stable-x86_64-pc-windows-msvc\\bin'; $$env:Path = \"$$rustToolchain;$$env:Path\"; Remove-Item Env:RUSTC -ErrorAction SilentlyContinue; $$env:RUSTFLAGS = '-C embed-bitcode=no'; rustup run stable cargo build --offline --release --target x86_64-pc-windows-gnullvm; Pop-Location"

generate:
	$(HIKEC) go -v .\fizzlib

archive: rust

package: generate archive
	$(HIKEPACK) syso -v -target windows .

run: package
	$(GO) run -ldflags "-linkmode=external -extldflags=-lkernel32" .

clean:
	powershell -Command "Remove-Item -LiteralPath '.\fizzlib\fizzlib.go','.\fizzlib\stub_windows_amd64.s','.\fizzlib\fizzlib.ll','.\fizzlib\hikepack_archive_windows.go','.\fizzlib_native' -Recurse -Force -ErrorAction SilentlyContinue; Remove-Item -LiteralPath '.\rustlib\target' -Recurse -Force -ErrorAction SilentlyContinue"
