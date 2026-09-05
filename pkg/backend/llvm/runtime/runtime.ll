; ==============================================================================
; Hike Language Builtin Runtime (LLVM IR)
; ==============================================================================

; ------------------------------------------------------------------------------
; External C Standard Library Declarations
; ------------------------------------------------------------------------------
declare noalias i8* @malloc(i64)
declare noalias i8* @calloc(i64, i64)
declare void @free(i8*)
declare i32 @strcmp(i8*, i8*)
declare i64 @strlen(i8*)
declare i8* @memcpy(i8*, i8*, i64)
declare i32 @memcmp(i8*, i8*, i64)
declare i64 @printf(i8*, ...)

; ------------------------------------------------------------------------------
; OS Native Threading & Synchronization (Kernel32 / Libc-Free)
; ------------------------------------------------------------------------------
declare i32 @QueueUserWorkItem(i32 (i8*)*, i8*, i32)
declare i8* @CreateEventA(i8*, i32, i32, i8*)
declare i32 @SetEvent(i8*)
declare i32 @WaitForSingleObject(i8*, i32)
declare i32 @CloseHandle(i8*)
declare void @Sleep(i32)

; ------------------------------------------------------------------------------
; Memory Management Types
; ------------------------------------------------------------------------------
%struct.Arena = type { i8*, i64, i64 }
%struct.Allocator = type { i8*, i8* }

; ------------------------------------------------------------------------------
; Standard OS Native Sleep Binding (std/time)
; ------------------------------------------------------------------------------

define void @c_os_sleep_ms(i32 %ms) {
entry:
  call void @Sleep(i32 %ms)
  ret void
}

define void @os_sleep_ms(i32 %ms) {
entry:
  call void @Sleep(i32 %ms)
  ret void
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
  call i32 @SetEvent(i8* %ev)
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
  %ev = call i8* @CreateEventA(i8* null, i32 0, i32 0, i8* null)
  %p_ev = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 4
  store i8* %ev, i8** %p_ev

  ; OS スレッドプールへ投入 (WT_EXECUTEDEFAULT = 0)
  call i32 @QueueUserWorkItem(i32 (i8*)* @__hike_task_worker_thunk, i8* %raw_task, i32 0)

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
  call i32 @WaitForSingleObject(i8* %ev, i32 -1)
  call i32 @CloseHandle(i8* %ev)
  store i8* null, i8** %p_ev
  br label %get_res
poll_loop:
  call void @Sleep(i32 1)
  %done_poll = load i32, i32* %p_done
  %is_done_poll = icmp ne i32 %done_poll, 0
  br i1 %is_done_poll, label %get_res, label %poll_loop
get_res:
  %p_buf = getelementptr inbounds %struct.__hike_task, %struct.__hike_task* %task, i32 0, i32 2
  %buf = load i8*, i8** %p_buf
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; String Runtime Functions
; ------------------------------------------------------------------------------

; 文字列等価比較 (hike_streq: a == b)
define internal i1 @hike_streq(i8* %a, i8* %b) {
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

; 部分文字列の切り出し (hike_substr: s[low:high])
define internal i8* @hike_substr(i8* %s, i64 %low, i64 %high) {
entry:
  %s_null = icmp eq i8* %s, null
  br i1 %s_null, label %ret_null, label %do_sub
do_sub:
  %len = sub i64 %high, %low
  %alloc_size = add i64 %len, 1
  %buf = call i8* @malloc(i64 %alloc_size)
  %src_ptr = getelementptr inbounds i8, i8* %s, i64 %low
  call i8* @memcpy(i8* %buf, i8* %src_ptr, i64 %len)
  %null_pos = getelementptr inbounds i8, i8* %buf, i64 %len
  store i8 0, i8* %null_pos
  ret i8* %buf
ret_null:
  ret i8* null
}

; 文字列連結 (hike_strcat: a + b)
define internal i8* @hike_strcat(i8* %a, i8* %b) {
entry:
  %len_a = call i64 @strlen(i8* %a)
  %len_b = call i64 @strlen(i8* %b)
  %total_len = add i64 %len_a, %len_b
  %alloc_size = add i64 %total_len, 1
  %buf = call i8* @malloc(i64 %alloc_size)
  call i8* @memcpy(i8* %buf, i8* %a, i64 %len_a)
  %dst_b = getelementptr inbounds i8, i8* %buf, i64 %len_a
  call i8* @memcpy(i8* %dst_b, i8* %b, i64 %len_b)
  %null_ptr = getelementptr inbounds i8, i8* %buf, i64 %total_len
  store i8 0, i8* %null_ptr
  ret i8* %buf
}

; ------------------------------------------------------------------------------
; Hash Map Runtime Types & Functions
; ------------------------------------------------------------------------------

%struct.__hike_map_entry = type { i64, i64, i64, %struct.__hike_map_entry* }
%struct.__hike_map = type { %struct.__hike_map_entry**, i64, i64, i64 }

; 文字列 FNV-1a ハッシュ算出
define internal i64 @__hike_hash_str(i8* %s) {
entry:
  %null_chk = icmp eq i8* %s, null
  br i1 %null_chk, label %ret_zero, label %loop_init
ret_zero:
  ret i64 0
loop_init:
  br label %loop.cond
loop.cond:
  %h = phi i64 [ -3750763034362895579, %loop_init ], [ %h.next, %loop.body ]
  %ptr = phi i8* [ %s, %loop_init ], [ %ptr.next, %loop.body ]
  %ch = load i8, i8* %ptr
  %is_null = icmp eq i8 %ch, 0
  br i1 %is_null, label %loop.end, label %loop.body
loop.body:
  %ch.zext = zext i8 %ch to i64
  %h.xor = xor i64 %h, %ch.zext
  %h.next = mul i64 %h.xor, 1099511628211
  %ptr.next = getelementptr inbounds i8, i8* %ptr, i64 1
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
  %eq_ptr = icmp eq i8* %p1, %p2
  br i1 %eq_ptr, label %ret_true, label %check_null
check_null:
  %n1 = icmp eq i8* %p1, null
  %n2 = icmp eq i8* %p2, null
  %either_null = or i1 %n1, %n2
  br i1 %either_null, label %ret_false, label %do_cmp
do_cmp:
  %res = call i32 @strcmp(i8* %p1, i8* %p2)
  %is_z = icmp eq i32 %res, 0
  ret i1 %is_z
ret_true:
  ret i1 true
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