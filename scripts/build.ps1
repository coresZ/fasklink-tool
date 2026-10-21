param(
    [string]$Version = "1.0.0"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $Root "dist"
Set-Location $Root

Write-Host "==> go test"
go test ./...
if ($LASTEXITCODE -ne 0) { throw "go test failed" }

Write-Host "==> go build"
New-Item -ItemType Directory -Path $Dist -Force | Out-Null
$ldflags = "-s -w -H=windowsgui -X main.version=$Version"
go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $Dist "fastlink-tool.exe") ./cmd/fastlink-tool
if ($LASTEXITCODE -ne 0) { throw "go build failed" }

$exe = Join-Path $Dist "fastlink-tool.exe"
$hash = Get-FileHash $exe -Algorithm SHA256
$hash.Hash | Set-Content (Join-Path $Dist "fastlink-tool.exe.sha256") -Encoding ASCII
Write-Host ("    fastlink-tool.exe {0:N0} bytes" -f (Get-Item $exe).Length)
Write-Host ("    sha256 {0}" -f $hash.Hash)
