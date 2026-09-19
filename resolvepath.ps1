# resolvepath.ps1
#
# Usage:
#   . .\resolvepath.ps1
#
# The leading dot and space are required so that PATH and LIB remain available
# in the current PowerShell session.

$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$vsPath = $null

if (Test-Path $vswhere) {
    $vsPath = & $vswhere -latest -products '*' `
        -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 `
        -property installationPath
}

if (-not $vsPath) {
    $vsPath = "C:\Program Files\Microsoft Visual Studio\2022\Community"
}

# MSVC
$msvcBase = Join-Path $vsPath "VC\Tools\MSVC"
if (Test-Path $msvcBase) {
    $latestMsvc = Get-ChildItem $msvcBase -Directory |
        Sort-Object Name -Descending |
        Select-Object -First 1

    if ($latestMsvc) {
        $env:LIB = (Join-Path $latestMsvc.FullName "lib\x64") + ";$env:LIB"
        $env:PATH = (Join-Path $latestMsvc.FullName "bin\Hostx64\x64") + ";$env:PATH"
    }
}

# Windows SDK
$sdkBase = "${env:ProgramFiles(x86)}\Windows Kits\10\Lib"
if (Test-Path $sdkBase) {
    $latestSdk = Get-ChildItem $sdkBase -Directory |
        Where-Object { $_.Name -match '^\d+\.' } |
        Sort-Object Name -Descending |
        Select-Object -First 1

    if ($latestSdk) {
        $ucrt = Join-Path $latestSdk.FullName "ucrt\x64"
        $um = Join-Path $latestSdk.FullName "um\x64"
        $env:LIB = "$ucrt;$um;$env:LIB"
    }
}

Write-Host "[OK] MSVC and Windows SDK paths loaded into the current session." -ForegroundColor Green
