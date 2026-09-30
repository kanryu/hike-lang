module hikec-go
hike 0.1.0

# Go-Hike compatibility configuration for compiling the HikeC command itself.
GoReplace hikec-go/pkg => ../../pkg
GoReplace std => ../../std
GoReplace text/template => ../../std/text/template
GoReplace strings => ../../std/strings
GoReplace encoding/json => ../../std/encoding/json
GoReplace bytes => ../../std/bytes
GoReplace bufio => ../../std/bufio
GoReplace embed => ../../std/embed
GoReplace fmt => ../../std/fmt
GoReplace io => ../../std/io
GoReplace os => ../../std/os
GoReplace os/exec => ../../std/os/exec
GoReplace path => ../../std/path
GoReplace path/filepath => ../../std/path/filepath
GoReplace regexp => ../../std/regexp
GoReplace runtime => ../../std/runtime
GoReplace runtime/debug => ../../std/runtime/debug
GoReplace sort => ../../std/sort
GoReplace strconv => ../../std/strconv
GoReplace strings => ../../std/strings
GoReplace sync => ../../std/sync
GoReplace unicode => ../../std/unicode
GoReplace unicode/utf8 => ../../std/unicode/utf8
