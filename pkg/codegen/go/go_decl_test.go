package gocode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

func TestGenerateGoDeclarationsPreservesCFuncInterface(t *testing.T) {
	source := `package native

cfunc CreateDevice(device *byte, descriptor *byte) *byte = wgpuCreateDevice
cfunc DevicePoll(device *byte, wait bool, submissionIndex *byte) bool = wgpuDevicePoll
cfunc SetLogLevel(level uint32) = wgpuSetLogLevel
`
	program := parser.New(lexer.New(source)).ParseProgram()
	if len(program.Decls) != 3 {
		t.Fatalf("parsed %d declarations, want 3", len(program.Decls))
	}

	path := filepath.Join(t.TempDir(), "native.go")
	if err := GenerateGoDeclarations(path, "native", program.Decls, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(data)

	want := []string{
		"func CreateDevice(device *byte, descriptor *byte) *byte {",
		"r1, _, _ := syscall.SyscallN(_hike_CreateDevicePtr(), uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(descriptor)))",
		"func DevicePoll(device *byte, wait bool, submissionIndex *byte) bool {",
		"r1, _, _ := syscall.SyscallN(_hike_DevicePollPtr(), uintptr(unsafe.Pointer(device)), hikeBoolArg(wait), uintptr(unsafe.Pointer(submissionIndex)))",
		"func SetLogLevel(level uint32) {",
		"_, _, _ = syscall.SyscallN(_hike_SetLogLevelPtr(), uintptr(level))",
		"func _hike_CreateDevice(device *byte, descriptor *byte) *byte",
		"func _hike_DevicePoll(device *byte, wait bool, submissionIndex *byte) bool",
		"func _hike_SetLogLevel(level uint32)",
	}
	for _, fragment := range want {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated Go wrapper is missing %q:\n%s", fragment, generated)
		}
	}
	if strings.Contains(generated, "func CreateDevice(filename string) int") {
		t.Fatal("generated wrapper fell back to the obsolete filename/int interface")
	}
}

func TestGenerateGoDeclarationsEmitsWireStructTypes(t *testing.T) {
	source := `package native

type CallbackInfoWire struct {
    NextInChain uintptr
    Mode uintptr
    Callback uintptr
    Userdata1 uintptr
    Userdata2 uintptr
}

`
	program := parser.New(lexer.New(source)).ParseProgram()
	path := filepath.Join(t.TempDir(), "native.go")
	if err := GenerateGoDeclarations(path, "native", program.Decls, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(data)
	for _, fragment := range []string{
		"type CallbackInfoWire struct {",
		"NextInChain uintptr",
		"Mode uintptr",
		"Callback uintptr",
		"Userdata1 uintptr",
		"Userdata2 uintptr",
	} {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated Go declarations are missing %q:\n%s", fragment, generated)
		}
	}
}

func TestGenerateCallbackPointerExtern(t *testing.T) {
	source := `package native

cfunc WGPUStaticRequestAdapterCallback(status int) = wgpuCallback
extern func RequestAdapterCallbackPtr() uintptr
`
	program := parser.New(lexer.New(source)).ParseProgram()
	dir := t.TempDir()
	goPath := filepath.Join(dir, "native.go")
	if err := GenerateGoDeclarations(goPath, "native", program.Decls, false); err != nil {
		t.Fatal(err)
	}
	if err := GenerateGoStubs(dir, "native", program.Decls, false); err != nil {
		t.Fatal(err)
	}
	goData, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	asmData, err := os.ReadFile(filepath.Join(dir, "stub_windows_amd64.s"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goData), "func RequestAdapterCallbackPtr() uintptr") {
		t.Fatalf("callback pointer declaration was not generated:\n%s", goData)
	}
	asm := string(asmData)
	for _, fragment := range []string{
		"TEXT ·RequestAdapterCallbackPtr(SB), NOSPLIT, $0-8",
		"LEAQ WGPUStaticRequestAdapterCallback(SB), AX",
		"MOVQ AX, ret+0(FP)",
	} {
		if !strings.Contains(asm, fragment) {
			t.Errorf("callback pointer trampoline is missing %q:\n%s", fragment, asm)
		}
	}
}

func TestGenerateGoStubsSeparatesGoFrameAndNativeCallArea(t *testing.T) {
	source := `package native

cfunc CreateInstance(descriptor *byte) *byte = wgpuCreateInstance
cfunc InstanceRequestAdapterABI(instance *byte, options *byte, nextInChain uintptr, mode uintptr, callback uintptr, userdata1 uintptr, userdata2 uintptr) uint64 = wgpuInstanceRequestAdapter
cfunc AdapterRequestDeviceABI(adapter *byte, descriptor *byte, nextInChain uintptr, mode uintptr, callback uintptr, userdata1 uintptr, userdata2 uintptr) uint64 = wgpuAdapterRequestDevice
cfunc DevicePopErrorScopeABI(device *byte, nextInChain uintptr, mode uintptr, callback uintptr, userdata1 uintptr, userdata2 uintptr) = wgpuDevicePopErrorScope
cfunc BufferMapAsyncABI(buffer *byte, mode uintptr, offset uintptr, size uintptr, nextInChain uintptr, callbackMode uintptr, callback uintptr, userdata1 uintptr, userdata2 uintptr) = wgpuBufferMapAsync
cfunc SurfaceGetCapabilities(surface *byte, adapter *byte, capabilities *byte) uint32 = wgpuSurfaceGetCapabilities
cfunc SurfaceConfigure(surface *byte, config *byte) = wgpuSurfaceConfigure
`
	program := parser.New(lexer.New(source)).ParseProgram()
	if len(program.Decls) != 7 {
		t.Fatalf("parsed %d declarations, want 7", len(program.Decls))
	}

	dir := t.TempDir()
	if err := GenerateGoStubs(dir, "native", program.Decls, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stub_windows_amd64.s"))
	if err != nil {
		t.Fatal(err)
	}
	generated := string(data)

	createStart := strings.Index(generated, "TEXT ·_hike_CreateInstance")
	adapterStart := strings.Index(generated, "TEXT ·_hike_InstanceRequestAdapterABI")
	if createStart < 0 || adapterStart < 0 || adapterStart <= createStart {
		t.Fatalf("generated stub is missing expected functions:\n%s", generated)
	}
	createStub := generated[createStart:adapterStart]
	adapterStub := generated[adapterStart:]
	if !strings.Contains(createStub, "TEXT ·_hike_CreateInstance(SB), 0, $32-16") {
		t.Fatalf("Go frame for CreateInstance was changed:\n%s", createStub)
	}
	if !strings.Contains(createStub, "ANDQ $15, R11") || !strings.Contains(createStub, "SUBQ R11, SP") ||
		!strings.Contains(createStub, "SUBQ $48, SP") || !strings.Contains(createStub, "ADDQ R11, SP") {
		t.Fatalf("simple native call must dynamically normalize RSP:\n%s", createStub)
	}
	if !strings.Contains(adapterStub, "TEXT ·_hike_InstanceRequestAdapterABI(SB), 0, $32-64") {
		t.Fatalf("Go frame for callback API was changed:\n%s", adapterStub)
	}
	if !strings.Contains(adapterStub, "ANDQ $15, R11") || !strings.Contains(adapterStub, "SUBQ R11, SP") ||
		!strings.Contains(adapterStub, "SUBQ $128, SP") || !strings.Contains(adapterStub, "ADDQ R11, SP") {
		t.Fatalf("callback native call must dynamically normalize RSP:\n%s", adapterStub)
	}

	wantCallbackABI := []string{
		"MOVQ nextInChain+16(FP), AX",
		"MOVQ AX, 40(SP)",
		"MOVQ userdata2+48(FP), AX",
		"MOVQ AX, 72(SP)",
		"LEAQ 40(SP), R10",
	}
	for _, fragment := range wantCallbackABI {
		if !strings.Contains(generated, fragment) {
			t.Errorf("callback-info trampoline is missing %q:\n%s", fragment, generated)
		}
	}
	if !strings.Contains(adapterStub, "MOVQ nextInChain+16(FP), AX") ||
		!strings.Contains(adapterStub, "MOVQ AX, 40(SP)") ||
		!strings.Contains(adapterStub, "MOVQ R10, R8") {
		t.Fatalf("scalar callback arguments must be rebuilt as a callback record:\n%s", adapterStub)
	}
	bufferStart := strings.Index(generated, "TEXT ·_hike_BufferMapAsyncABI")
	if bufferStart < 0 || !strings.Contains(generated[bufferStart:], "MOVQ R10, 32(SP)") {
		t.Fatalf("BufferMapAsync must place the expanded callback-info pointer in the fifth native argument slot:\n%s", generated)
	}
	capStart := strings.Index(generated, "TEXT ·_hike_SurfaceGetCapabilities")
	configureStart := strings.Index(generated, "TEXT ·_hike_SurfaceConfigure")
	if capStart < 0 || configureStart < 0 {
		t.Fatalf("generated surface trampolines are missing:\n%s", generated)
	}
	capStub := generated[capStart:configureStart]
	configureStub := generated[configureStart:]
	if !strings.Contains(capStub, "SUBQ $48, SP") || !strings.Contains(capStub, "ADDQ R11, SP") {
		t.Fatalf("ordinary typed calls must use the dynamic call area:\n%s", capStub)
	}
	if !strings.Contains(configureStub, "SUBQ $48, SP") || !strings.Contains(configureStub, "ADDQ R11, SP") {
		t.Fatalf("ordinary typed calls must use the dynamic call area:\n%s", configureStub)
	}
}
