package target

import (
	"fmt"
	"runtime"
)

type Target struct {
	Name        string
	Triple      string
	PointerBits int
	IsWasm      bool
	Cflags      string
}

var (
	TargetX86_64Windows = Target{
		Name:        "windows",
		Triple:      "x86_64-w64-windows-gnu",
		PointerBits: 64,
		IsWasm:      false,
		Cflags:      "",
	}
	TargetX86Windows = Target{
		Name:        "windows-x86",
		Triple:      "i686-w64-windows-gnu",
		PointerBits: 32,
		IsWasm:      false,
		Cflags:      "",
	}
	TargetX86_64WindowsMSVC = Target{
		Name:        "windows-msvc",
		Triple:      "x86_64-pc-windows-msvc",
		PointerBits: 64,
		IsWasm:      false,
		// UCRT でインライン化された stdio シンボルを解決するライブラリを指定
		Cflags: "-llegacy_stdio_definitions -Wno-override-module",
	}
	TargetX86_64Linux = Target{
		Name:        "linux",
		Triple:      "x86_64-unknown-linux-gnu",
		PointerBits: 64,
		IsWasm:      false,
		Cflags:      "-pthread -lm",
	}
	TargetX86Linux = Target{
		Name:        "linux-x86",
		Triple:      "i686-unknown-linux-gnu",
		PointerBits: 32,
		IsWasm:      false,
		Cflags:      "-pthread -lm",
	}
	TargetARM64Linux = Target{
		Name:        "linux-arm64",
		Triple:      "aarch64-unknown-linux-gnu",
		PointerBits: 64,
		IsWasm:      false,
		Cflags:      "-pthread -lm",
	}
	TargetARM32Linux = Target{
		Name:        "linux-arm32",
		Triple:      "armv7-unknown-linux-gnueabihf",
		PointerBits: 32,
		IsWasm:      false,
		Cflags:      "-pthread -lm",
	}
	TargetAarch64Darwin = Target{
		Name:        "darwin",
		Triple:      "arm64-apple-darwin",
		PointerBits: 64,
		IsWasm:      false,
		Cflags:      "",
	}
	TargetWasm32 = Target{
		Name:        "wasm32",
		Triple:      "wasm32-unknown-unknown",
		PointerBits: 32,
		IsWasm:      true,
		Cflags:      "",
	}
	TargetWasm64 = Target{
		Name:        "wasm64",
		Triple:      "wasm64-unknown-unknown",
		PointerBits: 64,
		IsWasm:      true,
		Cflags:      "",
	}
	// TargetWabt emits WebAssembly text (WAT). The WAT is assembled by the
	// system wat2wasm command during build/run.
	TargetWabt = Target{
		Name:        "wabt",
		Triple:      "wasm32-unknown-unknown",
		PointerBits: 32,
		IsWasm:      true,
		Cflags:      "",
	}
	// TargetCortexM0 targets bare-metal ARMv6-M (e.g. RP2040), which has no
	// OS/libc. Callers are expected to use emit-ir and link the resulting
	// object with an external toolchain (e.g. pico-sdk's CMake build), not
	// hikec's own native "build" link step.
	TargetCortexM0 = Target{
		Name:        "cortex-m0",
		Triple:      "thumbv6m-none-eabi",
		PointerBits: 32,
		IsWasm:      false,
		Cflags:      "-mcpu=cortex-m0plus -mthumb",
	}
)

func DefaultTarget() *Target {
	switch runtime.GOOS {
	case "", "windows":
		return &TargetX86_64Windows
	case "darwin":
		return &TargetAarch64Darwin
	default:
		return &TargetX86_64Linux
	}
}

func ParseTarget(name string) (*Target, error) {
	if name == "" {
		return DefaultTarget(), nil
	}
	// CLI target names are normalized by the caller; keeping this lookup
	// allocation-free is important during self-host bootstrap.
	switch name {
	case "windows", "x86_64-windows", "x86_64-windows-gnu", "x86_64-w64-windows-gnu":
		return &TargetX86_64Windows, nil
	case "windows-x86", "windows-386", "x86-windows", "386-windows", "i686-windows", "i686-windows-gnu", "i686-w64-windows-gnu":
		return &TargetX86Windows, nil
	case "windows-msvc", "x86_64-windows-msvc", "x86_64-pc-windows-msvc":
		return &TargetX86_64WindowsMSVC, nil
	case "linux", "x86_64-linux", "x86_64-linux-gnu", "x86_64-unknown-linux-gnu":
		return &TargetX86_64Linux, nil
	case "linux-x86", "linux-386", "x86-linux", "386-linux", "i386-linux", "i686-linux", "i686-linux-gnu", "i386-unknown-linux-gnu", "i686-unknown-linux-gnu":
		return &TargetX86Linux, nil
	case "linux-arm64", "arm64-linux", "aarch64-linux", "aarch64-linux-gnu", "aarch64-unknown-linux-gnu":
		return &TargetARM64Linux, nil
	case "linux-arm32", "arm-linux", "arm32-linux", "armv7-linux", "armv7-linux-gnueabihf", "armv7-unknown-linux-gnueabihf":
		return &TargetARM32Linux, nil
	case "darwin", "macos", "arm64-darwin", "aarch64-apple-darwin":
		return &TargetAarch64Darwin, nil
	case "wasm", "wasm32", "wasm32-unknown", "wasm32-unknown-unknown":
		return &TargetWasm32, nil
	case "wasm64", "wasm64-unknown", "wasm64-unknown-unknown":
		return &TargetWasm64, nil
	case "wabt", "wat", "wasm-text":
		return &TargetWabt, nil
	case "cortex-m0", "rp2040", "thumbv6m", "thumbv6m-none-eabi":
		return &TargetCortexM0, nil
	default:
		return nil, fmt.Errorf("unknown target: %s", name)
	}
}
