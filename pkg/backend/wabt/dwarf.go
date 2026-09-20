package wabt

// This file contains the small DWARF producer used by the WABT backend.  The
// WAT format can preserve names, but it cannot describe source locations.  We
// therefore keep the source-level function table while emitting WAT and add
// the standard WebAssembly DWARF custom sections after wat2wasm has assembled
// the module.

import (
	"path/filepath"

	"hikec-go/pkg/hir"
)

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
}

type DebugInfo struct {
	SourcePath string
	Functions  []DebugFunction
}

type codeRange struct {
	low     uint32
	high    uint32
	markers []uint32
}

func (d *DebugInfo) Empty() bool { return d == nil || len(d.Functions) == 0 }

func (e *Emitter) DebugInfo(sourcePath string) *DebugInfo {
	info := &DebugInfo{SourcePath: sourcePath}
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
		for index, param := range fn.Params {
			if param == nil {
				continue
			}
			debugFn.Locals = append(debugFn.Locals, DebugLocal{
				Name: reg(param), Line: line, Index: uint32(index),
			})
		}
		nextLocal := uint32(len(fn.Params))
		seen := make(map[string]bool)
		for _, param := range fn.Params {
			if param != nil {
				seen[reg(param)] = true
			}
		}
		for _, bb := range fn.Blocks {
			for _, instr := range bb.Instructions {
				debugFn.Lines = append(debugFn.Lines, instructionLine(e.p, instr, line))
				result := instr.Result()
				if result == nil || seen[reg(result)] {
					continue
				}
				localLine := line
				if loc, ok := e.p.InstructionLocations[hir.InstructionKey(instr)]; ok && loc.Line > 0 {
					localLine = uint32(loc.Line)
				}
				debugFn.Locals = append(debugFn.Locals, DebugLocal{
					Name: reg(result), Line: localLine, Index: nextLocal,
				})
				seen[reg(result)] = true
				nextLocal++
			}
			if bb.Terminator != nil {
				debugFn.Lines = append(debugFn.Lines, instructionLine(e.p, bb.Terminator, line))
			}
		}
		info.Functions = append(info.Functions, debugFn)
	}
	return info
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
	ranges := wasmCodeRanges(wasm)
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
	add(filepath.Base(info.SourcePath))
	for _, fn := range info.Functions {
		add(fn.Name)
		for _, local := range fn.Locals {
			add(local.Name)
		}
	}
	return data, offsets
}

func dwarfAbbrev() []byte {
	// Abbrev 1: compile unit.  Abbrev 2: subprogram.
	return []byte{
		1, 0x11, 1, 0x03, 0x0e, 0x10, 0x06, 0x13, 0x05, 0, 0,
		2, 0x2e, 1, 0x03, 0x0e, 0x3a, 0x0b, 0x3b, 0x0b, 0x11, 0x01, 0x12, 0x01, 0, 0,
		3, 0x34, 0, 0x03, 0x0e, 0x3a, 0x0b, 0x3b, 0x0b, 0x02, 0x0a, 0, 0,
		0,
	}
}

func dwarfInfo(info *DebugInfo, offsets map[string]uint32, ranges []codeRange) []byte {
	body := make([]byte, 0, 64+len(info.Functions)*16)
	body = append(body, 1) // compile-unit abbreviation
	putU32(&body, offsets[filepath.Base(info.SourcePath)])
	putU32(&body, 0)      // DW_AT_stmt_list: the line table starts at zero.
	putU16(&body, 0x0002) // DW_LANG_C is the closest standard language tag.
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
			expr := []byte{0xed, 0}
			putULEB(&expr, local.Index)
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
	header = append(header, []byte(filepath.Base(info.SourcePath))...)
	header = append(header, 0)
	putULEB(&header, 0)
	putULEB(&header, 0)
	putULEB(&header, 0)
	header = append(header, 0) // file table terminator

	program := make([]byte, 0, len(ranges)*12+3)
	lastLine := uint32(1)
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
		for markerIndex, marker := range markers {
			line := fn.Line
			if markerIndex < len(fn.Lines) {
				line = fn.Lines[markerIndex]
			}
			program = append(program, 0, 5, 2)
			putU32(&program, marker)
			program = append(program, 3)
			putSLEB(&program, int32(line)-int32(lastLine))
			program = append(program, 1) // DW_LNS_copy
			lastLine = line
		}
		lineIndex++
	}
	program = append(program, 0, 1, 1) // DW_LNE_end_sequence

	unit := make([]byte, 0, len(header)+len(program)+16)
	putU32(&unit, uint32(2+4+len(header)+len(program)))
	putU16(&unit, 4)
	putU32(&unit, uint32(len(header)))
	unit = append(unit, header...)
	unit = append(unit, program...)
	return unit
}

func wasmCodeRanges(wasm []byte) []codeRange {
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
			ranges := make([]codeRange, 0, count)
			for i := uint32(0); i < count && at < end; i++ {
				bodySize, k := readULEB(wasm[at:])
				if k == 0 || at+k+int(bodySize) > end {
					return nil
				}
				low := uint32(at + k)
				r := codeRange{low: low, high: low + bodySize}
				for marker := low; marker+4 <= r.high; marker++ {
					if wasm[marker] == 0x02 && wasm[marker+1] == 0x40 && wasm[marker+2] == 0x01 && wasm[marker+3] == 0x0b {
						r.markers = append(r.markers, marker)
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
