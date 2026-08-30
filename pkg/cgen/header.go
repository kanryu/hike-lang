package cgen

import (
	"fmt"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
)

// GenerateHeader はC/C++両対応のヘッダー文字列を生成します
func GenerateHeader(prog *ast.Program, semaCtx *sema.Context, outputFileName string) string {
	var b strings.Builder

	base := filepath.Base(outputFileName)
	ext := filepath.Ext(base)
	cleanName := strings.TrimSuffix(base, ext)
	guard := fmt.Sprintf("HIKE_%s_H", strings.ToUpper(strings.ReplaceAll(cleanName, "-", "_")))

	b.WriteString("/*\n")
	b.WriteString(" * ========================================================\n")
	b.WriteString(" * Powered by Hike Language\n")
	b.WriteString(" * Auto-generated C/C++ Header File\n")
	b.WriteString(" * ========================================================\n")
	b.WriteString(" */\n\n")

	b.WriteString(fmt.Sprintf("#ifndef %s\n", guard))
	b.WriteString(fmt.Sprintf("#define %s\n\n", guard))

	b.WriteString("#include <stdint.h>\n")
	b.WriteString("#include <stdbool.h>\n")
	b.WriteString("#include <stddef.h>\n\n")

	b.WriteString("#ifdef __cplusplus\n")
	b.WriteString("extern \"C\" {\n")
	b.WriteString("#endif\n\n")

	// Windows環境では __declspec(dllimport) を明示
	b.WriteString("#ifndef HIKE_API\n")
	b.WriteString("  #if defined(_WIN32) || defined(__CYGWIN__)\n")
	b.WriteString("    #define HIKE_API __declspec(dllimport)\n")
	b.WriteString("  #else\n")
	b.WriteString("    #define HIKE_API extern\n")
	b.WriteString("  #endif\n")
	b.WriteString("#endif\n\n")

	// ==========================================
	// 1. 構造体定義
	// ==========================================
	b.WriteString("/* --- Struct Definitions --- */\n")
	emittedStructs := make(map[string]bool)

	if semaCtx != nil {
		for name, st := range semaCtx.Structs {
			if isInternalStruct(name) || emittedStructs[name] {
				continue
			}
			emitStructDefinition(&b, st)
			emittedStructs[name] = true
		}
	}

	if prog != nil {
		for _, decl := range prog.Decls {
			if td, ok := decl.(*ast.TypeDecl); ok && len(td.TypeParams) == 0 {
				if stType, isStruct := td.Type.(*ast.StructType); isStruct {
					name := td.Name.Value
					if !emittedStructs[name] && !isInternalStruct(name) {
						b.WriteString(fmt.Sprintf("typedef struct %s {\n", name))
						for _, f := range stType.Fields {
							cType := "void*"
							if semaCtx != nil {
								cType = toCType(semaCtx.ResolveType(f.Type))
							}
							b.WriteString(fmt.Sprintf("    %s %s;\n", cType, f.Name.Value))
						}
						b.WriteString(fmt.Sprintf("} %s;\n\n", name))
						emittedStructs[name] = true
					}
				}
			}
		}
	}

	// ==========================================
	// 2. 関数プロトタイプ宣言
	// ==========================================
	b.WriteString("/* --- Function Prototypes --- */\n")
	emittedFuncs := make(map[string]bool)

	if prog != nil {
		for _, decl := range prog.Decls {
			fnDecl, ok := decl.(*ast.FuncDecl)
			if !ok || fnDecl.Name == nil {
				continue
			}
			fnName := fnDecl.Name.Value
			if isInternalFunc(fnName) || len(fnDecl.TypeParams) > 0 || fnDecl.Receiver != nil || emittedFuncs[fnName] {
				continue
			}

			var fnMeta *sema.FuncType
			if semaCtx != nil {
				fnMeta = semaCtx.Functions[fnName]
			}

			retTypeStr := "void"
			if fnMeta != nil && len(fnMeta.ReturnTypes) == 1 {
				retTypeStr = toCType(fnMeta.ReturnTypes[0])
			} else if len(fnDecl.ReturnTypes) == 1 && semaCtx != nil {
				retTypeStr = toCType(semaCtx.ResolveType(fnDecl.ReturnTypes[0]))
			}

			params := []string{}
			for i, p := range fnDecl.Params {
				var pType sema.Type = sema.TypeInt
				if fnMeta != nil && i < len(fnMeta.ParamTypes) {
					pType = fnMeta.ParamTypes[i]
				} else if semaCtx != nil {
					pType = semaCtx.ResolveType(p.Type)
				}
				params = append(params, fmt.Sprintf("%s %s", toCType(pType), p.Name.Value))
			}
			if len(params) == 0 {
				params = append(params, "void")
			}

			b.WriteString(fmt.Sprintf("HIKE_API %s %s(%s);\n", retTypeStr, fnName, strings.Join(params, ", ")))
			emittedFuncs[fnName] = true
		}
	}

	b.WriteString("\n#ifdef __cplusplus\n")
	b.WriteString("}\n")
	b.WriteString("#endif\n\n")

	b.WriteString(fmt.Sprintf("#endif /* %s */\n", guard))
	return b.String()
}

func isInternalStruct(name string) bool {
	return strings.HasPrefix(name, "__") || strings.Contains(name, "[") || name == "Arena" || name == "Allocator"
}

func isInternalFunc(name string) bool {
	return name == "main" || strings.HasPrefix(name, "__") || strings.HasPrefix(name, "hike_") ||
		name == "malloc" || name == "free" || name == "calloc" || name == "strcmp" ||
		name == "strlen" || name == "memcpy" || name == "memcmp" || name == "printf"
}

func emitStructDefinition(b *strings.Builder, st *sema.StructType) {
	b.WriteString(fmt.Sprintf("typedef struct %s {\n", st.Name))
	for _, f := range st.Fields {
		cType := toCType(f.Type)
		b.WriteString(fmt.Sprintf("    %s %s;\n", cType, f.Name))
	}
	b.WriteString(fmt.Sprintf("} %s;\n\n", st.Name))
}

func toCType(t sema.Type) string {
	if t == nil {
		return "void"
	}
	switch v := t.(type) {
	case *sema.BasicType:
		switch v.Name {
		case "int":
			return "int64_t"
		case "byte":
			return "uint8_t"
		case "bool":
			return "bool"
		case "float32":
			return "float"
		case "float64", "float":
			return "double"
		case "string":
			return "const char*"
		case "void":
			return "void"
		default:
			return v.Name
		}
	case *sema.PointerType:
		base := toCType(v.Base)
		return base + "*"
	case *sema.StructType:
		return v.Name
	}
	return "void*"
}
