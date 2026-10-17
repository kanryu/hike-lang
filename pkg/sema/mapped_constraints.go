package sema

import (
	"fmt"

	"hikec-go/pkg/ast"
)

// validateMappedMemoryBlocks rejects the legacy bucketed hashmap from mapped
// state. Mapped serialization relies on the compact-dict entry layout; the
// legacy hashmap contains a different pointer graph and is not a supported
// persistent representation.
func validateMappedMemoryBlocks(prog *ast.Program, ctx *Context) error {
	for _, decl := range prog.Decls {
		block, ok := decl.(*ast.MemoryBlockDecl)
		if !ok || block.MapName == nil {
			continue
		}
		for _, variable := range block.Vars {
			typ := ctx.Globals[variable.Name.Value]
			if err := validateMappedType(variable.Name.Value, typ, make(map[Type]bool)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMappedType(variableName string, typ Type, seen map[Type]bool) error {
	if typ == nil || seen[typ] {
		return nil
	}
	seen[typ] = true

	switch t := typ.(type) {
	case *MapType:
		if t.LegacyHashMap {
			return fmt.Errorf("mapped variable %q cannot contain legacy hashmap; use map (compact dict) instead", variableName)
		}
		if err := validateMappedType(variableName, t.Key, seen); err != nil {
			return err
		}
		return validateMappedType(variableName, t.Value, seen)
	case *SliceType:
		return validateMappedType(variableName, t.Elem, seen)
	case *ArrayType:
		return validateMappedType(variableName, t.Elem, seen)
	case *PointerType:
		return validateMappedType(variableName, t.Base, seen)
	case *StructType:
		for _, field := range t.Fields {
			if err := validateMappedType(variableName, field.Type, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
