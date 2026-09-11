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