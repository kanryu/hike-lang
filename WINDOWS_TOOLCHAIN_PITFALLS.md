# Windows Toolchain Pitfalls

Windows HikeC development has two toolchain worlds. MinGW-w64 and MSVC may
both use LLVM Clang, but they differ in target triples, linkers, runtime
libraries, environment variables, and debug formats.

Before choosing a development workflow, inspect the default Clang target:

```powershell
clang -print-target-triple
```

For example:

```text
x86_64-pc-windows-msvc
```

This result means that the installed Clang defaults to the MSVC development
style: Visual Studio and Windows SDK libraries, `clang-cl` or `lld-link`, and
CodeView/PDB debugging. A MinGW target such as
`x86_64-w64-windows-gnu` means the MinGW-w64 development style: MSYS2's GNU
runtime and libraries, GNU-compatible linking, and DWARF debugging. Do not
select flags or debuggers until this target decision is explicit.

## 1. Recommended First: MinGW-w64

MinGW-w64 is the recommended choice when portable DWARF debugging is wanted.

- Target: `x86_64-w64-windows-gnu`
- Compiler: MinGW-w64 Clang or GCC
- Debug format: DWARF
- MSVC `LIB` and Visual Studio startup libraries are not required

Install MSYS2 from <https://www.msys2.org/>. Open the **UCRT64** or
**MINGW64** terminal and update it:

```bash
pacman -Syu
pacman -Su
pacman -S --needed mingw-w64-ucrt-x86_64-clang mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-runtime mingw-w64-ucrt-x86_64-winpthreads
```

For a typical PowerShell setup:

```powershell
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
hikec build -target windows -g -o app.exe main.hike
```

The repository `clang-target.toml` contains the matching profile:

```toml
[profiles.windows-gnu]
command = "clang"
args = ["--target=x86_64-w64-windows-gnu", "-g", "{input}", "-o", "{output}"]
```

## 2. MSVC

Use MSVC when Visual Studio compatibility, CodeView, and PDB files are
required.

The official LLVM project Windows distribution provides the MSVC-oriented
Clang toolchain by default. In particular, its `clang` commonly reports:

```text
Target: x86_64-pc-windows-msvc
```

This is not the MinGW-w64 compiler. The official LLVM package expects the
Visual Studio and Windows SDK environment when it links Windows programs.
Use the MSVC setup below, or explicitly select the MinGW target and use an
MSYS2 MinGW-w64 toolchain.

- Target: `x86_64-pc-windows-msvc`
- Compiler: `clang-cl` or MSVC-targeted Clang
- Linker: `lld-link` or the Visual Studio linker
- Debug format: CodeView / PDB
- Required components: Visual Studio 2022 C++ tools and Windows SDK
- Required environment: `PATH`, `LIB`, and sometimes `INCLUDE`

Install **Build Tools for Visual Studio 2022** from
<https://visualstudio.microsoft.com/downloads/>. Select **Desktop development
with C++**, MSVC v143 x64/x86 tools, and a Windows 10 or Windows 11 SDK.

The official LLVM distribution is available at
<https://github.com/llvm/llvm-project/releases>.

Load the MSVC and SDK paths before building. This is equivalent to the
repository's `resolvepath.ps1` setup:

```powershell
$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$vsPath = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
$msvc = Get-ChildItem (Join-Path $vsPath "VC\Tools\MSVC") -Directory | Sort-Object Name -Descending | Select-Object -First 1
$sdk = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\Lib" -Directory | Where-Object { $_.Name -match '^\d+\.' } | Sort-Object Name -Descending | Select-Object -First 1
$env:PATH = "$(Join-Path $msvc.FullName 'bin\Hostx64\x64');$env:PATH"
$env:LIB = "$(Join-Path $msvc.FullName 'lib\x64');$(Join-Path $sdk.FullName 'ucrt\x64');$(Join-Path $sdk.FullName 'um\x64');$env:LIB"
hikec build -target windows-msvc -g -o app.exe main.hike
```

The MSVC profile in `clang-target.toml` is:

```toml
[profiles.windows-msvc]
command = "clang-cl"
args = ["/Zi", "/Od", "{input}", "/Fe:{output}", "/link", "msvcrt.lib", "legacy_stdio_definitions.lib"]
```

## 3. Three Major Pitfalls

### Pitfall A: The official LLVM installer defaults to MSVC

Official LLVM Clang commonly reports:

```text
Target: x86_64-pc-windows-msvc
```

Check it directly with:

```powershell
clang -print-target-triple
```

When the result is `x86_64-pc-windows-msvc`, use the MSVC development style.
When the result is `x86_64-w64-windows-gnu`, use the MinGW-w64 development
style. The target triple determines which runtime libraries, linker options,
environment variables, and debugger format are appropriate.

Without Visual Studio and Windows SDK paths, linking may fail with:

```text
lld-link: error: could not open 'legacy_stdio_definitions.lib'
```

Load the MSVC environment, or explicitly select MinGW:

```powershell
clang --target=x86_64-w64-windows-gnu -g main.ll -o app.exe
```

### Pitfall B: Mixing MSYS2 Go with official Go

The MSYS2 `mingw-w64-x86_64-go` package assumes the MSYS2 filesystem and
toolchain layout. Mixing it with an external `go.exe` or `GOROOT` can produce
mismatched standard libraries, incompatible assemblers, unsupported flags such
as `-std`, and `runtime/cgo` failures.

Use official Go for the Go compiler and `GOROOT`. Use MSYS2 for MinGW-w64
Clang/GCC and runtime libraries, but do not mix the two Go distributions:

```powershell
go version
go env GOROOT GOPATH GOHOSTOS GOHOSTARCH
where.exe go
where.exe gcc
where.exe clang
```

### Pitfall C: DWARF and PDB are not interchangeable

HikeC currently emits LLVM `DICompileUnit`, `DISubprogram`, `DILocalVariable`,
and `DILocation` metadata in the DWARF model. An MSVC link may therefore
produce both DWARF sections (`.debug_info`, `.debug_line`, and others) and a
CodeView debug directory with a PDB.

The existence of a PDB does not guarantee that all HikeC source variables are
available through that PDB. HikeC-specific information may remain in DWARF
until CodeView emission is implemented in the LLVM backend. Use a DWARF-capable
debugger for current HikeC source mappings; use Visual Studio or another PDB
debugger when the generated metadata is CodeView-compatible.

## 4. Diagnostics

```powershell
clang --version
clang-cl --version
go version
where.exe clang
where.exe clang-cl
where.exe go
where.exe lld-link
echo $env:LIB
```

Inspect the generated information with:

```powershell
llvm-readobj --coff-debug-directory app.exe
llvm-pdbutil dump -summary app.pdb
llvm-dwarfdump --debug-info app.exe
```

Always check the actual target triple, linker, and debug format.

## 5. Toolchain Configuration Policy

Compiler and linker details belong in `clang-target.toml`, not in HikeC source code.
HikeC treats a profile as an opaque command description and expands only:

- `{input}`: the generated LLVM IR file;
- `{output}`: the requested output file.

All other arguments are passed to the configured command in the configured
order. This keeps MSVC switches, MinGW switches, runtime libraries, and linker
options out of the compiler implementation.
