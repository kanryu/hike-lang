module hikec-go
hike 0.1.0

# Go-Hike compatibility configuration for the wasm-hikec command package.
# Paths are relative to this module file because the command directory is the
# Go-Hike module root for this entry point.
GoReplace hikec-go/pkg => ../../pkg
GoReplace std => ../../std
GoReplace bytes => ../../std/bytes
GoReplace bufio => ../../std/stub/bufio
GoReplace embed => ../../std/stub/embed
GoReplace fmt => ../../std/fmt
GoReplace os => ../../std/stub/os
GoReplace os/exec => ../../std/stub/os/exec
GoReplace path => ../../std/stub/path
GoReplace path/filepath => ../../std/stub/path/filepath
GoReplace regexp => ../../std/stub/regexp
GoReplace runtime => ../../std/stub/runtime
GoReplace sort => ../../std/stub/sort
GoReplace strconv => ../../std/strconv
GoReplace strings => ../../std/strings
GoReplace sync => ../../std/stub/sync
GoReplace unicode => ../../std/stub/unicode

# The replacement targets above deliberately point back to the repository
# packages while keeping the project-wide hike.mod out of this command's
# module discovery path.
