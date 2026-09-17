module hikec-go
hike 0.1.0

# Hike self-hosting compatibility configuration.
#
# Enable this file with: hikec emit-ir -go-hike=1 <source.go>
# GoReplace entries rewrite Go import paths to Hike standard-library paths.
# Local hikec-go/pkg/* imports are self-hosted compiler packages and are not
# standard-library replacements.

GoReplace bytes => std/bytes
GoReplace fmt => std/fmt
GoReplace os => std/os
GoReplace regexp => std/regexp
GoReplace strconv => std/strconv
GoReplace strings => std/strings

# The following Go standard-library imports currently have no corresponding
# implementation under std/ and therefore remain unsupported by self-hosting:
#
# - bufio
# - embed
# - os/exec
# - path
# - path/filepath
# - runtime
# - sort
# - sync
# - unicode

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
# Missing from Hike std/:
# - bufio, embed, os/exec, path, path/filepath
# - runtime, sort, sync, unicode
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
