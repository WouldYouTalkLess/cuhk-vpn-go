param([switch]$Dev,[switch]$Test)
$ErrorActionPreference='Stop'
Push-Location $PSScriptRoot
try {
    # Use the existing sibling toolchain on this workstation, or a regular Go installation elsewhere.
    $localTools=Join-Path (Split-Path $PSScriptRoot) 'openconnect\.tools'
    if(-not (Get-Command go -ErrorAction SilentlyContinue) -and (Test-Path (Join-Path $localTools 'go\bin\go.exe'))){
        $env:PATH=(Join-Path $localTools 'go\bin')+';'+(Join-Path $localTools 'bin')+';'+$env:PATH
        $env:GOPATH=Join-Path $localTools 'gopath'
        $env:GOCACHE=Join-Path $localTools 'gocache'
    }
    if(-not (Get-Command go -ErrorAction SilentlyContinue)){throw 'Install Go 1.25 or newer first.'}
    go mod download
    if($LASTEXITCODE -ne 0){throw 'Dependency download failed.'}
    if($Test){
        go test ./...
        if($LASTEXITCODE -ne 0){throw 'Tests failed.'}
        go vet ./...
        if($LASTEXITCODE -ne 0){throw 'Go vet failed.'}
    }
    $wails=Get-Command wails -ErrorAction SilentlyContinue
    if(-not $wails){
        go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
        if($LASTEXITCODE -ne 0){throw 'Wails installation failed.'}
        $toolBin=go env GOBIN
        if(-not $toolBin){$toolBin=Join-Path (go env GOPATH) 'bin'}
        $env:PATH=$toolBin+';'+$env:PATH
    }
    if($Dev){wails dev}else{wails build}
    if($LASTEXITCODE -ne 0){throw 'Wails build failed.'}
} finally {Pop-Location}
