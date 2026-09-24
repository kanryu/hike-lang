; Hike native Linux OS runtime

declare i32 @pthread_create(i64*, i8*, i8* (i8*)*, i8*)
declare i32 @pthread_mutex_init(i8*, i8*)
declare i32 @pthread_mutex_lock(i8*)
declare i32 @pthread_mutex_unlock(i8*)
declare i32 @pthread_cond_init(i8*, i8*)
declare i32 @pthread_cond_wait(i8*, i8*)
declare i32 @pthread_cond_signal(i8*)
declare i32 @pthread_mutex_destroy(i8*)
declare i32 @pthread_cond_destroy(i8*)
declare i32 @clock_gettime(i32, i8*)
declare i32 @nanosleep(i8*, i8*)

%struct.__hike_linux_event = type { [40 x i8], [48 x i8], i32 }

define internal i32 @hike_thread_spawn(i32 (i8*)* %fn, i8* %param) {
entry:
  %tid = alloca i64, align 8
  %start = bitcast i32 (i8*)* %fn to i8* (i8*)*
  %r = call i32 @pthread_create(i64* %tid, i8* null, i8* (i8*)* %start, i8* %param)
  ret i32 %r
}

define internal i8* @hike_event_create() {
entry:
  %raw = call i8* @malloc(i64 96)
  %ev = bitcast i8* %raw to %struct.__hike_linux_event*
  %mp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 0
  %cp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 1
  %m = bitcast [40 x i8]* %mp to i8*
  %c = bitcast [48 x i8]* %cp to i8*
  call i32 @pthread_mutex_init(i8* %m, i8* null)
  call i32 @pthread_cond_init(i8* %c, i8* null)
  %state = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 2
  store i32 0, i32* %state
  ret i8* %raw
}

define internal i32 @hike_event_signal(i8* %raw) {
entry:
  %ev = bitcast i8* %raw to %struct.__hike_linux_event*
  %mp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 0
  %cp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 1
  %m = bitcast [40 x i8]* %mp to i8*
  %c = bitcast [48 x i8]* %cp to i8*
  %state = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 2
  call i32 @pthread_mutex_lock(i8* %m)
  store i32 1, i32* %state
  call i32 @pthread_cond_signal(i8* %c)
  call i32 @pthread_mutex_unlock(i8* %m)
  ret i32 0
}

define internal i32 @hike_event_wait(i8* %raw, i32 %timeout) {
entry:
  %ev = bitcast i8* %raw to %struct.__hike_linux_event*
  %mp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 0
  %cp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 1
  %m = bitcast [40 x i8]* %mp to i8*
  %c = bitcast [48 x i8]* %cp to i8*
  %state = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 2
  call i32 @pthread_mutex_lock(i8* %m)
  br label %check
check:
  %ready = load i32, i32* %state
  %has = icmp ne i32 %ready, 0
  br i1 %has, label %done, label %wait
wait:
  call i32 @pthread_cond_wait(i8* %c, i8* %m)
  br label %check
done:
  store i32 0, i32* %state
  call i32 @pthread_mutex_unlock(i8* %m)
  ret i32 0
}

define internal i32 @hike_event_destroy(i8* %raw) {
entry:
  %ev = bitcast i8* %raw to %struct.__hike_linux_event*
  %mp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 0
  %cp = getelementptr %struct.__hike_linux_event, %struct.__hike_linux_event* %ev, i32 0, i32 1
  %m = bitcast [40 x i8]* %mp to i8*
  %c = bitcast [48 x i8]* %cp to i8*
  call i32 @pthread_mutex_destroy(i8* %m)
  call i32 @pthread_cond_destroy(i8* %c)
  call void @free(i8* %raw)
  ret i32 0
}

define internal void @hike_sleep_ms(i32 %ms) {
entry:
  %ts = alloca [16 x i8], align 8
  %sec = bitcast [16 x i8]* %ts to i64*
  %nsec = getelementptr i64, i64* %sec, i32 1
  %s = udiv i32 %ms, 1000
  %r = urem i32 %ms, 1000
  %ns = mul i32 %r, 1000000
  %s64 = zext i32 %s to i64
  %ns64 = zext i32 %ns to i64
  store i64 %s64, i64* %sec
  store i64 %ns64, i64* %nsec
  %p = bitcast [16 x i8]* %ts to i8*
  call i32 @nanosleep(i8* %p, i8* null)
  ret void
}

define internal i64 @hike_now_ns() {
entry:
  %ts = alloca [16 x i8], align 8
  %p = bitcast [16 x i8]* %ts to i8*
  call i32 @clock_gettime(i32 1, i8* %p)
  %sec = bitcast [16 x i8]* %ts to i64*
  %nsec = getelementptr i64, i64* %sec, i32 1
  %s = load i64, i64* %sec
  %ns = load i64, i64* %nsec
  %s_ns = mul i64 %s, 1000000000
  %total = add i64 %s_ns, %ns
  ret i64 %total
}

; Compatibility entry point for Hike programs that bind the platform Sleep API.
define void @Sleep(i64 %ms) {
entry:
  %ms32 = trunc i64 %ms to i32
  call void @hike_sleep_ms(i32 %ms32)
  ret void
}
