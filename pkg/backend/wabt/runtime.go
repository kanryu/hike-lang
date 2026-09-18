package wabt

import "hikec-go/pkg/hir"

// runtimeFunc is deliberately kept as WAT rather than generated from LLVM IR.
// WAT has no `internal`/`noinline` attribute: reachability is therefore the
// useful equivalent of LLVM's internal linkage for this backend.
type runtimeFunc struct {
	deps []string
	body string
}

// WabtRuntimeSymbols lists names implemented by the WAT runtime. Keeping the
// catalogue beside the runtime definitions makes symbol reservation follow the
// same ownership model as the LLVM backend.
var WabtRuntimeSymbols = map[string]bool{
	"malloc": true, "calloc": true, "free": true,
	"memcpy": true, "memcmp": true, "strlen": true, "strcmp": true,
	"memcpy32": true, "memcmp32": true, "strlen32": true, "strcmp32": true,
	"__hike_region_begin": true, "__hike_region_alloc": true, "__hike_region_end": true,
	"__hike_region_begin32": true, "__hike_region_alloc32": true, "__hike_region_end32": true,
}

func IsWabtRuntimeSymbol(name string) bool {
	for symbol := range WabtRuntimeSymbols {
		if symbol == name {
			return true
		}
	}
	return false
}

var wasmRuntime = map[string]runtimeFunc{
	"malloc": runtimeFunc{body: `(func $malloc (param $n i32) (result i32)
    (local $p i32) (local $next i32) (local $pages i32)
    (local.set $p (global.get $__heap))
    (local.set $next (i32.add (local.get $p) (i32.and (i32.add (local.get $n) (i32.const 7)) (i32.const -8))))
    (if (i32.gt_u (local.get $next) (i32.mul (memory.size) (i32.const 65536)))
      (then
        (local.set $pages (i32.div_u (i32.add (i32.sub (local.get $next) (i32.mul (memory.size) (i32.const 65536))) (i32.const 65535)) (i32.const 65536)))
        (drop (memory.grow (local.get $pages)))))
    (global.set $__heap (local.get $next))
    (local.get $p))`},
	"calloc": runtimeFunc{deps: []string{"malloc"}, body: `(func $calloc (param $count i32) (param $size i32) (result i32)
    (local $p i32) (local $n i32)
    (local.set $n (i32.mul (local.get $count) (local.get $size)))
    (local.set $p (call $malloc (local.get $n)))
    (memory.fill (local.get $p) (i32.const 0) (local.get $n))
    (local.get $p))`},
	"free": runtimeFunc{body: `(func $free (param $p i32))`},
	"memcpy32": runtimeFunc{body: `(func $memcpy32 (param $dst i32) (param $src i32) (param $n i32) (result i32)
    (memory.copy (local.get $dst) (local.get $src) (local.get $n))
    (local.get $dst))`},
	"memcmp32": runtimeFunc{body: `(func $memcmp32 (param $a i32) (param $b i32) (param $n i32) (result i32)
    (local $i i32) (local $x i32) (local $y i32)
    (block $done (loop $loop
      (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
      (local.set $x (i32.load8_u (local.get $a)))
      (local.set $y (i32.load8_u (local.get $b)))
      (br_if $done (i32.ne (local.get $x) (local.get $y)))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (local.set $a (i32.add (local.get $a) (i32.const 1)))
      (local.set $b (i32.add (local.get $b) (i32.const 1)))
      (br $loop)))
    (i32.sub (local.get $x) (local.get $y)))`},
	"strlen32": runtimeFunc{body: `(func $strlen32 (param $s i32) (result i32)
    (local $p i32)
    (local.set $p (local.get $s))
    (block $done (loop $loop
      (br_if $done (i32.eqz (i32.load8_u (local.get $p))))
      (local.set $p (i32.add (local.get $p) (i32.const 1)))
      (br $loop)))
    (i32.sub (local.get $p) (local.get $s)))`},
	"strcmp32": runtimeFunc{deps: []string{"strlen32", "memcmp32"}, body: `(func $strcmp32 (param $a i32) (param $b i32) (result i32)
    (local $la i32) (local $lb i32) (local $n i32) (local $r i32)
    (local.set $la (call $strlen32 (local.get $a)))
    (local.set $lb (call $strlen32 (local.get $b)))
    (local.set $n (select (local.get $la) (local.get $lb) (i32.lt_u (local.get $la) (local.get $lb))))
    (local.set $r (call $memcmp32 (local.get $a) (local.get $b) (local.get $n)))
    (if (result i32) (i32.ne (local.get $r) (i32.const 0)) (then (local.get $r))
      (else (i32.sub (local.get $la) (local.get $lb)))))`},
	"__hike_region_begin32": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_region_begin32 (result i32)
    (local $r i32) (local $buf i32)
    (local.set $r (call $malloc (i32.const 12)))
    (local.set $buf (call $malloc (i32.const 65536)))
    (i32.store (local.get $r) (local.get $buf))
    (i32.store offset=4 (local.get $r) (i32.const 0))
    (i32.store offset=8 (local.get $r) (i32.const 65536))
    (global.set $__region_active (i32.add (global.get $__region_active) (i32.const 1)))
    (global.set $__region_begin_count (i32.add (global.get $__region_begin_count) (i32.const 1)))
    (local.get $r))`},
	"__hike_region_alloc32": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_region_alloc32 (param $r i32) (param $n i32) (result i32)
    (local $cur i32) (local $end i32) (local $p i32)
    (local.set $cur (i32.load offset=4 (local.get $r)))
    (local.set $end (i32.load offset=8 (local.get $r)))
    (if (i32.le_u (i32.add (local.get $cur) (local.get $n)) (local.get $end))
      (then (local.set $p (i32.add (i32.load (local.get $r)) (local.get $cur)))
            (i32.store offset=4 (local.get $r) (i32.add (local.get $cur) (local.get $n))))
      (else (local.set $p (call $malloc (local.get $n)))))
    (local.get $p))`},
	"__hike_region_end32": runtimeFunc{deps: []string{"free"}, body: `(func $__hike_region_end32 (param $r i32)
	    (local $released i32)
	    (local.set $released (i32.load offset=8 (local.get $r)))
	    (call $free (i32.load (local.get $r)))
	    (call $free (local.get $r))
	    (global.set $__region_active (i32.sub (global.get $__region_active) (i32.const 1)))
	    (global.set $__region_end_count (i32.add (global.get $__region_end_count) (i32.const 1)))
	    (global.set $__region_released_bytes (i32.add (global.get $__region_released_bytes) (local.get $released))))`},
	"__hike_region_active_count":    {body: `(func $__hike_region_active_count (result i32) (global.get $__region_active))`},
	"__hike_region_begin_count":     {body: `(func $__hike_region_begin_count (result i32) (global.get $__region_begin_count))`},
	"__hike_region_end_count":       {body: `(func $__hike_region_end_count (result i32) (global.get $__region_end_count))`},
	"__hike_region_allocated_bytes": runtimeFunc{body: `(func $__hike_region_allocated_bytes (result i32) (i32.const 0))`},
	"__hike_region_released_bytes":  runtimeFunc{body: `(func $__hike_region_released_bytes (result i32) (global.get $__region_released_bytes))`},
}

func lookupWasmRuntime(name string) (runtimeFunc, bool) {
	for key, fn := range wasmRuntime {
		if key == name {
			return fn, true
		}
	}
	return runtimeFunc{}, false
}

func (e *Emitter) emitRuntime() {
	needed := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if needed[name] {
			return
		}
		fn, ok := lookupWasmRuntime(name)
		if !ok {
			return
		}
		needed[name] = true
		for _, dep := range fn.deps {
			visit(dep)
		}
	}
	for _, fn := range e.p.Functions {
		for _, bb := range fn.Blocks {
			for _, in := range bb.Instructions {
				if x, ok := in.(*hir.InstrCallStatic); ok {
					visit(x.CalleeName)
				}
				switch in.(type) {
				case *hir.InstrHeapAlloc:
					visit("malloc")
				case *hir.InstrRegionBegin:
					visit("__hike_region_begin32")
				case *hir.InstrRegionAlloc:
					visit("__hike_region_alloc32")
				case *hir.InstrRegionEnd:
					visit("__hike_region_end32")
				}
			}
		}
	}
	order := []string{"malloc", "calloc", "free", "memcpy32", "memcmp32", "strlen32", "strcmp32", "__hike_region_begin32", "__hike_region_alloc32", "__hike_region_end32", "__hike_region_active_count", "__hike_region_begin_count", "__hike_region_end_count", "__hike_region_allocated_bytes", "__hike_region_released_bytes"}
	for _, name := range order {
		if needed[name] {
			fn, ok := lookupWasmRuntime(name)
			if ok {
				e.b.WriteString("  " + fn.body + "\n")
			}
		}
	}
}

func isWasmRuntime(name string) bool { _, ok := lookupWasmRuntime(name); return ok }
