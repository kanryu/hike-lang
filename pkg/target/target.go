package target

import (
	"fmt"
	"runtime"
)

type Target struct {
	Name   string
	Triple string
	IsWasm bool
	Cflags string
}

var (
	TargetX86_64Windows = Target{
		Name:   "windows",
		Triple: "x86_64-w64-windows-gnu",
		IsWasm: false,
		Cflags: "",
	}
	TargetX86_64WindowsMSVC = Target{
		Name:   "windows-msvc",
		Triple: "x86_64-pc-windows-msvc",
		IsWasm: false,
		// UCRT でインライン化された stdio シンボルを解決するライブラリを指定
		Cflags: "-llegacy_stdio_definitions -Wno-override-module",
	}
	TargetX86_64Linux = Target{
		Name:   "linux",
		Triple: "x86_64-unknown-linux-gnu",
		IsWasm: false,
		Cflags: "-pthread -lm",
	}
	TargetAarch64Darwin = Target{
		Name:   "darwin",
		Triple: "arm64-apple-darwin",
		IsWasm: false,
		Cflags: "",
	}
	TargetWasm32 = Target{
		Name:   "wasm32",
		Triple: "wasm32-unknown-unknown",
		IsWasm: true,
		Cflags: "",
	}
	TargetWasm64 = Target{
		Name:   "wasm64",
		Triple: "wasm64-unknown-unknown",
		IsWasm: true,
		Cflags: "",
	}
	// TargetWabt emits WebAssembly text (WAT). The WAT is assembled by the
	// system wat2wasm command during build/run.
	TargetWabt = Target{
		Name:   "wabt",
		Triple: "wasm32-unknown-unknown",
		IsWasm: true,
		Cflags: "",
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
	case "windows-msvc", "x86_64-windows-msvc", "x86_64-pc-windows-msvc":
		return &TargetX86_64WindowsMSVC, nil
	case "linux", "x86_64-linux", "x86_64-linux-gnu", "x86_64-unknown-linux-gnu":
		return &TargetX86_64Linux, nil
	case "darwin", "macos", "arm64-darwin", "aarch64-apple-darwin":
		return &TargetAarch64Darwin, nil
	case "wasm", "wasm32", "wasm32-unknown", "wasm32-unknown-unknown":
		return &TargetWasm32, nil
	case "wasm64", "wasm64-unknown", "wasm64-unknown-unknown":
		return &TargetWasm64, nil
	case "wabt", "wat", "wasm-text":
		return &TargetWabt, nil
	default:
		return nil, fmt.Errorf("unknown target: %s", name)
	}
}
