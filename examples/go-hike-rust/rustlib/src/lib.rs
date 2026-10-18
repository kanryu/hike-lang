#![no_std]

use core::alloc::{GlobalAlloc, Layout};
use core::ffi::c_void;
use core_maths::CoreFloat;
use libc::{c_char, c_longlong};
use core::sync::atomic::{AtomicUsize, Ordering};
use url::Url;

type Handle = *mut c_void;
type DWord = u32;
type Bool = i32;

const STD_OUTPUT_HANDLE: DWord = (-11i32) as DWord;
static mut URL_HOST: [u8; 256] = [0; 256];
static mut HEAP: [u8; 1024 * 1024] = [0; 1024 * 1024];
static HEAP_OFFSET: AtomicUsize = AtomicUsize::new(0);

struct BumpAllocator;

unsafe impl GlobalAlloc for BumpAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let base = core::ptr::addr_of_mut!(HEAP) as *mut u8 as usize;
        loop {
            let current = HEAP_OFFSET.load(Ordering::Relaxed);
            let start = (current + layout.align() - 1) & !(layout.align() - 1);
            let end = match start.checked_add(layout.size()) {
                Some(end) if end <= 1024 * 1024 => end,
                _ => return core::ptr::null_mut(),
            };
            if HEAP_OFFSET
                .compare_exchange_weak(current, end, Ordering::Relaxed, Ordering::Relaxed)
                .is_ok()
            {
                return (base + start) as *mut u8;
            }
        }
    }

    unsafe fn dealloc(&self, _: *mut u8, _: Layout) {}
}

#[global_allocator]
static ALLOCATOR: BumpAllocator = BumpAllocator;

unsafe extern "system" {
    fn GetStdHandle(standard_handle: DWord) -> Handle;
    fn WriteFile(
        handle: Handle,
        buffer: *const c_void,
        bytes_to_write: DWord,
        bytes_written: *mut DWord,
        overlapped: *mut c_void,
    ) -> Bool;
}

#[panic_handler]
fn panic(_: &core::panic::PanicInfo<'_>) -> ! {
    loop {}
}

// The Windows GNU staticlib may retain this EH personality reference even
// though this crate is no_std and uses aborting panics.
#[unsafe(no_mangle)]
pub extern "C" fn rust_eh_personality() {}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_get_file_size(_: *const u8, len: i64) -> i64 {
    len + 100
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_string_length(value: *const u8) -> i64 {
    c_strlen(value)
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_echo_string(value: *const u8) -> *const u8 {
    value
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_print_string(value: *const u8) -> i64 {
    let len = c_strlen(value);
    let handle = GetStdHandle(STD_OUTPUT_HANDLE);
    let mut written = 0;
    if handle.is_null() || handle == (-1isize) as Handle {
        return -1;
    }
    if len > 0 && WriteFile(handle, value.cast(), len as DWord, &mut written, core::ptr::null_mut()) == 0 {
        return -1;
    }
    let newline = b"\r\n";
    if WriteFile(handle, newline.as_ptr().cast(), 2, &mut written, core::ptr::null_mut()) == 0 {
        return -1;
    }
    len
}

#[unsafe(no_mangle)]
pub extern "C" fn fizz_math_score(value: c_longlong) -> c_longlong {
    // Exercise core_maths inside Rust while exposing only a plain C ABI.
    CoreFloat::round(CoreFloat::sqrt(value as f64) * 10.0) as c_longlong
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_url_host(value: *const c_char) -> *const u8 {
    let input = match c_str(value) {
        Some(input) => input,
        None => return core::ptr::null(),
    };
    let parsed = match Url::parse(input) {
        Ok(url) => url,
        Err(_) => return core::ptr::null(),
    };
    let host = match parsed.host_str() {
        Some(host) => host.as_bytes(),
        None => return core::ptr::null(),
    };
    if host.len() + 1 > 256 {
        return core::ptr::null();
    }
    let buffer = core::slice::from_raw_parts_mut(core::ptr::addr_of_mut!(URL_HOST) as *mut u8, 256);
    buffer[..host.len()].copy_from_slice(host);
    buffer[host.len()] = 0;
    buffer.as_ptr()
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_url_port(value: *const c_char) -> c_longlong {
    let input = match c_str(value) {
        Some(input) => input,
        None => return -1,
    };
    match Url::parse(input) {
        Ok(url) => url.port().map_or(-1, c_longlong::from),
        Err(_) => -1,
    }
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_read_file(_: *const u8, read_size: i64, buf: *mut u8) -> i64 {
    if read_size <= 0 || buf.is_null() {
        return 0;
    }
    for i in 0..read_size as usize {
        *buf.add(i) = b'A' + (i % 26) as u8;
    }
    read_size
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn fizz_get_metadata(_: *const u8, _: i64, out_len: *mut i64) -> *const u8 {
    static VALUE: &[u8] = b"fizzbuzz\0";
    if !out_len.is_null() {
        *out_len = 8;
    }
    VALUE.as_ptr()
}

unsafe fn c_strlen(value: *const u8) -> i64 {
    if value.is_null() {
        return 0;
    }
    let mut len = 0;
    while *value.add(len as usize) != 0 {
        len += 1;
    }
    len
}

unsafe fn c_str<'a>(value: *const c_char) -> Option<&'a str> {
    if value.is_null() {
        return None;
    }
    let len = c_strlen(value.cast());
    core::str::from_utf8(core::slice::from_raw_parts(value.cast(), len as usize)).ok()
}
