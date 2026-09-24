// Package symbols provides a source-level symbol catalogue for Hike programs.
package symbols

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
)

// Method describes a method and the receiver it is attached to.
type Method struct {
	Name     string `json:"name"`
	Receiver string `json:"receiver"`
}

// Struct describes a structure and its declared members.
type Struct struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// Interface describes an interface type and its method set.
type Interface struct {
	Name    string   `json:"name"`
	Methods []string `json:"methods"`
}

// Export is the JSON representation written by --export-symbols.
type Export struct {
	Modules        []string    `json:"modules"`
	TypeStructs    []Struct    `json:"structs"`
	TypeInterfaces []Interface `json:"interfaces"`
	Functions      []string    `json:"functions"`
	Globals        []string    `json:"globals"`
	Locals         []string    `json:"locals"`
	FixedReceivers []Method    `json:"fixed_receivers"`
}

// Collect discovers declaration identifiers in a loaded Hike program. Fixed
// receivers are limited to methods that satisfy a declared or built-in
// interface capability.
func Collect(program *ast.Program, ctx *sema.Context) Export {
	result := Export{
		TypeStructs:    make([]Struct, 0),
		TypeInterfaces: make([]Interface, 0),
		FixedReceivers: make([]Method, 0),
	}
	if program == nil {
		return result
	}

	modules := stringSet{}
	functions := stringSet{}
	globals := stringSet{}
	locals := stringSet{}
	methods := make(map[string]Method)
	packages := declarationPackages(program)

	for _, imp := range program.Imports {
		if imp != nil && imp.Path != "" {
			modules.add(imp.Path)
		}
	}

	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil {
				continue
			}
			if d.Receiver != nil {
				receiver := typeName(d.Receiver.Type)
				if isFixedReceiverMethod(d, ctx) {
					pkg := receiverPackage(receiver, program.Package, packages)
					methods[receiver+"\x00"+d.Name.Value] = Method{
						Name:     qualifyName(pkg, d.Name.Value),
						Receiver: qualifyTypeName(pkg, receiver),
					}
				}
				if d.Receiver.Name != nil {
					locals.add(d.Receiver.Name.Value)
				}
			} else {
				functions.add(qualifiedDeclName(d.InternalKey, d.Name.Value, program.Package, packages))
			}
			collectParams(d.Params, locals)
			collectBlock(d.Body, locals)
		case *ast.CFuncDecl:
			if d.Name != nil {
				functions.add(qualifiedDeclName(d.InternalKey, d.Name.Value, program.Package, packages))
			}
			collectParams(d.Params, locals)
			collectBlock(d.Body, locals)
		case *ast.ExternFuncDecl:
			if d.Name != nil {
				functions.add(qualifiedDeclName(d.InternalKey, d.Name.Value, program.Package, packages))
			}
			collectParams(d.Params, locals)
		case *ast.JFuncDecl:
			if d.Name != nil {
				functions.add(qualifiedDeclName(d.InternalKey, d.Name.Value, program.Package, packages))
			}
			collectParams(d.Params, locals)
		case *ast.VarDecl:
			addIdentifier(d.Name, globals)
		case *ast.ConstDecl:
			addIdentifier(d.Name, globals)
		case *ast.MemoryBlockDecl:
			for _, variable := range d.Vars {
				if variable != nil {
					addIdentifier(variable.Name, globals)
				}
			}
		case *ast.TypeDecl:
			if st, ok := d.Type.(*ast.StructType); ok && d.Name != nil {
				pkg, typeNameValue := declarationIdentity(d.InternalKey, d.Name.Value, program.Package, packages)
				members := make([]string, 0, len(st.Fields))
				for _, field := range st.Fields {
					if field != nil && field.Name != nil {
						members = append(members, qualifyName(pkg+"."+typeNameValue, field.Name.Value))
					}
				}
				sort.Strings(members)
				result.TypeStructs = append(result.TypeStructs, Struct{
					Name:    qualifyName(pkg, typeNameValue),
					Members: members,
				})
			} else if ifaceAST, ok := d.Type.(*ast.InterfaceType); ok && d.Name != nil {
				pkg, typeNameValue := declarationIdentity(d.InternalKey, d.Name.Value, program.Package, packages)
				methodNames := make([]string, 0, len(ifaceAST.Methods))
				var ctxIface *sema.InterfaceType
				if ctx != nil {
					ctxIface, _ = ctx.LookupInterface(d.Name.Value)
				}
				if ctxIface != nil {
					for _, method := range ctxIface.Methods {
						methodNames = append(methodNames, qualifyName(pkg+"."+typeNameValue, method.Name))
					}
				} else {
					for _, method := range ifaceAST.Methods {
						if method != nil && method.Name != nil {
							methodNames = append(methodNames, qualifyName(pkg+"."+typeNameValue, method.Name.Value))
						}
					}
				}
				sort.Strings(methodNames)
				result.TypeInterfaces = append(result.TypeInterfaces, Interface{
					Name:    qualifyName(pkg, typeNameValue),
					Methods: methodNames,
				})
			}
		}
	}

	result.Modules = modules.values()
	result.Functions = functions.values()
	result.Globals = globals.values()
	result.Locals = locals.values()
	sort.Slice(result.TypeStructs, func(i, j int) bool { return result.TypeStructs[i].Name < result.TypeStructs[j].Name })
	sort.Slice(result.TypeInterfaces, func(i, j int) bool { return result.TypeInterfaces[i].Name < result.TypeInterfaces[j].Name })
	for _, method := range methods {
		result.FixedReceivers = append(result.FixedReceivers, method)
	}
	sort.Slice(result.FixedReceivers, func(i, j int) bool {
		if result.FixedReceivers[i].Receiver == result.FixedReceivers[j].Receiver {
			return result.FixedReceivers[i].Name < result.FixedReceivers[j].Name
		}
		return result.FixedReceivers[i].Receiver < result.FixedReceivers[j].Receiver
	})
	return result
}

// WriteJSON writes a stable, human-readable symbol catalogue.
func WriteJSON(w io.Writer, program *ast.Program, ctx *sema.Context) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(Collect(program, ctx)); err != nil {
		return fmt.Errorf("encode symbols: %w", err)
	}
	return nil
}

type stringSet map[string]struct{}

func (s stringSet) add(value string) {
	if value != "" && value != "_" {
		s[value] = struct{}{}
	}
}

func (s stringSet) values() []string {
	values := make([]string, 0, len(s))
	for value := range s {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func addIdentifier(id *ast.Identifier, set stringSet) {
	if id != nil {
		set.add(id.Value)
	}
}

func qualifyName(pkg, name string) string {
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

func qualifyTypeName(pkg, name string) string {
	if strings.HasPrefix(name, "*") {
		return "*" + qualifyName(pkg, unmangleTypeName(pkg, strings.TrimPrefix(name, "*")))
	}
	return qualifyName(pkg, unmangleTypeName(pkg, name))
}

func qualifiedDeclName(internalKey, name, fallbackPackage string, packages map[string]struct{}) string {
	pkg, baseName := declarationIdentity(internalKey, name, fallbackPackage, packages)
	return qualifyName(pkg, baseName)
}

func declarationIdentity(internalKey, name, fallbackPackage string, packages map[string]struct{}) (string, string) {
	pkg := packageFromInternalKey(internalKey, fallbackPackage)
	baseName := name
	if inferred := packageForMangledName(name, fallbackPackage, packages); inferred != "" && inferred != fallbackPackage {
		pkg = inferred
		baseName = strings.TrimPrefix(name, inferred+"_")
	}
	internalPackage := packageFromInternalKey(internalKey, "")
	if slash := strings.IndexByte(internalKey, '/'); slash > 0 {
		keyName := internalKey[slash+1:]
		if at := strings.IndexAny(keyName, "@\\"); at >= 0 {
			keyName = keyName[:at]
		}
		if keyName != "" && internalPackage != "" && internalPackage == pkg && pkg != fallbackPackage {
			baseName = keyName
		}
	}
	return pkg, baseName
}

func unmangleTypeName(pkg, name string) string {
	return strings.TrimPrefix(name, pkg+"_")
}

func receiverPackage(receiver, fallback string, packages map[string]struct{}) string {
	name := strings.TrimPrefix(receiver, "*")
	if pkg := packageForMangledName(name, fallback, packages); pkg != "" {
		return pkg
	}
	return fallback
}

func packageFromInternalKey(internalKey, fallback string) string {
	if slash := strings.IndexByte(internalKey, '/'); slash > 0 {
		return internalKey[:slash]
	}
	return fallback
}

func packageForMangledName(name, fallback string, packages map[string]struct{}) string {
	best := ""
	for pkg := range packages {
		if pkg != "" && strings.HasPrefix(name, pkg+"_") && len(pkg) > len(best) {
			best = pkg
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

func declarationPackages(program *ast.Program) map[string]struct{} {
	packages := map[string]struct{}{}
	if program == nil {
		return packages
	}
	if program.Package != "" {
		packages[program.Package] = struct{}{}
	}
	for _, imp := range program.Imports {
		if imp == nil {
			continue
		}
		if imp.Alias != "" {
			packages[imp.Alias] = struct{}{}
		}
		path := strings.TrimRight(imp.Path, "/")
		if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
			path = path[slash+1:]
		}
		if path != "" {
			packages[path] = struct{}{}
		}
	}
	for _, decl := range program.Decls {
		var internalKey string
		switch d := decl.(type) {
		case *ast.TypeDecl:
			internalKey = d.InternalKey
		case *ast.FuncDecl:
			internalKey = d.InternalKey
		case *ast.CFuncDecl:
			internalKey = d.InternalKey
		case *ast.ExternFuncDecl:
			internalKey = d.InternalKey
		case *ast.JFuncDecl:
			internalKey = d.InternalKey
		}
		if pkg := packageFromInternalKey(internalKey, ""); pkg != "" {
			packages[pkg] = struct{}{}
		}
	}
	return packages
}

func collectParams(params []*ast.ParamDecl, locals stringSet) {
	for _, param := range params {
		if param != nil {
			addIdentifier(param.Name, locals)
		}
	}
}

func collectBlock(block *ast.BlockStmt, locals stringSet) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		collectStatement(stmt, locals)
	}
}

func collectStatement(stmt ast.Statement, locals stringSet) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if s.Token.Literal == "var" || s.Token.Literal == ":=" {
			for _, left := range s.Left {
				if id, ok := left.(*ast.Identifier); ok {
					addIdentifier(id, locals)
				}
			}
		}
		for _, right := range s.Right {
			collectExpression(right, locals)
		}
	case *ast.BlockStmt:
		collectBlock(s, locals)
	case *ast.IfStmt:
		collectStatement(s.Init, locals)
		collectExpression(s.Condition, locals)
		collectBlock(s.Consequence, locals)
		collectStatement(s.Alternative, locals)
	case *ast.ForStmt:
		collectStatement(s.Init, locals)
		collectExpression(s.Cond, locals)
		collectStatement(s.Post, locals)
		collectBlock(s.Body, locals)
	case *ast.ForRangeStmt:
		if id, ok := s.Key.(*ast.Identifier); ok {
			addIdentifier(id, locals)
		}
		if id, ok := s.Value.(*ast.Identifier); ok {
			addIdentifier(id, locals)
		}
		collectExpression(s.X, locals)
		collectBlock(s.Body, locals)
	case *ast.SwitchStmt:
		collectStatement(s.Init, locals)
		collectExpression(s.Value, locals)
		for _, clause := range s.Cases {
			if clause != nil {
				for _, value := range clause.Values {
					collectExpression(value, locals)
				}
				for _, bodyStmt := range clause.Body {
					collectStatement(bodyStmt, locals)
				}
			}
		}
	case *ast.TypeSwitchStmt:
		addIdentifier(s.Variable, locals)
		collectStatement(s.Init, locals)
		collectExpression(s.Expr, locals)
		for _, clause := range s.Cases {
			if clause != nil {
				for _, bodyStmt := range clause.Body {
					collectStatement(bodyStmt, locals)
				}
			}
		}
	case *ast.ExprStmt:
		collectExpression(s.Expr, locals)
	case *ast.ReturnStmt:
		for _, value := range s.Values {
			collectExpression(value, locals)
		}
	case *ast.DeferStmt:
		collectExpression(s.Call, locals)
	case *ast.LockStmt:
		collectBlock(s.Body, locals)
	case *ast.AreaStmt:
		collectExpression(s.Size, locals)
		collectBlock(s.Body, locals)
	case *ast.SendStmt:
		collectExpression(s.Chan, locals)
		collectExpression(s.Value, locals)
	}
}

func collectExpression(expr ast.Expression, locals stringSet) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		collectExpression(e.Function, locals)
		for _, arg := range e.Args {
			collectExpression(arg, locals)
		}
	case *ast.FuncLit:
		collectParams(e.Params, locals)
		collectBlock(e.Body, locals)
	case *ast.PrefixExpr:
		collectExpression(e.Right, locals)
	case *ast.ReceiveExpr:
		collectExpression(e.Expr, locals)
	case *ast.AsyncExpr:
		collectExpression(e.Fn, locals)
	case *ast.BinaryExpr:
		collectExpression(e.Left, locals)
		collectExpression(e.Right, locals)
	case *ast.IndexExpr:
		collectExpression(e.Left, locals)
		collectExpression(e.Index, locals)
	case *ast.MemberExpr:
		collectExpression(e.Object, locals)
	case *ast.GenericInstExpr:
		collectExpression(e.Left, locals)
	case *ast.TypeAssertExpr:
		collectExpression(e.Expr, locals)
	case *ast.SliceExpr:
		collectExpression(e.Left, locals)
		collectExpression(e.Low, locals)
		collectExpression(e.High, locals)
	case *ast.SliceLiteral:
		for _, element := range e.Elements {
			collectExpression(element, locals)
		}
	case *ast.ArrayLiteral:
		for _, element := range e.Elements {
			collectExpression(element, locals)
		}
	case *ast.StructLiteral:
		for _, field := range e.Fields {
			if field != nil {
				collectExpression(field.Value, locals)
			}
		}
	case *ast.MapLiteral:
		for _, entry := range e.Entries {
			if entry != nil {
				collectExpression(entry.Key, locals)
				collectExpression(entry.Value, locals)
			}
		}
	case *ast.InlineAsmExpr:
		for _, operand := range e.Operands {
			collectExpression(operand, locals)
		}
	}
}

func typeName(typ ast.TypeExpr) string {
	switch t := typ.(type) {
	case *ast.NamedType:
		name := ""
		if t.Package != nil {
			name += t.Package.Value + "."
		}
		if t.Name != nil {
			name += t.Name.Value
		}
		return name
	case *ast.PointerType:
		return "*" + typeName(t.Base)
	case *ast.SliceType:
		return "[]" + typeName(t.Elem)
	case *ast.ArrayType:
		return fmt.Sprintf("[%d]%s", t.Len, typeName(t.Elem))
	case *ast.MapType:
		return "map[" + typeName(t.Key) + "]" + typeName(t.Value)
	case *ast.ChanType:
		return "chan " + typeName(t.Elem)
	case *ast.EllipsisType:
		return "..." + typeName(t.Elem)
	default:
		return strings.TrimSpace(fmt.Sprintf("%T", typ))
	}
}

func isFixedReceiverMethod(fn *ast.FuncDecl, ctx *sema.Context) bool {
	if fn == nil || fn.Receiver == nil || fn.Name == nil || ctx == nil {
		return false
	}

	receiverName := typeName(fn.Receiver.Type)
	method, _ := ctx.LookupMethod(receiverName, fn.Name.Value)
	if method == nil {
		return false
	}

	// A fixed receiver is emitted only when the receiver satisfies an
	// interface containing this method. This deliberately excludes ordinary
	// methods that do not participate in interface dispatch.
	receiverType := ctx.ResolveType(fn.Receiver.Type)
	seenInterfaces := make(map[*sema.InterfaceType]bool)
	for _, iface := range ctx.Interfaces {
		if iface == nil || iface.IsAny() || seenInterfaces[iface] {
			continue
		}
		seenInterfaces[iface] = true
		if !ctx.Implements(receiverType, iface) {
			continue
		}
		if _, index := iface.GetMethod(fn.Name.Value); index >= 0 {
			return true
		}
	}

	// Built-in capabilities are interface-like contracts maintained by the
	// semantic checker rather than entries in Context.Interfaces. Include the
	// method registered for a satisfied built-in capability as well.
	baseName := strings.TrimPrefix(receiverName, "*")
	if st, _ := ctx.LookupStruct(baseName); st != nil {
		for _, builtin := range st.BuiltinCapabilities {
			if builtin == method || (builtin != nil && builtin.InternalKey == method.InternalKey) {
				return true
			}
		}
	}
	return false
}
