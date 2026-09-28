param(
    [Parameter(Mandatory = $true)]
    [string]$InputIr,
    [Parameter(Mandatory = $true)]
    [string]$OutputReport
)

$lines = Get-Content -LiteralPath $InputIr
$records = [System.Collections.Generic.List[object]]::new()
$currentFunction = "<module>"
$currentPackage = ""
$pending = $null
$sourcePackageCache = @{}
$counts = @{
    string_retain = 0
    string_release = 0
    slice_retain = 0
    slice_release = 0
}

foreach ($line in $lines) {
    if ($line -match '^define(?:\s+[^@]+)?@([^\s(]+)\((.*?)\)') {
        $currentFunction = $Matches[1]
        $currentPackage = ""
        if ($currentFunction -match '^([^_]+)_(.+)$') {
            $currentPackage = $Matches[1]
        }
        $pending = $null
        continue
    }

    if ($line -match '^\s*; ownership: (string_retain|string_release|slice_retain|slice_release) var=(\S+) target=(\S+)(?: at (.*))?$') {
        $ownershipApi = $Matches[1]
        $ownershipVariable = $Matches[2]
        $ownershipTarget = $Matches[3]
        $location = if ($Matches[4]) { $Matches[4] } else { "<source location unavailable>" }
        $embeddedSource = ''
        if ($location -match '^(.*:\d+:\d+) source: (.*)$') {
            $location = $Matches[1]
            $embeddedSource = $Matches[2]
        }
        $pending = [ordered]@{
            api = $ownershipApi
            variable = $ownershipVariable
            target = $ownershipTarget
            source = $location
            embedded_source = $embeddedSource
            function = $currentFunction
            package = $currentPackage
            comment = $line.Trim()
        }
        continue
    }

    if ($line -match '^\s*call void @__(hike_string_retain|hike_string_release|hike_slice_retain|hike_slice_release)\((.*)\)') {
        $api = $Matches[1] -replace '^hike_', ''
        $counts[$api]++
        $sourcePath = if ($pending) { $pending.source -replace ':\d+:\d+$', '' } else { '' }
        $sourcePackage = $currentPackage
        $sourceLineText = ''
        if ($sourcePath -and $sourcePath -notlike '<*') {
            if (-not $sourcePackageCache.ContainsKey($sourcePath)) {
                $declared = ''
                if (Test-Path -LiteralPath $sourcePath) {
                    $declaredMatch = Select-String -LiteralPath $sourcePath -Pattern '^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)' | Select-Object -First 1
                    if ($declaredMatch) { $declared = $declaredMatch.Matches[0].Groups[1].Value }
                }
                if (-not $declared) {
                    $parent = Split-Path -Parent $sourcePath
                    $declared = if ($parent) { Split-Path -Leaf $parent } else { [System.IO.Path]::GetFileNameWithoutExtension($sourcePath) }
                }
                $sourcePackageCache[$sourcePath] = $declared
            }
            $sourcePackage = $sourcePackageCache[$sourcePath]
            if ((Test-Path -LiteralPath $sourcePath) -and ($pending.source -match ':(\d+):\d+$')) {
                $sourceLineNumber = [int]$Matches[1]
                $sourceLine = Get-Content -LiteralPath $sourcePath | Select-Object -Skip ($sourceLineNumber - 1) -First 1
                if ($null -ne $sourceLine) { $sourceLineText = $sourceLine.Trim() }
            }
        }
        $record = [ordered]@{
            function = $currentFunction
            package = $sourcePackage
            api = $api
            variable = if ($pending) { $pending.variable } else { "<missing -vv ownership comment>" }
            target = if ($pending) { $pending.target } else { "<missing -vv ownership comment>" }
            source = if ($pending) { $pending.source } else { "<missing -vv ownership comment>" }
            source_context = if ($pending.embedded_source) { $pending.embedded_source } else { $sourceLineText }
            comment = if ($pending) { $pending.comment } else { "<missing -vv ownership comment>" }
            call = $line.Trim()
        }
        $records.Add([pscustomobject]$record)
        $pending = $null
        continue
    }

    if ($line -notmatch '^\s*$' -and $line -notmatch '^;') {
        $pending = $null
    }
}

$writer = [System.IO.StreamWriter]::new($OutputReport, $false, [System.Text.UTF8Encoding]::new($false))
try {
    $writer.WriteLine("HikeC self EmitIR -vv retain/release report")
    $writer.WriteLine("Input: $InputIr")
    $writer.WriteLine("Generated: $(Get-Date -Format o)")
    $writer.WriteLine("All records below are matched to the -vv ownership comment immediately preceding the LLVM call.")
    $writer.WriteLine()
    $writer.WriteLine("SUMMARY")
    foreach ($key in @('string_retain', 'string_release', 'slice_retain', 'slice_release')) {
        $writer.WriteLine(("  {0}={1}" -f $key, $counts[$key]))
    }
    $writer.WriteLine(("  total={0}" -f $records.Count))
    $writer.WriteLine()

    $groups = $records | Group-Object function
    foreach ($group in $groups) {
        $first = $group.Group[0]
        $writer.WriteLine(("FUNCTION {0}" -f $first.function))
        if ($first.package) { $writer.WriteLine(("  package: {0}" -f $first.package)) }
        $writer.WriteLine(("  calls: {0}" -f $group.Count))
        foreach ($r in $group.Group) {
            $writer.WriteLine(("  {0} var={1} target={2} source={3}" -f $r.api, $r.variable, $r.target, $r.source))
            if ($r.source_context) { $writer.WriteLine(("    source_context: {0}" -f $r.source_context)) }
            $writer.WriteLine(("    {0}" -f $r.comment))
            $writer.WriteLine(("    {0}" -f $r.call))
        }
        $writer.WriteLine()
    }

    $missing = $records | Where-Object { $_.target -like '<missing*' }
    $writer.WriteLine("COMMENT_COVERAGE")
    $writer.WriteLine(("  calls_with_missing_ownership_comment={0}" -f $missing.Count))
}
finally {
    $writer.Dispose()
}
