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
cfunc EchoString(value cstring) cstring = wgpuEchoString
`
	program := parser.New(lexer.New(source)).ParseProgram()
	if len(program.Decls) != 4 {
		t.Fatalf("parsed %d declarations, want 4", len(program.Decls))
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
		"r1 := _hike_CreateDevice(device, descriptor)",
		"func DevicePoll(device *byte, wait bool, submissionIndex *byte) bool {",
		"r1 := _hike_DevicePoll(device, wait, submissionIndex)",
		"func SetLogLevel(level uint32) {",
		"_hike_SetLogLevel(level)",
		"func EchoString(value string) string {",
		"valueBytes := append([]byte(value), 0)",
		"r1 := _hike_EchoString(&valueBytes[0])",
		"return hikeCStringToString(r1)",
		"func _hike_CreateDevice(device *byte, descriptor *byte) *byte",
		"func _hike_DevicePoll(device *byte, wait bool, submissionIndex *byte) bool",
		"func _hike_SetLogLevel(level uint32)",
		"func _hike_EchoString(value *byte) *byte",
	}
	for _, fragment := range want {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated Go wrapper is missing %q:\n%s", fragment, generated)
		}
	}
	if strings.Contains(generated, "func CreateDevice(filename string) int") {
		t.Fatal("generated wrapper fell back to the obsolete filename/int interface")
	}
	if strings.Contains(generated, "syscall.SyscallN") || strings.Contains(generated, "hikeBoolArg") {
		t.Fatalf("generated wrappers should call typed trampoline declarations directly:\n%s", generated)
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
	if err := GenerateGoStubs(dir, "native", program.Decls, "windows", false); err != nil {
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

func TestGenerateLinuxAMD64StubsIncludesFunctionPointers(t *testing.T) {
	source := `package native

cfunc NativeCall(a uintptr, b uintptr, c uintptr, d uintptr, e uintptr, f uintptr, g uintptr) uintptr = nativeCall
cfunc WGPUStaticRequestAdapterCallback(status int) = wgpuCallback
extern func RequestAdapterCallbackPtr() uintptr
`
	program := parser.New(lexer.New(source)).ParseProgram()
	dir := t.TempDir()
	if err := GenerateGoStubs(dir, "native", program.Decls, "linux", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stub_linux_amd64.s"))
	if err != nil {
		t.Fatal(err)
	}
	generated := string(data)
	for _, fragment := range []string{
		"MOVQ 16(BP), DI",
		"MOVQ 56(BP), R9",
		"MOVQ 64(BP), AX",
		"MOVQ AX, 0(SP)",
		"CALL nativeCall(SB)",
		"TEXT ·RequestAdapterCallbackPtr(SB), NOSPLIT, $0-8",
		"LEAQ wgpuCallback(SB), AX",
		"TEXT ·_hike_NativeCallPtr(SB), NOSPLIT, $0-8",
		"LEAQ nativeCall(SB), AX",
	} {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated Linux stub is missing %q:\n%s", fragment, generated)
		}
	}
}

func TestGenerateLinuxAMD64StubsMatchesDirectCallTrampoline(t *testing.T) {
	source := `package native

cfunc GenerateReport(instance *byte, report *byte) = wgpuGenerateReport
`
	program := parser.New(lexer.New(source)).ParseProgram()
	dir := t.TempDir()
	if err := GenerateGoStubs(dir, "native", program.Decls, "linux", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stub_linux_amd64.s"))
	if err != nil {
		t.Fatal(err)
	}

	want := `// GenerateReport (SysV AMD64)
TEXT ·_hike_GenerateReport(SB), 0, $32-16
    PCDATA $PCDATA_UnsafePoint, $-2
    MOVQ SP, R11
    ANDQ $15, R11
    SUBQ R11, SP
    MOVQ R11, 8(SP)
    MOVQ 16(BP), DI
    MOVQ 24(BP), SI
    CALL wgpuGenerateReport(SB)
    MOVQ 8(SP), R11
    ADDQ R11, SP
    PCDATA $PCDATA_UnsafePoint, $0
    RET`
	if !strings.Contains(string(data), want) {
		t.Fatalf("generated Linux trampoline does not match the direct-call assembly:\n%s", data)
	}
	if strings.Contains(string(data), "SYSCALL") || strings.Contains(string(data), "Syscall") {
		t.Fatalf("generated Linux trampoline must call the native function directly:\n%s", data)
	}
	if !strings.Contains(string(data), "#include \"funcdata.h\"") {
		t.Fatal("generated Linux stubs must import Go runtime PCDATA identifiers")
	}
}

func TestGenerateLinuxAMD64StubsUsesSysVAndGoLayouts(t *testing.T) {
	source := `package native

type CallbackInfo struct {
    Next uintptr
    Mode uintptr
    Callback uintptr
    Userdata1 uintptr
    Userdata2 uintptr
}

type MixedAggregate struct {
    Count uint32
    Ratio float32
}

type InnerAggregate struct {
    Ratio float32
    Count uint32
}

type NestedAggregate struct {
    Prefix uint32
    Inner InnerAggregate
}

type FloatAggregate struct {
    First float64
    Second float64
}

cfunc LargeAggregate(instance *byte, options *byte, info CallbackInfo) uint64 = nativeLargeAggregate
cfunc Mixed(a uint32, b float64, c int8, d uintptr, e uintptr, f uintptr, g uintptr, h float32) float32 = nativeMixed
cfunc SmallAggregate(value MixedAggregate) uint64 = nativeSmallAggregate
cfunc Nested(value NestedAggregate) uint64 = nativeNested
cfunc FloatAggregateCall(value FloatAggregate) = nativeFloatAggregate
cfunc FloatStack(a float64, b float64, c float64, d float64, e float64, f float64, g float64, h float64, i float32) uint32 = nativeFloatStack
`
	program := parser.New(lexer.New(source)).ParseProgram()
	if len(program.Decls) != 11 {
		t.Fatalf("parsed %d declarations, want 11", len(program.Decls))
	}

	dir := t.TempDir()
	if err := GenerateGoStubs(dir, "native", program.Decls, "linux", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stub_linux_amd64.s"))
	if err != nil {
		t.Fatal(err)
	}
	generated := string(data)

	for _, fragment := range []string{
		"TEXT ·_hike_LargeAggregate(SB), 0, $64-64",
		"ANDQ $15, R11",
		"SUBQ R11, SP",
		"MOVQ R11, 40(SP)",
		"MOVQ 32(BP), AX",
		"MOVQ AX, 0(SP)",
		"MOVQ 64(BP), AX",
		"MOVQ AX, 32(SP)",
		"CALL nativeLargeAggregate(SB)",
		"ADDQ R11, SP",
		"MOVQ AX, ret+56(FP)",
		"TEXT ·_hike_Mixed(SB), 0, $32-64",
		"MOVLQZX 16(BP), DI",
		"MOVSD 24(BP), X0",
		"MOVBQSX 32(BP), SI",
		"MOVQ 40(BP), DX",
		"MOVQ 64(BP), R9",
		"MOVSS 72(BP), X1",
		"MOVSS X0, ret+60(FP)",
		"TEXT ·_hike_SmallAggregate(SB), 0, $32-16",
		"MOVLQZX 16(BP), R10",
		"MOVSS 20(BP), X15",
		"SHLQ $32, R10",
		"MOVQ AX, DI",
		"TEXT ·_hike_Nested(SB), 0, $32-24",
		"MOVSS 20(BP), X15",
		"MOVLQZX 24(BP), R10",
		"MOVQ AX, SI",
		"TEXT ·_hike_FloatAggregateCall(SB), 0, $32-16",
		"MOVSD 16(BP), X15",
		"MOVQ AX, X0",
		"MOVSD 24(BP), X15",
		"MOVQ AX, X1",
		"TEXT ·_hike_FloatStack(SB), 0, $32-72",
		"MOVSS 80(BP), X15",
		"MOVSS X15, 0(SP)",
		"MOVL AX, ret+68(FP)",
	} {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated SysV stub is missing %q:\n%s", fragment, generated)
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
	if err := GenerateGoStubs(dir, "native", program.Decls, "windows", false); err != nil {
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
	if !strings.Contains(createStub, "PCDATA $PCDATA_UnsafePoint, $-2") ||
		!strings.Contains(createStub, "ANDQ $15, R11") || !strings.Contains(createStub, "SUBQ R11, SP") ||
		!strings.Contains(createStub, "SUBQ $48, SP") || !strings.Contains(createStub, "ADDQ R11, SP") {
		t.Fatalf("simple native call must dynamically normalize RSP:\n%s", createStub)
	}
	if !strings.Contains(adapterStub, "TEXT ·_hike_InstanceRequestAdapterABI(SB), 0, $32-64") {
		t.Fatalf("Go frame for callback API was changed:\n%s", adapterStub)
	}
	if !strings.Contains(adapterStub, "PCDATA $PCDATA_UnsafePoint, $-2") ||
		!strings.Contains(adapterStub, "ANDQ $15, R11") || !strings.Contains(adapterStub, "SUBQ R11, SP") ||
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
