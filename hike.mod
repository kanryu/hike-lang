module hikec-go
hike 0.1.0

# Hike self-hosting compatibility configuration.
#
# Enable this file with: hikec emit-ir -go-hike=1 <source.go>
# GoReplace entries rewrite Go import paths to Hike standard-library paths.
# Local hikec-go/pkg/* imports are self-hosted compiler packages and are not
# standard-library replacements.

GoReplace bytes => std/bytes
GoReplace bufio => std/stub/bufio
GoReplace embed => std/stub/embed
GoReplace fmt => std/fmt
GoReplace os => std/os
GoReplace os/exec => std/stub/os/exec
GoReplace path => std/stub/path
GoReplace path/filepath => std/stub/path/filepath
GoReplace regexp => std/regexp
GoReplace runtime => std/stub/runtime
GoReplace sort => std/stub/sort
GoReplace strconv => std/strconv
GoReplace strings => std/strings
GoReplace sync => std/stub/sync
GoReplace unicode => std/stub/unicode

# All standard-library imports found in pkg/**/*.go now have a minimal Hike
# compatibility module under std/stub/. The implementations intentionally cover
# only the APIs required by the self-hosted compiler.
#
# Runtime limitations are documented in the corresponding module source.

# Import inventory collected from pkg/**/*.go (excluding *_test.go):
#
# Mapped to Hike std/:
# - bytes       -> std/bytes
# - fmt         -> std/fmt
# - os          -> std/os
# - regexp      -> std/regexp
# - strconv     -> std/strconv
# - strings     -> std/strings
#
# No standard-library package remains missing for the current pkg/**/*.go scan.
#
# Internal compiler packages found in the same scan:
# - hikec-go/pkg/ast
# - hikec-go/pkg/backend/llvm
# - hikec-go/pkg/codegen
# - hikec-go/pkg/codegen/go
# - hikec-go/pkg/compiler
# - hikec-go/pkg/diag
# - hikec-go/pkg/hir
# - hikec-go/pkg/lexer
# - hikec-go/pkg/loader
# - hikec-go/pkg/logger
# - hikec-go/pkg/lower
# - hikec-go/pkg/mod
# - hikec-go/pkg/parser
# - hikec-go/pkg/sema
# - hikec-go/pkg/target
# - hikec-go/pkg/token
# - hikec-go/pkg/transform
