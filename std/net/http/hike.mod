module std/net/http

hike 1.0

// WinHTTP is required only by programs that import std/net/http.
package . {
    target windows {
        link: "-lwinhttp"
    }
    target linux {
        link: "-l:libcurl.so.4"
    }
}
