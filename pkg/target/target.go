package target

import (
	"fmt"
	"runtime"
	"strings"
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
		Cflags: "",
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
)

func DefaultTarget() *Target {
	switch runtime.GOOS {
	case "windows":
		t := TargetX86_64Windows
		return &t
	case "darwin":
		t := TargetAarch64Darwin
		return &t
	default:
		t := TargetX86_64Linux
		return &t
	}
}

func ParseTarget(name string) (*Target, error) {
	if name == "" {
		return DefaultTarget(), nil
	}
	switch strings.ToLower(name) {
	case "windows", "x86_64-windows", "x86_64-windows-gnu", "x86_64-w64-windows-gnu":
		t := TargetX86_64Windows
		return &t, nil
	case "windows-msvc", "x86_64-windows-msvc", "x86_64-pc-windows-msvc":
		t := TargetX86_64WindowsMSVC
		return &t, nil
	case "linux", "x86_64-linux", "x86_64-linux-gnu", "x86_64-unknown-linux-gnu":
		t := TargetX86_64Linux
		return &t, nil
	case "darwin", "macos", "arm64-darwin", "aarch64-apple-darwin":
		t := TargetAarch64Darwin
		return &t, nil
	case "wasm", "wasm32", "wasm32-unknown", "wasm32-unknown-unknown":
		t := TargetWasm32
		return &t, nil
	case "wasm64", "wasm64-unknown", "wasm64-unknown-unknown":
		t := TargetWasm64
		return &t, nil
	default:
		return nil, fmt.Errorf("unknown target: %s", name)
	}
}
