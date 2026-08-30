package codegen

import (
	"fmt"
	"path/filepath"
	"strings"
)

type SubprogramMeta struct {
	ID      int
	Name    string
	Line    int
	TypeID  int
	EmptyID int
}

type DebugManager struct {
	enabled      bool
	sourcePath   string
	filename     string
	directory    string
	cuID         int
	dwarfVerID   int
	debugVerID   int
	fileID       int
	currentSP    int // 現在コンパイル中の関数スコープID
	subprograms  []*SubprogramMeta
	locMap       map[string]int
	metadataList []string
	nextID       int
}

func NewDebugManager(sourcePath string, enabled bool) *DebugManager {
	if !enabled || sourcePath == "" {
		return &DebugManager{enabled: false}
	}

	absPath, _ := filepath.Abs(sourcePath)
	dir := filepath.ToSlash(filepath.Dir(absPath))
	file := filepath.Base(absPath)

	dm := &DebugManager{
		enabled:      true,
		sourcePath:   absPath,
		filename:     file,
		directory:    dir,
		currentSP:    0,
		subprograms:  make([]*SubprogramMeta, 0),
		locMap:       make(map[string]int),
		metadataList: make([]string, 0),
		nextID:       0,
	}

	dm.cuID = dm.allocID()       // !0
	dm.dwarfVerID = dm.allocID() // !1
	dm.debugVerID = dm.allocID() // !2
	dm.fileID = dm.allocID()     // !3

	return dm
}

func (dm *DebugManager) allocID() int {
	id := dm.nextID
	dm.nextID++
	return id
}

// 関数開始時に呼び出し、現在のスコープIDを設定
func (dm *DebugManager) StartFunction(funcName string, line int) int {
	if !dm.enabled {
		return 0
	}
	if line <= 0 {
		line = 1
	}

	spID := dm.allocID()
	typeID := dm.allocID()
	emptyID := dm.allocID()

	dm.currentSP = spID
	dm.subprograms = append(dm.subprograms, &SubprogramMeta{
		ID:      spID,
		Name:    funcName,
		Line:    line,
		TypeID:  typeID,
		EmptyID: emptyID,
	})
	return spID
}

// 行・列番号から !dbg タグを即座に生成
func (dm *DebugManager) GetLocationTag(line, col int) string {
	if !dm.enabled || dm.currentSP == 0 || line <= 0 {
		return ""
	}
	if col <= 0 {
		col = 1
	}

	key := fmt.Sprintf("%d:%d:%d", line, col, dm.currentSP)
	locID, exists := dm.locMap[key]
	if !exists {
		locID = dm.allocID()
		dm.locMap[key] = locID
		dm.metadataList = append(dm.metadataList,
			fmt.Sprintf("!%d = !DILocation(line: %d, column: %d, scope: !%d)", locID, line, col, dm.currentSP))
	}
	return fmt.Sprintf(", !dbg !%d", locID)
}

func (dm *DebugManager) EmitMetadata() string {
	if !dm.enabled {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n; ==========================================\n")
	b.WriteString("; LLVM Debug Metadata (DWARF)\n")
	b.WriteString("; ==========================================\n")
	b.WriteString(fmt.Sprintf("!llvm.dbg.cu = !{!%d}\n", dm.cuID))
	b.WriteString(fmt.Sprintf("!llvm.module.flags = !{!%d, !%d}\n\n", dm.dwarfVerID, dm.debugVerID))

	b.WriteString(fmt.Sprintf("!%d = distinct !DICompileUnit(language: DW_LANG_C, file: !%d, producer: \"hikec\", isOptimized: false, runtimeVersion: 0, emissionKind: FullDebug)\n", dm.cuID, dm.fileID))
	b.WriteString(fmt.Sprintf("!%d = !{i32 2, !\"Dwarf Version\", i32 4}\n", dm.dwarfVerID))
	b.WriteString(fmt.Sprintf("!%d = !{i32 2, !\"Debug Info Version\", i32 3}\n", dm.debugVerID))
	b.WriteString(fmt.Sprintf("!%d = !DIFile(filename: \"%s\", directory: \"%s\")\n\n", dm.fileID, dm.filename, dm.directory))

	for _, sp := range dm.subprograms {
		b.WriteString(fmt.Sprintf("!%d = distinct !DISubprogram(name: \"%s\", scope: !%d, file: !%d, line: %d, type: !%d, scopeLine: %d, spFlags: DISPFlagDefinition, unit: !%d)\n",
			sp.ID, sp.Name, dm.fileID, dm.fileID, sp.Line, sp.TypeID, sp.Line, dm.cuID))
		b.WriteString(fmt.Sprintf("!%d = !DISubroutineType(types: !%d)\n", sp.TypeID, sp.EmptyID))
		b.WriteString(fmt.Sprintf("!%d = !{null}\n", sp.EmptyID))
	}
	b.WriteString("\n")

	for _, meta := range dm.metadataList {
		b.WriteString(meta + "\n")
	}

	return b.String()
}
