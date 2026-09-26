package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/logger"
	"hikec-go/pkg/sema"
)

// CallLowerer は関数呼び出し、メソッド解決、型キャスト判定、引数パッキングを担当する

func (c *CallLowerer) LowerCall(call *ast.CallExpr) hir.Value {
	if call != nil {
		restoreLocation := c.root.setTokenLocation(c.root.sourceFile, call.Token)
		defer restoreLocation()
	}
	logger.LogVerbose2("[Verbose2] Lower call input: function=%T (%+v) args=%d\\n", call.Function, call.Function, len(call.Args))
	// 0. ジェネリクス関数の明示的型引数適用呼び出し (例: Add[float64](a, b))
	if genInst, ok := call.Function.(*ast.GenericInstExpr); ok {
		var baseName string
		if id, okId := genInst.Left.(*ast.Identifier); okId {
			baseName = astIDValue(id)
		} else if mem, okMem := genInst.Left.(*ast.MemberExpr); okMem {
			if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
				baseName = pkgId.Value + "_" + mem.Field.Value
			} else {
				baseName = mem.Field.Value
			}
		}

		if baseName != "" {
			logger.LogVerbose2("[Verbose2] Lower generic call: base=%s typeArgs=%v\\n", baseName, genInst.TypeArgs)
			typeArgs := make([]sema.Type, len(genInst.TypeArgs))
			for i, ta := range genInst.TypeArgs {
				typeArgs[i] = c.root.semaCtx.ResolveType(ta)
			}

			specName, specFn := c.getOrSpecializeFunc(baseName, typeArgs)
			if specFn != nil {
				logger.LogVerbose2("[Verbose2] Lower generic call resolved: callee=%s returnTypes=%v\\n", specName, specFn.ReturnTypes)
				callArgs := c.fillDefaultArgs(call.Args, c.getFuncParams(specFn, specName))
				isCVarArg := semaFuncCFunc(specFn) || semaFuncExtern(specFn) || (semaFuncIsVariadic(specFn) && semaFuncVariadicElem(specFn) == nil)
				args := c.lowerArgs(callArgs, specFn.ParamTypes, semaFuncIsVariadic(specFn), isCVarArg, semaFuncVariadicElem(specFn), call.HasEllipsis)

				var retType sema.Type = sema.TypeVoid
				if len(specFn.ReturnTypes) == 1 {
					retType = specFn.ReturnTypes[0]
				} else if len(specFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: specFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = c.root.nextReg(retType)
				}
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: specName, Args: args})
				return dst
			}
		}
	}

	// 1. 型キャスト呼び出し (例: Duration(ns), int64(x), string(cs), cstring(s), uint('a'))
	if len(call.Args) == 1 {
		targetType := c.ResolveTypeFromExpr(call.Function)
		if targetType != nil && targetType != sema.TypeVoid {
			if _, isFunc := targetType.(*sema.FuncType); !isFunc {
				if cl, ok := call.Args[0].(*ast.CharLiteral); ok {
					intRank := func(llvm string) int {
						switch llvm {
						case "i64":
							return 64
						case "i32":
							return 32
						case "i16":
							return 16
						case "i8":
							return 8
						default:
							return 0
						}
					}
					if intRank(sema.LLVMTypeOf(targetType)) > 0 {
						return &hir.ConstInt{Val: int64(cl.CodePoint), Typ: targetType}
					}
				}

				argVal := c.root.Expr.LowerExpr(call.Args[0])

				if (argVal.Type() == sema.TypeString || semaTypeName(argVal.Type()) == "string") && targetType == sema.TypeCString {
					return c.lowerStringToCString(argVal)
				}

				if (argVal.Type() == sema.TypeCString || semaTypeName(argVal.Type()) == "cstring") && targetType == sema.TypeString {
					return c.lowerCStringToString(argVal)
				}

				if _, isSlice := argVal.Type().(*sema.SliceType); isSlice && targetType == sema.TypeString {
					rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					rawLen := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
					c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
					// A Hike string is a pointer/offset/length view.  The slice
					// backing store already has the exact pointer and length needed
					// for that view; copying through __hike_slice_to_str here used
					// the C-string allocation path and could leave the generated
					// native image with an invalid backing pointer.  C-string
					// consumers still receive a terminated copy in
					// lowerStringToCString.
					return c.root.makeString(rawPtr, rawLen)
				}

				if sema.LLVMTypeOf(argVal.Type()) == sema.LLVMTypeOf(targetType) && argVal.Type() == targetType {
					return argVal
				}
				if c.root.semaCtx.GoHikeMode {
					switch argVal.Type().(type) {
					case *sema.StructType, *sema.ArrayType, *sema.SliceType, *sema.TupleType:
						// Go-Hike's compatibility layer does not define aggregate-to-
						// scalar casts. Keep compiler metadata conversions harmless.
						return c.root.defaultConstValue(targetType)
					}
				}

				dst := c.root.nextReg(targetType)
				c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
				return dst
			}
		}
	}

	return c.lowerCallRemainder(call)
}

// LowerCall's remaining dispatch paths live in a separate helper to keep the entry point small.
func (c *CallLowerer) lowerCallRemainder(call *ast.CallExpr) hir.Value {
	// 2. 言語組み込み関数 (make, close, delete, len, cap, append, string, cstring, )
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		switch fnId.Value {
		case "panic":
			if len(call.Args) != 1 && len(call.Args) != 2 {
				panic(fmt.Sprintf("[Lower Error] panic expects one value or a message and cause, got %d arguments", len(call.Args)))
			}
			// Preserve the dynamic operand evaluation now. The panic record and
			// defer unwinding are added by the next lowering phase; the source
			// site is already stable in HIR for both backends.
			var value hir.Value = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
			if len(call.Args) > 0 {
				value = c.root.Expr.LowerExpr(call.Args[0])
			}
			panicType := &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
			value = c.root.emitValueCoerce(value, panicType)
			var cause hir.Value
			if len(call.Args) == 2 {
				cause = c.root.Expr.LowerExpr(call.Args[1])
				cause = c.root.emitValueCoerce(cause, panicType)
			}
			siteID := c.root.registerPanicSite()
			// Store the boxed value before running deferred calls so a deferred
			// recover() observes the panic that caused the unwind.
			c.root.emit(&hir.InstrCallStatic{
				CalleeName: "__hike_panic_set",
				Args: []hir.Value{
					value,
					&hir.ConstZero{Typ: panicType},
					&hir.ConstInt{Val: int64(siteID), Typ: sema.TypeInt32},
				},
			})
			for i := len(c.root.deferStack) - 1; i >= 0; i-- {
				c.LowerCall(c.root.deferStack[i])
			}
			c.root.terminate(&hir.InstrPanic{Value: value, Cause: cause, SiteID: siteID})
			return nil

		case "make":
			return c.lowerMakeCall(call)

		case "copy":
			// The Go-Hike self-hosting path currently uses copy only while
			// rearranging compiler-owned slices. Keep the intrinsic lowering
			// side-effect free until the slice memmove ABI is available in both
			// backends.
			return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}

		case "sizeof":
			return c.lowerSizeofCall(call)

		case "recover":
			panicType := &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
			dst := c.root.nextReg(panicType, "recover")
			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_panic_get"})
			return dst

		case "recover_cause":
			panicType := &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
			dst := c.root.nextReg(panicType, "recover_cause")
			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_panic_cause"})
			return dst

		case "recover_site":
			dst := c.root.nextReg(sema.TypeInt, "recover_site")
			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_panic_site"})
			return dst

		case "close":
			if len(call.Args) > 0 {
				chVal := c.root.Expr.LowerExpr(call.Args[0])
				c.root.emit(&hir.InstrChanClose{Chan: chVal})
				return nil
			}

		case "delete":
			argVal := c.root.Expr.LowerExpr(call.Args[0])
			keyVal := c.root.Expr.LowerExpr(call.Args[1])
			if mp, isMap := argVal.Type().(*sema.MapType); isMap {
				keyI64 := c.root.coerceToI64(keyVal, mp.Key)
				c.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_delete", Args: []hir.Value{argVal, keyI64}})
				return nil
			}

		case "len", "cap":
			argVal := c.root.Expr.LowerExpr(call.Args[0])
			if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
				idx := 1
				if fnId.Value == "cap" {
					idx = 2
				}
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: idx})
				return dst
			}
			if fnId.Value == "len" && (argVal.Type() == sema.TypeString || argVal.Type() == sema.TypeCString) {
				if argVal.Type() == sema.TypeString {
					_, length := c.root.stringParts(argVal)
					return length
				}
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: c.root.BuiltinName("strlen"), Args: []hir.Value{argVal}})
				return dst
			}
			if fnId.Value == "len" {
				if ptr, ok := argVal.Type().(*sema.PointerType); ok && ptr.Base == sema.TypeByte {
					dst := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: c.root.BuiltinName("strlen"), Args: []hir.Value{argVal}})
					return dst
				}
			}
			if _, isMap := argVal.Type().(*sema.MapType); isMap {
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_map_len", Args: []hir.Value{argVal}})
				return dst
			}

		case "append":
			return c.LowerAppend(call)

		case "string":
			if value, handled := c.lowerStringBuiltin(call); handled {
				return value
			}

		case "deepcopy":
			if value, handled := c.lowerDeepCopyBuiltin(call); handled {
				return value
			}

		case "cstring":
			if value, handled := c.lowerCStringBuiltin(call); handled {
				return value
			}
		}
	}

	// 3. メンバー式経由の呼び出し (パッケージ関数呼び出し、またはオブジェクトメソッド呼び出し)
	if mem, ok := call.Function.(*ast.MemberExpr); ok {
		// 3A. パッケージ名修飾による関数呼び出し (例: fmt.Printf, time.Now, japanese.NewShiftJIS)
		if pkgIdent, isIdent := mem.Object.(*ast.Identifier); isIdent {
			isPackage := c.isPackageName(pkgIdent.Value)
			if !isPackage {
				fn, _ := c.root.semaCtx.LookupFunction(pkgIdent.Value + "_" + mem.Field.Value)
				isPackage = fn != nil
			}
			if isPackage {
				return c.lowerPackageMemberCall(call, mem, pkgIdent)
			}
		}

		// 3B. インターフェースまたは構造体/基本型のメソッド呼び出し
		objPtr := c.root.Expr.LowerStructPtr(mem.Object)
		objType := objPtr.Type().(*sema.PointerType).Base

		// インターフェースメソッド呼び出し (動的ディスパッチ)
		if iface, isIface := objType.(*sema.InterfaceType); isIface && !iface.IsAny() {
			return c.lowerInterfaceMethodCall(call, mem, objPtr, iface)
		}

		// 静的型メソッド呼び出し (構造体および基本型エイリアス)
		targetFnName, targetFn, finalRecv, found := c.ResolveMethod(objType, mem.Field.Value, objPtr)
		if found && targetFn != nil {
			isPtrRecv := false
			if len(targetFn.ParamTypes) > 0 {
				_, isPtrRecv = targetFn.ParamTypes[0].(*sema.PointerType)
			}

			recvArg := finalRecv
			if !isPtrRecv {
				if ptrType, ok := finalRecv.Type().(*sema.PointerType); ok {
					loaded := c.root.nextReg(ptrType.Base)
					c.root.emit(&hir.InstrLoad{Dst: loaded, Ptr: finalRecv})
					recvArg = loaded
				}
			} else {
				if _, ok := finalRecv.Type().(*sema.PointerType); !ok {
					allocaTmp := c.root.nextReg(&sema.PointerType{Base: finalRecv.Type()})
					c.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: finalRecv.Type()})
					c.root.emit(&hir.InstrStore{Val: finalRecv, Ptr: allocaTmp})
					recvArg = allocaTmp
				}
			}

			params := c.getFuncParams(targetFn, targetFnName)
			callArgs := c.fillDefaultArgs(call.Args, params)

			methodParamTypes := []sema.Type{}
			if len(targetFn.ParamTypes) > 1 {
				methodParamTypes = targetFn.ParamTypes[1:]
			}
			isCVarArg := semaFuncCFunc(targetFn) || semaFuncExtern(targetFn) || (semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil)
			callArgVals := c.lowerArgs(callArgs, methodParamTypes, semaFuncIsVariadic(targetFn), isCVarArg, semaFuncVariadicElem(targetFn), call.HasEllipsis)
			args := append([]hir.Value{recvArg}, callArgVals...)

			var retType sema.Type = sema.TypeVoid
			if len(targetFn.ReturnTypes) == 1 {
				retType = targetFn.ReturnTypes[0]
			} else if len(targetFn.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: targetFn.ReturnTypes}
			}

			var dst *hir.Reg = nil
			if retType != sema.TypeVoid {
				dst = c.root.nextReg(retType)
			}
			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: targetFnName, Args: args})
			return dst
		}

		// 3C. 構造体フィールドに関数ポインタが格納されている場合
		st, sName := c.root.findStruct(objType)
		if st != nil {
			fieldPtr, fieldType, _, fieldFound := c.root.Expr.ResolveFieldPath(st, sName, objPtr, mem.Field.Value)
			if fieldFound {
				if ft, ok := fieldType.(*sema.FuncType); ok {
					loadedFn := c.root.nextReg(ft)
					c.root.emit(&hir.InstrLoad{Dst: loadedFn, Ptr: fieldPtr})
					return c.lowerIndirectCall(loadedFn, call.Args, call.HasEllipsis)
				}
			}
		}

		file := c.root.sourceFile
		if file == "" {
			file = "input.hike"
		}
		panic(fmt.Sprintf("%s:%d:%d: method or function field '%s' not found on type '%s'", file, mem.Field.Token.Line, mem.Field.Token.Col, mem.Field.Value, semaTypeName(objType)))
	}

	// 4. 単一識別子によるトップレベル関数呼び出し (例: myFunc())
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		_, isLocal := c.root.symbols[astIDValue(fnId)]
		_, isGlobal := c.root.semaCtx.Globals[astIDValue(fnId)]
		if !isLocal && !isGlobal {
			// パッケージ内の未修飾関数呼び出しは、現在の関数名から
			// パッケージ接頭辞を補って先に完全修飾名で解決する。
			// LookupFunction のサフィックス検索だけに任せると、同名の
			// md5_compress / sha256_compress がmapの反復順で入れ替わる。
			lookupName := astIDValue(fnId)
			var targetFn *sema.FuncType
			var canonicalName string
			if c.root.curFunc != nil {
				if sep := strings.IndexByte(c.root.curFunc.Name, '_'); sep > 0 {
					qualified := c.root.curFunc.Name[:sep] + "_" + astIDValue(fnId)
					targetFn, canonicalName = c.root.semaCtx.LookupFunction(qualified)
				}
			}
			if targetFn == nil {
				targetFn, canonicalName = c.root.semaCtx.LookupFunction(lookupName)
			}
			if targetFn != nil {
				params := c.getFuncParams(targetFn, canonicalName)
				callArgs := c.fillDefaultArgs(call.Args, params)

				if semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil {
					if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
						return c.inlineVariadicCall(fnDecl, &ast.CallExpr{
							Token:       call.Token,
							Function:    call.Function,
							Args:        callArgs,
							HasEllipsis: call.HasEllipsis,
						})
					}
				}

				isCVarArg := semaFuncCFunc(targetFn) || semaFuncExtern(targetFn) || (semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil)
				args := c.lowerArgs(callArgs, targetFn.ParamTypes, semaFuncIsVariadic(targetFn), isCVarArg, semaFuncVariadicElem(targetFn), call.HasEllipsis)

				var retType sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = c.root.nextReg(retType)
				}
				callee := canonicalName
				if semaFuncCFunc(targetFn) {
					if semaFuncCFuncAst(targetFn) != nil && !semaFuncCFuncAst(targetFn).IsAlias() {
						callee = "__hike_impl_" + semaFuncName(targetFn)
					} else if semaFuncCFuncTarget(targetFn) != "" {
						callee = semaFuncCFuncTarget(targetFn)
					} else {
						callee = "c_" + canonicalName
					}
				} else if semaFuncExtern(targetFn) {
					if semaFuncIRName(targetFn) != "" {
						callee = semaFuncIRName(targetFn)
					}
				}
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: callee, Args: args})
				return dst
			}
		}
	}

	// 5. 間接関数ポインタ / クロージャ呼び出し (例: act())
	fnFatPtr := c.root.Expr.LowerExpr(call.Function)
	return c.lowerIndirectCall(fnFatPtr, call.Args, call.HasEllipsis)
}

func (c *CallLowerer) isPackageName(name string) bool {
	for _, imp := range c.root.prog.Imports {
		pkgName := imp.Alias
		if pkgName == "" {
			pkgName = strings.Trim(imp.Path, "\"`")
		}
		if idx := strings.LastIndex(pkgName, "/"); idx != -1 {
			pkgName = pkgName[idx+1:]
		}
		if pkgName == name {
			return true
		}
	}
	return false
}

func (c *CallLowerer) lowerIndirectCall(fnFatPtr hir.Value, callArgs []ast.Expression, hasEllipsis bool) hir.Value {
	fnPtrReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	envPtrReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrExtractValue{Dst: fnPtrReg, Agg: fnFatPtr, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: envPtrReg, Agg: fnFatPtr, Index: 1})

	ft, _ := fnFatPtr.Type().(*sema.FuncType)
	var isVariadic bool
	var variadicElem sema.Type
	var paramTypes []sema.Type
	if ft != nil {
		isVariadic = ft.IsVariadic
		variadicElem = semaFuncVariadicElem(ft)
		paramTypes = ft.ParamTypes
	}
	args := c.lowerArgs(callArgs, paramTypes, isVariadic, false, variadicElem, hasEllipsis)

	var retType sema.Type = sema.TypeVoid
	if ft != nil {
		if len(ft.ReturnTypes) == 1 {
			retType = ft.ReturnTypes[0]
		} else if len(ft.ReturnTypes) > 1 {
			retType = &sema.TupleType{Types: ft.ReturnTypes}
		}
	}

	var dst *hir.Reg = nil
	if retType != sema.TypeVoid {
		dst = c.root.nextReg(retType)
	}
	c.root.emit(&hir.InstrCallIndirect{
		Dst:    dst,
		FnPtr:  fnPtrReg,
		EnvPtr: envPtrReg,
		Args:   args,
	})
	return dst
}

// -------------------------------------------------------------
// スライス追加 (Append)
// -------------------------------------------------------------

func (c *CallLowerer) LowerAppend(call *ast.CallExpr) hir.Value {
	sliceVal := c.root.Expr.LowerExpr(call.Args[0])
	slType := sliceVal.Type().(*sema.SliceType)
	if call.HasEllipsis && len(call.Args) == 2 {
		srcVal := c.root.Expr.LowerExpr(call.Args[1])
		if srcType, ok := srcVal.Type().(*sema.SliceType); ok {
			return c.lowerAppendSlice(sliceVal, slType, srcVal, srcType)
		}
	}
	elemSize := sema.SizeOf(slType.Elem)
	if elemSize <= 0 {
		elemSize = 1
	}

	oldRawBytePtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	oldLen := c.root.nextReg(sema.TypeInt)
	oldCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrExtractValue{Dst: oldRawBytePtr, Agg: sliceVal, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: oldLen, Agg: sliceVal, Index: 1})
	c.root.emit(&hir.InstrExtractValue{Dst: oldCap, Agg: sliceVal, Index: 2})

	oldTypedPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	c.root.emit(&hir.InstrCast{Dst: oldTypedPtr, Val: oldRawBytePtr, ToType: &sema.PointerType{Base: slType.Elem}})

	numElems := len(call.Args) - 1
	reqCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: reqCap, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(numElems), Typ: sema.TypeInt}})

	growCond := c.root.nextReg(sema.TypeBool)
	c.root.emit(&hir.InstrBinary{Dst: growCond, Op: hir.OpGt, L: reqCap, R: oldCap})

	growBB := c.root.newBlock("append.grow")
	noGrowBB := c.root.newBlock("append.nogrow")
	storeBB := c.root.newBlock("append.store")

	finalPtrAlloca := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: slType.Elem}}, "finalPtr")
	finalCapAlloca := c.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "finalCap")
	c.root.emit(&hir.InstrAlloca{Dst: finalPtrAlloca, AllocType: &sema.PointerType{Base: slType.Elem}})
	c.root.emit(&hir.InstrAlloca{Dst: finalCapAlloca, AllocType: sema.TypeInt})
	var structuredAppend *hir.IfNode
	if len(c.root.structuredStack) > 0 {
		structuredAppend = &hir.IfNode{Label: growBB.Label, Cond: growCond}
		c.root.appendStructuredNode(structuredAppend)
	}

	c.root.terminate(&hir.InstrBranch{Cond: growCond, ThenTarget: growBB.Label, ElseTarget: noGrowBB.Label})

	c.root.setBlock(noGrowBB)
	if structuredAppend != nil {
		c.root.pushStructuredFrame(structuredAppend.Label)
		c.root.pushStructuredBody(&structuredAppend.Else)
	}
	c.root.emit(&hir.InstrStore{Val: oldTypedPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrStore{Val: oldCap, Ptr: finalCapAlloca})
	if structuredAppend != nil {
		c.root.popStructuredBody()
		c.root.popStructuredFrame()
	}
	c.root.terminate(&hir.InstrJump{Target: storeBB.Label})

	c.root.setBlock(growBB)
	if structuredAppend != nil {
		c.root.pushStructuredFrame(structuredAppend.Label)
		c.root.pushStructuredBody(&structuredAppend.Then)
	}
	doubleCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: doubleCap, Op: hir.OpMul, L: oldCap, R: &hir.ConstInt{Val: 2, Typ: sema.TypeInt}})
	newCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: newCap, Op: hir.OpAdd, L: doubleCap, R: reqCap})
	newBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: newBytes, Op: hir.OpMul, L: newCap, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	newRawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: newRawPtr, Size: newBytes, AllocType: sema.TypeByte, KeepOnHeapInArea: true})

	newTypedPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	c.root.emit(&hir.InstrCast{Dst: newTypedPtr, Val: newRawPtr, ToType: &sema.PointerType{Base: slType.Elem}})

	oldBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: oldBytes, Op: hir.OpMul, L: oldLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	memcpyTmp := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: memcpyTmp, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{newRawPtr, oldRawBytePtr, oldBytes}})

	c.root.emit(&hir.InstrStore{Val: newTypedPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrStore{Val: newCap, Ptr: finalCapAlloca})
	if structuredAppend != nil {
		c.root.popStructuredBody()
		c.root.popStructuredFrame()
	}
	c.root.terminate(&hir.InstrJump{Target: storeBB.Label})

	c.root.setBlock(storeBB)
	resPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	resCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrLoad{Dst: resPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrLoad{Dst: resCap, Ptr: finalCapAlloca})

	for i := 0; i < numElems; i++ {
		elVal := c.root.Expr.LowerExpr(call.Args[1+i])
		elVal = c.root.emitValueCoerce(elVal, slType.Elem)
		offsetIdx := c.root.nextReg(sema.TypeInt)
		c.root.emit(&hir.InstrBinary{Dst: offsetIdx, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
		destPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
		c.root.emit(&hir.InstrGetElemPtr{Dst: destPtr, BasePtr: resPtr, Index: offsetIdx})
		c.root.emit(&hir.InstrStore{Val: elVal, Ptr: destPtr})
	}

	resBytePtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCast{Dst: resBytePtr, Val: resPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

	t1 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(slType), Val: resBytePtr, Index: 0})
	t2 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: reqCap, Index: 1})
	t3 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: resCap, Index: 2})
	return t3
}

// lowerAppendSlice handles append(dst, src...) without treating src itself as
// one element. Both slices use the same fat-pointer layout, so the elements
// can be copied in one operation after allocating the combined backing store.
func (c *CallLowerer) lowerAppendSlice(dst hir.Value, dstType *sema.SliceType, src hir.Value, srcType *sema.SliceType) hir.Value {
	if semaTypeName(dstType.Elem) != semaTypeName(srcType.Elem) {
		return dst
	}
	elemSize := sema.SizeOf(dstType.Elem)
	if elemSize <= 0 {
		elemSize = 1
	}
	oldPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	oldLen := c.root.nextReg(sema.TypeInt)
	srcPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	srcLen := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrExtractValue{Dst: oldPtr, Agg: dst, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: oldLen, Agg: dst, Index: 1})
	c.root.emit(&hir.InstrExtractValue{Dst: srcPtr, Agg: src, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: srcLen, Agg: src, Index: 1})
	totalLen := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: totalLen, Op: hir.OpAdd, L: oldLen, R: srcLen})
	totalBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: totalBytes, Op: hir.OpMul, L: totalLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	raw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: raw, Size: totalBytes, AllocType: sema.TypeByte, KeepOnHeapInArea: true})
	oldBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: oldBytes, Op: hir.OpMul, L: oldLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	srcBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: srcBytes, Op: hir.OpMul, L: srcLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	copyDst := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: copyDst, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{raw, oldPtr, oldBytes}})
	appendPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrGetElemPtr{Dst: appendPtr, BasePtr: raw, Index: oldBytes})
	copySrc := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: copySrc, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{appendPtr, srcPtr, srcBytes}})
	t1 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(dstType), Val: raw, Index: 0})
	t2 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: totalLen, Index: 1})
	t3 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: totalLen, Index: 2})
	return t3
}

// -------------------------------------------------------------
// itab 生成
// -------------------------------------------------------------

func (c *CallLowerer) GetOrCreateItab(concreteType sema.Type, iface *sema.InterfaceType) *hir.ItabDef {
	sName := strings.TrimPrefix(semaTypeName(concreteType), "*")
	sName = strings.ReplaceAll(sName, ".", "_")
	sName = strings.ReplaceAll(sName, "[", "_")
	sName = strings.ReplaceAll(sName, "]", "_")
	sName = strings.ReplaceAll(sName, ",", "_")
	sName = strings.ReplaceAll(sName, "*", "ptr")
	ifName := iface.Name
	if ifName == "" {
		ifName = "anon_iface"
	}
	ifName = strings.ReplaceAll(ifName, ".", "_")
	key := fmt.Sprintf("%s_%s", sName, ifName)
	if existing, ok := c.root.itabs[key]; ok {
		return existing
	}

	typeID := concreteType.TypeID(c.root.semaCtx)
	globalName := fmt.Sprintf("__itab_%s_%s", sName, ifName)
	itabStructName := fmt.Sprintf("__itab_%s", ifName)

	methods := []hir.ItabMethodEntry{}

	for _, m := range iface.Methods {
		targetFnName, fnMeta, _, found := c.ResolveMethod(concreteType, m.Name, nil)
		if !found && !strings.HasPrefix(semaTypeName(concreteType), "*") {
			targetFnName, fnMeta, _, found = c.ResolveMethod(&sema.PointerType{Base: concreteType}, m.Name, nil)
		}
		if found && fnMeta != nil {
			targetFnName = semaFuncIRName(fnMeta)
			if targetFnName == "" {
				targetFnName = semaFuncName(fnMeta)
			}
		} else if !found {
			targetFnName = sema.CanonicalMethodName(sName, m.Name)
		}
		methods = append(methods, hir.ItabMethodEntry{
			MethodName:   m.Name,
			TargetFnName: targetFnName,
			MethodType:   m,
		})
	}

	def := &hir.ItabDef{
		GlobalName:     globalName,
		ConcreteType:   concreteType,
		InterfaceType:  iface,
		TypeID:         typeID,
		ItabStructName: itabStructName,
		Methods:        methods,
	}
	c.root.itabs[key] = def
	c.root.hirProg.Itabs = append(c.root.hirProg.Itabs, def)
	return def
}
