# build_debug.ps1
$ErrorActionPreference = "Stop"

$src = "main.hike"
$ll = "main.ll"
$hashFile = ".main.ll.md5"
$exe = "app.exe"

# 1. Hikeコンパイラを実行して最新の LLVM IR を生成
go run ../../cmd/hikec $src -g -o $ll
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# 2. 生成された IR の MD5 ハッシュを計算
$currentHash = (Get-FileHash -Path $ll -Algorithm MD5).Hash

# 3. 前回のハッシュと比較
$prevHash = ""
if (Test-Path $hashFile) {
    $prevHash = Get-Content $hashFile -Raw
    $prevHash = $prevHash.Trim()
}

# 4. ハッシュが一致し、かつ実行バイナリが存在する場合は Clang をスキップ
if (($currentHash -eq $prevHash) -and (Test-Path $exe)) {
    Write-Host "[BUILD CACHE] IR is unchanged (MD5: $currentHash). Skipping Clang build." -ForegroundColor Green
    exit 0
}

# 5. ハッシュが異なる（または初回/exe未存在）場合は Clang でビルド
Write-Host "[BUILD] IR changed. Compiling with Clang..." -ForegroundColor Yellow
clang -g -O0 $ll -o $exe
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# 6. 新しいハッシュ値を保存
$currentHash | Set-Content -Path $hashFile -NoNewline
Write-Host "[BUILD] Clang compilation finished." -ForegroundColor Green