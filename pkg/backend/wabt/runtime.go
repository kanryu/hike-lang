package wabt

import (
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

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
	"hike_streq": true, "hike_streq_len": true, "hike_strcat_len": true,
	"__hike_map_create": true, "__hike_map_len": true,
	"__hike_map_set": true, "__hike_map_get": true,
	"__hike_map_delete":    true,
	"__hike_string_retain": true, "__hike_string_release": true, "__hike_string_append": true,
	"__hike_panic_set": true, "__hike_panic_get": true, "__hike_panic_fatal": true,
	"__hike_panic_cause": true, "__hike_panic_site": true, "__hike_panic_is_active": true,
	"__hike_lock": true, "__hike_unlock": true,
	"llvm.trap":           true,
	"__hike_region_begin": true, "__hike_region_alloc": true, "__hike_region_end": true,
	"__hike_area_begin": true, "__hike_area_alloc": true, "__hike_area_end": true,
	"__hike_sort_strings": true,
}

func IsWabtRuntimeSymbol(name string) bool {
	for symbol := range WabtRuntimeSymbols {
		if symbol == name {
			return true
		}
	}
	return false
}

// Builtin lowering still appends a 32-bit suffix for targets shared with the
// LLVM backend. WABT is wasm32-only, so its emitted runtime names are unsuffixed.
func normalizeWabtRuntimeName(name string) string {
	switch name {
	case "memcpy32":
		return "memcpy"
	case "memcmp32":
		return "memcmp"
	case "strlen32":
		return "strlen"
	case "strcmp32":
		return "strcmp"
	case "hike_streq32":
		return "hike_streq"
	case "hike_streq_len32":
		return "hike_streq_len"
	case "hike_strcat_len32":
		return "hike_strcat_len"
	case "__hike_string_retain32":
		return "__hike_string_retain"
	case "__hike_string_release32":
		return "__hike_string_release"
	case "__hike_string_append32":
		return "__hike_string_append"
	case "__hike_region_begin32":
		return "__hike_region_begin"
	case "__hike_region_alloc32":
		return "__hike_region_alloc"
	case "__hike_region_end32":
		return "__hike_region_end"
	default:
		return name
	}
}

var wasmRuntime = map[string]runtimeFunc{
	"__hike_lock": runtimeFunc{body: `(func $__hike_lock
    (block $done
      (loop $retry
        (br_if $done (i32.eqz (i32.atomic.rmw.cmpxchg (i32.const 65528) (i32.const 0) (i32.const 1) (i32.const 0))))
		        (br $retry))))`},
	"__hike_unlock": runtimeFunc{body: `(func $__hike_unlock
    (i32.atomic.store (i32.const 65528) (i32.const 0)))`},
	"__hike_panic_set": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_panic_set (param $value i32) (param $cause i32) (param $site i32)
    (local $record i32)
    (local.set $record (call $malloc (i32.const 8)))
    (memory.copy (local.get $record) (local.get $value) (i32.const 8))
    (global.set $__panic_value (local.get $record))
    (if (i32.ne (local.get $cause) (i32.const 0))
      (then
        (local.set $record (call $malloc (i32.const 8)))
        (memory.copy (local.get $record) (local.get $cause) (i32.const 8))
        (global.set $__panic_cause (local.get $record))))
    (global.set $__panic_site (local.get $site))
    (global.set $__panic_active (i32.const 1)))`},
	"__hike_panic_get": runtimeFunc{body: `(func $__hike_panic_get (result i32)
    (global.set $__panic_active (i32.const 0))
    (global.get $__panic_value))`},
	"__hike_panic_cause": runtimeFunc{body: `(func $__hike_panic_cause (result i32)
    (global.get $__panic_cause))`},
	"__hike_panic_site": runtimeFunc{body: `(func $__hike_panic_site (result i32)
    (global.get $__panic_site))`},
	"__hike_panic_is_active": runtimeFunc{body: `(func $__hike_panic_is_active (result i32)
    (global.get $__panic_active))`},
	"__hike_panic_fatal": runtimeFunc{deps: []string{"llvm.trap"}, body: `(func $__hike_panic_fatal (param $site i32)
    ;; The site ID remains available through $__panic_site for host diagnostics.
    (call $llvm.trap))`},
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
	"memcpy": runtimeFunc{body: `(func $memcpy (param $dst i32) (param $src i32) (param $n i32) (result i32)
    (local $mem i32) (local $copyN i32)
    (local.set $mem (i32.mul (memory.size) (i32.const 65536)))
    (if (i32.or (i32.ge_u (local.get $dst) (local.get $mem)) (i32.ge_u (local.get $src) (local.get $mem)))
      (then (return (local.get $dst))))
    (local.set $copyN (local.get $n))
    (if (i32.gt_u (local.get $copyN) (i32.sub (local.get $mem) (local.get $dst)))
      (then (local.set $copyN (i32.sub (local.get $mem) (local.get $dst)))))
    (if (i32.gt_u (local.get $copyN) (i32.sub (local.get $mem) (local.get $src)))
      (then (local.set $copyN (i32.sub (local.get $mem) (local.get $src)))))
    (memory.copy (local.get $dst) (local.get $src) (local.get $copyN))
    (local.get $dst))`},
	"memcmp": runtimeFunc{body: `(func $memcmp (param $a i32) (param $b i32) (param $n i32) (result i32)
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
	"strlen": runtimeFunc{body: `(func $strlen (param $s i32) (result i32)
    (local $p i32) (local $mem i32)
    (local.set $mem (i32.mul (memory.size) (i32.const 65536)))
    (if (i32.ge_u (local.get $s) (local.get $mem)) (then (return (i32.const 0))))
    (local.set $p (local.get $s))
    (block $done (loop $loop
      (br_if $done (i32.ge_u (local.get $p) (local.get $mem)))
      (br_if $done (i32.eqz (i32.load8_u (local.get $p))))
      (local.set $p (i32.add (local.get $p) (i32.const 1)))
      (br $loop)))
    (i32.sub (local.get $p) (local.get $s)))`},
	"strcmp": runtimeFunc{deps: []string{"strlen", "memcmp"}, body: `(func $strcmp (param $a i32) (param $b i32) (result i32)
    (local $la i32) (local $lb i32) (local $n i32) (local $r i32)
    (local.set $la (call $strlen (local.get $a)))
    (local.set $lb (call $strlen (local.get $b)))
    (local.set $n (select (local.get $la) (local.get $lb) (i32.lt_u (local.get $la) (local.get $lb))))
    (local.set $r (call $memcmp (local.get $a) (local.get $b) (local.get $n)))
    (if (result i32) (i32.ne (local.get $r) (i32.const 0)) (then (local.get $r))
      (else (i32.sub (local.get $la) (local.get $lb)))))`},
	"__hike_region_begin": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_region_begin (result i32)
    (local $r i32) (local $buf i32)
    (local.set $r (call $malloc (i32.const 12)))
    (local.set $buf (call $malloc (i32.const 65536)))
    (i32.store (local.get $r) (local.get $buf))
    (i32.store offset=4 (local.get $r) (i32.const 0))
    (i32.store offset=8 (local.get $r) (i32.const 65536))
    (global.set $__region_active (i32.add (global.get $__region_active) (i32.const 1)))
    (global.set $__region_begin_count (i32.add (global.get $__region_begin_count) (i32.const 1)))
    (local.get $r))`},
	"__hike_region_alloc": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_region_alloc (param $r i32) (param $n i32) (result i32)
    (local $cur i32) (local $end i32) (local $p i32)
    (local.set $cur (i32.load offset=4 (local.get $r)))
    (local.set $end (i32.load offset=8 (local.get $r)))
    (if (i32.le_u (i32.add (local.get $cur) (local.get $n)) (local.get $end))
      (then (local.set $p (i32.add (i32.load (local.get $r)) (local.get $cur)))
            (i32.store offset=4 (local.get $r) (i32.add (local.get $cur) (local.get $n))))
      (else (local.set $p (call $malloc (local.get $n)))))
    (local.get $p))`},
	"__hike_region_end": runtimeFunc{deps: []string{"free"}, body: `(func $__hike_region_end (param $r i32)
	    (local $released i32)
	    (local.set $released (i32.load offset=8 (local.get $r)))
	    (call $free (i32.load (local.get $r)))
	    (call $free (local.get $r))
	    (global.set $__region_active (i32.sub (global.get $__region_active) (i32.const 1)))
	    (global.set $__region_end_count (i32.add (global.get $__region_end_count) (i32.const 1)))
	    (global.set $__region_released_bytes (i32.add (global.get $__region_released_bytes) (local.get $released))))`},
	"__hike_area_begin": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_area_begin (param $size i32) (param $parent i32) (result i32)
    (local $a i32) (local $cap i32) (local $start i32) (local $remaining i32) (local $half i32)
    (local.set $a (call $malloc (i32.const 20)))
    (if (i32.eqz (local.get $parent))
      (then
        (local.set $cap (select (local.get $size) (i32.const 16384) (i32.eqz (local.get $size))))
        (i32.store (local.get $a) (call $malloc (local.get $cap)))
        (i32.store offset=4 (local.get $a) (i32.const 0))
        (i32.store offset=8 (local.get $a) (local.get $cap))
        (i32.store offset=12 (local.get $a) (i32.const 0))
        (i32.store offset=16 (local.get $a) (i32.const 0)))
      (else
        (local.set $start (i32.load offset=4 (local.get $parent)))
        (local.set $remaining (i32.sub (i32.load offset=8 (local.get $parent)) (local.get $start)))
        (local.set $half (i32.div_u (local.get $remaining) (i32.const 2)))
        (i32.store offset=4 (local.get $parent) (i32.add (local.get $start) (local.get $half)))
        (i32.store (local.get $a) (i32.add (i32.load (local.get $parent)) (local.get $start)))
        (i32.store offset=4 (local.get $a) (i32.const 0))
        (i32.store offset=8 (local.get $a) (local.get $half))
        (i32.store offset=12 (local.get $a) (local.get $parent))
        (i32.store offset=16 (local.get $a) (local.get $start))))
    (local.get $a))`},
	"__hike_area_alloc": runtimeFunc{deps: []string{"malloc"}, body: `(func $__hike_area_alloc (param $r i32) (param $n i32) (result i32)
    (local $cur i32) (local $aligned i32) (local $next i32)
    (local.set $cur (i32.load offset=4 (local.get $r)))
    (local.set $aligned (i32.and (i32.add (local.get $cur) (i32.const 7)) (i32.const -8)))
    (local.set $next (i32.add (local.get $aligned) (local.get $n)))
    (if (i32.le_u (local.get $next) (i32.load offset=8 (local.get $r)))
      (then
        (i32.store offset=4 (local.get $r) (local.get $next))
        (return (i32.add (i32.load (local.get $r)) (local.get $aligned)))))
    (call $malloc (local.get $n)))`},
	"__hike_area_end": runtimeFunc{deps: []string{"free"}, body: `(func $__hike_area_end (param $r i32)
    (local $parent i32)
    (local.set $parent (i32.load offset=12 (local.get $r)))
    (if (i32.eqz (local.get $parent))
      (then
        (call $free (i32.load (local.get $r)))
        (call $free (local.get $r)))
      (else
        (i32.store offset=4 (local.get $parent) (i32.load offset=16 (local.get $r)))
        (call $free (local.get $r)))))`},
	"__hike_region_active_count":    {body: `(func $__hike_region_active_count (result i32) (global.get $__region_active))`},
	"__hike_region_begin_count":     {body: `(func $__hike_region_begin_count (result i32) (global.get $__region_begin_count))`},
	"__hike_region_end_count":       {body: `(func $__hike_region_end_count (result i32) (global.get $__region_end_count))`},
	"__hike_region_allocated_bytes": runtimeFunc{body: `(func $__hike_region_allocated_bytes (result i32) (i32.const 0))`},
	"__hike_region_released_bytes":  runtimeFunc{body: `(func $__hike_region_released_bytes (result i32) (global.get $__region_released_bytes))`},
	"__hike_string_less": runtimeFunc{body: `(func $__hike_string_less (param $a i32) (param $b i32) (result i32)
    (local $pa i32) (local $pb i32) (local $la i32) (local $lb i32)
    (local $i i32) (local $limit i32) (local $ca i32) (local $cb i32) (local $cmp i32)
    (local.set $pa (i32.add (i32.load (local.get $a)) (i32.load offset=4 (local.get $a))))
    (local.set $pb (i32.add (i32.load (local.get $b)) (i32.load offset=4 (local.get $b))))
    (local.set $la (i32.load offset=8 (local.get $a)))
    (local.set $lb (i32.load offset=8 (local.get $b)))
    (local.set $limit (select (local.get $la) (local.get $lb) (i32.lt_u (local.get $la) (local.get $lb))))
    (local.set $cmp (i32.const 0))
    (block $done (loop $scan
      (br_if $done (i32.ge_u (local.get $i) (local.get $limit)))
      (local.set $ca (i32.load8_u (i32.add (local.get $pa) (local.get $i))))
      (local.set $cb (i32.load8_u (i32.add (local.get $pb) (local.get $i))))
      (if (i32.lt_u (local.get $ca) (local.get $cb)) (then (local.set $cmp (i32.const -1)) (br $done)))
      (if (i32.gt_u (local.get $ca) (local.get $cb)) (then (local.set $cmp (i32.const 1)) (br $done)))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (br $scan)))
    (if (i32.eqz (local.get $cmp))
      (then (if (i32.lt_u (local.get $la) (local.get $lb)) (then (local.set $cmp (i32.const -1)))))
      (else))
    (i32.lt_s (local.get $cmp) (i32.const 0)))`},
	"__hike_sort_strings": runtimeFunc{deps: []string{"__hike_string_less"}, body: `(func $__hike_sort_strings (param $slice i32)
    (local $data i32) (local $n i32) (local $i i32) (local $j i32)
    (local $a i32) (local $b i32) (local $tmp i32)
    (local.set $data (i32.load (local.get $slice)))
    (local.set $n (i32.load offset=4 (local.get $slice)))
    (block $outer_done (loop $outer
      (br_if $outer_done (i32.ge_u (local.get $i) (local.get $n)))
      (local.set $j (i32.const 0))
      (block $inner_done (loop $inner
        (br_if $inner_done (i32.ge_u (local.get $j) (i32.sub (local.get $n) (i32.const 1))))
        (local.set $a (i32.add (local.get $data) (i32.mul (local.get $j) (i32.const 12))))
        (local.set $b (i32.add (local.get $a) (i32.const 12)))
        (if (call $__hike_string_less (local.get $b) (local.get $a))
          (then
            (local.set $tmp (i32.load (local.get $a)))
            (i32.store (local.get $a) (i32.load (local.get $b)))
            (i32.store (local.get $b) (local.get $tmp))
            (local.set $tmp (i32.load offset=4 (local.get $a)))
            (i32.store offset=4 (local.get $a) (i32.load offset=4 (local.get $b)))
            (i32.store offset=4 (local.get $b) (local.get $tmp))
            (local.set $tmp (i32.load offset=8 (local.get $a)))
            (i32.store offset=8 (local.get $a) (i32.load offset=8 (local.get $b)))
            (i32.store offset=8 (local.get $b) (local.get $tmp))))
        (local.set $j (i32.add (local.get $j) (i32.const 1)))
        (br $inner)))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (br $outer))))`},
	"hike_streq": runtimeFunc{body: `(func $hike_streq (param $a i32) (param $b i32) (result i32)
    (local $i i32) (local $ca i32) (local $cb i32) (local $mem i32)
    (local.set $mem (i32.mul (memory.size) (i32.const 65536)))
    (if (i32.or (i32.ge_u (local.get $a) (local.get $mem)) (i32.ge_u (local.get $b) (local.get $mem)))
      (then (return (i32.const 0))))
    (block $done (loop $scan
      (br_if $done (i32.ge_u (i32.add (local.get $a) (local.get $i)) (local.get $mem)))
      (br_if $done (i32.ge_u (i32.add (local.get $b) (local.get $i)) (local.get $mem)))
      (local.set $ca (i32.load8_u (i32.add (local.get $a) (local.get $i))))
      (local.set $cb (i32.load8_u (i32.add (local.get $b) (local.get $i))))
      (br_if $done (i32.ne (local.get $ca) (local.get $cb)))
      (br_if $done (i32.eqz (local.get $ca)))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (br $scan)))
    (i32.eq (local.get $ca) (local.get $cb)))`},
	"hike_streq_len": runtimeFunc{body: `(func $hike_streq_len (param $a i32) (param $alen i32) (param $b i32) (param $blen i32) (result i32)
    (local $i i32) (local $limit i32) (local $same i32) (local $mem i32)
    (local.set $mem (i32.mul (memory.size) (i32.const 65536)))
    (if (i32.or (i32.ge_u (local.get $a) (local.get $mem)) (i32.ge_u (local.get $b) (local.get $mem)))
      (then (return (i32.const 0))))
    (local.set $limit (select (local.get $alen) (local.get $blen) (i32.lt_u (local.get $alen) (local.get $blen))))
    (if (i32.gt_u (local.get $limit) (i32.sub (local.get $mem) (local.get $a)))
      (then (local.set $limit (i32.sub (local.get $mem) (local.get $a)))))
    (if (i32.gt_u (local.get $limit) (i32.sub (local.get $mem) (local.get $b)))
      (then (local.set $limit (i32.sub (local.get $mem) (local.get $b)))))
    (local.set $same (i32.const 1))
    (block $done (loop $scan
      (br_if $done (i32.ge_u (local.get $i) (local.get $limit)))
      (if (i32.ne (i32.load8_u (i32.add (local.get $a) (local.get $i))) (i32.load8_u (i32.add (local.get $b) (local.get $i))))
        (then (local.set $same (i32.const 0)) (br $done)))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (br $scan)))
    (i32.and (local.get $same) (i32.eq (local.get $alen) (local.get $blen))))`},
	"hike_strcat_len": runtimeFunc{deps: []string{"malloc"}, body: `(func $hike_strcat_len (param $a i32) (param $alen i32) (param $b i32) (param $blen i32) (result i32)
    (local $p i32) (local $n i32)
    (local.set $n (i32.add (i32.add (local.get $alen) (local.get $blen)) (i32.const 1)))
    (local.set $p (call $malloc (local.get $n)))
    (memory.copy (local.get $p) (local.get $a) (local.get $alen))
    (memory.copy (i32.add (local.get $p) (local.get $alen)) (local.get $b) (local.get $blen))
    (i32.store8 (i32.add (local.get $p) (i32.add (local.get $alen) (local.get $blen))) (i32.const 0))
	(local.get $p))`},
	"__hike_string_append": runtimeFunc{deps: []string{"hike_strcat_len"}, body: `(func $__hike_string_append (param $a i32) (param $offset i32) (param $alen i32) (param $b i32) (param $boffset i32) (param $blen i32) (result i32)
    (call $hike_strcat_len
      (i32.add (local.get $a) (local.get $offset))
      (local.get $alen)
      (i32.add (local.get $b) (local.get $boffset))
      (local.get $blen)))`},
	"__hike_map_create": runtimeFunc{deps: []string{"calloc"}, body: `(func $__hike_map_create (param $cap i32) (param $is_str i32) (result i32)
    (local $m i32) (local $n i32) (local $buckets i32)
    (local.set $n (select (local.get $cap) (i32.const 16) (i32.ge_u (local.get $cap) (i32.const 16))))
    (local.set $m (call $malloc (i32.const 16)))
    (local.set $buckets (call $calloc (local.get $n) (i32.const 4)))
    (i32.store (local.get $m) (local.get $buckets))
    (i32.store offset=4 (local.get $m) (local.get $n))
    (i32.store offset=8 (local.get $m) (i32.const 0))
    (i32.store offset=12 (local.get $m) (local.get $is_str))
    (local.get $m))`},
	"__hike_map_len": runtimeFunc{body: `(func $__hike_map_len (param $m i32) (result i32)
    (i32.load offset=8 (local.get $m)))`},
	"__hike_map_set": runtimeFunc{deps: []string{"malloc", "strcmp", "strlen"}, body: `(func $__hike_map_set (param $m i32) (param $key i32) (param $value i32)
    (local $n i32) (local $idx i32) (local $entry i32) (local $same i32) (local $is_str i32) (local $size i32) (local $new i32)
    (local.set $n (i32.load offset=4 (local.get $m)))
    (local.set $is_str (i32.load offset=12 (local.get $m)))
    (local.set $idx (if (result i32) (local.get $is_str) (then (i32.const 0)) (else (i32.rem_s (local.get $key) (local.get $n)))))
    (local.set $entry (i32.load (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4)))))
    (block $done (loop $scan
      (br_if $done (i32.eqz (local.get $entry)))
      (local.set $same (if (result i32) (local.get $is_str)
        (then (i32.eqz (call $strcmp (i32.load offset=4 (local.get $entry)) (local.get $key))))
        (else (i32.eq (i32.load offset=4 (local.get $entry)) (local.get $key)))))
      (if (local.get $same)
        (then
          (i32.store (i32.add (local.get $entry) (if (result i32) (local.get $is_str) (then (i32.const 16)) (else (i32.const 8)))) (local.get $value))
          (br $done)))
      (local.set $entry (i32.load (i32.add (local.get $entry) (if (result i32) (local.get $is_str) (then (i32.const 20)) (else (i32.const 12))))))
      (br $scan)))
    (local.set $size (if (result i32) (local.get $is_str) (then (i32.const 24)) (else (i32.const 16))))
    (local.set $new (call $malloc (local.get $size)))
    (i32.store (local.get $new) (i32.const 0))
    (i32.store offset=4 (local.get $new) (local.get $key))
    (if (local.get $is_str)
      (then
        (i32.store offset=8 (local.get $new) (i32.const 0))
        (i32.store offset=12 (local.get $new) (call $strlen (local.get $key)))
        (i32.store offset=16 (local.get $new) (local.get $value))
        (i32.store offset=20 (local.get $new) (i32.load (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4))))))
      (else
        (i32.store offset=8 (local.get $new) (local.get $value))
        (i32.store offset=12 (local.get $new) (i32.load (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4)))))))
    (i32.store (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4))) (local.get $new))
    (i32.store offset=8 (local.get $m) (i32.add (i32.load offset=8 (local.get $m)) (i32.const 1))))`},
	"__hike_map_get": runtimeFunc{deps: []string{"strcmp"}, body: `(func $__hike_map_get (param $m i32) (param $key i32) (param $out i32) (result i32)
    (local $entry i32) (local $idx i32) (local $n i32) (local $is_str i32) (local $same i32)
    (i32.store (local.get $out) (i32.const 0))
    (local.set $n (i32.load offset=4 (local.get $m)))
    (local.set $is_str (i32.load offset=12 (local.get $m)))
    (local.set $idx (if (result i32) (local.get $is_str) (then (i32.const 0)) (else (i32.rem_s (local.get $key) (local.get $n)))))
    (local.set $entry (i32.load (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4)))))
    (block $done (loop $scan
      (br_if $done (i32.eqz (local.get $entry)))
      (local.set $same (if (result i32) (local.get $is_str)
        (then (i32.eqz (call $strcmp (i32.load offset=4 (local.get $entry)) (local.get $key))))
        (else (i32.eq (i32.load offset=4 (local.get $entry)) (local.get $key)))))
      (if (local.get $same)
        (then
          (i32.store (local.get $out) (i32.load (i32.add (local.get $entry) (if (result i32) (local.get $is_str) (then (i32.const 16)) (else (i32.const 8))))) )
          (br $done)))
      (local.set $entry (i32.load (i32.add (local.get $entry) (if (result i32) (local.get $is_str) (then (i32.const 20)) (else (i32.const 12))))))
      (br $scan)))
    (i32.const 0))`},
	"__hike_map_delete": runtimeFunc{deps: []string{"strcmp"}, body: `(func $__hike_map_delete (param $m i32) (param $key i32)
    (local $n i32) (local $idx i32) (local $entry i32) (local $prev i32) (local $same i32) (local $is_str i32) (local $next i32)
    (local.set $n (i32.load offset=4 (local.get $m)))
    (local.set $is_str (i32.load offset=12 (local.get $m)))
    (local.set $idx (if (result i32) (local.get $is_str) (then (i32.const 0)) (else (i32.rem_s (local.get $key) (local.get $n)))))
    (local.set $entry (i32.load (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4)))))
    (local.set $prev (i32.const 0))
    (block $done (loop $scan
      (br_if $done (i32.eqz (local.get $entry)))
      (local.set $same (if (result i32) (local.get $is_str)
        (then (i32.eqz (call $strcmp (i32.load offset=4 (local.get $entry)) (local.get $key))))
        (else (i32.eq (i32.load offset=4 (local.get $entry)) (local.get $key)))))
      (local.set $next (i32.load (i32.add (local.get $entry) (if (result i32) (local.get $is_str) (then (i32.const 20)) (else (i32.const 12))))))
      (if (local.get $same)
        (then
          (if (i32.eqz (local.get $prev))
            (then (i32.store (i32.add (i32.load (local.get $m)) (i32.mul (local.get $idx) (i32.const 4))) (local.get $next)))
            (else (i32.store (i32.add (local.get $prev) (if (result i32) (local.get $is_str) (then (i32.const 20)) (else (i32.const 12)))) (local.get $next))))
          (i32.store offset=8 (local.get $m) (i32.sub (i32.load offset=8 (local.get $m)) (i32.const 1)))
          (br $done)))
      (local.set $prev (local.get $entry))
      (local.set $entry (local.get $next))
      (br $scan))))`},
	"__hike_string_retain":  runtimeFunc{body: `(func $__hike_string_retain (param $s i32))`},
	"__hike_string_release": runtimeFunc{body: `(func $__hike_string_release (param $s i32))`},
	"llvm.trap":             runtimeFunc{body: `(func $llvm.trap (unreachable))`},
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
	e.runtimeFunctions = 0
	needed := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		name = normalizeWabtRuntimeName(name)
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
	for _, g := range e.p.Globals {
		if g.Typ == sema.TypeString || g.Typ.TypeName() == "string" {
			visit("malloc")
		}
	}
	for _, fn := range e.p.Functions {
		if len(fn.ReturnTypes) > 1 {
			visit("malloc")
		}
		for _, bb := range e.blocksForEmission(fn) {
			for _, in := range bb.Instructions {
				if x, ok := in.(*hir.InstrCallStatic); ok {
					visit(x.CalleeName)
				}
				switch in.(type) {
				case *hir.InstrLock:
					visit("__hike_lock")
				case *hir.InstrUnlock:
					visit("__hike_unlock")
				case *hir.InstrHeapAlloc:
					visit("malloc")
				case *hir.InstrChanMake:
					visit("malloc")
				case *hir.InstrRegionBegin:
					visit("__hike_region_begin")
				case *hir.InstrRegionAlloc:
					visit("__hike_region_alloc")
				case *hir.InstrRegionEnd:
					visit("__hike_region_end")
				case *hir.InstrAreaBegin:
					visit("__hike_area_begin")
				case *hir.InstrAreaAlloc:
					visit("__hike_area_alloc")
				case *hir.InstrAreaEnd:
					visit("__hike_area_end")
				}
				if _, ok := bb.Terminator.(*hir.InstrPanic); ok {
					visit("__hike_panic_fatal")
					visit("__hike_panic_set")
					if fn.HasLocalRecover {
						visit("__hike_panic_is_active")
					}
				}
			}
		}
	}
	order := []string{"malloc", "calloc", "free", "memcpy", "memcmp", "strlen", "strcmp", "hike_streq", "hike_streq_len", "hike_strcat_len", "__hike_map_create", "__hike_map_len", "__hike_map_set", "__hike_map_get", "__hike_map_delete", "__hike_string_retain", "__hike_string_release", "__hike_lock", "__hike_unlock", "__hike_panic_set", "__hike_panic_get", "__hike_panic_cause", "__hike_panic_site", "__hike_panic_is_active", "__hike_panic_fatal", "llvm.trap", "__hike_region_begin", "__hike_region_alloc", "__hike_region_end", "__hike_area_begin", "__hike_area_alloc", "__hike_area_end", "__hike_region_active_count", "__hike_region_begin_count", "__hike_region_end_count", "__hike_region_allocated_bytes", "__hike_region_released_bytes", "__hike_string_less", "__hike_sort_strings"}
	for _, name := range order {
		if needed[name] {
			fn, ok := lookupWasmRuntime(name)
			if ok {
				if name == "__hike_lock" {
					if e.Concurrent {
						fn.body = `(func $__hike_lock
    (block $done
      (loop $retry
        (br_if $done (i32.eqz (i32.atomic.rmw.cmpxchg (i32.const 65528) (i32.const 0) (i32.const 1) (i32.const 0))))
		        (br $retry))))`
					} else {
						fn.body = `(func $__hike_lock
    (block $done
      (loop $retry
        (br_if $done (if (result i32) (i32.eqz (global.get $__hike_lock_state)) (then (global.set $__hike_lock_state (i32.const 1)) (i32.const 1)) (else (i32.const 0))))
		        (br $retry))))`
					}
				}
				if name == "__hike_unlock" && !e.Concurrent {
					fn.body = `(func $__hike_unlock
    (global.set $__hike_lock_state (i32.const 0)))`
				}
				e.b.WriteString("  " + fn.body + "\n")
				e.runtimeFunctions++
			}
		}
	}
}

func isWasmRuntime(name string) bool { _, ok := lookupWasmRuntime(name); return ok }
