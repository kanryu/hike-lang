; Hike native Windows OS runtime

declare i32 @QueueUserWorkItem(i32 (i8*)*, i8*, i32)
declare i8* @CreateEventA(i8*, i32, i32, i8*)
declare i32 @SetEvent(i8*)
declare i32 @WaitForSingleObject(i8*, i32)
declare i32 @CloseHandle(i8*)
declare void @Sleep(i32)
declare i64 @GetTickCount64()

define internal i32 @hike_thread_spawn(i32 (i8*)* %fn, i8* %param) {
entry:
  %r = call i32 @QueueUserWorkItem(i32 (i8*)* %fn, i8* %param, i32 0)
  ret i32 %r
}
define internal i8* @hike_event_create() {
entry:
  %r = call i8* @CreateEventA(i8* null, i32 0, i32 0, i8* null)
  ret i8* %r
}
define internal i32 @hike_event_signal(i8* %ev) {
entry:
  %r = call i32 @SetEvent(i8* %ev)
  ret i32 %r
}
define internal i32 @hike_event_wait(i8* %ev, i32 %timeout) {
entry:
  %r = call i32 @WaitForSingleObject(i8* %ev, i32 %timeout)
  ret i32 %r
}
define internal i32 @hike_event_destroy(i8* %ev) {
entry:
  %r = call i32 @CloseHandle(i8* %ev)
  ret i32 %r
}
define internal void @hike_sleep_ms(i32 %ms) {
entry:
  call void @Sleep(i32 %ms)
  ret void
}
define internal i64 @hike_now_ns() {
entry:
  %ms = call i64 @GetTickCount64()
  %ns = mul i64 %ms, 1000000
  ret i64 %ns
}
