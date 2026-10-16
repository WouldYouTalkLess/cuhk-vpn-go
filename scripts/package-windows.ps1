param([string]$OutputDirectory)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot
$version = (Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'wails.json') | ConvertFrom-Json).info.productVersion
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $projectRoot ('dist\CUHK-VPN-Windows-' + $version) }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $OutputDirectory) { throw 'Output directory already exists; choose a new directory.' }
$binRoot = Join-Path $projectRoot 'build\bin'
$coreBundles = @{
    'windows-amd64' = '9.21-8ee104b6a782'
    'windows-arm64' = '9.21-a6f61d4f7a13'
}
# Validate all inputs before creating a release. No source data/ or old EXEs are copied.
foreach ($name in @('CuhkVpn.exe', 'CuhkVpn-arm64.exe')) {
    if (-not (Test-Path -LiteralPath (Join-Path $binRoot $name) -PathType Leaf)) { throw ('Missing GUI: ' + $name) }
}
foreach ($platform in $coreBundles.Keys) {
    $bundleRoot = Join-Path (Join-Path $binRoot ('cores\' + $platform)) $coreBundles[$platform]
    $receipt = Get-Content -Raw -LiteralPath (Join-Path $bundleRoot 'bundle.json') | ConvertFrom-Json
    if ($receipt.plan -ne $coreBundles[$platform]) { throw 'Core plan mismatch.' }
    foreach ($property in $receipt.files.PSObject.Properties) {
        $coreFile = [IO.Path]::GetFullPath((Join-Path $bundleRoot $property.Name))
        if (-not $coreFile.StartsWith($bundleRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe core manifest path.' }
        if ((Get-FileHash -LiteralPath $coreFile -Algorithm SHA256).Hash.ToLowerInvariant() -ne $property.Value) { throw ('Core hash mismatch: ' + $property.Name) }
    }
}
$null = New-Item -ItemType Directory -Path $OutputDirectory
foreach ($name in @('CuhkVpn.exe', 'CuhkVpn-arm64.exe')) { Copy-Item -LiteralPath (Join-Path $binRoot $name) -Destination $OutputDirectory }
foreach ($platform in $coreBundles.Keys) {
    $sourceBundle = Join-Path (Join-Path $binRoot ('cores\' + $platform)) $coreBundles[$platform]
    $targetPlatform = Join-Path $OutputDirectory ('cores\' + $platform)
    $null = New-Item -ItemType Directory -Path $targetPlatform -Force
    Copy-Item -LiteralPath $sourceBundle -Destination $targetPlatform -Recurse
}
$dataRoot = Join-Path $OutputDirectory 'data'
$null = New-Item -ItemType Directory -Path $dataRoot
$defaults = [ordered]@{
    server='https://vpn.cuhk.edu.cn'; group='CUHK(SZ)'; username=''; cidrs=@('10.20.0.0/16')
    keepDNS=$false; remember=$false; corePath=''; passwordStorage='portable'
}
$utf8 = [Text.UTF8Encoding]::new($false)
[IO.File]::WriteAllText((Join-Path $dataRoot 'settings.json'), ($defaults | ConvertTo-Json -Depth 3), $utf8)
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'windows-release-readme.txt') -Destination (Join-Path $OutputDirectory 'README.txt')
$licenses = Join-Path $OutputDirectory 'LICENSES'
$null = New-Item -ItemType Directory -Path $licenses
Copy-Item -LiteralPath (Join-Path $projectRoot 'frontend\icons\LICENSE.txt') -Destination (Join-Path $licenses 'Lucide-and-Feather.txt')
Copy-Item -LiteralPath (Join-Path $projectRoot 'internal\core\LICENSE.go-codesign') -Destination (Join-Path $licenses 'go-codesign.txt')
$extraNotices = Join-Path $projectRoot 'build\package-work\notices'
if (Test-Path -LiteralPath $extraNotices) {
    foreach ($notice in Get-ChildItem -LiteralPath $extraNotices -Force) { Copy-Item -LiteralPath $notice.FullName -Destination $licenses -Recurse }
}
[IO.File]::WriteAllText((Join-Path $OutputDirectory 'VERSION.txt'), ('CUHK VPN ' + $version + "`r`nWindows x64 / ARM64 GUI`r`nOpenConnect 9.21 portable core`r`n"), $utf8)
$files = @(Get-ChildItem -LiteralPath $OutputDirectory -Recurse -File | Sort-Object FullName)
$manifest = foreach ($file in $files) {
    $relative = $file.FullName.Substring($OutputDirectory.Length + 1).Replace('\','/')
    (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $relative
}
[IO.File]::WriteAllLines((Join-Path $OutputDirectory 'MANIFEST.sha256'), $manifest, $utf8)
$totalBytes = ($files | Measure-Object Length -Sum).Sum + (Get-Item -LiteralPath (Join-Path $OutputDirectory 'MANIFEST.sha256')).Length
[pscustomobject]@{directory=$OutputDirectory;files=$files.Count+1;sizeMiB=[math]::Round($totalBytes/1MB,2);personalDataIncluded=$false} | ConvertTo-Json
