# Windows 上的安装 / 更新 / 卸载测试（CI 使用）。镜像站已在 http://127.0.0.1:9000 运行。
$ErrorActionPreference = "Stop"
$Mirror = "http://127.0.0.1:9000"
$Bin = Join-Path $env:LOCALAPPDATA "Programs\buugoo\buugoo.exe"
function Check($cond, $msg) { if (-not $cond) { Write-Host "FAIL: $msg" -ForegroundColor Red; exit 1 } else { Write-Host "PASS: $msg" -ForegroundColor Green } }

& .\mirror\v0.0.2\install.ps1 -Mirror $Mirror -Version v0.0.1 -Yes
Check ((& $Bin version --short) -eq "v0.0.1") "安装 v0.0.1"
Check (Test-Path (Join-Path $env:APPDATA "buugoo\config.yaml")) "生成配置文件"

& .\mirror\v0.0.2\install.ps1 -Mirror $Mirror -Yes
Check ((& $Bin version --short) -eq "v0.0.2") "重复执行安装脚本完成更新"

& $Bin update --rollback -y
Check ((& $Bin version --short) -eq "v0.0.1") "回滚到 v0.0.1"

& $Bin update -y --mirror $Mirror
Check ((& $Bin version --short) -eq "v0.0.2") "buugoo update 更新到 v0.0.2"

& .\mirror\v0.0.2\uninstall.ps1 -Yes
Check (-not (Test-Path $Bin)) "卸载删除程序"
Check (Test-Path (Join-Path $env:LOCALAPPDATA "buugoo")) "卸载保留数据"
Write-Host "全部通过"
