package codegen

import (
	"fmt"
	"path/filepath"
	"strings"

	"hikec-go/pkg/sema"
)

type SubprogramMeta struct {
	ID      int
	Name    string
	Line    int
	TypeID  int
	EmptyID int
}

type LocalVarMeta struct {
	ID      int
	Name    string
	ScopeID int
	Line    int
	TypeID  int
	IsParam bool
	ArgIdx  int
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
	currentSP    int
	subprograms  []*SubprogramMeta
	localVars    []*LocalVarMeta
	locMap       map[string]int
	typeMap      map[string]int
	typeMetaList []string
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
		localVars:    make([]*LocalVarMeta, 0),
		locMap:       make(map[string]int),
		typeMap:      make(map[string]int),
		typeMetaList: make([]string, 0),
		metadataList: make([]string, 0),
		nextID:       0,
	}

	dm.cuID = dm.allocID()       // !0: CompileUnit
	dm.dwarfVerID = dm.allocID() // !1: Dwarf Version
	dm.debugVerID = dm.allocID() // !2: Debug Info Version
	dm.fileID = dm.allocID()     // !3: DIFile

	return dm
}

func (dm *DebugManager) allocID() int {
	id := dm.nextID
	dm.nextID++
	return id
}

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

func (dm *DebugManager) GetTypeID(t sema.Type) int {
	if !dm.enabled {
		return 0
	}

	typeName := "int"
	if t != nil {
		typeName = t.TypeName()
	}

	if id, exists := dm.typeMap[typeName]; exists {
		return id
	}

	typeID := dm.allocID()
	dm.typeMap[typeName] = typeID

	if t == nil || t == sema.TypeInt {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"int\", size: 64, encoding: DW_ATE_signed)", typeID))
	} else if t == sema.TypeBool {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"bool\", size: 8, encoding: DW_ATE_boolean)", typeID))
	} else if t == sema.TypeByte {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"byte\", size: 8, encoding: DW_ATE_unsigned_char)", typeID))
	} else if t == sema.TypeFloat64 {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"float64\", size: 64, encoding: DW_ATE_float)", typeID))
	} else if t == sema.TypeFloat32 {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"float32\", size: 32, encoding: DW_ATE_float)", typeID))
	} else if t == sema.TypeString {
		byteTypeID := dm.GetTypeID(sema.TypeByte)
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIDerivedType(tag: DW_TAG_pointer_type, baseType: !%d, size: 64)", typeID, byteTypeID))
	} else if ptrType, ok := t.(*sema.PointerType); ok {
		baseTypeID := dm.GetTypeID(ptrType.Base)
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIDerivedType(tag: DW_TAG_pointer_type, baseType: !%d, size: 64)", typeID, baseTypeID))
	} else {
		dm.typeMetaList = append(dm.typeMetaList,
			fmt.Sprintf("!%d = !DIBasicType(name: \"%s\", size: 64, encoding: DW_ATE_signed)", typeID, typeName))
	}

	return typeID
}

func (dm *DebugManager) RegisterLocalVariable(name string, line, col int, t sema.Type, isParam bool, argIdx int) (int, int) {
	if !dm.enabled || dm.currentSP == 0 {
		return 0, 0
	}
	if line <= 0 {
		line = 1
	}
	if col <= 0 {
		col = 1
	}

	varID := dm.allocID()
	typeID := dm.GetTypeID(t)

	dm.localVars = append(dm.localVars, &LocalVarMeta{
		ID:      varID,
		Name:    name,
		ScopeID: dm.currentSP,
		Line:    line,
		TypeID:  typeID,
		IsParam: isParam,
		ArgIdx:  argIdx,
	})

	key := fmt.Sprintf("%d:%d:%d", line, col, dm.currentSP)
	locID, exists := dm.locMap[key]
	if !exists {
		locID = dm.allocID()
		dm.locMap[key] = locID
		dm.metadataList = append(dm.metadataList,
			fmt.Sprintf("!%d = !DILocation(line: %d, column: %d, scope: !%d)", locID, line, col, dm.currentSP))
	}

	return varID, locID
}

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

	// 型メタデータの出力
	for _, tMeta := range dm.typeMetaList {
		b.WriteString(tMeta + "\n")
	}
	b.WriteString("\n")

	// 関数メタデータの出力
	for _, sp := range dm.subprograms {
		b.WriteString(fmt.Sprintf("!%d = distinct !DISubprogram(name: \"%s\", scope: !%d, file: !%d, line: %d, type: !%d, scopeLine: %d, spFlags: DISPFlagDefinition, unit: !%d)\n",
			sp.ID, sp.Name, dm.fileID, dm.fileID, sp.Line, sp.TypeID, sp.Line, dm.cuID))
		b.WriteString(fmt.Sprintf("!%d = !DISubroutineType(types: !%d)\n", sp.TypeID, sp.EmptyID))
		b.WriteString(fmt.Sprintf("!%d = !{null}\n", sp.EmptyID))
	}
	b.WriteString("\n")

	// ローカル変数メタデータの出力
	for _, lv := range dm.localVars {
		if lv.IsParam {
			b.WriteString(fmt.Sprintf("!%d = !DILocalVariable(name: \"%s\", arg: %d, scope: !%d, file: !%d, line: %d, type: !%d)\n",
				lv.ID, lv.Name, lv.ArgIdx, lv.ScopeID, dm.fileID, lv.Line, lv.TypeID))
		} else {
			b.WriteString(fmt.Sprintf("!%d = !DILocalVariable(name: \"%s\", scope: !%d, file: !%d, line: %d, type: !%d)\n",
				lv.ID, lv.Name, lv.ScopeID, dm.fileID, lv.Line, lv.TypeID))
		}
	}
	b.WriteString("\n")

	// 行・列位置メタデータの出力
	for _, meta := range dm.metadataList {
		b.WriteString(meta + "\n")
	}

	return b.String()
}
