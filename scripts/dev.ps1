<#
.SYNOPSIS
  FastLink-tool 一键开发启动（Windows）。

.DESCRIPTION
  检查 Go 环境，准备开发状态目录，然后以 --dev 模式运行源码。
  装有 air 时默认走热重载；任何非默认参数都会回落到 go run。

.EXAMPLE
  .\scripts\dev.ps1

.EXAMPLE
  .\scripts\dev.ps1 -Port 8790 -Root .run-dev2 -NoOpen
#>
param(
    [int]$Port = 8787,
    [string]$Root = ".run-dev",
    [switch]$NoOpen,
    [switch]$Plain,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Host "FastLink-tool 开发启动"
    Write-Host ""
    Write-Host "  -Port <端口>    监听端口，默认 8787"
    Write-Host "  -Root <目录>    状态目录，默认 .run-dev"
    Write-Host "  -NoOpen         不自动打开浏览器"
    Write-Host "  -Plain          强制使用 go run（默认在装有 air 时自动热重载）"
    Write-Host "  -Help           显示本帮助"
    exit 0
}

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "错误：未找到 go。请安装 Go 1.27+ 并加入 PATH：https://go.dev/dl/" -ForegroundColor Red
    exit 1
}

$goVersion = (& go version) -replace '^go version ', ''
New-Item -ItemType Directory -Path (Join-Path $Root ".run") -Force | Out-Null

# .air.toml 固定了 8787 端口与 --no-open，只有全默认时才走热重载，避免参数被静默忽略。
$useAir = (-not $Plain) -and ($Port -eq 8787) -and (-not $NoOpen) -and ($Root -eq ".run-dev") -and
    [bool](Get-Command air -ErrorAction SilentlyContinue)

$mode = "go run（改 Go 代码需重启）"
if ($useAir) { $mode = "air 热重载" }

Write-Host ""
Write-Host "FastLink-tool dev" -ForegroundColor Cyan
Write-Host "  地址: http://127.0.0.1:$Port/"
Write-Host "  状态: $Root"
Write-Host "  Go:   $goVersion"
Write-Host "  模式: $mode"
Write-Host "  退出: Ctrl+C"
Write-Host ""

if ($useAir) {
    & air
    exit $LASTEXITCODE
}

$goArgs = @("run", "./cmd/fastlink-tool", "--dev", "--root", $Root, "--port", "$Port")
if ($NoOpen) { $goArgs += "--no-open" }
& go @goArgs
exit $LASTEXITCODE
