module hikec-go
hike 0.1.0

# Go-Hike compatibility configuration for the wasm-hikec command package.
# Paths are relative to this module file because the command directory is the
# Go-Hike module root for this entry point.
GoReplace hikec-go/pkg => ../../pkg
GoReplace std => ../../std
GoReplace encoding/json => ../../std/encoding/json
GoReplace bytes => ../../std/bytes
GoReplace bufio => ../../std/bufio
GoReplace embed => ../../std/embed
GoReplace fmt => ../../std/fmt
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

# The replacement targets above deliberately point back to the repository
# packages while keeping the project-wide hike.mod out of this command's
# module discovery path.
