# Buugoo（布谷）卸载脚本（Windows）。buugoo 程序本身损坏时也能使用。
#
#   irm https://github.com/HugehardJiang/buugoo/releases/latest/download/uninstall.ps1 | iex
#
# 默认删除程序并保留配置和数据；-Purge 同时删除配置和数据（不可恢复，需确认；非交互模式需加 -Yes）。
[CmdletBinding()]
param([switch]$Purge, [switch]$Yes)
$ErrorActionPreference = "Stop"

$ConfigDir = Join-Path $env:APPDATA "buugoo"
$Manifest = Join-Path $ConfigDir "install.json"
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\buugoo"
$DataDir = Join-Path $env:LOCALAPPDATA "buugoo"
if (Test-Path $Manifest) {
  $m = Get-Content $Manifest -Raw | ConvertFrom-Json
  $InstallDir = $m.bin_dir; $DataDir = $m.data_dir; $ConfigDir = $m.config_dir
}

if ($Purge -and -not $Yes) {
  $ans = Read-Host "将永久删除 $ConfigDir 和 $DataDir（包括所有聊天记录、记忆和备份），无法恢复。输入 yes 确认"
  if ($ans -ne "yes") { Write-Host "已取消"; exit 1 }
}

$done = @()
Get-Process buugoo -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "$InstallDir*" } | Stop-Process -Force
if (Test-Path $InstallDir) { Remove-Item -Recurse -Force $InstallDir; $done += "已删除程序目录 $InstallDir" }
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$newPath = ($userPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';'
if ($newPath -ne $userPath) { [Environment]::SetEnvironmentVariable("Path", $newPath, "User"); $done += "已从用户 PATH 中移除" }

if ($Purge) {
  foreach ($d in @($ConfigDir, $DataDir)) {
    if ($d -and (Test-Path $d)) { Remove-Item -Recurse -Force $d; $done += "已删除 $d" }
  }
} elseif (Test-Path $Manifest) {
  Remove-Item -Force $Manifest
}

Write-Host "卸载结果："
if ($done.Count -eq 0) { Write-Host "  没有找到需要卸载的内容" } else { $done | ForEach-Object { Write-Host "  OK $_" } }
if (-not $Purge) {
  Write-Host "已保留："
  Write-Host "  配置 $ConfigDir"
  Write-Host "  数据 $DataDir"
}
