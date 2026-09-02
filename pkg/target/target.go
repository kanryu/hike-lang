package target

import (
	"fmt"
	"runtime"
	"strings"
)

type Target struct {
	Name     string
	Triple   string
	PtrType  string // "i64" or "i32"
	PtrBytes int    // 8 or 4
	IsWasm   bool
	IsWASI   bool
}

var (
	TargetX86_64Windows = &Target{
		Name:     "x86_64-windows",
		Triple:   "x86_64-w64-windows-gnu",
		PtrType:  "i64",
		PtrBytes: 8,
		IsWasm:   false,
		IsWASI:   false,
	}
	TargetX86_64Linux = &Target{
		Name:     "x86_64-linux",
		Triple:   "x86_64-unknown-linux-gnu",
		PtrType:  "i64",
		PtrBytes: 8,
		IsWasm:   false,
		IsWASI:   false,
	}
	TargetX86_64Darwin = &Target{
		Name:     "x86_64-darwin",
		Triple:   "x86_64-apple-darwin",
		PtrType:  "i64",
		PtrBytes: 8,
		IsWasm:   false,
		IsWASI:   false,
	}
	TargetWasm32WASI = &Target{
		Name:     "wasm32-wasi",
		Triple:   "wasm32-unknown-wasi",
		PtrType:  "i32",
		PtrBytes: 4,
		IsWasm:   true,
		IsWASI:   true,
	}
	TargetWasm32Unknown = &Target{
		Name:     "wasm32-unknown",
		Triple:   "wasm32-unknown-unknown",
		PtrType:  "i32",
		PtrBytes: 4,
		IsWasm:   true,
		IsWASI:   false,
	}
	TargetWasm64 = &Target{
		Name:     "wasm64",
		Triple:   "wasm64-unknown-unknown",
		PtrType:  "i64",
		PtrBytes: 8,
		IsWasm:   true,
		IsWASI:   false,
	}
)

func DefaultTarget() *Target {
	switch runtime.GOOS {
	case "windows":
		return TargetX86_64Windows
	case "darwin":
		return TargetX86_64Darwin
	default:
		return TargetX86_64Linux
	}
}

func ParseTarget(name string) (*Target, error) {
	if name == "" {
		return DefaultTarget(), nil
	}
	switch strings.ToLower(name) {
	case "windows", "x86_64-windows", "win64":
		return TargetX86_64Windows, nil
	case "linux", "x86_64-linux":
		return TargetX86_64Linux, nil
	case "darwin", "macos", "x86_64-darwin":
		return TargetX86_64Darwin, nil
	case "wasm", "wasm32", "wasm32-wasi", "wasi", "node":
		return TargetWasm32WASI, nil
	case "wasm32-unknown", "browser":
		return TargetWasm32Unknown, nil
	case "wasm64", "wasm64-unknown":
		return TargetWasm64, nil
	default:
		return nil, fmt.Errorf("unsupported target: %s", name)
	}
}

func (t *Target) Is64Bit() bool {
	return t.PtrBytes == 8
}
