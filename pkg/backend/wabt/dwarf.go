package wabt

// This file contains the small DWARF producer used by the WABT backend.  The
// WAT format can preserve names, but it cannot describe source locations.  We
// therefore keep the source-level function table while emitting WAT and add
// the standard WebAssembly DWARF custom sections after wat2wasm has assembled
// the module.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hikec-go/pkg/hir"
)

type sourceMapFile struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
	SourcesContent []string `json:"sourcesContent,omitempty"`
}

type DebugFunction struct {
	Name   string
	Line   uint32
	Locals []DebugLocal
	Lines  []uint32
}

type DebugLocal struct {
	Name  string
	Line  uint32
	Index uint32
	Size  uint8
	Deref bool
}

type DebugInfo struct {
	SourcePath string
	// SourceURL is used in DWARF file entries when the source is served by a
	// browser. It prevents DevTools from synthesizing a wasm:// URL.
	SourceURL        string
	RuntimeFunctions int
	Functions        []DebugFunction
}

func debugSourceName(info *DebugInfo) string {
	if info != nil && info.SourceURL != "" {
		return info.SourceURL
	}
	return filepath.Base(info.SourcePath)
}

type codeRange struct {
	low     uint32
	high    uint32
	markers []uint32
}

func (d *DebugInfo) Empty() bool { return d == nil || len(d.Functions) == 0 }

func (e *Emitter) DebugInfo(sourcePath string) *DebugInfo {
	info := &DebugInfo{SourcePath: sourcePath, RuntimeFunctions: e.runtimeFunctions}
	if e == nil || e.p == nil {
		return info
	}
	for _, fn := range e.p.Functions {
		if fn == nil || fn.IsExtern {
			continue
		}
		line := uint32(fn.Location.Line)
		if line == 0 {
			line = 1
		}
		debugFn := DebugFunction{Name: fn.Name, Line: line}
		seenNames := make(map[string]bool)
		resultLocals := make(map[*hir.Reg]uint32)
		resultLines := make(map[*hir.Reg]uint32)
		returnRegs := make(map[*hir.Reg]bool)
		returnLine := uint32(0)
		for _, bb := range e.blocksForEmission(fn) {
			if ret, ok := bb.Terminator.(*hir.InstrReturn); ok && len(ret.Vals) == 1 {
				if returned, ok := ret.Vals[0].(*hir.Reg); ok {
					returnRegs[returned] = true
				}
			}
		}
		nextLocal := uint32(len(fn.Params))
		// Function parameters remain live Wasm locals. Describe them as
		// values, rather than as addresses that need to be dereferenced.
		for index, param := range fn.Params {
			if param == nil {
				continue
			}
			name := debugLocalName(reg(param))
			if name == "" || seenNames[name] {
				continue
			}
			debugFn.Locals = append(debugFn.Locals, DebugLocal{
				Name: name, Line: line, Index: uint32(index), Size: uint8(typeSize(param.Typ)),
			})
			seenNames[name] = true
		}
		for _, bb := range e.blocksForEmission(fn) {
			for _, instr := range bb.Instructions {
				result := instr.Result()
				instrLine := instructionLine(e.p, instr, line)
				if result != nil {
					if loc, ok := e.p.InstructionLocations[hir.InstructionKey(instr)]; ok && loc.Line > 0 {
						resultLines[result] = uint32(loc.Line)
					}
				}
				if returnLine == 0 && instrLine != line && result != nil && returnRegs[result] {
					returnLine = instrLine
				}
				if returnLine != 0 && instrLine == returnLine && !returnRegs[result] {
					instrLine = line
				}
				debugFn.Lines = append(debugFn.Lines, instrLine)
				if result == nil {
					continue
				}
				resultLocals[result] = nextLocal
				name := debugLocalName(reg(result))
				if name == "" || seenNames[name] {
					nextLocal++
					continue
				}
				localLine := line
				if resultLines[result] > 0 {
					localLine = resultLines[result]
				}
				debugFn.Locals = append(debugFn.Locals, DebugLocal{
					Name: name, Line: localLine, Index: nextLocal, Size: uint8(typeSize(result.Typ)), Deref: true,
				})
				seenNames[name] = true
				nextLocal++
			}
			// Terminators do not receive debug markers in the WABT emitter.
			// Keep them out of Lines as well: this slice is positional and must
			// have exactly one entry for every emitted marker, otherwise all
			// following source lines are assigned to the wrong Wasm addresses.
			if ret, ok := bb.Terminator.(*hir.InstrReturn); ok {
				returnLine := line + 1
				if len(ret.Vals) == 1 {
					if returned, ok := ret.Vals[0].(*hir.Reg); ok && resultLines[returned] > 0 {
						returnLine = resultLines[returned] + 1
					}
				}
				debugFn.Lines = append(debugFn.Lines, returnLine)
			}
		}
		// A scalar expression returned directly from a function has no source
		// identifier, but it is still useful at a return breakpoint. Expose it
		// as a synthetic `return` variable using the actual Wasm local that holds
		// the expression result.
		for _, bb := range e.blocksForEmission(fn) {
			ret, ok := bb.Terminator.(*hir.InstrReturn)
			if !ok || len(ret.Vals) != 1 || seenNames["return_of_function"] {
				continue
			}
			regValue, ok := ret.Vals[0].(*hir.Reg)
			if !ok {
				continue
			}
			index, ok := resultLocals[regValue]
			if !ok {
				continue
			}
			returnLine := line + 1
			if resultLines[regValue] > 0 {
				returnLine = resultLines[regValue] + 1
			}
			debugFn.Locals = append(debugFn.Locals, DebugLocal{Name: "return_of_function", Line: returnLine, Index: index, Size: uint8(typeSize(regValue.Typ))})
			seenNames["return_of_function"] = true
		}
		info.Functions = append(info.Functions, debugFn)
	}
	return info
}

func debugLocalName(name string) string {
	if len(name) == 0 || (name[0] == 'v' && isDecimal(name[1:])) {
		return ""
	}
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 && isDecimal(name[dot+1:]) {
		name = name[:dot]
	}
	name = strings.TrimSuffix(name, "_arg")
	return name
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

func instructionLine(program *hir.Program, instr hir.Instruction, fallback uint32) uint32 {
	if program != nil && program.InstructionLocations != nil {
		if loc, ok := program.InstructionLocations[hir.InstructionKey(instr)]; ok && loc.Line > 0 {
			return uint32(loc.Line)
		}
	}
	return fallback
}

// AppendDWARF appends the standard DWARF sections used by WebAssembly tools.
// WABT has already assigned final code offsets at this point, so the function
// DIEs and line table can refer to actual code ranges rather than placeholders.
func AppendDWARF(wasm []byte, info *DebugInfo) ([]byte, error) {
	if info == nil || info.Empty() {
		return wasm, nil
	}
	str, offsets := dwarfStrings(info)
	ranges := debugCodeRanges(wasm, info, true)
	abbrev := dwarfAbbrev()
	die := dwarfInfo(info, offsets, ranges)
	line := dwarfLine(info, ranges, offsets)
	result := append([]byte{}, wasm...)
	result = appendCustomSection(result, ".debug_abbrev", abbrev)
	result = appendCustomSection(result, ".debug_str", str)
	result = appendCustomSection(result, ".debug_line", line)
	result = appendCustomSection(result, ".debug_info", die)
	return result, nil
}

// SourceMapJSON creates a WebAssembly source map for the instruction markers
// emitted by the WABT backend. WebAssembly source-map generated columns are
// byte offsets in the final binary, so the marker offsets collected by
// wasmCodeRanges can be used directly.
func SourceMapJSON(wasm []byte, info *DebugInfo) ([]byte, error) {
	if info == nil || info.Empty() {
		return nil, nil
	}
	ranges := debugCodeRanges(wasm, info, false)
	var mappings []byte
	var previousOffset, previousSource, previousLine, previousColumn int32
	first := true
	for functionIndex, r := range ranges {
		if functionIndex >= len(info.Functions) {
			break
		}
		fn := info.Functions[functionIndex]
		// Make the function declaration itself a resolvable source location.
		// The first HIR instruction may be backend prologue code, so relying
		// only on instruction markers can leave a breakpoint on the function
		// signature without a generated address.
		line := int32(fn.Line)
		if line > 0 {
			line--
		}
		if !first {
			mappings = append(mappings, ',')
		}
		first = false
		values := []int32{
			int32(r.low) - previousOffset,
			0 - previousSource,
			line - previousLine,
			0 - previousColumn,
		}
		for _, value := range values {
			mappings = append(mappings, encodeSourceMapVLQ(value)...)
		}
		previousOffset = int32(r.low)
		previousSource = 0
		previousLine = line
		previousColumn = 0
		for markerIndex, marker := range r.markers {
			line := int32(fn.Line)
			if markerIndex < len(fn.Lines) && fn.Lines[markerIndex] != 0 {
				line = int32(fn.Lines[markerIndex])
			}
			if line > 0 {
				line-- // Source Map original lines are zero-based.
			}
			if !first {
				mappings = append(mappings, ',')
			}
			first = false
			values := []int32{
				int32(marker) - previousOffset,
				0 - previousSource,
				line - previousLine,
				0 - previousColumn,
			}
			for _, value := range values {
				mappings = append(mappings, encodeSourceMapVLQ(value)...)
			}
			previousOffset = int32(marker)
			previousSource = 0
			previousLine = line
			previousColumn = 0
		}
	}
	result := sourceMapFile{
		Version:  3,
		Sources:  []string{filepath.Base(info.SourcePath)},
		Names:    []string{},
		Mappings: string(mappings),
	}
	if source, err := os.ReadFile(info.SourcePath); err == nil {
		result.SourcesContent = []string{string(source)}
	}
	return json.MarshalIndent(result, "", "  ")
}

func debugCodeRanges(wasm []byte, info *DebugInfo, codeRelative bool) []codeRange {
	ranges := wasmCodeRanges(wasm, codeRelative)
	if info == nil || info.RuntimeFunctions <= 0 {
		return ranges
	}
	start := info.RuntimeFunctions
	if start >= len(ranges) {
		return nil
	}
	end := start + len(info.Functions)
	if end > len(ranges) {
		end = len(ranges)
	}
	return ranges[start:end]
}

// AppendSourceMapURL adds the WebAssembly sourceMappingURL custom section.
// The URL is normally a relative URL such as "./main.wasm.map".
func AppendSourceMapURL(wasm []byte, url string) []byte {
	payload := make([]byte, 0, len(url)+8)
	putULEB(&payload, uint32(len(url)))
	payload = append(payload, []byte(url)...)
	return appendCustomSection(wasm, "sourceMappingURL", payload)
}

const sourceMapBase64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func encodeSourceMapVLQ(value int32) []byte {
	encoded := uint32(value << 1)
	if value < 0 {
		encoded = uint32((-value << 1) | 1)
	}
	result := make([]byte, 0, 5)
	for {
		digit := encoded & 31
		encoded >>= 5
		if encoded != 0 {
			digit |= 32
		}
		result = append(result, sourceMapBase64[digit])
		if encoded == 0 {
			return result
		}
	}
}

func dwarfStrings(info *DebugInfo) ([]byte, map[string]uint32) {
	data := []byte{0}
	offsets := make(map[string]uint32)
	add := func(s string) {
		if _, ok := offsets[s]; ok {
			return
		}
		offsets[s] = uint32(len(data))
		data = append(data, []byte(s)...)
		data = append(data, 0)
	}
	add(debugSourceName(info))
	add("i32")
	for _, fn := range info.Functions {
		add(fn.Name)
		for _, local := range fn.Locals {
			add(local.Name)
		}
	}
	return data, offsets
}

func dwarfAbbrev() []byte {
	// Abbrev 1: compile unit. Abbrev 2: subprogram. Abbrev 3: variable.
	// Abbrev 4: the i32 base type used by the current Wasm ABI.
	return []byte{
		1, 0x11, 1, 0x03, 0x0e, 0x10, 0x06, 0x13, 0x05, 0, 0,
		2, 0x2e, 1, 0x03, 0x0e, 0x3a, 0x0b, 0x3b, 0x0b, 0x11, 0x01, 0x12, 0x01, 0, 0,
		3, 0x34, 0, 0x03, 0x0e, 0x3a, 0x0b, 0x3b, 0x0b, 0x49, 0x13, 0x02, 0x0a, 0, 0,
		4, 0x24, 0, 0x03, 0x0e, 0x0b, 0x0b, 0x3e, 0x0b, 0, 0,
		0,
	}
}

func dwarfInfo(info *DebugInfo, offsets map[string]uint32, ranges []codeRange) []byte {
	body := make([]byte, 0, 64+len(info.Functions)*16)
	body = append(body, 1) // compile-unit abbreviation
	putU32(&body, offsets[debugSourceName(info)])
	putU32(&body, 0)      // DW_AT_stmt_list: the line table starts at zero.
	putU16(&body, 0x0002) // DW_LANG_C is the closest standard language tag.
	// The type DIE is first, so its CU-relative offset is the fixed unit
	// header size: 4-byte length, version, abbrev offset, and address size.
	body = append(body, 4)
	putU32(&body, offsets["i32"])
	body = append(body, 4, 0x05) // 4-byte signed integer.
	for i, fn := range info.Functions {
		body = append(body, 2)
		putU32(&body, offsets[fn.Name])
		body = append(body, 1) // DW_AT_decl_file: the first file in the CU.
		body = append(body, byte(clampLine(fn.Line)))
		if i < len(ranges) {
			putU32(&body, ranges[i].low)
			putU32(&body, ranges[i].high)
		} else {
			putU32(&body, 0)
			putU32(&body, 0)
		}
		for _, local := range fn.Locals {
			body = append(body, 3)
			putU32(&body, offsets[local.Name])
			body = append(body, 1, byte(clampLine(local.Line)))
			putU32(&body, 22) // CU-relative offset of the i32 type DIE.
			expr := []byte{0xed, 0}
			putULEB(&expr, local.Index)
			if local.Deref && local.Size == 4 {
				expr = append(expr, 0x06) // DW_OP_deref
			} else if local.Deref && local.Size > 0 {
				expr = append(expr, 0x94, local.Size) // DW_OP_deref_size
			} else {
				expr = append(expr, 0x9f) // DW_OP_stack_value
			}
			body = append(body, byte(len(expr)))
			body = append(body, expr...)
		}
		body = append(body, 0) // end subprogram children
	}
	body = append(body, 0) // end children

	unit := make([]byte, 0, len(body)+12)
	putU32(&unit, uint32(len(body)+7))
	putU16(&unit, 4)
	putU32(&unit, 0)       // abbrev section offset
	unit = append(unit, 4) // address size
	unit = append(unit, body...)
	return unit
}

func dwarfLine(info *DebugInfo, ranges []codeRange, offsets map[string]uint32) []byte {
	_ = offsets
	header := make([]byte, 0, 64)
	header = append(header, 1, 1, 1, 251, 14, 13)
	header = append(header, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0)
	header = append(header, 0) // include-directory table
	header = append(header, []byte(debugSourceName(info))...)
	header = append(header, 0)
	putULEB(&header, 0)
	putULEB(&header, 0)
	putULEB(&header, 0)
	header = append(header, 0) // file table terminator

	program := make([]byte, 0, len(ranges)*12+3)
	lastLine := uint32(1)
	var lastAddress uint32
	addressSet := false
	advancePC := func(address uint32) {
		if address <= lastAddress {
			return
		}
		program = append(program, 2) // DW_LNS_advance_pc
		putULEB(&program, address-lastAddress)
		lastAddress = address
	}
	emitLine := func(line uint32) {
		if line == 0 || line == lastLine {
			return
		}
		program = append(program, 3) // DW_LNS_advance_line
		putSLEB(&program, int32(line)-int32(lastLine))
		program = append(program, 1) // DW_LNS_copy
		lastLine = line
	}
	lineIndex := 0
	for _, r := range ranges {
		if lineIndex >= len(info.Functions) {
			break
		}
		fn := info.Functions[lineIndex]
		markers := r.markers
		if len(markers) == 0 {
			markers = []uint32{r.low}
		}
		// Emit a row at low_pc for the source-level function declaration.
		// This keeps a breakpoint on a signature line resolvable even when
		// the first emitted instruction is compiler-generated prologue code.
		if !addressSet {
			program = append(program, 0, 5, 2)
			putU32(&program, r.low)
			lastAddress = r.low
			addressSet = true
		} else {
			advancePC(r.low)
		}
		emitLine(fn.Line)
		for markerIndex, marker := range markers {
			if marker == r.low {
				continue
			}
			line := fn.Line
			if markerIndex < len(fn.Lines) {
				line = fn.Lines[markerIndex]
			}
			if marker < lastAddress {
				continue
			}
			advancePC(marker)
			emitLine(line)
		}
		lineIndex++
	}
	if lineIndex > 0 && lineIndex <= len(ranges) {
		// Keep one monotonically increasing line sequence for the whole code
		// section. Wasm LLDB versions in the wild do not reliably handle a
		// separate sequence for every function, but still require a final
		// end_sequence row to make source breakpoints resolvable.
		if ranges[lineIndex-1].high > lastAddress {
			advancePC(ranges[lineIndex-1].high)
		}
		program = append(program, 0, 1, 1)
	}

	unit := make([]byte, 0, len(header)+len(program)+16)
	putU32(&unit, uint32(2+4+len(header)+len(program)))
	putU16(&unit, 4)
	putU32(&unit, uint32(len(header)))
	unit = append(unit, header...)
	unit = append(unit, program...)
	return unit
}

func wasmCodeRanges(wasm []byte, codeRelative bool) []codeRange {
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return nil
	}
	for pos := 8; pos < len(wasm); {
		sectionID := wasm[pos]
		pos++
		size, n := readULEB(wasm[pos:])
		if n == 0 || pos+n+int(size) > len(wasm) {
			return nil
		}
		payload := pos + n
		end := payload + int(size)
		if sectionID == 10 {
			count, m := readULEB(wasm[payload:])
			if m == 0 {
				return nil
			}
			at := payload + m
			codeBase := 0
			if codeRelative {
				// DWARF WebAssembly addresses are relative to the beginning
				// of the Code section contents. Source maps use file offsets
				// instead and call this function with codeRelative=false.
				codeBase = payload
			}
			ranges := make([]codeRange, 0, count)
			for i := uint32(0); i < count && at < end; i++ {
				bodySize, k := readULEB(wasm[at:])
				if k == 0 || at+k+int(bodySize) > end {
					return nil
				}
				absoluteLow := uint32(at + k)
				absoluteHigh := absoluteLow + bodySize
				low := absoluteLow - uint32(codeBase)
				r := codeRange{low: low, high: low + bodySize}
				for marker := absoluteLow; marker+4 <= absoluteHigh; marker++ {
					if wasm[marker] == 0x02 && wasm[marker+1] == 0x40 && wasm[marker+2] == 0x01 && wasm[marker+3] == 0x0b {
						r.markers = append(r.markers, marker-uint32(codeBase))
					}
				}
				ranges = append(ranges, r)
				at += k + int(bodySize)
			}
			return ranges
		}
		pos = end
	}
	return nil
}

func readULEB(data []byte) (uint32, int) {
	var value uint32
	for i, b := range data {
		if i == 5 {
			return 0, 0
		}
		value |= uint32(b&0x7f) << uint(7*i)
		if b&0x80 == 0 {
			return value, i + 1
		}
	}
	return 0, 0
}

func putSLEB(dst *[]byte, value int32) {
	more := true
	for more {
		b := byte(value) & 0x7f
		value >>= 7
		more = !((value == 0 && b&0x40 == 0) || (value == -1 && b&0x40 != 0))
		if more {
			b |= 0x80
		}
		*dst = append(*dst, b)
	}
}

func clampLine(line uint32) uint32 {
	if line == 0 {
		return 1
	}
	if line > 255 {
		return 255
	}
	return line
}

func appendCustomSection(wasm []byte, name string, payload []byte) []byte {
	section := make([]byte, 0, len(name)+len(payload)+8)
	putULEB(&section, uint32(len(name)))
	section = append(section, []byte(name)...)
	section = append(section, payload...)
	result := append(wasm, 0)
	putULEB(&result, uint32(len(section)))
	result = append(result, section...)
	return result
}

func putU16(dst *[]byte, value uint16) {
	*dst = append(*dst, byte(value), byte(value>>8))
}

func putU32(dst *[]byte, value uint32) {
	*dst = append(*dst, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

func putULEB(dst *[]byte, value uint32) {
	for value >= 0x80 {
		*dst = append(*dst, byte(value)|0x80)
		value >>= 7
	}
	*dst = append(*dst, byte(value))
}
