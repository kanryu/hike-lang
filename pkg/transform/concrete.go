package transform

import (
	"fmt"
	"reflect"

	"hikec-go/pkg/ast"
)

// ValidateConcreteProgram verifies the contract between Transform and lower:
// the AST must not contain generic declarations or unresolved generic
// instantiation expressions after monomorphization.
func ValidateConcreteProgram(prog *ast.Program) error {
	if prog == nil {
		return fmt.Errorf("concrete AST is nil")
	}
	seen := make(map[concreteVisit]bool)
	return validateConcreteValue(reflect.ValueOf(prog), "program", seen)
}

type concreteVisit struct {
	typ reflect.Type
	ptr uintptr
}

func validateConcreteValue(value reflect.Value, path string, seen map[concreteVisit]bool) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateConcreteValue(value.Elem(), path, seen)
	}

	if value.CanInterface() {
		switch node := value.Interface().(type) {
		case *ast.GenericInstExpr:
			return fmt.Errorf("unresolved generic instantiation at %s (line %d, token %q)", path, node.Token.Line, node.Token.Literal)
		case *ast.FuncDecl:
			if len(node.TypeParams) != 0 {
				return fmt.Errorf("generic function remains at %s", path)
			}
		case *ast.TypeDecl:
			if len(node.TypeParams) != 0 {
				return fmt.Errorf("generic type remains at %s", path)
			}
		}
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		visit := concreteVisit{typ: value.Type(), ptr: value.Pointer()}
		if seen[visit] {
			return nil
		}
		seen[visit] = true
		return validateConcreteValue(value.Elem(), path, seen)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if err := validateConcreteValue(value.Field(i), path+"."+field.Name, seen); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateConcreteValue(value.Index(i), fmt.Sprintf("%s[%d]", path, i), seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if err := validateConcreteValue(value.MapIndex(key), path, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
