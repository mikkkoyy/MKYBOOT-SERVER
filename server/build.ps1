# MKYBOOT Windows-native server — reproducible build (run from server/)
#
#   .\build.ps1                 -> dist\mkyboot-server.exe (Windows AMD64)
#   .\build.ps1 -Test           -> run gofmt check, vet and tests, then build
#
# No CGO, fully static, no console suppression (the server logs to stdout in
# console mode). Cross-build flags mirror the existing launcher CI.

$ErrorActionPreference = 'Stop'

$go = (Get-Command go -ErrorAction SilentlyContinue)
if (-not $go) {
    $candidate = 'C:\Program Files\Go\bin\go.exe'
    if (Test-Path $candidate) {
        $env:Path = 'C:\Program Files\Go\bin;' + $env:Path
    } else {
        throw 'Go toolchain not found. Install Go for Windows AMD64 (>= 1.21) or add it to PATH.'
    }
}

Write-Host "Using: $(go version)"

$runTests = $false
if ($args -contains '-Test') { $runTests = $true }

Set-Location -Path $PSScriptRoot

if ($runTests) {
    Write-Host '== gofmt check =='
    $unformatted = & gofmt -l .
    if ($unformatted) { throw "gofmt reports unformatted files:`n$unformatted" }

    Write-Host '== go vet =='
    & go vet -unsafeptr=false ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }

    Write-Host '== go test =='
    & go test -timeout 120s ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
}

Write-Host '== build =='
New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
& go build -trimpath -ldflags '-s -w' -o 'dist\mkyboot-server.exe' .
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

$exe = Join-Path $PSScriptRoot 'dist\mkyboot-server.exe'
$size = (Get-Item $exe).Length
Write-Host "BUILD OK: $exe ($([math]::Round($size/1MB,2)) MB)"
