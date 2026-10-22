<#
.SYNOPSIS
    Manage the Windows Firewall rule that lets other devices reach the FastLink-tool panel.

.DESCRIPTION
    The panel listens on 0.0.0.0 when "监听范围" is set to 局域网 + 本机, but Windows
    Firewall blocks unsolicited inbound connections on Private/Public profiles unless a
    rule exists. This script creates exactly one rule for that purpose.

    Design choices:
      - Scoped to LocalSubnet only. The panel is a LAN tool; opening it to the whole
        internet would expose the subscription keys it hands out.
      - Bound to the executable path, not just the port, so another program cannot
        inherit the allowance by listening on the same port.
      - Inbound only. Outbound is allowed by default on Windows and this program only
        talks to the panel API and subscription hosts; adding outbound rules would
        widen the surface for no benefit.
      - Idempotent: re-running updates the existing rule instead of stacking duplicates.

    Requires an elevated PowerShell session (Run as Administrator).

.EXAMPLE
    .\scripts\firewall.ps1 -Apply
    .\scripts\firewall.ps1 -Status
    .\scripts\firewall.ps1 -Remove
#>
param(
    [switch]$Apply,
    [switch]$Remove,
    [switch]$Status,
    [string]$ExePath,
    [int]$Port = 8787,
    [string]$RuleName = "FastLink-tool Panel"
)

$ErrorActionPreference = "Stop"

function Test-Elevated {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    return ([Security.Principal.WindowsPrincipal]$id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-PanelExe {
    if ($ExePath) {
        if (-not (Test-Path $ExePath)) { throw "找不到可执行文件：$ExePath" }
        return (Resolve-Path $ExePath).Path
    }
    # Default to the release build next to this script's repository root.
    $root = Split-Path -Parent $PSScriptRoot
    foreach ($candidate in @("dist\fastlink-tool.exe", "dist\FastLink.exe")) {
        $full = Join-Path $root $candidate
        if (Test-Path $full) { return (Resolve-Path $full).Path }
    }
    throw "未找到 dist\fastlink-tool.exe。请先运行 scripts\build.ps1，或用 -ExePath 指定路径。"
}

function Get-ProfileState {
    Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction
}

function Get-ExistingRule {
    Get-NetFirewallRule -DisplayName $RuleName -ErrorAction SilentlyContinue
}

function Show-Status {
    Write-Host "== 防火墙配置文件 ==" -ForegroundColor Cyan
    Get-ProfileState | Format-Table -AutoSize

    $enabled = (Get-NetFirewallProfile | Where-Object { $_.Enabled }).Count
    if ($enabled -eq 0) {
        Write-Host "所有配置文件均已关闭防火墙，局域网可直接访问（当前无需规则）。" -ForegroundColor Yellow
    }

    Write-Host "== 面板规则 ==" -ForegroundColor Cyan
    $rule = Get-ExistingRule
    if (-not $rule) {
        Write-Host "规则「$RuleName」不存在。" -ForegroundColor Yellow
        Write-Host "若要用局域网访问，请以管理员身份运行： .\scripts\firewall.ps1 -Apply" -ForegroundColor Yellow
    } else {
        $app = $rule | Get-NetFirewallApplicationFilter
        $port = $rule | Get-NetFirewallPortFilter
        $addr = $rule | Get-NetFirewallAddressFilter
        [pscustomobject]@{
            名称   = $rule.DisplayName
            启用   = $rule.Enabled
            方向   = $rule.Direction
            动作   = $rule.Action
            配置文件 = $rule.Profile
            程序   = $app.Program
            协议   = $port.Protocol
            本地端口 = $port.LocalPort
            远程地址 = $addr.RemoteAddress
        } | Format-List
    }

    Write-Host "== 端口监听 ==" -ForegroundColor Cyan
    $listen = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    if ($listen) {
        $listen | Select-Object LocalAddress, LocalPort, OwningProcess | Format-Table -AutoSize
        $any = $listen | Where-Object { $_.LocalAddress -in @("0.0.0.0", "::") }
        if (-not $any) {
            Write-Host "仅监听环回地址：局域网设备无法访问（高级设置里改为「局域网 + 本机」）。" -ForegroundColor Yellow
        }
    } else {
        Write-Host "端口 $Port 未被监听（服务未运行）。" -ForegroundColor Yellow
    }
}

if ($Status -or (-not $Apply -and -not $Remove)) {
    Show-Status
    exit 0
}

if (-not (Test-Elevated)) {
    Write-Host "需要管理员权限。请右键 PowerShell → 以管理员身份运行，再执行本脚本。" -ForegroundColor Red
    exit 1
}

if ($Remove) {
    $rule = Get-ExistingRule
    if ($rule) {
        $rule | Remove-NetFirewallRule
        Write-Host "已删除规则「$RuleName」。" -ForegroundColor Green
    } else {
        Write-Host "规则「$RuleName」不存在，无需删除。" -ForegroundColor Yellow
    }
    exit 0
}

# -Apply
$exe = Get-PanelExe
Write-Host "面板程序：$exe"
Write-Host "端口：$Port"

$existing = Get-ExistingRule
if ($existing) {
    Write-Host "更新已有规则「$RuleName」..." -ForegroundColor Cyan
    $existing | Remove-NetFirewallRule
}

New-NetFirewallRule `
    -DisplayName $RuleName `
    -Description "Allow LAN devices to reach the FastLink-tool panel (subscription + QR)." `
    -Direction Inbound `
    -Action Allow `
    -Protocol TCP `
    -LocalPort $Port `
    -Program $exe `
    -RemoteAddress LocalSubnet `
    -Profile Any `
    -Enabled True | Out-Null

Write-Host ""
Write-Host "已创建规则：" -ForegroundColor Green
Get-ExistingRule | ForEach-Object {
    $app = $_ | Get-NetFirewallApplicationFilter
    $port = $_ | Get-NetFirewallPortFilter
    $addr = $_ | Get-NetFirewallAddressFilter
    Write-Host ("  名称      {0}" -f $_.DisplayName)
    Write-Host ("  方向/动作 {0} / {1}" -f $_.Direction, $_.Action)
    Write-Host ("  程序      {0}" -f $app.Program)
    Write-Host ("  协议/端口 {0} / {1}" -f $port.Protocol, $port.LocalPort)
    Write-Host ("  远程地址  {0}" -f $addr.RemoteAddress)
}
Write-Host ""
Write-Host "提示：规则限定为「本地子网」，公网无法访问。撤销请运行 -Remove。" -ForegroundColor Cyan
