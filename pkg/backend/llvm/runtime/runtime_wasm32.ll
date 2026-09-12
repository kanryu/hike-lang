; ==============================================================================
; Hike Runtime for WebAssembly (wasm32-unknown-unknown)
; ==============================================================================

; --- 32-bit Allocator Declarations ---
declare noalias i8* @malloc(i32)
declare noalias i8* @calloc(i32, i32)
declare void @free(i8*)

; --- POSIX / WASM Host Threading & Synchronization Imports ---
declare i32 @hike_thread_spawn(void (i8*)*, i8*)
declare i8* @hike_event_create()
declare void @hike_event_signal(i8*)
declare i32 @hike_event_wait(i8*, i32)
declare void @hike_event_destroy(i8*)
declare void @hike_sleep_ms(i32)
declare i64 @hike_now_ns()

; --- 32-bit Task Descriptor: 20 bytes (ptr*4 + i32*1) ---
%struct.__hike_task = type { void (i8*, i8*)*, i8*, i8*, i32, i8* }

; ワーカースレッドのエントリサンク
define internal void @__hike_task_worker_thunk(i8* %param) {
entry:
  %task = bitcast i8* %param to %struct.__hike_task*
  %p_fn = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 0
  %fn = load void (i8*, i8*)*, void (i8*, i8*)** %p_fn
  %p_env = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 1
  %env = load i8*, i8** %p_env
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  %buf = load i8*, i8** %p_buf

  ; タスク本体を実行
  call void %fn(i8* %env, i8* %buf)

  ; 完了フラグ更新
  %p_done = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 3
  store i32 1, i32* %p_done

  ; 起床シグナル発火
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  %ev = load i8*, i8** %p_ev
  %has_ev = icmp ne i8* %ev, null
  br i1 %has_ev, label %do_signal, label %exit
do_signal:
  call void @hike_event_signal(i8* %ev)
  br label %exit
exit:
  ret void
}

; タスクの非同期オフロード
define internal %struct.__hike_task* @__hike_async(i8* %fn_ptr, i8* %env_ptr, i32 %ret_size) {
entry:
  %raw_task = call i8* @malloc(i32 20)
  %task = bitcast i8* %raw_task to %struct.__hike_task*

  %fn_thunk = bitcast i8* %fn_ptr to void (i8*, i8*)*
  %p_fn = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 0
  store void (i8*, i8*)* %fn_thunk, void (i8*, i8*)** %p_fn

  %p_env = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 1
  store i8* %env_ptr, i8** %p_env

  %need_buf = icmp sgt i32 %ret_size, 0
  br i1 %need_buf, label %alloc_buf, label %no_buf
alloc_buf:
  %buf = call i8* @malloc(i32 %ret_size)
  br label %set_buf
no_buf:
  br label %set_buf
set_buf:
  %buf_val = phi i8* [ %buf, %alloc_buf ], [ null, %no_buf ]
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  store i8* %buf_val, i8** %p_buf

  %p_done = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 3
  store i32 0, i32* %p_done

  ; POSIX/WASM 抽象イベントの作成
  %ev = call i8* @hike_event_create()
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  store i8* %ev, i8** %p_ev

  ; スレッド生成 API を呼び出し
  call i32 @hike_thread_spawn(void (i8*)* @__hike_task_worker_thunk, i8* %raw_task)
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
  ; -1 = INFINITE 待機
  call i32 @hike_event_wait(i8* %ev, i32 -1)
  call void @hike_event_destroy(i8* %ev)
  store i8* null, i8** %p_ev
  br label %get_res
get_res:
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  %buf = load i8*, i8** %p_buf
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; Pure Memory & String Builtin Implementations (32-bit Native, wasm32)
; ------------------------------------------------------------------------------

; メモリブロックの複製 (32-bit)
define internal i8* @memcpy32(i8* %dst, i8* %src, i32 %n) #0 {
entry:
  %cmp = icmp eq i32 %n, 0
  br i1 %cmp, label %exit, label %loop.body
loop.body:
  %i = phi i32 [ 0, %entry ], [ %i.next, %loop.body ]
  %p_src = getelementptr inbounds i8, i8* %src, i32 %i
  %val = load i8, i8* %p_src, align 1
  %p_dst = getelementptr inbounds i8, i8* %dst, i32 %i
  store i8 %val, i8* %p_dst, align 1
  %i.next = add i32 %i, 1
  %cont = icmp ult i32 %i.next, %n
  br i1 %cont, label %loop.body, label %exit
exit:
  ret i8* %dst
}

; メモリブロックの比較 (32-bit)
define internal i32 @memcmp32(i8* %s1, i8* %s2, i32 %n) #0 {
entry:
  %cmp = icmp eq i32 %n, 0
  br i1 %cmp, label %ret_zero, label %loop.body
loop.body:
  %i = phi i32 [ 0, %entry ], [ %i.next, %loop.inc ]
  %p1 = getelementptr inbounds i8, i8* %s1, i32 %i
  %b1 = load i8, i8* %p1, align 1
  %p2 = getelementptr inbounds i8, i8* %s2, i32 %i
  %b2 = load i8, i8* %p2, align 1
  %diff = icmp ne i8 %b1, %b2
  br i1 %diff, label %calc_diff, label %loop.inc
loop.inc:
  %i.next = add i32 %i, 1
  %cont = icmp ult i32 %i.next, %n
  br i1 %cont, label %loop.body, label %ret_zero
calc_diff:
  %u1 = zext i8 %b1 to i32
  %u2 = zext i8 %b2 to i32
  %res = sub i32 %u1, %u2
  ret i32 %res
ret_zero:
  ret i32 0
}

; 文字列長の算出 (32-bit)
define internal i32 @strlen32(i8* %s) #0 {
entry:
  %is_null = icmp eq i8* %s, null
  br i1 %is_null, label %ret_zero, label %loop.body
loop.body:
  %len = phi i32 [ 0, %entry ], [ %len.next, %loop.body ]
  %p = getelementptr inbounds i8, i8* %s, i32 %len
  %c = load i8, i8* %p, align 1
  %is_end = icmp eq i8 %c, 0
  %len.next = add i32 %len, 1
  br i1 %is_end, label %ret_len, label %loop.body
ret_len:
  ret i32 %len
ret_zero:
  ret i32 0
}

; 文字列の辞書順比較 (32-bit)
define internal i32 @strcmp32(i8* %s1, i8* %s2) #0 {
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
  %idx = phi i32 [ 0, %check_s2 ], [ %idx.next, %loop.inc ]
  %p1 = getelementptr inbounds i8, i8* %s1, i32 %idx
  %c1 = load i8, i8* %p1, align 1
  %p2 = getelementptr inbounds i8, i8* %s2, i32 %idx
  %c2 = load i8, i8* %p2, align 1
  %diff = icmp ne i8 %c1, %c2
  br i1 %diff, label %calc_diff, label %check_end
check_end:
  %is_end = icmp eq i8 %c1, 0
  br i1 %is_end, label %ret_zero, label %loop.inc
loop.inc:
  %idx.next = add i32 %idx, 1
  br label %loop.body
calc_diff:
  %u1 = zext i8 %c1 to i32
  %u2 = zext i8 %c2 to i32
  %res = sub i32 %u1, %u2
  ret i32 %res
}

; ------------------------------------------------------------------------------
; String Runtime Functions (32-bit, wasm32)
; ------------------------------------------------------------------------------

; 文字列等価比較 (hike_streq: a == b) (32-bit)
define internal i1 @hike_streq32(i8* %a, i8* %b) #0 {
entry:
  %eq_ptr = icmp eq i8* %a, %b
  br i1 %eq_ptr, label %ret_true, label %check_null
check_null:
  %a_null = icmp eq i8* %a, null
  %b_null = icmp eq i8* %b, null
  %either_null = or i1 %a_null, %b_null
  br i1 %either_null, label %ret_false, label %do_strcmp
do_strcmp:
  %res = call i32 @strcmp32(i8* %a, i8* %b)
  %is_zero = icmp eq i32 %res, 0
  ret i1 %is_zero
ret_true:
  ret i1 true
ret_false:
  ret i1 false
}

; 部分文字列の切り出し (hike_substr: s[low:high]) (32-bit)
define internal i8* @hike_substr32(i8* %s, i32 %low, i32 %high) #0 {
entry:
  %s_null = icmp eq i8* %s, null
  br i1 %s_null, label %ret_null, label %do_sub
do_sub:
  %len = sub i32 %high, %low
  %alloc_size = add i32 %len, 1
  %buf = call i8* @malloc(i32 %alloc_size)
  %src_ptr = getelementptr inbounds i8, i8* %s, i32 %low
  call i8* @memcpy32(i8* %buf, i8* %src_ptr, i32 %len)
  %null_pos = getelementptr inbounds i8, i8* %buf, i32 %len
  store i8 0, i8* %null_pos
  ret i8* %buf
ret_null:
  ret i8* null
}

; 文字列連結 (hike_strcat: a + b) (32-bit)
define internal i8* @hike_strcat32(i8* %a, i8* %b) #0 {
entry:
  %len_a = call i32 @strlen32(i8* %a)
  %len_b = call i32 @strlen32(i8* %b)
  %total_len = add i32 %len_a, %len_b
  %alloc_size = add i32 %total_len, 1
  %buf = call i8* @malloc(i32 %alloc_size)
  call i8* @memcpy32(i8* %buf, i8* %a, i32 %len_a)
  %dst_b = getelementptr inbounds i8, i8* %buf, i32 %len_a
  call i8* @memcpy32(i8* %dst_b, i8* %b, i32 %len_b)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i32 %total_len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; スライス ([]byte) から null 終端文字列 (string) への安全な複製変換 (32-bit)
define internal i8* @__hike_slice_to_str32(i8* %ptr, i32 %len) #0 {
entry:
  %null_chk = icmp eq i8* %ptr, null
  br i1 %null_chk, label %ret_empty, label %alloc
ret_empty:
  %empty = call i8* @malloc(i32 1)
  store i8 0, i8* %empty
  ret i8* %empty
alloc:
  %alloc_size = add i32 %len, 1
  %buf = call i8* @malloc(i32 %alloc_size)
  call i8* @memcpy32(i8* %buf, i8* %ptr, i32 %len)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i32 %len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; Attributes
; ------------------------------------------------------------------------------
attributes #0 = { noinline nounwind "no-builtins" }