; ==============================================================================
; Hike Language Builtin Runtime (LLVM IR)
; ==============================================================================

; ------------------------------------------------------------------------------
; External C Standard Library Declarations (Allocators Only)
; ------------------------------------------------------------------------------
declare noalias i8* @malloc(i64)
declare noalias i8* @calloc(i64, i64)
declare void @free(i8*)

; Slice backing storage. The returned pointer addresses the first payload byte;
; capacity is stored as i32 at owner-16. calloc keeps make([]T,n) zeroed while
; giving all slice allocation paths one header layout.
define internal i8* @__hike_slice_alloc(i64 %size, i64 %capacity) #0 {
entry:
  %total = add i64 %size, 16
  %raw = call i8* @calloc(i64 1, i64 %total)
  %cap_ptr = bitcast i8* %raw to i32*
  %cap32 = trunc i64 %capacity to i32
  store i32 %cap32, i32* %cap_ptr
  %ref_raw = getelementptr inbounds i8, i8* %raw, i64 4
  %ref_ptr = bitcast i8* %ref_raw to i32*
  store i32 1, i32* %ref_ptr
  %owner = getelementptr inbounds i8, i8* %raw, i64 16
  ret i8* %owner
}

define internal i64 @__hike_slice_cap(i8* %owner) #0 {
entry:
  %is_null = icmp eq i8* %owner, null
  br i1 %is_null, label %zero, label %load
zero:
  ret i64 0
load:
  %raw = getelementptr inbounds i8, i8* %owner, i64 -16
  %cap_ptr = bitcast i8* %raw to i32*
  %cap32 = load i32, i32* %cap_ptr
  %cap = zext i32 %cap32 to i64
  ret i64 %cap
}

define internal void @__hike_slice_retain(i8* %owner, i32 %offset) #0 {
entry:
  %is_null = icmp eq i8* %owner, null
  %literal = icmp slt i32 %offset, 0
  %skip = or i1 %is_null, %literal
  br i1 %skip, label %done, label %load
load:
  %raw = getelementptr inbounds i8, i8* %owner, i64 -12
  %ref_ptr = bitcast i8* %raw to i32*
  %old = load i32, i32* %ref_ptr
  %immortal = icmp eq i32 %old, -2147483648
  br i1 %immortal, label %done, label %increment
increment:
  %next = add i32 %old, 1
  store i32 %next, i32* %ref_ptr
  br label %done
done:
  ret void
}

define internal void @__hike_slice_release(i8* %owner, i32 %offset) #0 {
entry:
  %is_null = icmp eq i8* %owner, null
  %literal = icmp slt i32 %offset, 0
  %skip = or i1 %is_null, %literal
  br i1 %skip, label %done, label %load
load:
  %raw = getelementptr inbounds i8, i8* %owner, i64 -12
  %ref_ptr = bitcast i8* %raw to i32*
  %old = load i32, i32* %ref_ptr
  %immortal = icmp eq i32 %old, -2147483648
  br i1 %immortal, label %done, label %check_free
check_free:
  %last = icmp eq i32 %old, 1
  br i1 %last, label %free_buffer, label %decrement
decrement:
  %next = sub i32 %old, 1
  store i32 %next, i32* %ref_ptr
  br label %done
free_buffer:
  %allocation = getelementptr inbounds i8, i8* %owner, i64 -16
  call void @free(i8* %allocation)
  br label %done
done:
  ret void
}

@__hike_region_active_stat = internal global i64 0
@__hike_region_begin_count_stat = internal global i64 0
@__hike_region_end_count_stat = internal global i64 0
@__hike_region_allocated_bytes_stat = internal global i64 0
@__hike_region_released_bytes_stat = internal global i64 0
@__hike_panic_value = internal global { i32, i8* } zeroinitializer
@__hike_panic_cause_state = internal global { i32, i8* } zeroinitializer
@__hike_panic_site_state = internal global i32 -1
@__hike_panic_active = internal global i1 false
@__hike_lock_state = internal global i32 0

define internal void @__hike_lock() {
entry:
  br label %retry
retry:
  %attempt = cmpxchg i32* @__hike_lock_state, i32 0, i32 1 acquire acquire
  %acquired = extractvalue { i32, i1 } %attempt, 1
  br i1 %acquired, label %done, label %retry
done:
  ret void
}
define internal void @__hike_unlock() {
entry:
  store atomic i32 0, i32* @__hike_lock_state release, align 4
  ret void
}

declare void @llvm.trap()

define internal void @__hike_panic_set({ i32, i8* } %value, { i32, i8* } %cause, i32 %site) {
entry:
  store { i32, i8* } %value, { i32, i8* }* @__hike_panic_value
  store { i32, i8* } %cause, { i32, i8* }* @__hike_panic_cause_state
  store i32 %site, i32* @__hike_panic_site_state
  store i1 true, i1* @__hike_panic_active
  ret void
}
define internal { i32, i8* } @__hike_panic_get() {
entry:
  %value = load { i32, i8* }, { i32, i8* }* @__hike_panic_value
  store i1 false, i1* @__hike_panic_active
  ret { i32, i8* } %value
}
define internal { i32, i8* } @__hike_panic_cause() {
entry:
  %cause = load { i32, i8* }, { i32, i8* }* @__hike_panic_cause_state
  ret { i32, i8* } %cause
}
define internal i32 @__hike_panic_site() {
entry:
  %site = load i32, i32* @__hike_panic_site_state
  ret i32 %site
}
define internal i1 @__hike_panic_is_active() {
entry:
  %active = load i1, i1* @__hike_panic_active
  ret i1 %active
}
define internal void @__hike_panic_fatal(i32 %site) {
entry:
  %panic = load { i32, i8* }, { i32, i8* }* @__hike_panic_value
  %data = extractvalue { i32, i8* } %panic, 1
  %string = bitcast i8* %data to { i8*, i32, i32 }*
  %base_ptr = getelementptr { i8*, i32, i32 }, { i8*, i32, i32 }* %string, i32 0, i32 0
  %offset_ptr = getelementptr { i8*, i32, i32 }, { i8*, i32, i32 }* %string, i32 0, i32 1
  %length_ptr = getelementptr { i8*, i32, i32 }, { i8*, i32, i32 }* %string, i32 0, i32 2
  %base = load i8*, i8** %base_ptr
  %offset = load i32, i32* %offset_ptr
  %length = load i32, i32* %length_ptr
  %start = call i8* @__hike_string_start(i8* %base, i32 %offset)
  call void @__hike_stderr_write(i8* %start, i32 %length)
  call void @llvm.trap()
  unreachable
}
define internal i64 @__hike_region_active_count() {
entry:
  %v = load i64, i64* @__hike_region_active_stat
  ret i64 %v
}
define internal i64 @__hike_region_begin_count() {
entry:
  %v = load i64, i64* @__hike_region_begin_count_stat
  ret i64 %v
}
define internal i64 @__hike_region_end_count() {
entry:
  %v = load i64, i64* @__hike_region_end_count_stat
  ret i64 %v
}
define internal i64 @__hike_region_allocated_bytes() {
entry:
  %v = load i64, i64* @__hike_region_allocated_bytes_stat
  ret i64 %v
}
define internal i64 @__hike_region_released_bytes() {
entry:
  %v = load i64, i64* @__hike_region_released_bytes_stat
  ret i64 %v
}

; Per-function bump arena. The single end call releases the arena in O(1).
%struct.__hike_region = type { i8*, i64, i64 }
define internal i8* @__hike_region_begin() {
entry:
  %bc = load i64, i64* @__hike_region_begin_count_stat
  %bc1 = add i64 %bc, 1
  store i64 %bc1, i64* @__hike_region_begin_count_stat
  %ac = load i64, i64* @__hike_region_active_stat
  %ac1 = add i64 %ac, 1
  store i64 %ac1, i64* @__hike_region_active_stat
  %r = call i8* @malloc(i64 24)
  %buf = call i8* @malloc(i64 65536)
  %rp = bitcast i8* %r to %struct.__hike_region*
  %p0 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 0
  store i8* %buf, i8** %p0
  %p1 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 1
  store i64 0, i64* %p1
  %p2 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 2
  store i64 65536, i64* %p2
  ret i8* %r
}
define internal i8* @__hike_region_alloc(i8* %r, i64 %n) {
entry:
  %rp = bitcast i8* %r to %struct.__hike_region*
  %p1 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 1
  %old = load i64, i64* %p1
  %aligned0 = add i64 %old, 7
  %aligned = and i64 %aligned0, -8
  %next = add i64 %aligned, %n
  %p2 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 2
  %cap = load i64, i64* %p2
  %ok = icmp ule i64 %next, %cap
  br i1 %ok, label %in, label %fallback
in:
  %p0 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 0
  %buf = load i8*, i8** %p0
  %ret = getelementptr i8, i8* %buf, i64 %aligned
  store i64 %next, i64* %p1
  %ab = load i64, i64* @__hike_region_allocated_bytes_stat
  %ab1 = add i64 %ab, %n
  store i64 %ab1, i64* @__hike_region_allocated_bytes_stat
  ret i8* %ret
fallback:
  %heap = call i8* @malloc(i64 %n)
  ret i8* %heap
}
define internal void @__hike_region_end(i8* %r) {
entry:
  %rp = bitcast i8* %r to %struct.__hike_region*
  %p0 = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 0
  %buf = load i8*, i8** %p0
  %usedp = getelementptr %struct.__hike_region, %struct.__hike_region* %rp, i32 0, i32 1
  %used = load i64, i64* %usedp
  %rel = load i64, i64* @__hike_region_released_bytes_stat
  %rel1 = add i64 %rel, %used
  store i64 %rel1, i64* @__hike_region_released_bytes_stat
  %ec = load i64, i64* @__hike_region_end_count_stat
  %ec1 = add i64 %ec, 1
  store i64 %ec1, i64* @__hike_region_end_count_stat
  %ac = load i64, i64* @__hike_region_active_stat
  %ac1 = sub i64 %ac, 1
  store i64 %ac1, i64* @__hike_region_active_stat
  call void @free(i8* %buf)
  call void @free(i8* %r)
  ret void
}

; Lexical area allocator. A root area owns a separate arena. Nested areas
; reserve half of the remaining parent area and restore the parent cursor on
; exit, so all nested allocations become invalid in O(1).
%struct.__hike_area = type { i8*, i64, i64, i8*, i64 }
define internal i8* @__hike_area_begin(i64 %size, i8* %parent) {
entry:
  %a = call i8* @malloc(i64 40)
  %isroot = icmp eq i8* %parent, null
  br i1 %isroot, label %root, label %nested
root:
  %default = icmp eq i64 %size, 0
  %cap = select i1 %default, i64 16384, i64 %size
  %buf = call i8* @malloc(i64 %cap)
  %ap = bitcast i8* %a to %struct.__hike_area*
  %p0 = getelementptr %struct.__hike_area, %struct.__hike_area* %ap, i32 0, i32 0
  store i8* %buf, i8** %p0
  %p1 = getelementptr %struct.__hike_area, %struct.__hike_area* %ap, i32 0, i32 1
  store i64 0, i64* %p1
  %p2 = getelementptr %struct.__hike_area, %struct.__hike_area* %ap, i32 0, i32 2
  store i64 %cap, i64* %p2
  %p3 = getelementptr %struct.__hike_area, %struct.__hike_area* %ap, i32 0, i32 3
  store i8* null, i8** %p3
  %p4 = getelementptr %struct.__hike_area, %struct.__hike_area* %ap, i32 0, i32 4
  store i64 0, i64* %p4
  ret i8* %a
nested:
  %pp = bitcast i8* %parent to %struct.__hike_area*
  %usedp = getelementptr %struct.__hike_area, %struct.__hike_area* %pp, i32 0, i32 1
  %start = load i64, i64* %usedp
  %capp = getelementptr %struct.__hike_area, %struct.__hike_area* %pp, i32 0, i32 2
  %pcap = load i64, i64* %capp
  %remaining = sub i64 %pcap, %start
  %half = udiv i64 %remaining, 2
  %next = add i64 %start, %half
  store i64 %next, i64* %usedp
  %bufp = getelementptr %struct.__hike_area, %struct.__hike_area* %pp, i32 0, i32 0
  %base = load i8*, i8** %bufp
  %bufn = getelementptr i8, i8* %base, i64 %start
  %apn = bitcast i8* %a to %struct.__hike_area*
  %n0 = getelementptr %struct.__hike_area, %struct.__hike_area* %apn, i32 0, i32 0
  store i8* %bufn, i8** %n0
  %n1 = getelementptr %struct.__hike_area, %struct.__hike_area* %apn, i32 0, i32 1
  store i64 0, i64* %n1
  %n2 = getelementptr %struct.__hike_area, %struct.__hike_area* %apn, i32 0, i32 2
  store i64 %half, i64* %n2
  %n3 = getelementptr %struct.__hike_area, %struct.__hike_area* %apn, i32 0, i32 3
  store i8* %parent, i8** %n3
  %n4 = getelementptr %struct.__hike_area, %struct.__hike_area* %apn, i32 0, i32 4
  store i64 %start, i64* %n4
  ret i8* %a
}
define internal i8* @__hike_area_alloc(i8* %r, i64 %n) {
entry:
  %rp = bitcast i8* %r to %struct.__hike_area*
  %p1 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 1
  %old = load i64, i64* %p1
  %aligned0 = add i64 %old, 7
  %aligned = and i64 %aligned0, -8
  %next = add i64 %aligned, %n
  %p2 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 2
  %cap = load i64, i64* %p2
  %ok = icmp ule i64 %next, %cap
  br i1 %ok, label %in, label %fallback
in:
  %p0 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 0
  %buf = load i8*, i8** %p0
  %ret = getelementptr i8, i8* %buf, i64 %aligned
  store i64 %next, i64* %p1
  ret i8* %ret
fallback:
  %heap = call i8* @malloc(i64 %n)
  ret i8* %heap
}
define internal void @__hike_area_end(i8* %r) {
entry:
  %rp = bitcast i8* %r to %struct.__hike_area*
  %p3 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 3
  %parent = load i8*, i8** %p3
  %nested = icmp ne i8* %parent, null
  br i1 %nested, label %restore, label %root
restore:
  %pp = bitcast i8* %parent to %struct.__hike_area*
  %usedp = getelementptr %struct.__hike_area, %struct.__hike_area* %pp, i32 0, i32 1
  %p4 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 4
  %start = load i64, i64* %p4
  store i64 %start, i64* %usedp
  call void @free(i8* %r)
  ret void
root:
  %p0 = getelementptr %struct.__hike_area, %struct.__hike_area* %rp, i32 0, i32 0
  %buf = load i8*, i8** %p0
  call void @free(i8* %buf)
  call void @free(i8* %r)
  ret void
}

; ------------------------------------------------------------------------------
; Pure Memory & String Builtin Implementations (64-bit Native, Libc-Free)
; ------------------------------------------------------------------------------

; メモリブロックの複製 (64-bit)
define internal i8* @memcpy(i8* %dst, i8* %src, i64 %n) #0 {
entry:
  %cmp = icmp eq i64 %n, 0
  br i1 %cmp, label %exit, label %loop.body
loop.body:
  %i = phi i64 [ 0, %entry ], [ %i.next, %loop.body ]
  %p_src = getelementptr inbounds i8, i8* %src, i64 %i
  %val = load i8, i8* %p_src, align 1
  %p_dst = getelementptr inbounds i8, i8* %dst, i64 %i
  store i8 %val, i8* %p_dst, align 1
  %i.next = add i64 %i, 1
  %cont = icmp ult i64 %i.next, %n
  br i1 %cont, label %loop.body, label %exit
exit:
  ret i8* %dst
}

; メモリブロックの比較 (64-bit)
define internal i32 @memcmp(i8* %s1, i8* %s2, i64 %n) #0 {
entry:
  %cmp = icmp eq i64 %n, 0
  br i1 %cmp, label %ret_zero, label %loop.body
loop.body:
  %i = phi i64 [ 0, %entry ], [ %i.next, %loop.inc ]
  %p1 = getelementptr inbounds i8, i8* %s1, i64 %i
  %b1 = load i8, i8* %p1, align 1
  %p2 = getelementptr inbounds i8, i8* %s2, i64 %i
  %b2 = load i8, i8* %p2, align 1
  %diff = icmp ne i8 %b1, %b2
  br i1 %diff, label %calc_diff, label %loop.inc
loop.inc:
  %i.next = add i64 %i, 1
  %cont = icmp ult i64 %i.next, %n
  br i1 %cont, label %loop.body, label %ret_zero
calc_diff:
  %u1 = zext i8 %b1 to i32
  %u2 = zext i8 %b2 to i32
  %res = sub i32 %u1, %u2
  ret i32 %res
ret_zero:
  ret i32 0
}

; 文字列長の算出 (64-bit)
define internal i64 @strlen(i8* %s) #0 {
entry:
  %is_null = icmp eq i8* %s, null
  br i1 %is_null, label %ret_zero, label %loop.body
loop.body:
  %len = phi i64 [ 0, %entry ], [ %len.next, %loop.body ]
  %p = getelementptr inbounds i8, i8* %s, i64 %len
  %c = load i8, i8* %p, align 1
  %is_end = icmp eq i8 %c, 0
  %len.next = add i64 %len, 1
  br i1 %is_end, label %ret_len, label %loop.body
ret_len:
  ret i64 %len
ret_zero:
  ret i64 0
}

; 文字列の辞書順比較 (64-bit)
define internal i32 @strcmp(i8* %s1, i8* %s2) #0 {
entry:
  %eq_ptr = icmp eq i8* %s1, %s2
  br i1 %eq_ptr, label %ret_zero, label %check_null
check_null:
  %n1 = icmp eq i8* %s1, null
  %n2 = icmp eq i8* %s2, null
  %either_null = or i1 %n1, %n2
  br i1 %either_null, label %s1_is_null, label %check_s2
s1_is_null:
  br i1 %n2, label %ret_zero, label %ret_neg
check_s2:
  br i1 %n2, label %ret_pos, label %loop.body
ret_neg:
  ret i32 -1
ret_pos:
  ret i32 1
ret_zero:
  ret i32 0
loop.body:
  %idx = phi i64 [ 0, %check_s2 ], [ %idx.next, %loop.inc ]
  %p1 = getelementptr inbounds i8, i8* %s1, i64 %idx
  %c1 = load i8, i8* %p1, align 1
  %p2 = getelementptr inbounds i8, i8* %s2, i64 %idx
  %c2 = load i8, i8* %p2, align 1
  %diff = icmp ne i8 %c1, %c2
  br i1 %diff, label %calc_diff, label %check_end
check_end:
  %is_end = icmp eq i8 %c1, 0
  br i1 %is_end, label %ret_zero, label %loop.inc
loop.inc:
  %idx.next = add i64 %idx, 1
  br label %loop.body
calc_diff:
  %u1 = zext i8 %c1 to i32
  %u2 = zext i8 %c2 to i32
  %res = sub i32 %u1, %u2
  ret i32 %res
}

; ------------------------------------------------------------------------------
; OS Native Threading & Synchronization (Kernel32 / Libc-Free)
; ------------------------------------------------------------------------------
; ------------------------------------------------------------------------------
; Memory Management Types
; ------------------------------------------------------------------------------
%struct.Arena = type { i8*, i64, i64 }
%struct.Allocator = type { i8*, i8* }

; ------------------------------------------------------------------------------
; Standard OS Native Sleep & Time Binding (std/time)
; ------------------------------------------------------------------------------

define internal void @c_os_sleep_ms(i32 %ms) {
entry:
  call void @hike_sleep_ms(i32 %ms)
  ret void
}

define internal void @os_sleep_ms(i32 %ms) {
entry:
  call void @hike_sleep_ms(i32 %ms)
  ret void
}

define internal i64 @c_os_now_ns() {
entry:
  %ms = call i64 @hike_now_ns()
  %ns = mul i64 %ms, 1
  ret i64 %ns
}

define internal i64 @os_now_ns() {
entry:
  %ms = call i64 @hike_now_ns()
  %ns = mul i64 %ms, 1
  ret i64 %ns
}

; ------------------------------------------------------------------------------
; Async Thread Pool & Task Runtime
; ------------------------------------------------------------------------------

; %struct.__hike_task = { fn_thunk, env_ptr, ret_buf, completed, event_handle }
%struct.__hike_task = type { void (i8*, i8*)*, i8*, i8*, i32, i8* }

; スレッドプールワーカースレッドのエントリルーチン
define internal i32 @__hike_task_worker_thunk(i8* %param) {
entry:
  %task = bitcast i8* %param to %struct.__hike_task*
  %p_fn = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 0
  %fn = load void (i8*, i8*)*, void (i8*, i8*)** %p_fn
  %p_env = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 1
  %env = load i8*, i8** %p_env
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  %buf = load i8*, i8** %p_buf

  ; タスク本体（サンク）の実行
  call void %fn(i8* %env, i8* %buf)

  ; 完了状態へ遷移
  %p_done = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 3
  store i32 1, i32* %p_done

  ; 待機側スレッドへの起床シグナル発火
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  %ev = load i8*, i8** %p_ev
  %ev_null = icmp eq i8* %ev, null
  br i1 %ev_null, label %exit, label %signal_ev
signal_ev:
  call i32 @hike_event_signal(i8* %ev)
  br label %exit
exit:
  ret i32 0
}

; スレッドプールへの非同期タスク投入
define internal %struct.__hike_task* @__hike_async(i8* %fn_ptr, i8* %env_ptr, i64 %ret_size) {
entry:
  %raw_task = call i8* @malloc(i64 40)
  %task = bitcast i8* %raw_task to %struct.__hike_task*

  ; 関数ポインタ・環境ポインタの設定
  %fn_thunk = bitcast i8* %fn_ptr to void (i8*, i8*)*
  %p_fn = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 0
  store void (i8*, i8*)* %fn_thunk, void (i8*, i8*)** %p_fn

  %p_env = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 1
  store i8* %env_ptr, i8** %p_env

  ; 戻り値格納バッファの確保
  %need_buf = icmp sgt i64 %ret_size, 0
  br i1 %need_buf, label %alloc_buf, label %no_buf
alloc_buf:
  %buf = call i8* @malloc(i64 %ret_size)
  br label %set_buf
no_buf:
  br label %set_buf
set_buf:
  %buf_val = phi i8* [ %buf, %alloc_buf ], [ null, %no_buf ]
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  store i8* %buf_val, i8** %p_buf

  ; 完了フラグ初期化 (0: 実行中)
  %p_done = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 3
  store i32 0, i32* %p_done

  ; 同期イベントの生成 (自動リセット)
  %ev = call i8* @hike_event_create()
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  store i8* %ev, i8** %p_ev

  ; OS スレッドプールへ投入 (WT_EXECUTEDEFAULT = 0)
  call i32 @hike_thread_spawn(i32 (i8*)* @__hike_task_worker_thunk, i8* %raw_task)

  ret %struct.__hike_task* %task
}

; タスクの同期待ち (<- 演算子の実体)
define internal i8* @__hike_task_wait(%struct.__hike_task* %task) {
entry:
  %task_null = icmp eq %struct.__hike_task* %task, null
  br i1 %task_null, label %ret_null, label %check_done
ret_null:
  ret i8* null
check_done:
  %p_done = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 3
  %done = load i32, i32* %p_done
  %is_done = icmp ne i32 %done, 0
  br i1 %is_done, label %get_res, label %wait_ev
wait_ev:
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  %ev = load i8*, i8** %p_ev
  %has_ev = icmp ne i8* %ev, null
  br i1 %has_ev, label %do_wait, label %poll_loop
do_wait:
  ; INFINITE = 0xFFFFFFFF (-1)
  call i32 @hike_event_wait(i8* %ev, i32 -1)
  call i32 @hike_event_destroy(i8* %ev)
  store i8* null, i8** %p_ev
  br label %get_res
poll_loop:
  call void @hike_sleep_ms(i32 1)
  %done_poll = load i32, i32* %p_done
  %is_done_poll = icmp ne i32 %done_poll, 0
  br i1 %is_done_poll, label %get_res, label %poll_loop
get_res:
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  %buf = load i8*, i8** %p_buf
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; Channel & Concurrency Queue Runtime
; ------------------------------------------------------------------------------

; %struct.__hike_chan = { elem_size, cap, count, head, tail, buf, lock, closed, ev_recv, ev_send }
%struct.__hike_chan = type { i64, i64, i64, i64, i64, i8*, i32, i32, i8*, i8* }

; チャネル内部スピンロックの獲得 (Yield 付き)
define internal void @__hike_chan_lock(i32* %lock) {
entry:
  br label %spin
spin:
  %prev = atomicrmw xchg i32* %lock, i32 1 seq_cst
  %is_free = icmp eq i32 %prev, 0
  br i1 %is_free, label %acquired, label %wait
wait:
  call void @hike_sleep_ms(i32 0)
  br label %spin
acquired:
  ret void
}

; チャネル内部スピンロックの解放
define internal void @__hike_chan_unlock(i32* %lock) {
entry:
  store atomic i32 0, i32* %lock seq_cst, align 4
  ret void
}

; チャネルの新規生成 (make(chan T, cap))
define internal i8* @__hike_chan_make(i64 %elem_size, i64 %cap) {
entry:
  %cap_le_0 = icmp sle i64 %cap, 0
  %real_cap = select i1 %cap_le_0, i64 1, i64 %cap

  %raw = call i8* @malloc(i64 72)
  %ch = bitcast i8* %raw to %struct.__hike_chan*

  %buf_bytes = mul i64 %real_cap, %elem_size
  %buf = call i8* @malloc(i64 %buf_bytes)

  %p_es = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 0
  store i64 %elem_size, i64* %p_es
  %p_cap = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 1
  store i64 %real_cap, i64* %p_cap
  %p_cnt = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 2
  store i64 0, i64* %p_cnt
  %p_hd = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 3
  store i64 0, i64* %p_hd
  %p_tl = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 4
  store i64 0, i64* %p_tl
  %p_buf = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 5
  store i8* %buf, i8** %p_buf
  %p_lock = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 6
  store i32 0, i32* %p_lock
  %p_cls = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 7
  store i32 0, i32* %p_cls

  ; 自動リセットイベントの生成 (受信側・送信側)
  %ev_recv = call i8* @hike_event_create()
  %p_ev_r = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 8
  store i8* %ev_recv, i8** %p_ev_r

  %ev_send = call i8* @hike_event_create()
  %p_ev_s = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 9
  store i8* %ev_send, i8** %p_ev_s

  ret i8* %raw
}

; チャネルへの値送信 (ch <- val)
define internal void @__hike_chan_send(i8* %ch_raw, i8* %val_ptr) {
entry:
  %ch = bitcast i8* %ch_raw to %struct.__hike_chan*
  %p_lock = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 6
  %p_cnt = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 2
  %p_cap = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 1
  %p_tl = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 4
  %p_es = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 0
  %p_buf = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 5
  %p_cls = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 7
  %p_ev_r = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 8
  %p_ev_s = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 9
  br label %try_send

try_send:
  call void @__hike_chan_lock(i32* %p_lock)
  ; クローズ済みチャネルへの送信チェック (デッドロック抑止)
  %cls = load i32, i32* %p_cls
  %is_closed = icmp ne i32 %cls, 0
  br i1 %is_closed, label %on_closed_send, label %check_room

on_closed_send:
  call void @__hike_chan_unlock(i32* %p_lock)
  ret void

check_room:
  %cnt = load i64, i64* %p_cnt
  %cap = load i64, i64* %p_cap
  %has_room = icmp slt i64 %cnt, %cap
  br i1 %has_room, label %do_send, label %full

do_send:
  %tl = load i64, i64* %p_tl
  %es = load i64, i64* %p_es
  %buf = load i8*, i8** %p_buf
  %offset = mul i64 %tl, %es
  %dst = getelementptr inbounds i8, i8* %buf, i64 %offset
  call i8* @memcpy(i8* %dst, i8* %val_ptr, i64 %es)

  %next_tl_raw = add i64 %tl, 1
  %next_tl = urem i64 %next_tl_raw, %cap
  store i64 %next_tl, i64* %p_tl

  %next_cnt = add i64 %cnt, 1
  store i64 %next_cnt, i64* %p_cnt

  ; 受信待ちスレッドを起床
  %ev_r = load i8*, i8** %p_ev_r
  call i32 @hike_event_signal(i8* %ev_r)

  call void @__hike_chan_unlock(i32* %p_lock)
  ret void

full:
  call void @__hike_chan_unlock(i32* %p_lock)
  %ev_s = load i8*, i8** %p_ev_s
  call i32 @hike_event_wait(i8* %ev_s, i32 10)
  br label %try_send
}

; チャネルからの値受信 (<-ch)
define internal void @__hike_chan_recv(i8* %ch_raw, i8* %val_ptr) {
entry:
  %ch = bitcast i8* %ch_raw to %struct.__hike_chan*
  %p_lock = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 6
  %p_cnt = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 2
  %p_cap = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 1
  %p_hd = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 3
  %p_es = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 0
  %p_buf = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 5
  %p_cls = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 7
  %p_ev_r = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 8
  %p_ev_s = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 9
  br label %try_recv

try_recv:
  call void @__hike_chan_lock(i32* %p_lock)
  %cnt = load i64, i64* %p_cnt
  %has_data = icmp sgt i64 %cnt, 0
  br i1 %has_data, label %do_recv, label %empty

do_recv:
  %hd = load i64, i64* %p_hd
  %cap = load i64, i64* %p_cap
  %es = load i64, i64* %p_es
  %buf = load i8*, i8** %p_buf
  %offset = mul i64 %hd, %es
  %src = getelementptr inbounds i8, i8* %buf, i64 %offset
  call i8* @memcpy(i8* %val_ptr, i8* %src, i64 %es)

  %next_hd_raw = add i64 %hd, 1
  %next_hd = urem i64 %next_hd_raw, %cap
  store i64 %next_hd, i64* %p_hd

  %next_cnt = sub i64 %cnt, 1
  store i64 %next_cnt, i64* %p_cnt

  ; 送信待ちスレッドを起床
  %ev_s = load i8*, i8** %p_ev_s
  call i32 @hike_event_signal(i8* %ev_s)

  call void @__hike_chan_unlock(i32* %p_lock)
  ret void

empty:
  %cls = load i32, i32* %p_cls
  %is_closed = icmp ne i32 %cls, 0
  br i1 %is_closed, label %on_closed, label %wait_data

on_closed:
  ; クローズ済みで空の場合はゼロ値（null / 0）を書き込んで即座に復帰
  %es_c = load i64, i64* %p_es
  br label %zero_loop.cond

zero_loop.cond:
  %zi = phi i64 [ 0, %on_closed ], [ %zi.next, %zero_loop.body ]
  %z_cmp = icmp slt i64 %zi, %es_c
  br i1 %z_cmp, label %zero_loop.body, label %zero_loop.end

zero_loop.body:
  %z_ptr = getelementptr inbounds i8, i8* %val_ptr, i64 %zi
  store i8 0, i8* %z_ptr
  %zi.next = add i64 %zi, 1
  br label %zero_loop.cond

zero_loop.end:
  call void @__hike_chan_unlock(i32* %p_lock)
  ret void

wait_data:
  call void @__hike_chan_unlock(i32* %p_lock)
  ; 空の間は OS イベントでスリープ待機 (CPU 使用率 0%)
  %ev_r = load i8*, i8** %p_ev_r
  call i32 @hike_event_wait(i8* %ev_r, i32 10)
  br label %try_recv
}

; チャネルのクローズ (close(ch))
define internal void @__hike_chan_close(i8* %ch_raw) {
entry:
  %ch = bitcast i8* %ch_raw to %struct.__hike_chan*
  %p_lock = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 6
  %p_cls = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 7
  %p_ev_r = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 8
  %p_ev_s = getelementptr inbounds %struct.__hike_chan, %struct.__hike_chan* %ch, i32 0, i32 9

  call void @__hike_chan_lock(i32* %p_lock)
  store i32 1, i32* %p_cls

  ; 待機中の全スレッドを起床させて終了状態を検知させる
  %ev_r = load i8*, i8** %p_ev_r
  call i32 @hike_event_signal(i8* %ev_r)

  %ev_s = load i8*, i8** %p_ev_s
  call i32 @hike_event_signal(i8* %ev_s)

  call void @__hike_chan_unlock(i32* %p_lock)
  ret void
}

; ------------------------------------------------------------------------------
; String Runtime Functions (64-bit)
; ------------------------------------------------------------------------------

; Reference-count operations receive the payload pointer. The allocation
; header is stored immediately before it: capacity at -8 and refcount at -4.
define internal void @__hike_string_retain(i8* %data, i32 %offset) #0 {
entry:
  %literal = icmp slt i32 %offset, 0
  br i1 %literal, label %done, label %load
load:
  %raw = getelementptr inbounds i8, i8* %data, i64 -8
  %ref = getelementptr inbounds i8, i8* %raw, i64 4
  %ref32 = bitcast i8* %ref to i32*
  %old = load i32, i32* %ref32
  %immortal = icmp eq i32 %old, -2147483648
  br i1 %immortal, label %done, label %increment
increment:
  %next = add i32 %old, 1
  store i32 %next, i32* %ref32
  br label %done
done:
  ret void
}

define internal void @__hike_string_release(i8* %data, i32 %offset) #0 {
entry:
  %is_null = icmp eq i8* %data, null
  %literal = icmp slt i32 %offset, 0
  %skip = or i1 %is_null, %literal
  br i1 %skip, label %done, label %decrement
decrement:
  %raw = getelementptr inbounds i8, i8* %data, i64 -8
  %ref = getelementptr inbounds i8, i8* %raw, i64 4
  %ref32 = bitcast i8* %ref to i32*
  %old = load i32, i32* %ref32
  %immortal = icmp eq i32 %old, -2147483648
  br i1 %immortal, label %done, label %decrement_count
decrement_count:
  %next = sub i32 %old, 1
  store i32 %next, i32* %ref32
  %last = icmp eq i32 %next, 0
  br i1 %last, label %free_buffer, label %done
free_buffer:
  call void @free(i8* %raw)
  br label %done
done:
  ret void
}

; Return a writable string view.  A uniquely owned heap buffer is reused;
; shared or static storage is copied and the old reference is released.
define internal { i8*, i32, i32 } @__hike_string_writable(i8* %base, i32 %offset, i32 %len) #0 {
entry:
  %literal = icmp slt i32 %offset, 0
  br i1 %literal, label %copy_literal, label %load_header
load_header:
  %raw = getelementptr inbounds i8, i8* %base, i64 -8
  %ref = getelementptr inbounds i8, i8* %raw, i64 4
  %ref32 = bitcast i8* %ref to i32*
  %count = load i32, i32* %ref32
  %unique = icmp eq i32 %count, 1
  br i1 %unique, label %reuse, label %copy
copy_literal:
  %literal_size64 = zext i32 %len to i64
  %literal_alloc_size = add i64 %literal_size64, 9
  %literal_raw = call i8* @malloc(i64 %literal_alloc_size)
  %literal_cap_ptr = bitcast i8* %literal_raw to i32*
  store i32 %len, i32* %literal_cap_ptr
  %literal_ref = getelementptr inbounds i8, i8* %literal_raw, i64 4
  %literal_ref32 = bitcast i8* %literal_ref to i32*
  store i32 1, i32* %literal_ref32
  %literal_data = getelementptr inbounds i8, i8* %literal_raw, i64 8
  call i8* @memcpy(i8* %literal_data, i8* %base, i64 %literal_size64)
  %literal_nul = getelementptr inbounds i8, i8* %literal_data, i64 %literal_size64
  store i8 0, i8* %literal_nul
  %literal_copy0 = insertvalue { i8*, i32, i32 } undef, i8* %literal_data, 0
  %literal_copy1 = insertvalue { i8*, i32, i32 } %literal_copy0, i32 0, 1
  %literal_copy2 = insertvalue { i8*, i32, i32 } %literal_copy1, i32 %len, 2
  ret { i8*, i32, i32 } %literal_copy2
reuse:
  %reuse0 = insertvalue { i8*, i32, i32 } undef, i8* %base, 0
  %reuse1 = insertvalue { i8*, i32, i32 } %reuse0, i32 %offset, 1
  %reuse2 = insertvalue { i8*, i32, i32 } %reuse1, i32 %len, 2
  ret { i8*, i32, i32 } %reuse2
copy:
  %size64 = zext i32 %len to i64
  %alloc_size = add i64 %size64, 9
  %new_raw = call i8* @malloc(i64 %alloc_size)
  %cap_ptr = bitcast i8* %new_raw to i32*
  store i32 %len, i32* %cap_ptr
  %new_ref = getelementptr inbounds i8, i8* %new_raw, i64 4
  %new_ref32 = bitcast i8* %new_ref to i32*
  store i32 1, i32* %new_ref32
  %new_data = getelementptr inbounds i8, i8* %new_raw, i64 8
  %offset64 = zext i32 %offset to i64
  %src = getelementptr inbounds i8, i8* %base, i64 %offset64
  call i8* @memcpy(i8* %new_data, i8* %src, i64 %size64)
  %nul = getelementptr inbounds i8, i8* %new_data, i64 %size64
  store i8 0, i8* %nul
  %immortal = icmp eq i32 %count, -2147483648
  br i1 %immortal, label %return_copy, label %decrement_old
decrement_old:
  %old_next = sub i32 %count, 1
  %old_last = icmp eq i32 %old_next, 0
  store i32 %old_next, i32* %ref32
  br i1 %old_last, label %free_old, label %return_copy
free_old:
  call void @free(i8* %raw)
  br label %return_copy
return_copy:
  %copy0 = insertvalue { i8*, i32, i32 } undef, i8* %new_data, 0
  %copy1 = insertvalue { i8*, i32, i32 } %copy0, i32 0, 1
  %copy2 = insertvalue { i8*, i32, i32 } %copy1, i32 %len, 2
  ret { i8*, i32, i32 } %copy2
}

; Append to a string variable. Unique buffers reserve 256 bytes on the first
; append and are reused while the accumulated string fits in that capacity.
; The bootstrap compiler only requires the runtime entry point for sort.Strings
; to be linkable. The Hike stdlib currently provides ordering at the source
; level, so keep this ABI-compatible fallback as a no-op until native sorting
; is lowered directly.
define internal void @__hike_sort_strings({ i8*, i64, i64 } %items) #0 {
entry:
  ret void
}

define internal i8* @__hike_string_append(i8* %base, i32 %offset, i32 %len, i8* %b, i32 %blen) #0 {
entry:
  %total = add i32 %len, %blen
  %len64 = zext i32 %len to i64
  %blen64 = zext i32 %blen to i64
  %literal = icmp slt i32 %offset, 0
  br i1 %literal, label %copy_literal, label %load_header
load_header:
  %raw = getelementptr inbounds i8, i8* %base, i64 -8
  %cap_ptr = bitcast i8* %raw to i32*
  %cap = load i32, i32* %cap_ptr
  %ref = getelementptr inbounds i8, i8* %raw, i64 4
  %ref32 = bitcast i8* %ref to i32*
  %count = load i32, i32* %ref32
  %available = sub i32 %cap, %offset
  %fits = icmp uge i32 %available, %total
  %unique = icmp eq i32 %count, 1
  %reuse_ok = and i1 %unique, %fits
  br i1 %reuse_ok, label %reuse, label %copy
copy_literal:
  %literal_new_data = call i8* @hike_strcat_len(i8* %base, i64 %len64, i8* %b, i64 %blen64)
  ret i8* %literal_new_data
reuse:
  %dst = getelementptr inbounds i8, i8* %base, i32 %offset
  %dst_b = getelementptr inbounds i8, i8* %dst, i32 %len
  call i8* @memcpy(i8* %dst_b, i8* %b, i64 %blen64)
  %nul = getelementptr inbounds i8, i8* %dst, i32 %total
  store i8 0, i8* %nul
  ret i8* %dst
copy:
  %src = getelementptr inbounds i8, i8* %base, i32 %offset
  %new_data = call i8* @hike_strcat_len(i8* %src, i64 %len64, i8* %b, i64 %blen64)
  ; ownership: string_release var=base target=%base at runtime_common.ll:992:3
  call void @__hike_string_release(i8* %base, i32 %offset)
  ret i8* %new_data
}

; 文字列等価比較 (hike_streq: a == b) (64-bit)
define internal i1 @hike_streq(i8* %a, i8* %b) #0 {
entry:
  %eq_ptr = icmp eq i8* %a, %b
  br i1 %eq_ptr, label %ret_true, label %check_null
check_null:
  %a_null = icmp eq i8* %a, null
  %b_null = icmp eq i8* %b, null
  %either_null = or i1 %a_null, %b_null
  br i1 %either_null, label %ret_false, label %do_strcmp
do_strcmp:
  %res = call i32 @strcmp(i8* %a, i8* %b)
  %is_zero = icmp eq i32 %res, 0
  ret i1 %is_zero
ret_true:
  ret i1 true
ret_false:
  ret i1 false
}

; Length-aware string equality for fat-string views (not NUL-terminated).
define internal i1 @hike_streq_len(i8* %a, i64 %alen, i8* %b, i64 %blen) #0 {
entry:
  %same_len = icmp eq i64 %alen, %blen
  br i1 %same_len, label %compare, label %false
compare:
  %res = call i32 @memcmp(i8* %a, i8* %b, i64 %alen)
  %equal = icmp eq i32 %res, 0
  ret i1 %equal
false:
  ret i1 false
}

define internal i32 @hike_strcmp_len(i8* %a, i64 %alen, i8* %b, i64 %blen) #0 {
entry:
  %shorter = icmp ult i64 %alen, %blen
  %n = select i1 %shorter, i64 %alen, i64 %blen
  %res = call i32 @memcmp(i8* %a, i8* %b, i64 %n)
  %different = icmp ne i32 %res, 0
  br i1 %different, label %done, label %compare_len
compare_len:
  %less = icmp ult i64 %alen, %blen
  %greater = icmp ugt i64 %alen, %blen
  br i1 %less, label %ret_less, label %check_greater
check_greater:
  br i1 %greater, label %ret_greater, label %ret_equal
ret_less:
  ret i32 -1
ret_greater:
  ret i32 1
ret_equal:
  ret i32 0
done:
  ret i32 %res
}

; Length-aware concatenation for fat-string views (not NUL-terminated).
define internal i8* @hike_strcat_len(i8* %a, i64 %alen, i8* %b, i64 %blen) #0 {
entry:
  %total_len = add i64 %alen, %blen
  %double_len = shl i64 %total_len, 1
  %overflow = icmp ult i64 %double_len, %total_len
  %grown_len = select i1 %overflow, i64 %total_len, i64 %double_len
  %large = icmp ugt i64 %total_len, 256
  %capacity = select i1 %large, i64 %grown_len, i64 256
  %alloc_size = add i64 %capacity, 9
  %raw = call i8* @malloc(i64 %alloc_size)
  %cap_ptr = bitcast i8* %raw to i32*
  %capacity32 = trunc i64 %capacity to i32
  store i32 %capacity32, i32* %cap_ptr
  %ref_ptr = getelementptr inbounds i8, i8* %raw, i64 4
  %ref_ptr32 = bitcast i8* %ref_ptr to i32*
  store i32 1, i32* %ref_ptr32
  %buf = getelementptr inbounds i8, i8* %raw, i64 8
  call i8* @memcpy(i8* %buf, i8* %a, i64 %alen)
  %dst_b = getelementptr inbounds i8, i8* %buf, i64 %alen
  call i8* @memcpy(i8* %dst_b, i8* %b, i64 %blen)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i64 %total_len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; 部分文字列の切り出し (hike_substr: s[low:high]) (64-bit)
define internal i8* @hike_substr(i8* %s, i64 %low, i64 %high) #0 {
entry:
  %s_null = icmp eq i8* %s, null
  br i1 %s_null, label %ret_null, label %check_range
check_range:
  %inv = icmp slt i64 %high, %low
  br i1 %inv, label %ret_empty, label %do_sub
ret_empty:
	%empty_raw = call i8* @malloc(i64 9)
 %empty_cap = bitcast i8* %empty_raw to i32*
 store i32 0, i32* %empty_cap
	%empty_ref = getelementptr inbounds i8, i8* %empty_raw, i64 4
 %empty_ref32 = bitcast i8* %empty_ref to i32*
 store i32 1, i32* %empty_ref32
	%empty = getelementptr inbounds i8, i8* %empty_raw, i64 8
	store i8 0, i8* %empty
	ret i8* %empty
do_sub:
  %len = sub i64 %high, %low
	%alloc_size = add i64 %len, 9
	%raw = call i8* @malloc(i64 %alloc_size)
	%cap_ptr = bitcast i8* %raw to i32*
	%capacity = trunc i64 %len to i32
	store i32 %capacity, i32* %cap_ptr
	%ref_ptr = getelementptr inbounds i8, i8* %raw, i64 4
 %ref_ptr32 = bitcast i8* %ref_ptr to i32*
 store i32 1, i32* %ref_ptr32
	%buf = getelementptr inbounds i8, i8* %raw, i64 8
  %src_ptr = getelementptr inbounds i8, i8* %s, i64 %low
  call i8* @memcpy(i8* %buf, i8* %src_ptr, i64 %len)
  %null_pos = getelementptr inbounds i8, i8* %buf, i64 %len
  store i8 0, i8* %null_pos
  ret i8* %buf
ret_null:
  ret i8* null
}

; 文字列連結 (hike_strcat: a + b) (64-bit)
define internal i8* @hike_strcat(i8* %a, i8* %b) #0 {
entry:
  %len_a = call i64 @strlen(i8* %a)
  %len_b = call i64 @strlen(i8* %b)
  %total_len = add i64 %len_a, %len_b
	%alloc_size = add i64 %total_len, 9
	%raw = call i8* @malloc(i64 %alloc_size)
	%cap_ptr = bitcast i8* %raw to i32*
	%capacity = trunc i64 %total_len to i32
	store i32 %capacity, i32* %cap_ptr
	%ref_ptr = getelementptr inbounds i8, i8* %raw, i64 4
	%ref_ptr32 = bitcast i8* %ref_ptr to i32*
  store i32 1, i32* %ref_ptr32
	%buf = getelementptr inbounds i8, i8* %raw, i64 8
  call i8* @memcpy(i8* %buf, i8* %a, i64 %len_a)
  %dst_b = getelementptr inbounds i8, i8* %buf, i64 %len_a
  call i8* @memcpy(i8* %dst_b, i8* %b, i64 %len_b)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i64 %total_len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; スライス ([]byte) から null 終端文字列 (string) への安全な複製変換 (64-bit)
define internal i8* @__hike_slice_to_str(i8* %ptr, i64 %len) #0 {
entry:
  %null_chk = icmp eq i8* %ptr, null
  br i1 %null_chk, label %ret_empty, label %alloc
ret_empty:
	%empty_raw = call i8* @malloc(i64 9)
	%empty_cap = bitcast i8* %empty_raw to i32*
  store i32 0, i32* %empty_cap
	%empty_ref = getelementptr inbounds i8, i8* %empty_raw, i64 4
	%empty_ref32 = bitcast i8* %empty_ref to i32*
  store i32 1, i32* %empty_ref32
	%empty = getelementptr inbounds i8, i8* %empty_raw, i64 8
	store i8 0, i8* %empty
	ret i8* %empty
alloc:
	%alloc_size = add i64 %len, 9
	%raw = call i8* @malloc(i64 %alloc_size)
	%cap_ptr = bitcast i8* %raw to i32*
	%capacity = trunc i64 %len to i32
	store i32 %capacity, i32* %cap_ptr
	%ref_ptr = getelementptr inbounds i8, i8* %raw, i64 4
	%ref_ptr32 = bitcast i8* %ref_ptr to i32*
  store i32 1, i32* %ref_ptr32
	%buf = getelementptr inbounds i8, i8* %raw, i64 8
  call i8* @memcpy(i8* %buf, i8* %ptr, i64 %len)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i64 %len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; Hash Map Runtime Types & Functions
; ------------------------------------------------------------------------------

%struct.__hike_map_entry = type { i64, i64, i64, %struct.__hike_map_entry* }
%struct.__hike_map = type { %struct.__hike_map_entry**, i64, i64, i64 }
%struct.__hike_string_key = type { i8*, i64 }

; Return the visible start of a string view. The offset uses the same encoded
; representation as the Hike fat-string value, so literals and borrowed views
; are normalized at one runtime boundary.
define internal i8* @__hike_string_start(i8* %base, i32 %encoded_offset) {
entry:
  %sign = ashr i32 %encoded_offset, 31
  %offset = xor i32 %encoded_offset, %sign
  %start = getelementptr inbounds i8, i8* %base, i32 %offset
  ret i8* %start
}

; Create an owned descriptor for a length-aware Hike string view.  The bytes
; themselves remain owned by the string value; only the descriptor is stored
; as the map key.
define internal i64 @__hike_string_key(i8* %ptr, i64 %len) {
entry:
  %raw = call i8* @malloc(i64 16)
  %key = bitcast i8* %raw to %struct.__hike_string_key*
  %p_ptr = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %p_ptr
  %p_len = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %p_len
  %token = ptrtoint i8* %raw to i64
  ret i64 %token
}

define internal i8* @__hike_map_key_ptr(i64 %token) {
entry:
  %raw = inttoptr i64 %token to i8*
  %key = bitcast i8* %raw to %struct.__hike_string_key*
  %p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  %ptr = load i8*, i8** %p
  ret i8* %ptr
}

define internal i64 @__hike_map_key_len(i64 %token) {
entry:
  %raw = inttoptr i64 %token to i8*
  %key = bitcast i8* %raw to %struct.__hike_string_key*
  %p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  %len = load i64, i64* %p
  ret i64 %len
}

; 文字列 FNV-1a ハッシュ算出
define internal i64 @__hike_hash_str(i8* %key_raw) {
entry:
  %null_chk = icmp eq i8* %key_raw, null
  br i1 %null_chk, label %ret_zero, label %loop_init
ret_zero:
  ret i64 0
loop_init:
  %key = bitcast i8* %key_raw to %struct.__hike_string_key*
  %p_data = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  %data = load i8*, i8** %p_data
  %p_len = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  %len = load i64, i64* %p_len
  br label %loop.cond
loop.cond:
  %h = phi i64 [ -3750763034362895579, %loop_init ], [ %h.next, %loop.body ]
  %i = phi i64 [ 0, %loop_init ], [ %i.next, %loop.body ]
  %ptr = getelementptr inbounds i8, i8* %data, i64 %i
  %done = icmp uge i64 %i, %len
  br i1 %done, label %loop.end, label %loop.body
loop.body:
  %ch = load i8, i8* %ptr
  %ch.zext = zext i8 %ch to i64
  %h.xor = xor i64 %h, %ch.zext
  %h.next = mul i64 %h.xor, 1099511628211
  %i.next = add i64 %i, 1
  br label %loop.cond
loop.end:
  ret i64 %h
}

; マップキーの一致判定
define internal i1 @__hike_map_key_eq(i64 %k1, i64 %k2, i64 %is_str) {
entry:
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %check_str, label %check_int
check_int:
  %eq_int = icmp eq i64 %k1, %k2
  ret i1 %eq_int
check_str:
  %p1 = inttoptr i64 %k1 to i8*
  %p2 = inttoptr i64 %k2 to i8*
  %n1 = icmp eq i8* %p1, null
  %n2 = icmp eq i8* %p2, null
  %either_null = or i1 %n1, %n2
  br i1 %either_null, label %check_both_null, label %load_keys
check_both_null:
  %both_null = and i1 %n1, %n2
  ret i1 %both_null
load_keys:
  %key1 = bitcast i8* %p1 to %struct.__hike_string_key*
  %key2 = bitcast i8* %p2 to %struct.__hike_string_key*
  %d1p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key1, i32 0, i32 0
  %d2p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key2, i32 0, i32 0
  %d1 = load i8*, i8** %d1p
  %d2 = load i8*, i8** %d2p
  %l1p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key1, i32 0, i32 1
  %l2p = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key2, i32 0, i32 1
  %l1 = load i64, i64* %l1p
  %l2 = load i64, i64* %l2p
  %len_eq = icmp eq i64 %l1, %l2
  br i1 %len_eq, label %compare_bytes, label %ret_false
compare_bytes:
  %res = call i32 @memcmp(i8* %d1, i8* %d2, i64 %l1)
  %is_z = icmp eq i32 %res, 0
  ret i1 %is_z
ret_false:
  ret i1 false
}

; マップの新規生成
define internal %struct.__hike_map* @__hike_map_create(i64 %cap, i64 %is_str) {
entry:
  %raw = call i8* @malloc(i64 32)
  %m = bitcast i8* %raw to %struct.__hike_map*
  %buckets_raw = call i8* @calloc(i64 16, i64 8)
  %buckets = bitcast i8* %buckets_raw to %struct.__hike_map_entry**
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  store %struct.__hike_map_entry** %buckets, %struct.__hike_map_entry*** %p_b
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  store i64 16, i64* %p_nb
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  store i64 0, i64* %p_len
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  store i64 %is_str, i64* %p_str
  ret %struct.__hike_map* %m
}

; マップバケットの拡張と再ハッシュ
define internal void @__hike_map_grow(%struct.__hike_map* %m) {
entry:
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %old_nb = load i64, i64* %p_nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %old_b = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %new_nb = mul i64 %old_nb, 2
  %new_raw = call i8* @calloc(i64 %new_nb, i64 8)
  %new_b = bitcast i8* %new_raw to %struct.__hike_map_entry**
  br label %loop.i
loop.i:
  %i = phi i64 [ 0, %entry ], [ %i.next, %loop.i.inc ]
  %cmp.i = icmp slt i64 %i, %old_nb
  br i1 %cmp.i, label %loop.entry.init, label %loop.i.done
loop.entry.init:
  %p_cur_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %old_b, i64 %i
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_cur_head
  br label %loop.entry
loop.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %loop.entry.init ], [ %nxt, %loop.entry.body ]
  %has_cur = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_cur, label %loop.entry.body, label %loop.i.inc
loop.entry.body:
  %p_nxt = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %nxt = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_nxt
  %p_hash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %h_val = load i64, i64* %p_hash
  %h_pos = and i64 %h_val, 9223372036854775807
  %new_idx = urem i64 %h_pos, %new_nb
  %p_new_slot = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %new_b, i64 %new_idx
  %existing = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_new_slot
  store %struct.__hike_map_entry* %existing, %struct.__hike_map_entry** %p_nxt
  store %struct.__hike_map_entry* %cur, %struct.__hike_map_entry** %p_new_slot
  br label %loop.entry
loop.i.inc:
  %i.next = add i64 %i, 1
  br label %loop.i
loop.i.done:
  %old_b_raw = bitcast %struct.__hike_map_entry** %old_b to i8*
  call void @free(i8* %old_b_raw)
  store %struct.__hike_map_entry** %new_b, %struct.__hike_map_entry*** %p_b
  store i64 %new_nb, i64* %p_nb
  ret void
}

; マップへのキー・値格納
define internal void @__hike_map_set(%struct.__hike_map* %m, i64 %key, i64 %val) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_void, label %check_grow
ret_void:
  ret void
check_grow:
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %cur_len = load i64, i64* %p_len
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %limit = mul i64 %nb, 3
  %limit_div = sdiv i64 %limit, 4
  %needs_grow = icmp sge i64 %cur_len, %limit_div
  br i1 %needs_grow, label %do_grow, label %do_hash
do_grow:
  call void @__hike_map_grow(%struct.__hike_map* %m)
  br label %do_hash
do_hash:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %lookup
hash_int:
  br label %lookup
lookup:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb_2 = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb_cur = load i64, i64* %p_nb_2
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb_cur
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %lookup ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %insert_new
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %update_val, label %search.next
update_val:
  %p_eval = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 2
  store i64 %val, i64* %p_eval
  ret void
search.next:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  br label %search.entry
insert_new:
  %new_entry_raw = call i8* @malloc(i64 32)
  %new_e = bitcast i8* %new_entry_raw to %struct.__hike_map_entry*
  %np_hash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 0
  store i64 %hash, i64* %np_hash
  %np_key = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 1
  store i64 %key, i64* %np_key
  %np_val = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 2
  store i64 %val, i64* %np_val
  %np_next = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 3
  %cur_head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  store %struct.__hike_map_entry* %cur_head, %struct.__hike_map_entry** %np_next
  store %struct.__hike_map_entry* %new_e, %struct.__hike_map_entry** %p_head
  %new_len = add i64 %cur_len, 1
  store i64 %new_len, i64* %p_len
  ret void
}

; マップからの値取得
define internal i1 @__hike_map_get(%struct.__hike_map* %m, i64 %key, i64* %out_val) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_not_found, label %do_lookup
ret_not_found:
  store i64 0, i64* %out_val
  ret i1 false
do_lookup:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %search_init
hash_int:
  br label %search_init
search_init:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %search_init ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %ret_not_found
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %found, label %search.next
found:
  %p_eval = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 2
  %val = load i64, i64* %p_eval
  store i64 %val, i64* %out_val
  ret i1 true
search.next:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  br label %search.entry
}

; Boxed map lookup. A missing key receives the caller-provided zero value
; pointer so composite values can be read without dereferencing null.
define internal void @__hike_map_get_boxed(%struct.__hike_map* %m, i64 %key, i64* %out_val, i8* %zero_ptr) {
entry:
  %found = call i1 @__hike_map_get(%struct.__hike_map* %m, i64 %key, i64* %out_val)
  br i1 %found, label %done, label %missing
missing:
  %zero_val = ptrtoint i8* %zero_ptr to i64
  store i64 %zero_val, i64* %out_val
  br label %done
done:
  ret void
}

define internal i1 @__hike_map_get_boxed_ok(%struct.__hike_map* %m, i64 %key, i64* %out_val, i8* %zero_ptr) {
entry:
  %found = call i1 @__hike_map_get(%struct.__hike_map* %m, i64 %key, i64* %out_val)
  br i1 %found, label %done, label %missing
missing:
  %zero_val = ptrtoint i8* %zero_ptr to i64
  store i64 %zero_val, i64* %out_val
  br label %done
done:
  ret i1 %found
}

; String-key map ABI. The lookup boundary receives the visible start pointer
; and length directly; only the stored entry uses an owned key descriptor.
define internal void @__hike_map_set_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64 %val) {
entry:
  %key = call i64 @__hike_string_key(i8* %ptr, i64 %len)
  call void @__hike_map_set(%struct.__hike_map* %m, i64 %key, i64 %val)
  ret void
}

define internal i1 @__hike_map_get_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out_val) {
entry:
  %key = alloca %struct.__hike_string_key
  %key_ptr = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %key_ptr
  %key_len = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %key_len
  %token = ptrtoint %struct.__hike_string_key* %key to i64
  %found = call i1 @__hike_map_get(%struct.__hike_map* %m, i64 %token, i64* %out_val)
  ret i1 %found
}

define internal void @__hike_map_get_boxed_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out_val, i8* %zero_ptr) {
entry:
  %found = call i1 @__hike_map_get_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out_val)
  br i1 %found, label %done, label %missing
missing:
  %zero_val = ptrtoint i8* %zero_ptr to i64
  store i64 %zero_val, i64* %out_val
  br label %done
done:
  ret void
}

define internal i1 @__hike_map_get_boxed_str_ok(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out_val, i8* %zero_ptr) {
entry:
  %found = call i1 @__hike_map_get_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out_val)
  br i1 %found, label %done, label %missing
missing:
  %zero_val = ptrtoint i8* %zero_ptr to i64
  store i64 %zero_val, i64* %out_val
  br label %done
done:
  ret i1 %found
}

define internal void @__hike_map_delete_str(%struct.__hike_map* %m, i8* %ptr, i64 %len) {
entry:
  %key = alloca %struct.__hike_string_key
  %key_ptr = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %key_ptr
  %key_len = getelementptr inbounds %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %key_len
  %token = ptrtoint %struct.__hike_string_key* %key to i64
  call void @__hike_map_delete(%struct.__hike_map* %m, i64 %token)
  ret void
}

define internal i32 @__hike_iface_typeid(i8* %itab) {
entry:
  %is_null = icmp eq i8* %itab, null
  br i1 %is_null, label %missing, label %present
missing:
  ret i32 0
present:
  %type_ptr = bitcast i8* %itab to i32*
  %type_id = load i32, i32* %type_ptr
  ret i32 %type_id
}

; マップ要素の削除
define internal void @__hike_map_delete(%struct.__hike_map* %m, i64 %key) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_void, label %do_del
ret_void:
  ret void
do_del:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %search_init
hash_int:
  br label %search_init
search_init:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %prev = phi %struct.__hike_map_entry* [ null, %search_init ], [ %cur, %search.next ]
  %cur = phi %struct.__hike_map_entry* [ %head, %search_init ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %ret_void
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %do_unlink, label %search.next
do_unlink:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %nxt = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  %has_prev = icmp ne %struct.__hike_map_entry* %prev, null
  br i1 %has_prev, label %unlink_prev, label %unlink_head
unlink_prev:
  %p_pnext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %prev, i32 0, i32 3
  store %struct.__hike_map_entry* %nxt, %struct.__hike_map_entry** %p_pnext
  br label %after_unlink
unlink_head:
  store %struct.__hike_map_entry* %nxt, %struct.__hike_map_entry** %p_head
  br label %after_unlink
after_unlink:
  %cur_raw = bitcast %struct.__hike_map_entry* %cur to i8*
  call void @free(i8* %cur_raw)
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %cur_len = load i64, i64* %p_len
  %new_len = sub i64 %cur_len, 1
  store i64 %new_len, i64* %p_len
  ret void
search.next:
  %p_enext2 = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext2
  br label %search.entry
}

; マップ要素数の取得
define internal i64 @__hike_map_len(%struct.__hike_map* %m) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_zero, label %get_len
ret_zero:
  ret i64 0
get_len:
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %l = load i64, i64* %p_len
  ret i64 %l
}

; ------------------------------------------------------------------------------
; Attributes
; ------------------------------------------------------------------------------

; Compatibility wrappers keep the source-level MapType pointer ABI stable while
; routing 64-bit lowering through the compact representation above.
define internal %struct.__hike_map* @__hike_cdict_create(i64 %cap, i64 %is_str) {
entry:
  %m = call %struct.__hike_compact_map* @__hike_compact_map_create(i64 %cap, i64 %is_str)
  %state_raw = bitcast %struct.__hike_compact_map* %m to i8*
  %legacy_raw = getelementptr i8, i8* %state_raw, i64 -32
  %legacy = bitcast i8* %legacy_raw to %struct.__hike_map*
  ret %struct.__hike_map* %legacy
}

define internal %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m) {
entry:
  %raw = bitcast %struct.__hike_map* %m to i8*
  %state_raw = getelementptr i8, i8* %raw, i64 32
  %state = bitcast i8* %state_raw to %struct.__hike_compact_map*
  ret %struct.__hike_compact_map* %state
}

define internal void @__hike_cdict_set(%struct.__hike_map* %m, i64 %key, i64 %value) {
entry:
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  call void @__hike_compact_map_set(%struct.__hike_compact_map* %compact, i64 %key, i64 %key, i64 %value)
  ret void
}

define internal i1 @__hike_cdict_get(%struct.__hike_map* %m, i64 %key, i64* %out) {
entry:
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %found = call i1 @__hike_compact_map_get(%struct.__hike_compact_map* %compact, i64 %key, i64 %key, i64* %out)
  ret i1 %found
}

define internal void @__hike_cdict_set_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64 %value) {
entry:
  %key = call i64 @__hike_string_key(i8* %ptr, i64 %len)
  %key_ptr = inttoptr i64 %key to i8*
  %hash = call i64 @__hike_hash_str(i8* %key_ptr)
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  call void @__hike_compact_map_set(%struct.__hike_compact_map* %compact, i64 %key, i64 %hash, i64 %value)
  ret void
}

define internal void @__hike_cdict_set_str_hash(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64 %hash, i64 %value) {
entry:
  %key = call i64 @__hike_string_key(i8* %ptr, i64 %len)
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  call void @__hike_compact_map_set(%struct.__hike_compact_map* %compact, i64 %key, i64 %hash, i64 %value)
  ret void
}

define internal i1 @__hike_cdict_get_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out) {
entry:
  %key = alloca %struct.__hike_string_key
  %p_ptr = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %p_ptr
  %p_len = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %p_len
  %token = ptrtoint %struct.__hike_string_key* %key to i64
  %hash = call i64 @__hike_hash_str(i8* %key)
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %found = call i1 @__hike_compact_map_get(%struct.__hike_compact_map* %compact, i64 %token, i64 %hash, i64* %out)
  ret i1 %found
}

define internal i1 @__hike_cdict_get_boxed(%struct.__hike_map* %m, i64 %key, i64* %out, i8* %zero) {
entry:
  %found = call i1 @__hike_cdict_get(%struct.__hike_map* %m, i64 %key, i64* %out)
  br i1 %found, label %done, label %missing
missing:
  %z = ptrtoint i8* %zero to i64
  store i64 %z, i64* %out
  br label %done
done:
  ret i1 %found
}

define internal i1 @__hike_cdict_get_boxed_ok(%struct.__hike_map* %m, i64 %key, i64* %out, i8* %zero) {
entry:
  %found = call i1 @__hike_cdict_get_boxed(%struct.__hike_map* %m, i64 %key, i64* %out, i8* %zero)
  ret i1 %found
}

define internal i1 @__hike_cdict_get_boxed_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out, i8* %zero) {
entry:
  %found = call i1 @__hike_cdict_get_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out)
  br i1 %found, label %done, label %missing
missing:
  %z = ptrtoint i8* %zero to i64
  store i64 %z, i64* %out
  br label %done
done:
  ret i1 %found
}

define internal i1 @__hike_cdict_get_boxed_str_ok(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out, i8* %zero) {
entry:
  %found = call i1 @__hike_cdict_get_boxed_str(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64* %out, i8* %zero)
  ret i1 %found
}

define internal void @__hike_cdict_delete(%struct.__hike_map* %m, i64 %key) {
entry:
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %ignored = call i1 @__hike_compact_map_delete(%struct.__hike_compact_map* %compact, i64 %key, i64 %key)
  ret void
}

define internal void @__hike_cdict_delete_str(%struct.__hike_map* %m, i8* %ptr, i64 %len) {
entry:
  %key = alloca %struct.__hike_string_key
  %p_ptr = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %p_ptr
  %p_len = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %p_len
  %token = ptrtoint %struct.__hike_string_key* %key to i64
  %hash = call i64 @__hike_hash_str(i8* %key)
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %ignored = call i1 @__hike_compact_map_delete(%struct.__hike_compact_map* %compact, i64 %token, i64 %hash)
  ret void
}

define internal void @__hike_cdict_delete_str_hash(%struct.__hike_map* %m, i8* %ptr, i64 %len, i64 %hash) {
entry:
  %key = alloca %struct.__hike_string_key
  %p_ptr = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 0
  store i8* %ptr, i8** %p_ptr
  %p_len = getelementptr %struct.__hike_string_key, %struct.__hike_string_key* %key, i32 0, i32 1
  store i64 %len, i64* %p_len
  %token = ptrtoint %struct.__hike_string_key* %key to i64
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %ignored = call i1 @__hike_compact_map_delete(%struct.__hike_compact_map* %compact, i64 %token, i64 %hash)
  ret void
}

define internal i64 @__hike_cdict_len(%struct.__hike_map* %m) {
entry:
  %compact = call %struct.__hike_compact_map* @__hike_cdict_state(%struct.__hike_map* %m)
  %len = call i64 @__hike_compact_map_len(%struct.__hike_compact_map* %compact)
  ret i64 %len
}

; Compact Dict ABI.  indices stores entry numbers (-1 empty, -2 deleted),
; while entries remains dense and is kept in insertion order by the lowering.
%struct.__hike_compact_map_entry = type { i64, i64, i64, i64, i64 }
%struct.__hike_compact_map = type { i64*, %struct.__hike_compact_map_entry*, i64, i64, i64, i64, i64 }

define internal void @__hike_cdict_legacy_append(%struct.__hike_compact_map* %m, %struct.__hike_compact_map_entry* %new_entry) {
entry:
  %state_raw = bitcast %struct.__hike_compact_map* %m to i8*
  %legacy_raw = getelementptr i8, i8* %state_raw, i64 -32
  %legacy = bitcast i8* %legacy_raw to %struct.__hike_map*
  %p_buckets = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_buckets
  %p_head = getelementptr %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 0
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  %entry_legacy = bitcast %struct.__hike_compact_map_entry* %new_entry to %struct.__hike_map_entry*
  %p_entry_next = getelementptr %struct.__hike_map_entry, %struct.__hike_map_entry* %entry_legacy, i32 0, i32 3
  store %struct.__hike_map_entry* null, %struct.__hike_map_entry** %p_entry_next
  %has_head = icmp ne %struct.__hike_map_entry* %head, null
  br i1 %has_head, label %find_tail, label %store_head
store_head:
  store %struct.__hike_map_entry* %entry_legacy, %struct.__hike_map_entry** %p_head
  ret void
find_tail:
  br label %tail_loop
tail_loop:
  %current = phi %struct.__hike_map_entry* [ %head, %find_tail ], [ %next, %tail_next ]
  %p_current_next = getelementptr %struct.__hike_map_entry, %struct.__hike_map_entry* %current, i32 0, i32 3
  %next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_current_next
  %has_next = icmp ne %struct.__hike_map_entry* %next, null
  br i1 %has_next, label %tail_next, label %store_tail
tail_next:
  br label %tail_loop
store_tail:
  store %struct.__hike_map_entry* %entry_legacy, %struct.__hike_map_entry** %p_current_next
  ret void
}

define internal i1 @__hike_compact_key_eq(%struct.__hike_compact_map* %m, i64 %left, i64 %right) {
entry:
  %p_str = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 5
  %is_str = load i64, i64* %p_str
  %same_kind = icmp ne i64 %is_str, 0
  br i1 %same_kind, label %string_key, label %integer_key
integer_key:
  %same_int = icmp eq i64 %left, %right
  ret i1 %same_int
string_key:
  %left_ptr = inttoptr i64 %left to i8*
  %right_ptr = inttoptr i64 %right to i8*
  %same_string = call i1 @__hike_map_key_eq(i64 %left, i64 %right, i64 1)
  ret i1 %same_string
}

define internal %struct.__hike_compact_map* @__hike_compact_map_create(i64 %requested_cap, i64 %is_str) {
entry:
  %cap_ok = icmp uge i64 %requested_cap, 8
  %cap = select i1 %cap_ok, i64 %requested_cap, i64 8
  %map_raw = call i8* @malloc(i64 88)
  %state_raw = getelementptr i8, i8* %map_raw, i64 32
  %map = bitcast i8* %state_raw to %struct.__hike_compact_map*
  %index_bytes = mul i64 %cap, 8
  %index_raw = call i8* @malloc(i64 %index_bytes)
  %indices = bitcast i8* %index_raw to i64*
  %entry_bytes = mul i64 %cap, 40
  %entry_raw = call i8* @malloc(i64 %entry_bytes)
  %entries = bitcast i8* %entry_raw to %struct.__hike_compact_map_entry*
  %legacy = bitcast i8* %map_raw to %struct.__hike_map*
  %bucket_raw = call i8* @calloc(i64 1, i64 8)
  %legacy_buckets = bitcast i8* %bucket_raw to %struct.__hike_map_entry**
  %legacy_b = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy, i32 0, i32 0
  store %struct.__hike_map_entry** %legacy_buckets, %struct.__hike_map_entry*** %legacy_b
  %legacy_cap = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy, i32 0, i32 1
  store i64 1, i64* %legacy_cap
  %legacy_len = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy, i32 0, i32 2
  store i64 0, i64* %legacy_len
  %legacy_str = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy, i32 0, i32 3
  store i64 %is_str, i64* %legacy_str
  br label %init
init:
  %i = phi i64 [ 0, %entry ], [ %next, %init_body ]
  %done = icmp uge i64 %i, %cap
  br i1 %done, label %store_map, label %init_body
init_body:
  %p = getelementptr i64, i64* %indices, i64 %i
  store i64 -1, i64* %p
  %next = add i64 %i, 1
  br label %init
store_map:
  %p_indices = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 0
  store i64* %indices, i64** %p_indices
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 1
  store %struct.__hike_compact_map_entry* %entries, %struct.__hike_compact_map_entry** %p_entries
  %p_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 2
  store i64 %cap, i64* %p_cap
  %p_len = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 3
  store i64 0, i64* %p_len
  %p_live = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 4
  store i64 0, i64* %p_live
  %p_str = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 5
  store i64 %is_str, i64* %p_str
  %p_entry_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %map, i32 0, i32 6
  store i64 %cap, i64* %p_entry_cap
  ret %struct.__hike_compact_map* %map
}

define internal i1 @__hike_compact_map_get(%struct.__hike_compact_map* %m, i64 %key, i64 %hash, i64* %out) {
entry:
  store i64 0, i64* %out
  %null_m = icmp eq %struct.__hike_compact_map* %m, null
  br i1 %null_m, label %not_found, label %load_map
load_map:
  %p_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 2
  %cap = load i64, i64* %p_cap
  %p_indices = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 0
  %indices = load i64*, i64** %p_indices
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 1
  %entries = load %struct.__hike_compact_map_entry*, %struct.__hike_compact_map_entry** %p_entries
  %start = urem i64 %hash, %cap
  br label %probe
probe:
  %slot = phi i64 [ %start, %load_map ], [ %next_slot, %advance ]
  %tries = phi i64 [ 0, %load_map ], [ %next_try, %advance ]
  %p_index = getelementptr i64, i64* %indices, i64 %slot
  %entry_index = load i64, i64* %p_index
  %empty = icmp eq i64 %entry_index, -1
  br i1 %empty, label %not_found, label %check_deleted
check_deleted:
  %deleted = icmp eq i64 %entry_index, -2
  br i1 %deleted, label %next, label %check_entry
check_entry:
  %entry_ptr = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entries, i64 %entry_index
  %p_hash = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 0
  %stored_hash = load i64, i64* %p_hash
  %hash_match = icmp eq i64 %stored_hash, %hash
  br i1 %hash_match, label %check_key, label %next
check_key:
  %p_key = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 1
  %stored_key = load i64, i64* %p_key
  %key_match = call i1 @__hike_compact_key_eq(%struct.__hike_compact_map* %m, i64 %stored_key, i64 %key)
  br i1 %key_match, label %found, label %next
found:
  %p_value = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 2
  %value = load i64, i64* %p_value
  store i64 %value, i64* %out
  ret i1 true
next:
  %next_try = add i64 %tries, 1
  %exhausted = icmp uge i64 %next_try, %cap
  br i1 %exhausted, label %not_found, label %advance
advance:
  %raw_next = add i64 %slot, 1
  %wrapped = icmp uge i64 %raw_next, %cap
  %next_slot = select i1 %wrapped, i64 0, i64 %raw_next
  br label %probe
not_found:
  ret i1 false
}

define internal void @__hike_compact_map_grow(%struct.__hike_compact_map* %m) {
entry:
  %p_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 2
  %old_cap = load i64, i64* %p_cap
  %new_cap = mul i64 %old_cap, 2
  %p_indices = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 0
  %old_indices = load i64*, i64** %p_indices
  %new_index_bytes = mul i64 %new_cap, 8
  %new_index_raw = call i8* @malloc(i64 %new_index_bytes)
  %new_indices = bitcast i8* %new_index_raw to i64*
  br label %init_indices
init_indices:
  %ii = phi i64 [ 0, %entry ], [ %ii_next, %init_body ]
  %ii_done = icmp uge i64 %ii, %new_cap
  br i1 %ii_done, label %copy_entries, label %init_body
init_body:
  %ip = getelementptr i64, i64* %new_indices, i64 %ii
  store i64 -1, i64* %ip
  %ii_next = add i64 %ii, 1
  br label %init_indices
copy_entries:
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 1
  %old_entries = load %struct.__hike_compact_map_entry*, %struct.__hike_compact_map_entry** %p_entries
  %old_entry_bytes = mul i64 %old_cap, 40
  %new_entry_bytes = mul i64 %new_cap, 40
  %new_entry_raw = call i8* @malloc(i64 %new_entry_bytes)
  %new_entries = bitcast i8* %new_entry_raw to %struct.__hike_compact_map_entry*
  %old_entries_raw = bitcast %struct.__hike_compact_map_entry* %old_entries to i8*
  call i8* @memcpy(i8* %new_entry_raw, i8* %old_entries_raw, i64 %old_entry_bytes)
  %p_len = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 3
  %entry_len = load i64, i64* %p_len
  %state_raw_grow = bitcast %struct.__hike_compact_map* %m to i8*
  %legacy_raw_grow = getelementptr i8, i8* %state_raw_grow, i64 -32
  %legacy_grow = bitcast i8* %legacy_raw_grow to %struct.__hike_map*
  %p_buckets_grow = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy_grow, i32 0, i32 0
  %buckets_grow = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_buckets_grow
  %p_head_grow = getelementptr %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets_grow, i64 0
  store %struct.__hike_map_entry* null, %struct.__hike_map_entry** %p_head_grow
  br label %rehash
rehash:
  %ri = phi i64 [ 0, %copy_entries ], [ %ri_next, %rehash_next ]
  %ri_done = icmp uge i64 %ri, %entry_len
  br i1 %ri_done, label %finish, label %rehash_body
rehash_body:
  %old_ep = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %new_entries, i64 %ri
  %state_p = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %old_ep, i32 0, i32 4
  %state = load i64, i64* %state_p
  %live = icmp ne i64 %state, 0
  br i1 %live, label %rehash_live, label %rehash_next
rehash_live:
  %hash_p = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %old_ep, i32 0, i32 0
  %hash = load i64, i64* %hash_p
  call void @__hike_cdict_legacy_append(%struct.__hike_compact_map* %m, %struct.__hike_compact_map_entry* %old_ep)
  %slot = urem i64 %hash, %new_cap
  br label %find_slot
find_slot:
  %fs = phi i64 [ %slot, %rehash_live ], [ %fs_next, %find_next ]
  %fsp = getelementptr i64, i64* %new_indices, i64 %fs
  %fsv = load i64, i64* %fsp
  %free_slot = icmp eq i64 %fsv, -1
  br i1 %free_slot, label %store_slot, label %find_next
find_next:
  %fs_raw = add i64 %fs, 1
  %fs_wrap = icmp uge i64 %fs_raw, %new_cap
  %fs_next = select i1 %fs_wrap, i64 0, i64 %fs_raw
  br label %find_slot
store_slot:
  store i64 %ri, i64* %fsp
  br label %rehash_next
rehash_next:
  %ri_next = add i64 %ri, 1
  br label %rehash
finish:
  %old_indices_raw = bitcast i64* %old_indices to i8*
  call void @free(i8* %old_indices_raw)
  call void @free(i8* %old_entries_raw)
  store i64* %new_indices, i64** %p_indices
  store %struct.__hike_compact_map_entry* %new_entries, %struct.__hike_compact_map_entry** %p_entries
  store i64 %new_cap, i64* %p_cap
  %p_entry_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 6
  store i64 %new_cap, i64* %p_entry_cap
  ret void
}

define internal void @__hike_compact_map_set(%struct.__hike_compact_map* %m, i64 %key, i64 %hash, i64 %value) {
entry:
  %null_m = icmp eq %struct.__hike_compact_map* %m, null
  br i1 %null_m, label %done, label %load_map
load_map:
  %p_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 2
  %cap = load i64, i64* %p_cap
  %p_indices = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 0
  %indices = load i64*, i64** %p_indices
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 1
  %entries = load %struct.__hike_compact_map_entry*, %struct.__hike_compact_map_entry** %p_entries
  %p_len = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 3
  %entry_len = load i64, i64* %p_len
  %p_live = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 4
  %live = load i64, i64* %p_live
  %live4 = mul i64 %live, 4
  %cap3 = mul i64 %cap, 3
  %needs_load_grow = icmp uge i64 %live4, %cap3
  %p_entry_cap_check = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 6
  %entry_cap_check = load i64, i64* %p_entry_cap_check
  %entry_full = icmp uge i64 %entry_len, %entry_cap_check
  %needs_grow = or i1 %needs_load_grow, %entry_full
  br i1 %needs_grow, label %grow, label %begin_probe
grow:
  call void @__hike_compact_map_grow(%struct.__hike_compact_map* %m)
  br label %load_map
begin_probe:
  %start = urem i64 %hash, %cap
  br label %probe
probe:
  %slot = phi i64 [ %start, %begin_probe ], [ %next_slot, %advance ]
  %tries = phi i64 [ 0, %begin_probe ], [ %next_try, %advance ]
  %p_index = getelementptr i64, i64* %indices, i64 %slot
  %entry_index = load i64, i64* %p_index
  %empty = icmp eq i64 %entry_index, -1
  %deleted = icmp eq i64 %entry_index, -2
  br i1 %empty, label %insert, label %check_deleted
check_deleted:
  br i1 %deleted, label %insert, label %check_entry
check_entry:
  %entry_ptr = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entries, i64 %entry_index
  %p_hash = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 0
  %stored_hash = load i64, i64* %p_hash
  %hash_match = icmp eq i64 %stored_hash, %hash
  br i1 %hash_match, label %check_key, label %next
check_key:
  %p_key = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 1
  %stored_key = load i64, i64* %p_key
  %key_match = call i1 @__hike_compact_key_eq(%struct.__hike_compact_map* %m, i64 %stored_key, i64 %key)
  br i1 %key_match, label %update, label %next
update:
  %p_value = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 2
  store i64 %value, i64* %p_value
  br label %done
insert:
  %entry_ptr_new = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entries, i64 %entry_len
  %p_hash_new = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr_new, i32 0, i32 0
  store i64 %hash, i64* %p_hash_new
  %p_key_new = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr_new, i32 0, i32 1
  store i64 %key, i64* %p_key_new
  %p_value_new = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr_new, i32 0, i32 2
  store i64 %value, i64* %p_value_new
  %p_state_new = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr_new, i32 0, i32 4
  store i64 1, i64* %p_state_new
  %state_raw_new = bitcast %struct.__hike_compact_map* %m to i8*
  %legacy_raw_new = getelementptr i8, i8* %state_raw_new, i64 -32
  %legacy_new = bitcast i8* %legacy_raw_new to %struct.__hike_map*
  call void @__hike_cdict_legacy_append(%struct.__hike_compact_map* %m, %struct.__hike_compact_map_entry* %entry_ptr_new)
  store i64 %entry_len, i64* %p_index
  %next_len = add i64 %entry_len, 1
  store i64 %next_len, i64* %p_len
  %p_live2 = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 4
  %live2 = load i64, i64* %p_live2
  %next_live2 = add i64 %live2, 1
  store i64 %next_live2, i64* %p_live2
  %p_legacy_len = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy_new, i32 0, i32 2
  store i64 %next_live2, i64* %p_legacy_len
  br label %done
next:
  %next_try = add i64 %tries, 1
  %exhausted = icmp uge i64 %next_try, %cap
  br i1 %exhausted, label %done, label %advance
advance:
  %raw_next = add i64 %slot, 1
  %wrapped = icmp uge i64 %raw_next, %cap
  %next_slot = select i1 %wrapped, i64 0, i64 %raw_next
  br label %probe
done:
  ret void
}

define internal i1 @__hike_compact_map_delete(%struct.__hike_compact_map* %m, i64 %key, i64 %hash) {
entry:
  %null_m = icmp eq %struct.__hike_compact_map* %m, null
  br i1 %null_m, label %not_found, label %load_map
load_map:
  %p_cap = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 2
  %cap = load i64, i64* %p_cap
  %p_indices = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 0
  %indices = load i64*, i64** %p_indices
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 1
  %entries = load %struct.__hike_compact_map_entry*, %struct.__hike_compact_map_entry** %p_entries
  %start = urem i64 %hash, %cap
  br label %probe
probe:
  %slot = phi i64 [ %start, %load_map ], [ %next_slot, %advance ]
  %tries = phi i64 [ 0, %load_map ], [ %next_try, %advance ]
  %p_index = getelementptr i64, i64* %indices, i64 %slot
  %entry_index = load i64, i64* %p_index
  %empty = icmp eq i64 %entry_index, -1
  br i1 %empty, label %not_found, label %check_deleted
check_deleted:
  %deleted = icmp eq i64 %entry_index, -2
  br i1 %deleted, label %next, label %check_entry
check_entry:
  %entry_ptr = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entries, i64 %entry_index
  %p_hash = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 0
  %stored_hash = load i64, i64* %p_hash
  %hash_match = icmp eq i64 %stored_hash, %hash
  br i1 %hash_match, label %check_key, label %next
check_key:
  %p_key = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 1
  %stored_key = load i64, i64* %p_key
  %key_match = call i1 @__hike_compact_key_eq(%struct.__hike_compact_map* %m, i64 %stored_key, i64 %key)
  br i1 %key_match, label %remove, label %next
remove:
  store i64 -2, i64* %p_index
  %p_state = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 4
  store i64 0, i64* %p_state
  %p_live = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 4
  %live = load i64, i64* %p_live
  %new_live = sub i64 %live, 1
  store i64 %new_live, i64* %p_live
  %state_raw_del = bitcast %struct.__hike_compact_map* %m to i8*
  %legacy_raw_del = getelementptr i8, i8* %state_raw_del, i64 -32
  %legacy_del = bitcast i8* %legacy_raw_del to %struct.__hike_map*
  %p_buckets_del = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy_del, i32 0, i32 0
  %buckets_del = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_buckets_del
  %p_head_del = getelementptr %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets_del, i64 0
  %head_del = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head_del
  br label %unlink_loop
unlink_loop:
  %prev_del = phi %struct.__hike_map_entry* [ null, %remove ], [ %cur_del, %unlink_advance ]
  %cur_del = phi %struct.__hike_map_entry* [ %head_del, %remove ], [ %next_del, %unlink_advance ]
  %has_cur_del = icmp ne %struct.__hike_map_entry* %cur_del, null
  br i1 %has_cur_del, label %unlink_check, label %unlink_done
unlink_check:
  %cur_compact_del = bitcast %struct.__hike_map_entry* %cur_del to %struct.__hike_compact_map_entry*
  %same_entry_del = icmp eq %struct.__hike_compact_map_entry* %cur_compact_del, %entry_ptr
  br i1 %same_entry_del, label %unlink_found, label %unlink_advance
unlink_advance:
  %p_next_del = getelementptr %struct.__hike_map_entry, %struct.__hike_map_entry* %cur_del, i32 0, i32 3
  %next_del = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_next_del
  br label %unlink_loop
unlink_found:
  %p_entry_next_del = getelementptr %struct.__hike_map_entry, %struct.__hike_map_entry* %cur_del, i32 0, i32 3
  %entry_next_del = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_entry_next_del
  %has_prev_del = icmp ne %struct.__hike_map_entry* %prev_del, null
  br i1 %has_prev_del, label %unlink_prev, label %unlink_head
unlink_prev:
  %p_prev_next_del = getelementptr %struct.__hike_map_entry, %struct.__hike_map_entry* %prev_del, i32 0, i32 3
  store %struct.__hike_map_entry* %entry_next_del, %struct.__hike_map_entry** %p_prev_next_del
  br label %unlink_done
unlink_head:
  store %struct.__hike_map_entry* %entry_next_del, %struct.__hike_map_entry** %p_head_del
  br label %unlink_done
unlink_done:
  %p_legacy_len_del = getelementptr %struct.__hike_map, %struct.__hike_map* %legacy_del, i32 0, i32 2
  store i64 %new_live, i64* %p_legacy_len_del
  ret i1 true
next:
  %next_try = add i64 %tries, 1
  %exhausted = icmp uge i64 %next_try, %cap
  br i1 %exhausted, label %not_found, label %advance
advance:
  %raw_next = add i64 %slot, 1
  %wrapped = icmp uge i64 %raw_next, %cap
  %next_slot = select i1 %wrapped, i64 0, i64 %raw_next
  br label %probe
not_found:
  ret i1 false
}

define internal i64 @__hike_compact_map_len(%struct.__hike_compact_map* %m) {
entry:
  %null_m = icmp eq %struct.__hike_compact_map* %m, null
  br i1 %null_m, label %zero, label %load_live
load_live:
  %p_live = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 4
  %live = load i64, i64* %p_live
  ret i64 %live
zero:
  ret i64 0
}

define internal i1 @__hike_compact_map_entry_at(%struct.__hike_compact_map* %m, i64 %ordinal, i64* %out_key, i64* %out_value) {
entry:
  %null_m = icmp eq %struct.__hike_compact_map* %m, null
  br i1 %null_m, label %not_found, label %load_entries
load_entries:
  %p_len = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 3
  %len = load i64, i64* %p_len
  %out_of_range = icmp uge i64 %ordinal, %len
  br i1 %out_of_range, label %not_found, label %scan
scan:
  %p_entries = getelementptr %struct.__hike_compact_map, %struct.__hike_compact_map* %m, i32 0, i32 1
  %entries = load %struct.__hike_compact_map_entry*, %struct.__hike_compact_map_entry** %p_entries
  %entry_ptr = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entries, i64 %ordinal
  %p_state = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 4
  %state = load i64, i64* %p_state
  %live = icmp ne i64 %state, 0
  br i1 %live, label %copy, label %not_found
copy:
  %p_key = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 1
  %key = load i64, i64* %p_key
  %p_value = getelementptr %struct.__hike_compact_map_entry, %struct.__hike_compact_map_entry* %entry_ptr, i32 0, i32 2
  %value = load i64, i64* %p_value
  store i64 %key, i64* %out_key
  store i64 %value, i64* %out_value
  ret i1 true
not_found:
  store i64 0, i64* %out_key
  store i64 0, i64* %out_value
  ret i1 false
}

attributes #0 = { noinline nounwind "no-builtins" }
